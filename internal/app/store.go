package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/ui/common"
)

// entry represents a piece of telemetry with its position in the global line list.
type entry struct {
	Msg   telemetry.Message
	Start int // inclusive
	End   int // exclusive
	lines []string
}

// store keeps incoming telemetry split into lines so we can map viewport lines
// back to their source message.
type store struct {
	entries []entry
	lines   []string
}

func newStore() *store { return &store{} }

func (s *store) append(msg telemetry.Message) {
	lines := strings.Split(common.HighlightKeys(msg.Pretty), "\n")
	start := len(s.lines)
	s.lines = append(s.lines, lines...)
	s.entries = append(s.entries, entry{Msg: msg, Start: start, End: start + len(lines), lines: lines})
}

func (s *store) content() string {
	return strings.Join(s.lines, "\n")
}

// messageForLine returns the entry containing the given line index or nil.
func (s *store) messageForLine(line int) *entry {
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if line >= e.Start && line < e.End {
			return &s.entries[i]
		}
	}
	return nil
}

var (
	cursorStyle  = lipgloss.NewStyle().Reverse(true)
	messageStyle = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#404040", Dark: "#333333"})
)

// render assembles the content with optional highlighting for the cursor and the
// message that contains it.
func (s *store) render(paused bool, cursorLine int) string {
	if !paused {
		return s.content()
	}
	var b strings.Builder
	for i, line := range s.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		e := s.messageForLine(cursorLine)
		if i == cursorLine {
			b.WriteString(cursorStyle.Render(line))
		} else if e != nil && i >= e.Start && i < e.End {
			b.WriteString(messageStyle.Render(line))
		} else {
			b.WriteString(line)
		}
	}
	return b.String()
}
