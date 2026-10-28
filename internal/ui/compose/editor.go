package compose

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// EditorDoneMsg is returned by tea.ExecProcess after the external editor exits.
type EditorDoneMsg struct {
	Body string
	Err  error
}

// LaunchEditor suspends the TUI, opens path in $EDITOR (fallback: $VISUAL, vi),
// reads the file back, and emits EditorDoneMsg. Uses tea.ExecProcess so the
// bubbletea program correctly restores the terminal after the editor exits.
func LaunchEditor(path string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	c := exec.Command(parts[0], append(parts[1:], path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err != nil {
			return EditorDoneMsg{Err: err}
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return EditorDoneMsg{Err: readErr}
		}
		return EditorDoneMsg{Body: string(b)}
	})
}
