package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

// ModelOption represents a configured model with optional alias.
type ModelOption struct {
	ID    string
	Alias string
}

// FetchConfiguredModels reads the model config from openclaw.json and returns
// the primary model, fallbacks, and any additional models in the models map.
func (c *Client) FetchConfiguredModels() ([]ModelOption, error) {
	home := homeDir()
	data, err := os.ReadFile(filepath.Join(home, ".openclaw", "openclaw.json"))
	if err != nil {
		return nil, err
	}

	var cfg struct {
		Agents struct {
			Defaults struct {
				Model struct {
					Primary   string   `json:"primary"`
					Fallbacks []string `json:"fallbacks"`
				} `json:"model"`
				Models map[string]struct {
					Alias string `json:"alias"`
				} `json:"models"`
			} `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var opts []ModelOption

	// Primary first
	if p := cfg.Agents.Defaults.Model.Primary; p != "" {
		alias := ""
		if m, ok := cfg.Agents.Defaults.Models[p]; ok && m.Alias != "" {
			alias = m.Alias
		}
		opts = append(opts, ModelOption{ID: p, Alias: alias})
		seen[p] = true
	}

	// Then fallbacks
	for _, fb := range cfg.Agents.Defaults.Model.Fallbacks {
		if seen[fb] {
			continue
		}
		alias := ""
		if m, ok := cfg.Agents.Defaults.Models[fb]; ok && m.Alias != "" {
			alias = m.Alias
		}
		opts = append(opts, ModelOption{ID: fb, Alias: alias})
		seen[fb] = true
	}

	// Then any remaining models in the map
	for id, m := range cfg.Agents.Defaults.Models {
		if seen[id] {
			continue
		}
		opts = append(opts, ModelOption{ID: id, Alias: m.Alias})
		seen[id] = true
	}

	return opts, nil
}

// SpawnSession creates a new agent session via `openclaw agent` CLI.
// Model is accepted for display/future use but the CLI currently uses the
// agent's configured primary model.
func (c *Client) SpawnSession(prompt, model, label string) (*SpawnResult, error) {
	args := []string{"agent", "--message", prompt, "--json"}
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
		Model: model,
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
