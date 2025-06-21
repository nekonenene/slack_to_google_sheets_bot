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

		// Check for recent member joins in same channel (within 120 seconds)
		recentMemberJoinMutex.Lock()
		channelKey := fmt.Sprintf("channel_%s", event.Event.Channel)
		if lastJoinTime, exists := recentMemberJoins[channelKey]; exists {
			if time.Since(lastJoinTime) < 120*time.Second {
				recentMemberJoinMutex.Unlock()
				processingMutex.Lock()
				delete(processingEvents, eventKey)
				processingMutex.Unlock()
				log.Printf("Recent member join detected in channel %s (within 120s), skipping", event.Event.Channel)
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

		// Check for recent mentions in same channel (within 120 seconds) or if blocked by member join
		recentMutex.Lock()
		if lastMentionTime, exists := recentMentions[event.Event.Channel]; exists {
			if time.Since(lastMentionTime) < 120*time.Second {
				recentMutex.Unlock()
				processingMutex.Lock()
				delete(processingEvents, eventKey)
				processingMutex.Unlock()
				log.Printf("Recent mention detected in channel %s (within 120s) or blocked by member join, skipping", event.Event.Channel)
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

	// Skip bot messages and messages without text
	if event.Event.User == "" || event.Event.Text == "" {
		return nil
	}

	// Skip messages that are app mentions to avoid duplicate processing
	// (app_mention events are already handled above)
	// We'll use a simpler approach: if this message event has the same timestamp as an app_mention,
	// we skip it. For now, we'll skip any message with bot mentions.
	if strings.Contains(event.Event.Text, "<@") {
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
	// Get user information
	userInfo, err := slackClient.GetUserInfo(event.Event.User)
	if err != nil {
		log.Printf("Error getting user info: %v", err)
		userInfo = &UserInfo{ID: event.Event.User, Name: "Unknown", RealName: "Unknown"}
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
			log.Printf("Error writing to Google Sheets: %v", err)

			// Send error notification to Slack for individual message failures
			errorMessage := fmt.Sprintf("⚠️ メッセージの記録に失敗しました。\n"+
				"エラー: %v\n"+
				"管理者にお問い合わせください。", err)
			if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
				log.Printf("Error sending failure notification: %v", err)
			}

			return err
		}

		log.Printf("Message recorded: %s in #%s by %s", record.Text, record.ChannelName, record.UserHandle)
	} else {
		log.Printf("Google Sheets not configured, message logged: %s in #%s by %s", record.Text, record.ChannelName, record.UserHandle)
	}

	return nil
}

func handleMemberJoined(cfg *config.Config, event *Event) error {
	// Check if the bot itself was added to the channel
	slackClient := NewClient(cfg.SlackBotToken)

	// Get bot user info to check if it's the bot being added
	// For now, we'll handle any member join as a potential bot addition

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

	// Initialize Google Sheets if configured
	if cfg.GoogleSheetsCredentials != "" && cfg.SpreadsheetID != "" {
		sheetsClient, err := sheets.NewClient(cfg.GoogleSheetsCredentials)
		if err != nil {
			log.Printf("Error creating Google Sheets client: %v", err)
			errorMessage := "❌ Google Sheetsへの接続に失敗しました。"
			slackClient.SendMessage(event.Event.Channel, errorMessage)
			return err
		}

		// Ensure channel-specific sheet exists (this will create it if needed)
		if err := sheetsClient.EnsureChannelSheetExists(cfg.SpreadsheetID, event.Event.Channel, channelInfo.Name); err != nil {
			log.Printf("Error ensuring channel sheet exists: %v", err)
			errorMessage := "❌ スプレッドシートの初期化に失敗しました。"
			slackClient.SendMessage(event.Event.Channel, errorMessage)
			return err
		}

		// Get channel history with progress tracking
		progressMgr := progress.NewManager()

		// Check if there's existing progress
		if progressMgr.HasProgress(event.Event.Channel) {
			log.Printf("Found existing progress for channel %s, resuming...", event.Event.Channel)
			resumeMessage := "🔄 前回の処理を再開しています..."
			if err := slackClient.SendMessage(event.Event.Channel, resumeMessage); err != nil {
				log.Printf("Error sending resume message: %v", err)
			}
		}

		records, err := slackClient.GetChannelHistoryWithProgress(event.Event.Channel, channelInfo.Name, 0, progressMgr)
		if err != nil {
			log.Printf("Error getting channel history for initial recording: %v", err)
			errorMessage := "❌ チャンネル履歴の取得に失敗しました。"
			slackClient.SendMessage(event.Event.Channel, errorMessage)
			return err
		}

		processedCount := 0
		failureCount := 0

		if len(records) > 0 {
			// Send progress message
			progressMessage := fmt.Sprintf("📚 %d件のメッセージ履歴をスプレッドシートに記録しています...", len(records))
			if err := slackClient.SendMessage(event.Event.Channel, progressMessage); err != nil {
				log.Printf("Error sending progress message: %v", err)
			}

			log.Printf("Starting to write %d messages to spreadsheet for initial recording in channel %s", len(records), channelInfo.Name)

			// Update progress phase to writing
			if err := progressMgr.UpdatePhase(event.Event.Channel, "writing"); err != nil {
				log.Printf("Warning: Could not update progress phase: %v", err)
			}

			// Write all messages in batch (this ensures chronological order)
			if err := sheetsClient.WriteBatchMessages(cfg.SpreadsheetID, records); err != nil {
				log.Printf("Error writing batch messages to sheets after retries: %v", err)
				errorMessage := fmt.Sprintf("❌ 初回記録のスプレッドシートへの記録に失敗しました（4回試行後）\n"+
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

			processedCount = len(records)
			failureCount = 0
		}

		// Send completion message with sheet URL and statistics
		sheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s", cfg.SpreadsheetID)
		var completionMessage string

		if len(records) == 0 {
			completionMessage = fmt.Sprintf("✅ スプレッドシートの初期化が完了しました！\n"+
				"記録するメッセージはありませんでした。\n"+
				"記録先: %s", sheetURL)
		} else if failureCount > 0 {
			completionMessage = fmt.Sprintf("⚠️ 初回のメッセージ履歴記録が完了しました（一部エラーあり）\n"+
				"記録されたメッセージ数: %d件\n"+
				"失敗したメッセージ数: %d件\n"+
				"記録先: %s", processedCount, failureCount, sheetURL)
		} else {
			completionMessage = fmt.Sprintf("✅ 初回のメッセージ履歴記録が完了しました！\n"+
				"記録されたメッセージ数: %d件\n"+
				"記録先: %s", processedCount, sheetURL)
		}

		if err := slackClient.SendMessage(event.Event.Channel, completionMessage); err != nil {
			log.Printf("Error sending completion message: %v", err)
		}

		log.Printf("Bot added to channel #%s, %d messages recorded", channelInfo.Name, processedCount)
	} else {
		// Send message about missing configuration
		configMessage := "⚠️ Google Sheetsの設定が完了していません。管理者にお問い合わせください。"
		if err := slackClient.SendMessage(event.Event.Channel, configMessage); err != nil {
			log.Printf("Error sending config message: %v", err)
		}
	}

	return nil
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

	// Handle reset request
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

		resetMessage := "✅ シートをリセットしました。全履歴を再取得します..."
		if err := slackClient.SendMessage(event.Event.Channel, resetMessage); err != nil {
			log.Printf("Error sending reset confirmation: %v", err)
		}

		log.Printf("Sheet reset completed for channel %s", channelInfo.Name)
	}

	// Get channel history with progress tracking
	progressMgr := progress.NewManager()

	// For reset requests, clean up any existing progress first
	if isResetRequest {
		if err := progressMgr.DeleteProgress(event.Event.Channel); err != nil {
			log.Printf("Warning: Could not clean up existing progress: %v", err)
		}
	}

	// Check if there's existing progress
	if progressMgr.HasProgress(event.Event.Channel) {
		log.Printf("Found existing progress for channel %s, resuming...", event.Event.Channel)
		resumeMessage := "🔄 前回の処理を再開しています..."
		if err := slackClient.SendMessage(event.Event.Channel, resumeMessage); err != nil {
			log.Printf("Error sending resume message: %v", err)
		}
	}

	records, err := slackClient.GetChannelHistoryWithProgress(event.Event.Channel, channelInfo.Name, 0, progressMgr)
	if err != nil {
		log.Printf("Error getting channel history: %v", err)
		errorMessage := "❌ チャンネル履歴の取得に失敗しました。"
		slackClient.SendMessage(event.Event.Channel, errorMessage)
		return err
	}

	if len(records) == 0 {
		noMessagesMsg := "ℹ️ 記録するメッセージが見つかりませんでした。"
		slackClient.SendMessage(event.Event.Channel, noMessagesMsg)
		return nil
	}

	log.Printf("Starting to write %d messages to spreadsheet for channel %s", len(records), channelInfo.Name)

	// Update progress phase to writing
	if err := progressMgr.UpdatePhase(event.Event.Channel, "writing"); err != nil {
		log.Printf("Warning: Could not update progress phase: %v", err)
	}

	// Write all messages in batch (this ensures chronological order)
	var processedCount int
	var failureCount int

	if err := sheetsClient.WriteBatchMessages(cfg.SpreadsheetID, records); err != nil {
		log.Printf("Error writing batch messages to sheets after retries: %v", err)
		errorMessage := fmt.Sprintf("❌ スプレッドシートへの記録に失敗しました（4回試行後）\n"+
			"エラー: %v\n"+
			"ネットワークまたはAPI制限の問題の可能性があります。\n"+
			"しばらく時間をおいてから再度お試しください。", err)
		// Slack通知も再試行ロジックを使用
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

	processedCount = len(records)
	failureCount = 0

	// Send completion message
	sheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s", cfg.SpreadsheetID)
	var completionMessage string

	if isResetRequest {
		if failureCount > 0 {
			completionMessage = fmt.Sprintf("⚠️ シートリセット後の履歴記録が完了しました（一部エラーあり）\n"+
				"記録されたメッセージ数: %d件\n"+
				"失敗したメッセージ数: %d件\n"+
				"記録先: %s", processedCount, failureCount, sheetURL)
		} else {
			completionMessage = fmt.Sprintf("✅ シートリセット後の履歴記録が完了しました！\n"+
				"記録されたメッセージ数: %d件\n"+
				"記録先: %s", processedCount, sheetURL)
		}
	} else {
		if failureCount > 0 {
			completionMessage = fmt.Sprintf("⚠️ 過去のメッセージ履歴の記録が完了しました（一部エラーあり）\n"+
				"記録されたメッセージ数: %d件\n"+
				"失敗したメッセージ数: %d件\n"+
				"記録先: %s", processedCount, failureCount, sheetURL)
		} else {
			completionMessage = fmt.Sprintf("✅ 過去のメッセージ履歴の記録が完了しました！\n"+
				"記録されたメッセージ数: %d件\n"+
				"記録先: %s", processedCount, sheetURL)
		}
	}

	if err := slackClient.SendMessage(event.Event.Channel, completionMessage); err != nil {
		log.Printf("Error sending completion message: %v", err)
	}

	log.Printf("App mention processed: %d messages recorded from #%s", processedCount, channelInfo.Name)
	log.Printf("App mention processing completed for user %s, timestamp %s", event.Event.User, event.Event.Timestamp)
	return nil
}
