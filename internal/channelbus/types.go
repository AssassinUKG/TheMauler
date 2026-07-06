package channelbus

import (
	"strings"
	"time"
)

type Lane string

const (
	LaneSideChat  Lane = "side_chat"
	LaneControl   Lane = "control"
	LaneQuick     Lane = "quick_action"
	LaneWork      Lane = "work_request"
	LaneInterrupt Lane = "interrupt"
	LaneNote      Lane = "note"
	LaneUnknown   Lane = "unknown"
)

type WorkPolicy string

const (
	WorkQueueIfBusy WorkPolicy = "queue_if_busy"
	WorkRejectBusy  WorkPolicy = "reject_busy"
	WorkStartNow    WorkPolicy = "start_now"
)

type Envelope struct {
	ID          string            `json:"id"`
	Source      string            `json:"source"`
	SessionID   string            `json:"session_id"`
	UserID      string            `json:"user_id,omitempty"`
	Username    string            `json:"username,omitempty"`
	Text        string            `json:"text"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
}

type Attachment struct {
	Kind        string `json:"kind"`
	FileID      string `json:"file_id,omitempty"`
	FileName    string `json:"file_name,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Path        string `json:"path,omitempty"`
	Text        string `json:"text,omitempty"`
}

type Route struct {
	Lane      Lane       `json:"lane"`
	Command   string     `json:"command,omitempty"`
	Argument  string     `json:"argument,omitempty"`
	Policy    WorkPolicy `json:"policy,omitempty"`
	ReadOnly  bool       `json:"read_only"`
	Reason    string     `json:"reason,omitempty"`
	Project   string     `json:"project,omitempty"`
	Mode      string     `json:"mode,omitempty"`
	Toolset   string     `json:"toolset,omitempty"`
	FromVoice bool       `json:"from_voice,omitempty"`
}

type Response struct {
	Lane       Lane              `json:"lane"`
	Status     string            `json:"status"`
	Message    string            `json:"message"`
	Queued     bool              `json:"queued,omitempty"`
	QueueID    string            `json:"queue_id,omitempty"`
	RunStarted bool              `json:"run_started,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
}

type WorkItem struct {
	ID        string   `json:"id"`
	Envelope  Envelope `json:"envelope"`
	Route     Route    `json:"route"`
	Status    string   `json:"status"`
	CreatedAt string   `json:"created_at"`
}

func NormalizeEnvelope(mut Envelope) Envelope {
	mut.Source = strings.TrimSpace(mut.Source)
	if mut.Source == "" {
		mut.Source = "unknown"
	}
	mut.SessionID = strings.TrimSpace(mut.SessionID)
	if mut.SessionID == "" {
		mut.SessionID = mut.Source + ":default"
	}
	mut.Text = strings.TrimSpace(mut.Text)
	if mut.CreatedAt == "" {
		mut.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if mut.ID == "" {
		mut.ID = "msg-" + strings.ReplaceAll(time.Now().Format(time.RFC3339Nano), ":", "-")
	}
	return mut
}
