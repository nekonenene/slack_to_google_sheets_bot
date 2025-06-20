package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
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

	if err := rsyncCmd.Run(); err != nil {
		log.Printf("Deploy failed: %s", err)
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

	// Restart service on remote server
	restartCmd := exec.Command("ssh", fmt.Sprintf("%s@%s", remoteUser, remoteHost),
		fmt.Sprintf("sudo systemctl restart slack-to-google-sheets-bot || %s/slack-to-google-sheets-bot &", remotePath))

	if err := restartCmd.Run(); err != nil {
		log.Printf("Restart failed: %s", err)
		return
	}

	log.Println("✅ Deploy completed successfully")
}

func deployEnvFile(remoteHost, remotePath, remoteUser, envFilePath string) {
	log.Printf("Deploying environment file: %s", envFilePath)

	// Check if file exists
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		log.Printf("Environment file not found: %s", envFilePath)
		return
	}

	// Rsync env file to remote server
	rsyncCmd := exec.Command("rsync", "-avz",
		envFilePath,
		fmt.Sprintf("%s@%s:%s/", remoteUser, remoteHost, remotePath))

	if err := rsyncCmd.Run(); err != nil {
		log.Printf("Environment file deploy failed: %s", err)
		return
	}

	// Restart service on remote server
	restartCmd := exec.Command("ssh", fmt.Sprintf("%s@%s", remoteUser, remoteHost),
		fmt.Sprintf("sudo systemctl restart slack-to-google-sheets-bot || %s/slack-to-google-sheets-bot &", remotePath))

	if err := restartCmd.Run(); err != nil {
		log.Printf("Service restart failed: %s", err)
		return
	}

	log.Println("✅ Environment file deployed and service restarted")
}
