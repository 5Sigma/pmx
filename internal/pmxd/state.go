package pmxd

import (
	"encoding/json"
	"os"
)

// State persists pmxd runtime state across restarts.
type State struct {
	// LastNotifiedUID maps accountName → folderName → last UID notified.
	LastNotifiedUID map[string]map[string]uint32 `json:"last_notified_uid"`
	path            string
}

func LoadState(path string) (*State, error) {
	s := &State{
		LastNotifiedUID: make(map[string]map[string]uint32),
		path:            path,
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return s, err
	}
	s.path = path
	if s.LastNotifiedUID == nil {
		s.LastNotifiedUID = make(map[string]map[string]uint32)
	}
	return s, nil
}

func (s *State) Save() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *State) GetCursor(acctName, folderName string) (uint32, bool) {
	if fm, ok := s.LastNotifiedUID[acctName]; ok {
		uid, ok := fm[folderName]
		return uid, ok
	}
	return 0, false
}

func (s *State) SetCursor(acctName, folderName string, uid uint32) {
	if _, ok := s.LastNotifiedUID[acctName]; !ok {
		s.LastNotifiedUID[acctName] = make(map[string]uint32)
	}
	s.LastNotifiedUID[acctName][folderName] = uid
}
