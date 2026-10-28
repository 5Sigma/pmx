package ui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openInDefault launches the system default handler for path, detached from
// the current TTY so the bubbletea render loop is not blocked.
var openFn = openInDefault

func openInDefault(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
