// internal/telemetry/parser.go
package telemetry

import (
	"encoding/json"
	"fmt"
	"strings"

	plog "go.opentelemetry.io/collector/pdata/plog"
	pmetric "go.opentelemetry.io/collector/pdata/pmetric"
	ptrace "go.opentelemetry.io/collector/pdata/ptrace"
)

// Kind represents the high-level category of an incoming message.
type Kind int

const (
	KindLogs Kind = iota
	KindMetrics
	KindTraces
	KindUnknown
)

func (k Kind) String() string {
	switch k {
	case KindMetrics:
		return "metrics"
	case KindTraces:
		return "traces"
	case KindLogs:
		return "logs"
	default:
		return "unknown"
	}
}

// Message is the canonical form that UI and transport layers consume.
type Message struct {
	Kind          Kind     // logs, metrics, traces, or unknown
	IndentedLines []string // indented, parsed JSON for ui
}

// Parse inspects a raw websocket frame and classifies it.
// It never returns an error; unknown data are flagged as KindUnknown.
func Parse(data []byte) Message {
	// Helpers -------------------------------------------------------------

	pretty := func(b []byte) []string {
		var v interface{}
		// If we can re-indent nicely, do so; otherwise fall back.
		if json.Unmarshal(b, &v) == nil {
			if pb, err := json.MarshalIndent(v, "", "  "); err == nil {
				return strings.Split(string(pb), "\n")
			}
		}
		return []string{string(b)}
	}

	asMsg := func(kind Kind, raw []byte, marshal func() ([]byte, error)) Message {
		out, err := marshal()
		if err != nil {
			// Fallback: just show the incoming bytes.
			return Message{Kind: kind, IndentedLines: pretty(raw)}
		}
		return Message{Kind: kind, IndentedLines: pretty(out)}
	}

	// Logs ----------------------------------------------------------------
	if logs, err := (&plog.JSONUnmarshaler{}).UnmarshalLogs(data); err == nil &&
		logs.ResourceLogs().Len() > 0 {

		return asMsg(KindLogs, data, func() ([]byte, error) {
			return (&plog.JSONMarshaler{}).MarshalLogs(logs)
		})
	}

	// Metrics -------------------------------------------------------------
	if metrics, err := (&pmetric.JSONUnmarshaler{}).UnmarshalMetrics(data); err == nil &&
		metrics.ResourceMetrics().Len() > 0 {

		return asMsg(KindMetrics, data, func() ([]byte, error) {
			return (&pmetric.JSONMarshaler{}).MarshalMetrics(metrics)
		})
	}

	// Traces --------------------------------------------------------------
	if traces, err := (&ptrace.JSONUnmarshaler{}).UnmarshalTraces(data); err == nil &&
		traces.ResourceSpans().Len() > 0 {

		return asMsg(KindTraces, data, func() ([]byte, error) {
			return (&ptrace.JSONMarshaler{}).MarshalTraces(traces)
		})
	}

	// Unknown or malformed payload ---------------------------------------
	return Message{
		Kind:          KindUnknown,
		IndentedLines: pretty(data),
	}
}

// ErrUnsupportedKind can be returned by callers that need to reject unknown kinds.
var ErrUnsupportedKind = fmt.Errorf("unsupported message kind")
