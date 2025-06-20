package config

import (
	"os"
)

type Config struct {
	SlackBotToken     string
	SlackSigningSecret string
	GoogleSheetsCredentials string
	SpreadsheetID     string
	Port              string
}

func Load() *Config {
	return &Config{
		SlackBotToken:           os.Getenv("SLACK_BOT_TOKEN"),
		SlackSigningSecret:      os.Getenv("SLACK_SIGNING_SECRET"),
		GoogleSheetsCredentials: os.Getenv("GOOGLE_SHEETS_CREDENTIALS"),
		SpreadsheetID:           os.Getenv("SPREADSHEET_ID"),
		Port:                    getEnvOrDefault("PORT", "8080"),
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}