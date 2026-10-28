package config

import "github.com/5Sigma/pmx/internal/ui/styles"

func (t Theme) Palette() styles.ColorPalette {
	return styles.ColorPalette{
		Primary:     t.Primary,
		Accent:      t.Accent,
		Muted:       t.Muted,
		Fg:          t.Fg,
		DimFg:       t.DimFg,
		Error:       t.Error,
		Warning:     t.Warning,
		Success:     t.Success,
		SelectionBg: t.SelectionBg,
		SelectionFg: t.SelectionFg,
		UnreadFg:    t.UnreadFg,
	}
}
