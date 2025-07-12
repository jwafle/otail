package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"

	"golang.org/x/net/websocket"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// otelMsg represents a chunk of telemetry data received over the websocket.
type otelMsg struct {
	Data []byte
}

// errMsg is returned when the websocket reader encounters an error.
type errMsg struct{ error }

// model is the Bubble Tea model for the application.
type model struct {
	conn     *websocket.Conn
	spinner  spinner.Model
	messages []string
	err      error
}

func newModel(conn *websocket.Conn) model {
	sp := spinner.New()
	return model{conn: conn, spinner: sp}
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

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, listenForMessage(m.conn))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	case otelMsg:
		var v interface{}
		if err := json.Unmarshal(msg.Data, &v); err == nil {
			pretty, _ := json.MarshalIndent(v, "", "  ")
			m.messages = append(m.messages, string(pretty))
		} else {
			m.messages = append(m.messages, string(msg.Data))
		}
		if len(m.messages) > 20 {
			m.messages = m.messages[len(m.messages)-20:]
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
	b.WriteString(" Streaming telemetry. Press q to quit.\n\n")
	for _, msg := range m.messages {
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
	if _, err := tea.NewProgram(newModel(conn)).Run(); err != nil {
		fmt.Println("Error running program:", err)
	}
}
