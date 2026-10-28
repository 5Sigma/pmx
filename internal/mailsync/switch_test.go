package mailsync_test

import (
	"context"
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsync"
)

// TestSwitchFolder_NotWatching verifies that SwitchFolder returns ErrNotWatching
// when no watcher is running for the named account.
func TestSwitchFolder_NotWatching(t *testing.T) {
	_, acct := startMockServer(t)
	_, engine := openTestEngine(t, acct)

	err := engine.SwitchFolder(acct.Name, "Sent")
	if err != mailsync.ErrNotWatching {
		t.Errorf("SwitchFolder with no watcher: want ErrNotWatching, got %v", err)
	}
}

// TestSwitchFolder causes the watcher to re-SELECT the named folder and emit a
// FolderDone event for that folder. The test polls mode to avoid relying on IDLE.
func TestSwitchFolder(t *testing.T) {
	_, acct := startMockServer(t)
	acct.IDLEEnabled = false
	_, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 30*time.Second, 0, events)
	}()

	// Wait for the watcher to register.
	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Wait for the initial INBOX sync to settle.
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()
	if _, ok := waitEvent[mailsync.FolderDone](initCtx, events); !ok {
		t.Fatal("did not receive initial FolderDone for INBOX")
	}

	// Request a switch to Sent.
	if err := engine.SwitchFolder(acct.Name, "Sent"); err != nil {
		t.Fatalf("SwitchFolder: %v", err)
	}

	// The watcher must emit FolderDone{Folder: "Sent"} after re-SELECTing.
	switchCtx, switchCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer switchCancel()

	var gotSent bool
	for {
		ev, ok := func() (mailsync.Event, bool) {
			select {
			case ev, ok := <-events:
				return ev, ok
			case <-switchCtx.Done():
				return nil, false
			}
		}()
		if !ok {
			break
		}
		if fd, ok := ev.(mailsync.FolderDone); ok && fd.Folder == "Sent" {
			gotSent = true
			break
		}
	}

	if !gotSent {
		t.Error("did not receive FolderDone{Folder: \"Sent\"} after SwitchFolder")
	}
}

// TestSwitchFolder_ReturnsNilWhenWatcherAlive verifies that SwitchFolder returns
// nil (accepted) when a watcher is running for the account.
func TestSwitchFolder_ReturnsNilWhenWatcherAlive(t *testing.T) {
	_, acct := startMockServer(t)
	acct.IDLEEnabled = false
	_, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 30*time.Second, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	err := engine.SwitchFolder(acct.Name, "Drafts")
	if err != nil {
		t.Errorf("SwitchFolder with live watcher: want nil, got %v", err)
	}
}

// TestWatchOnce_PersistsAllFolders verifies that after Watch connects and runs
// its initial sync, all six fixture folders are stored with the correct roles.
func TestWatchOnce_PersistsAllFolders(t *testing.T) {
	_, acct := startMockServer(t)
	acct.IDLEEnabled = false
	st, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 30*time.Second, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Wait for the initial INBOX FolderDone — by then watchOnce has already
	// called EnsureFolder + UpsertFolderRole for all 6 folders.
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()
	if _, ok := waitEvent[mailsync.FolderDone](initCtx, events); !ok {
		t.Fatal("did not receive initial FolderDone")
	}

	// Give the store a moment to flush any in-progress writes.
	time.Sleep(50 * time.Millisecond)

	accountID, err := st.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}

	folders, err := st.ListFolders(accountID)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}

	const wantCount = 6
	if len(folders) != wantCount {
		names := make([]string, len(folders))
		for i, f := range folders {
			names[i] = f.Name
		}
		t.Fatalf("ListFolders: got %d folders %v, want %d", len(folders), names, wantCount)
	}

	wantRoles := map[string]mail.FolderRole{
		"INBOX":   mail.RoleInbox,
		"Sent":    mail.RoleSent,
		"Drafts":  mail.RoleDrafts,
		"Archive": mail.RoleArchive,
		"Trash":   mail.RoleTrash,
		"Junk":    mail.RoleJunk,
	}
	for _, f := range folders {
		want, ok := wantRoles[f.Name]
		if !ok {
			t.Errorf("unexpected folder %q in store", f.Name)
			continue
		}
		if f.Role != want {
			t.Errorf("folder %q: got role %q, want %q", f.Name, f.Role, want)
		}
	}
}
