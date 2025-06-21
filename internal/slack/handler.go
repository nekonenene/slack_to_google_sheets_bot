package slack

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"slack-to-google-sheets-bot/internal/config"
	"slack-to-google-sheets-bot/internal/progress"
	"slack-to-google-sheets-bot/internal/sheets"
)

const (
	MaxFailureCount = 3
)

var (
	processingEvents      = make(map[string]bool)
	processingMutex       = sync.Mutex{}
	recentMentions        = make(map[string]time.Time)
	recentMutex           = sync.Mutex{}
	recentMemberJoins     = make(map[string]time.Time)
	recentMemberJoinMutex = sync.Mutex{}
	historyInProgress     = make(map[string]bool)
	historyStartTime      = make(map[string]time.Time)
	historyProgressMutex  = sync.Mutex{}
)

func HandleEvent(cfg *config.Config, event *Event) error {
	// Log all incoming events for debugging
	log.Printf("Received event: type=%s, user=%s, text=%s, timestamp=%s",
		event.Event.Type, event.Event.User, event.Event.Text, event.Event.Timestamp)

	// Handle member joined channel event
	if event.Event.Type == "member_joined_channel" {
		log.Printf("Processing member_joined_channel event for channel: %s, user: %s", event.Event.Channel, event.Event.User)

		// Create unique key for this member join event
		eventKey := fmt.Sprintf("member_joined_%s_%s", event.Event.Channel, event.Event.User)

		// Check if already processing this event
		processingMutex.Lock()
		if processingEvents[eventKey] {
			processingMutex.Unlock()
			log.Printf("Already processing member_joined for channel %s, user %s, skipping", event.Event.Channel, event.Event.User)
			return nil
		}
		processingEvents[eventKey] = true
		processingMutex.Unlock()

		// Check for recent member joins in same channel (within 90 seconds)
		recentMemberJoinMutex.Lock()
		channelKey := fmt.Sprintf("channel_%s", event.Event.Channel)
		if lastJoinTime, exists := recentMemberJoins[channelKey]; exists {
			if time.Since(lastJoinTime) < 90*time.Second {
				recentMemberJoinMutex.Unlock()
				processingMutex.Lock()
				delete(processingEvents, eventKey)
				processingMutex.Unlock()
				log.Printf("Recent member join detected in channel %s (within 90s), skipping", event.Event.Channel)
				return nil
			}
		}
		recentMemberJoins[channelKey] = time.Now()
		recentMemberJoinMutex.Unlock()

		// Block app_mention events for this channel for the next 5 minutes
		recentMutex.Lock()
		recentMentions[event.Event.Channel] = time.Now().Add(5 * time.Minute)
		recentMutex.Unlock()
		log.Printf("Blocked app_mention events for channel %s for 5 minutes due to member join", event.Event.Channel)

		// Clean up after processing
		defer func() {
			processingMutex.Lock()
			delete(processingEvents, eventKey)
			processingMutex.Unlock()
		}()

		return handleMemberJoined(cfg, event)
	}

	// Handle app mention event
	if event.Event.Type == "app_mention" {
		log.Printf("Processing app_mention event for timestamp: %s", event.Event.Timestamp)

		// Create unique key for this app mention event
		eventKey := fmt.Sprintf("app_mention_%s_%s", event.Event.Channel, event.Event.Timestamp)

		// Check if already processing this event
		processingMutex.Lock()
		if processingEvents[eventKey] {
			processingMutex.Unlock()
			log.Printf("Already processing app_mention for timestamp %s, skipping", event.Event.Timestamp)
			return nil
		}
		processingEvents[eventKey] = true
		processingMutex.Unlock()

		// Check for recent mentions in same channel (within 90 seconds) or if blocked by member join
		recentMutex.Lock()
		if lastMentionTime, exists := recentMentions[event.Event.Channel]; exists {
			if time.Since(lastMentionTime) < 90*time.Second {
				recentMutex.Unlock()
				processingMutex.Lock()
				delete(processingEvents, eventKey)
				processingMutex.Unlock()
				log.Printf("Recent mention detected in channel %s (within 90s) or blocked by member join, skipping", event.Event.Channel)
				return nil
			}
		}
		recentMentions[event.Event.Channel] = time.Now()
		recentMutex.Unlock()

		// Clean up after processing
		defer func() {
			processingMutex.Lock()
			delete(processingEvents, eventKey)
			processingMutex.Unlock()
		}()

		return handleAppMention(cfg, event)
	}

	// Only handle message events
	if event.Event.Type != "message" {
		log.Printf("Ignoring event type: %s", event.Event.Type)
		return nil
	}

	// Skip messages without text (but allow bot messages)
	if event.Event.Text == "" {
		return nil
	}

	// Skip message recording if history retrieval is in progress for this channel
	historyProgressMutex.Lock()
	if historyInProgress[event.Event.Channel] {
		historyProgressMutex.Unlock()
		log.Printf("Skipping message recording for channel %s - history retrieval in progress", event.Event.Channel)
		return nil
	}
	historyProgressMutex.Unlock()

	// Skip messages that are app mentions to avoid duplicate processing
	// (app_mention events are already handled above)
	// Only skip if this message mentions our bot specifically
	if strings.Contains(event.Event.Text, "<@") {
		// Check if this is an app mention to our bot by looking for bot mention patterns
		// This is a simplified check - in a real implementation you'd want to check the actual bot user ID
		log.Printf("Skipping message event that contains mentions to avoid duplicate processing")
		return nil
	}

	// Create Slack client
	slackClient := NewClient(cfg.SlackBotToken)

	// Get channel information
	channelInfo, err := slackClient.GetChannelInfo(event.Event.Channel)
	if err != nil {
		log.Printf("Error getting channel info: %v", err)
		channelInfo = &ChannelInfo{ID: event.Event.Channel, Name: "Unknown"}
	}

	return recordSingleMessage(cfg, slackClient, event, channelInfo)
}

func recordSingleMessage(cfg *config.Config, slackClient *Client, event *Event, channelInfo *ChannelInfo) error {
	// Get user information (handle both human users and bots)
	var userInfo *UserInfo
	if event.Event.User != "" {
		// Human user message
		var err error
		userInfo, err = slackClient.GetUserInfo(event.Event.User)
		if err != nil {
			log.Printf("Error getting user info for %s: %v", event.Event.User, err)
			userInfo = &UserInfo{ID: event.Event.User, Name: "Unknown", RealName: "Unknown"}
		}
	} else {
		// Bot message or system message - create a placeholder user info
		userInfo = &UserInfo{ID: "", Name: "Bot", RealName: "Bot"}
	}

	// Parse timestamp
	ts, err := strconv.ParseFloat(event.Event.Timestamp, 64)
	if err != nil {
		ts = float64(time.Now().Unix())
	}
	timestamp := time.Unix(int64(ts), 0)

	// Format message text (convert mentions and channels)
	formattedText := slackClient.FormatMessageText(event.Event.Text)

	// Create message record
	record := sheets.MessageRecord{
		Timestamp:    timestamp,
		Channel:      event.Event.Channel,
		ChannelName:  channelInfo.Name,
		User:         event.Event.User,
		UserHandle:   userInfo.Name,
		UserRealName: userInfo.RealName,
		Text:         formattedText,
		ThreadTS:     event.Event.ThreadTS,
		MessageTS:    event.Event.Timestamp,
	}

	// Write to Google Sheets
	if cfg.GoogleSheetsCredentials != "" && cfg.SpreadsheetID != "" {
		log.Printf("Creating Google Sheets client with credentials length: %d", len(cfg.GoogleSheetsCredentials))
		sheetsClient, err := sheets.NewClient(cfg.GoogleSheetsCredentials)
		if err != nil {
			log.Printf("Error creating Google Sheets client: %v", err)
			preview := cfg.GoogleSheetsCredentials
			if len(preview) > 100 {
				preview = preview[:100]
			}
			log.Printf("Credentials preview: %s...", preview)
			log.Printf("Credentials starts with: %c", cfg.GoogleSheetsCredentials[0])
			log.Printf("Is it a file path? Contains '.json': %t", strings.Contains(cfg.GoogleSheetsCredentials, ".json"))

			// Send error notification to Slack
			errorMessage := fmt.Sprintf("❌ Google Sheetsへの接続に失敗しました。\n"+
				"エラー: %v\n"+
				"管理者にお問い合わせください。", err)
			if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
				log.Printf("Error sending failure notification: %v", err)
			}

			return err
		}

		if err := sheetsClient.WriteMessage(cfg.SpreadsheetID, &record); err != nil {
			log.Printf("Error writing message to Google Sheets (channel: %s, user: %s): %v",
				record.ChannelName, record.UserHandle, err)

			// For individual message failures, only log the error (don't spam the channel)
			// Only send notification for critical failures
			return err
		}

		log.Printf("✅ Message auto-recorded in #%s by %s: %s",
			record.ChannelName, record.UserHandle,
			truncateText(record.Text, 50))
	} else {
		log.Printf("Google Sheets not configured, message logged: %s in #%s by %s", record.Text, record.ChannelName, record.UserHandle)
	}

	return nil
}

// truncateText truncates text to the specified length with ellipsis
func truncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}
	return text[:maxLength] + "..."
}

// isRateLimitError checks if the error is a Slack API rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "ratelimited")
}

// scheduleHistoryRetry schedules a retry of history retrieval after 5 minutes
func scheduleHistoryRetry(cfg *config.Config, channelID, channelName string, isInitialRecording bool) {
	log.Printf("Scheduling history retry for channel %s in 5 minutes due to rate limit", channelID)

	go func() {
		time.Sleep(5 * time.Minute)
		log.Printf("Retrying history retrieval for channel %s after rate limit delay", channelID)

		// Create a mock event for retry
		mockEvent := &Event{
			Event: EventData{
				Channel: channelID,
			},
		}

		if isInitialRecording {
			if err := retryMemberJoinedHistory(cfg, mockEvent, channelName); err != nil {
				log.Printf("Failed to retry member joined history for channel %s: %v", channelID, err)
			}
		} else {
			if err := retryAppMentionHistory(cfg, mockEvent, channelName); err != nil {
				log.Printf("Failed to retry app mention history for channel %s: %v", channelID, err)
			}
		}
	}()
}

// retryMemberJoinedHistory retries the member joined history retrieval
func retryMemberJoinedHistory(cfg *config.Config, event *Event, channelName string) error {
	slackClient := NewClient(cfg.SlackBotToken)

	// Get channel information
	channelInfo := &ChannelInfo{ID: event.Event.Channel, Name: channelName}
	if channelName == "" {
		if info, err := slackClient.GetChannelInfo(event.Event.Channel); err == nil {
			channelInfo = info
		}
	}

	// Call the original member joined logic (without the initial setup)
	return performHistoryRetrieval(cfg, slackClient, event, channelInfo, true)
}

// retryAppMentionHistory retries the app mention history retrieval
func retryAppMentionHistory(cfg *config.Config, event *Event, channelName string) error {
	slackClient := NewClient(cfg.SlackBotToken)

	// Get channel information
	channelInfo := &ChannelInfo{ID: event.Event.Channel, Name: channelName}
	if channelName == "" {
		if info, err := slackClient.GetChannelInfo(event.Event.Channel); err == nil {
			channelInfo = info
		}
	}

	// Call the original app mention logic (without reset handling)
	return performHistoryRetrieval(cfg, slackClient, event, channelInfo, false)
}

// performHistoryRetrieval performs the actual history retrieval with progress tracking
func performHistoryRetrieval(cfg *config.Config, slackClient *Client, event *Event, channelInfo *ChannelInfo, isInitialRecording bool) error {
	// Check if Google Sheets is configured
	if cfg.GoogleSheetsCredentials == "" || cfg.SpreadsheetID == "" {
		configMessage := "⚠️ Google Sheetsの設定が完了していません。管理者にお問い合わせください。"
		slackClient.SendMessage(event.Event.Channel, configMessage)
		return nil
	}

	// Create Google Sheets client
	sheetsClient, err := sheets.NewClient(cfg.GoogleSheetsCredentials)
	if err != nil {
		log.Printf("Error creating Google Sheets client: %v", err)
		errorMessage := "❌ Google Sheetsへの接続に失敗しました。"
		slackClient.SendMessage(event.Event.Channel, errorMessage)
		return err
	}

	// Ensure channel-specific sheet exists
	if err := sheetsClient.EnsureChannelSheetExists(cfg.SpreadsheetID, event.Event.Channel, channelInfo.Name); err != nil {
		log.Printf("Error ensuring channel sheet exists: %v", err)
		errorMessage := "❌ スプレッドシートの初期化に失敗しました。"
		slackClient.SendMessage(event.Event.Channel, errorMessage)
		return err
	}

	// Set history retrieval in progress flag with start time
	historyProgressMutex.Lock()
	historyInProgress[event.Event.Channel] = true
	historyStartTime[event.Event.Channel] = time.Now()
	historyProgressMutex.Unlock()

	// Ensure flag is cleared when function exits
	defer func() {
		historyProgressMutex.Lock()
		delete(historyInProgress, event.Event.Channel)
		delete(historyStartTime, event.Event.Channel)
		historyProgressMutex.Unlock()
	}()

	// Get channel history with progress tracking
	progressMgr := progress.NewManager()

	// Check if there's existing progress
	if progressMgr.HasProgress(event.Event.Channel) {
		log.Printf("Found existing progress for channel %s, resuming...", event.Event.Channel)
	}

	records, err := slackClient.GetChannelHistoryWithProgress(event.Event.Channel, channelInfo.Name, 0, progressMgr)
	if err != nil {
		log.Printf("Error getting channel history: %v", err)

		// Check if this is a rate limit error
		if isRateLimitError(err) {
			rateLimitMessage := "⏳ Slack APIのレート制限により一時的に処理が中断されました。\n5分後に自動的に再開します..."
			if err := slackClient.SendMessage(event.Event.Channel, rateLimitMessage); err != nil {
				log.Printf("Error sending rate limit message: %v", err)
			}

			// Schedule retry after 5 minutes
			scheduleHistoryRetry(cfg, event.Event.Channel, channelInfo.Name, isInitialRecording)
			return nil // Don't return error, let the retry handle it
		}

		errorMessage := "❌ チャンネル履歴の取得に失敗しました。"
		slackClient.SendMessage(event.Event.Channel, errorMessage)
		return err
	}

	if len(records) == 0 {
		noMessagesMsg := "ℹ️ 記録するメッセージが見つかりませんでした。"
		slackClient.SendMessage(event.Event.Channel, noMessagesMsg)
		return nil
	}

	// Write messages to spreadsheet
	// Use WriteBatchMessagesFromRow2 for initial recording and reset operations
	// to ensure data starts from row 2 regardless of existing content
	if err := sheetsClient.WriteBatchMessagesFromRow2(cfg.SpreadsheetID, records); err != nil {
		log.Printf("Error writing batch messages to sheets after retries: %v", err)
		errorMessage := fmt.Sprintf("❌ スプレッドシートへの記録に失敗しました（4回試行後）\n"+
			"エラー: %v\n"+
			"ネットワークまたはAPI制限の問題の可能性があります。\n"+
			"しばらく時間をおいてから再度お試しください。", err)
		if notifyErr := slackClient.SendMessage(event.Event.Channel, errorMessage); notifyErr != nil {
			log.Printf("Error sending failure notification after retries: %v", notifyErr)
		}
		return err
	}

	// Mark progress as completed and clean up
	if err := progressMgr.UpdatePhase(event.Event.Channel, "completed"); err != nil {
		log.Printf("Warning: Could not update progress phase: %v", err)
	}

	// Delete progress file after successful completion
	if err := progressMgr.DeleteProgress(event.Event.Channel); err != nil {
		log.Printf("Warning: Could not delete progress file: %v", err)
	}

	// Get any new messages that arrived during history retrieval
	historyProgressMutex.Lock()
	startTime := historyStartTime[event.Event.Channel]
	historyProgressMutex.Unlock()

	newMessages, err := slackClient.getMessagesAfterTime(event.Event.Channel, channelInfo.Name, startTime)
	if err != nil {
		log.Printf("Warning: Could not get new messages after history retrieval: %v", err)
	} else if len(newMessages) > 0 {
		log.Printf("Found %d new messages during history retrieval, adding them", len(newMessages))
		if err := sheetsClient.WriteBatchMessages(cfg.SpreadsheetID, newMessages); err != nil {
			log.Printf("Warning: Could not write new messages after history retrieval: %v", err)
		} else {
			log.Printf("Successfully added %d new messages after history retrieval", len(newMessages))
		}
	}

	// Send completion message
	sheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s", cfg.SpreadsheetID)
	var completionMessage string

	if isInitialRecording {
		completionMessage = fmt.Sprintf("✅ 初回のメッセージ履歴記録が完了しました！\n"+
			"記録されたメッセージ数: %d件\n"+
			"記録先: %s", len(records), sheetURL)
	} else {
		completionMessage = fmt.Sprintf("✅ 過去のメッセージ履歴の記録が完了しました！\n"+
			"記録されたメッセージ数: %d件\n"+
			"記録先: %s", len(records), sheetURL)
	}

	if err := slackClient.SendMessage(event.Event.Channel, completionMessage); err != nil {
		log.Printf("Error sending completion message: %v", err)
	}

	return nil
}

func handleMemberJoined(cfg *config.Config, event *Event) error {
	// Check if the bot itself was added to the channel
	slackClient := NewClient(cfg.SlackBotToken)

	// Get channel information
	channelInfo, err := slackClient.GetChannelInfo(event.Event.Channel)
	if err != nil {
		log.Printf("Error getting channel info for member join: %v", err)
		channelInfo = &ChannelInfo{ID: event.Event.Channel, Name: "Unknown"}
	}

	// Send initial message
	message := fmt.Sprintf("🚀 初回の記録を開始します...\n"+
		"このチャンネル (#%s) のメッセージをGoogle Sheetsに記録します。", channelInfo.Name)

	if err := slackClient.SendMessage(event.Event.Channel, message); err != nil {
		log.Printf("Error sending initial message: %v", err)
	}

	// Use the common history retrieval function
	return performHistoryRetrieval(cfg, slackClient, event, channelInfo, true)
}

func handleAppMention(cfg *config.Config, event *Event) error {
	slackClient := NewClient(cfg.SlackBotToken)

	// Get channel information
	channelInfo, err := slackClient.GetChannelInfo(event.Event.Channel)
	if err != nil {
		log.Printf("Error getting channel info for app mention: %v", err)
		channelInfo = &ChannelInfo{ID: event.Event.Channel, Name: "Unknown"}
	}

	// Check if this is a reset request
	isResetRequest := strings.Contains(strings.ToLower(event.Event.Text), "reset")

	// First, record the mention message itself
	if err := recordSingleMessage(cfg, slackClient, event, channelInfo); err != nil {
		log.Printf("Error recording mention message: %v", err)
	}

	// If not a reset request, just respond with instruction and return
	if !isResetRequest {
		ackMessage := "🤖 このチャンネルの記録を取得し直すには「Reset!」とメンションしてください"
		if err := slackClient.SendMessage(event.Event.Channel, ackMessage); err != nil {
			log.Printf("Error sending acknowledgment message: %v", err)
		}
		return nil
	}

	// Send acknowledgment message for reset request
	ackMessage := fmt.Sprintf("🔄 シートをリセットして過去のメッセージ履歴を再取得しています... (#%s)", channelInfo.Name)
	if err := slackClient.SendMessage(event.Event.Channel, ackMessage); err != nil {
		log.Printf("Error sending acknowledgment message: %v", err)
	}

	// Check if Google Sheets is configured
	if cfg.GoogleSheetsCredentials == "" || cfg.SpreadsheetID == "" {
		configMessage := "⚠️ Google Sheetsの設定が完了していません。管理者にお問い合わせください。"
		slackClient.SendMessage(event.Event.Channel, configMessage)
		return nil
	}

	// Create Google Sheets client
	sheetsClient, err := sheets.NewClient(cfg.GoogleSheetsCredentials)
	if err != nil {
		log.Printf("Error creating Google Sheets client: %v", err)
		errorMessage := "❌ Google Sheetsへの接続に失敗しました。"
		slackClient.SendMessage(event.Event.Channel, errorMessage)
		return err
	}

	// Handle reset request - clear existing data
	if isResetRequest {
		sheetName := fmt.Sprintf("%s-%s", channelInfo.Name, event.Event.Channel)

		// Ensure the sheet exists first
		if err := sheetsClient.EnsureChannelSheetExists(cfg.SpreadsheetID, event.Event.Channel, channelInfo.Name); err != nil {
			log.Printf("Error ensuring sheet exists for reset: %v", err)
			errorMessage := "❌ シートの確認に失敗しました。"
			slackClient.SendMessage(event.Event.Channel, errorMessage)
			return err
		}

		// Clear existing data
		if err := sheetsClient.ClearSheetData(cfg.SpreadsheetID, sheetName); err != nil {
			log.Printf("Error clearing sheet data: %v", err)
			errorMessage := "❌ シートのクリアに失敗しました。"
			slackClient.SendMessage(event.Event.Channel, errorMessage)
			return err
		}

		log.Printf("Sheet reset completed for channel %s", channelInfo.Name)

		// Clean up any existing progress for reset
		progressMgr := progress.NewManager()
		if err := progressMgr.DeleteProgress(event.Event.Channel); err != nil {
			log.Printf("Warning: Could not clean up existing progress: %v", err)
		}
	}

	// Use the common history retrieval function
	return performHistoryRetrieval(cfg, slackClient, event, channelInfo, false)
}
