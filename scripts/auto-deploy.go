package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/term"
)

var (
	cachedPassword string
	passwordSet    bool
)

// ANSI color codes
const (
	ColorReset  = "\033[0m"
	ColorYellow = "\033[33m"
	ColorGreen  = "\033[32m"
	ColorRed    = "\033[31m"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run scripts/auto-deploy.go <remote-host> <remote-path> <remote-user>")
		fmt.Println("Example: go run scripts/auto-deploy.go server-hostname /home/user/app server-username")
		os.Exit(1)
	}

	remoteHost := os.Args[1]
	remotePath := os.Args[2]
	remoteUser := os.Args[3]

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Watch directories
	watchDirs := []string{".", "internal"}
	for _, dir := range watchDirs {
		err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() && !strings.Contains(path, ".git") && !strings.Contains(path, "build") {
				log.Printf("Watching: %s", path)
				return watcher.Add(path)
			}
			return nil
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	// Test SSH connection first
	if !testSSHConnection(remoteHost, remoteUser) {
		log.Fatal("SSH connection test failed. Please check your connection and try again.")
	}

	// Initial build and deploy
	buildAndDeploy(remoteHost, remotePath, remoteUser)

	// Watch for changes
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if strings.HasSuffix(event.Name, ".go") || strings.HasSuffix(event.Name, ".mod") {
					log.Printf("Go file modified: %s", event.Name)
					time.Sleep(500 * time.Millisecond) // Debounce
					buildAndDeploy(remoteHost, remotePath, remoteUser)
				} else if strings.HasSuffix(event.Name, ".env") {
					log.Printf("Environment file modified: %s", event.Name)
					time.Sleep(500 * time.Millisecond) // Debounce
					deployEnvFile(remoteHost, remotePath, remoteUser, event.Name)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Error: %s", err)
		}
	}
}

func buildAndDeploy(remoteHost, remotePath, remoteUser string) {
	log.Println("Building application...")

	// Build for Linux
	buildCmd := exec.Command("go", "build", "-o", "build/slack-to-google-sheets-bot", "main.go")
	buildCmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64")

	if err := buildCmd.Run(); err != nil {
		log.Printf("Build failed: %s", err)
		return
	}

	log.Println("Deploying to remote server...")

	// Rsync binary to remote server
	rsyncCmd := exec.Command("rsync", "-avz", "--delete",
		"build/slack-to-google-sheets-bot",
		fmt.Sprintf("%s@%s:%s/", remoteUser, remoteHost, remotePath))

	// Capture both stdout and stderr
	output, err := rsyncCmd.CombinedOutput()
	if err != nil {
		log.Printf("Deploy failed: %s", err)
		log.Printf("Rsync output: %s", string(output))
		log.Printf("Check SSH connection to %s@%s", remoteUser, remoteHost)
		return
	}

	// Also sync .env file if it exists
	if _, err := os.Stat(".env"); err == nil {
		log.Println("Syncing .env file...")
		envRsyncCmd := exec.Command("rsync", "-avz",
			".env",
			fmt.Sprintf("%s@%s:%s/", remoteUser, remoteHost, remotePath))

		if err := envRsyncCmd.Run(); err != nil {
			log.Printf("Warning: .env file sync failed: %s", err)
		}
	}

	// Start or restart service on remote server (using cached password)
	log.Println("Starting/restarting service...")
	serviceCommand := "systemctl is-active slack-to-google-sheets-bot >/dev/null 2>&1 && sudo systemctl restart slack-to-google-sheets-bot || sudo systemctl start slack-to-google-sheets-bot"

	if err := runSudoCommand(remoteUser, remoteHost, serviceCommand); err != nil {
		log.Printf("Service start/restart failed: %s", err)
		log.Printf("Check SSH connection and sudo permissions for %s@%s", remoteUser, remoteHost)
		return
	}

	// Verify service is running
	log.Println("Verifying service status...")
	verifyCommand := "systemctl is-active slack-to-google-sheets-bot && echo 'Service is active' || echo 'Service is not active'"

	if err := runSudoCommand(remoteUser, remoteHost, verifyCommand); err != nil {
		log.Printf("⚠️  Could not verify service status: %s", err)
	}

	log.Println("✅ Deploy completed successfully")
}

func deployEnvFile(remoteHost, remotePath, remoteUser, envFilePath string) {
	log.Printf("Deploying environment file: %s", envFilePath)
	log.Println("Note: You may be prompted for sudo password during service restart")

	// Check if file exists
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		log.Printf("Environment file not found: %s", envFilePath)
		return
	}

	// Rsync env file to remote server
	rsyncCmd := exec.Command("rsync", "-avz",
		envFilePath,
		fmt.Sprintf("%s@%s:%s/", remoteUser, remoteHost, remotePath))

	// Capture both stdout and stderr
	output, err := rsyncCmd.CombinedOutput()
	if err != nil {
		log.Printf("Environment file deploy failed: %s", err)
		log.Printf("Rsync output: %s", string(output))
		log.Printf("Check SSH connection to %s@%s", remoteUser, remoteHost)
		return
	}

	// Start or restart service on remote server (using cached password)
	log.Println("Restarting service after environment file update...")
	serviceCommand := "systemctl is-active slack-to-google-sheets-bot >/dev/null 2>&1 && systemctl restart slack-to-google-sheets-bot || systemctl start slack-to-google-sheets-bot"

	if err := runSudoCommand(remoteUser, remoteHost, serviceCommand); err != nil {
		log.Printf("Service start/restart failed: %s", err)
		log.Printf("Check SSH connection and sudo permissions for %s@%s", remoteUser, remoteHost)
		return
	}

	log.Println("✅ Environment file deployed and service restarted")
}

func testSSHConnection(remoteHost, remoteUser string) bool {
	log.Printf("Testing SSH connection to %s@%s...", remoteUser, remoteHost)

	testCmd := exec.Command("ssh", "-o", "ConnectTimeout=10", "-o", "BatchMode=yes",
		fmt.Sprintf("%s@%s", remoteUser, remoteHost), "echo 'SSH connection test successful'")

	output, err := testCmd.CombinedOutput()
	if err != nil {
		log.Printf("❌ SSH connection failed: %s", err)
		log.Printf("SSH output: %s", string(output))
		log.Printf("Troubleshooting tips:")
		log.Printf("  1. Check if SSH key is properly configured")
		log.Printf("  2. Try manual SSH: ssh %s@%s", remoteUser, remoteHost)
		log.Printf("  3. Check if the remote host is reachable: ping %s", remoteHost)
		log.Printf("  4. Verify deploy.env has correct REMOTE_HOST and REMOTE_USER")
		return false
	}

	log.Printf("✅ SSH connection successful: %s", string(output))
	return true
}

func getPassword(remoteUser, remoteHost string) string {
	if passwordSet {
		return cachedPassword
	}

	// Yellow color for password prompt
	fmt.Printf("%sEnter sudo password for %s@%s: %s", ColorYellow, remoteUser, remoteHost, ColorReset)

	// Disable echo for password input
	fd := int(syscall.Stdin)
	password, err := term.ReadPassword(fd)
	if err != nil {
		log.Printf("Failed to read password: %s", err)
		return ""
	}

	fmt.Println() // New line after password input

	cachedPassword = string(password)
	passwordSet = true

	// Green color for success message
	fmt.Println("\033[32mPassword cached for this session\033[0m")
	return cachedPassword
}

func runSudoCommand(remoteUser, remoteHost, command string) error {
	password := getPassword(remoteUser, remoteHost)
	if password == "" {
		return fmt.Errorf("no password provided")
	}

	// Create a temporary script on remote server to handle sudo with password
	scriptContent := fmt.Sprintf("#!/bin/bash\necho '%s' | sudo -S %s", password, command)

	// Upload and execute the script
	uploadCmd := fmt.Sprintf("cat > /tmp/sudo_script.sh << 'EOF'\n%s\nEOF", scriptContent)

	// First, upload the script
	sshCmd1 := exec.Command("ssh", fmt.Sprintf("%s@%s", remoteUser, remoteHost), uploadCmd)
	if err := sshCmd1.Run(); err != nil {
		return fmt.Errorf("failed to upload script: %v", err)
	}

	// Make it executable and run it
	executeCmd := "chmod +x /tmp/sudo_script.sh && /tmp/sudo_script.sh && rm /tmp/sudo_script.sh"
	sshCmd2 := exec.Command("ssh", fmt.Sprintf("%s@%s", remoteUser, remoteHost), executeCmd)
	sshCmd2.Stdout = os.Stdout
	sshCmd2.Stderr = os.Stderr

	return sshCmd2.Run()
}
