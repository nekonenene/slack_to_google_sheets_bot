package main

import (
	"fmt"
	"log"
	"net/http"

	"slack-to-google-sheets-bot/internal/config"
)

func main() {
	cfg := config.Load()

	// Health check endpoint
	http.HandleFunc("/health", handleHealth)

	// Slack events endpoint
	http.HandleFunc("/slack/events", handleSlackEvents)

	fmt.Printf("Server starting on port %s\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "ok"}`))
}

func handleSlackEvents(w http.ResponseWriter, r *http.Request) {
	// TODO: Implement Slack events handling
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "received"}`))
}