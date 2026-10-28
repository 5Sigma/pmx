package folderdrawer_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/ui/folderdrawer"
)

// threeFixtureFolders returns a fixed slice of three folders covering Inbox,
// Sent, and Trash — enough for most navigation tests.
func threeFixtureFolders() []*mail.Folder {
	return []*mail.Folder{
		{ID: 1, Name: "INBOX", Role: mail.RoleInbox},
		{ID: 2, Name: "Sent", Role: mail.RoleSent},
		{ID: 3, Name: "Trash", Role: mail.RoleTrash},
	}
}

// key is a convenience constructor for single-rune tea.KeyMsg values that
// avoids repeating []rune{ch} throughout tests.
func key(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func keyStr(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestDrawer_Navigation verifies that "j" moves the cursor down and that
// pressing Enter fires onSelect with the highlighted folder and closes the
// drawer.
func TestDrawer_Navigation(t *testing.T) {
	var selected *mail.Folder
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg {
		selected = f
		return nil
	})
	d.SetFolders(threeFixtureFolders())
	d.Open(0)

	// Two "j" presses from index 0 should land at index 2 (Trash).
	d.Update(key('j'))
	d.Update(key('j'))

	// Enter should select Trash and close the drawer.
	cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd() // fires onSelect
	}

	if selected == nil {
		t.Fatal("onSelect was not called after Enter")
	}
	if selected.ID != 3 {
		t.Errorf("expected Trash (ID=3) to be selected, got ID=%d name=%q", selected.ID, selected.Name)
	}
	if d.IsOpen() {
		t.Error("drawer should close after selection")
	}
}

// TestDrawer_NavigationBoundaries verifies that the cursor does not go below 0
// or above len(folders)-1.
func TestDrawer_NavigationBoundaries(t *testing.T) {
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg { return nil })
	d.SetFolders(threeFixtureFolders())
	d.Open(0)

	// Moving up at the top should stay at 0 — the drawer should not panic or go negative.
	d.Update(key('k'))
	d.Update(key('k'))

	// The drawer must still be open and functional.
	if !d.IsOpen() {
		t.Error("drawer should remain open after boundary navigation")
	}

	// Moving down past the last item should clamp.
	for i := 0; i < 10; i++ {
		d.Update(key('j'))
	}
	// Selecting now should get the last folder (Trash, ID=3), not panic.
	var got *mail.Folder
	d2 := folderdrawer.New(func(f *mail.Folder) tea.Msg {
		got = f
		return nil
	})
	d2.SetFolders(threeFixtureFolders())
	d2.Open(0)
	for i := 0; i < 10; i++ {
		d2.Update(key('j'))
	}
	cmd := d2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if got == nil {
		t.Fatal("onSelect not called after boundary navigation + Enter")
	}
	if got.ID != 3 {
		t.Errorf("want ID=3 (Trash) after clamping, got ID=%d", got.ID)
	}
}

// TestDrawer_Filter verifies that typing "/" then characters narrows the
// visible folder list: the View should contain only matching names.
func TestDrawer_Filter(t *testing.T) {
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg { return nil })
	d.SetFolders([]*mail.Folder{
		{ID: 1, Name: "INBOX"},
		{ID: 2, Name: "Sent"},
		{ID: 3, Name: "Sent Items"},
	})
	d.Open(0)
	d.SetSize(100, 30)

	// Enter filter mode.
	d.Update(key('/'))

	// Type "sent".
	for _, ch := range "sent" {
		d.Update(key(ch))
	}

	v := d.View()
	if !strings.Contains(v, "Sent") {
		t.Error("expected 'Sent' in filtered view, got none")
	}
	if strings.Contains(v, "INBOX") {
		t.Error("expected 'INBOX' to be filtered out, but it appears in the view")
	}
}

// TestDrawer_FilterEnterSelectsFirstMatch verifies that pressing Enter in filter
// mode selects the first visible match.
func TestDrawer_FilterEnterSelectsFirstMatch(t *testing.T) {
	var selected *mail.Folder
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg {
		selected = f
		return nil
	})
	d.SetFolders([]*mail.Folder{
		{ID: 1, Name: "INBOX"},
		{ID: 2, Name: "Sent"},
		{ID: 3, Name: "Trash"},
	})
	d.Open(0)
	d.SetSize(100, 30)

	// Filter down to "sent".
	d.Update(key('/'))
	for _, ch := range "sent" {
		d.Update(key(ch))
	}

	// Enter in filter mode selects the first (and only) visible match.
	cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}

	if selected == nil {
		t.Fatal("onSelect was not called after Enter in filter mode")
	}
	if selected.ID != 2 {
		t.Errorf("expected Sent (ID=2), got ID=%d name=%q", selected.ID, selected.Name)
	}
	if d.IsOpen() {
		t.Error("drawer should close after selection in filter mode")
	}
}

// TestDrawer_EscClosesFilter verifies that pressing Esc while filtering clears
// the filter and returns to normal navigation mode without closing the drawer.
func TestDrawer_EscClosesFilter(t *testing.T) {
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg { return nil })
	d.SetFolders(threeFixtureFolders())
	d.Open(0)
	d.SetSize(100, 30)

	// Enter filter mode and type something.
	d.Update(key('/'))
	for _, ch := range "trash" {
		d.Update(key(ch))
	}

	if !d.IsCapturingInput() {
		t.Error("drawer should be capturing input after '/'")
	}

	// Esc should exit filter mode but leave the drawer open.
	d.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !d.IsOpen() {
		t.Error("drawer should remain open after Esc exits filter mode")
	}
	if d.IsCapturingInput() {
		t.Error("drawer should not be capturing input after Esc exits filter mode")
	}

	// After clearing the filter, all folders should be visible again.
	v := d.View()
	if !strings.Contains(v, "INBOX") {
		t.Error("expected 'INBOX' visible after filter cleared, but not found in view")
	}
}

// TestDrawer_EscClosesDrawer verifies that pressing Esc in normal navigation
// mode closes the drawer entirely.
func TestDrawer_EscClosesDrawer(t *testing.T) {
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg { return nil })
	d.SetFolders(threeFixtureFolders())
	d.Open(0)

	if !d.IsOpen() {
		t.Fatal("drawer should be open after Open()")
	}

	d.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if d.IsOpen() {
		t.Error("drawer should be closed after Esc in navigation mode")
	}
	if d.Width() != 0 {
		t.Errorf("Width() should be 0 when closed, got %d", d.Width())
	}
}

// TestDrawer_GNavigation verifies that "g" jumps to the first folder and "G"
// jumps to the last.
func TestDrawer_GNavigation(t *testing.T) {
	var selected *mail.Folder
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg {
		selected = f
		return nil
	})
	d.SetFolders(threeFixtureFolders())
	d.Open(0)

	// Move to the bottom with G.
	d.Update(keyStr("G"))
	cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if selected == nil || selected.ID != 3 {
		t.Errorf("G: expected Trash (ID=3), got %v", selected)
	}

	// Reopen and move back to top with g.
	selected = nil
	d.Open(0)
	d.Update(key('j'))
	d.Update(key('j')) // cursor at 2
	d.Update(key('g')) // jump back to 0
	cmd = d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if selected == nil || selected.ID != 1 {
		t.Errorf("g: expected INBOX (ID=1), got %v", selected)
	}
}

// TestDrawer_OpenResetsCursor verifies that calling Open() always resets the
// cursor to position 0 regardless of where it was before.
func TestDrawer_OpenResetsCursor(t *testing.T) {
	var selected *mail.Folder
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg {
		selected = f
		return nil
	})
	d.SetFolders(threeFixtureFolders())
	d.Open(0)

	// Move cursor to last item.
	d.Update(key('j'))
	d.Update(key('j'))

	// Re-open should reset to 0.
	d.Open(0)

	cmd := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if selected == nil || selected.ID != 1 {
		t.Errorf("after Open(): expected INBOX (ID=1) at cursor 0, got %v", selected)
	}
}

// TestDrawer_SetFoldersUpdatesVisible verifies that calling SetFolders after the
// drawer is open immediately updates the folder list.
func TestDrawer_SetFoldersUpdatesVisible(t *testing.T) {
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg { return nil })
	d.SetFolders(threeFixtureFolders())
	d.Open(0)
	d.SetSize(100, 30)

	// Replace with a different folder list.
	d.SetFolders([]*mail.Folder{
		{ID: 10, Name: "Work"},
		{ID: 11, Name: "Personal"},
	})

	v := d.View()
	if !strings.Contains(v, "Work") {
		t.Error("expected 'Work' in view after SetFolders, not found")
	}
	if strings.Contains(v, "INBOX") {
		t.Error("expected old 'INBOX' folder to be gone after SetFolders")
	}
}

// TestDrawer_ClosedViewIsEmpty verifies that View() returns an empty string
// when the drawer is closed.
func TestDrawer_ClosedViewIsEmpty(t *testing.T) {
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg { return nil })
	d.SetFolders(threeFixtureFolders())
	// Drawer is closed by default.

	if d.IsOpen() {
		t.Error("drawer should be closed after construction")
	}
	if v := d.View(); v != "" {
		t.Errorf("closed drawer View() should return empty string, got %q", v[:min(len(v), 40)])
	}
	if d.Width() != 0 {
		t.Errorf("Width() of closed drawer should be 0, got %d", d.Width())
	}
}

// TestDrawer_BlockCurrentPreventsSelect verifies that when SetBlockCurrent(true)
// is set, pressing Enter on the highlighted folder that matches currentFolderID
// is a no-op (callback not fired, drawer stays open).
func TestDrawer_BlockCurrentPreventsSelect(t *testing.T) {
	var selected *mail.Folder
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg {
		selected = f
		return nil
	})
	folders := threeFixtureFolders()
	d.SetFolders(folders)
	d.SetBlockCurrent(true)
	d.Open(1) // currentFolderID = 1 (INBOX); cursor positions on INBOX

	enter := tea.KeyMsg{Type: tea.KeyEnter}
	d.Update(enter)

	if selected != nil {
		t.Errorf("callback should not fire when blocking current folder, got %v", selected.Name)
	}
	if !d.IsOpen() {
		t.Error("drawer should remain open when Enter on blocked folder")
	}
}

// TestDrawer_BlockCurrentAllowsOtherFolders verifies that when SetBlockCurrent(true)
// is set, pressing Enter on a non-current folder still fires the callback and
// closes the drawer.
func TestDrawer_BlockCurrentAllowsOtherFolders(t *testing.T) {
	var selected *mail.Folder
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg {
		selected = f
		return nil
	})
	folders := threeFixtureFolders()
	d.SetFolders(folders)
	d.SetBlockCurrent(true)
	d.Open(1) // currentFolderID = 1 (INBOX)

	// Move cursor to Sent (ID=2).
	d.Update(key('j'))

	enter := tea.KeyMsg{Type: tea.KeyEnter}
	cmd := d.Update(enter)
	if cmd != nil {
		cmd()
	}

	if selected == nil {
		t.Fatal("callback should fire for non-current folder")
	}
	if selected.ID != 2 {
		t.Errorf("want Sent (ID=2), got ID=%d", selected.ID)
	}
	if d.IsOpen() {
		t.Error("drawer should close after selecting a non-blocked folder")
	}
}

// TestDrawer_BlockCurrentClearedOnClose verifies that Close resets the block
// flag so a subsequent Open without SetBlockCurrent(true) operates normally.
func TestDrawer_BlockCurrentClearedOnClose(t *testing.T) {
	var selected *mail.Folder
	d := folderdrawer.New(func(f *mail.Folder) tea.Msg {
		selected = f
		return nil
	})
	folders := threeFixtureFolders()
	d.SetFolders(folders)

	// First open: block current, then close without selecting.
	d.SetBlockCurrent(true)
	d.Open(1)
	d.Close()

	// Second open: no explicit SetBlockCurrent — blockCurrent must be false.
	d.Open(1) // cursor on INBOX again
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	cmd := d.Update(enter)
	if cmd != nil {
		cmd()
	}

	if selected == nil {
		t.Error("callback should fire after block was cleared by Close")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
