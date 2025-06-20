package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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

func (c *Client) GetUserInfo(userID string) (*UserInfo, error) {
	// Check cache first
	if user, exists := c.userCache[userID]; exists {
		return user, nil
	}

	// Rate limiting: small delay between API calls
	time.Sleep(100 * time.Millisecond)

	url := fmt.Sprintf("https://slack.com/api/users.info?user=%s", userID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var userResp UserResponse
	if err := json.Unmarshal(body, &userResp); err != nil {
		return nil, err
	}

	if !userResp.OK {
		return nil, fmt.Errorf("slack API error: %s", string(body))
	}

	// Cache the result
	c.userCache[userID] = &userResp.User

	return &userResp.User, nil
}

func (c *Client) GetChannelInfo(channelID string) (*ChannelInfo, error) {
	// Check cache first
	if channel, exists := c.channelCache[channelID]; exists {
		return channel, nil
	}

	// Rate limiting: small delay between API calls
	time.Sleep(100 * time.Millisecond)

	url := fmt.Sprintf("https://slack.com/api/conversations.info?channel=%s", channelID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var channelResp ChannelResponse
	if err := json.Unmarshal(body, &channelResp); err != nil {
		return nil, err
	}

	if !channelResp.OK {
		return nil, fmt.Errorf("slack API error: %s", string(body))
	}

	// Cache the result
	c.channelCache[channelID] = &channelResp.Channel

	return &channelResp.Channel, nil
}

func (c *Client) SendMessage(channel, text string) error {
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
}

type HistoryResponse struct {
	OK       bool             `json:"ok"`
	Messages []HistoryMessage `json:"messages"`
	HasMore  bool             `json:"has_more"`
}

type HistoryMessage struct {
	Type      string `json:"type"`
	User      string `json:"user"`
	Text      string `json:"text"`
	Timestamp string `json:"ts"`
	ThreadTS  string `json:"thread_ts,omitempty"`
}

func (c *Client) GetChannelHistory(channelID string, limit int) ([]HistoryMessage, error) {
	url := fmt.Sprintf("https://slack.com/api/conversations.history?channel=%s&limit=%d", channelID, limit)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var historyResp HistoryResponse
	if err := json.Unmarshal(body, &historyResp); err != nil {
		return nil, err
	}

	if !historyResp.OK {
		return nil, fmt.Errorf("slack API error: %s", string(body))
	}

	return historyResp.Messages, nil
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
