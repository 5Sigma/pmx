package config

import (
	"encoding/json"
	"errors"
	"os"
)

// UIState holds cross-session UI preferences that are small enough to live
// outside config.toml. The file is written atomically on change and tolerates
// missing or malformed files by returning a zero value.
type UIState struct {
	LastActiveAccount string `json:"last_active_account"`
}

// LoadUIState reads the UI state file. Missing or malformed files yield a
// zero value and no error — callers should treat the state as advisory.
func LoadUIState() (UIState, error) {
	path, err := UIStatePath()
	if err != nil {
		return UIState{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return UIState{}, nil
	}
	if err != nil {
		return UIState{}, err
	}
	var s UIState
	if err := json.Unmarshal(data, &s); err != nil {
		return UIState{}, nil
	}
	return s, nil
}

// SaveUIState writes the UI state atomically (temp file + rename, 0600).
func SaveUIState(s UIState) error {
	path, err := UIStatePath()
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

// ResolveInitialAccount picks the account to activate at startup. The order
// of preference is:
//  1. state.LastActiveAccount, if it resolves in cfg.Accounts.
//  2. cfg.General.DefaultAccount, if it resolves.
//  3. cfg.Accounts[0], if any accounts are configured.
//
// The second return value reports whether an account was resolved.
func ResolveInitialAccount(cfg *Config, state UIState) (*Account, bool) {
	if cfg == nil {
		return nil, false
	}
	if state.LastActiveAccount != "" {
		if a := cfg.AccountByName(state.LastActiveAccount); a != nil {
			return a, true
		}
	}
	if cfg.General.DefaultAccount != "" {
		if a := cfg.AccountByName(cfg.General.DefaultAccount); a != nil {
			return a, true
		}
	}
	if len(cfg.Accounts) > 0 {
		cp := cfg.Accounts[0]
		return &cp, true
	}
	return nil, false
}
