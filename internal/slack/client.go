package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	token      string
	httpClient *http.Client
}

type UserInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
		token:      token,
		httpClient: &http.Client{},
	}
}

func (c *Client) GetUserInfo(userID string) (*UserInfo, error) {
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

	return &userResp.User, nil
}

func (c *Client) GetChannelInfo(channelID string) (*ChannelInfo, error) {
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
	OK       bool            `json:"ok"`
	Messages []HistoryMessage `json:"messages"`
	HasMore  bool            `json:"has_more"`
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
