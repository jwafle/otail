package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/net/websocket"

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

type model struct {
	conn    *websocket.Conn
	spinner spinner.Model

	logs    []string
	metrics []string
	traces  []string

	active streamKind
	err    error
}

func newModel(conn *websocket.Conn, initial streamKind) model {
	sp := spinner.New()
	return model{conn: conn, spinner: sp, active: initial}
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
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "l":
			m.active = streamLogs
		case "m":
			m.active = streamMetrics
		case "t":
			m.active = streamTraces
		}
	case otelMsg:
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
	b.WriteString(m.spinner.View())
	b.WriteString(" Streaming ")
	b.WriteString(m.active.String())
	b.WriteString(". Press l/m/t to switch, q to quit.\n\n")

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

	url := "ws://127.0.0.1:12001"
	origin := "http://localhost/"
	conn, err := websocket.Dial(url, "", origin)
	if err != nil {
		log.Fatalf("failed to connect to %s: %v", url, err)
	}
	if _, err := tea.NewProgram(newModel(conn, initial)).Run(); err != nil {
		fmt.Println("Error running program:", err)
	}
}
