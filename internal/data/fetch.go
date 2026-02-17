package data

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FetchSessions calls the sessions_list API tool and returns sessions.
func (c *Client) FetchSessions() ([]Session, error) {
	body, err := c.invoke(toolRequest{
		Tool: "sessions_list",
		Args: map[string]interface{}{},
	})
	if err != nil {
		return nil, err
	}

	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse sessions response: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("sessions_list: API returned ok=false")
	}

	var result SessionsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("parse sessions result: %w", err)
	}
	return result.Details.Sessions, nil
}

// FetchProcesses reads the agent-maintained process list file,
// falling back to ps scanning if the file doesn't exist.
func (c *Client) FetchProcesses() ([]Process, error) {
	// Try agent-maintained file first
	procFile := filepath.Join(homeDir(), ".openclaw", "process-list.json")
	if data, err := os.ReadFile(procFile); err == nil {
		var pf struct {
			Processes []struct {
				Name    string `json:"name"`
				Status  string `json:"status"`
				Runtime string `json:"runtime"`
				Command string `json:"command"`
			} `json:"processes"`
			UpdatedAt int64 `json:"updatedAt"`
		}
		if json.Unmarshal(data, &pf) == nil && len(pf.Processes) > 0 {
			// Check staleness — if older than 2 minutes, also scan ps
			var procs []Process
			for _, p := range pf.Processes {
				procs = append(procs, Process{
					SessionName: p.Name,
					Status:      p.Status,
					Runtime:     p.Runtime,
					Command:     p.Command,
				})
			}
			return procs, nil
		}
	}

	// Fallback: scan OS processes
	out, err := exec.Command("ps", "axo", "pid,etime,command").Output()
	if err != nil {
		return nil, nil
	}

	var procs []Process
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		isRelevant := strings.Contains(lower, "claude") ||
			strings.Contains(lower, "openclaw") ||
			strings.Contains(lower, "oclaw-tui")

		if !isRelevant {
			continue
		}

		if strings.Contains(lower, "chrome") || strings.Contains(lower, "chromium") ||
			strings.Contains(lower, "firefox") || strings.Contains(lower, "electron") ||
			strings.HasPrefix(line, "PID") || strings.Contains(line, "ps axo") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		pid := fields[0]
		etime := fields[1]
		cmd := strings.Join(fields[2:], " ")
		if len(cmd) > 60 {
			cmd = cmd[:57] + "..."
		}

		procs = append(procs, Process{
			SessionName: "pid:" + pid,
			Status:      "running",
			Runtime:     etime,
			Command:     cmd,
		})
	}

	return procs, nil
}

// parseProcessList parses the text table from the process list API.
// Each line has the format: "name status runtime :: command"
func parseProcessList(text string) []Process {
	var procs []Process
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p := parseProcessLine(line)
		if p.SessionName != "" {
			procs = append(procs, p)
		}
	}
	return procs
}

func parseProcessLine(line string) Process {
	// Format: "name status runtime :: command"
	parts := strings.SplitN(line, "::", 2)
	var cmd string
	if len(parts) == 2 {
		cmd = strings.TrimSpace(parts[1])
	}
	fields := strings.Fields(strings.TrimSpace(parts[0]))
	var p Process
	switch {
	case len(fields) >= 3:
		p.SessionName = fields[0]
		p.Status = fields[1]
		p.Runtime = fields[2]
		p.Command = cmd
	case len(fields) == 2:
		p.SessionName = fields[0]
		p.Status = fields[1]
		p.Command = cmd
	case len(fields) == 1:
		p.SessionName = fields[0]
		p.Command = cmd
	}
	return p
}

// FetchProcessLog tries the gateway API for process logs.
func (c *Client) FetchProcessLog(sessionID string, limit int) (string, error) {
	if limit <= 0 {
		limit = 100
	}
	body, err := c.invoke(toolRequest{
		Tool: "process",
		Args: map[string]interface{}{
			"action":    "log",
			"sessionId": sessionID,
			"limit":     limit,
		},
	})
	if err != nil {
		return "", fmt.Errorf("process log unavailable: %w", err)
	}

	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", nil
	}
	if !resp.OK {
		return "", nil
	}

	var result TextResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", nil
	}

	var sb strings.Builder
	for _, c := range result.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return StripANSI(sb.String()), nil
}

// FetchSessionHistory calls sessions_history for a given session key.
func (c *Client) FetchSessionHistory(sessionKey string, limit int) (string, error) {
	msgs, err := c.FetchSessionMessages(sessionKey, limit)
	if err != nil {
		return "", err
	}
	return FormatHistory(msgs, VerboseSummary), nil
}

// FetchSessionMessages returns parsed history messages.
func (c *Client) FetchSessionMessages(sessionKey string, limit int) ([]HistoryMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	body, err := c.invoke(toolRequest{
		Tool: "sessions_history",
		Args: map[string]interface{}{
			"sessionKey":   sessionKey,
			"limit":        limit,
			"includeTools": true,
		},
	})
	if err != nil {
		return nil, err
	}

	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse history response: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("sessions_history: API returned ok=false")
	}

	var outer struct {
		Details json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(resp.Result, &outer); err != nil {
		return nil, fmt.Errorf("parse history result: %w", err)
	}

	var result struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(outer.Details, &result); err != nil {
		return nil, fmt.Errorf("parse history details: %w", err)
	}

	var msgs []HistoryMessage
	for _, raw := range result.Messages {
		var base struct {
			Role     string `json:"role"`
			Model    string `json:"model,omitempty"`
			Content  []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			ToolName   string `json:"toolName,omitempty"`
			ToolCallId string `json:"toolCallId,omitempty"`
			IsError    bool   `json:"isError,omitempty"`
			Timestamp  int64  `json:"timestamp,omitempty"`
		}
		if json.Unmarshal(raw, &base) != nil {
			continue
		}

		var text strings.Builder
		for _, c := range base.Content {
			if c.Type == "text" && c.Text != "" {
				if text.Len() > 0 {
					text.WriteString("\n")
				}
				text.WriteString(c.Text)
			}
		}

		msg := HistoryMessage{
			Role:      base.Role,
			Model:     base.Model,
			Text:      text.String(),
			Timestamp: base.Timestamp,
		}

		if base.Role == "toolResult" || base.Role == "toolUse" || base.Role == "tool" {
			msg.ToolName = base.ToolName
			msg.ToolError = base.IsError
			msg.ToolArgs = extractToolArgs(raw)
		}

		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// extractToolArgs tries to get a short summary of tool arguments.
func extractToolArgs(raw json.RawMessage) string {
	var entry struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		// toolUse has args directly
		Args json.RawMessage `json:"args,omitempty"`
		// Some have input
		Input json.RawMessage `json:"input,omitempty"`
	}
	if json.Unmarshal(raw, &entry) != nil {
		return ""
	}

	// Try args first (toolUse), then input
	argsRaw := entry.Args
	if len(argsRaw) == 0 {
		argsRaw = entry.Input
	}
	if len(argsRaw) == 0 {
		return ""
	}

	var args map[string]interface{}
	if json.Unmarshal(argsRaw, &args) != nil {
		return ""
	}

	// Build a short summary from args
	var parts []string
	// Prioritize common fields
	for _, key := range []string{"command", "file_path", "path", "query", "url", "action", "tool"} {
		if v, ok := args[key]; ok {
			s := fmt.Sprintf("%v", v)
			if len(s) > 50 {
				s = s[:47] + "..."
			}
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		// Fallback: just show first value
		for _, v := range args {
			s := fmt.Sprintf("%v", v)
			if len(s) > 50 {
				s = s[:47] + "..."
			}
			parts = append(parts, s)
			break
		}
	}
	return strings.Join(parts, " ")
}

// toolEmoji returns an emoji for a tool name.
func toolEmoji(name string) string {
	switch strings.ToLower(name) {
	case "read", "file_read":
		return "📖"
	case "write", "file_write":
		return "✍️"
	case "edit", "file_edit":
		return "✏️"
	case "exec", "bash", "shell":
		return "🛠️"
	case "web_search", "search":
		return "🔎"
	case "web_fetch", "fetch":
		return "🌐"
	case "browser":
		return "🖥️"
	case "message":
		return "💬"
	case "image":
		return "🖼️"
	case "tts":
		return "🔊"
	case "process":
		return "⚙️"
	case "nodes":
		return "📱"
	case "canvas":
		return "🎨"
	default:
		return "🔧"
	}
}

// FormatHistory renders messages according to the verbose level.
func FormatHistory(msgs []HistoryMessage, verbose VerboseLevel) string {
	var sb strings.Builder
	for _, msg := range msgs {
		switch msg.Role {
		case "toolResult", "toolUse", "tool":
			switch verbose {
			case VerboseOff:
				continue
			case VerboseSummary:
				name := msg.ToolName
				if name == "" {
					name = "tool"
				}
				emoji := toolEmoji(name)
				status := "✓"
				if msg.ToolError {
					status = "✗"
				}
				if msg.Role == "toolUse" {
					// toolUse is the call, show args
					line := fmt.Sprintf(" %s %s %s", emoji, name, msg.ToolArgs)
					sb.WriteString(line + "\n")
					continue
				}
				// toolResult — show status
				line := fmt.Sprintf(" %s %s %s  %s", status, emoji, name, msg.ToolArgs)
				sb.WriteString(line + "\n")
				if msg.ToolError && msg.Text != "" {
					// Auto-expand: show first 6 lines of error
					errLines := strings.Split(msg.Text, "\n")
					limit := 6
					if len(errLines) < limit {
						limit = len(errLines)
					}
					for _, el := range errLines[:limit] {
						sb.WriteString("   " + el + "\n")
					}
					if len(errLines) > 6 {
						sb.WriteString("   …\n")
					}
				}
			case VerboseFull:
				role := strings.ToUpper(msg.Role)
				name := msg.ToolName
				if name != "" {
					role = role + " (" + name + ")"
				}
				sb.WriteString(fmt.Sprintf("─── %s ───\n", role))
				if msg.Text != "" {
					sb.WriteString(msg.Text + "\n")
				}
				sb.WriteString("\n")
			}
		default:
			role := strings.ToUpper(msg.Role)
			sb.WriteString(fmt.Sprintf("─── %s ", role))
			if msg.Model != "" {
				sb.WriteString(fmt.Sprintf("(%s) ", msg.Model))
			}
			sb.WriteString("───\n")
			if msg.Text != "" {
				sb.WriteString(msg.Text + "\n")
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// SendMessage sends a message to a session via `openclaw agent`.
func (c *Client) SendMessage(sessionID, message string) (string, error) {
	out, err := exec.Command("openclaw", "agent",
		"--session-id", sessionID,
		"--message", message,
		"--json").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("openclaw agent: %s", string(out))
	}
	return string(out), nil
}

// FetchArchivedRuns finds transcript files that aren't in the active sessions list.
// These are typically completed/cleaned-up sub-agent runs.
func (c *Client) FetchArchivedRuns(activeSessions []Session) ([]ArchivedRun, error) {
	sessDir := filepath.Join(homeDir(), ".openclaw", "agents", "main", "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil, nil // graceful if dir doesn't exist
	}

	// Build set of active session IDs
	activeIDs := make(map[string]bool)
	for _, s := range activeSessions {
		activeIDs[s.SessionID] = true
	}

	var runs []ArchivedRun
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(e.Name(), ".jsonl")
		if activeIDs[sessionID] {
			continue // skip active sessions
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		// Try to read first line to get a label
		label := readTranscriptLabel(filepath.Join(sessDir, e.Name()))

		runs = append(runs, ArchivedRun{
			SessionID:  sessionID,
			Label:      label,
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UnixMilli(),
			Path:       filepath.Join(sessDir, e.Name()),
		})
	}

	// Sort by modified time, newest first
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].ModifiedAt > runs[j].ModifiedAt
	})

	return runs, nil
}

// readTranscriptLabel reads the first user message from a transcript to use as a label.
func readTranscriptLabel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}

		role := entry.Message.Role
		content := entry.Message.Content
		if role == "" {
			role = entry.Role
			content = entry.Content
		}

		if role == "user" {
			for _, c := range content {
				if c.Type == "text" && c.Text != "" {
					text := c.Text
					if idx := strings.IndexByte(text, '\n'); idx > 0 {
						text = text[:idx]
					}
					if len(text) > 60 {
						text = text[:57] + "..."
					}
					return text
				}
			}
		}
	}
	return ""
}

// ReadTranscript reads a full transcript file and formats it for display.
func (c *Client) ReadTranscript(path string) (string, error) {
	return c.ReadTranscriptVerbose(path, VerboseSummary)
}

// ReadTranscriptVerbose reads a transcript with the given verbose level.
func (c *Client) ReadTranscriptVerbose(path string, verbose VerboseLevel) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var msgs []HistoryMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				ToolName string `json:"toolName,omitempty"`
				IsError  bool   `json:"isError,omitempty"`
			} `json:"message"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Model    string `json:"model,omitempty"`
			ToolName string `json:"toolName,omitempty"`
			IsError  bool   `json:"isError,omitempty"`
		}
		if json.Unmarshal(line, &entry) != nil {
			continue
		}

		role := entry.Message.Role
		content := entry.Message.Content
		toolName := entry.Message.ToolName
		isError := entry.Message.IsError
		if role == "" {
			role = entry.Role
			content = entry.Content
			toolName = entry.ToolName
			isError = entry.IsError
		}

		if role == "" || (entry.Type != "" && entry.Type != "message") {
			continue
		}

		var text strings.Builder
		for _, c := range content {
			if c.Type == "text" && c.Text != "" {
				if text.Len() > 0 {
					text.WriteString("\n")
				}
				text.WriteString(c.Text)
			}
		}

		msg := HistoryMessage{
			Role:  role,
			Model: entry.Model,
			Text:  text.String(),
		}

		if role == "toolResult" || role == "toolUse" || role == "tool" {
			msg.ToolName = toolName
			msg.ToolError = isError
			msg.ToolArgs = extractToolArgs(line)
		}

		msgs = append(msgs, msg)
	}
	return FormatHistory(msgs, verbose), nil
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// FetchGatewayHealth does a simple GET to the gateway root to check connectivity.
func (c *Client) FetchGatewayHealth() (*GatewayHealth, error) {
	start := time.Now()
	resp, err := c.http.Get(c.cfg.GatewayURL + "/health")
	dur := time.Since(start)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	h := &GatewayHealth{
		OK:         resp.StatusCode == http.StatusOK,
		DurationMs: int(dur.Milliseconds()),
		Ts:         time.Now().UnixMilli(),
	}
	return h, nil
}
