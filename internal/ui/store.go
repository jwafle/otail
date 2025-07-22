package ui

import "github.com/jwafle/otail/internal/telemetry"

// messageStore keeps messages separated by kind.
type messageStore struct {
	logs    []telemetry.Message
	metrics []telemetry.Message
	traces  []telemetry.Message
}

func (s *messageStore) Add(m telemetry.Message) {
	switch m.Kind {
	case telemetry.KindMetrics:
		s.metrics = append(s.metrics, m)
	case telemetry.KindTraces:
		s.traces = append(s.traces, m)
	default:
		s.logs = append(s.logs, m)
	}
}

func (s *messageStore) Messages(k telemetry.Kind) []telemetry.Message {
	switch k {
	case telemetry.KindMetrics:
		return s.metrics
	case telemetry.KindTraces:
		return s.traces
	default:
		return s.logs
	}
}

func (s *messageStore) TotalLines(k telemetry.Kind) int {
	msgs := s.Messages(k)
	lines := 0
	for _, m := range msgs {
		lines += len(m.IndentedLines)
	}
	return lines
}
