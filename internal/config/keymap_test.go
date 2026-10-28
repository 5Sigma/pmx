package config_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/config"
)

func defaultKeymap() config.Keymap {
	return config.Default().Keymap()
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestKeymap_Matches(t *testing.T) {
	km := defaultKeymap()

	tests := []struct {
		action string
		msg    tea.KeyMsg
		want   bool
	}{
		{"quit", keyMsg("q"), true},
		{"quit", keyMsg("x"), false},
		{"open", tea.KeyMsg{Type: tea.KeyEnter}, true},
		{"back", tea.KeyMsg{Type: tea.KeyEsc}, true},
		{"back", tea.KeyMsg{Type: tea.KeyBackspace}, true},
		{"scroll_up", keyMsg("k"), true},
		{"scroll_down", keyMsg("j"), true},
		{"filter", keyMsg("/"), true},
		{"refresh", keyMsg("r"), true},
		{"quit", keyMsg("j"), false},
	}

	for _, tt := range tests {
		got := km.Matches(tt.action, tt.msg)
		if got != tt.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", tt.action, tt.msg.String(), got, tt.want)
		}
	}
}

func TestKeymap_Hint(t *testing.T) {
	km := defaultKeymap()

	if got := km.Hint("quit"); got != "q" {
		t.Errorf("Hint(quit) = %q, want %q", got, "q")
	}
	if got := km.Hint("open"); got != "enter" {
		t.Errorf("Hint(open) = %q, want %q", got, "enter")
	}
	if got := km.Hint("unknown"); got != "" {
		t.Errorf("Hint(unknown) = %q, want empty", got)
	}
}

func TestKeymap_StatusLine(t *testing.T) {
	km := defaultKeymap()

	line := km.StatusLine(
		[2]string{"quit", "quit"},
		[2]string{"open", "open"},
		[2]string{"filter", "filter"},
	)

	if !strings.Contains(line, "q quit") {
		t.Errorf("StatusLine missing %q, got %q", "q quit", line)
	}
	if !strings.Contains(line, "enter open") {
		t.Errorf("StatusLine missing %q, got %q", "enter open", line)
	}
	if !strings.Contains(line, "/ filter") {
		t.Errorf("StatusLine missing %q, got %q", "/ filter", line)
	}
}

func TestKeymap_CustomBindings(t *testing.T) {
	cfg := config.Default()
	cfg.Keys.Quit = []string{"x", "ctrl+c"}
	km := cfg.Keymap()

	if !km.Matches("quit", keyMsg("x")) {
		t.Error("expected x to match quit")
	}
	if km.Matches("quit", keyMsg("q")) {
		t.Error("q should not match quit after override")
	}
	if km.Hint("quit") != "x" {
		t.Errorf("expected hint x, got %q", km.Hint("quit"))
	}
}
