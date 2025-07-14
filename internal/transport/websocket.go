package transport

import (
	"context"
	"errors"
	"io"
	"log"
	"net/url"
	"time"

	"golang.org/x/net/websocket"
)

// Stream exposes a read-only frame channel plus an error stream.
type Stream struct {
	msgCh  chan []byte // never closed by user code
	errCh  chan error  // unrecoverable faults
	cancel context.CancelFunc
}

// Messages returns the channel on which callers receive raw frames.
func (s *Stream) Messages() <-chan []byte { return s.msgCh }

// Errors returns the error stream. A fatal error is *also* followed by
// closing msgCh, so callers should select on both.
func (s *Stream) Errors() <-chan error { return s.errCh }

// Close cancels the underlying context and shuts the channels.
func (s *Stream) Close() { s.cancel() }

// --------------------------------------------------------------------

// Config tweaks behaviour; zero-value is sane.
type Config struct {
	PingInterval time.Duration // 0 = no pings
	BaseBackoff  time.Duration // default 500 ms
	MaxBackoff   time.Duration // default 30 s
	Logger       *log.Logger   // nil = discard
}

// Dial starts a background goroutine that
//   - dials endpoint (with Origin header)
//   - pipes frames into Stream.msgCh
//   - auto-reconnects with exponential back-off
func Dial(ctx context.Context, endpoint, origin string, cfg *Config) (*Stream, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.BaseBackoff == 0 {
		cfg.BaseBackoff = 500 * time.Millisecond
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	// Validate URL up-front.
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("transport: invalid websocket endpoint")
	}

	ctx, cancel := context.WithCancel(ctx)
	s := &Stream{
		msgCh:  make(chan []byte, 1024),
		errCh:  make(chan error, 1), // buffer so goroutine can exit
		cancel: cancel,
	}

	go func() {
		defer func() {
			cancel()
			close(s.msgCh)
			close(s.errCh)
		}()

		backoffAttempt := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			c, err := websocket.Dial(endpoint, "", origin)
			if err != nil {
				delay := backoff(backoffAttempt, cfg.BaseBackoff, cfg.MaxBackoff)
				logger.Printf("dial error: %v (retry in %s)", err, delay)
				time.Sleep(delay)
				backoffAttempt++
				continue
			}
			backoffAttempt = 0 // successful dial → reset

			// Optional: keep-alive pings.
			if cfg.PingInterval > 0 {
				go pingLoop(ctx, c, cfg.PingInterval, logger)
			}

			if err = readLoop(ctx, c, s.msgCh); err != nil {
				// Connection dropped – try again unless context cancelled.
				if ctx.Err() == nil {
					logger.Printf("read loop ended: %v", err)
					// next iteration will redial
				} else {
					s.errCh <- err
					return
				}
			}
		}
	}()

	return s, nil
}

// --------------------------------------------------------------------
// Internal helpers

// readLoop blocks, copying frames to out until EOF or ctx.Done().
func readLoop(ctx context.Context, c *websocket.Conn, out chan<- []byte) error {
	defer c.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var frame []byte
		if err := websocket.Message.Receive(c, &frame); err != nil {
			return err // includes io.EOF on clean close
		}
		// Non-blocking send; drop frame if no reader (paused UI).
		select {
		case out <- frame:
		default:
		}
	}
}

// pingLoop sends a WebSocket Ping every interval until ctx.Done().
func pingLoop(ctx context.Context, c *websocket.Conn, interval time.Duration, l *log.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// RFC-6455 ping frame is opcode 0x9 with zero-length payload.
			// x/net/websocket doesn’t expose opcodes, but WriteControlMsg = 0x9.
			if err := c.SetDeadline(time.Now().Add(interval)); err == nil {
				if _, err := c.Write([]byte{0x89, 0}); err != nil {
					l.Printf("ping failed: %v", err)
					return
				}
			}
		}
	}
}
