package pmxproto

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
)

// rawMsg is the envelope for both commands and events on the wire.
type rawMsg struct {
	// Command fields
	Cmd      string `json:"cmd,omitempty"`
	Account  string `json:"account,omitempty"`
	Folder   string `json:"folder,omitempty"`
	FolderID int64  `json:"folderID,omitempty"`
	UID      uint32 `json:"uid,omitempty"`
	Seen     *bool  `json:"seen,omitempty"`
	Flagged  *bool  `json:"flagged,omitempty"`
	Dst      string `json:"dst,omitempty"`
	// Event fields
	Ev       string `json:"ev,omitempty"`
	Name     string `json:"name,omitempty"`
	OldName  string `json:"oldName,omitempty"`
	NewName  string `json:"newName,omitempty"`
	Grace    bool   `json:"grace,omitempty"`
	Added    int    `json:"added,omitempty"`
	Updated  int    `json:"updated,omitempty"`
	Deleted  int    `json:"deleted,omitempty"`
	Final    bool   `json:"final,omitempty"`
	ErrStr   string `json:"err,omitempty"`
	ItemID   int64  `json:"itemID,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
	// WatcherState snapshot fields (encoded on both the CmdWatcherState
	// request's response event and never on the request itself).
	Running   bool      `json:"running,omitempty"`
	IDLE      bool      `json:"idle,omitempty"`
	LastEvent time.Time `json:"lastEvent,omitempty"`
}

// Typed command structs (TUI → daemon).

type CmdRefresh struct{ Account string }
type CmdSwitchFolder struct{ Account, Folder string }
type CmdMarkSeen struct {
	Account  string
	FolderID int64
	UID      uint32
	Seen     bool
}
type CmdMarkFlagged struct {
	Account  string
	FolderID int64
	UID      uint32
	Flagged  bool
}
type CmdMoveToTrash struct {
	Account  string
	FolderID int64
	UID      uint32
}
type CmdMoveToArchive struct {
	Account  string
	FolderID int64
	UID      uint32
}
type CmdMoveToFolder struct {
	Account  string
	FolderID int64
	UID      uint32
	Dst      string
}
type CmdDrain struct{ Account string }
type CmdWatcherState struct{ Account string }

// EncodeCommand marshals a typed command struct to a JSON line on w.
func EncodeCommand(w io.Writer, cmd any) error {
	var m rawMsg
	switch c := cmd.(type) {
	case CmdRefresh:
		m = rawMsg{Cmd: "Refresh", Account: c.Account}
	case CmdSwitchFolder:
		m = rawMsg{Cmd: "SwitchFolder", Account: c.Account, Folder: c.Folder}
	case CmdMarkSeen:
		seen := c.Seen
		m = rawMsg{Cmd: "MarkSeen", Account: c.Account, FolderID: c.FolderID, UID: c.UID, Seen: &seen}
	case CmdMarkFlagged:
		flagged := c.Flagged
		m = rawMsg{Cmd: "MarkFlagged", Account: c.Account, FolderID: c.FolderID, UID: c.UID, Flagged: &flagged}
	case CmdMoveToTrash:
		m = rawMsg{Cmd: "MoveToTrash", Account: c.Account, FolderID: c.FolderID, UID: c.UID}
	case CmdMoveToArchive:
		m = rawMsg{Cmd: "MoveToArchive", Account: c.Account, FolderID: c.FolderID, UID: c.UID}
	case CmdMoveToFolder:
		m = rawMsg{Cmd: "MoveToFolder", Account: c.Account, FolderID: c.FolderID, UID: c.UID, Dst: c.Dst}
	case CmdDrain:
		m = rawMsg{Cmd: "Drain", Account: c.Account}
	case CmdWatcherState:
		m = rawMsg{Cmd: "WatcherState", Account: c.Account}
	default:
		return fmt.Errorf("pmxproto: unknown command type %T", cmd)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// DecodeCommand reads one JSON line from r and returns a typed command struct.
func DecodeCommand(r *bufio.Reader) (any, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var m rawMsg
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, fmt.Errorf("pmxproto: decode command: %w", err)
	}
	switch m.Cmd {
	case "Refresh":
		return CmdRefresh{m.Account}, nil
	case "SwitchFolder":
		return CmdSwitchFolder{m.Account, m.Folder}, nil
	case "MarkSeen":
		seen := m.Seen != nil && *m.Seen
		return CmdMarkSeen{m.Account, m.FolderID, m.UID, seen}, nil
	case "MarkFlagged":
		flagged := m.Flagged != nil && *m.Flagged
		return CmdMarkFlagged{m.Account, m.FolderID, m.UID, flagged}, nil
	case "MoveToTrash":
		return CmdMoveToTrash{m.Account, m.FolderID, m.UID}, nil
	case "MoveToArchive":
		return CmdMoveToArchive{m.Account, m.FolderID, m.UID}, nil
	case "MoveToFolder":
		return CmdMoveToFolder{m.Account, m.FolderID, m.UID, m.Dst}, nil
	case "Drain":
		return CmdDrain{m.Account}, nil
	case "WatcherState":
		return CmdWatcherState{m.Account}, nil
	default:
		return nil, fmt.Errorf("pmxproto: unknown command %q", m.Cmd)
	}
}

// EncodeEvent accepts a mailsync.Event or mailsend.Event and writes a JSON line to w.
// Unknown event types are silently skipped (returns nil).
func EncodeEvent(w io.Writer, ev any) error {
	var m rawMsg
	switch e := ev.(type) {
	case mailsync.FolderStarted:
		m = rawMsg{Ev: "FolderStarted", Account: e.Account, Folder: e.Folder}
	case mailsync.FolderDone:
		m = rawMsg{Ev: "FolderDone", Account: e.Account, Folder: e.Folder, Added: e.Added, Updated: e.Updated, Deleted: e.Deleted, Final: e.Final}
	case mailsync.FolderAdded:
		m = rawMsg{Ev: "FolderAdded", Account: e.Account, Name: e.Name, FolderID: e.FolderID}
	case mailsync.FolderRenamed:
		m = rawMsg{Ev: "FolderRenamed", Account: e.Account, OldName: e.OldName, NewName: e.NewName, FolderID: e.FolderID}
	case mailsync.FolderDisappeared:
		m = rawMsg{Ev: "FolderDisappeared", Account: e.Account, Name: e.Name, FolderID: e.FolderID, Grace: e.Grace}
	case mailsync.WatchStarted:
		m = rawMsg{Ev: "WatchStarted", Account: e.Account}
	case mailsync.WatchStopped:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		m = rawMsg{Ev: "WatchStopped", Account: e.Account, ErrStr: errStr}
	case mailsync.WatchError:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		m = rawMsg{Ev: "WatchError", Account: e.Account, ErrStr: errStr}
	case mailsend.WorkerStarted:
		m = rawMsg{Ev: "WorkerStarted", Account: e.Account}
	case mailsend.WorkerStopped:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		m = rawMsg{Ev: "WorkerStopped", Account: e.Account, ErrStr: errStr}
	case mailsend.WorkerError:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		m = rawMsg{Ev: "WorkerError", Account: e.Account, ErrStr: errStr}
	case mailsend.SendSucceeded:
		m = rawMsg{Ev: "SendSucceeded", Account: e.Account, ItemID: e.ItemID}
	case mailsend.SendFailed:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		m = rawMsg{Ev: "SendFailed", Account: e.Account, ItemID: e.ItemID, Attempts: e.Attempts, ErrStr: errStr}
	case mailsync.WatcherState:
		m = rawMsg{
			Ev:        "WatcherState",
			Account:   e.Account,
			Running:   e.Running,
			IDLE:      e.IDLE,
			Folder:    e.Folder,
			LastEvent: e.LastEvent,
			ErrStr:    e.LastError,
		}
	default:
		return nil // silently skip unknown/unforwarded event types
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// DaemonDisconnectedMsg is sent to the bubbletea program when the socket closes.
type DaemonDisconnectedMsg struct{}

// DecodeEvent reads one JSON line from r and returns a concrete mailsync or
// mailsend struct ready for prog.Send. Returns (nil, err) on read/decode
// errors. Returns (nil, nil) for unknown event types — callers should skip
// nil messages.
func DecodeEvent(r *bufio.Reader) (tea.Msg, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var m rawMsg
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, fmt.Errorf("pmxproto: decode event: %w", err)
	}
	switch m.Ev {
	case "FolderStarted":
		return mailsync.FolderStarted{Account: m.Account, Folder: m.Folder}, nil
	case "FolderDone":
		return mailsync.FolderDone{Account: m.Account, Folder: m.Folder, Added: m.Added, Updated: m.Updated, Deleted: m.Deleted, Final: m.Final}, nil
	case "FolderAdded":
		return mailsync.FolderAdded{Account: m.Account, Name: m.Name, FolderID: m.FolderID}, nil
	case "FolderRenamed":
		return mailsync.FolderRenamed{Account: m.Account, OldName: m.OldName, NewName: m.NewName, FolderID: m.FolderID}, nil
	case "FolderDisappeared":
		return mailsync.FolderDisappeared{Account: m.Account, Name: m.Name, FolderID: m.FolderID, Grace: m.Grace}, nil
	case "WatchStarted":
		return mailsync.WatchStarted{Account: m.Account}, nil
	case "WatchStopped":
		var e error
		if m.ErrStr != "" {
			e = errors.New(m.ErrStr)
		}
		return mailsync.WatchStopped{Account: m.Account, Err: e}, nil
	case "WatchError":
		var e error
		if m.ErrStr != "" {
			e = errors.New(m.ErrStr)
		}
		return mailsync.WatchError{Account: m.Account, Err: e}, nil
	case "WorkerStarted":
		return mailsend.WorkerStarted{Account: m.Account}, nil
	case "WorkerStopped":
		var e error
		if m.ErrStr != "" {
			e = errors.New(m.ErrStr)
		}
		return mailsend.WorkerStopped{Account: m.Account, Err: e}, nil
	case "WorkerError":
		var e error
		if m.ErrStr != "" {
			e = errors.New(m.ErrStr)
		}
		return mailsend.WorkerError{Account: m.Account, Err: e}, nil
	case "SendSucceeded":
		return mailsend.SendSucceeded{Account: m.Account, ItemID: m.ItemID}, nil
	case "SendFailed":
		var e error
		if m.ErrStr != "" {
			e = errors.New(m.ErrStr)
		}
		return mailsend.SendFailed{Account: m.Account, ItemID: m.ItemID, Attempts: m.Attempts, Err: e}, nil
	case "WatcherState":
		return mailsync.WatcherState{
			Account:   m.Account,
			Running:   m.Running,
			IDLE:      m.IDLE,
			Folder:    m.Folder,
			LastEvent: m.LastEvent,
			LastError: m.ErrStr,
		}, nil
	default:
		return nil, nil // unknown event; caller should skip
	}
}
