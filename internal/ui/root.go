package ui

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.design/x/clipboard"

	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/transport"
)

/* ------------------------------------------------------------------ */
/* Key-bindings & shared styles                                       */
/* ------------------------------------------------------------------ */

var statusStyle = lipgloss.NewStyle().Foreground(
	lipgloss.AdaptiveColor{Light: "#909090", Dark: "#626262"},
)

var msgHighlightStyle = lipgloss.NewStyle().
	Background(lipgloss.AdaptiveColor{Light: "#404040", Dark: "#303030"})
var msgHighlightJsonKeyStyle = msgHighlightStyle.Bold(true).Foreground(lipgloss.Color("214"))

var cursorStyle = msgHighlightStyle.Reverse(true)
var cursorJsonKeyStyle = cursorStyle.Bold(true).Foreground(lipgloss.Color("214"))

// jsonKeyRegex matches JSON keys (quoted key followed by colon).
var jsonKeyRegex = regexp.MustCompile(`"[^"\\]*"\s*:`)

// highlightJsonKeys applies baseStyle to all non-key text and keyStyle to JSON keys in the string.
func highlightJsonKeys(s string, baseStyle, keyStyle lipgloss.Style) string {
	var b strings.Builder
	last := 0
	for _, loc := range jsonKeyRegex.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		if last < start {
			b.WriteString(baseStyle.Render(s[last:start]))
		}
		b.WriteString(keyStyle.Render(s[start:end]))
		last = end
	}
	if last < len(s) {
		b.WriteString(baseStyle.Render(s[last:]))
	}
	return b.String()
}

// Tabs --------------------------------------------------------------------

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
	viewport Viewport

	/* cursor */
	cursorLine int
	cursorMsg  *telemetry.Message

	/* data */
	logs, metrics, traces []telemetry.Message
	Active                telemetry.Kind

	/* error handling */
	err error
}

func (m *model) activeMessages() []telemetry.Message {
	switch m.Active {
	case telemetry.KindMetrics:
		return m.metrics
	case telemetry.KindTraces:
		return m.traces
	default:
		return m.logs
	}
}

func (m *model) totalLines() int {
	msgs := m.activeMessages()
	n := 0
	for _, it := range msgs {
		n += len(it.IndentedLines)
	}
	return n
}

func (m *model) cursorMsgIndex() int {
	line := 0
	msgs := m.activeMessages()
	for i, msg := range msgs {
		if m.cursorLine < line+len(msg.IndentedLines) {
			return i
		}
		line += len(msg.IndentedLines)
	}
	if len(msgs) == 0 {
		return 0
	}
	return len(msgs) - 1
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
		case key.Matches(msg, Keys.Quit):
			m.ctxCancel() // stop transport goroutine
			return m, tea.Quit
		case key.Matches(msg, Keys.Logs):
			m.Active = telemetry.KindLogs
			m.syncViewport()
		case key.Matches(msg, Keys.Metrics):
			m.Active = telemetry.KindMetrics
			m.syncViewport()
		case key.Matches(msg, Keys.Traces):
			m.Active = telemetry.KindTraces
			m.syncViewport()
		case key.Matches(msg, Keys.Pause):
			m.paused = !m.paused
			if m.paused {
				m.cursorLine = m.viewport.YOffset + m.viewport.VisibleLineCount() - 1
				if m.cursorLine < 0 {
					m.cursorLine = 0
				}
			}
		case m.paused && key.Matches(msg, Keys.Yank):
			if m.cursorMsg == nil {
				return m, nil // nothing to yank
			}
			clipboard.Write(clipboard.FmtText, []byte(strings.Join(m.cursorMsg.IndentedLines, "\n")))
			return m, nil
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
			m.viewport = Viewport{viewport.New(msg.Width, msg.Height-verticalMargin)}
			m.ready = true
		} else {
			m.viewport.Width, m.viewport.Height = msg.Width, msg.Height-verticalMargin
		}
		m.syncViewport()

	case telemetry.Message:
		if !m.paused {
			switch msg.Kind {
			case telemetry.KindMetrics:
				m.metrics = append(m.metrics, msg)
			case telemetry.KindTraces:
				m.traces = append(m.traces, msg)
			default:
				m.logs = append(m.logs, msg)
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
	viewport, c := m.viewport.Update(msg)
	m.viewport = Viewport{viewport}
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

	b.WriteString(m.RenderTabs())
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	var status strings.Builder
	if m.paused {
		status.WriteString("[PAUSED] ")
	} else {
		status.WriteString(m.spinner.View())
		status.WriteString(" Streaming ")
	}
	status.WriteString(m.Active.String())
	b.WriteString(statusStyle.Render(status.String()))
	b.WriteString("\n")
	b.WriteString(m.help.View(Keys))

	return b.String()
}

/* ------------- helpers -------------------------------------------- */

func (m *model) syncViewport() {
	var src []telemetry.Message
	switch m.Active {
	case telemetry.KindMetrics:
		src = m.metrics
	case telemetry.KindTraces:
		src = m.traces
	default:
		src = m.logs
	}
	total := 0
	for _, msg := range src {
		total += len(msg.IndentedLines)
	}
	if m.cursorLine >= total {
		m.cursorLine = total - 1
	}

	var b strings.Builder
	line := 0
	var current *telemetry.Message
	for i := range src {
		highlight := m.paused && i == m.cursorMsgIndex()
		for j, l := range src[i].IndentedLines {
			padded := l
			if highlight || (m.paused && line == m.cursorLine) {
				if w := m.viewport.Width; w > 0 {
					if diff := w - lipgloss.Width(padded); diff > 0 {
						padded += strings.Repeat(" ", diff)
					}
				}
			}
			content := padded
			if m.paused && line == m.cursorLine {
				content = highlightJsonKeys(content, cursorStyle, cursorJsonKeyStyle)
				current = &src[i]
			} else if highlight {
				content = highlightJsonKeys(content, msgHighlightStyle, msgHighlightJsonKeyStyle)
			}
			b.WriteString(content)
			line++
			if i < len(src)-1 || j < len(src[i].IndentedLines)-1 {
				b.WriteString("\n")
			}
		}
	}
	m.cursorMsg = current
	m.viewport.SetContent(b.String())
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
		Active:    initial,
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
