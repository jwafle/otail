package ui

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/transport"
)

// readFrame returns a command that receives one frame from the stream.
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

// Run creates the transport, spins up the Bubble Tea program, and blocks until the TUI exits.
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

	m := newModel(stream, cancel, initial)
	_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
