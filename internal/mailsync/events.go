package mailsync

import "time"

// Event is the sealed interface implemented by all sync progress events.
type Event interface{ isEvent() }

// SyncStarted is emitted once per SyncAccount call, before any folder work.
type SyncStarted struct{ Account string }

// FolderStarted is emitted when the engine begins processing a specific folder.
type FolderStarted struct{ Account, Folder string }

// FolderDone is emitted when a folder batch or full sync completes.
// Final is true only for the last emission per folder (after flag refresh
// and deletion); intermediate batched emissions have Final == false.
type FolderDone struct {
	Account, Folder         string
	Added, Updated, Deleted int
	Final                   bool
}

// SyncCompleted is emitted after all folders finished without error.
type SyncCompleted struct {
	Account string
	At      time.Time
}

// SyncFailed is emitted when the sync aborts due to an error.
type SyncFailed struct {
	Account string
	Err     error
}

// WatchStarted is emitted when Watch begins monitoring an account.
type WatchStarted struct{ Account string }

// WatchStopped is emitted when Watch exits. Err is nil on clean shutdown.
type WatchStopped struct {
	Account string
	Err     error
}

// WatchError is emitted on a non-fatal error during Watch (e.g. a lost
// connection that will be retried).
type WatchError struct {
	Account string
	Err     error
}

// FolderAdded is emitted when a new mailbox appears on the server.
type FolderAdded struct {
	Account  string
	Name     string
	FolderID int64
}

// FolderRenamed is emitted when a server-side rename is detected via
// UIDVALIDITY matching. FolderID is the stable local ID (unchanged).
type FolderRenamed struct {
	Account  string
	OldName  string
	NewName  string
	FolderID int64
}

// FolderDisappeared is emitted when a mailbox vanishes from the server.
// When Grace is true the folder is hidden but not yet deleted (grace period).
// When Grace is false the folder has been cascade-deleted from the local store.
type FolderDisappeared struct {
	Account  string
	Name     string
	FolderID int64
	Grace    bool
}

func (SyncStarted) isEvent()       {}
func (FolderStarted) isEvent()     {}
func (FolderDone) isEvent()        {}
func (SyncCompleted) isEvent()     {}
func (SyncFailed) isEvent()        {}
func (WatchStarted) isEvent()      {}
func (WatchStopped) isEvent()      {}
func (WatchError) isEvent()        {}
func (FolderAdded) isEvent()       {}
func (FolderRenamed) isEvent()     {}
func (FolderDisappeared) isEvent() {}
