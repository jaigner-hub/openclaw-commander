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

// FetchProcesses scans for running openclaw-related processes via ps.
func (c *Client) FetchProcesses() ([]Process, error) {
	// Find claude, openclaw agent, and other relevant child processes
	out, err := exec.Command("ps", "axo", "pid,etime,command").Output()
	if err != nil {
		return nil, nil
	}

	var procs []Process
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		// Match relevant processes
		isRelevant := strings.Contains(lower, "claude") ||
			strings.Contains(lower, "openclaw") ||
			strings.Contains(lower, "oclaw-tui")

		if !isRelevant {
			continue
		}

		// Skip browser tabs, header, grep, ps itself
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

		// Shorten command for display
		if len(cmd) > 60 {
			cmd = cmd[:57] + "..."
		}

		status := "running"
		name := "pid:" + pid

		procs = append(procs, Process{
			SessionName: name,
			Status:      status,
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
		return "", err
	}

	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse history response: %w", err)
	}
	if !resp.OK {
		return "", fmt.Errorf("sessions_history: API returned ok=false")
	}

	// Result has a "details" object with the parsed data
	var outer struct {
		Details json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(resp.Result, &outer); err != nil {
		return "", fmt.Errorf("parse history result: %w", err)
	}

	var result struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Model     string `json:"model,omitempty"`
			Timestamp int64  `json:"timestamp,omitempty"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(outer.Details, &result); err != nil {
		return "", fmt.Errorf("parse history details: %w", err)
	}

	var sb strings.Builder
	for _, msg := range result.Messages {
		role := strings.ToUpper(msg.Role)
		sb.WriteString(fmt.Sprintf("─── %s ", role))
		if msg.Model != "" {
			sb.WriteString(fmt.Sprintf("(%s) ", msg.Model))
		}
		sb.WriteString("───\n")
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				sb.WriteString(c.Text)
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
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
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
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
			// Also support flat role/content (API responses format)
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Model string `json:"model,omitempty"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}

		// Determine role and content - try nested .message first, then flat
		role := entry.Message.Role
		content := entry.Message.Content
		if role == "" {
			role = entry.Role
			content = entry.Content
		}

		// Skip non-message entries (session, model_change, custom, etc.)
		if role == "" || (entry.Type != "" && entry.Type != "message") {
			continue
		}

		hasText := false
		for _, c := range content {
			if c.Type == "text" && c.Text != "" {
				hasText = true
				break
			}
		}
		if !hasText {
			continue
		}

		sb.WriteString(fmt.Sprintf("─── %s ", strings.ToUpper(role)))
		if entry.Model != "" {
			sb.WriteString(fmt.Sprintf("(%s) ", entry.Model))
		}
		sb.WriteString("───\n")
		for _, c := range content {
			if c.Type == "text" && c.Text != "" {
				sb.WriteString(c.Text)
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
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
