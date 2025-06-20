package slack

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"slack-to-google-sheets-bot/internal/config"
	"slack-to-google-sheets-bot/internal/sheets"
)

const (
	MaxFailureCount = 3
)

var (
	processingEvents = make(map[string]bool)
	processingMutex  = sync.Mutex{}
	recentMentions   = make(map[string]time.Time)
	recentMutex      = sync.Mutex{}
)

func HandleEvent(cfg *config.Config, event *Event) error {
	// Log all incoming events for debugging
	log.Printf("Received event: type=%s, user=%s, text=%s, timestamp=%s",
		event.Event.Type, event.Event.User, event.Event.Text, event.Event.Timestamp)

	// Handle member joined channel event
	if event.Event.Type == "member_joined_channel" {
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

		// Check for recent mentions in same channel (within 120 seconds)
		recentMutex.Lock()
		if lastMentionTime, exists := recentMentions[event.Event.Channel]; exists {
			if time.Since(lastMentionTime) < 120*time.Second {
				recentMutex.Unlock()
				processingMutex.Lock()
				delete(processingEvents, eventKey)
				processingMutex.Unlock()
				log.Printf("Recent mention detected in channel %s (within 120s), skipping", event.Event.Channel)
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

		// Get channel history for initial recording (limit to 100 messages to avoid overwhelming)
		messages, err := slackClient.GetChannelHistory(event.Event.Channel, 100)
		if err != nil {
			log.Printf("Error getting channel history for initial recording: %v", err)
			errorMessage := "❌ チャンネル履歴の取得に失敗しました。"
			slackClient.SendMessage(event.Event.Channel, errorMessage)
			return err
		}

		// Filter out bot messages and process in reverse order (oldest first)
		var validMessages []HistoryMessage
		for i := len(messages) - 1; i >= 0; i-- {
			msg := messages[i]
			if msg.Type == "message" && msg.User != "" && msg.Text != "" {
				validMessages = append(validMessages, msg)
			}
		}

		processedCount := 0
		failureCount := 0

		if len(validMessages) > 0 {
			// Send progress message
			progressMessage := fmt.Sprintf("📚 %d件のメッセージ履歴を記録しています...", len(validMessages))
			if err := slackClient.SendMessage(event.Event.Channel, progressMessage); err != nil {
				log.Printf("Error sending progress message: %v", err)
			}

			log.Printf("Starting to process %d messages for initial recording in channel %s", len(validMessages), channelInfo.Name)

			// Process each message with failure tracking
			var lastError error
			for i, msg := range validMessages {
				previewText := msg.Text
				if len(previewText) > 50 {
					previewText = previewText[:50] + "..."
				}
				log.Printf("Processing message %d/%d: %s", i+1, len(validMessages), previewText)

				// Get user info
				userInfo, err := slackClient.GetUserInfo(msg.User)
				if err != nil {
					log.Printf("Error getting user info for %s: %v", msg.User, err)
					userInfo = &UserInfo{ID: msg.User, Name: "Unknown", RealName: "Unknown"}
				}

				// Parse timestamp
				ts, err := strconv.ParseFloat(msg.Timestamp, 64)
				if err != nil {
					ts = float64(time.Now().Unix())
				}
				timestamp := time.Unix(int64(ts), 0)

				// Format message text (convert mentions and channels)
				formattedText := slackClient.FormatMessageText(msg.Text)

				// Create message record
				record := sheets.MessageRecord{
					Timestamp:    timestamp,
					Channel:      event.Event.Channel,
					ChannelName:  channelInfo.Name,
					User:         msg.User,
					UserHandle:   userInfo.Name,
					UserRealName: userInfo.RealName,
					Text:         formattedText,
					ThreadTS:     msg.ThreadTS,
					MessageTS:    msg.Timestamp,
				}

				// Write to Google Sheets
				if err := sheetsClient.WriteMessage(cfg.SpreadsheetID, &record); err != nil {
					log.Printf("Error writing message to sheets: %v", err)
					failureCount++
					lastError = err

					// Check if we've exceeded the failure limit
					if failureCount >= MaxFailureCount {
						errorMessage := fmt.Sprintf("❌ スプレッドシートへの記録で%d回連続して失敗したため、処理を中断します。\n"+
							"最後のエラー: %v\n"+
							"記録済みメッセージ数: %d件\n"+
							"管理者にお問い合わせください。", MaxFailureCount, lastError, processedCount)

						if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
							log.Printf("Error sending failure notification: %v", err)
						}

						log.Printf("Stopped initial recording due to %d consecutive failures. Last error: %v", MaxFailureCount, lastError)
						return fmt.Errorf("too many failures (%d): %v", MaxFailureCount, lastError)
					}
					continue
				}

				// Reset failure count on success
				failureCount = 0
				processedCount++
			}
		}

		// Send completion message with sheet URL and statistics
		sheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s", cfg.SpreadsheetID)
		var completionMessage string

		if len(validMessages) == 0 {
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

	// First, record the mention message itself
	if err := recordSingleMessage(cfg, slackClient, event, channelInfo); err != nil {
		log.Printf("Error recording mention message: %v", err)
	}

	// Send acknowledgment message
	ackMessage := fmt.Sprintf("📚 過去のメッセージ履歴を取得しています... (#%s)", channelInfo.Name)
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

	// Get channel history (limit to 100 messages to avoid overwhelming)
	messages, err := slackClient.GetChannelHistory(event.Event.Channel, 100)
	if err != nil {
		log.Printf("Error getting channel history: %v", err)
		errorMessage := "❌ チャンネル履歴の取得に失敗しました。"
		slackClient.SendMessage(event.Event.Channel, errorMessage)
		return err
	}

	// Filter out bot messages and process in reverse order (oldest first)
	var validMessages []HistoryMessage
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Type == "message" && msg.User != "" && msg.Text != "" {
			validMessages = append(validMessages, msg)
		}
	}

	if len(validMessages) == 0 {
		noMessagesMsg := "ℹ️ 記録するメッセージが見つかりませんでした。"
		slackClient.SendMessage(event.Event.Channel, noMessagesMsg)
		return nil
	}

	// Process each message with failure tracking
	processedCount := 0
	failureCount := 0
	var lastError error

	log.Printf("Starting to process %d messages for channel %s", len(validMessages), channelInfo.Name)

	for _, msg := range validMessages {
		// Get user info
		userInfo, err := slackClient.GetUserInfo(msg.User)
		if err != nil {
			log.Printf("Error getting user info for %s: %v", msg.User, err)
			userInfo = &UserInfo{ID: msg.User, Name: "Unknown", RealName: "Unknown"}
		}

		// Parse timestamp
		ts, err := strconv.ParseFloat(msg.Timestamp, 64)
		if err != nil {
			ts = float64(time.Now().Unix())
		}
		timestamp := time.Unix(int64(ts), 0)

		// Format message text (convert mentions and channels)
		formattedText := slackClient.FormatMessageText(msg.Text)

		// Create message record
		record := sheets.MessageRecord{
			Timestamp:    timestamp,
			Channel:      event.Event.Channel,
			ChannelName:  channelInfo.Name,
			User:         msg.User,
			UserHandle:   userInfo.Name,
			UserRealName: userInfo.RealName,
			Text:         formattedText,
			ThreadTS:     msg.ThreadTS,
			MessageTS:    msg.Timestamp,
		}

		// Write to Google Sheets
		if err := sheetsClient.WriteMessage(cfg.SpreadsheetID, &record); err != nil {
			log.Printf("Error writing message to sheets: %v", err)
			failureCount++
			lastError = err

			// Check if we've exceeded the failure limit
			if failureCount >= MaxFailureCount {
				errorMessage := fmt.Sprintf("❌ スプレッドシートへの記録で%d回連続して失敗したため、処理を中断します。\n"+
					"最後のエラー: %v\n"+
					"記録済みメッセージ数: %d件\n"+
					"管理者にお問い合わせください。", MaxFailureCount, lastError, processedCount)

				if err := slackClient.SendMessage(event.Event.Channel, errorMessage); err != nil {
					log.Printf("Error sending failure notification: %v", err)
				}

				log.Printf("Stopped processing due to %d consecutive failures. Last error: %v", MaxFailureCount, lastError)
				return fmt.Errorf("too many failures (%d): %v", MaxFailureCount, lastError)
			}
			continue
		}

		// Reset failure count on success
		failureCount = 0
		processedCount++
	}

	// Send completion message
	sheetURL := fmt.Sprintf("https://docs.google.com/spreadsheets/d/%s", cfg.SpreadsheetID)
	var completionMessage string

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

	if err := slackClient.SendMessage(event.Event.Channel, completionMessage); err != nil {
		log.Printf("Error sending completion message: %v", err)
	}

	log.Printf("App mention processed: %d messages recorded from #%s", processedCount, channelInfo.Name)
	log.Printf("App mention processing completed for user %s, timestamp %s", event.Event.User, event.Event.Timestamp)
	return nil
}
