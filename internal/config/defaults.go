package config

import "time"

func Default() *Config {
	return &Config{
		Daemon: Daemon{
			CheckInterval:        15 * time.Minute,
			NotificationsEnabled: true,
			NotifyFolders:        []string{"inbox"},
		},
		General: General{
			DefaultAccount:  "",
			LogLevel:        "info",
			PollInterval:    2 * time.Minute,
			SweepInterval:   5 * time.Minute,
			SendInterval:    1 * time.Minute,
			SendMaxAttempts: 5,
		},
		Theme: Theme{
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
		},
		Features: Features{
			AltScreen:     true,
			Mouse:         true,
			FilterEnabled: true,
			ShowStatusBar: true,
			ConfirmQuit:   false,
		},
		Keys: Keys{
			Quit:       []string{"q", "ctrl+c"},
			Open:       []string{"enter"},
			Back:       []string{"esc", "backspace"},
			ScrollUp:   []string{"up", "k"},
			ScrollDown: []string{"down", "j"},
			Filter:     []string{"/"},
			Refresh:    []string{"r"},
		},
		Accounts: []Account{},
	}
}

// applyAccountDefaults fills in per-account fields that cannot be seeded via
// the top-level defaultsMap because koanf replaces the entire accounts slice
// rather than merging element-by-element.
func applyAccountDefaults(cfg *Config) {
	for i := range cfg.Accounts {
		if cfg.Accounts[i].OAuthTenant == "" {
			cfg.Accounts[i].OAuthTenant = "common"
		}
	}
}

// defaultsMap returns the default config as a nested map[string]any suitable
// for seeding koanf via confmap. Using a literal map avoids reflection/JSON
// round-trip surprises with koanf key naming.
func defaultsMap() map[string]any {
	d := Default()
	return map[string]any{
		"daemon": map[string]any{
			"check_interval":        d.Daemon.CheckInterval.String(),
			"notifications_enabled": d.Daemon.NotificationsEnabled,
			"notify_folders":        d.Daemon.NotifyFolders,
		},
		"general": map[string]any{
			"default_account":   d.General.DefaultAccount,
			"log_level":         d.General.LogLevel,
			"poll_interval":     d.General.PollInterval.String(),
			"sweep_interval":    d.General.SweepInterval.String(),
			"send_interval":     d.General.SendInterval.String(),
			"send_max_attempts": d.General.SendMaxAttempts,
		},
		"theme": map[string]any{
			"primary":      d.Theme.Primary,
			"accent":       d.Theme.Accent,
			"muted":        d.Theme.Muted,
			"fg":           d.Theme.Fg,
			"dim_fg":       d.Theme.DimFg,
			"error":        d.Theme.Error,
			"warning":      d.Theme.Warning,
			"success":      d.Theme.Success,
			"selection_bg": d.Theme.SelectionBg,
			"selection_fg": d.Theme.SelectionFg,
			"unread_fg":    d.Theme.UnreadFg,
		},
		"features": map[string]any{
			"alt_screen":      d.Features.AltScreen,
			"mouse":           d.Features.Mouse,
			"filter_enabled":  d.Features.FilterEnabled,
			"show_status_bar": d.Features.ShowStatusBar,
			"confirm_quit":    d.Features.ConfirmQuit,
		},
		"keys": map[string]any{
			"quit":        d.Keys.Quit,
			"open":        d.Keys.Open,
			"back":        d.Keys.Back,
			"scroll_up":   d.Keys.ScrollUp,
			"scroll_down": d.Keys.ScrollDown,
			"filter":      d.Keys.Filter,
			"refresh":     d.Keys.Refresh,
		},
		"accounts": []any{},
	}
}
