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
	Type        string          `json:"type"`
	Channel     string          `json:"channel,omitempty"`
	User        string          `json:"user,omitempty"`
	Text        string          `json:"text,omitempty"`
	Timestamp   string          `json:"ts,omitempty"`
	ThreadTS    string          `json:"thread_ts,omitempty"`
	EventTS     string          `json:"event_ts,omitempty"`
	ChannelType string          `json:"channel_type,omitempty"`
	Inviter     string          `json:"inviter,omitempty"`
	Message     *MessageChanged `json:"message,omitempty"`     // For message_changed events
	Subtype     string          `json:"subtype,omitempty"`     // For message subtypes
	Attachments []Attachment    `json:"attachments,omitempty"` // Message attachments
	Files       []FileInfo      `json:"files,omitempty"`       // File attachments
}

// MessageChanged represents the structure of a changed message in Slack
type MessageChanged struct {
	Type        string       `json:"type"`
	User        string       `json:"user,omitempty"`
	Text        string       `json:"text,omitempty"`
	Timestamp   string       `json:"ts,omitempty"`
	ThreadTS    string       `json:"thread_ts,omitempty"`
	Edited      *EditInfo    `json:"edited,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Files       []FileInfo   `json:"files,omitempty"`
}

// EditInfo contains information about when and by whom a message was edited
type EditInfo struct {
	User      string `json:"user"`
	Timestamp string `json:"ts"`
}

// Attachment represents a Slack message attachment (rich text, links, etc.)
type Attachment struct {
	ID         int64             `json:"id,omitempty"`
	Fallback   string            `json:"fallback,omitempty"`   // Plain text fallback
	Text       string            `json:"text,omitempty"`       // Main text content
	Pretext    string            `json:"pretext,omitempty"`    // Text before the attachment
	Title      string            `json:"title,omitempty"`      // Title of the attachment
	TitleLink  string            `json:"title_link,omitempty"` // URL for the title
	AuthorName string            `json:"author_name,omitempty"`
	AuthorIcon string            `json:"author_icon,omitempty"`
	AuthorLink string            `json:"author_link,omitempty"`
	Color      string            `json:"color,omitempty"` // Color bar color
	Fields     []AttachmentField `json:"fields,omitempty"`
	ImageURL   string            `json:"image_url,omitempty"`
	ThumbURL   string            `json:"thumb_url,omitempty"`
	Footer     string            `json:"footer,omitempty"`
	FooterIcon string            `json:"footer_icon,omitempty"`
	Timestamp  int64             `json:"ts,omitempty"`
}

// AttachmentField represents a field within an attachment
type AttachmentField struct {
	Title string `json:"title,omitempty"`
	Value string `json:"value,omitempty"`
	Short bool   `json:"short,omitempty"`
}

// FileInfo represents a file attachment in Slack
type FileInfo struct {
	ID                 string `json:"id,omitempty"`
	Name               string `json:"name,omitempty"`
	Title              string `json:"title,omitempty"`
	Mimetype           string `json:"mimetype,omitempty"`
	Filetype           string `json:"filetype,omitempty"`
	PrettyType         string `json:"pretty_type,omitempty"`
	User               string `json:"user,omitempty"`
	Mode               string `json:"mode,omitempty"`
	Editable           bool   `json:"editable,omitempty"`
	IsExternal         bool   `json:"is_external,omitempty"`
	ExternalType       string `json:"external_type,omitempty"`
	Size               int    `json:"size,omitempty"`
	URL                string `json:"url,omitempty"`          // Private download URL
	URLDownload        string `json:"url_download,omitempty"` // Direct download URL
	URLPrivate         string `json:"url_private,omitempty"`  // Private view URL
	URLPrivateDownload string `json:"url_private_download,omitempty"`
	Permalink          string `json:"permalink,omitempty"`         // Public permalink
	PermalinkPublic    string `json:"permalink_public,omitempty"`  // Public permalink
	Preview            string `json:"preview,omitempty"`           // Text preview for text files
	PreviewHighlight   string `json:"preview_highlight,omitempty"` // Highlighted preview
	Lines              int    `json:"lines,omitempty"`
	LinesMore          int    `json:"lines_more,omitempty"`
}
