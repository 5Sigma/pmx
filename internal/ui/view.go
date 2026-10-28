package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/ui/keymap"
)

// View is implemented by every full-screen view pushed onto the Navigator stack.
//
// A View's StatusKeys() is derived from its KeyMap().Primary by the shell —
// implementations should return keyHintGroupFromKeyMap(v.KeyMap()) unless they
// need custom behaviour.
type View interface {
	Init() tea.Cmd
	Update(tea.Msg) (View, tea.Cmd)
	View() string
	SetSize(width, height int) View
	Title() string
	StatusKeys() string
	KeyMap() keymap.KeyMap
}

type pushMsg struct{ view View }
type popMsg struct{}

func Push(v View) tea.Cmd {
	return func() tea.Msg { return pushMsg{view: v} }
}

func Pop() tea.Cmd {
	return func() tea.Msg { return popMsg{} }
}
