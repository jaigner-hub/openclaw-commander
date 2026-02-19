package data

import "encoding/json"

// Session represents an OpenClaw agent session.
type Session struct {
	Key            string `json:"key"`
	Kind           string `json:"kind"`
	Channel        string `json:"channel"`
	DisplayName    string `json:"displayName"`
	Label          string `json:"label"`
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
	Status         string `json:"status"`
	ErrorMessage   string `json:"errorMessage"`
}

// ModelAlias returns a short alias for a model name.
func ModelAlias(model string) string {
	aliases := map[string]string{
		"claude-opus-4-6":           "opus",
		"claude-opus-4":             "opus",
		"claude-sonnet-4":           "sonnet",
		"claude-3-5-sonnet":         "sonnet-3.5",
		"claude-3-5-haiku":          "haiku-3.5",
		"claude-3-haiku":            "haiku",
		"kimi-coding/k2p5":          "k2p5",
		"gpt-4o":                    "4o",
		"gpt-4o-mini":               "4o-mini",
		"o1":                        "o1",
		"o1-mini":                   "o1-mini",
		"o3":                        "o3",
		"o3-mini":                   "o3-mini",
		"gemini-2.5-pro":            "gem-pro",
		"gemini-2.5-flash":          "gem-flash",
		"deepseek-chat":             "ds-chat",
		"deepseek-reasoner":         "ds-r1",
	}
	// Exact match
	if a, ok := aliases[model]; ok {
		return a
	}
	// Try partial match (model string may have provider prefix)
	for k, v := range aliases {
		if len(k) > 5 && (len(model) > len(k)) {
			// Check if model ends with the key
			if model[len(model)-len(k):] == k {
				return v
			}
		}
		// Check if model contains the key
		if len(k) > 8 && contains(model, k) {
			return v
		}
	}
	// Fallback: last segment, truncated
	parts := splitAny(model, "/:")
	short := parts[len(parts)-1]
	if len(short) > 12 {
		short = short[:12]
	}
	return short
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr) >= 0
}

func searchString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func splitAny(s string, delims string) []string {
	f := func(c rune) bool {
		for _, d := range delims {
			if c == d {
				return true
			}
		}
		return false
	}
	parts := []string{}
	current := ""
	for _, c := range s {
		if f(c) {
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	if len(parts) == 0 {
		return []string{s}
	}
	return parts
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
