package slack

type Event struct {
	Type      string    `json:"type"`
	Challenge string    `json:"challenge,omitempty"`
	Event     EventData `json:"event,omitempty"`
	TeamID    string    `json:"team_id,omitempty"`
	APIAppID  string    `json:"api_app_id,omitempty"`
	EventID   string    `json:"event_id,omitempty"`
	EventTime int64     `json:"event_time,omitempty"`
}

type EventData struct {
	Type        string           `json:"type"`
	Channel     string           `json:"channel,omitempty"`
	User        string           `json:"user,omitempty"`
	Text        string           `json:"text,omitempty"`
	Timestamp   string           `json:"ts,omitempty"`
	ThreadTS    string           `json:"thread_ts,omitempty"`
	EventTS     string           `json:"event_ts,omitempty"`
	ChannelType string           `json:"channel_type,omitempty"`
	Inviter     string           `json:"inviter,omitempty"`
	Message     *MessageChanged  `json:"message,omitempty"`     // For message_changed events
	Subtype     string           `json:"subtype,omitempty"`     // For message subtypes
}

// MessageChanged represents the structure of a changed message in Slack
type MessageChanged struct {
	Type      string `json:"type"`
	User      string `json:"user,omitempty"`
	Text      string `json:"text,omitempty"`
	Timestamp string `json:"ts,omitempty"`
	ThreadTS  string `json:"thread_ts,omitempty"`
	Edited    *EditInfo `json:"edited,omitempty"`
}

// EditInfo contains information about when and by whom a message was edited
type EditInfo struct {
	User      string `json:"user"`
	Timestamp string `json:"ts"`
}
