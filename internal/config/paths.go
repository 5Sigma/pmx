package config

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

func ConfigPath() (string, error) {
	return xdg.ConfigFile("pmx/config.toml")
}

// DataDir returns the XDG data directory for pmx, creating it if needed.
func DataDir() (string, error) {
	dir := filepath.Join(xdg.DataHome, "pmx")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// DatabasePath returns the path to the SQLite mail database.
func DatabasePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mail.db"), nil
}

// LogPath returns the path to the pmx log file.
func LogPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pmx.log"), nil
}

// AttachmentsDir returns the directory used to store attachment files,
// creating it if needed.
func AttachmentsDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	attachs := filepath.Join(dir, "attachments")
	if err := os.MkdirAll(attachs, 0700); err != nil {
		return "", err
	}
	return attachs, nil
}

// MarkdownThemesDir returns the directory where user markdown CSS themes are
// stored. The directory may not exist; callers should treat absence as "no
// user themes."
func MarkdownThemesDir() string {
	return filepath.Join(xdg.ConfigHome, "pmx", "markdown")
}

// ComposeStatePath returns the path to the compose UI state file.
func ComposeStatePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "compose_state.json"), nil
}

// UIStatePath returns the path to the pmx UI state file. The file holds
// cross-session UI preferences like the last-active account.
func UIStatePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ui_state.json"), nil
}

// PmxdLogPath returns the path to the pmxd log file.
func PmxdLogPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pmxd.log"), nil
}

// PmxdStatePath returns the path to the pmxd state file.
func PmxdStatePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pmxd-state.json"), nil
}
