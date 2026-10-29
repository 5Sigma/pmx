// Package mailops declares the mutation and send contracts that the TUI
// and MCP server use to drive mail state changes. Both *mailsync.Engine
// (in-process) and *pmxsock.Client (daemon-proxy) implement them.
//
// Kept in a dependency-free package so neither consumer has to pull in the
// other's transitive deps (notably, mcpserver no longer imports ui).
package mailops

import (
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mailsync"
)

// MailOps is the sync engine's mutation surface.
type MailOps interface {
	Refresh(acct string) error
	SwitchFolder(acct, folder string) error
	MarkSeen(acct string, folderID int64, uid uint32, seen bool) error
	MarkFlagged(acct string, folderID int64, uid uint32, flagged bool) error
	MoveToTrash(acct config.Account, folderID int64, uid uint32) error
	MoveToArchive(acct config.Account, folderID int64, uid uint32) error
	MoveToFolder(acct config.Account, folderID int64, uid uint32, dest string) error
}

// SendOps is the send engine's trigger surface.
type SendOps interface {
	Trigger(acct string) error
}

// StatusOps is the read-only surface the status screen queries for per-account
// live watcher state. The standalone engine answers synchronously; the daemon
// client fires a request and the real snapshot arrives later as a
// pmxproto.WatcherStateEvent on the shared event stream (so the first return
// after a daemon round-trip is a zero-value placeholder).
type StatusOps interface {
	WatcherState(acct string) mailsync.WatcherState
}
