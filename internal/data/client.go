package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// SpawnSession creates a new agent session via sessions_spawn.
func (c *Client) SpawnSession(prompt, model, label string) (*SpawnResult, error) {
	args := map[string]interface{}{
		"prompt": prompt,
	}
	if model != "" {
		args["model"] = model
	}
	if label != "" {
		args["label"] = label
	}

	body, err := c.invoke(toolRequest{
		Tool: "sessions_spawn",
		Args: args,
	})
	if err != nil {
		return nil, err
	}

	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse spawn response: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("sessions_spawn: API returned ok=false")
	}

	// Try to extract session info from result
	var result struct {
		Details *SpawnResult `json:"details"`
	}
	if err := json.Unmarshal(resp.Result, &result); err == nil && result.Details != nil {
		return result.Details, nil
	}

	// Fallback: return minimal result
	return &SpawnResult{}, nil
}
