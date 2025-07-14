// Package pretty adds terminal styling helpers for telemetry payloads.
package common

import (
	"regexp"

	"github.com/charmbracelet/lipgloss"
)

// keyRe matches a JSON key emitted by json.MarshalIndent, e.g. "level":
var keyRe = regexp.MustCompile(`"([^"]+)":`)

// keyStyle is bold + amber (ANSI 256 colour 214). Change it however you like.
var keyStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("214"))

/*
HighlightKeys colours every JSON key and leaves values untouched.

Input must already be valid, indented JSON (the output of json.MarshalIndent
or telemetry.Parse’s Pretty field).  The function is side-effect-free and
idempotent.
*/
func HighlightKeys(src string) string {
	return keyRe.ReplaceAllStringFunc(src, func(m string) string {
		// keyRe’s first sub-match is the key text without quotes.
		sub := keyRe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		// Re-assemble `"key":` with styling applied to the key only.
		return keyStyle.Render(`"`+sub[1]+`"`) + ":"
	})
}
