package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/jaigner-hub/openclaw-tui/internal/config"
)

// SpawnResult holds the response from sessions_spawn.
type SpawnResult struct {
	SessionID string `json:"sessionId"`
	Label     string `json:"label"`
	Model     string `json:"model"`
}

// Client talks to the OpenClaw Gateway HTTP API.
type Client struct {
	cfg    config.Config
	http   *http.Client
}

// NewClient creates an API client from the given config.
func NewClient(cfg config.Config) *Client {
	return &Client{
		cfg: cfg,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// toolRequest is the POST body for /tools/invoke.
type toolRequest struct {
	Tool string      `json:"tool"`
	Args interface{} `json:"args"`
}

// invoke calls POST /tools/invoke and returns the raw response body.
func (c *Client) invoke(req toolRequest) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.cfg.GatewayURL+"/tools/invoke", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gateway request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// FetchAgents returns the list of configured agent IDs.
func (c *Client) FetchAgents() ([]string, error) {
	out, err := exec.Command("openclaw", "agents", "list", "--json").CombinedOutput()
	if err != nil {
		// Fallback: try without --json
		out2, err2 := exec.Command("openclaw", "agents", "list").CombinedOutput()
		if err2 != nil {
			return nil, fmt.Errorf("openclaw agents list: %s", strings.TrimSpace(string(out)))
		}
		out = out2
	}

	// Try JSON parse
	var agents []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(out, &agents) == nil {
		var ids []string
		for _, a := range agents {
			if a.ID != "" {
				ids = append(ids, a.ID)
			} else if a.Name != "" {
				ids = append(ids, a.Name)
			}
		}
		return ids, nil
	}

	// Fallback: parse "- name (...)" lines from text output
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			// "- main (default)" -> "main"
			name := strings.TrimPrefix(line, "- ")
			if idx := strings.IndexByte(name, ' '); idx > 0 {
				name = name[:idx]
			}
			ids = append(ids, name)
		}
	}
	return ids, nil
}

// SpawnSession creates a new agent session via `openclaw agent` CLI.
// The agent parameter selects a pre-configured agent (which controls the model).
// If label is set, it's used as the session ID.
func (c *Client) SpawnSession(prompt, agent, label string) (*SpawnResult, error) {
	args := []string{"agent", "--message", prompt, "--json"}
	if agent != "" {
		args = append(args, "--agent", agent)
	}
	if label != "" {
		args = append(args, "--session-id", label)
	}

	cmd := exec.Command("openclaw", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("openclaw agent: %s", strings.TrimSpace(string(out)))
	}

	result := &SpawnResult{
		Label: label,
		Model: agent,
	}

	// Try to parse session ID from JSON output
	var resp struct {
		SessionID string `json:"sessionId"`
		Session   string `json:"session"`
	}
	if json.Unmarshal(out, &resp) == nil {
		if resp.SessionID != "" {
			result.SessionID = resp.SessionID
		} else if resp.Session != "" {
			result.SessionID = resp.Session
		}
	}

	return result, nil
}
