package ui

import (
	"testing"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
)

// stubListStore stands in for mail.MessageStore for mailList tests.
// It embeds the interface to satisfy the full method set; ListFolder is the
// only method called during the tests here.
type stubListStore struct {
	mail.MessageStore
	messages []*mail.Message
}

func (s *stubListStore) ListFolder(_ int64, _, _ int) ([]*mail.Message, error) {
	return s.messages, nil
}

// ── Mark-unread leaf tests ───────────────────────────────────────────────────

// TestMailList_MarkUnreadLeafUsesSelection verifies that the ["m","u"] leaf in
// the mailList KeyMap returns a markSeenMsg carrying the UID and folderID of
// the currently selected item.
func TestMailList_MarkUnreadLeafUsesSelection(t *testing.T) {
	msgs := []*mail.Message{
		{ID: 1, FolderID: 1, UID: 10, Subject: "First"},
		{ID: 2, FolderID: 1, UID: 20, Subject: "Second"},
	}
	st := &stubListStore{messages: msgs}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 1, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)

	// Load items into the list so SelectedItem works.
	items := make([]interface{ Title() string }, len(msgs))
	_ = items
	// Drive Init to populate the list — Init returns a cmd that loads from store.
	cmd := ml.Init()
	if cmd != nil {
		msg := cmd()
		updated, _ := ml.Update(msg)
		ml = updated.(mailList)
	}

	// Select row 1 (UID=20).
	ml.list.Select(1)

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "u"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","u"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Message == nil {
		t.Fatal(`["m","u"] action returned Message=nil, want markSeenMsg`)
	}
	msg, ok := result.Message.(markSeenMsg)
	if !ok {
		t.Fatalf(`["m","u"] action Message type = %T, want markSeenMsg`, result.Message)
	}
	if msg.uid != 20 {
		t.Errorf("markSeenMsg.uid = %d, want 20 (row 1)", msg.uid)
	}
	if msg.folderID != 1 {
		t.Errorf("markSeenMsg.folderID = %d, want 1", msg.folderID)
	}
	if msg.seen {
		t.Errorf("markSeenMsg.seen = true, want false (mark-unread action)")
	}
}

// TestMailList_MarkUnreadNoSelection verifies that the ["m","u"] leaf returns a
// non-empty Note and a nil Message when the list has no items (nothing selected).
func TestMailList_MarkUnreadNoSelection(t *testing.T) {
	st := &stubListStore{messages: nil}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 1, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)
	// Do not load any messages — list is empty.

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "u"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","u"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Note == "" {
		t.Error(`["m","u"] with no selection: want non-empty Note, got empty string`)
	}
	if result.Message != nil {
		t.Errorf(`["m","u"] with no selection: want Message=nil, got %T`, result.Message)
	}
}

// ── Flag leaf tests ──────────────────────────────────────────────────────────

// TestMailList_FlagLeafUsesSelection verifies ["m","f"] emits markFlaggedMsg
// with the selected item and flagged=true when the message is not yet flagged.
func TestMailList_FlagLeafUsesSelection(t *testing.T) {
	msgs := []*mail.Message{
		{ID: 1, FolderID: 2, UID: 10, Subject: "First"},
		{ID: 2, FolderID: 2, UID: 20, Subject: "Second"},
	}
	st := &stubListStore{messages: msgs}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 2, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)

	cmd := ml.Init()
	if cmd != nil {
		msg := cmd()
		updated, _ := ml.Update(msg)
		ml = updated.(mailList)
	}

	// Select row 1 (UID=20).
	ml.list.Select(1)

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "f"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","f"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Message == nil {
		t.Fatal(`["m","f"] action returned Message=nil, want markFlaggedMsg`)
	}
	fmsg, ok := result.Message.(markFlaggedMsg)
	if !ok {
		t.Fatalf(`["m","f"] action Message type = %T, want markFlaggedMsg`, result.Message)
	}
	if fmsg.uid != 20 {
		t.Errorf("markFlaggedMsg.uid = %d, want 20 (row 1)", fmsg.uid)
	}
	if fmsg.folderID != 2 {
		t.Errorf("markFlaggedMsg.folderID = %d, want 2", fmsg.folderID)
	}
	if !fmsg.flagged {
		t.Errorf("markFlaggedMsg.flagged = false, want true (message not yet flagged)")
	}
}

// TestMailList_FlagLeafUnflagsWhenAlreadyFlagged verifies ["m","f"] sets
// flagged=false when the selected message already has FlagFlagged set.
func TestMailList_FlagLeafUnflagsWhenAlreadyFlagged(t *testing.T) {
	msgs := []*mail.Message{
		{ID: 1, FolderID: 2, UID: 10, Subject: "First", Flags: mail.FlagFlagged},
	}
	st := &stubListStore{messages: msgs}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 2, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)

	cmd := ml.Init()
	if cmd != nil {
		msg := cmd()
		updated, _ := ml.Update(msg)
		ml = updated.(mailList)
	}

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "f"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","f"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Message == nil {
		t.Fatal(`["m","f"] action returned Message=nil, want markFlaggedMsg`)
	}
	fmsg, ok := result.Message.(markFlaggedMsg)
	if !ok {
		t.Fatalf(`["m","f"] action Message type = %T, want markFlaggedMsg`, result.Message)
	}
	if fmsg.flagged {
		t.Errorf("markFlaggedMsg.flagged = true, want false (message already flagged)")
	}
}

// TestMailList_FlagNoSelection verifies ["m","f"] returns a non-empty Note
// and nil Message when nothing is selected.
func TestMailList_FlagNoSelection(t *testing.T) {
	st := &stubListStore{messages: nil}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 1, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)
	// No messages loaded — list is empty.

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "f"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","f"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Note == "" {
		t.Error(`["m","f"] with no selection: want non-empty Note, got empty string`)
	}
	if result.Message != nil {
		t.Errorf(`["m","f"] with no selection: want Message=nil, got %T`, result.Message)
	}
}

// ── Delete leaf tests ────────────────────────────────────────────────────────

// TestMailList_DeleteLeafUsesSelection verifies that the ["m","d"] leaf returns
// a deleteMsg carrying the folderID and uid of the selected item, with
// popAfter==false (the list stays open after deletion).
func TestMailList_DeleteLeafUsesSelection(t *testing.T) {
	msgs := []*mail.Message{
		{ID: 1, FolderID: 3, UID: 10, Subject: "Alpha"},
		{ID: 2, FolderID: 3, UID: 20, Subject: "Beta"},
	}
	st := &stubListStore{messages: msgs}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 3, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)

	// Populate the list via Init so SelectedItem works.
	cmd := ml.Init()
	if cmd != nil {
		msg := cmd()
		updated, _ := ml.Update(msg)
		ml = updated.(mailList)
	}

	// Select row 1 (UID=20, folderID=3).
	ml.list.Select(1)

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "d"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","d"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Message == nil {
		t.Fatal(`["m","d"] action returned Message=nil, want deleteMsg`)
	}
	dmsg, ok := result.Message.(deleteMsg)
	if !ok {
		t.Fatalf(`["m","d"] action Message type = %T, want deleteMsg`, result.Message)
	}
	if dmsg.uid != 20 {
		t.Errorf("deleteMsg.uid = %d, want 20 (row 1)", dmsg.uid)
	}
	if dmsg.folderID != 3 {
		t.Errorf("deleteMsg.folderID = %d, want 3", dmsg.folderID)
	}
	if dmsg.popAfter {
		t.Errorf("deleteMsg.popAfter = true, want false (list view must not pop after delete)")
	}
}

// TestMailList_DeleteLeafNoSelection verifies that the ["m","d"] leaf returns a
// non-empty Note and a nil Message when the list has no selected item (uid==0).
func TestMailList_DeleteLeafNoSelection(t *testing.T) {
	st := &stubListStore{messages: nil}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 1, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)
	// No messages loaded — SelectedItem returns nothing useful.

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "d"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","d"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Note == "" {
		t.Error(`["m","d"] with no selection: want non-empty Note, got empty string`)
	}
	if result.Message != nil {
		t.Errorf(`["m","d"] with no selection: want Message=nil, got %T`, result.Message)
	}
}

// ── Archive leaf tests ───────────────────────────────────────────────────────

// TestMailList_ArchiveLeafUsesSelection verifies that the ["m","a"] leaf returns
// an archiveMsg carrying the folderID and uid of the selected item, with
// popAfter==false (the list stays open after archiving).
func TestMailList_ArchiveLeafUsesSelection(t *testing.T) {
	msgs := []*mail.Message{
		{ID: 1, FolderID: 3, UID: 10, Subject: "Alpha"},
		{ID: 2, FolderID: 3, UID: 20, Subject: "Beta"},
	}
	st := &stubListStore{messages: msgs}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 3, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)

	// Populate the list via Init so SelectedItem works.
	cmd := ml.Init()
	if cmd != nil {
		msg := cmd()
		updated, _ := ml.Update(msg)
		ml = updated.(mailList)
	}

	// Select row 1 (UID=20, folderID=3).
	ml.list.Select(1)

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "a"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","a"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Message == nil {
		t.Fatal(`["m","a"] action returned Message=nil, want archiveMsg`)
	}
	amsg, ok := result.Message.(archiveMsg)
	if !ok {
		t.Fatalf(`["m","a"] action Message type = %T, want archiveMsg`, result.Message)
	}
	if amsg.uid != 20 {
		t.Errorf("archiveMsg.uid = %d, want 20 (row 1)", amsg.uid)
	}
	if amsg.folderID != 3 {
		t.Errorf("archiveMsg.folderID = %d, want 3", amsg.folderID)
	}
	if amsg.popAfter {
		t.Errorf("archiveMsg.popAfter = true, want false (list view must not pop after archive)")
	}
}

// TestMailList_ArchiveLeafNoSelection verifies that the ["m","a"] leaf returns a
// non-empty Note and a nil Message when the list has no selected item (uid==0).
func TestMailList_ArchiveLeafNoSelection(t *testing.T) {
	st := &stubListStore{messages: nil}
	cfg := config.Default()
	ml := newMailList(st, cfg, nil, 1, "Inbox", mail.RoleInbox)
	ml = ml.SetSize(80, 24).(mailList)
	// No messages loaded — SelectedItem returns nothing useful.

	_, action, ok := ml.KeyMap().FindLeader([]string{"m", "a"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","a"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Note == "" {
		t.Error(`["m","a"] with no selection: want non-empty Note, got empty string`)
	}
	if result.Message != nil {
		t.Errorf(`["m","a"] with no selection: want Message=nil, got %T`, result.Message)
	}
}
