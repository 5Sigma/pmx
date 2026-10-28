package styles_test

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/ui/styles"
)

func TestBuild_ReturnsPopulatedTheme(t *testing.T) {
	p := styles.ColorPalette{
		Primary:     "#FF0000",
		Accent:      "#00FF00",
		Muted:       "#333333",
		Fg:          "#FFFFFF",
		DimFg:       "#777777",
		Error:       "#FF4444",
		Warning:     "#FFAA00",
		Success:     "#44FF44",
		SelectionBg: "#111122",
		SelectionFg: "#FFFFFF",
		UnreadFg:    "#00FF00",
	}

	theme := styles.Build(p)

	// Header should have the primary color as background.
	if theme.Header.GetBackground() != lipgloss.Color("#FF0000") {
		t.Errorf("Header.Background = %v, want %v", theme.Header.GetBackground(), lipgloss.Color("#FF0000"))
	}

	// StatusBar should use muted as background.
	if theme.StatusBar.GetBackground() != lipgloss.Color("#333333") {
		t.Errorf("StatusBar.Background = %v, want %v", theme.StatusBar.GetBackground(), lipgloss.Color("#333333"))
	}

	// Unread should use unread_fg.
	if theme.Unread.GetForeground() != lipgloss.Color("#00FF00") {
		t.Errorf("Unread.Foreground = %v, want %v", theme.Unread.GetForeground(), lipgloss.Color("#00FF00"))
	}

	// Error style should use error color.
	if theme.Error.GetForeground() != lipgloss.Color("#FF4444") {
		t.Errorf("Error.Foreground = %v, want %v", theme.Error.GetForeground(), lipgloss.Color("#FF4444"))
	}

	// SelectedTitle should use selection colors.
	if theme.SelectedTitle.GetBackground() != lipgloss.Color("#111122") {
		t.Errorf("SelectedTitle.Background = %v, want %v", theme.SelectedTitle.GetBackground(), lipgloss.Color("#111122"))
	}
}

func TestBuild_DefaultPaletteProducesNonZeroTheme(t *testing.T) {
	p := styles.ColorPalette{
		Primary:     "#7D56F4",
		Accent:      "#73F59F",
		Muted:       "#3C3C3C",
		Fg:          "#FAFAFA",
		DimFg:       "#888888",
		Error:       "#F7768E",
		Warning:     "#E0AF68",
		Success:     "#9ECE6A",
		SelectionBg: "#2A2A3C",
		SelectionFg: "#FAFAFA",
		UnreadFg:    "#73F59F",
	}

	theme := styles.Build(p)

	// Verify theme fields are set (non-zero lipgloss.Style has render capability).
	if theme.Header.GetBold() != true {
		t.Error("Header should be bold")
	}
	if theme.Unread.GetBold() != true {
		t.Error("Unread should be bold")
	}
	if theme.SelectedTitle.GetBold() != true {
		t.Error("SelectedTitle should be bold")
	}
}
