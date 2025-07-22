package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.design/x/clipboard"

	"github.com/jwafle/otail/internal/telemetry"
	"github.com/jwafle/otail/internal/transport"
)

// cursorBuffer is the number of lines to keep between the cursor and the edge of the viewport while navigating.
const cursorBuffer = 3

// Model is the Bubble Tea model driving the UI.
type Model struct {
	stream *transport.Stream
	cancel context.CancelFunc

	spinner spinner.Model
	help    help.Model
	ready   bool
	paused  bool

	viewport Viewport

	cur    cursor
	store  messageStore
	Active telemetry.Kind

	err error
}

func newModel(stream *transport.Stream, cancel context.CancelFunc, active telemetry.Kind) Model {
	return Model{
		stream:  stream,
		cancel:  cancel,
		spinner: spinner.New(),
		help:    help.New(),
		Active:  active,
	}
}

func (m *Model) activeMessages() []telemetry.Message {
	return m.store.Messages(m.Active)
}

func (m *Model) totalLines() int {
	return m.store.TotalLines(m.Active)
}

func (m *Model) cursorMsgIndex() int {
	line := 0
	msgs := m.activeMessages()
	for i, msg := range msgs {
		if m.cur.line < line+len(msg.IndentedLines) {
			return i
		}
		line += len(msg.IndentedLines)
	}
	if len(msgs) == 0 {
		return 0
	}
	return len(msgs) - 1
}

func (m *Model) ensureCursorVisible() {
	if !m.paused {
		return
	}
	if m.cur.line < m.viewport.YOffset {
		m.viewport.SetYOffset(m.cur.line)
	} else if m.cur.line >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(m.cur.line - m.viewport.Height + 1)
	}
}

func (m *Model) cursorUp() {
	if m.cur.line == 0 {
		return
	}
	m.cur.line--
	if m.cur.line < m.viewport.YOffset+cursorBuffer && !m.viewport.AtTop() {
		m.viewport.SetYOffset(m.viewport.YOffset - 1)
	}
}

func (m *Model) cursorDown() {
	if m.cur.line >= m.totalLines()-1 {
		return
	}
	m.cur.line++
	bottom := m.viewport.YOffset + m.viewport.VisibleLineCount() - cursorBuffer
	if m.cur.line >= bottom && !m.viewport.AtBottom() {
		m.viewport.SetYOffset(m.viewport.YOffset + 1)
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		readFrame(m.stream),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, Keys.Quit):
			m.cancel()
			return m, tea.Quit
		case key.Matches(msg, Keys.Logs):
			m.Active = telemetry.KindLogs
			m.syncViewport()
		case key.Matches(msg, Keys.Metrics):
			m.Active = telemetry.KindMetrics
			m.syncViewport()
		case key.Matches(msg, Keys.Traces):
			m.Active = telemetry.KindTraces
			m.syncViewport()
		case key.Matches(msg, Keys.Pause):
			m.paused = !m.paused
			if m.paused {
				m.cur.line = m.viewport.YOffset + m.viewport.VisibleLineCount() - 1
				if m.cur.line < 0 {
					m.cur.line = 0
				}
			}
		case m.paused && key.Matches(msg, Keys.Yank):
			if m.cur.msg == nil {
				return m, nil
			}
			clipboard.Write(clipboard.FmtText, []byte(strings.Join(m.cur.msg.IndentedLines, "\n")))
			return m, nil
		case m.paused && key.Matches(msg, m.viewport.KeyMap.Up):
			m.cursorUp()
			m.ensureCursorVisible()
			m.syncViewport()
			return m, nil
		case m.paused && key.Matches(msg, m.viewport.KeyMap.Down):
			m.cursorDown()
			m.ensureCursorVisible()
			m.syncViewport()
			return m, nil
		}
		var c tea.Cmd
		m.help, c = m.help.Update(msg)
		cmds = append(cmds, c)

	case tea.WindowSizeMsg:
		verticalMargin := 5
		if !m.ready {
			m.viewport = Viewport{viewport.New(msg.Width, msg.Height-verticalMargin)}
			m.ready = true
		} else {
			m.viewport.Width, m.viewport.Height = msg.Width, msg.Height-verticalMargin
		}
		m.syncViewport()

	case telemetry.Message:
		if !m.paused {
			m.store.Add(msg)
			m.viewport.GotoBottom()
			m.syncViewport()
		}
		cmds = append(cmds, readFrame(m.stream))

	case error:
		m.err = msg
		return m, tea.Quit

	case spinner.TickMsg:
		var c tea.Cmd
		m.spinner, c = m.spinner.Update(msg)
		cmds = append(cmds, c)
	}

	var c tea.Cmd
	oldOffset := m.viewport.YOffset
	viewport, c := m.viewport.Update(msg)
	m.viewport = Viewport{viewport}
	cmds = append(cmds, c)
	if m.paused {
		delta := m.viewport.YOffset - oldOffset
		if delta != 0 {
			m.cur.line += delta
		}
		if m.cur.line < 0 {
			m.cur.line = 0
		}
		if total := m.totalLines(); total > 0 && m.cur.line >= total {
			m.cur.line = total - 1
		}
		m.ensureCursorVisible()
		m.syncViewport()
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(m.RenderTabs())
	b.WriteString("\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")

	var status strings.Builder
	if m.paused {
		status.WriteString("[PAUSED] ")
	} else {
		status.WriteString(m.spinner.View())
		status.WriteString(" Streaming ")
	}
	status.WriteString(m.Active.String())
	b.WriteString(statusStyle.Render(status.String()))
	b.WriteString("\n")
	b.WriteString(m.help.View(Keys))

	return b.String()
}

func (m *Model) syncViewport() {
	src := m.store.Messages(m.Active)
	total := m.store.TotalLines(m.Active)
	if m.cur.line >= total {
		m.cur.line = total - 1
	}

	var b strings.Builder
	line := 0
	var current *telemetry.Message
	for i := range src {
		highlight := m.paused && i == m.cursorMsgIndex()
		for j, l := range src[i].IndentedLines {
			padded := l
			if highlight || (m.paused && line == m.cur.line) {
				if w := m.viewport.Width; w > 0 {
					if diff := w - lipgloss.Width(padded); diff > 0 {
						padded += strings.Repeat(" ", diff)
					}
				}
			}
			content := padded
			if m.paused && line == m.cur.line {
				content = highlightJSONKeys(content, cursorStyle, cursorJSONKeyStyle)
				current = &src[i]
			} else if highlight {
				content = highlightJSONKeys(content, msgHighlightStyle, msgHighlightJSONKeyStyle)
			}
			b.WriteString(content)
			line++
			if i < len(src)-1 || j < len(src[i].IndentedLines)-1 {
				b.WriteString("\n")
			}
		}
	}
	m.cur.msg = current
	m.viewport.SetContent(b.String())
}
