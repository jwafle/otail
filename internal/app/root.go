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
	"github.com/jwafle/otail/internal/ui/common"
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
	logs, metrics, traces []string
	active                telemetry.Kind

	/* error handling */
	err error
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
			stylizedMsg := common.HighlightKeys(msg.Pretty)
			switch msg.Kind {
			case telemetry.KindMetrics:
				m.metrics = append(m.metrics, stylizedMsg)
			case telemetry.KindTraces:
				m.traces = append(m.traces, stylizedMsg)
			default:
				m.logs = append(m.logs, stylizedMsg)
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
	var src []string
	switch m.active {
	case telemetry.KindMetrics:
		src = m.metrics
	case telemetry.KindTraces:
		src = m.traces
	default:
		src = m.logs
	}
	m.viewport.SetContent(strings.Join(src, "\n"))
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
