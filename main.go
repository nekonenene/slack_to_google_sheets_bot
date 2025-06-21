package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"slack-to-google-sheets-bot/internal/config"
	"slack-to-google-sheets-bot/internal/slack"
)

func main() {
	cfg := config.Load()

	// Validate required configuration
	if cfg.SlackBotToken == "" || cfg.SlackSigningSecret == "" {
		log.Fatal("SLACK_BOT_TOKEN and SLACK_SIGNING_SECRET are required")
	}

	// Log configuration status
	log.Printf("Configuration loaded:")
	log.Printf("  SLACK_BOT_TOKEN: %s", maskToken(cfg.SlackBotToken))
	log.Printf("  SLACK_SIGNING_SECRET: %s", maskToken(cfg.SlackSigningSecret))
	log.Printf("  GOOGLE_SHEETS_CREDENTIALS length: %d", len(cfg.GoogleSheetsCredentials))
	log.Printf("  GOOGLE_SPREADSHEET_ID: %s", maskToken(cfg.SpreadsheetID))
	log.Printf("  PORT: %s", cfg.Port)

	// Health check endpoint
	http.HandleFunc("/health", handleHealth)

	// Slack events endpoint
	http.HandleFunc("/slack/events", handleSlackEvents(cfg))

	fmt.Printf("Server starting on port %s\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

func maskToken(token string) string {
	if len(token) < 8 {
		return "***"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

func handleSlackEvents(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Verify request signature
		if !slack.VerifySignature(cfg.SlackSigningSecret, r.Header, body) {
			log.Printf("Invalid signature")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var event slack.Event
		if err := json.Unmarshal(body, &event); err != nil {
			log.Printf("Error parsing JSON: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Handle URL verification challenge
		if event.Type == "url_verification" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(event.Challenge))
			return
		}

		// Handle events
		if event.Type == "event_callback" {
			// Response 200 OK immediately because HandleEvent usually takes time
			// Slack Events API requires 200 OK within 3 seconds : https://api.slack.com/apis/events-api#responding
			w.WriteHeader(http.StatusOK)

			// Handle the event asynchronously
			go func() {
				if err := slack.HandleEvent(cfg, &event); err != nil {
					log.Printf("Error handling event: %v", err)
				}
			}()

			return
		}

		w.WriteHeader(http.StatusOK)
	}
}
