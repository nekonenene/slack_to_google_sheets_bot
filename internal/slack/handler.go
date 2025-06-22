package slack

import (
	"fmt"
	"log"
	"regexp"
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
	// JST timezone for timestamp conversion
	jstLocation *time.Location
)

func init() {
	var err error
	jstLocation, err = time.LoadLocation("Asia/Tokyo")
	if err != nil {
		log.Printf("Warning: Could not load JST timezone, using UTC: %v", err)
		jstLocation = time.UTC
	}
}

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

		// Check for recent member joins in same channel (within 30 seconds)
		recentMemberJoinMutex.Lock()
		channelKey := fmt.Sprintf("channel_%s", event.Event.Channel)
		if lastJoinTime, exists := recentMemberJoins[channelKey]; exists {
			if time.Since(lastJoinTime) < 30*time.Second {
				recentMemberJoinMutex.Unlock()
				processingMutex.Lock()
				delete(processingEvents, eventKey)
				processingMutex.Unlock()
				log.Printf("Recent member join detected in channel %s (within 30s), skipping", event.Event.Channel)
				return nil
			}
		}
		recentMemberJoins[channelKey] = time.Now()
		recentMemberJoinMutex.Unlock()

		// Block app_mention events for this channel for the next 5 seconds
		recentMutex.Lock()
		recentMentions[event.Event.Channel] = time.Now().Add(5 * time.Second)
		recentMutex.Unlock()
		log.Printf("Blocked app_mention events for channel %s for 5 seconds due to member join", event.Event.Channel)

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

		// Clean up after processing
		defer func() {
			processingMutex.Lock()
			delete(processingEvents, eventKey)
			processingMutex.Unlock()
		}()

		return handleAppMention(cfg, event)
	}

	// Handle message changed events (edits)
	if event.Event.Type == "message" && event.Event.Subtype == "message_changed" {
		log.Printf("Processing message_changed event for channel: %s", event.Event.Channel)
		return handleMessageChanged(cfg, event)
	}

	// Only handle regular message events
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

	// Parse timestamp and convert to JST
	timestamp := convertSlackTimestampToJST(event.Event.Timestamp)

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

// extractEmailFromShowMe extracts email address from "show me" command
func extractEmailFromShowMe(text string) string {
	matches := regexp.MustCompile(`show\s+me\s+(.+)`).FindStringSubmatch(text)

	if len(matches) > 1 {
		emailContainsString := matches[1]
		emailPattern := regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
		matches := emailPattern.FindStringSubmatch(emailContainsString)

		if len(matches) > 0 {
			return matches[0]
		}
	}

	return ""
}

// isRateLimitError checks if the error is a Slack API rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "ratelimited")
}

// scheduleHistoryRetry schedules a retry of history retrieval after specified duration
// Preserves the original start time to ensure new messages are properly captured
func scheduleHistoryRetry(cfg *config.Config, channelID, channelName string, isInitialRecording bool, originalStartTime time.Time, retryDelay time.Duration) {
	log.Printf("Scheduling history retry for channel %s in %v due to rate limit (preserving start time: %v)", channelID, retryDelay, originalStartTime)

	go func() {
		time.Sleep(retryDelay)
		log.Printf("Retrying history retrieval for channel %s after %v delay", channelID, retryDelay)

		// Create a mock event for retry
		mockEvent := &Event{
			Event: EventData{
				Channel: channelID,
			},
		}

		if isInitialRecording {
			if err := retryMemberJoinedHistoryWithStartTime(cfg, mockEvent, channelName, originalStartTime); err != nil {
				log.Printf("Failed to retry member joined history for channel %s: %v", channelID, err)
			}
		} else {
			if err := retryAppMentionHistoryWithStartTime(cfg, mockEvent, channelName, originalStartTime); err != nil {
				log.Printf("Failed to retry app mention history for channel %s: %v", channelID, err)
			}
		}
	}()
}

// retryMemberJoinedHistoryWithStartTime retries the member joined history retrieval with preserved start time
func retryMemberJoinedHistoryWithStartTime(cfg *config.Config, event *Event, channelName string, originalStartTime time.Time) error {
	slackClient := NewClient(cfg.SlackBotToken)

	// Get channel information
	channelInfo := &ChannelInfo{ID: event.Event.Channel, Name: channelName}
	if channelName == "" {
		if info, err := slackClient.GetChannelInfo(event.Event.Channel); err == nil {
			channelInfo = info
		}
	}

	// Call the history retrieval with preserved start time
	return performHistoryRetrievalWithStartTime(cfg, slackClient, event, channelInfo, true, originalStartTime)
}

// retryAppMentionHistoryWithStartTime retries the app mention history retrieval with preserved start time
func retryAppMentionHistoryWithStartTime(cfg *config.Config, event *Event, channelName string, originalStartTime time.Time) error {
	slackClient := NewClient(cfg.SlackBotToken)

	// Get channel information
	channelInfo := &ChannelInfo{ID: event.Event.Channel, Name: channelName}
	if channelName == "" {
		if info, err := slackClient.GetChannelInfo(event.Event.Channel); err == nil {
			channelInfo = info
		}
	}

	// Call the history retrieval with preserved start time
	return performHistoryRetrievalWithStartTime(cfg, slackClient, event, channelInfo, false, originalStartTime)
}

// performHistoryRetrieval performs the actual history retrieval with progress tracking
func performHistoryRetrieval(cfg *config.Config, slackClient *Client, event *Event, channelInfo *ChannelInfo, isInitialRecording bool) error {
	return performHistoryRetrievalWithStartTime(cfg, slackClient, event, channelInfo, isInitialRecording, time.Now())
}

// performHistoryRetrievalWithStartTime performs the actual history retrieval with a specified start time
func performHistoryRetrievalWithStartTime(cfg *config.Config, slackClient *Client, event *Event, channelInfo *ChannelInfo, isInitialRecording bool, originalStartTime time.Time) error {
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

	// Set history retrieval in progress flag with original start time
	historyProgressMutex.Lock()
	historyInProgress[event.Event.Channel] = true
	historyStartTime[event.Event.Channel] = originalStartTime
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
			// Schedule retry after 3 minutes with preserved original start time
			scheduleHistoryRetry(cfg, event.Event.Channel, channelInfo.Name, isInitialRecording, originalStartTime, 3*time.Minute)
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

	log.Printf("Checking for new messages after original start time: %v (channel: %s)", startTime, event.Event.Channel)
	log.Printf("Wait for 5 minutes before checking for new messages to avoid rate limits")
	time.Sleep(5 * time.Minute) // Wait to avoid rate limits
	newMessages, err := slackClient.getMessagesAfterTime(event.Event.Channel, channelInfo.Name, startTime)

	if err != nil {
		log.Printf("Error: Could not get new messages after history retrieval: %v", err)

		// For non-rate-limit errors, send error message but continue
		errorMessage := "⚠️ 処理中の新着メッセージ取得に失敗しました。一部のメッセージが記録されていない可能性があります。"
		if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
			log.Printf("Error sending new messages error notification: %v", err)
		}
	} else if len(newMessages) > 0 {
		log.Printf("Found %d new messages during history retrieval, adding them", len(newMessages))
		if err := sheetsClient.WriteBatchMessages(cfg.SpreadsheetID, newMessages); err != nil {
			log.Printf("Error: Could not write new messages after history retrieval: %v", err)

			// Critical failure - unable to write new messages
			errorMessage := "❌ 処理中の新着メッセージの記録に失敗しました。再度実行してください。"
			if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
				log.Printf("Error sending write failure notification: %v", err)
			}
			return err
		} else {
			log.Printf("Successfully added %d new messages after history retrieval", len(newMessages))
		}
	} else {
		log.Printf("No new messages found during history retrieval period")
	}

	// Send completion message
	sheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s", cfg.SpreadsheetID)
	var completionMessage string

	totalRecorded := len(records)
	if len(newMessages) > 0 {
		totalRecorded += len(newMessages)
	}

	if isInitialRecording {
		if len(newMessages) > 0 {
			completionMessage = fmt.Sprintf("✅ 初回のメッセージ履歴記録が完了しました！\n"+
				"履歴メッセージ数: %d件\n"+
				"処理中の新着メッセージ数: %d件\n"+
				"合計記録数: %d件\n"+
				"記録先: %s", len(records), len(newMessages), totalRecorded, sheetURL)
		} else {
			completionMessage = fmt.Sprintf("✅ 初回のメッセージ履歴記録が完了しました！\n"+
				"記録されたメッセージ数: %d件\n"+
				"記録先: %s", totalRecorded, sheetURL)
		}
	} else {
		if len(newMessages) > 0 {
			completionMessage = fmt.Sprintf("✅ 過去のメッセージ履歴の記録が完了しました！\n"+
				"履歴メッセージ数: %d件\n"+
				"処理中の新着メッセージ数: %d件\n"+
				"合計記録数: %d件\n"+
				"記録先: %s", len(records), len(newMessages), totalRecorded, sheetURL)
		} else {
			completionMessage = fmt.Sprintf("✅ 過去のメッセージ履歴の記録が完了しました！\n"+
				"記録されたメッセージ数: %d件\n"+
				"記録先: %s", totalRecorded, sheetURL)
		}
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

	// Check if this is a "show me" command
	isShowMeCmd := strings.Contains(strings.ToLower(event.Event.Text), "show me")
	var extractedEmail string
	if isShowMeCmd {
		extractedEmail = extractEmailFromShowMe(event.Event.Text)
	}

	// First, record the mention message itself
	if err := recordSingleMessage(cfg, slackClient, event, channelInfo); err != nil {
		log.Printf("Error recording mention message: %v", err)
	}

	// Handle "show me" command
	if isShowMeCmd {
		return handleShowMeCommand(cfg, slackClient, event, channelInfo, extractedEmail)
	}

	// If not a reset request, just respond with instruction and return
	if !isResetRequest {
		ackMessage := "🔗 ユーザーにスプレッドシート閲覧権限を付与するには「show me <メールアドレス>」とメンションしてください\n" +
			"🤖 このチャンネルの記録を取得し直すには「Reset!」とメンションしてください\n"

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

// handleMessageChanged handles message edit events
func handleMessageChanged(cfg *config.Config, event *Event) error {
	// Check if Google Sheets is configured
	if cfg.GoogleSheetsCredentials == "" || cfg.SpreadsheetID == "" {
		log.Printf("Google Sheets not configured, ignoring message edit")
		return nil
	}

	// Ensure we have the changed message data
	if event.Event.Message == nil {
		log.Printf("No message data in message_changed event")
		return nil
	}

	changedMessage := event.Event.Message

	// Skip if this is not actually an edit (some subtypes we don't care about)
	if changedMessage.Edited == nil {
		log.Printf("Message change event without edit info, skipping")
		return nil
	}

	// Create Slack client
	slackClient := NewClient(cfg.SlackBotToken)

	// Get channel information
	channelInfo, err := slackClient.GetChannelInfo(event.Event.Channel)
	if err != nil {
		log.Printf("Error getting channel info for message edit: %v", err)
		channelInfo = &ChannelInfo{ID: event.Event.Channel, Name: "Unknown"}
	}

	// Get user information for the edited message
	var userInfo *UserInfo
	if changedMessage.User != "" {
		userInfo, err = slackClient.GetUserInfo(changedMessage.User)
		if err != nil {
			log.Printf("Error getting user info for edited message: %v", err)
			userInfo = &UserInfo{ID: changedMessage.User, Name: "Unknown", RealName: "Unknown"}
		}
	} else {
		userInfo = &UserInfo{ID: "", Name: "Bot", RealName: "Bot"}
	}

	// Parse timestamp and convert to JST
	timestamp := convertSlackTimestampToJST(changedMessage.Timestamp)

	// Format message text
	formattedText := slackClient.FormatMessageText(changedMessage.Text)

	// Create message record for the edited message
	record := sheets.MessageRecord{
		Timestamp:    timestamp,
		Channel:      event.Event.Channel,
		ChannelName:  channelInfo.Name,
		User:         changedMessage.User,
		UserHandle:   userInfo.Name,
		UserRealName: userInfo.RealName,
		Text:         formattedText,
		ThreadTS:     changedMessage.ThreadTS,
		MessageTS:    changedMessage.Timestamp,
	}

	// Create Google Sheets client and update the message
	sheetsClient, err := sheets.NewClient(cfg.GoogleSheetsCredentials)
	if err != nil {
		log.Printf("Error creating Google Sheets client for message edit: %v", err)
		return err
	}

	// Update the message in the sheet
	if err := sheetsClient.UpdateMessage(cfg.SpreadsheetID, &record); err != nil {
		log.Printf("Error updating edited message in Google Sheets: %v", err)
		return err
	}

	log.Printf("✅ Message edit recorded in #%s by %s: %s",
		record.ChannelName, record.UserHandle,
		truncateText(record.Text, 50))

	return nil
}

// handleShowMeCommand handles the "show me" command to grant spreadsheet access
func handleShowMeCommand(cfg *config.Config, slackClient *Client, event *Event, channelInfo *ChannelInfo, email string) error {
	// Validate email
	if email == "" {
		errorMessage := "❌ 有効なメールアドレスが見つかりませんでした。\n" +
			"使用例: `@bot show me test@example.com`"
		if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
			log.Printf("Error sending invalid email message: %v", err)
		}
		return nil
	}

	// Check if Google Sheets is configured
	if cfg.GoogleSheetsCredentials == "" || cfg.SpreadsheetID == "" {
		configMessage := "⚠️ Google Sheetsの設定が完了していません。管理者にお問い合わせください。"
		if err := slackClient.SendMessage(event.Event.Channel, configMessage); err != nil {
			log.Printf("Error sending config message: %v", err)
		}
		return nil
	}

	// Create Google Sheets client
	sheetsClient, err := sheets.NewClient(cfg.GoogleSheetsCredentials)
	if err != nil {
		log.Printf("Error creating Google Sheets client for sharing: %v", err)
		errorMessage := "❌ Google Sheetsへの接続に失敗しました。"
		if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
			log.Printf("Error sending connection error message: %v", err)
		}
		return err
	}

	// Share the spreadsheet
	if err := sheetsClient.ShareSpreadsheet(cfg.SpreadsheetID, email); err != nil {
		log.Printf("Error sharing spreadsheet with %s: %v", email, err)
		errorMessage := fmt.Sprintf("❌ %s への権限付与に失敗しました（エラー: %v）", email, err)
		if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
			log.Printf("Error sending share error message: %v", err)
		}
		return err
	}

	// Send success message
	sheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s", cfg.SpreadsheetID)
	successMessage := fmt.Sprintf("✅ %s に<%s|スプレッドシート>の閲覧権限を付与しました。", email, sheetURL)
	if err := slackClient.SendMessage(event.Event.Channel, successMessage); err != nil {
		log.Printf("Error sending success message: %v", err)
	}

	log.Printf("Successfully granted spreadsheet access to %s for channel %s", email, channelInfo.Name)
	return nil
}

// convertSlackTimestampToJST converts a Slack timestamp string to JST time
func convertSlackTimestampToJST(timestampStr string) time.Time {
	ts, err := strconv.ParseFloat(timestampStr, 64)
	if err != nil {
		log.Printf("Error parsing timestamp %s, using current time: %v", timestampStr, err)
		return time.Now().In(jstLocation)
	}

	// Convert Unix timestamp to UTC time, then to JST
	utcTime := time.Unix(int64(ts), 0)
	return utcTime.In(jstLocation)
}
