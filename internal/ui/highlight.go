package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#909090", Dark: "#626262"})

	msgHighlightStyle        = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#404040", Dark: "#303030"})
	msgHighlightJSONKeyStyle = msgHighlightStyle.Bold(true).Foreground(lipgloss.Color("214"))

	cursorStyle        = msgHighlightStyle.Reverse(true)
	cursorJSONKeyStyle = cursorStyle.Bold(true).Foreground(lipgloss.Color("214"))

	jsonKeyRegex = regexp.MustCompile(`"[^"\\]*"\s*:`)
)

func highlightJSONKeys(s string, baseStyle, keyStyle lipgloss.Style) string {
	var b strings.Builder
	last := 0
	for _, loc := range jsonKeyRegex.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		if last < start {
			b.WriteString(baseStyle.Render(s[last:start]))
		}
		b.WriteString(keyStyle.Render(s[start:end]))
		last = end
	}
	if last < len(s) {
		b.WriteString(baseStyle.Render(s[last:]))
	}
	return b.String()
}
