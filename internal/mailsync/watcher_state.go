package mailsync

import "time"

// WatcherState is a snapshot of a single account's live Watch goroutine state.
// It is returned by (*Engine).WatcherState and transported over the daemon
// socket so standalone and daemon-mode UIs render consistently.
//
// Zero value represents "no watcher registered for this account yet" — all
// fields will be their zero values and Running is false.
type WatcherState struct {
	Account   string    // account name (empty when no watcher)
	Running   bool      // handle is registered in Engine.watchers
	IDLE      bool      // true once the IDLE loop is entered; false for poll
	Folder    string    // currently SELECTed mailbox (best-effort)
	LastEvent time.Time // last FolderDone or IDLE wake
	LastError string    // most recent WatchError text; cleared on next success
	ErrSince  time.Time // when the current error streak started; zero if no error
}

// observe updates the watcher handle's live-state fields in response to an
// outbound event. Called from Watch's emit wrapper so every emitted event
// transitions the status snapshot in lock-step with downstream observers.
func (h *watcherHandle) observe(ev Event) {
	switch e := ev.(type) {
	case WatchStarted:
		h.mu.Lock()
		h.running = true
		h.mu.Unlock()
	case WatchStopped:
		h.mu.Lock()
		h.running = false
		if e.Err != nil {
			h.lastErr = e.Err.Error()
		}
		h.mu.Unlock()
	case WatchError:
		h.mu.Lock()
		if e.Err != nil {
			h.lastErr = e.Err.Error()
			if h.errSince.IsZero() {
				h.errSince = time.Now()
			}
		}
		h.mu.Unlock()
	case FolderStarted:
		h.mu.Lock()
		h.folder = e.Folder
		h.lastErr = ""
		h.errSince = time.Time{}
		h.mu.Unlock()
	case FolderDone:
		h.mu.Lock()
		h.lastEvent = time.Now()
		h.lastErr = ""
		h.errSince = time.Time{}
		h.mu.Unlock()
	}
}

// setIdle records whether the live loop entered IDLE or poll mode.
func (h *watcherHandle) setIdle(idle bool) {
	h.mu.Lock()
	h.idle = idle
	h.mu.Unlock()
}

// WatcherState returns a snapshot of the named account's watcher state. If no
// watcher is active for acct, returns a zero-value WatcherState with
// Running=false. Safe to call concurrently with the Watch goroutine.
func (e *Engine) WatcherState(acct string) WatcherState {
	e.mu.Lock()
	h, ok := e.watchers[acct]
	e.mu.Unlock()
	if !ok {
		return WatcherState{Account: acct}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return WatcherState{
		Account:   acct,
		Running:   h.running,
		IDLE:      h.idle,
		Folder:    h.folder,
		LastEvent: h.lastEvent,
		LastError: h.lastErr,
		ErrSince:  h.errSince,
	}
}
