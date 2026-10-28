package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/ui/pathpicker"
)

// stubStore stands in for mail.MessageStore at test boundaries.
// Embedding the interface satisfies the ~20 methods we never call in these
// tests; any accidental call will panic with a clear nil-pointer message.
type stubStore struct {
	mail.MessageStore
	attachments []mail.Attachment
	bodyHTML    string
	// paths overrides the resolved path for specific filenames.
	// Entries not present fall back to "/att/<filename>" (which does not
	// exist on disk and will trigger the "not on disk" note path).
	paths map[string]string
}

func (s *stubStore) ListAttachments(_ int64) ([]mail.Attachment, error) {
	return s.attachments, nil
}

func (s *stubStore) AttachmentPath(a mail.Attachment) string {
	if s.paths != nil {
		if p, ok := s.paths[a.Filename]; ok {
			return p
		}
	}
	return "/att/" + a.Filename
}

// Body and BodyHTML are called by the async Init goroutine.  Returning empty
// strings keeps buildBodyMD from producing output without touching the
// filesystem or network.
func (s *stubStore) Body(_ int64) (string, error)     { return "", nil }
func (s *stubStore) BodyHTML(_ int64) (string, error) { return s.bodyHTML, nil }

// threeAttachments is the canonical three-item slice used across most cases.
var threeAttachments = []mail.Attachment{
	{ID: 1, Filename: "a.pdf", SizeBytes: 100, DiskPath: "a.pdf"},
	{ID: 2, Filename: "b.png", SizeBytes: 200, DiskPath: "b.png"},
	{ID: 3, Filename: "c.zip", SizeBytes: 300, DiskPath: "c.zip"},
}

// buildModel constructs a sized mailRead model ready for Update calls.
// It does NOT run Init so no goroutine is started; callers inject
// attachments directly via bodyLoadedMsg.
func buildModel(t *testing.T, st *stubStore) mailRead {
	t.Helper()
	keys := config.Default().Keymap()
	mr := newMailRead(mailItem{messageID: 1}, st, nil, nil, keys)
	mr = mr.SetSize(80, 24).(mailRead)
	return mr
}

// injectAttachments sends a bodyLoadedMsg carrying the given attachments
// through Update and returns the updated model.
func injectAttachments(t *testing.T, mr mailRead, atts []mail.Attachment) mailRead {
	t.Helper()
	v, _ := mr.Update(bodyLoadedMsg{id: 1, attachments: atts})
	return v.(mailRead)
}

// sendKey sends a single tea.KeyMsg and returns the updated model and cmd.
func sendKey(t *testing.T, mr mailRead, key tea.KeyType) (mailRead, tea.Cmd) {
	t.Helper()
	v, cmd := mr.Update(tea.KeyMsg{Type: key})
	return v.(mailRead), cmd
}

// isPopCmd executes cmd and reports whether the resulting tea.Msg is popMsg.
func isPopCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(popMsg)
	return ok
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestMailRead_TabCyclesForwardWithWrap(t *testing.T) {
	// After 4 tabs through 3 attachments the index must wrap to 0.
	st := &stubStore{attachments: threeAttachments}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)

	for i := range 4 {
		var cmd tea.Cmd
		mr, cmd = sendKey(t, mr, tea.KeyTab)
		if cmd != nil {
			t.Errorf("tab %d: expected nil cmd, got non-nil", i+1)
		}
	}

	if mr.attachmentIdx != 0 {
		t.Errorf("after 4 tabs through 3 attachments: attachmentIdx = %d, want 0 (wrapped)", mr.attachmentIdx)
	}
}

func TestMailRead_ShiftTabFromNoneSelectsLast(t *testing.T) {
	// Shift-tab when nothing is selected (-1) should wrap to the last index.
	st := &stubStore{attachments: threeAttachments}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)

	if mr.attachmentIdx != -1 {
		t.Fatalf("precondition: attachmentIdx = %d, want -1", mr.attachmentIdx)
	}

	mr, cmd := sendKey(t, mr, tea.KeyShiftTab)

	if cmd != nil {
		t.Errorf("shift+tab: expected nil cmd, got non-nil")
	}
	want := len(threeAttachments) - 1
	if mr.attachmentIdx != want {
		t.Errorf("shift+tab from -1: attachmentIdx = %d, want %d (last)", mr.attachmentIdx, want)
	}
}

func TestMailRead_EscWithSelectionClearsWithoutPop(t *testing.T) {
	// esc when an attachment is selected clears the selection; it must NOT pop.
	st := &stubStore{attachments: threeAttachments}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)
	mr.attachmentIdx = 1

	mr, cmd := sendKey(t, mr, tea.KeyEsc)

	if mr.attachmentIdx != -1 {
		t.Errorf("esc with selection: attachmentIdx = %d, want -1", mr.attachmentIdx)
	}
	if isPopCmd(cmd) {
		t.Error("esc with selection: must not emit popMsg, but it did")
	}
}

func TestMailRead_EscWithNoSelectionPops(t *testing.T) {
	// esc when attachmentIdx == -1 must return a Pop() cmd.
	st := &stubStore{attachments: threeAttachments}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)

	if mr.attachmentIdx != -1 {
		t.Fatalf("precondition: attachmentIdx = %d, want -1", mr.attachmentIdx)
	}

	_, cmd := sendKey(t, mr, tea.KeyEsc)

	if !isPopCmd(cmd) {
		t.Error("esc with no selection: expected popMsg cmd, got none or different cmd")
	}
}

func TestMailRead_EnterWithSelectionCallsOpener(t *testing.T) {
	// enter with a selected attachment must invoke openFn with the correct path.
	// Create a real file so os.Stat passes and openFn is reached.
	bPng := filepath.Join(t.TempDir(), "b.png")
	if err := os.WriteFile(bPng, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	st := &stubStore{
		attachments: threeAttachments,
		paths:       map[string]string{"b.png": bPng},
	}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)
	mr.attachmentIdx = 1 // "b.png"

	var capturedPath string
	orig := openFn
	openFn = func(path string) error {
		capturedPath = path
		return nil
	}
	t.Cleanup(func() { openFn = orig })

	mr2, cmd := mr.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = mr2

	if capturedPath == "" {
		t.Fatal("enter with selection: openFn was not called")
	}
	if capturedPath != bPng {
		t.Errorf("enter with selection: openFn called with %q, want %q", capturedPath, bPng)
	}
	if cmd != nil {
		t.Errorf("enter with selection: expected nil cmd, got non-nil")
	}
}

func TestMailRead_EnterWithNoSelectionDoesNotCallOpener(t *testing.T) {
	// enter when no attachment is selected must not invoke openFn.
	st := &stubStore{attachments: threeAttachments}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)

	if mr.attachmentIdx != -1 {
		t.Fatalf("precondition: attachmentIdx = %d, want -1", mr.attachmentIdx)
	}

	called := false
	orig := openFn
	openFn = func(_ string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { openFn = orig })

	mr.Update(tea.KeyMsg{Type: tea.KeyEnter}) //nolint:errcheck

	if called {
		t.Error("enter with no selection: openFn should not be called, but it was")
	}
}

func TestMailRead_TabNoOpWhenNoAttachments(t *testing.T) {
	// tab when the message has no attachments must leave attachmentIdx at -1.
	st := &stubStore{}
	mr := injectAttachments(t, buildModel(t, st), nil)

	if mr.attachmentIdx != -1 {
		t.Fatalf("precondition: attachmentIdx = %d, want -1", mr.attachmentIdx)
	}

	mr, _ = sendKey(t, mr, tea.KeyTab)

	if mr.attachmentIdx != -1 {
		t.Errorf("tab with no attachments: attachmentIdx = %d, want -1", mr.attachmentIdx)
	}
}

// ── Additional edge-case coverage ────────────────────────────────────────────

func TestMailRead_ShiftTabCyclesBackwardWithWrap(t *testing.T) {
	// Full backward cycle: tab once to select idx 0, then shift-tab to wrap
	// back to the last index.
	st := &stubStore{attachments: threeAttachments}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)

	// Advance to idx 0.
	mr, _ = sendKey(t, mr, tea.KeyTab)
	if mr.attachmentIdx != 0 {
		t.Fatalf("precondition tab: attachmentIdx = %d, want 0", mr.attachmentIdx)
	}

	// Shift-tab from idx 0 must wrap to last.
	mr, cmd := sendKey(t, mr, tea.KeyShiftTab)
	if cmd != nil {
		t.Errorf("shift+tab wrap: expected nil cmd")
	}
	want := len(threeAttachments) - 1
	if mr.attachmentIdx != want {
		t.Errorf("shift+tab from 0: attachmentIdx = %d, want %d", mr.attachmentIdx, want)
	}
}

func TestMailRead_BodyLoadedMsgResetsIndex(t *testing.T) {
	// A second bodyLoadedMsg (e.g. body reloaded) must reset attachmentIdx to -1.
	st := &stubStore{attachments: threeAttachments}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)

	// Move index away from -1.
	mr, _ = sendKey(t, mr, tea.KeyTab)
	if mr.attachmentIdx == -1 {
		t.Fatal("precondition: tab should have moved attachmentIdx off -1")
	}

	// Re-inject a body loaded message; index must reset.
	mr = injectAttachments(t, mr, threeAttachments)
	if mr.attachmentIdx != -1 {
		t.Errorf("after bodyLoadedMsg: attachmentIdx = %d, want -1", mr.attachmentIdx)
	}
}

func TestMailRead_TabSequenceCoversAllAttachments(t *testing.T) {
	// Cycling tab exactly len(attachments) times returns to -1→0→1→2→0.
	// We also verify each intermediate index, not just the final one.
	st := &stubStore{attachments: threeAttachments}

	cases := []struct {
		afterTabs int
		wantIdx   int
	}{
		{1, 0},
		{2, 1},
		{3, 2},
		{4, 0}, // wrapped
	}

	for _, tc := range cases {
		// Rebuild fresh model for each subtest to avoid state bleed.
		fresh := injectAttachments(t, buildModel(t, st), threeAttachments)
		for range tc.afterTabs {
			fresh, _ = sendKey(t, fresh, tea.KeyTab)
		}
		if fresh.attachmentIdx != tc.wantIdx {
			t.Errorf("after %d tabs: attachmentIdx = %d, want %d",
				tc.afterTabs, fresh.attachmentIdx, tc.wantIdx)
		}
	}
}

func TestMailRead_EnterOpenerReceivesPathFromStore(t *testing.T) {
	// Verify that the path passed to openFn is exactly what AttachmentPath returns.
	// Create a real file so os.Stat passes and openFn is reached.
	reportPdf := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(reportPdf, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	atts := []mail.Attachment{
		{ID: 10, Filename: "report.pdf", SizeBytes: 1024, DiskPath: "report.pdf"},
	}
	st := &stubStore{
		attachments: atts,
		paths:       map[string]string{"report.pdf": reportPdf},
	}
	mr := injectAttachments(t, buildModel(t, st), atts)
	mr.attachmentIdx = 0

	var got string
	orig := openFn
	openFn = func(path string) error { got = path; return nil }
	t.Cleanup(func() { openFn = orig })

	mr.Update(tea.KeyMsg{Type: tea.KeyEnter}) //nolint:errcheck

	want := st.AttachmentPath(atts[0])
	if got != want {
		t.Errorf("openFn path = %q, want %q", got, want)
	}
}

func TestMailRead_LeaderVVHasHTMLViewLeaf(t *testing.T) {
	st := &stubStore{}
	mr := buildModel(t, st)
	entry, ok := mr.KeyMap().Lookup([]string{"v", "v"})
	if !ok {
		t.Fatal("KeyMap().Lookup([v, v]) returned not-found — leaf not registered")
	}
	if entry.Desc != "html view" {
		t.Errorf("leaf Desc = %q, want %q", entry.Desc, "html view")
	}
	if entry.Action == nil {
		t.Error("leaf Action is nil")
	}
}

func TestMailRead_OpenHTMLMsgTriggersOpener(t *testing.T) {
	dir := t.TempDir()
	origTmp := tempDirFn
	tempDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { tempDirFn = origTmp })

	var openedPath string
	origOpen := openFn
	openFn = func(path string) error { openedPath = path; return nil }
	t.Cleanup(func() { openFn = origOpen })

	st := &stubStore{bodyHTML: `<p>hello</p>`}
	mr := buildModel(t, st)

	v, cmd := mr.Update(openHTMLMsg{id: 1})
	_ = v
	if cmd == nil {
		t.Fatal("expected a tea.Cmd from openHTMLMsg, got nil")
	}
	result := cmd()
	msg, ok := result.(htmlOpenedMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want htmlOpenedMsg", result)
	}
	if msg.note != "" {
		t.Errorf("htmlOpenedMsg.note = %q, want empty (success)", msg.note)
	}
	if openedPath == "" {
		t.Error("openFn was not called")
	}
	if filepath.Dir(openedPath) != dir {
		t.Errorf("opened path %q is not inside stubbed temp dir %q", openedPath, dir)
	}
}

func TestMailRead_OpenHTMLMsgNoHTMLBody(t *testing.T) {
	called := false
	origOpen := openFn
	openFn = func(_ string) error { called = true; return nil }
	t.Cleanup(func() { openFn = origOpen })

	st := &stubStore{bodyHTML: ""}
	mr := buildModel(t, st)

	_, cmd := mr.Update(openHTMLMsg{id: 1})
	if cmd == nil {
		t.Fatal("expected a tea.Cmd")
	}
	result := cmd()
	msg, ok := result.(htmlOpenedMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want htmlOpenedMsg", result)
	}
	if msg.note != "no html body" {
		t.Errorf("note = %q, want %q", msg.note, "no html body")
	}
	if called {
		t.Error("openFn should not be called when there is no html body")
	}
}

func TestMailRead_HtmlOpenedMsgSetsOpenNote(t *testing.T) {
	st := &stubStore{}
	mr := buildModel(t, st)

	v, cmd := mr.Update(htmlOpenedMsg{note: "no html body"})
	if cmd != nil {
		t.Errorf("htmlOpenedMsg: expected nil cmd, got non-nil")
	}
	updated := v.(mailRead)
	if updated.openNote != "no html body" {
		t.Errorf("openNote = %q, want %q", updated.openNote, "no html body")
	}
}

// TestMailRead_SPressEmitsOpenPathPickerMsg verifies that pressing 's' on a
// selected attachment returns a cmd that emits openPathPickerMsg, and that
// the embedded OnSelect callback produces a saveAttachmentMsg with the correct
// destination path.
func TestMailRead_SPressEmitsOpenPathPickerMsg(t *testing.T) {
	st := &stubStore{attachments: threeAttachments}
	mr := injectAttachments(t, buildModel(t, st), threeAttachments)
	mr.attachmentIdx = 0 // select "a.pdf"

	v, cmd := mr.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	_ = v
	if cmd == nil {
		t.Fatal("'s' key: cmd is nil, want non-nil")
	}

	msg := cmd()
	openMsg, ok := msg.(openPathPickerMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want openPathPickerMsg", msg)
	}
	if openMsg.cfg.Mode != pathpicker.ModeDirs {
		t.Errorf("openPathPickerMsg.cfg.Mode = %v, want ModeDirs", openMsg.cfg.Mode)
	}
	if openMsg.cfg.OnSelect == nil {
		t.Fatal("openPathPickerMsg.cfg.OnSelect is nil")
	}

	// Invoke the callback with a chosen directory and verify the result.
	result := openMsg.cfg.OnSelect("/tmp/pickeddir")
	saveMsg, ok := result.(saveAttachmentMsg)
	if !ok {
		t.Fatalf("OnSelect returned %T, want saveAttachmentMsg", result)
	}
	wantDst := "/tmp/pickeddir/" + threeAttachments[0].Filename
	if saveMsg.dst != wantDst {
		t.Errorf("saveAttachmentMsg.dst = %q, want %q", saveMsg.dst, wantDst)
	}
}

// TestMailRead_AttachmentSavedMsgSetsNote verifies that receiving an
// attachmentSavedMsg stores the note in mr.openNote for display.
func TestMailRead_AttachmentSavedMsgSetsNote(t *testing.T) {
	st := &stubStore{}
	mr := buildModel(t, st)

	v, cmd := mr.Update(attachmentSavedMsg{note: "saved to /x"})
	if cmd != nil {
		t.Errorf("attachmentSavedMsg: expected nil cmd, got non-nil")
	}
	updated := v.(mailRead)
	if updated.openNote != "saved to /x" {
		t.Errorf("openNote = %q, want %q", updated.openNote, "saved to /x")
	}
}

// Ensure the stub itself implements mail.MessageStore at compile time.
var _ mail.MessageStore = (*stubStore)(nil)

// Keep the time import used via mailItem.receivedAt zero value.
var _ = time.Time{}

// ── Mark-seen Init tests ─────────────────────────────────────────────────────

// batchCmds calls cmd() and, if the result is a tea.BatchMsg, returns the
// individual sub-commands so callers can interrogate each message in turn.
// For a non-batch cmd it returns a single-element slice.
func batchCmds(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		return []tea.Cmd(batch)
	}
	// Wrap the original cmd so the caller can still call it.
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// hasMarkSeen reports whether any cmd in cmds returns a markSeenMsg, and if so
// also returns that message.
func hasMarkSeen(cmds []tea.Cmd) (markSeenMsg, bool) {
	for _, c := range cmds {
		if c == nil {
			continue
		}
		if m, ok := c().(markSeenMsg); ok {
			return m, true
		}
	}
	return markSeenMsg{}, false
}

// TestMailRead_InitEmitsMarkSeenWhenUnread verifies that Init batches a
// markSeenMsg when the item is unread with valid IMAP coordinates.
func TestMailRead_InitEmitsMarkSeenWhenUnread(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 1,
		uid:       42,
		folderID:  7,
		unread:    true,
	}
	mr := newMailRead(item, st, nil, nil, keys)
	mr = mr.SetSize(80, 24).(mailRead)

	cmd := mr.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd, want a BatchMsg containing markSeenMsg")
	}

	cmds := batchCmds(cmd)
	msg, ok := hasMarkSeen(cmds)
	if !ok {
		t.Fatal("Init: want a markSeenMsg in the batch, got none")
	}
	if msg.uid != 42 {
		t.Errorf("markSeenMsg.uid = %d, want 42", msg.uid)
	}
	if msg.folderID != 7 {
		t.Errorf("markSeenMsg.folderID = %d, want 7", msg.folderID)
	}
	if !msg.seen {
		t.Errorf("markSeenMsg.seen = false, want true")
	}
	if msg.popAfter {
		t.Errorf("markSeenMsg.popAfter = true, want false (auto-mark should not pop)")
	}
}

// TestMailRead_InitSkipsMarkSeenWhenAlreadyRead verifies that Init does NOT
// emit markSeenMsg when the item is already read.
func TestMailRead_InitSkipsMarkSeenWhenAlreadyRead(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 1,
		uid:       42,
		folderID:  7,
		unread:    false, // already read
	}
	mr := newMailRead(item, st, nil, nil, keys)
	mr = mr.SetSize(80, 24).(mailRead)

	cmd := mr.Init()
	cmds := batchCmds(cmd)
	if _, ok := hasMarkSeen(cmds); ok {
		t.Error("Init: must not emit markSeenMsg when item is already read, but it did")
	}
}

// TestMailRead_InitSkipsMarkSeenWhenNoUID verifies that Init does NOT emit
// markSeenMsg when the item has uid=0, even if unread=true.
func TestMailRead_InitSkipsMarkSeenWhenNoUID(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 1,
		uid:       0, // no IMAP coord
		folderID:  7,
		unread:    true,
	}
	mr := newMailRead(item, st, nil, nil, keys)
	mr = mr.SetSize(80, 24).(mailRead)

	cmd := mr.Init()
	cmds := batchCmds(cmd)
	if _, ok := hasMarkSeen(cmds); ok {
		t.Error("Init: must not emit markSeenMsg when uid=0, but it did")
	}
}

// ── Delete leaf tests ────────────────────────────────────────────────────────

// TestMailRead_DeleteLeafEmitsDeleteMsgWithPopAfter verifies that the ["m","d"]
// leaf in the mailRead KeyMap returns a deleteMsg with the item's folderID and
// uid, and popAfter==true (the read view should close after deletion).
func TestMailRead_DeleteLeafEmitsDeleteMsgWithPopAfter(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 5,
		uid:       77,
		folderID:  9,
	}
	mr := newMailRead(item, st, nil, nil, keys)

	_, action, ok := mr.KeyMap().FindLeader([]string{"m", "d"})
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
	if dmsg.uid != 77 {
		t.Errorf("deleteMsg.uid = %d, want 77", dmsg.uid)
	}
	if dmsg.folderID != 9 {
		t.Errorf("deleteMsg.folderID = %d, want 9", dmsg.folderID)
	}
	if !dmsg.popAfter {
		t.Errorf("deleteMsg.popAfter = false, want true (read view must pop after delete)")
	}
}

// TestMailRead_DeleteLeafNoIMAPCoords verifies that the ["m","d"] leaf returns a
// non-empty Note and nil Message when the item has uid==0 (no IMAP coordinates).
func TestMailRead_DeleteLeafNoIMAPCoords(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 5,
		uid:       0, // no IMAP coord
		folderID:  9,
	}
	mr := newMailRead(item, st, nil, nil, keys)

	_, action, ok := mr.KeyMap().FindLeader([]string{"m", "d"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","d"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Note == "" {
		t.Error(`["m","d"] with uid=0: want non-empty Note, got empty string`)
	}
	if result.Message != nil {
		t.Errorf(`["m","d"] with uid=0: want Message=nil, got %T`, result.Message)
	}
}

// ── Archive leaf tests ────────────────────────────────────────────────────────

// TestMailRead_ArchiveLeafEmitsArchiveMsgWithPopAfter verifies that the ["m","a"]
// leaf in the mailRead KeyMap returns an archiveMsg with the item's folderID and
// uid, and popAfter==true (the read view should close after archiving).
func TestMailRead_ArchiveLeafEmitsArchiveMsgWithPopAfter(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 5,
		uid:       77,
		folderID:  9,
	}
	mr := newMailRead(item, st, nil, nil, keys)

	_, action, ok := mr.KeyMap().FindLeader([]string{"m", "a"})
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
	if amsg.uid != 77 {
		t.Errorf("archiveMsg.uid = %d, want 77", amsg.uid)
	}
	if amsg.folderID != 9 {
		t.Errorf("archiveMsg.folderID = %d, want 9", amsg.folderID)
	}
	if !amsg.popAfter {
		t.Errorf("archiveMsg.popAfter = false, want true (read view must pop after archive)")
	}
}

// TestMailRead_ArchiveLeafNoIMAPCoords verifies that the ["m","a"] leaf returns a
// non-empty Note and nil Message when the item has uid==0 (no IMAP coordinates).
func TestMailRead_ArchiveLeafNoIMAPCoords(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 5,
		uid:       0, // no IMAP coord
		folderID:  9,
	}
	mr := newMailRead(item, st, nil, nil, keys)

	_, action, ok := mr.KeyMap().FindLeader([]string{"m", "a"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","a"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Note == "" {
		t.Error(`["m","a"] with uid=0: want non-empty Note, got empty string`)
	}
	if result.Message != nil {
		t.Errorf(`["m","a"] with uid=0: want Message=nil, got %T`, result.Message)
	}
}

// TestMailRead_MarkUnreadLeafPopsAndFlipsSeen verifies that the ["m","u"] leaf
// in the mailRead KeyMap returns a markSeenMsg with seen=false and popAfter=true.
func TestMailRead_MarkUnreadLeafPopsAndFlipsSeen(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 1,
		uid:       42,
		folderID:  7,
		flags:     mail.FlagSeen,
	}
	mr := newMailRead(item, st, nil, nil, keys)

	_, action, ok := mr.KeyMap().FindLeader([]string{"m", "u"})
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
	if msg.seen {
		t.Errorf("markSeenMsg.seen = true, want false (mark-unread action)")
	}
	if !msg.popAfter {
		t.Errorf("markSeenMsg.popAfter = false, want true (read view should pop after marking unread)")
	}
	if msg.uid != 42 {
		t.Errorf("markSeenMsg.uid = %d, want 42", msg.uid)
	}
	if msg.folderID != 7 {
		t.Errorf("markSeenMsg.folderID = %d, want 7", msg.folderID)
	}
}

// ── Flag leaf tests (mailread) ───────────────────────────────────────────────

// TestMailRead_FlagLeafSetsFlag verifies that ["m","F"] emits markFlaggedMsg
// with flagged=true when the item does not yet have FlagFlagged set.
func TestMailRead_FlagLeafSetsFlag(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 3,
		uid:       55,
		folderID:  8,
		flags:     mail.FlagSeen, // not flagged
	}
	cfg := config.Default()
	mr := newMailRead(item, st, cfg, nil, keys)

	_, action, ok := mr.KeyMap().FindLeader([]string{"m", "F"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","F"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Message == nil {
		t.Fatal(`["m","F"] action returned Message=nil, want markFlaggedMsg`)
	}
	fmsg, ok := result.Message.(markFlaggedMsg)
	if !ok {
		t.Fatalf(`["m","F"] action Message type = %T, want markFlaggedMsg`, result.Message)
	}
	if !fmsg.flagged {
		t.Errorf("markFlaggedMsg.flagged = false, want true (message not yet flagged)")
	}
	if fmsg.uid != 55 {
		t.Errorf("markFlaggedMsg.uid = %d, want 55", fmsg.uid)
	}
	if fmsg.folderID != 8 {
		t.Errorf("markFlaggedMsg.folderID = %d, want 8", fmsg.folderID)
	}
}

// TestMailRead_FlagLeafUnflagsWhenAlreadyFlagged verifies that ["m","F"] sets
// flagged=false when the item already has FlagFlagged set.
func TestMailRead_FlagLeafUnflagsWhenAlreadyFlagged(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 4,
		uid:       66,
		folderID:  8,
		flags:     mail.FlagFlagged | mail.FlagSeen,
	}
	cfg := config.Default()
	mr := newMailRead(item, st, cfg, nil, keys)

	_, action, ok := mr.KeyMap().FindLeader([]string{"m", "F"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","F"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Message == nil {
		t.Fatal(`["m","F"] action returned Message=nil, want markFlaggedMsg`)
	}
	fmsg, ok := result.Message.(markFlaggedMsg)
	if !ok {
		t.Fatalf(`["m","F"] action Message type = %T, want markFlaggedMsg`, result.Message)
	}
	if fmsg.flagged {
		t.Errorf("markFlaggedMsg.flagged = true, want false (message already flagged)")
	}
}

// TestMailRead_FlagLeafNoIMAPCoords verifies that ["m","F"] returns a
// non-empty Note and nil Message when the item has uid==0 (no IMAP coordinates).
func TestMailRead_FlagLeafNoIMAPCoords(t *testing.T) {
	st := &stubStore{}
	keys := config.Default().Keymap()
	item := mailItem{
		messageID: 5,
		uid:       0, // no IMAP coord
		folderID:  8,
	}
	cfg := config.Default()
	mr := newMailRead(item, st, cfg, nil, keys)

	_, action, ok := mr.KeyMap().FindLeader([]string{"m", "F"})
	if !ok || action == nil {
		t.Fatal(`KeyMap().FindLeader(["m","F"]): got ok=false or nil action, want a leaf action`)
	}

	result := action()
	if result.Note == "" {
		t.Error(`["m","F"] with uid=0: want non-empty Note, got empty string`)
	}
	if result.Message != nil {
		t.Errorf(`["m","F"] with uid=0: want Message=nil, got %T`, result.Message)
	}
}
