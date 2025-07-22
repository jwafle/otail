package ui

import "github.com/jwafle/otail/internal/telemetry"

// cursor tracks the currently selected line when paused.
type cursor struct {
	line int
	msg  *telemetry.Message
}

func (c *cursor) reset() {
	c.line = 0
	c.msg = nil
}
