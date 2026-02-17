package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Base colors
	colorBg        = lipgloss.Color("#1a1b26")
	colorFg        = lipgloss.Color("#a9b1d6")
	colorBorder    = lipgloss.Color("#3b4261")
	colorAccent    = lipgloss.Color("#7aa2f7")
	colorGreen     = lipgloss.Color("#9ece6a")
	colorYellow    = lipgloss.Color("#e0af68")
	colorRed       = lipgloss.Color("#f7768e")
	colorDim       = lipgloss.Color("#565f89")
	colorTitle     = lipgloss.Color("#c0caf5")
	colorStatusBar = lipgloss.Color("#24283b")

	// Panel styles
	panelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	activePanelBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorAccent)

	// Text styles
	titleStyle = lipgloss.NewStyle().
			Foreground(colorTitle).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	accentStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	queryStyle = lipgloss.NewStyle().
			Foreground(colorYellow).
			Italic(true)

	// Status colors
	statusRunning  = lipgloss.NewStyle().Foreground(colorGreen)
	statusThinking = lipgloss.NewStyle().Foreground(colorYellow)
	statusFailed   = lipgloss.NewStyle().Foreground(colorRed)
	statusIdle     = lipgloss.NewStyle().Foreground(colorDim)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Background(colorStatusBar).
			Foreground(colorFg).
			Padding(0, 1)

	// Selected item
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#3b82f6")).
			Bold(true)

	// Tab styles
	activeTabStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			Underline(true).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Padding(0, 1)
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "running", "active", "warm":
		return statusRunning
	case "thinking", "working":
		return statusThinking
	case "failed", "error":
		return statusFailed
	case "completed", "done", "idle":
		return statusIdle
	default:
		return dimStyle
	}
}

func statusIndicator(status string) string {
	switch status {
	case "running", "active", "warm":
		return statusRunning.Render("\u25cf")
	case "thinking", "working":
		return statusThinking.Render("\u25cf")
	case "failed", "error":
		return statusFailed.Render("\u25cf")
	default:
		return dimStyle.Render("\u25cb")
	}
}

func processIndicator(status string) string {
	switch status {
	case "running", "active":
		return statusRunning.Render("\u25b6")
	case "failed", "error":
		return statusFailed.Render("\u25a0")
	default:
		return dimStyle.Render("\u25a0")
	}
}
