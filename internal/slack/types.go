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
	Type        string `json:"type"`
	Channel     string `json:"channel,omitempty"`
	User        string `json:"user,omitempty"`
	Text        string `json:"text,omitempty"`
	Timestamp   string `json:"ts,omitempty"`
	ThreadTS    string `json:"thread_ts,omitempty"`
	EventTS     string `json:"event_ts,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
	Inviter     string `json:"inviter,omitempty"`
}
