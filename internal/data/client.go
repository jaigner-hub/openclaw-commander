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
