package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"net/url"
	"os"
	"strings"

	"golang.org/x/net/websocket"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	plog "go.opentelemetry.io/collector/pdata/plog"
	pmetric "go.opentelemetry.io/collector/pdata/pmetric"
	ptrace "go.opentelemetry.io/collector/pdata/ptrace"
)

// otelMsg represents a chunk of telemetry data received over the websocket.
type otelMsg struct {
	Data []byte
}

// errMsg is returned when the websocket reader encounters an error.
type errMsg struct{ error }

// model is the Bubble Tea model for the application.
type streamKind int

const (
	streamLogs streamKind = iota
	streamMetrics
	streamTraces
)

func (s streamKind) String() string {
	switch s {
	case streamMetrics:
		return "metrics"
	case streamTraces:
		return "traces"
	default:
		return "logs"
	}
}

var statusStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
	Light: "#909090",
	Dark:  "#626262",
})

type keyMap struct {
	Logs    key.Binding
	Metrics key.Binding
	Traces  key.Binding
	Pause   key.Binding
	Quit    key.Binding
}

var keys = keyMap{
	Logs:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
	Metrics: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "metrics")),
	Traces:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "traces")),
	Pause:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type model struct {
	conn    *websocket.Conn
	spinner spinner.Model
	help    help.Model

	viewport viewport.Model
	ready    bool

	paused bool

	logs    []string
	metrics []string
	traces  []string

	active streamKind
	err    error
}

func (m model) ShortHelp() []key.Binding {
	return []key.Binding{keys.Logs, keys.Metrics, keys.Traces, keys.Pause, keys.Quit}
}

func (m model) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keys.Logs, keys.Metrics, keys.Traces, keys.Pause, keys.Quit}}
}
func newModel(conn *websocket.Conn, initial streamKind) model {
	sp := spinner.New()
	return model{conn: conn, spinner: sp, help: help.New(), active: initial, viewport: viewport.Model{}}
}

// listenForMessage waits for a single message from the websocket.
func listenForMessage(conn *websocket.Conn) tea.Cmd {
	return func() tea.Msg {
		var msg []byte
		if err := websocket.Message.Receive(conn, &msg); err != nil {
			return errMsg{err}
		}
		return otelMsg{Data: msg}
	}
}

func categorize(data []byte) (streamKind, string) {
	// try logs
	if logs, err := (&plog.JSONUnmarshaler{}).UnmarshalLogs(data); err == nil && logs.ResourceLogs().Len() > 0 {
		b, err := (&plog.JSONMarshaler{}).MarshalLogs(logs)
		if err == nil {
			var pretty interface{}
			if json.Unmarshal(b, &pretty) == nil {
				pb, _ := json.MarshalIndent(pretty, "", "  ")
				return streamLogs, string(pb)
			}
			return streamLogs, string(b)
		}
	}

	if metrics, err := (&pmetric.JSONUnmarshaler{}).UnmarshalMetrics(data); err == nil && metrics.ResourceMetrics().Len() > 0 {
		b, err := (&pmetric.JSONMarshaler{}).MarshalMetrics(metrics)
		if err == nil {
			var pretty interface{}
			if json.Unmarshal(b, &pretty) == nil {
				pb, _ := json.MarshalIndent(pretty, "", "  ")
				return streamMetrics, string(pb)
			}
			return streamMetrics, string(b)
		}
	}

	if traces, err := (&ptrace.JSONUnmarshaler{}).UnmarshalTraces(data); err == nil && traces.ResourceSpans().Len() > 0 {
		b, err := (&ptrace.JSONMarshaler{}).MarshalTraces(traces)
		if err == nil {
			var pretty interface{}
			if json.Unmarshal(b, &pretty) == nil {
				pb, _ := json.MarshalIndent(pretty, "", "  ")
				return streamTraces, string(pb)
			}
			return streamTraces, string(b)
		}
	}

	// unknown data, return as logs by default
	return streamLogs, string(data)
}

func (m *model) syncViewportContent() {
	var msgs []string
	switch m.active {
	case streamMetrics:
		msgs = m.metrics
	case streamTraces:
		msgs = m.traces
	default:
		msgs = m.logs
	}
	m.viewport.SetContent(strings.Join(msgs, "\n"))
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, listenForMessage(m.conn))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Logs):
			m.active = streamLogs
			m.syncViewportContent()
		case key.Matches(msg, keys.Metrics):
			m.active = streamMetrics
			m.syncViewportContent()
		case key.Matches(msg, keys.Traces):
			m.active = streamTraces
			m.syncViewportContent()
		case key.Matches(msg, keys.Pause):
			m.paused = !m.paused
		}
		var c tea.Cmd
		m.help, c = m.help.Update(msg)
		cmds = append(cmds, c)
	case tea.WindowSizeMsg:
		verticalMarginHeight := 2
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = 0
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
		}
		m.syncViewportContent()
	case otelMsg:
		if !m.paused {
			kind, pretty := categorize(msg.Data)
			switch kind {
			case streamMetrics:
				m.metrics = append(m.metrics, pretty)
			case streamTraces:
				m.traces = append(m.traces, pretty)
			default:
				m.logs = append(m.logs, pretty)
			}
			m.syncViewportContent()
		}
		cmds = append(cmds, listenForMessage(m.conn))
	case errMsg:
		m.err = msg.error
		return m, tea.Quit
	case spinner.TickMsg:
		var c tea.Cmd
		m.spinner, c = m.spinner.Update(msg)
		cmds = append(cmds, c)
	}

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
	var statusLine strings.Builder
	if m.paused {
		statusLine.WriteString("[PAUSED] ")
	} else {
		statusLine.WriteString(m.spinner.View())
		statusLine.WriteString(" Streaming ")
	}
	statusLine.WriteString(m.active.String())
	b.WriteString(statusStyle.Render(statusLine.String()))
	b.WriteString("\n")
	b.WriteString(m.help.View(m))
	return b.String()
}

func main() {
	initial := streamLogs
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "logs":
			initial = streamLogs
		case "metrics":
			initial = streamMetrics
		case "traces":
			initial = streamTraces
		default:
			fmt.Println("usage: otail [logs|metrics|traces]")
			return
		}
	}

	endpoint := "ws://127.0.0.1:12001"
	flag.StringVar(&endpoint, "endpoint", endpoint, "websocket endpoint")
	flag.StringVar(&endpoint, "e", endpoint, "websocket endpoint")
	flag.Parse()

	if u, err := url.Parse(endpoint); err != nil || u.Scheme == "" || u.Host == "" {
		log.Fatalf("invalid endpoint %q: %v", endpoint, err)
	}

	origin := "http://localhost/"
	conn, err := websocket.Dial(endpoint, "", origin)
	if err != nil {
		log.Fatalf("failed to connect to %s: %v", endpoint, err)
	}
	if _, err := tea.NewProgram(newModel(conn, initial)).Run(); err != nil {
		fmt.Println("Error running program:", err)
	}
}
