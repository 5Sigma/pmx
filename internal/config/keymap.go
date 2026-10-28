package config

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type Keymap struct {
	Quit       []string
	Open       []string
	Back       []string
	ScrollUp   []string
	ScrollDown []string
	Filter     []string
	Refresh    []string
}

func (c *Config) Keymap() Keymap {
	return Keymap{
		Quit:       c.Keys.Quit,
		Open:       c.Keys.Open,
		Back:       c.Keys.Back,
		ScrollUp:   c.Keys.ScrollUp,
		ScrollDown: c.Keys.ScrollDown,
		Filter:     c.Keys.Filter,
		Refresh:    c.Keys.Refresh,
	}
}

// Matches reports whether the key message matches any binding for the named action.
func (k Keymap) Matches(action string, msg tea.KeyMsg) bool {
	bindings := k.bindingsFor(action)
	key := msg.String()
	for _, b := range bindings {
		if b == key {
			return true
		}
	}
	return false
}

// Hint returns the first binding string for the action (for status bar display).
func (k Keymap) Hint(action string) string {
	bs := k.bindingsFor(action)
	if len(bs) == 0 {
		return ""
	}
	return bs[0]
}

// StatusLine builds a compact status bar string from (action, label) pairs.
func (k Keymap) StatusLine(pairs ...[2]string) string {
	var sb strings.Builder
	for _, p := range pairs {
		action, label := p[0], p[1]
		hint := k.Hint(action)
		if hint == "" {
			hint = action
		}
		fmt.Fprintf(&sb, "  %s %s", hint, label)
	}
	return sb.String()
}

func (k Keymap) bindingsFor(action string) []string {
	switch action {
	case "quit":
		return k.Quit
	case "open":
		return k.Open
	case "back":
		return k.Back
	case "scroll_up":
		return k.ScrollUp
	case "scroll_down":
		return k.ScrollDown
	case "filter":
		return k.Filter
	case "refresh":
		return k.Refresh
	}
	return nil
}
