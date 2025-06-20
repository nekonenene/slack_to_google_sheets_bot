package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	token        string
	httpClient   *http.Client
	userCache    map[string]*UserInfo
	channelCache map[string]*ChannelInfo
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

type UserResponse struct {
	OK   bool     `json:"ok"`
	User UserInfo `json:"user"`
}

type ChannelResponse struct {
	OK      bool        `json:"ok"`
	Channel ChannelInfo `json:"channel"`
}

func NewClient(token string) *Client {
	return &Client{
		token:        token,
		httpClient:   &http.Client{},
		userCache:    make(map[string]*UserInfo),
		channelCache: make(map[string]*ChannelInfo),
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

func (c *Client) searchMessages(query string, page int) (*SearchResponse, error) {
	var result *SearchResponse
	err := retryWithBackoff(func() error {
		// Rate limiting: small delay between API calls
		time.Sleep(200 * time.Millisecond)

		url := fmt.Sprintf("https://slack.com/api/search.messages?query=%s&sort=timestamp&sort_dir=asc&count=100&page=%d",
			strings.ReplaceAll(query, " ", "%20"), page)

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

		var searchResp SearchResponse
		if err := json.Unmarshal(body, &searchResp); err != nil {
			return err
		}

		if !searchResp.OK {
			return fmt.Errorf("slack API error: %s", string(body))
		}

		result = &searchResp
		return nil
	}, fmt.Sprintf("search messages with query: %s", query))

	return result, err
}

func (c *Client) GetChannelHistoryBySearch(channelID string, afterTS string, userTokenClient *Client) ([]HistoryMessage, error) {
	// Get channel info to build query
	channelInfo, err := c.GetChannelInfo(channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel info: %v", err)
	}

	// Build search query
	query := fmt.Sprintf("in:#%s", channelInfo.Name)
	if afterTS != "" {
		// Convert timestamp to date format for search API
		ts, err := strconv.ParseFloat(afterTS, 64)
		if err == nil {
			date := time.Unix(int64(ts), 0).Format("2006-01-02")
			query += fmt.Sprintf(" after:%s", date)
		}
	}

	log.Printf("Starting search-based history retrieval for channel %s with query: %s", channelID, query)

	var allMessages []HistoryMessage
	page := 1
	maxPages := 100 // API limitation

	for page <= maxPages {
		searchResp, err := userTokenClient.searchMessages(query, page)
		if err != nil {
			return nil, fmt.Errorf("search failed on page %d: %v", page, err)
		}

		if len(searchResp.Messages.Matches) == 0 {
			log.Printf("No more messages found on page %d", page)
			break
		}

		// Convert SearchMessage to HistoryMessage and filter by channel
		var pageMessages []HistoryMessage
		for _, msg := range searchResp.Messages.Matches {
			// Only include messages from the target channel
			if msg.Channel.ID == channelID && msg.Type == "message" && msg.User != "" && msg.Text != "" {
				historyMsg := HistoryMessage{
					Type:      msg.Type,
					User:      msg.User,
					Text:      msg.Text,
					Timestamp: msg.Timestamp,
					ThreadTS:  msg.ThreadTS,
				}
				pageMessages = append(pageMessages, historyMsg)
			}
		}

		allMessages = append(allMessages, pageMessages...)
		log.Printf("Retrieved %d messages from page %d (total so far: %d)",
			len(pageMessages), page, len(allMessages))

		// Check if we've reached the end
		if page >= searchResp.Messages.Paging.Pages {
			log.Printf("Reached final page %d of %d", page, searchResp.Messages.Paging.Pages)
			break
		}

		page++
	}

	// Sort by timestamp to ensure chronological order (search API should already return sorted, but let's be sure)
	sort.Slice(allMessages, func(i, j int) bool {
		return allMessages[i].Timestamp < allMessages[j].Timestamp
	})

	log.Printf("Search-based retrieval completed: %d total messages from channel %s", len(allMessages), channelID)
	return allMessages, nil
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
}

type SearchResponse struct {
	OK       bool `json:"ok"`
	Messages struct {
		Matches []SearchMessage `json:"matches"`
		Paging  struct {
			Count int `json:"count"`
			Total int `json:"total"`
			Page  int `json:"page"`
			Pages int `json:"pages"`
		} `json:"paging"`
	} `json:"messages"`
}

type SearchMessage struct {
	Type      string `json:"type"`
	User      string `json:"user"`
	Text      string `json:"text"`
	Timestamp string `json:"ts"`
	ThreadTS  string `json:"thread_ts,omitempty"`
	Channel   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"channel"`
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
