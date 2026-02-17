package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const DefaultGatewayURL = "http://127.0.0.1:18789"

// Config holds the gateway connection settings.
type Config struct {
	GatewayURL string
	Token      string
}

// openclawJSON mirrors the relevant fields of ~/.openclaw/openclaw.json.
type openclawJSON struct {
	Gateway struct {
		Auth struct {
			Token string `json:"token"`
		} `json:"auth"`
	} `json:"gateway"`
}

// Load builds a Config by merging sources (lowest to highest priority):
//  1. ~/.openclaw/openclaw.json  gateway.auth.token
//  2. OPENCLAW_GATEWAY_TOKEN env var
//  3. Explicit flag values (passed as arguments)
func Load(flagURL, flagToken string) Config {
	cfg := Config{GatewayURL: DefaultGatewayURL}

	// 1. Config file
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".openclaw", "openclaw.json")
		if data, err := os.ReadFile(p); err == nil {
			var f openclawJSON
			if json.Unmarshal(data, &f) == nil && f.Gateway.Auth.Token != "" {
				cfg.Token = f.Gateway.Auth.Token
			}
		}
	}

	// 2. Env var overrides file
	if v := os.Getenv("OPENCLAW_GATEWAY_TOKEN"); v != "" {
		cfg.Token = v
	}

	// 3. CLI flags override everything
	if flagToken != "" {
		cfg.Token = flagToken
	}
	if flagURL != "" {
		cfg.GatewayURL = flagURL
	}

	return cfg
}
