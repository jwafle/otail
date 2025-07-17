package app

import (
	"strings"

	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/ui/common"
)

// item represents a single telemetry payload with pre-split lines for display.
type item struct {
	telemetry.Message
	Styled string   // Highlighted, prettified JSON
	Lines  []string // Styled split lines for viewport rendering
}

func newItem(m telemetry.Message) item {
	styled := common.HighlightKeys(m.Pretty)
	return item{Message: m, Styled: styled, Lines: strings.Split(styled, "\n")}
}
