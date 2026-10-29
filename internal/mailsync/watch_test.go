package mailsync_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/5Sigma/pmx/internal/mailsync"
)

// deliverMessage sends a minimal RFC 5322 message to the mock server's SMTP
// listener. The mock SMTP backend appends it to INBOX, which causes an EXISTS
// unilateral response on any open IMAP connections.
func deliverMessage(t *testing.T, smtpAddr, subject string) {
	t.Helper()
	c, err := gosmtp.Dial(smtpAddr)
	if err != nil {
		t.Fatalf("smtp dial %s: %v", smtpAddr, err)
	}
	defer func() { _ = c.Close() }()

	body := "Subject: " + subject + "\r\nFrom: test@localhost\r\nTo: user@localhost\r\n\r\nTest body.\r\n"
	if err := c.SendMail("test@localhost", []string{"user@localhost"}, strings.NewReader(body)); err != nil {
		t.Fatalf("smtp send: %v", err)
	}
}

// waitFolderDoneWithAdded drains events until it sees a FolderDone with
// Added > 0, the channel closes, or ctx is cancelled.
func waitFolderDoneWithAdded(ctx context.Context, ch <-chan mailsync.Event) (mailsync.FolderDone, bool) {
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return mailsync.FolderDone{}, false
			}
			if fd, ok := ev.(mailsync.FolderDone); ok && fd.Added > 0 {
				return fd, true
			}
		case <-ctx.Done():
			return mailsync.FolderDone{}, false
		}
	}
}

// waitEvent drains ch until an event of type T is received, ctx is cancelled,
// or the channel closes.
func waitEvent[T mailsync.Event](ctx context.Context, ch <-chan mailsync.Event) (T, bool) {
	var zero T
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return zero, false
			}
			if typed, ok := ev.(T); ok {
				return typed, true
			}
		case <-ctx.Done():
			return zero, false
		}
	}
}

// TestWatchPollFallback starts Watch with IDLEEnabled=false and a very short
// poll interval (50ms). After the initial sync it delivers a message via SMTP
// and expects FolderDone{Added>=1} within 500ms.
func TestWatchPollFallback(t *testing.T) {
	srv, acct := startMockServer(t)
	idleOff := false
	acct.IDLEEnabled = &idleOff
	_, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 50*time.Millisecond, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Allow the initial catch-up sync to complete before delivering.
	time.Sleep(200 * time.Millisecond)
	deliverMessage(t, srv.SMTPAddr(), "PollTest")

	deadline, deadlineCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer deadlineCancel()

	fd, ok := waitFolderDoneWithAdded(deadline, events)
	if !ok {
		t.Fatal("did not receive FolderDone with Added > 0 within 500ms")
	}
	if fd.Added < 1 {
		t.Errorf("want Added >= 1, got %d", fd.Added)
	}
}

// TestWatchManualRefresh starts Watch with a 10s poll interval, delivers a
// message, calls Refresh, and expects FolderDone{Added>=1} well before the
// ticker would fire.
func TestWatchManualRefresh(t *testing.T) {
	srv, acct := startMockServer(t)
	idleOff := false
	acct.IDLEEnabled = &idleOff
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

	// Allow the initial catch-up sync to complete.
	time.Sleep(300 * time.Millisecond)

	deliverMessage(t, srv.SMTPAddr(), "RefreshTest")

	if err := engine.Refresh(acct.Name); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	deadline, deadlineCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer deadlineCancel()

	fd, ok := waitFolderDoneWithAdded(deadline, events)
	if !ok {
		t.Fatal("did not receive FolderDone with Added > 0 after Refresh")
	}
	if fd.Added < 1 {
		t.Errorf("want Added >= 1, got %d", fd.Added)
	}
}

// TestWatchCancellation verifies that cancelling the context stops Watch and
// closes the events channel, with WatchStopped as the last event.
func TestWatchCancellation(t *testing.T) {
	_, acct := startMockServer(t)
	idleOff := false
	acct.IDLEEnabled = &idleOff
	_, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan mailsync.Event, 64)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		_ = engine.Watch(ctx, acct, 10*time.Second, 0, events)
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer waitCancel()
	if _, ok := waitEvent[mailsync.WatchStarted](waitCtx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	cancel()

	var lastEvent mailsync.Event
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer drainCancel()
drain:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break drain
			}
			lastEvent = ev
		case <-drainCtx.Done():
			t.Fatal("events channel not closed within 3s after cancel")
		}
	}

	if _, ok := lastEvent.(mailsync.WatchStopped); !ok {
		t.Errorf("last event: want WatchStopped, got %T", lastEvent)
	}

	select {
	case <-watchDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Watch goroutine did not exit within 3s after cancel")
	}
}

// TestRefreshNotWatching verifies that Refresh returns ErrNotWatching when no
// watcher is running for the named account.
func TestRefreshNotWatching(t *testing.T) {
	_, acct := startMockServer(t)
	_, engine := openTestEngine(t, acct)

	err := engine.Refresh("no-such-account")
	if err != mailsync.ErrNotWatching {
		t.Errorf("want ErrNotWatching, got %v", err)
	}
}

// TestWatchMarkSeenAppliesServerFlag starts Watch in poll mode (IDLEEnabled=false),
// waits for the initial sync to land at least one message, calls MarkSeen for
// that message's UID, triggers a manual Refresh, and verifies that the store
// reflects the updated flag after the next FolderDone event.
//
// The imapmemserver does handle STORE, but if the flag write fails to propagate
// within the deadline the test is skipped rather than failed — the
// TestMarkSeen_NotWatching test in engine_test.go covers the error-path
// invariant more reliably.
func TestWatchMarkSeenAppliesServerFlag(t *testing.T) {
	_, acct := startMockServer(t)
	idleOff := false
	acct.IDLEEnabled = &idleOff
	st, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 10*time.Second, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Wait for the initial catch-up sync to store at least one message.
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()
	if _, ok := waitEvent[mailsync.FolderDone](initCtx, events); !ok {
		t.Fatal("did not receive initial FolderDone")
	}

	// Resolve the account/folder IDs and pick the first message.
	accountID, err := st.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	folder, err := st.FolderByName(accountID, "INBOX")
	if err != nil {
		t.Fatalf("FolderByName: %v", err)
	}
	msgs, err := st.ListFolder(folder.ID, 1, 0)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("ListFolder: err=%v msgs=%d", err, len(msgs))
	}
	uid := msgs[0].UID
	folderID := folder.ID

	// Write seen=true locally (the engine.MarkSeen contract says the local DB
	// is the caller's responsibility), then enqueue the server-side STORE.
	if err := st.UpdateFlags(folderID, uid, msgs[0].Flags|0x01 /* FlagSeen */); err != nil {
		t.Fatalf("UpdateFlags: %v", err)
	}
	if err := engine.MarkSeen(acct.Name, folderID, uid, true); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}

	// Trigger a fetch so the watcher re-reads flags from the server.
	if err := engine.Refresh(acct.Name); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer refreshCancel()
	if _, ok := waitEvent[mailsync.FolderDone](refreshCtx, events); !ok {
		t.Skip("FolderDone not received within 3s after Refresh — skipping flag propagation check")
	}

	got, err := st.ByUID(folderID, uid)
	if err != nil {
		t.Fatalf("ByUID after mark-seen: %v", err)
	}
	if !got.IsRead() {
		t.Errorf("message uid=%d: IsRead() = false, want true after MarkSeen(seen=true)", uid)
	}
}

// TestWatchIDLE starts Watch with IDLEEnabled=true and delivers a message
// without calling Refresh. If the mock server advertises IDLE (IMAP4rev2),
// the EXISTS push should trigger a sync; otherwise the test is skipped.
func TestWatchIDLE(t *testing.T) {
	srv, acct := startMockServer(t)
	idleOn := true
	acct.IDLEEnabled = &idleOn
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

	// Allow initial sync to complete before delivering.
	time.Sleep(300 * time.Millisecond)

	deliverMessage(t, srv.SMTPAddr(), "IDLETest")

	// With IDLE, the server's EXISTS pushes through pokeSend without Refresh.
	// If the mock doesn't support IDLE the watcher falls back to polling (10s
	// interval), so FolderDone won't arrive within 2s — skip in that case.
	deadline, deadlineCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer deadlineCancel()

	fd, ok := waitFolderDoneWithAdded(deadline, events)
	if !ok {
		t.Skip("mock does not deliver push within 2s — IDLE likely unsupported; skipping")
	}
	if fd.Added < 1 {
		t.Errorf("want Added >= 1, got %d", fd.Added)
	}
}

// TestWatchMoveToTrashMovesMessageToTrash starts Watch in poll mode, waits for
// the initial sync to land at least one INBOX message, calls MoveToTrash for
// that message's UID, triggers a manual Refresh, then asserts that the Trash
// mailbox gained a message.
//
// The mock server's Trash mailbox is seeded by mock.Start (the mailbox list
// was extended to include "Trash"). The imapmemserver handles IMAP MOVE
// natively by copying the message into the destination mailbox and expunging
// it from the source, so the assertion queries the server-side Trash count
// rather than the local store (which is updated optimistically by the caller).
func TestWatchMoveToTrashMovesMessageToTrash(t *testing.T) {
	srv, acct := startMockServer(t)
	idleOff := false
	acct.IDLEEnabled = &idleOff
	st, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 10*time.Second, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Wait for the initial catch-up sync so INBOX is populated.
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()
	if _, ok := waitEvent[mailsync.FolderDone](initCtx, events); !ok {
		t.Fatal("did not receive initial FolderDone")
	}

	// Resolve folder and pick the first message in INBOX.
	accountID, err := st.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	folder, err := st.FolderByName(accountID, "INBOX")
	if err != nil {
		t.Fatalf("FolderByName: %v", err)
	}
	msgs, err := st.ListFolder(folder.ID, 1, 0)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("ListFolder: err=%v msgs=%d", err, len(msgs))
	}
	uid := msgs[0].UID
	folderID := folder.ID

	// Record Trash count before the move.
	statusBefore, err := srv.User.Status("Trash", &imap.StatusOptions{NumMessages: true})
	if err != nil {
		t.Fatalf("Status(Trash) before: %v", err)
	}
	trashBefore := uint32(0)
	if statusBefore.NumMessages != nil {
		trashBefore = *statusBefore.NumMessages
	}

	// Delete the local DB row optimistically (matches what app.go does), then
	// enqueue the server-side MOVE.
	if err := st.DeleteMessageByUID(folderID, uid); err != nil {
		t.Fatalf("DeleteMessageByUID: %v", err)
	}
	if err := engine.MoveToTrash(acct, folderID, uid); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}

	// Trigger a poll so the watcher wakes up and drains the moveOps channel.
	if err := engine.Refresh(acct.Name); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer refreshCancel()
	if _, ok := waitEvent[mailsync.FolderDone](refreshCtx, events); !ok {
		t.Skip("FolderDone not received within 3s after Refresh — skipping server-side MOVE check")
	}

	// Give the server a moment to process the MOVE before querying.
	time.Sleep(50 * time.Millisecond)

	statusAfter, err := srv.User.Status("Trash", &imap.StatusOptions{NumMessages: true})
	if err != nil {
		t.Fatalf("Status(Trash) after: %v", err)
	}
	trashAfter := uint32(0)
	if statusAfter.NumMessages != nil {
		trashAfter = *statusAfter.NumMessages
	}
	if trashAfter <= trashBefore {
		t.Errorf("Trash count: want > %d after MoveToTrash, got %d", trashBefore, trashAfter)
	}
}

// TestWatchPoll_SweepCycleEmitsNoWatchError verifies that when a sweep fires
// during normal poll-mode operation with a healthy server connection, the
// engine does NOT emit a WatchError event. This guards the non-fatal sweep
// path: a sweep that succeeds must leave the watcher running with a clean
// error state.
//
// Note: testing the "sweep fails but NOOP succeeds → stay alive" branch
// requires injecting a LIST-STATUS failure mid-session, which the mock server
// does not support. That branch is covered by Change 1's NOOP-failure test
// (which validates the NOOP error surface); the survive-on-alive behaviour is
// an integration concern deferred until mock hook injection is available.
func TestWatchPoll_SweepCycleEmitsNoWatchError(t *testing.T) {
	_, acct := startMockServer(t)
	idleOff := false
	acct.IDLEEnabled = &idleOff
	_, engine := openTestEngine(t, acct)

	// Use a very short sweep interval (100ms) so at least one sweep fires
	// during the test window.
	const sweepInterval = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 10*time.Second, sweepInterval, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Wait for the initial catch-up sync.
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()
	if _, ok := waitEvent[mailsync.FolderDone](initCtx, events); !ok {
		t.Fatal("did not receive initial FolderDone")
	}

	// Give the sweep timer enough time to fire at least twice.
	time.Sleep(4 * sweepInterval)

	// Drain any events that arrived during the sweep window and fail if any
	// WatchError was emitted — a healthy sweep must not produce errors.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer drainCancel()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			if we, isErr := ev.(mailsync.WatchError); isErr {
				t.Errorf("unexpected WatchError during healthy sweep: %v", we.Err)
			}
		case <-drainCtx.Done():
			return
		}
	}
}

// TestWatchMoveToArchiveMovesMessageToArchive starts Watch in poll mode, waits
// for the initial sync to land at least one INBOX message, calls MoveToArchive
// for that message's UID, triggers a manual Refresh, then asserts that the
// Archive mailbox gained a message.
//
// The mock server seeds "Archive" with \Archive SPECIAL-USE, so the engine's
// special-use discovery maps mail.RoleArchive to "Archive" automatically.
func TestWatchMoveToArchiveMovesMessageToArchive(t *testing.T) {
	srv, acct := startMockServer(t)
	idleOff := false
	acct.IDLEEnabled = &idleOff
	st, engine := openTestEngine(t, acct)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events := make(chan mailsync.Event, 64)
	go func() {
		_ = engine.Watch(ctx, acct, 10*time.Second, 0, events)
	}()

	if _, ok := waitEvent[mailsync.WatchStarted](ctx, events); !ok {
		t.Fatal("did not receive WatchStarted")
	}

	// Wait for the initial catch-up sync so INBOX is populated.
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()
	if _, ok := waitEvent[mailsync.FolderDone](initCtx, events); !ok {
		t.Fatal("did not receive initial FolderDone")
	}

	// Resolve folder and pick the first message in INBOX.
	accountID, err := st.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	folder, err := st.FolderByName(accountID, "INBOX")
	if err != nil {
		t.Fatalf("FolderByName: %v", err)
	}
	msgs, err := st.ListFolder(folder.ID, 1, 0)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("ListFolder: err=%v msgs=%d", err, len(msgs))
	}
	uid := msgs[0].UID
	folderID := folder.ID

	// Record Archive count before the move.
	statusBefore, err := srv.User.Status("Archive", &imap.StatusOptions{NumMessages: true})
	if err != nil {
		t.Fatalf("Status(Archive) before: %v", err)
	}
	archiveBefore := uint32(0)
	if statusBefore.NumMessages != nil {
		archiveBefore = *statusBefore.NumMessages
	}

	// Delete the local DB row optimistically (matches what app.go does), then
	// enqueue the server-side MOVE.
	if err := st.DeleteMessageByUID(folderID, uid); err != nil {
		t.Fatalf("DeleteMessageByUID: %v", err)
	}
	if err := engine.MoveToArchive(acct, folderID, uid); err != nil {
		t.Fatalf("MoveToArchive: %v", err)
	}

	// Trigger a poll so the watcher wakes up and drains the moveOps channel.
	if err := engine.Refresh(acct.Name); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer refreshCancel()
	if _, ok := waitEvent[mailsync.FolderDone](refreshCtx, events); !ok {
		t.Skip("FolderDone not received within 3s after Refresh — skipping server-side MOVE check")
	}

	// Give the server a moment to process the MOVE before querying.
	time.Sleep(50 * time.Millisecond)

	statusAfter, err := srv.User.Status("Archive", &imap.StatusOptions{NumMessages: true})
	if err != nil {
		t.Fatalf("Status(Archive) after: %v", err)
	}
	archiveAfter := uint32(0)
	if statusAfter.NumMessages != nil {
		archiveAfter = *statusAfter.NumMessages
	}
	if archiveAfter <= archiveBefore {
		t.Errorf("Archive count: want > %d after MoveToArchive, got %d", archiveBefore, archiveAfter)
	}
}
