package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jwafle/otail/internal/telemetry"
)

var (
	activeTabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      " ",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┘",
		BottomRight: "└",
	}

	tabBorder = lipgloss.Border{
		Top:         "─",
		Bottom:      "─",
		Left:        "│",
		Right:       "│",
		TopLeft:     "╭",
		TopRight:    "╮",
		BottomLeft:  "┴",
		BottomRight: "┴",
	}

	tabStyle = lipgloss.NewStyle().
			Border(tabBorder, true).
			BorderForeground(lipgloss.Color("214")).
			Padding(0, 1)

	activeTabStyle = tabStyle.Border(activeTabBorder, true)

	tabGap = tabStyle.
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false)
)

func (m Model) RenderTabs() string {
	tabs := []string{
		tabStyle.Render("Logs"),
		tabStyle.Render("Metrics"),
		tabStyle.Render("Traces"),
	}
	switch m.Active {
	case telemetry.KindMetrics:
		tabs[1] = activeTabStyle.Render("Metrics")
	case telemetry.KindTraces:
		tabs[2] = activeTabStyle.Render("Traces")
	default:
		tabs[0] = activeTabStyle.Render("Logs")
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	if m.viewport.Width > 0 {
		gapWidth := m.viewport.Width - lipgloss.Width(row)
		if gapWidth < 0 {
			gapWidth = 0
		}
		row = lipgloss.JoinHorizontal(lipgloss.Bottom,
			row,
			tabGap.Render(strings.Repeat(" ", gapWidth)),
		)
	}
	return row
}
