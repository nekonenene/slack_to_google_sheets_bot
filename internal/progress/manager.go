package progress

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"slack-to-google-sheets-bot/internal/sheets"
)

// ChannelProgress represents the progress state of channel history retrieval
type ChannelProgress struct {
	ChannelID         string                  `json:"channel_id"`
	ChannelName       string                  `json:"channel_name"`
	StartTime         time.Time               `json:"start_time"`
	LastUpdated       time.Time               `json:"last_updated"`
	LastCursor        string                  `json:"last_cursor"`
	TotalMessages     int                     `json:"total_messages"`
	ProcessedMessages int                     `json:"processed_messages"`
	Messages          []*sheets.MessageRecord `json:"messages"`
	Phase             string                  `json:"phase"` // "fetching", "writing", "completed"
}

// Manager handles progress persistence for channel history operations
type Manager struct {
	tmpDir string
}

// NewManager creates a new progress manager
func NewManager() *Manager {
	return &Manager{
		tmpDir: "/tmp/slack-bot-progress",
	}
}

// ensureTmpDir creates the temporary directory if it doesn't exist
func (m *Manager) ensureTmpDir() error {
	if err := os.MkdirAll(m.tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory: %v", err)
	}
	return nil
}

// getProgressFilePath returns the file path for a channel's progress
func (m *Manager) getProgressFilePath(channelID string) string {
	return filepath.Join(m.tmpDir, fmt.Sprintf("channel_%s.json", channelID))
}

// SaveProgress saves the current progress to a temporary file
func (m *Manager) SaveProgress(progress *ChannelProgress) error {
	if err := m.ensureTmpDir(); err != nil {
		return err
	}

	progress.LastUpdated = time.Now()

	filePath := m.getProgressFilePath(progress.ChannelID)
	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal progress: %v", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write progress file: %v", err)
	}

	log.Printf("Progress saved for channel %s: %d/%d messages, phase: %s",
		progress.ChannelID, progress.ProcessedMessages, progress.TotalMessages, progress.Phase)
	return nil
}

// LoadProgress loads progress from a temporary file
func (m *Manager) LoadProgress(channelID string) (*ChannelProgress, error) {
	filePath := m.getProgressFilePath(channelID)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, nil // No existing progress
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read progress file: %v", err)
	}

	var progress ChannelProgress
	if err := json.Unmarshal(data, &progress); err != nil {
		return nil, fmt.Errorf("failed to unmarshal progress: %v", err)
	}

	log.Printf("Progress loaded for channel %s: %d/%d messages, phase: %s, last updated: %s",
		progress.ChannelID, progress.ProcessedMessages, progress.TotalMessages,
		progress.Phase, progress.LastUpdated.Format("2006-01-02 15:04:05"))

	return &progress, nil
}

// HasProgress checks if there's existing progress for a channel
func (m *Manager) HasProgress(channelID string) bool {
	filePath := m.getProgressFilePath(channelID)
	_, err := os.Stat(filePath)
	return err == nil
}

// DeleteProgress removes the progress file for a channel
func (m *Manager) DeleteProgress(channelID string) error {
	filePath := m.getProgressFilePath(channelID)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // File doesn't exist, nothing to delete
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete progress file: %v", err)
	}

	log.Printf("Progress file deleted for channel %s", channelID)
	return nil
}

// UpdatePhase updates the current phase of progress
func (m *Manager) UpdatePhase(channelID, phase string) error {
	progress, err := m.LoadProgress(channelID)
	if err != nil {
		return err
	}
	if progress == nil {
		return fmt.Errorf("no progress found for channel %s", channelID)
	}

	progress.Phase = phase
	return m.SaveProgress(progress)
}

// AddMessages adds new messages to the progress
func (m *Manager) AddMessages(channelID string, messages []*sheets.MessageRecord) error {
	progress, err := m.LoadProgress(channelID)
	if err != nil {
		return err
	}
	if progress == nil {
		return fmt.Errorf("no progress found for channel %s", channelID)
	}

	progress.Messages = append(progress.Messages, messages...)
	progress.ProcessedMessages = len(progress.Messages)

	return m.SaveProgress(progress)
}

// GetResumeInfo returns information needed to resume processing
func (m *Manager) GetResumeInfo(channelID string) (cursor string, messages []*sheets.MessageRecord, err error) {
	progress, err := m.LoadProgress(channelID)
	if err != nil {
		return "", nil, err
	}
	if progress == nil {
		return "", nil, nil
	}

	return progress.LastCursor, progress.Messages, nil
}

// SetCursor updates the last cursor position
func (m *Manager) SetCursor(channelID, cursor string) error {
	progress, err := m.LoadProgress(channelID)
	if err != nil {
		return err
	}
	if progress == nil {
		return fmt.Errorf("no progress found for channel %s", channelID)
	}

	progress.LastCursor = cursor
	return m.SaveProgress(progress)
}

// ClearMessagesForMemory clears the messages array to save memory while keeping other progress data
func (m *Manager) ClearMessagesForMemory(channelID string) error {
	progress, err := m.LoadProgress(channelID)
	if err != nil {
		return err
	}
	if progress == nil {
		return fmt.Errorf("no progress found for channel %s", channelID)
	}

	// Keep message count but clear the actual messages to save memory
	progress.ProcessedMessages = len(progress.Messages)
	progress.Messages = []*sheets.MessageRecord{} // Clear to save memory

	return m.SaveProgress(progress)
}
