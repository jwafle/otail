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
	tea "github.com/charmbracelet/bubbletea"

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
	return model{conn: conn, spinner: sp, help: help.New(), active: initial}
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

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, listenForMessage(m.conn))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Logs):
			m.active = streamLogs
		case key.Matches(msg, keys.Metrics):
			m.active = streamMetrics
		case key.Matches(msg, keys.Traces):
			m.active = streamTraces
		case key.Matches(msg, keys.Pause):
			m.paused = !m.paused
		}
		var cmd tea.Cmd
		m.help, cmd = m.help.Update(msg)
		return m, cmd
	case otelMsg:
		if !m.paused {
			kind, pretty := categorize(msg.Data)
			switch kind {
			case streamMetrics:
				m.metrics = append(m.metrics, pretty)
				if len(m.metrics) > 20 {
					m.metrics = m.metrics[len(m.metrics)-20:]
				}
			case streamTraces:
				m.traces = append(m.traces, pretty)
				if len(m.traces) > 20 {
					m.traces = m.traces[len(m.traces)-20:]
				}
			default:
				m.logs = append(m.logs, pretty)
				if len(m.logs) > 20 {
					m.logs = m.logs[len(m.logs)-20:]
				}
			}
		}
		return m, listenForMessage(m.conn)
	case errMsg:
		m.err = msg.error
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	if m.paused {
		b.WriteString("[PAUSED] ")
	} else {
		b.WriteString(m.spinner.View())
		b.WriteString(" Streaming ")
	}
	b.WriteString(m.active.String())
	b.WriteString("\n\n")

	var msgs []string
	switch m.active {
	case streamMetrics:
		msgs = m.metrics
	case streamTraces:
		msgs = m.traces
	default:
		msgs = m.logs
	}

	for _, msg := range msgs {
		b.WriteString(msg)
		b.WriteString("\n")
	}
	if m.err != nil {
		b.WriteString("error: ")
		b.WriteString(m.err.Error())
		b.WriteString("\n")
	}
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
