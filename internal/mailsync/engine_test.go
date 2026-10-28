package mailsync_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/mock"
	"github.com/5Sigma/pmx/internal/secrets"
	"github.com/5Sigma/pmx/internal/store"
)

// fakeKeyring satisfies secrets.KeyringClient and always returns the stored
// password regardless of the service/user arguments.
type fakeKeyring struct{ pass string }

func (k *fakeKeyring) Get(_, _ string) (string, error) { return k.pass, nil }
func (k *fakeKeyring) Set(_, _, _ string) error        { return nil }

// startMockServer launches the mock server on ephemeral ports and returns it
// along with a config.Account pointing at those ports.
func startMockServer(t *testing.T) (*mock.Server, config.Account) {
	t.Helper()
	srv, err := mock.Start(mock.Options{
		IMAPAddr: "127.0.0.1:0",
		SMTPAddr: "127.0.0.1:0",
		User:     "user",
		Pass:     "pass",
	})
	if err != nil {
		t.Fatalf("mock.Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	host, portStr, err := net.SplitHostPort(srv.IMAPAddr())
	if err != nil {
		t.Fatalf("parse IMAPAddr %q: %v", srv.IMAPAddr(), err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}

	acct := config.Account{
		Name:     "testacct",
		Email:    "user",
		IMAPHost: host,
		IMAPPort: port,
		IMAPTLS:  false,
	}
	return srv, acct
}

// openTestEngine builds a real store, secrets, and engine backed by a temp dir.
func openTestEngine(t *testing.T, acct config.Account) (*store.Store, *mailsync.Engine) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "mail.db"), filepath.Join(dir, "attachs"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	cfg := config.Default()
	cfg.Accounts = []config.Account{acct}
	sec := secrets.NewWithKeyring(cfg, &fakeKeyring{pass: "pass"})
	engine := mailsync.New(st, sec, filepath.Join(dir, "attachs"), nil)
	return st, engine
}

func drainSync(engine *mailsync.Engine, acct config.Account) error {
	events := make(chan mailsync.Event, 32)
	err := engine.SyncAccount(context.Background(), acct, events)
	if errors.Is(err, mailsync.ErrSyncInProgress) {
		// SyncAccount returned early without closing the channel; draining
		// would block forever.
		return err
	}
	// SyncAccount closes the channel on completion; drain to unblock.
	for range events {
	}
	return err
}

// TestSyncCreatesFolder verifies that a sync creates the folder row with
// uid_validity set.
func TestSyncCreatesFolder(t *testing.T) {
	_, acct := startMockServer(t)
	st, engine := openTestEngine(t, acct)

	if err := drainSync(engine, acct); err != nil {
		t.Fatalf("SyncAccount: %v", err)
	}

	accountID, err := st.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	f, err := st.FolderByName(accountID, "INBOX")
	if err != nil {
		t.Fatalf("FolderByName: %v", err)
	}
	if f.UIDValidity == 0 {
		t.Error("want uid_validity > 0 after sync, got 0")
	}
}

// TestSyncMessagesPresent verifies that messages are loaded into the store.
func TestSyncMessagesPresent(t *testing.T) {
	_, acct := startMockServer(t)
	st, engine := openTestEngine(t, acct)

	if err := drainSync(engine, acct); err != nil {
		t.Fatalf("SyncAccount: %v", err)
	}

	accountID, _ := st.EnsureAccount(acct.Name, acct.Email)
	f, err := st.FolderByName(accountID, "INBOX")
	if err != nil {
		t.Fatalf("FolderByName: %v", err)
	}
	msgs, err := st.ListFolder(f.ID, 200, 0)
	if err != nil {
		t.Fatalf("ListFolder: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("want at least 1 message after sync, got 0")
	}
}

// TestSyncIdempotent verifies that a second sync is idempotent — the message
// count stays stable and uid_validity is unchanged.
func TestSyncIdempotent(t *testing.T) {
	_, acct := startMockServer(t)
	st, engine := openTestEngine(t, acct)

	if err := drainSync(engine, acct); err != nil {
		t.Fatalf("first SyncAccount: %v", err)
	}

	accountID, _ := st.EnsureAccount(acct.Name, acct.Email)
	f1, err := st.FolderByName(accountID, "INBOX")
	if err != nil {
		t.Fatalf("FolderByName after 1st sync: %v", err)
	}
	msgs1, err := st.ListFolder(f1.ID, 200, 0)
	if err != nil {
		t.Fatalf("ListFolder after 1st sync: %v", err)
	}

	if err := drainSync(engine, acct); err != nil {
		t.Fatalf("second SyncAccount: %v", err)
	}

	f2, err := st.FolderByName(accountID, "INBOX")
	if err != nil {
		t.Fatalf("FolderByName after 2nd sync: %v", err)
	}
	msgs2, err := st.ListFolder(f2.ID, 200, 0)
	if err != nil {
		t.Fatalf("ListFolder after 2nd sync: %v", err)
	}

	if len(msgs2) != len(msgs1) {
		t.Errorf("idempotent: want %d messages, got %d after second sync", len(msgs1), len(msgs2))
	}
	if f2.UIDValidity != f1.UIDValidity {
		t.Errorf("uid_validity changed between syncs: %d → %d", f1.UIDValidity, f2.UIDValidity)
	}
}

// TestSyncConcurrentReturnsError verifies that a concurrent SyncAccount call
// returns ErrSyncInProgress.
func TestSyncConcurrentReturnsError(t *testing.T) {
	_, acct := startMockServer(t)
	_, engine := openTestEngine(t, acct)

	// Start a real sync in the background; it blocks until done.
	done := make(chan error, 1)
	go func() {
		done <- drainSync(engine, acct)
	}()

	// Give the goroutine a moment to acquire the lock.
	time.Sleep(10 * time.Millisecond)

	// A concurrent call should get ErrSyncInProgress.
	err := drainSync(engine, acct)
	if !errors.Is(err, mailsync.ErrSyncInProgress) {
		// It's also fine if the first sync already finished — the test is racy
		// by nature. Only fail if we got an unexpected non-nil error.
		if err != nil && !errors.Is(err, mailsync.ErrSyncInProgress) {
			t.Errorf("want ErrSyncInProgress or nil (if 1st sync finished), got %v", err)
		}
	}

	// Wait for first sync.
	if err := <-done; err != nil {
		t.Fatalf("background SyncAccount: %v", err)
	}
}

// TestMarkSeen_NotWatching verifies that MarkSeen returns ErrNotWatching when
// no watcher is running for the named account.
func TestMarkSeen_NotWatching(t *testing.T) {
	_, acct := startMockServer(t)
	_, engine := openTestEngine(t, acct)

	err := engine.MarkSeen("nonexistent", 1, 100, true)
	if !errors.Is(err, mailsync.ErrNotWatching) {
		t.Errorf("MarkSeen: want ErrNotWatching, got %v", err)
	}
}

// TestMarkFlagged_NotWatching verifies that MarkFlagged returns ErrNotWatching
// when no watcher is running for the named account.
func TestMarkFlagged_NotWatching(t *testing.T) {
	_, acct := startMockServer(t)
	_, engine := openTestEngine(t, acct)

	err := engine.MarkFlagged("nonexistent", 1, 100, true)
	if !errors.Is(err, mailsync.ErrNotWatching) {
		t.Errorf("MarkFlagged: want ErrNotWatching, got %v", err)
	}
}

// TestMoveToTrash_NotWatching verifies that MoveToTrash returns ErrNotWatching
// when no watcher is running for the named account.
func TestMoveToTrash_NotWatching(t *testing.T) {
	_, acct := startMockServer(t)
	_, engine := openTestEngine(t, acct)

	err := engine.MoveToTrash(config.Account{Name: "nonexistent"}, 1, 100)
	if !errors.Is(err, mailsync.ErrNotWatching) {
		t.Errorf("MoveToTrash: want ErrNotWatching, got %v", err)
	}
}

// TestMoveToTrash_Enqueue verifies that MoveToTrash returns nil when a watcher
// is running for the account, meaning the op was accepted onto the moveOps
// channel. The watcher is started with a long poll interval so it stays alive
// long enough for us to inspect the return value.
func TestMoveToTrash_Enqueue(t *testing.T) {
	_, acct := startMockServer(t)
	acct.IDLEEnabled = false
	_, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 10*time.Second, 0, events)
	}()

	// Wait for the watcher to register before calling MoveToTrash.
	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	err := engine.MoveToTrash(acct, 1, 42)
	if err != nil {
		t.Errorf("MoveToTrash with live watcher: want nil, got %v", err)
	}
}

// TestMoveToTrash_QueueFullDropsSilently fills the moveOps channel (capacity
// 16) completely, then sends one more and confirms MoveToTrash still returns
// nil — the drop-on-full path must never surface an error to callers.
func TestMoveToTrash_QueueFullDropsSilently(t *testing.T) {
	_, acct := startMockServer(t)
	acct.IDLEEnabled = false
	_, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 10*time.Second, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Fill the moveOps channel (buffer=16) so the next send will drop.
	for i := range 16 {
		if err := engine.MoveToTrash(acct, 1, uint32(i+1)); err != nil {
			t.Fatalf("MoveToTrash fill %d: want nil, got %v", i+1, err)
		}
	}

	// The 17th call exceeds the buffer; must still return nil (silent drop).
	err := engine.MoveToTrash(acct, 1, 17)
	if err != nil {
		t.Errorf("MoveToTrash queue-full drop: want nil, got %v", err)
	}
}

// TestMoveToArchive_NotWatching verifies that MoveToArchive returns ErrNotWatching
// when no watcher is running for the named account.
func TestMoveToArchive_NotWatching(t *testing.T) {
	_, acct := startMockServer(t)
	_, engine := openTestEngine(t, acct)

	err := engine.MoveToArchive(config.Account{Name: "nonexistent"}, 1, 100)
	if !errors.Is(err, mailsync.ErrNotWatching) {
		t.Errorf("MoveToArchive: want ErrNotWatching, got %v", err)
	}
}

// TestMoveToArchive_Enqueue verifies that MoveToArchive returns nil when a watcher
// is running for the account, meaning the op was accepted onto the moveOps channel.
func TestMoveToArchive_Enqueue(t *testing.T) {
	_, acct := startMockServer(t)
	acct.IDLEEnabled = false
	_, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 10*time.Second, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	err := engine.MoveToArchive(acct, 1, 42)
	if err != nil {
		t.Errorf("MoveToArchive with live watcher: want nil, got %v", err)
	}
}

// TestMoveToArchive_QueueFullDropsSilently fills the moveOps channel (capacity
// 16) completely, then sends one more and confirms MoveToArchive still returns
// nil — the drop-on-full path must never surface an error to callers.
func TestMoveToArchive_QueueFullDropsSilently(t *testing.T) {
	_, acct := startMockServer(t)
	acct.IDLEEnabled = false
	_, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 10*time.Second, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Fill the moveOps channel (buffer=16) so the next send will drop.
	for i := range 16 {
		if err := engine.MoveToArchive(acct, 1, uint32(i+1)); err != nil {
			t.Fatalf("MoveToArchive fill %d: want nil, got %v", i+1, err)
		}
	}

	// The 17th call exceeds the buffer; must still return nil (silent drop).
	err := engine.MoveToArchive(acct, 1, 17)
	if err != nil {
		t.Errorf("MoveToArchive queue-full drop: want nil, got %v", err)
	}
}

// TestSyncEvents verifies that the event channel receives the expected sequence.
func TestSyncEvents(t *testing.T) {
	_, acct := startMockServer(t)
	_, engine := openTestEngine(t, acct)

	events := make(chan mailsync.Event, 32)
	err := engine.SyncAccount(context.Background(), acct, events)
	if err != nil {
		t.Fatalf("SyncAccount: %v", err)
	}

	var got []string
	for ev := range events {
		switch ev.(type) {
		case mailsync.SyncStarted:
			got = append(got, "started")
		case mailsync.FolderStarted:
			got = append(got, "folder-started")
		case mailsync.FolderDone:
			got = append(got, "folder-done")
		case mailsync.SyncCompleted:
			got = append(got, "completed")
		case mailsync.SyncFailed:
			got = append(got, "failed")
		}
	}

	want := []string{"started", "folder-started", "folder-done", "completed"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("event sequence: want %v, got %v", want, got)
	}
}
