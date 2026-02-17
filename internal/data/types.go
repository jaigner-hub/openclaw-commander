package data

import "encoding/json"

// Session represents an OpenClaw agent session.
type Session struct {
	Key            string `json:"key"`
	Kind           string `json:"kind"`
	Channel        string `json:"channel"`
	DisplayName    string `json:"displayName"`
	Model          string `json:"model"`
	UpdatedAt      int64  `json:"updatedAt"`
	AgeMs          int64  `json:"ageMs"`
	SessionID      string `json:"sessionId"`
	InputTokens    int    `json:"inputTokens"`
	OutputTokens   int    `json:"outputTokens"`
	TotalTokens    int    `json:"totalTokens"`
	ContextTokens  int    `json:"contextTokens"`
	TranscriptPath string `json:"transcriptPath"`
	SystemSent     bool   `json:"systemSent"`
	AbortedLastRun bool   `json:"abortedLastRun"`
}

// SessionsResponse is the wrapper from `openclaw sessions --json` (CLI fallback).
type SessionsResponse struct {
	Path     string    `json:"path"`
	Count    int       `json:"count"`
	Sessions []Session `json:"sessions"`
}

// Process represents a running openclaw exec process.
type Process struct {
	SessionName string
	Status      string
	Runtime     string
	Command     string
}

// GatewayHealth represents the gateway health check response.
type GatewayHealth struct {
	OK         bool  `json:"ok"`
	DurationMs int   `json:"durationMs"`
	Ts         int64 `json:"ts"`
}

// --- API response types for /tools/invoke ---

// APIResponse is the top-level envelope from the gateway.
type APIResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
}

// ContentItem is a single content block inside a tool result.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// TextResult is the "result" shape for tools that return content[].text.
type TextResult struct {
	Content []ContentItem `json:"content"`
}

// SessionsListResult is the "result" shape for sessions_list.
type SessionsListResult struct {
	Details struct {
		Sessions []Session `json:"sessions"`
	} `json:"details"`
}

// VerboseLevel controls tool display detail.
type VerboseLevel int

const (
	VerboseSummary VerboseLevel = iota // one-line per tool (default)
	VerboseFull                        // show full tool output
	VerboseOff                         // hide tools entirely
)

func (v VerboseLevel) String() string {
	switch v {
	case VerboseOff:
		return "off"
	case VerboseSummary:
		return "summary"
	case VerboseFull:
		return "full"
	default:
		return "summary"
	}
}

func (v VerboseLevel) Next() VerboseLevel {
	return (v + 1) % 3
}

// HistoryMessage is a parsed message from session history.
type HistoryMessage struct {
	Role      string
	Model     string
	Text      string // for user/assistant
	ToolName  string // for toolUse/toolResult
	ToolArgs  string // summary of tool args
	ToolError bool   // true if tool failed
	Timestamp int64
}

// ArchivedRun represents a completed sub-agent run with a transcript on disk.
type ArchivedRun struct {
	SessionID  string
	Label      string
	Size       int64
	ModifiedAt int64
	Path       string
}
