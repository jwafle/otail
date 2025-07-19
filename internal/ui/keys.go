package ui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Logs, Metrics, Traces key.Binding
	Pause, Quit, Yank     key.Binding
}

var Keys = KeyMap{
	Logs:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "logs")),
	Metrics: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "metrics")),
	Traces:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "traces")),
	Pause:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pause")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Yank:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "yank to clipboard")),
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Logs,
		k.Metrics,
		k.Traces,
		k.Pause,
		k.Quit,
		k.Yank,
	}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			k.Logs,
			k.Metrics,
			k.Traces,
			k.Pause,
			k.Quit,
			k.Yank,
		},
	}
}
