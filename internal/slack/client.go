package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"slack-to-google-sheets-bot/internal/progress"
	"slack-to-google-sheets-bot/internal/sheets"
)

type Client struct {
	token        string
	httpClient   *http.Client
	userCache    map[string]*UserInfo
	channelCache map[string]*ChannelInfo
	botCache     map[string]*BotInfo
}

type UserInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
}

type ChannelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type BotInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type UserResponse struct {
	OK   bool     `json:"ok"`
	User UserInfo `json:"user"`
}

type ChannelResponse struct {
	OK      bool        `json:"ok"`
	Channel ChannelInfo `json:"channel"`
}

type BotResponse struct {
	OK  bool    `json:"ok"`
	Bot BotInfo `json:"bot"`
}

func NewClient(token string) *Client {
	return &Client{
		token:        token,
		httpClient:   &http.Client{},
		userCache:    make(map[string]*UserInfo),
		channelCache: make(map[string]*ChannelInfo),
		botCache:     make(map[string]*BotInfo),
	}
}

const maxRetryAttempts = 4

// retryWithBackoff executes a function with exponential backoff retry logic
func retryWithBackoff(operation func() error, description string) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetryAttempts; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			if attempt > 1 {
				log.Printf("Retry successful for %s on attempt %d", description, attempt)
			}
			return nil
		}

		log.Printf("Attempt %d failed for %s: %v", attempt, description, lastErr)

		// If this was the last attempt, don't sleep
		if attempt == maxRetryAttempts {
			break
		}

		// Sleep for attempt seconds (1s, 2s, 3s)
		delay := time.Duration(attempt) * time.Second
		log.Printf("Retrying %s in %v (attempt %d)...", description, delay, attempt+1)
		time.Sleep(delay)
	}

	log.Printf("All retry attempts failed for %s. Final error: %v", description, lastErr)
	return lastErr
}

func (c *Client) GetUserInfo(userID string) (*UserInfo, error) {
	// Check cache first
	if user, exists := c.userCache[userID]; exists {
		return user, nil
	}

	var result *UserInfo
	err := retryWithBackoff(func() error {
		// Rate limiting: small delay between API calls
		time.Sleep(100 * time.Millisecond)

		url := fmt.Sprintf("https://slack.com/api/users.info?user=%s", userID)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		var userResp UserResponse
		if err := json.Unmarshal(body, &userResp); err != nil {
			return err
		}

		if !userResp.OK {
			return fmt.Errorf("slack API error: %s", string(body))
		}

		result = &userResp.User
		return nil
	}, fmt.Sprintf("get user info for %s", userID))

	if err != nil {
		return nil, err
	}

	// Cache the result
	c.userCache[userID] = result

	return result, nil
}

func (c *Client) GetChannelInfo(channelID string) (*ChannelInfo, error) {
	// Check cache first
	if channel, exists := c.channelCache[channelID]; exists {
		return channel, nil
	}

	var result *ChannelInfo
	err := retryWithBackoff(func() error {
		// Rate limiting: small delay between API calls
		time.Sleep(100 * time.Millisecond)

		url := fmt.Sprintf("https://slack.com/api/conversations.info?channel=%s", channelID)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		var channelResp ChannelResponse
		if err := json.Unmarshal(body, &channelResp); err != nil {
			return err
		}

		if !channelResp.OK {
			return fmt.Errorf("slack API error: %s", string(body))
		}

		result = &channelResp.Channel
		return nil
	}, fmt.Sprintf("get channel info for %s", channelID))

	if err != nil {
		return nil, err
	}

	// Cache the result
	c.channelCache[channelID] = result

	return result, nil
}

// GetBotInfo retrieves bot information from Slack API with caching and retry logic.
//
// Args:
//   - botID: Slack bot ID (e.g., "B123456789")
//
// Returns:
//   - *BotInfo: Bot information including name
//   - error: API error or network failure after 4 retry attempts
func (c *Client) GetBotInfo(botID string) (*BotInfo, error) {
	// Check cache first
	if bot, exists := c.botCache[botID]; exists {
		return bot, nil
	}

	var result *BotInfo
	err := retryWithBackoff(func() error {
		// Rate limiting: small delay between API calls
		time.Sleep(100 * time.Millisecond)

		url := fmt.Sprintf("https://slack.com/api/bots.info?bot=%s", botID)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		var botResp BotResponse
		if err := json.Unmarshal(body, &botResp); err != nil {
			return err
		}

		if !botResp.OK {
			return fmt.Errorf("slack API error: %s", string(body))
		}

		result = &botResp.Bot
		return nil
	}, fmt.Sprintf("get bot info for %s", botID))

	if err != nil {
		return nil, err
	}

	// Cache the result
	c.botCache[botID] = result

	return result, nil
}

func (c *Client) SendMessage(channel, text string) error {
	return retryWithBackoff(func() error {
		url := "https://slack.com/api/chat.postMessage"

		payload := map[string]interface{}{
			"channel": channel,
			"text":    text,
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonData)))
		if err != nil {
			return err
		}

		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		var response map[string]interface{}
		if err := json.Unmarshal(body, &response); err != nil {
			return err
		}

		if ok, exists := response["ok"].(bool); !exists || !ok {
			return fmt.Errorf("slack API error: %s", string(body))
		}

		return nil
	}, fmt.Sprintf("send message to channel %s", channel))
}

type HistoryResponse struct {
	OK               bool             `json:"ok"`
	Messages         []HistoryMessage `json:"messages"`
	HasMore          bool             `json:"has_more"`
	ResponseMetadata ResponseMetadata `json:"response_metadata"`
}

type ResponseMetadata struct {
	NextCursor string `json:"next_cursor"`
}

type HistoryMessage struct {
	Type      string `json:"type"`
	User      string `json:"user"`
	Text      string `json:"text"`
	Timestamp string `json:"ts"`
	ThreadTS  string `json:"thread_ts,omitempty"`
	BotID     string `json:"bot_id,omitempty"`
	Username  string `json:"username,omitempty"`
}

func (c *Client) GetChannelHistory(channelID string, limit int) ([]HistoryMessage, error) {
	var allMessages []HistoryMessage
	cursor := ""
	pageLimit := 200 // Maximum per page

	log.Printf("Starting to retrieve channel history for %s (limit: %d)", channelID, limit)

	for {
		var historyResp HistoryResponse
		err := retryWithBackoff(func() error {
			var url string
			if cursor == "" {
				url = fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&limit=%d", channelID, pageLimit)
			} else {
				url = fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&limit=%d&cursor=%s", channelID, pageLimit, cursor)
			}

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return err
			}

			req.Header.Set("Authorization", "Bearer "+c.token)

			resp, err := c.httpClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			if err := json.Unmarshal(body, &historyResp); err != nil {
				return err
			}

			if !historyResp.OK {
				return fmt.Errorf("slack API error: %s", string(body))
			}

			return nil
		}, fmt.Sprintf("get channel history page for %s", channelID))

		if err != nil {
			return nil, err
		}

		log.Printf("Retrieved %d messages in this page", len(historyResp.Messages))

		// Add main messages
		allMessages = append(allMessages, historyResp.Messages...)

		// Get thread replies for each message with thread_ts
		for _, msg := range historyResp.Messages {
			if msg.ThreadTS != "" && msg.ThreadTS == msg.Timestamp {
				// This is a parent message, get its replies
				threadReplies, err := c.getThreadReplies(channelID, msg.ThreadTS)
				if err != nil {
					log.Printf("Error getting thread replies for %s: %v", msg.ThreadTS, err)
					continue
				}
				log.Printf("Retrieved %d thread replies for message %s", len(threadReplies), msg.ThreadTS)
				allMessages = append(allMessages, threadReplies...)
			}
		}

		// Check if we have more pages and haven't reached the limit
		if !historyResp.HasMore || (limit > 0 && len(allMessages) >= limit) {
			break
		}

		cursor = historyResp.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}

		// Add rate limiting between requests
		time.Sleep(150 * time.Millisecond)
	}

	// Sort messages by timestamp (oldest first)
	sort.Slice(allMessages, func(i, j int) bool {
		return allMessages[i].Timestamp < allMessages[j].Timestamp
	})

	// Apply limit if specified
	if limit > 0 && len(allMessages) > limit {
		allMessages = allMessages[:limit]
	}

	log.Printf("Retrieved %d total messages (including thread replies) from channel %s", len(allMessages), channelID)
	return allMessages, nil
}

func (c *Client) getThreadReplies(channelID, threadTS string) ([]HistoryMessage, error) {
	var allReplies []HistoryMessage
	cursor := ""
	pageLimit := 200 // Maximum per page

	for {
		var repliesResp HistoryResponse
		err := retryWithBackoff(func() error {
			var url string
			if cursor == "" {
				url = fmt.Sprintf("https://slack.com/api/conversations.replies?channel=%s&ts=%s&limit=%d", channelID, threadTS, pageLimit)
			} else {
				url = fmt.Sprintf("https://slack.com/api/conversations.replies?channel=%s&ts=%s&limit=%d&cursor=%s", channelID, threadTS, pageLimit, cursor)
			}

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return err
			}

			req.Header.Set("Authorization", "Bearer "+c.token)

			resp, err := c.httpClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			if err := json.Unmarshal(body, &repliesResp); err != nil {
				return err
			}

			if !repliesResp.OK {
				return fmt.Errorf("slack API error getting thread replies: %s", string(body))
			}

			return nil
		}, fmt.Sprintf("get thread replies for %s in %s", threadTS, channelID))

		if err != nil {
			return nil, err
		}

		// Skip the first message as it's the parent (already included in main messages)
		if len(repliesResp.Messages) > 1 {
			allReplies = append(allReplies, repliesResp.Messages[1:]...)
		}

		// Check if we have more pages
		if !repliesResp.HasMore {
			break
		}

		cursor = repliesResp.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}

		// Add rate limiting between requests
		time.Sleep(150 * time.Millisecond)
	}

	return allReplies, nil
}

// GetChannelHistoryWithProgress retrieves channel history with progress tracking and resumption capability
func (c *Client) GetChannelHistoryWithProgress(channelID, channelName string, limit int, progressMgr *progress.Manager) ([]*sheets.MessageRecord, error) {
	// Check for existing progress
	existingProgress, err := progressMgr.LoadProgress(channelID)
	if err != nil {
		log.Printf("Error loading progress: %v", err)
		existingProgress = nil
	}

	var cursor string
	var allRecords []*sheets.MessageRecord
	startTime := time.Now()

	if existingProgress != nil {
		log.Printf("Resuming channel history retrieval for %s from previous session", channelID)
		cursor = existingProgress.LastCursor
		allRecords = existingProgress.Messages
		startTime = existingProgress.StartTime

		if existingProgress.Phase == "completed" {
			log.Printf("Channel history retrieval already completed for %s", channelID)
			return allRecords, nil
		}
	} else {
		log.Printf("Starting new channel history retrieval for %s", channelID)
		// Create new progress
		newProgress := &progress.ChannelProgress{
			ChannelID:         channelID,
			ChannelName:       channelName,
			StartTime:         startTime,
			LastUpdated:       startTime,
			LastCursor:        "",
			TotalMessages:     0,
			ProcessedMessages: 0,
			Messages:          []*sheets.MessageRecord{},
			Phase:             "fetching",
		}

		if err := progressMgr.SaveProgress(newProgress); err != nil {
			log.Printf("Warning: Could not save initial progress: %v", err)
		}
	}

	pageLimit := 200 // Maximum per page
	messageCount := 0

	for {
		var historyResp HistoryResponse
		err := retryWithBackoff(func() error {
			var url string
			if cursor == "" {
				url = fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&limit=%d", channelID, pageLimit)
			} else {
				url = fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&limit=%d&cursor=%s", channelID, pageLimit, cursor)
			}

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return err
			}

			req.Header.Set("Authorization", "Bearer "+c.token)

			resp, err := c.httpClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			if err := json.Unmarshal(body, &historyResp); err != nil {
				return err
			}

			if !historyResp.OK {
				return fmt.Errorf("slack API error: %s", string(body))
			}

			return nil
		}, fmt.Sprintf("get channel history page for %s", channelID))

		if err != nil {
			return nil, err
		}

		log.Printf("Retrieved %d messages in this page", len(historyResp.Messages))

		// Convert messages to MessageRecord format and add to collection
		var pageRecords []*sheets.MessageRecord
		for _, msg := range historyResp.Messages {
			if msg.Type == "message" {
				// Get user info (handle both human users and bots)
				var userInfo *UserInfo
				if msg.User != "" {
					// Human user message
					var err error
					userInfo, err = c.GetUserInfo(msg.User)
					if err != nil {
						log.Printf("Error getting user info for %s: %v", msg.User, err)
						userInfo = &UserInfo{ID: msg.User, Name: "Unknown", RealName: "Unknown"}
					}
				} else if msg.BotID != "" || msg.Username != "" {
					// Bot message - try to get bot information from API
					botName := msg.Username
					if msg.BotID != "" {
						// Try to get actual bot name from API
						if botInfo, err := c.GetBotInfo(msg.BotID); err == nil {
							botName = botInfo.Name
						} else {
							log.Printf("Could not get bot info for %s: %v", msg.BotID, err)
							// Fallback to username or "Bot"
							if msg.Username != "" {
								botName = msg.Username
							} else {
								botName = "Bot"
							}
						}
					} else if botName == "" {
						botName = "Bot"
					}
					userInfo = &UserInfo{
						ID:       msg.BotID,
						Name:     botName,
						RealName: botName,
					}
				} else {
					// System message or unknown
					userInfo = &UserInfo{ID: "", Name: "System", RealName: "System"}
				}

				// Parse timestamp and convert to JST
				timestamp := convertSlackTimestampToJST(msg.Timestamp)

				// Format message text
				formattedText := c.FormatMessageText(msg.Text)

				record := &sheets.MessageRecord{
					Timestamp:    timestamp,
					Channel:      channelID,
					ChannelName:  channelName,
					User:         msg.User,
					UserHandle:   userInfo.Name,
					UserRealName: userInfo.RealName,
					Text:         formattedText,
					ThreadTS:     msg.ThreadTS,
					MessageTS:    msg.Timestamp,
				}

				pageRecords = append(pageRecords, record)
			}
		}

		// Get thread replies for each message with thread_ts
		for _, msg := range historyResp.Messages {
			if msg.ThreadTS != "" && msg.ThreadTS == msg.Timestamp {
				// This is a parent message, get its replies
				threadReplies, err := c.getThreadReplies(channelID, msg.ThreadTS)
				if err != nil {
					log.Printf("Error getting thread replies for %s: %v", msg.ThreadTS, err)
					continue
				}
				log.Printf("Retrieved %d thread replies for message %s", len(threadReplies), msg.ThreadTS)

				// Convert thread replies to MessageRecord format
				for _, reply := range threadReplies {
					if reply.Type == "message" {
						// Get user info (handle both human users and bots)
						var userInfo *UserInfo
						if reply.User != "" {
							// Human user message
							var err error
							userInfo, err = c.GetUserInfo(reply.User)
							if err != nil {
								log.Printf("Error getting user info for %s: %v", reply.User, err)
								userInfo = &UserInfo{ID: reply.User, Name: "Unknown", RealName: "Unknown"}
							}
						} else if reply.BotID != "" || reply.Username != "" {
							// Bot message - try to get bot information from API
							botName := reply.Username
							if reply.BotID != "" {
								// Try to get actual bot name from API
								if botInfo, err := c.GetBotInfo(reply.BotID); err == nil {
									botName = botInfo.Name
								} else {
									log.Printf("Could not get bot info for %s: %v", reply.BotID, err)
									// Fallback to username or "Bot"
									if reply.Username != "" {
										botName = reply.Username
									} else {
										botName = "Bot"
									}
								}
							} else if botName == "" {
								botName = "Bot"
							}
							userInfo = &UserInfo{
								ID:       reply.BotID,
								Name:     botName,
								RealName: botName,
							}
						} else {
							// System message or unknown
							userInfo = &UserInfo{ID: "", Name: "System", RealName: "System"}
						}

						timestamp := convertSlackTimestampToJST(reply.Timestamp)

						formattedText := c.FormatMessageText(reply.Text)

						record := &sheets.MessageRecord{
							Timestamp:    timestamp,
							Channel:      channelID,
							ChannelName:  channelName,
							User:         reply.User,
							UserHandle:   userInfo.Name,
							UserRealName: userInfo.RealName,
							Text:         formattedText,
							ThreadTS:     reply.ThreadTS,
							MessageTS:    reply.Timestamp,
						}

						pageRecords = append(pageRecords, record)
					}
				}
			}
		}

		// Add page records to total collection
		allRecords = append(allRecords, pageRecords...)
		messageCount += len(pageRecords)

		// Update progress
		cursor = historyResp.ResponseMetadata.NextCursor
		updateProgress := &progress.ChannelProgress{
			ChannelID:         channelID,
			ChannelName:       channelName,
			StartTime:         startTime,
			LastUpdated:       time.Now(),
			LastCursor:        cursor,
			TotalMessages:     messageCount, // This will be updated as we discover more
			ProcessedMessages: messageCount,
			Messages:          allRecords,
			Phase:             "fetching",
		}

		if err := progressMgr.SaveProgress(updateProgress); err != nil {
			log.Printf("Warning: Could not save progress: %v", err)
		}

		log.Printf("Progress: %d messages collected so far", messageCount)

		// Check if we have more pages and haven't reached the limit
		if !historyResp.HasMore || (limit > 0 && messageCount >= limit) {
			break
		}

		if cursor == "" {
			break
		}

		// Add rate limiting between requests
		time.Sleep(150 * time.Millisecond)
	}

	// Sort messages by timestamp (oldest first)
	sort.Slice(allRecords, func(i, j int) bool {
		return allRecords[i].Timestamp.Before(allRecords[j].Timestamp)
	})

	// Apply limit if specified
	if limit > 0 && len(allRecords) > limit {
		allRecords = allRecords[:limit]
	}

	// Update final progress
	finalProgress := &progress.ChannelProgress{
		ChannelID:         channelID,
		ChannelName:       channelName,
		StartTime:         startTime,
		LastUpdated:       time.Now(),
		LastCursor:        "",
		TotalMessages:     len(allRecords),
		ProcessedMessages: len(allRecords),
		Messages:          allRecords,
		Phase:             "fetching_completed",
	}

	if err := progressMgr.SaveProgress(finalProgress); err != nil {
		log.Printf("Warning: Could not save final progress: %v", err)
	}

	log.Printf("Retrieved %d total messages (including thread replies) from channel %s", len(allRecords), channelID)
	return allRecords, nil
}

func (c *Client) FormatMessageText(text string) string {
	// Convert user mentions: <@U123456> -> @username
	userMentionRe := regexp.MustCompile(`<@([UW][A-Z0-9]+)>`)
	text = userMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		userID := userMentionRe.FindStringSubmatch(match)[1]
		if user, err := c.GetUserInfo(userID); err == nil {
			return "@" + user.Name
		}
		return match // Keep original if failed to resolve
	})

	// Convert channel mentions: <#C123456|general> -> #general
	channelMentionRe := regexp.MustCompile(`<#[CD][A-Z0-9]+\|([^>]+)>`)
	text = channelMentionRe.ReplaceAllString(text, "#$1")

	// Convert simple channel mentions: <#C123456> -> #channelname
	simpleChannelRe := regexp.MustCompile(`<#([CD][A-Z0-9]+)>`)
	text = simpleChannelRe.ReplaceAllStringFunc(text, func(match string) string {
		channelID := simpleChannelRe.FindStringSubmatch(match)[1]
		if channel, err := c.GetChannelInfo(channelID); err == nil {
			return "#" + channel.Name
		}
		return match // Keep original if failed to resolve
	})

	// Remove other Slack formatting
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&amp;", "&")

	return text
}

// getMessagesAfterTime retrieves messages posted after a specific time
// Uses optimized approach: starts from latest messages and stops when encountering older messages
func (c *Client) getMessagesAfterTime(channelID, channelName string, afterTime time.Time) ([]*sheets.MessageRecord, error) {
	var allRecords []*sheets.MessageRecord
	cursor := ""
	pageLimit := 50 // Smaller page size for faster response and reduced API calls

	log.Printf("Getting messages after %v for channel %s (optimized approach)", afterTime, channelID)

	for {
		var historyResp HistoryResponse
		err := retryWithBackoff(func() error {
			var url string
			if cursor == "" {
				url = fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&limit=%d&oldest=%f",
					channelID, pageLimit, float64(afterTime.Unix()))
			} else {
				url = fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&limit=%d&oldest=%f&cursor=%s",
					channelID, pageLimit, float64(afterTime.Unix()), cursor)
			}

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return err
			}

			req.Header.Set("Authorization", "Bearer "+c.token)

			resp, err := c.httpClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			if err := json.Unmarshal(body, &historyResp); err != nil {
				return err
			}

			if !historyResp.OK {
				return fmt.Errorf("slack API error: %s", string(body))
			}

			return nil
		}, fmt.Sprintf("get messages after time for %s", channelID))

		if err != nil {
			return nil, err
		}

		// Convert messages to MessageRecord format and check for early termination
		foundOlderMessage := false
		var pageRecords []*sheets.MessageRecord

		for _, msg := range historyResp.Messages {
			if msg.Type == "message" {
				// Parse timestamp and convert to JST
				msgTime := convertSlackTimestampToJST(msg.Timestamp)

				// If we encounter a message older than or equal to afterTime, stop processing
				// since messages are ordered newest first
				if msgTime.Before(afterTime) || msgTime.Equal(afterTime) {
					foundOlderMessage = true
					break
				}

				// Get user info (handle both human users and bots)
				var userInfo *UserInfo
				if msg.User != "" {
					var err error
					userInfo, err = c.GetUserInfo(msg.User)
					if err != nil {
						log.Printf("Error getting user info for %s: %v", msg.User, err)
						userInfo = &UserInfo{ID: msg.User, Name: "Unknown", RealName: "Unknown"}
					}
				} else if msg.BotID != "" || msg.Username != "" {
					botName := msg.Username
					if msg.BotID != "" {
						if botInfo, err := c.GetBotInfo(msg.BotID); err == nil {
							botName = botInfo.Name
						} else {
							log.Printf("Could not get bot info for %s: %v", msg.BotID, err)
							if msg.Username != "" {
								botName = msg.Username
							} else {
								botName = "Bot"
							}
						}
					} else if botName == "" {
						botName = "Bot"
					}
					userInfo = &UserInfo{
						ID:       msg.BotID,
						Name:     botName,
						RealName: botName,
					}
				} else {
					userInfo = &UserInfo{ID: "", Name: "System", RealName: "System"}
				}

				formattedText := c.FormatMessageText(msg.Text)

				record := &sheets.MessageRecord{
					Timestamp:    msgTime,
					Channel:      channelID,
					ChannelName:  channelName,
					User:         msg.User,
					UserHandle:   userInfo.Name,
					UserRealName: userInfo.RealName,
					Text:         formattedText,
					ThreadTS:     msg.ThreadTS,
					MessageTS:    msg.Timestamp,
				}

				pageRecords = append(pageRecords, record)
			}
		}

		// Add page records to total collection
		allRecords = append(allRecords, pageRecords...)

		// Get thread replies for messages in this page that have thread_ts and are newer than afterTime
		// Only process if we haven't found older messages yet
		if !foundOlderMessage {
			for _, msg := range historyResp.Messages {
				if msg.ThreadTS != "" && msg.ThreadTS == msg.Timestamp {
					// Parse parent message timestamp to check if it's newer than afterTime
					parentTime := convertSlackTimestampToJST(msg.Timestamp)

					// Only get thread replies for parent messages newer than afterTime
					if parentTime.Before(afterTime) || parentTime.Equal(afterTime) {
						continue
					}

					// This is a parent message newer than afterTime, get its replies
					threadReplies, err := c.getThreadReplies(channelID, msg.ThreadTS)
					if err != nil {
						log.Printf("Error getting thread replies for %s: %v", msg.ThreadTS, err)
						continue
					}

					// Process thread replies, filtering by afterTime
					for _, reply := range threadReplies {
						if reply.Type == "message" {
							replyTime := convertSlackTimestampToJST(reply.Timestamp)

							// Only include thread replies that are newer than afterTime
							if replyTime.Before(afterTime) || replyTime.Equal(afterTime) {
								continue
							}

							// Get user info for thread reply
							var userInfo *UserInfo
							if reply.User != "" {
								var err error
								userInfo, err = c.GetUserInfo(reply.User)
								if err != nil {
									log.Printf("Error getting user info for %s: %v", reply.User, err)
									userInfo = &UserInfo{ID: reply.User, Name: "Unknown", RealName: "Unknown"}
								}
							} else if reply.BotID != "" || reply.Username != "" {
								botName := reply.Username
								if reply.BotID != "" {
									if botInfo, err := c.GetBotInfo(reply.BotID); err == nil {
										botName = botInfo.Name
									} else {
										log.Printf("Could not get bot info for %s: %v", reply.BotID, err)
										if reply.Username != "" {
											botName = reply.Username
										} else {
											botName = "Bot"
										}
									}
								} else if botName == "" {
									botName = "Bot"
								}
								userInfo = &UserInfo{
									ID:       reply.BotID,
									Name:     botName,
									RealName: botName,
								}
							} else {
								userInfo = &UserInfo{ID: "", Name: "System", RealName: "System"}
							}

							formattedText := c.FormatMessageText(reply.Text)

							replyRecord := &sheets.MessageRecord{
								Timestamp:    replyTime,
								Channel:      channelID,
								ChannelName:  channelName,
								User:         reply.User,
								UserHandle:   userInfo.Name,
								UserRealName: userInfo.RealName,
								Text:         formattedText,
								ThreadTS:     reply.ThreadTS,
								MessageTS:    reply.Timestamp,
							}

							allRecords = append(allRecords, replyRecord)
						}
					}
				}
			}
		}

		// If we found an older message, we can stop searching
		if foundOlderMessage {
			log.Printf("Found messages older than %v, stopping search (optimization)", afterTime)
			break
		}

		// Check if we have more pages
		if !historyResp.HasMore {
			break
		}

		cursor = historyResp.ResponseMetadata.NextCursor
		if cursor == "" {
			break
		}

		time.Sleep(150 * time.Millisecond)
	}

	// Sort messages by timestamp (oldest first)
	sort.Slice(allRecords, func(i, j int) bool {
		return allRecords[i].Timestamp.Before(allRecords[j].Timestamp)
	})

	log.Printf("Retrieved %d new messages after %v from channel %s", len(allRecords), afterTime, channelID)
	return allRecords, nil
}
