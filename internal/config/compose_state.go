package config

import (
	"encoding/json"
	"errors"
	"os"
)

// ComposeState holds small pieces of compose-UI state persisted across sessions.
type ComposeState struct {
	LastMarkdownTheme string `json:"last_markdown_theme"`
}

// LoadComposeState reads the compose state file. Returns a zero-value struct
// if the file doesn't exist or can't be parsed — never returns an error for
// a missing file.
func LoadComposeState() (ComposeState, error) {
	path, err := ComposeStatePath()
	if err != nil {
		return ComposeState{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ComposeState{}, nil
	}
	if err != nil {
		return ComposeState{}, err
	}
	var s ComposeState
	if err := json.Unmarshal(data, &s); err != nil {
		return ComposeState{}, nil // treat malformed state as empty
	}
	return s, nil
}

// SaveComposeState writes the compose state atomically (temp file + rename).
func SaveComposeState(s ComposeState) error {
	path, err := ComposeStatePath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
