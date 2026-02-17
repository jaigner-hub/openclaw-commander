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
