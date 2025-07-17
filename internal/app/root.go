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
	HalfDown, HalfUp      key.Binding
}

var keys = keyMap{
	Logs:     key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
	Metrics:  key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "metrics")),
	Traces:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "traces")),
	Pause:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Yank:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yank to clipboard")),
	HalfDown: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("^D", "half page down")),
	HalfUp:   key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^U", "half page up")),
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Logs,
		k.Metrics,
		k.Traces,
		k.Pause,
		k.HalfDown,
		k.HalfUp,
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
			k.HalfDown,
			k.HalfUp,
			k.Quit,
			k.Yank,
		},
	}
}

var statusStyle = lipgloss.NewStyle().Foreground(
	lipgloss.AdaptiveColor{Light: "#909090", Dark: "#626262"},
)

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

	/* data */
	logs, metrics, traces *store
	active                telemetry.Kind

	/* cursor */
	cursorLine int
	cursorMsg  *telemetry.Message

	/* error handling */
	err error
}

func (m *model) activeStore() *store {
	switch m.active {
	case telemetry.KindMetrics:
		return m.metrics
	case telemetry.KindTraces:
		return m.traces
	default:
		return m.logs
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
				st := m.activeStore()
				if st != nil {
					last := m.viewport.YOffset + m.viewport.Height - 1
					if last >= len(st.lines) {
						last = len(st.lines) - 1
					}
					if last < 0 {
						last = 0
					}
					m.cursorLine = last
					if e := st.messageForLine(last); e != nil {
						m.cursorMsg = &e.Msg
					} else {
						m.cursorMsg = nil
					}
				}
			} else {
				m.cursorMsg = nil
			}
			m.syncViewport()
		case key.Matches(msg, keys.HalfDown):
			if m.paused {
				move := m.viewport.Height / 2
				m.viewport.LineDown(move)
				m.cursorLine += move
				st := m.activeStore()
				if m.cursorLine >= len(st.lines) {
					m.cursorLine = len(st.lines) - 1
				}
				if e := st.messageForLine(m.cursorLine); e != nil {
					m.cursorMsg = &e.Msg
				}
				m.syncViewport()
			}
		case key.Matches(msg, keys.HalfUp):
			if m.paused {
				move := m.viewport.Height / 2
				m.viewport.LineUp(move)
				m.cursorLine -= move
				if m.cursorLine < 0 {
					m.cursorLine = 0
				}
				st := m.activeStore()
				if e := st.messageForLine(m.cursorLine); e != nil {
					m.cursorMsg = &e.Msg
				}
				m.syncViewport()
			}
		}
		var c tea.Cmd
		m.help, c = m.help.Update(msg)
		cmds = append(cmds, c)

	case tea.WindowSizeMsg:
		verticalMargin := 2
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMargin)
			m.ready = true
		} else {
			m.viewport.Width, m.viewport.Height = msg.Width, msg.Height-verticalMargin
		}
		m.syncViewport()

	case telemetry.Message:
		if !m.paused {
			switch msg.Kind {
			case telemetry.KindMetrics:
				m.metrics.append(msg)
			case telemetry.KindTraces:
				m.traces.append(msg)
			default:
				m.logs.append(msg)
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
	m.viewport, c = m.viewport.Update(msg)
	cmds = append(cmds, c)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	var b strings.Builder

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
	st := m.activeStore()
	if st == nil {
		m.viewport.SetContent("")
		return
	}
	m.viewport.SetContent(st.render(m.paused, m.cursorLine))
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
		logs:      newStore(),
		metrics:   newStore(),
		traces:    newStore(),
	}
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
