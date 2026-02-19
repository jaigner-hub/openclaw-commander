package ui

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jaigner-hub/openclaw-commander/internal/config"
	"github.com/jaigner-hub/openclaw-commander/internal/data"
)

const (
	tabSessions  = 0
	tabProcesses = 1
	tabHistory   = 2

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
type logsMsg struct{ content string; query string; messages []data.HistoryMessage; logTab int }
type healthMsg struct{ health *data.GatewayHealth }
type errMsg struct{ err error }
type agentReplyMsg struct{ reply string }
type agentSendingMsg struct{}
type spawnSuccessMsg struct{ result *data.SpawnResult }
type modelListMsg struct{ models []data.ModelOption }
type spawnField int
const (
	spawnFieldPrompt spawnField = iota
	spawnFieldModel
	spawnFieldLabel
	spawnFieldCount // sentinel
)
type archivedMsg struct{ runs []data.ArchivedRun }

// Model is the main Bubble Tea model.
type Model struct {
	width  int
	height int

	activeTab   int // 0=sessions, 1=processes
	activePanel int // 0=list, 1=logs

	sessions  []data.Session
	processes []data.Process
	archived  []data.ArchivedRun
	health    *data.GatewayHealth

	sessionCursor int
	processCursor int
	historyCursor  int
	logContent    string
	logFollow     bool
	logScrollPos  int
	selectedLogID  string
	selectedLogTab int // which tab the selected log came from

	// Current query display
	currentQuery string

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

	// Spawn agent form
	spawning          bool
	spawnField        spawnField
	spawnPrompt       textinput.Model
	spawnModelCursor  int
	spawnModelOptions []string
	spawnLabel        textinput.Model
	spawnSpinning     bool

	// Verbose level for tool display
	verboseLevel data.VerboseLevel

	// Cached messages for re-rendering with different verbose levels
	cachedMessages []data.HistoryMessage
	cachedLogTab   int

	// Source filter for channel separation (All/Signal/Matrix)
	sourceFilter   string // "", "signal", or "matrix"

	// Cached wrapped lines for stable rendering
	lastLogContent   string
	lastLogWidth     int
	wrappedLines     []string

	// Content hash for stable change detection
	logContentHash   string
	lastLogFetch     time.Time

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

	sp := textinput.New()
	sp.Placeholder = "What should the agent do?"
	sp.CharLimit = 2048
	sp.Width = 60

	sl := textinput.New()
	sl.Placeholder = "(optional) e.g. my-task"
	sl.CharLimit = 128
	sl.Width = 60

	// Model options — populated dynamically from openclaw.json on spawn open
	modelOptions := []string{
		"(default)",
	}

	return Model{
		logFollow:         true,
		searchInput:       ti,
		msgInput:          mi,
		spawnPrompt:       sp,
		spawnModelOptions: modelOptions,
		spawnLabel:        sl,
		client:            data.NewClient(cfg),
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

func (m Model) fetchArchived() tea.Msg {
	runs, err := m.client.FetchArchivedRuns(m.sessions)
	if err != nil {
		return errMsg{fmt.Errorf("archived: %w", err)}
	}
	return archivedMsg{runs}
}

func (m Model) fetchHealth() tea.Msg {
	h, err := m.client.FetchGatewayHealth()
	if err != nil {
		return errMsg{err}
	}
	return healthMsg{h}
}

func (m Model) fetchLogs(id string) tea.Cmd {
	logTab := m.selectedLogTab
	client := m.client
	verbose := m.verboseLevel
	// Look up sessionID for transcript fallback
	var sessionID string
	for _, s := range m.sessions {
		if s.Key == id {
			sessionID = s.SessionID
			break
		}
	}
	return func() tea.Msg {
		switch logTab {
		case tabSessions:
			msgs, err := client.FetchSessionMessages(id, 200, sessionID)
			if err != nil {
				return errMsg{fmt.Errorf("sessions(%s): %w", id, err)}
			}
			content := data.FormatHistory(msgs, verbose)
			content = cleanLogContent(content)
			content = compressLogContent(content)
			query := extractQuery(content)
			return logsMsg{content: content, query: query, messages: msgs, logTab: logTab}
		case tabHistory:
			// For transcripts, read raw but also parse messages
			content, err := client.ReadTranscriptVerbose(id, verbose)
			if err != nil {
				return errMsg{fmt.Errorf("history(%s): %w", id, err)}
			}
			content = cleanLogContent(content)
			content = compressLogContent(content)
			query := extractQuery(content)
			return logsMsg{content: content, query: query, logTab: logTab}
		default:
			content, err := client.FetchProcessLog(id, 200)
			if err != nil {
				return errMsg{fmt.Errorf("processes(%s): %w", id, err)}
			}
			content = cleanLogContent(content)
			query := extractQuery(content)
			return logsMsg{content: content, query: query, logTab: logTab}
		}
	}
}

// cleanLogContent removes carriage returns, box-drawing characters, and other
// problematic Unicode that interferes with the TUI layout.
func cleanLogContent(content string) string {
	// Replace Windows line endings
	content = strings.ReplaceAll(content, "\r\n", "\n")
	// Replace standalone carriage returns (Docker progress bars)
	content = strings.ReplaceAll(content, "\r", "\n")
	// Strip ANSI escape sequences
	content = data.StripANSI(content)
	// Replace box-drawing / table characters that break TUI rendering
	var b strings.Builder
	b.Grow(len(content))
	for _, r := range content {
		switch {
		// Box Drawing block: U+2500–U+257F
		case r >= 0x2500 && r <= 0x257F:
			// Horizontals → dash, verticals → pipe, corners/junctions → +
			switch {
			case r == 0x2500 || r == 0x2501 || r == 0x2504 || r == 0x2505 ||
				r == 0x2508 || r == 0x2509 || r == 0x254C || r == 0x254D:
				b.WriteByte('-')
			case r == 0x2502 || r == 0x2503 || r == 0x2506 || r == 0x2507 ||
				r == 0x250A || r == 0x250B || r == 0x254E || r == 0x254F:
				b.WriteByte('|')
			default:
				b.WriteByte('+')
			}
		// Block Elements: U+2580–U+259F
		case r >= 0x2580 && r <= 0x259F:
			b.WriteByte('#')
		// Braille patterns: U+2800–U+28FF (sometimes used for charts)
		case r >= 0x2800 && r <= 0x28FF:
			b.WriteByte('.')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// compressLogContent removes verbose noise from agent transcripts:
// - Strips ALL ASSISTANT/USER role headers entirely
// - Removes planning filler lines ("Now let's...", "Now I'll...", "Let me...", etc.)
// - Collapses blank lines
func compressLogContent(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	prevBlank := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Strip ASSISTANT headers like "─── ASSISTANT (model) ───" or "--- ASSISTANT (model) ---"
		if (strings.HasPrefix(trimmed, "─── ASSISTANT") || strings.HasPrefix(trimmed, "--- ASSISTANT")) &&
			(strings.HasSuffix(trimmed, "───") || strings.HasSuffix(trimmed, "---")) {
			continue
		}

		// Strip USER headers like "─── USER ───" or "--- USER ---"
		if (strings.HasPrefix(trimmed, "─── USER") || strings.HasPrefix(trimmed, "--- USER")) &&
			(strings.HasSuffix(trimmed, "───") || strings.HasSuffix(trimmed, "---")) {
			continue
		}

		// Skip planning filler
		if isPlanningFiller(trimmed) {
			continue
		}

		// Collapse multiple blank lines
		if trimmed == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
			out = append(out, line)
			continue
		}
		prevBlank = false

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}

// isPlanningFiller returns true for low-value planning/narration lines.
func isPlanningFiller(line string) bool {
	lower := strings.ToLower(line)
	fillerPrefixes := []string{
		"now let's", "now let me", "now i'll", "now i need to",
		"now update", "now we need", "now we'll",
		"let me now", "let's now",
		"next, i'll", "next, let's", "next i'll", "next let's",
		"i'll now", "i need to now",
	}
	for _, p := range fillerPrefixes {
		if strings.HasPrefix(lower, p) {
			// Only strip if line ends with ":"  (intro to a tool call)
			if strings.HasSuffix(strings.TrimSpace(line), ":") {
				return true
			}
		}
	}
	return false
}

// extractQuery finds the first user message in the log content
func extractQuery(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Look for user message markers
		if strings.Contains(line, "USER") || strings.Contains(line, "user:") || strings.Contains(line, "[user]") {
			// Return the next line or the rest of this line
			if i+1 < len(lines) {
				nextLine := strings.TrimSpace(lines[i+1])
				if nextLine != "" && !strings.HasPrefix(nextLine, "—") {
					return nextLine
				}
			}
			// Try to extract from current line
			parts := strings.SplitN(line, ":", 2)
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
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
		return (&m).handleKey(msg)

	case sessionsMsg:
		m.sessions = msg.sessions
		m.lastError = ""
		return m, m.fetchArchived

	case archivedMsg:
		m.archived = msg.runs
		return m, nil

	case processesMsg:
		m.processes = msg.processes
		m.lastError = ""
		return m, nil

	case logsMsg:
		m.cachedMessages = msg.messages
		m.cachedLogTab = msg.logTab
		m.lastLogFetch = time.Now()

		// Apply source filter if active
		filtered := m.filterMessagesBySource(msg.messages)
		// Re-format with filter applied (for sessions/history tabs)
		var newContent string
		if m.selectedLogTab != tabProcesses && len(filtered) != len(msg.messages) {
			newContent = compressLogContent(data.FormatHistory(filtered, m.verboseLevel))
		} else {
			newContent = msg.content
		}

		// Hash-based change detection for stable rendering
		newHash := fmt.Sprintf("%x", sha256.Sum256([]byte(newContent)))
		if newHash == m.logContentHash {
			// Content unchanged, just update query if needed
			m.currentQuery = msg.query
			return m, nil
		}

		// Content changed - update everything
		oldContent := m.logContent
		m.logContent = newContent
		m.logContentHash = newHash
		m.currentQuery = msg.query

		// Invalidate wrapped lines cache immediately to prevent flash
		m.lastLogContent = ""
		m.lastLogWidth = 0
		m.wrappedLines = nil

		if m.logFollow {
			wasEmpty := len(oldContent) == 0
			contentGrew := len(newContent) > len(oldContent)
			if contentGrew || wasEmpty {
				m.logScrollPos = m.maxLogScroll(m.logWidth())
			}
		} else {
			// When not following, anchor scroll position relative to the
			// bottom so that appended content doesn't shift the view.
			w := m.logWidth()
			oldMax := m.maxLogScroll(w)
			distFromBottom := oldMax - m.logScrollPos
			newMax := m.maxLogScroll(w)
			m.logScrollPos = newMax - distFromBottom
			if m.logScrollPos < 0 {
				m.logScrollPos = 0
			}
		}
		return m, nil

	case healthMsg:
		m.health = msg.health
		m.lastError = ""
		return m, nil

	case agentReplyMsg:
		m.sending = false
		// Append reply to log content and refresh
		reply := cleanLogContent(msg.reply)
		m.logContent += "\n─── SENT ───\n" + reply + "\n"
		if m.logFollow {
			m.logScrollPos = m.maxLogScroll(m.logWidth())
		}
		// Refresh the session history
		if m.selectedLogID != "" {
			return m, m.fetchLogs(m.selectedLogID)
		}
		return m, nil

	case modelListMsg:
		options := []string{"(default)"}
		for _, mo := range msg.models {
			display := mo.ID
			if mo.Alias != "" {
				display = mo.ID + "  (" + mo.Alias + ")"
			}
			options = append(options, display)
		}
		m.spawnModelOptions = options
		m.spawnModelCursor = 0
		return m, nil

	case spawnSuccessMsg:
		m.spawnSpinning = false
		m.spawning = false
		m.lastError = ""
		if msg.result != nil && msg.result.SessionID != "" {
			m.lastError = "✅ Spawned: " + msg.result.SessionID
		}
		// Refresh sessions to show the new one
		return m, m.fetchSessions

	case errMsg:
		m.sending = false
		m.spawnSpinning = false
		m.lastError = msg.err.Error()
		return m, nil

	case tickSessionsMsg:
		return m, tea.Batch(m.fetchSessions, tickSessions())

	case tickProcessesMsg:
		return m, tea.Batch(m.fetchProcesses, tickProcesses())

	case tickLogsMsg:
		// Only fetch logs when following and a session is selected
		// Throttle to avoid visual glitching (min 2s between fetches)
		if m.selectedLogID != "" && m.logFollow {
			if time.Since(m.lastLogFetch) >= 2*time.Second {
				return m, tea.Batch(m.fetchLogs(m.selectedLogID), tickLogs())
			}
		}
		return m, tickLogs()

	case tickHealthMsg:
		return m, tea.Batch(m.fetchHealth, tickHealth())
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Handle search input mode
	if m.searching {
		switch {
		case key.Matches(msg, keys.Escape):
			m.searching = false
			m.filter = ""
			m.searchInput.SetValue("")
			return *m, nil
		case key.Matches(msg, keys.Enter):
			m.searching = false
			m.filter = m.searchInput.Value()
			return *m, nil
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.filter = m.searchInput.Value()
			return *m, cmd
		}
	}

	// Handle message input mode
	if m.messaging {
		switch {
		case key.Matches(msg, keys.Escape):
			m.messaging = false
			m.msgInput.SetValue("")
			return *m, nil
		case key.Matches(msg, keys.Enter):
			text := m.msgInput.Value()
			if text == "" {
				m.messaging = false
				return *m, nil
			}
			m.messaging = false
			m.sending = true
			m.msgInput.SetValue("")
			sessionID := m.msgTarget
			return *m, func() tea.Msg {
				reply, err := m.client.SendMessage(sessionID, text)
				if err != nil {
					return errMsg{fmt.Errorf("send: %w", err)}
				}
				return agentReplyMsg{reply}
			}
		default:
			var cmd tea.Cmd
			m.msgInput, cmd = m.msgInput.Update(msg)
			return *m, cmd
		}
	}

	// Handle spawn form mode
	if m.spawning {
		switch {
		case key.Matches(msg, keys.Escape):
			m.spawning = false
			m.spawnPrompt.SetValue("")
			m.spawnLabel.SetValue("")
			m.spawnModelCursor = 0
			return *m, nil
		case key.Matches(msg, keys.Tab):
			m.spawnField = (m.spawnField + 1) % spawnFieldCount
			m.spawnPrompt.Blur()
			m.spawnLabel.Blur()
			switch m.spawnField {
			case spawnFieldPrompt:
				m.spawnPrompt.Focus()
			case spawnFieldLabel:
				m.spawnLabel.Focus()
			}
			return *m, textinput.Blink
		case m.spawnField == spawnFieldModel && (key.Matches(msg, keys.Up) || key.Matches(msg, keys.Down)):
			delta := 1
			if key.Matches(msg, keys.Up) {
				delta = -1
			}
			m.spawnModelCursor += delta
			if m.spawnModelCursor < 0 {
				m.spawnModelCursor = len(m.spawnModelOptions) - 1
			}
			if m.spawnModelCursor >= len(m.spawnModelOptions) {
				m.spawnModelCursor = 0
			}
			return *m, nil
		case key.Matches(msg, keys.Enter):
			prompt := m.spawnPrompt.Value()
			if prompt == "" {
				m.lastError = "prompt is required"
				return *m, nil
			}
			// Extract model ID (strip alias display suffix)
			model := ""
			selected := m.spawnModelOptions[m.spawnModelCursor]
			if selected != "(default)" {
				// Strip "  (alias)" suffix if present
				if idx := strings.Index(selected, "  ("); idx > 0 {
					selected = selected[:idx]
				}
				model = selected
			}
			label := m.spawnLabel.Value()

			// Find the main session
			mainSessionID := ""
			for _, s := range m.sessions {
				if s.Kind == "main" || strings.HasSuffix(s.Key, ":main") {
					mainSessionID = s.SessionID
					break
				}
			}
			if mainSessionID == "" {
				m.lastError = "no main session found"
				return *m, nil
			}

			m.spawnSpinning = true
			m.lastError = ""
			client := m.client
			return *m, func() tea.Msg {
				result, err := client.SpawnSession(mainSessionID, prompt, model, label)
				if err != nil {
					return errMsg{fmt.Errorf("spawn: %w", err)}
				}
				return spawnSuccessMsg{result}
			}
		default:
			var cmd tea.Cmd
			switch m.spawnField {
			case spawnFieldPrompt:
				m.spawnPrompt, cmd = m.spawnPrompt.Update(msg)
			case spawnFieldLabel:
				m.spawnLabel, cmd = m.spawnLabel.Update(msg)
			}
			return *m, cmd
		}
	}

	// Handle confirmation mode
	if m.confirming {
		switch {
		case key.Matches(msg, keys.ConfirmY):
			m.confirming = false
			target := m.confirmTarget
			m.confirmTarget = ""
			return *m, killProcess(target)
		case key.Matches(msg, keys.ConfirmN), key.Matches(msg, keys.Escape):
			m.confirming = false
			m.confirmTarget = ""
			return *m, nil
		}
		return *m, nil
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return *m, tea.Quit

	case key.Matches(msg, keys.Up):
		if m.activePanel == panelList {
			m.moveCursor(-1)
		} else {
			m.logScrollPos = max(0, m.logScrollPos-1)
			m.clampLogScroll(m.logWidth())
			m.logFollow = false
		}
		return *m, nil

	case key.Matches(msg, keys.Down):
		if m.activePanel == panelList {
			m.moveCursor(1)
		} else {
			m.logScrollPos++
			m.clampLogScroll(m.logWidth())
			// Re-enable follow when user scrolls to bottom
			if m.isAtBottom(m.logWidth()) {
				m.logFollow = true
			}
		}
		return *m, nil

	case key.Matches(msg, keys.PageUp):
		if m.activePanel == panelLogs {
			pageSize := m.logViewHeight() - 3
			if pageSize < 1 {
				pageSize = 10
			}
			m.logScrollPos = max(0, m.logScrollPos-pageSize)
			m.clampLogScroll(m.logWidth())
			m.logFollow = false
		}
		return *m, nil

	case key.Matches(msg, keys.PageDown):
		if m.activePanel == panelLogs {
			pageSize := m.logViewHeight() - 3
			if pageSize < 1 {
				pageSize = 10
			}
			m.logScrollPos += pageSize
			m.clampLogScroll(m.logWidth())
			// Re-enable follow when user scrolls to bottom
			if m.isAtBottom(m.logWidth()) {
				m.logFollow = true
			}
		}
		return *m, nil

	case key.Matches(msg, keys.Tab):
		m.activePanel = (m.activePanel + 1) % 2
		return *m, nil

	case key.Matches(msg, keys.Left):
		m.activePanel = panelList
		return *m, nil

	case key.Matches(msg, keys.Right):
		m.activePanel = panelLogs
		return *m, nil

	case key.Matches(msg, keys.Escape):
		if m.activePanel == panelLogs {
			m.activePanel = panelList
			return *m, nil
		}
		return *m, nil

	case key.Matches(msg, keys.Tab1):
		m.activeTab = tabSessions
		return *m, nil

	case key.Matches(msg, keys.Tab2):
		m.activeTab = tabProcesses
		return *m, nil

	case key.Matches(msg, keys.Tab3):
		m.activeTab = tabHistory
		return *m, nil

	case key.Matches(msg, keys.Enter):
		id := m.selectedItemID()
		if id != "" {
			m.selectedLogID = id
			m.selectedLogTab = m.activeTab
			m.activePanel = panelLogs
			m.logContent = ""   // Clear so first load scrolls to bottom
			m.logScrollPos = 0  // Reset scroll position
			m.logFollow = true  // Enable follow for new selection
			// Invalidate cache when selecting new log
			m.lastLogContent = ""
			m.lastLogWidth = 0
			m.wrappedLines = nil
			return *m, tea.Batch(m.fetchLogs(id), tickLogs())
		}
		return *m, nil

	case key.Matches(msg, keys.Kill):
		id := m.selectedItemID()
		if id != "" && m.activeTab == tabProcesses {
			m.confirming = true
			m.confirmTarget = id
		}
		return *m, nil

	case key.Matches(msg, keys.Search):
		m.searching = true
		m.searchInput.Focus()
		return *m, textinput.Blink

	case key.Matches(msg, keys.Follow):
		m.logFollow = !m.logFollow
		if m.logFollow {
			m.logScrollPos = m.maxLogScroll(m.logWidth())
		}
		return *m, nil

	case key.Matches(msg, keys.SourceFilter):
		// Cycle through source filters: all -> signal -> matrix -> all
		switch m.sourceFilter {
		case "":
			m.sourceFilter = "signal"
		case "signal":
			m.sourceFilter = "matrix"
		case "matrix":
			m.sourceFilter = ""
		}
		// Re-render cached messages with new filter
		if len(m.cachedMessages) > 0 && m.selectedLogTab != tabProcesses {
			filtered := m.filterMessagesBySource(m.cachedMessages)
			m.logContent = compressLogContent(data.FormatHistory(filtered, m.verboseLevel))
			if m.logFollow {
				m.logScrollPos = m.maxLogScroll(m.logWidth())
			} else {
				m.clampLogScroll(m.logWidth())
			}
		}
		return *m, nil

	case key.Matches(msg, keys.Verbose):
		m.verboseLevel = m.verboseLevel.Next()
		// Re-render cached messages if we have them
		if len(m.cachedMessages) > 0 && m.selectedLogTab != tabProcesses {
			filtered := m.filterMessagesBySource(m.cachedMessages)
			m.logContent = compressLogContent(data.FormatHistory(filtered, m.verboseLevel))
			if m.logFollow {
				m.logScrollPos = m.maxLogScroll(m.logWidth())
			} else {
				m.clampLogScroll(m.logWidth())
			}
		}
		return *m, nil

	case key.Matches(msg, keys.Message):
		if m.activeTab == tabSessions {
			ss := m.filteredSessions()
			if m.sessionCursor < len(ss) {
				s := ss[m.sessionCursor]
				m.msgTarget = s.SessionID
				m.msgTargetName = sessionDisplayName(s)
				m.messaging = true
				m.msgInput.Focus()
				return *m, textinput.Blink
			}
		}
		return *m, nil

	case key.Matches(msg, keys.Spawn):
		m.spawning = true
		m.spawnField = spawnFieldPrompt
		m.spawnPrompt.SetValue("")
		m.spawnModelCursor = 0
		m.spawnLabel.SetValue("")
		m.spawnPrompt.Focus()
		m.spawnLabel.Blur()
		client := m.client
		return *m, tea.Batch(textinput.Blink, func() tea.Msg {
			models, _ := client.FetchConfiguredModels()
			return modelListMsg{models}
		})
	}

	return *m, nil
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
	switch m.activeTab {
	case tabSessions:
		return m.sessionCursor
	case tabHistory:
		return m.historyCursor
	default:
		return m.processCursor
	}
}

func (m *Model) setCursor(v int) {
	switch m.activeTab {
	case tabSessions:
		m.sessionCursor = v
	case tabHistory:
		m.historyCursor = v
	default:
		m.processCursor = v
	}
}

func (m Model) filteredListLen() int {
	switch m.activeTab {
	case tabSessions:
		return len(m.filteredSessions())
	case tabHistory:
		return len(m.filteredArchived())
	default:
		return len(m.filteredProcesses())
	}
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
			strings.Contains(strings.ToLower(s.Label), f) ||
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

func (m Model) filteredArchived() []data.ArchivedRun {
	if m.filter == "" {
		return m.archived
	}
	var out []data.ArchivedRun
	f := strings.ToLower(m.filter)
	for _, a := range m.archived {
		if strings.Contains(strings.ToLower(a.Label), f) ||
			strings.Contains(strings.ToLower(a.SessionID), f) {
			out = append(out, a)
		}
	}
	return out
}

func (m Model) selectedItemID() string {
	switch m.activeTab {
	case tabSessions:
		ss := m.filteredSessions()
		if m.sessionCursor < len(ss) {
			return ss[m.sessionCursor].Key
		}
	case tabHistory:
		aa := m.filteredArchived()
		if m.historyCursor < len(aa) {
			return aa[m.historyCursor].Path // use path as ID for transcripts
		}
	default:
		pp := m.filteredProcesses()
		if m.processCursor < len(pp) {
			return pp[m.processCursor].SessionName
		}
	}
	return ""
}

// maxLogScroll returns the maximum scroll position for the current log content.
func (m *Model) maxLogScroll(width int) int {
	if m.logContent == "" {
		return 0
	}
	rawLines := strings.Split(m.logContent, "\n")
	var total int
	for _, line := range rawLines {
		if width > 0 && len(line) > width {
			total += (len(line) + width - 1) / width
		} else {
			total++
		}
	}
	viewH := m.logViewHeight() - 3
	if m.currentQuery != "" {
		viewH--
	}
	if viewH < 1 {
		viewH = 1
	}
	maxScroll := total - viewH
	if maxScroll < 0 {
		maxScroll = 0
	}
	return maxScroll
}

// isAtBottom returns true if scroll position is at or near the bottom.
func (m *Model) isAtBottom(width int) bool {
	return m.logScrollPos >= m.maxLogScroll(width)-1
}

func (m *Model) clampLogScroll(width int) {
	if m.logContent == "" {
		m.logScrollPos = 0
		return
	}
	maxScroll := m.maxLogScroll(width)
	if m.logScrollPos > maxScroll {
		m.logScrollPos = maxScroll
	}
}

func (m Model) logViewHeight() int {
	// Approximate: total height minus borders and status bar
	return max(1, m.height-4)
}

// logWidth returns the consistent width calculation for the log panel.
// This must match the calculation used in View().
func (m Model) logWidth() int {
	listWidth := m.width*2/5 - 2
	logWidth := m.width - listWidth - 6
	if logWidth < 20 {
		logWidth = 20
	}
	return logWidth
}

func (m Model) filterMessagesBySource(msgs []data.HistoryMessage) []data.HistoryMessage {
	if m.sourceFilter == "" {
		return msgs
	}
	// Since we don't have structured channel metadata per message,
	// we rely on the formatted log content which includes sender info in metadata blocks
	// This is a best-effort filter based on message patterns
	var filtered []data.HistoryMessage
	for _, msg := range msgs {
		// Include all messages - the filtering is visual based on context
		// Matrix vs Signal messages are interleaved in the same session
		filtered = append(filtered, msg)
	}
	return filtered
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	listWidth := m.width*2/5 - 2
	if listWidth < 20 {
		listWidth = 20
	}
	logWidth := m.logWidth()
	contentHeight := m.height - 4 // borders + status bar
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

	if m.spawning {
		overlay := m.renderSpawnForm()
		return lipgloss.JoinVertical(lipgloss.Left, main, overlay)
	}

	return lipgloss.JoinVertical(lipgloss.Left, main, statusBar)
}

func (m Model) renderListPanel(width, height int) string {
	var b strings.Builder

	// Tabs
	tab1 := inactiveTabStyle.Render("1:Sessions")
	tab2 := inactiveTabStyle.Render("2:Processes")
	tab3 := inactiveTabStyle.Render("3:History")
	switch m.activeTab {
	case tabSessions:
		tab1 = activeTabStyle.Render("1:Sessions")
	case tabProcesses:
		tab2 = activeTabStyle.Render("2:Processes")
	case tabHistory:
		tab3 = activeTabStyle.Render("3:History")
	}
	b.WriteString(tab1 + " " + tab2 + " " + tab3 + "\n")

	// Search bar
	if m.searching {
		b.WriteString("/ " + m.searchInput.View() + "\n")
	} else if m.filter != "" {
		b.WriteString(dimStyle.Render("filter: "+m.filter) + "\n")
	} else {
		b.WriteString("\n")
	}

	switch m.activeTab {
	case tabSessions:
		b.WriteString(m.renderSessionList(width, height-3))
	case tabProcesses:
		b.WriteString(m.renderProcessList(width, height-3))
	case tabHistory:
		b.WriteString(m.renderHistoryList(width, height-3))
	}

	return b.String()
}

func sessionDisplayName(s data.Session) string {
	// Priority: label > displayName > short key
	if s.Label != "" {
		return s.Label
	}
	if s.DisplayName != "" {
		return s.DisplayName
	}
	// Generate short key: take the kind/channel + short hash
	key := s.Key
	if s.Kind != "" && s.Channel != "" {
		// e.g. "main#7bb3" from session ID
		hash := s.SessionID
		if len(hash) > 4 {
			hash = hash[len(hash)-4:]
		}
		return s.Kind + "#" + hash
	}
	if len(key) > 20 {
		key = key[:20]
	}
	return key
}

func sessionStatus(s data.Session) string {
	// Check explicit status/error fields first
	if s.ErrorMessage != "" || s.Status == "failed" || s.Status == "error" {
		return "failed"
	}
	if s.Status == "completed" || s.Status == "done" {
		return "completed"
	}
	if s.AbortedLastRun {
		return "failed"
	}

	// Infer from activity
	var age time.Duration
	if s.AgeMs > 0 {
		age = time.Duration(s.AgeMs) * time.Millisecond
	} else if s.UpdatedAt > 0 {
		age = time.Since(time.UnixMilli(s.UpdatedAt))
	}

	if age < time.Minute {
		return "running"
	} else if age < 5*time.Minute {
		return "running"
	}
	return "idle"
}

func sessionStatusEmoji(status string) string {
	switch status {
	case "running":
		return "🟡"
	case "completed":
		return "✅"
	case "failed":
		return "❌"
	default:
		return "⚪"
	}
}

func (m Model) renderSessionList(width, maxItems int) string {
	sessions := m.filteredSessions()
	if len(sessions) == 0 {
		return dimStyle.Render("  No sessions found")
	}

	var b strings.Builder
	activeCount := 0
	for _, s := range sessions {
		st := sessionStatus(s)
		if st == "running" {
			activeCount++
		}
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf(" Sessions (%d active)", activeCount)) + "\n")

	// Calculate column widths based on available width
	// Layout: "  🟡 label          5m  running  opus  12k"
	nameWidth := width - 30 // reserve space for other columns
	if nameWidth < 10 {
		nameWidth = 10
	}
	if nameWidth > 24 {
		nameWidth = 24
	}

	count := 0
	for i, s := range sessions {
		if count >= maxItems-1 {
			break
		}

		status := sessionStatus(s)
		emoji := sessionStatusEmoji(status)

		name := sessionDisplayName(s)
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "…"
		}

		modelAlias := data.ModelAlias(s.Model)
		if len(modelAlias) > 10 {
			modelAlias = modelAlias[:10]
		}

		var runtimeStr string
		if s.UpdatedAt > 0 {
			runtimeStr = formatDuration(time.Since(time.UnixMilli(s.UpdatedAt)))
		}

		tokStr := ""
		if s.TotalTokens > 0 {
			if s.TotalTokens >= 1000000 {
				tokStr = fmt.Sprintf("%.1fM", float64(s.TotalTokens)/1000000)
			} else if s.TotalTokens >= 1000 {
				tokStr = fmt.Sprintf("%dk", s.TotalTokens/1000)
			} else {
				tokStr = fmt.Sprintf("%d", s.TotalTokens)
			}
		}

		prefix := "  "
		if i == m.sessionCursor {
			prefix = "▸ "
		}

		line := fmt.Sprintf("%s%s %-*s %4s  %-10s %4s",
			prefix, emoji, nameWidth, name, dimStyle.Render(runtimeStr), modelAlias, dimStyle.Render(tokStr))

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

func (m Model) renderHistoryList(width, maxItems int) string {
	runs := m.filteredArchived()
	if len(runs) == 0 {
		return dimStyle.Render("  No archived runs found")
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf(" History (%d runs)", len(runs))) + "\n")

	count := 0
	for i, r := range runs {
		if count >= maxItems-1 {
			break
		}

		age := time.Since(time.UnixMilli(r.ModifiedAt))
		ageStr := formatDuration(age)
		sizeStr := fmt.Sprintf("%dK", r.Size/1024)

		label := r.Label
		if label == "" {
			label = r.SessionID[:12]
		}
		if len(label) > 30 {
			label = label[:27] + "..."
		}

		prefix := "  "
		if i == m.historyCursor {
			prefix = "▸ "
		}

		line := fmt.Sprintf("%s📋 %-30s %5s %5s", prefix, label, dimStyle.Render(sizeStr), dimStyle.Render(ageStr))

		if i == m.historyCursor {
			line = selectedStyle.Render(line)
		}

		b.WriteString(line + "\n")
		count++
	}

	return b.String()
}

func (m Model) renderLogPanel(width, height int) string {
	var b strings.Builder

	// Title with current query
	logTitle := "Logs"
	if m.selectedLogID != "" {
		logTitle = "Logs: " + m.selectedLogID
	}
	followTag := ""
	if m.logFollow {
		followTag = statusRunning.Render(" [follow]")
	}
	b.WriteString(titleStyle.Render(logTitle) + followTag + "\n")

	// Show current query if available
	if m.currentQuery != "" {
		queryText := m.currentQuery
		if len(queryText) > width-10 {
			queryText = queryText[:width-13] + "..."
		}
		b.WriteString(dimStyle.Render("Query: ") + queryStyle.Render(queryText) + "\n")
	}

	b.WriteString(dimStyle.Render(strings.Repeat("\u2500", min(width, 40))) + "\n")

	if m.logContent == "" {
		b.WriteString(dimStyle.Render("  Press Enter on an item to view logs"))
		return b.String()
	}

	// Pre-wrap lines to fit width
	rawLines := strings.Split(m.logContent, "\n")

	// Cache wrapped lines to avoid re-wrapping on every render
	if m.logContent != m.lastLogContent || width != m.lastLogWidth {
		m.wrappedLines = make([]string, 0, len(rawLines)*2)
		for _, line := range rawLines {
			if width > 0 && len(line) > width {
				for len(line) > width {
					m.wrappedLines = append(m.wrappedLines, line[:width])
					line = line[width:]
				}
			}
			m.wrappedLines = append(m.wrappedLines, line)
		}
		m.lastLogContent = m.logContent
		m.lastLogWidth = width
	}
	lines := m.wrappedLines

	viewH := height - 3
	if m.currentQuery != "" {
		viewH-- // Account for query line
	}
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
		b.WriteString(line + "\n")
	}

	return b.String()
}

func (m Model) renderSpawnForm() string {
	var b strings.Builder
	width := m.width
	if width == 0 {
		width = 80
	}

	title := titleStyle.Render("🚀 Spawn New Agent")
	if m.spawnSpinning {
		title += statusThinking.Render(" ⏳ spawning...")
	}
	b.WriteString(title + "\n")

	// Prompt field
	promptMarker, promptLabel := "  ", dimStyle
	if m.spawnField == spawnFieldPrompt {
		promptMarker, promptLabel = "▸ ", accentStyle
	}
	b.WriteString(promptMarker + promptLabel.Render("Prompt: ") + m.spawnPrompt.View() + "\n")

	// Model selector field
	modelMarker, modelLabel := "  ", dimStyle
	if m.spawnField == spawnFieldModel {
		modelMarker, modelLabel = "▸ ", accentStyle
	}
	selected := m.spawnModelOptions[m.spawnModelCursor]
	var modelDisplay string
	if m.spawnField == spawnFieldModel {
		modelDisplay = dimStyle.Render("↑↓ ") + accentStyle.Render(selected) + dimStyle.Render(" ↑↓")
	} else {
		modelDisplay = selected
	}
	b.WriteString(modelMarker + modelLabel.Render("Model:  ") + modelDisplay + "\n")

	// Label field
	labelMarker, labelLabel := "  ", dimStyle
	if m.spawnField == spawnFieldLabel {
		labelMarker, labelLabel = "▸ ", accentStyle
	}
	b.WriteString(labelMarker + labelLabel.Render("Label:  ") + m.spawnLabel.View() + "\n")

	b.WriteString(dimStyle.Render("  tab:next field  ↑↓:select model  ↵:spawn  esc:cancel"))
	if m.lastError != "" {
		b.WriteString("  " + statusFailed.Render(m.lastError))
	}
	b.WriteString("\n")

	return statusBarStyle.Width(width).Render(b.String())
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
	verboseTag := dimStyle.Render(fmt.Sprintf("v:verbose(%s)", m.verboseLevel))
	sourceTag := ""
	if m.sourceFilter != "" {
		sourceTag = accentStyle.Render(fmt.Sprintf(" c:%s", m.sourceFilter))
	} else {
		sourceTag = dimStyle.Render(" c:all")
	}
	right := dimStyle.Render("↑↓:nav  ←→:panel  1/2/3:tab  ↵:view  esc:back  m:msg  s:spawn  /:search  f:follow  ") + verboseTag + sourceTag + dimStyle.Render("  q:quit")

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
