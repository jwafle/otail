package app

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/transport"
)

/* ------------------------------------------------------------------ */
/* Key-bindings & shared styles                                       */
/* ------------------------------------------------------------------ */

type keyMap struct {
	Logs, Metrics, Traces key.Binding
	Pause, Quit, Yank     key.Binding
}

var keys = keyMap{
	Logs:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
	Metrics: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "metrics")),
	Traces:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "traces")),
	Pause:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Yank:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yank to clipboard")),
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Logs,
		k.Metrics,
		k.Traces,
		k.Pause,
		k.Quit,
		k.Yank,
	}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			k.Logs,
			k.Metrics,
			k.Traces,
			k.Pause,
			k.Quit,
			k.Yank,
		},
	}
}

var statusStyle = lipgloss.NewStyle().Foreground(
	lipgloss.AdaptiveColor{Light: "#909090", Dark: "#626262"},
)

var cursorStyle = lipgloss.NewStyle().Reverse(true)

var msgHighlightStyle = lipgloss.NewStyle().
	Background(lipgloss.AdaptiveColor{Light: "#404040", Dark: "#303030"})

// Tabs --------------------------------------------------------------------

var (
	activeTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      " ",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┘",
		BottomRight: "└",
	}

	tabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┴",
		BottomRight: "┴",
	}

	tabStyle = lipgloss.NewStyle().
			Border(tabBorder, true).
			BorderForeground(lipgloss.Color("214")).
			Padding(0, 1)

	activeTabStyle = tabStyle.Border(activeTabBorder, true)

	tabGap = tabStyle.
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false)
)

// cursorBuffer is the number of lines to keep between the cursor and the edge
// of the viewport while navigating.
const cursorBuffer = 3

/* ------------------------------------------------------------------ */
/* Model                                                              */
/* ------------------------------------------------------------------ */

type model struct {
	/* streaming */
	stream    *transport.Stream
	ctxCancel context.CancelFunc

	/* ui helpers */
	spinner spinner.Model
	help    help.Model
	ready   bool
	paused  bool

	/* viewport */
	viewport viewport.Model

	/* cursor */
	cursorLine int
	cursorMsg  *item

	/* data */
	logs, metrics, traces []item
	active                telemetry.Kind

	/* error handling */
	err error
}

func (m *model) activeItems() []item {
	switch m.active {
	case telemetry.KindMetrics:
		return m.metrics
	case telemetry.KindTraces:
		return m.traces
	default:
		return m.logs
	}
}

func (m *model) totalLines() int {
	items := m.activeItems()
	n := 0
	for _, it := range items {
		n += len(it.Lines)
	}
	return n
}

func (m *model) cursorMsgIndex() int {
	line := 0
	items := m.activeItems()
	for i, it := range items {
		if m.cursorLine < line+len(it.Lines) {
			return i
		}
		line += len(it.Lines)
	}
	if len(items) == 0 {
		return 0
	}
	return len(items) - 1
}

func (m *model) ensureCursorVisible() {
	if !m.paused {
		return
	}
	if m.cursorLine < m.viewport.YOffset {
		m.viewport.SetYOffset(m.cursorLine)
	} else if m.cursorLine >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(m.cursorLine - m.viewport.Height + 1)
	}
}

func (m *model) cursorUp() {
	if m.cursorLine == 0 {
		return
	}
	m.cursorLine--
	if m.cursorLine < m.viewport.YOffset+cursorBuffer && !m.viewport.AtTop() {
		m.viewport.SetYOffset(m.viewport.YOffset - 1)
	}
}

func (m *model) cursorDown() {
	total := m.totalLines()
	if m.cursorLine >= total-1 {
		return
	}
	m.cursorLine++
	bottom := m.viewport.YOffset + m.viewport.VisibleLineCount() - cursorBuffer
	if m.cursorLine >= bottom && !m.viewport.AtBottom() {
		m.viewport.SetYOffset(m.viewport.YOffset + 1)
	}
}

/* ------------- tea.Model interface -------------------------------- */

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		readFrame(m.stream),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			m.ctxCancel() // stop transport goroutine
			return m, tea.Quit
		case key.Matches(msg, keys.Logs):
			m.active = telemetry.KindLogs
			m.syncViewport()
		case key.Matches(msg, keys.Metrics):
			m.active = telemetry.KindMetrics
			m.syncViewport()
		case key.Matches(msg, keys.Traces):
			m.active = telemetry.KindTraces
			m.syncViewport()
		case key.Matches(msg, keys.Pause):
			m.paused = !m.paused
			if m.paused {
				m.cursorLine = m.viewport.YOffset + m.viewport.VisibleLineCount() - 1
				if m.cursorLine < 0 {
					m.cursorLine = 0
				}
			}
		case m.paused && key.Matches(msg, m.viewport.KeyMap.Up):
			m.cursorUp()
			m.ensureCursorVisible()
			m.syncViewport()
			return m, nil
		case m.paused && key.Matches(msg, m.viewport.KeyMap.Down):
			m.cursorDown()
			m.ensureCursorVisible()
			m.syncViewport()
			return m, nil
		}
		var c tea.Cmd
		m.help, c = m.help.Update(msg)
		cmds = append(cmds, c)

	case tea.WindowSizeMsg:
		verticalMargin := 5
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMargin)
			m.ready = true
		} else {
			m.viewport.Width, m.viewport.Height = msg.Width, msg.Height-verticalMargin
		}
		m.syncViewport()

	case telemetry.Message:
		if !m.paused {
			itm := newItem(msg)
			switch msg.Kind {
			case telemetry.KindMetrics:
				m.metrics = append(m.metrics, itm)
			case telemetry.KindTraces:
				m.traces = append(m.traces, itm)
			default:
				m.logs = append(m.logs, itm)
			}
			m.viewport.GotoBottom()
			m.syncViewport()
		}
		cmds = append(cmds, readFrame(m.stream))

	case error: // transport fatal error
		m.err = msg
		return m, tea.Quit

	case spinner.TickMsg:
		var c tea.Cmd
		m.spinner, c = m.spinner.Update(msg)
		cmds = append(cmds, c)
	}

	/* viewport internal updates */
	var c tea.Cmd
	oldOffset := m.viewport.YOffset
	m.viewport, c = m.viewport.Update(msg)
	cmds = append(cmds, c)
	if m.paused {
		delta := m.viewport.YOffset - oldOffset
		if delta != 0 {
			m.cursorLine += delta
		}
		if m.cursorLine < 0 {
			m.cursorLine = 0
		}
		if total := m.totalLines(); total > 0 && m.cursorLine >= total {
			m.cursorLine = total - 1
		}
		m.ensureCursorVisible()
		m.syncViewport()
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	var b strings.Builder

	b.WriteString(m.renderTabs())
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	if m.err != nil {
		b.WriteString("\nerror: ")
		b.WriteString(m.err.Error())
	}
	b.WriteString("\n")

	var status strings.Builder
	if m.paused {
		status.WriteString("[PAUSED] ")
	} else {
		status.WriteString(m.spinner.View())
		status.WriteString(" Streaming ")
	}
	status.WriteString(m.active.String())
	b.WriteString(statusStyle.Render(status.String()))
	b.WriteString("\n")
	b.WriteString(m.help.View(keys))

	return b.String()
}

/* ------------- helpers -------------------------------------------- */

func (m *model) syncViewport() {
	var src []item
	switch m.active {
	case telemetry.KindMetrics:
		src = m.metrics
	case telemetry.KindTraces:
		src = m.traces
	default:
		src = m.logs
	}
	total := 0
	for _, it := range src {
		total += len(it.Lines)
	}
	if m.cursorLine >= total {
		m.cursorLine = total - 1
	}

	var b strings.Builder
	line := 0
	var current *item
	for i := range src {
		highlight := m.paused && i == m.cursorMsgIndex()
		for j, l := range src[i].Lines {
			content := l
			if highlight {
				content = msgHighlightStyle.Render(content)
			}
			if m.paused && line == m.cursorLine {
				content = cursorStyle.Render(content)
				current = &src[i]
			}
			b.WriteString(content)
			line++
			if i < len(src)-1 || j < len(src[i].Lines)-1 {
				b.WriteString("\n")
			}
		}
	}
	m.cursorMsg = current
	m.viewport.SetContent(b.String())
}

func (m model) renderTabs() string {
	tabs := []string{
		tabStyle.Render("Logs"),
		tabStyle.Render("Metrics"),
		tabStyle.Render("Traces"),
	}
	switch m.active {
	case telemetry.KindMetrics:
		tabs[1] = activeTabStyle.Render("Metrics")
	case telemetry.KindTraces:
		tabs[2] = activeTabStyle.Render("Traces")
	default:
		tabs[0] = activeTabStyle.Render("Logs")
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	if m.viewport.Width > 0 {
		gapWidth := m.viewport.Width - lipgloss.Width(row)
		if gapWidth < 0 {
			gapWidth = 0
		}
		row = lipgloss.JoinHorizontal(lipgloss.Bottom,
			row,
			tabGap.Render(strings.Repeat(" ", gapWidth)),
		)
	}
	return row
}

/* readFrame returns a command that receives one frame from the stream */
func readFrame(s *transport.Stream) tea.Cmd {
	return func() tea.Msg {
		select {
		case b, ok := <-s.Messages():
			if !ok {
				return fmt.Errorf("stream closed")
			}
			return telemetry.Parse(b)
		case err, ok := <-s.Errors():
			if ok {
				return err
			}
			return fmt.Errorf("stream error channel closed")
		}
	}
}

/* ------------------------------------------------------------------ */
/* Public entry-point                                                 */
/* ------------------------------------------------------------------ */

// Run creates the transport, spins up the Bubble Tea program, and blocks
// until the TUI exits.
func Run(endpoint string, initial telemetry.Kind) error {
	if endpoint == "" {
		endpoint = "ws://127.0.0.1:12001"
	}
	if u, err := url.Parse(endpoint); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid endpoint %q: %v", endpoint, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := transport.Dial(ctx, endpoint, "http://localhost/", &transport.Config{
		PingInterval: 30 * time.Second,
		Logger:       log.New(os.Stderr, "[transport] ", log.LstdFlags),
	})
	if err != nil {
		cancel()
		return err
	}

	m := model{
		stream:    stream,
		ctxCancel: cancel,
		spinner:   spinner.New(),
		help:      help.New(),
		active:    initial,
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
