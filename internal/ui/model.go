package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jaigner-hub/openclaw-tui/internal/config"
	"github.com/jaigner-hub/openclaw-tui/internal/data"
)

const (
	tabSessions  = 0
	tabProcesses = 1

	panelList = 0
	panelLogs = 1
)

// Tick messages for auto-refresh
type tickSessionsMsg struct{}
type tickProcessesMsg struct{}
type tickLogsMsg struct{}
type tickHealthMsg struct{}

// Data messages
type sessionsMsg struct{ sessions []data.Session }
type processesMsg struct{ processes []data.Process }
type logsMsg struct{ content string }
type healthMsg struct{ health *data.GatewayHealth }
type errMsg struct{ err error }
type agentReplyMsg struct{ reply string }
type agentSendingMsg struct{}

// Model is the main Bubble Tea model.
type Model struct {
	width  int
	height int

	activeTab   int // 0=sessions, 1=processes
	activePanel int // 0=list, 1=logs

	sessions  []data.Session
	processes []data.Process
	health    *data.GatewayHealth

	sessionCursor int
	processCursor int
	logContent    string
	logFollow     bool
	logScrollPos  int
	selectedLogID  string
	selectedLogTab int // which tab the selected log came from

	// Search/filter
	searching   bool
	searchInput textinput.Model
	filter      string

	// Kill confirmation
	confirming    bool
	confirmTarget string

	// Message input
	messaging    bool
	msgInput     textinput.Model
	msgTarget    string // session ID to message
	msgTargetName string // display name for the target
	sending      bool   // true while waiting for agent reply

	lastError string

	client *data.Client
}

func NewModel(cfg config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 64

	mi := textinput.New()
	mi.Placeholder = "message..."
	mi.CharLimit = 1024
	mi.Width = 60

	return Model{
		logFollow:   true,
		searchInput: ti,
		msgInput:    mi,
		client:      data.NewClient(cfg),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchSessions,
		m.fetchProcesses,
		m.fetchHealth,
		tickSessions(),
		tickProcesses(),
		tickHealth(),
	)
}

// Commands that fetch data
func (m Model) fetchSessions() tea.Msg {
	s, err := m.client.FetchSessions()
	if err != nil {
		return errMsg{fmt.Errorf("sessions: %w", err)}
	}
	return sessionsMsg{s}
}

func (m Model) fetchProcesses() tea.Msg {
	p, err := m.client.FetchProcesses()
	if err != nil {
		return errMsg{fmt.Errorf("processes: %w", err)}
	}
	return processesMsg{p}
}

func (m Model) fetchHealth() tea.Msg {
	h, err := m.client.FetchGatewayHealth()
	if err != nil {
		return errMsg{err}
	}
	return healthMsg{h}
}

func (m Model) fetchLogs(id string) tea.Cmd {
	isSession := m.selectedLogTab == tabSessions
	return func() tea.Msg {
		var content string
		var err error
		if isSession {
			content, err = m.client.FetchSessionHistory(id, 200)
		} else {
			content, err = m.client.FetchProcessLog(id, 200)
		}
		if err != nil {
			kind := "process-log"
			if isSession {
				kind = "session-history"
			}
			return errMsg{fmt.Errorf("%s(%s): %w", kind, id, err)}
		}
		return logsMsg{content}
	}
}

// Tick commands for periodic refresh
func tickSessions() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return tickSessionsMsg{}
	})
}

func tickProcesses() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return tickProcessesMsg{}
	})
}

func tickLogs() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return tickLogsMsg{}
	})
}

func tickHealth() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
		return tickHealthMsg{}
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case sessionsMsg:
		m.sessions = msg.sessions
		m.lastError = ""
		return m, nil

	case processesMsg:
		m.processes = msg.processes
		m.lastError = ""
		return m, nil

	case logsMsg:
		m.logContent = msg.content
		if m.logFollow {
			lines := strings.Split(msg.content, "\n")
			m.logScrollPos = max(0, len(lines)-m.logViewHeight())
		}
		return m, nil

	case healthMsg:
		m.health = msg.health
		m.lastError = ""
		return m, nil

	case agentReplyMsg:
		m.sending = false
		// Append reply to log content and refresh
		m.logContent += "\n─── SENT ───\n" + msg.reply + "\n"
		if m.logFollow {
			lines := strings.Split(m.logContent, "\n")
			m.logScrollPos = max(0, len(lines)-m.logViewHeight())
		}
		// Refresh the session history
		if m.selectedLogID != "" {
			return m, m.fetchLogs(m.selectedLogID)
		}
		return m, nil

	case errMsg:
		m.sending = false
		m.lastError = msg.err.Error()
		return m, nil

	case tickSessionsMsg:
		return m, tea.Batch(m.fetchSessions, tickSessions())

	case tickProcessesMsg:
		return m, tea.Batch(m.fetchProcesses, tickProcesses())

	case tickLogsMsg:
		if m.selectedLogID != "" {
			return m, tea.Batch(m.fetchLogs(m.selectedLogID), tickLogs())
		}
		return m, nil

	case tickHealthMsg:
		return m, tea.Batch(m.fetchHealth, tickHealth())
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle search input mode
	if m.searching {
		switch {
		case key.Matches(msg, keys.Escape):
			m.searching = false
			m.filter = ""
			m.searchInput.SetValue("")
			return m, nil
		case key.Matches(msg, keys.Enter):
			m.searching = false
			m.filter = m.searchInput.Value()
			return m, nil
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.filter = m.searchInput.Value()
			return m, cmd
		}
	}

	// Handle message input mode
	if m.messaging {
		switch {
		case key.Matches(msg, keys.Escape):
			m.messaging = false
			m.msgInput.SetValue("")
			return m, nil
		case key.Matches(msg, keys.Enter):
			text := m.msgInput.Value()
			if text == "" {
				m.messaging = false
				return m, nil
			}
			m.messaging = false
			m.sending = true
			m.msgInput.SetValue("")
			sessionID := m.msgTarget
			return m, func() tea.Msg {
				reply, err := m.client.SendMessage(sessionID, text)
				if err != nil {
					return errMsg{fmt.Errorf("send: %w", err)}
				}
				return agentReplyMsg{reply}
			}
		default:
			var cmd tea.Cmd
			m.msgInput, cmd = m.msgInput.Update(msg)
			return m, cmd
		}
	}

	// Handle confirmation mode
	if m.confirming {
		switch {
		case key.Matches(msg, keys.ConfirmY):
			m.confirming = false
			target := m.confirmTarget
			m.confirmTarget = ""
			return m, killProcess(target)
		case key.Matches(msg, keys.ConfirmN), key.Matches(msg, keys.Escape):
			m.confirming = false
			m.confirmTarget = ""
			return m, nil
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Up):
		if m.activePanel == panelList {
			m.moveCursor(-1)
		} else {
			m.logScrollPos = max(0, m.logScrollPos-1)
			m.logFollow = false
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.activePanel == panelList {
			m.moveCursor(1)
		} else {
			m.logScrollPos++
			m.logFollow = false
		}
		return m, nil

	case key.Matches(msg, keys.Tab):
		m.activePanel = (m.activePanel + 1) % 2
		return m, nil

	case key.Matches(msg, keys.Tab1):
		m.activeTab = tabSessions
		return m, nil

	case key.Matches(msg, keys.Tab2):
		m.activeTab = tabProcesses
		return m, nil

	case key.Matches(msg, keys.Enter):
		id := m.selectedItemID()
		if id != "" {
			m.selectedLogID = id
			m.selectedLogTab = m.activeTab
			m.activePanel = panelLogs
			return m, tea.Batch(m.fetchLogs(id), tickLogs())
		}
		return m, nil

	case key.Matches(msg, keys.Kill):
		id := m.selectedItemID()
		if id != "" && m.activeTab == tabProcesses {
			m.confirming = true
			m.confirmTarget = id
		}
		return m, nil

	case key.Matches(msg, keys.Search):
		m.searching = true
		m.searchInput.Focus()
		return m, textinput.Blink

	case key.Matches(msg, keys.Follow):
		m.logFollow = !m.logFollow
		if m.logFollow && m.logContent != "" {
			lines := strings.Split(m.logContent, "\n")
			m.logScrollPos = max(0, len(lines)-m.logViewHeight())
		}
		return m, nil

	case key.Matches(msg, keys.Message):
		if m.activeTab == tabSessions {
			ss := m.filteredSessions()
			if m.sessionCursor < len(ss) {
				s := ss[m.sessionCursor]
				m.msgTarget = s.SessionID
				m.msgTargetName = s.DisplayName
				if m.msgTargetName == "" {
					m.msgTargetName = s.Key
				}
				m.messaging = true
				m.msgInput.Focus()
				return m, textinput.Blink
			}
		}
		return m, nil
	}

	return m, nil
}

func killProcess(sessionID string) tea.Cmd {
	return func() tea.Msg {
		// placeholder — actual kill would use a different API call
		return tickProcessesMsg{}
	}
}

func (m *Model) moveCursor(delta int) {
	listLen := m.filteredListLen()
	if listLen == 0 {
		return
	}
	cursor := m.currentCursor()
	cursor += delta
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= listLen {
		cursor = listLen - 1
	}
	m.setCursor(cursor)
}

func (m Model) currentCursor() int {
	if m.activeTab == tabSessions {
		return m.sessionCursor
	}
	return m.processCursor
}

func (m *Model) setCursor(v int) {
	if m.activeTab == tabSessions {
		m.sessionCursor = v
	} else {
		m.processCursor = v
	}
}

func (m Model) filteredListLen() int {
	if m.activeTab == tabSessions {
		return len(m.filteredSessions())
	}
	return len(m.filteredProcesses())
}

func (m Model) filteredSessions() []data.Session {
	if m.filter == "" {
		return m.sessions
	}
	var out []data.Session
	f := strings.ToLower(m.filter)
	for _, s := range m.sessions {
		if strings.Contains(strings.ToLower(s.Key), f) ||
			strings.Contains(strings.ToLower(s.Model), f) ||
			strings.Contains(strings.ToLower(s.Kind), f) ||
			strings.Contains(strings.ToLower(s.DisplayName), f) ||
			strings.Contains(strings.ToLower(s.Channel), f) {
			out = append(out, s)
		}
	}
	return out
}

func (m Model) filteredProcesses() []data.Process {
	if m.filter == "" {
		return m.processes
	}
	var out []data.Process
	f := strings.ToLower(m.filter)
	for _, p := range m.processes {
		if strings.Contains(strings.ToLower(p.SessionName), f) ||
			strings.Contains(strings.ToLower(p.Command), f) {
			out = append(out, p)
		}
	}
	return out
}

func (m Model) selectedItemID() string {
	if m.activeTab == tabSessions {
		ss := m.filteredSessions()
		if m.sessionCursor < len(ss) {
			return ss[m.sessionCursor].Key
		}
	} else {
		pp := m.filteredProcesses()
		if m.processCursor < len(pp) {
			return pp[m.processCursor].SessionName
		}
	}
	return ""
}

func (m Model) logViewHeight() int {
	// Approximate: total height minus borders and status bar
	return max(1, m.height-4)
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	listWidth := m.width*2/5 - 2
	logWidth := m.width - listWidth - 6
	contentHeight := m.height - 4 // borders + status bar

	if listWidth < 20 {
		listWidth = 20
	}
	if logWidth < 20 {
		logWidth = 20
	}
	if contentHeight < 5 {
		contentHeight = 5
	}

	leftPanel := m.renderListPanel(listWidth, contentHeight)
	rightPanel := m.renderLogPanel(logWidth, contentHeight)
	statusBar := m.renderStatusBar()

	// Apply panel borders
	var leftBorder, rightBorder lipgloss.Style
	if m.activePanel == panelList {
		leftBorder = activePanelBorder
		rightBorder = panelBorder
	} else {
		leftBorder = panelBorder
		rightBorder = activePanelBorder
	}

	left := leftBorder.Width(listWidth).Height(contentHeight).Render(leftPanel)
	right := rightBorder.Width(logWidth).Height(contentHeight).Render(rightPanel)

	main := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, main, statusBar)
}

func (m Model) renderListPanel(width, height int) string {
	var b strings.Builder

	// Tabs
	tab1 := inactiveTabStyle.Render("1:Sessions")
	tab2 := inactiveTabStyle.Render("2:Processes")
	if m.activeTab == tabSessions {
		tab1 = activeTabStyle.Render("1:Sessions")
	} else {
		tab2 = activeTabStyle.Render("2:Processes")
	}
	b.WriteString(tab1 + " " + tab2 + "\n")

	// Search bar
	if m.searching {
		b.WriteString("/ " + m.searchInput.View() + "\n")
	} else if m.filter != "" {
		b.WriteString(dimStyle.Render("filter: "+m.filter) + "\n")
	} else {
		b.WriteString("\n")
	}

	if m.activeTab == tabSessions {
		b.WriteString(m.renderSessionList(width, height-3))
	} else {
		b.WriteString(m.renderProcessList(width, height-3))
	}

	return b.String()
}

func (m Model) renderSessionList(width, maxItems int) string {
	sessions := m.filteredSessions()
	if len(sessions) == 0 {
		return dimStyle.Render("  No sessions found")
	}

	var b strings.Builder
	activeCount := 0
	for _, s := range sessions {
		if s.AgeMs > 0 && s.AgeMs < 300000 {
			activeCount++
		} else if s.UpdatedAt > 0 {
			age := time.Since(time.UnixMilli(s.UpdatedAt))
			if age < 5*time.Minute {
				activeCount++
			}
		}
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf(" Sessions (%d active)", activeCount)) + "\n")

	count := 0
	for i, s := range sessions {
		if count >= maxItems-1 {
			break
		}

		// Determine activity status
		var ageForStatus time.Duration
		if s.AgeMs > 0 {
			ageForStatus = time.Duration(s.AgeMs) * time.Millisecond
		} else if s.UpdatedAt > 0 {
			ageForStatus = time.Since(time.UnixMilli(s.UpdatedAt))
		}

		status := "idle"
		if ageForStatus < time.Minute {
			status = "active"
		} else if ageForStatus < 5*time.Minute {
			status = "warm"
		}
		indicator := statusIndicator(status)

		// Use displayName if available, else key
		name := s.DisplayName
		if name == "" {
			name = s.Key
		}
		if len(name) > 14 {
			name = name[:14]
		}

		model := s.Model
		if len(model) > 16 {
			model = model[:16]
		}

		tokens := dimStyle.Render(fmt.Sprintf("%dk", s.TotalTokens/1000))

		var runtimeStr string
		if s.UpdatedAt > 0 {
			runtimeStr = formatDuration(time.Since(time.UnixMilli(s.UpdatedAt)))
		}
		runtime := dimStyle.Render(runtimeStr)

		// Show channel if present
		channelTag := ""
		if s.Channel != "" {
			channelTag = dimStyle.Render(" [" + s.Channel + "]")
		}

		prefix := "  "
		if i == m.sessionCursor {
			prefix = "▸ "
		}

		line := fmt.Sprintf("%s%s %-14s %-16s %5s %5s%s",
			prefix, indicator, name, model, tokens, runtime, channelTag)

		if i == m.sessionCursor {
			line = selectedStyle.Render(line)
		}

		b.WriteString(line + "\n")
		count++
	}

	return b.String()
}

func (m Model) renderProcessList(width, maxItems int) string {
	procs := m.filteredProcesses()
	if len(procs) == 0 {
		return dimStyle.Render("  No processes found")
	}

	var b strings.Builder
	runCount := 0
	for _, p := range procs {
		if p.Status == "running" || p.Status == "active" {
			runCount++
		}
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf(" Processes (%d running)", runCount)) + "\n")

	count := 0
	for i, p := range procs {
		if count >= maxItems-1 {
			break
		}

		indicator := processIndicator(p.Status)
		name := p.SessionName
		if len(name) > 14 {
			name = name[:14]
		}

		cmd := p.Command
		if len(cmd) > 20 {
			cmd = cmd[:20]
		}

		runtime := dimStyle.Render(p.Runtime)

		prefix := "  "
		if i == m.processCursor {
			prefix = "▸ "
		}

		line := fmt.Sprintf("%s%s %-14s %-20s %s", prefix, indicator, name, cmd, runtime)

		if i == m.processCursor {
			line = selectedStyle.Render(line)
		}

		b.WriteString(line + "\n")
		count++
	}

	return b.String()
}

func (m Model) renderLogPanel(width, height int) string {
	var b strings.Builder

	// Title
	logTitle := "Logs"
	if m.selectedLogID != "" {
		logTitle = "Logs: " + m.selectedLogID
	}
	followTag := ""
	if m.logFollow {
		followTag = statusRunning.Render(" [follow]")
	}
	b.WriteString(titleStyle.Render(logTitle) + followTag + "\n")
	b.WriteString(dimStyle.Render(strings.Repeat("\u2500", min(width, 40))) + "\n")

	if m.logContent == "" {
		b.WriteString(dimStyle.Render("  Press Enter on an item to view logs"))
		return b.String()
	}

	lines := strings.Split(m.logContent, "\n")
	viewH := height - 3
	if viewH < 1 {
		viewH = 1
	}

	start := m.logScrollPos
	if start > len(lines)-viewH {
		start = max(0, len(lines)-viewH)
	}
	end := start + viewH
	if end > len(lines) {
		end = len(lines)
	}

	for _, line := range lines[start:end] {
		if len(line) > width {
			line = line[:width]
		}
		b.WriteString(line + "\n")
	}

	return b.String()
}

func (m Model) renderStatusBar() string {
	width := m.width
	if width == 0 {
		width = 80
	}

	// Left: gateway status
	var leftParts []string
	if m.health != nil {
		healthStatus := "connected"
		if !m.health.OK {
			healthStatus = "disconnected"
		}
		st := statusRunning.Render("\u25cf " + healthStatus)
		leftParts = append(leftParts, st)
		leftParts = append(leftParts, dimStyle.Render(fmt.Sprintf("%dms", m.health.DurationMs)))
	} else {
		leftParts = append(leftParts, dimStyle.Render("\u25cb gateway"))
	}

	if m.messaging {
		prompt := statusThinking.Render(fmt.Sprintf("→ %s: ", m.msgTargetName))
		leftParts = append(leftParts, prompt+m.msgInput.View())
		gap := width - lipgloss.Width(strings.Join(leftParts, " "))
		if gap < 1 {
			gap = 1
		}
		return statusBarStyle.Width(width).Render(strings.Join(leftParts, " ") + strings.Repeat(" ", gap))
	}

	if m.sending {
		leftParts = append(leftParts, statusThinking.Render(fmt.Sprintf("⏳ sending to %s...", m.msgTargetName)))
	}

	if m.lastError != "" {
		errText := m.lastError
		if len(errText) > 80 {
			errText = errText[:80] + "..."
		}
		leftParts = append(leftParts, statusFailed.Render(errText))
	}

	if m.confirming {
		leftParts = append(leftParts, statusThinking.Render(fmt.Sprintf("Kill %s? [y/n]", m.confirmTarget)))
	}

	left := strings.Join(leftParts, " ")

	// Right: keybindings help
	right := dimStyle.Render("\u2191\u2193:nav  tab:panel  1/2:tab  \u21b5:logs  m:msg  x:kill  /:search  f:follow  q:quit")

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return statusBarStyle.Width(width).Render(left + strings.Repeat(" ", gap) + right)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
