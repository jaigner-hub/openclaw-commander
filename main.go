package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jaigner-hub/openclaw-commander/internal/config"
	"github.com/jaigner-hub/openclaw-commander/internal/ui"
)

func main() {
	token := flag.String("token", "", "Gateway auth token (overrides env/config file)")
	url := flag.String("url", "", "Gateway URL (default: http://127.0.0.1:18789)")
	flag.Parse()

	cfg := config.Load(*url, *token)

	m := ui.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
