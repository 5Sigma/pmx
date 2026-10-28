package pathpicker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// key wraps a string in a tea.KeyMsg (rune input).
func key(k string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// specialKey wraps a tea.KeyType in a tea.KeyMsg.
func specialKey(t tea.KeyType) tea.Msg {
	return tea.KeyMsg{Type: t}
}

// makeFixture creates root/{alpha/, beta/, gamma/} and root/file.txt and
// returns the root path. root is a named subdirectory inside t.TempDir() so
// that openFixture can open the picker with "root/" and have the picker list
// only the fixture entries rather than the parent dir.
func makeFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "fixture")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"alpha", "beta", "gamma"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// openFixture creates a Model, sizes it, then opens it pointing at root.
// After Open, the input is forced to "root/" so the picker lists root's
// contents rather than treating the last path component as a stub filter
// (filepath.Abs inside Open strips any trailing separator).
func openFixture(t *testing.T, root string, mode Mode) *Model {
	t.Helper()
	m := New()
	m.SetSize(80, 20)
	m.Open(OpenConfig{Start: root, Mode: mode})
	// filepath.Abs strips the trailing slash that Open writes into the input,
	// causing the picker to list root's parent and use the basename as a stub.
	// Force the input to root+"/" and re-sync so the picker lists root itself.
	m.input.SetValue(root + "/")
	m.input.CursorEnd()
	m.syncFromInput()
	return m
}

func TestModel_OpensToStartDir(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	if m.listedDir != root {
		t.Errorf("listedDir = %q, want %q", m.listedDir, root)
	}
	// ModeDirs: alpha, beta, gamma are filtered (selectable); file.txt is unfiltered.
	if len(m.filtered) != 3 {
		t.Errorf("len(filtered) = %d, want 3 (alpha, beta, gamma)", len(m.filtered))
	}
	if len(m.unfiltered) != 1 {
		t.Errorf("len(unfiltered) = %d, want 1 (file.txt)", len(m.unfiltered))
	}
}

func TestModel_TypingFilters(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	m.Update(key("a"))

	if m.stub != "a" {
		t.Errorf("stub = %q, want %q", m.stub, "a")
	}
	if len(m.filtered) != 1 {
		t.Errorf("len(filtered) = %d, want 1 (only alpha)", len(m.filtered))
	}
	if len(m.filtered) == 1 {
		e := m.entries[m.filtered[0]]
		if e.Name != "alpha" {
			t.Errorf("filtered[0].Name = %q, want %q", e.Name, "alpha")
		}
	}
}

func TestModel_TabAutocompletes(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	// Move cursor to beta (index 1).
	m.Update(specialKey(tea.KeyDown))
	m.Update(specialKey(tea.KeyTab))

	wantValue := filepath.Join(root, "beta") + "/"
	if m.input.Value() != wantValue {
		t.Errorf("input.Value() = %q, want %q", m.input.Value(), wantValue)
	}
	wantDir := filepath.Join(root, "beta")
	if m.listedDir != wantDir {
		t.Errorf("listedDir = %q, want %q", m.listedDir, wantDir)
	}
}

func TestModel_UpDownNavigates(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	if m.cursor != 0 {
		t.Fatalf("precondition: cursor = %d, want 0", m.cursor)
	}

	m.Update(specialKey(tea.KeyDown))
	m.Update(specialKey(tea.KeyDown))
	if m.cursor != 2 {
		t.Errorf("after 2 downs: cursor = %d, want 2", m.cursor)
	}

	m.Update(specialKey(tea.KeyUp))
	if m.cursor != 1 {
		t.Errorf("after up: cursor = %d, want 1", m.cursor)
	}
}

func TestModel_EnterAcceptsHighlightedDir(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	// Type 'a' so stub="a" uniquely matches alpha. acceptSelection will
	// complete the stub to the highlighted entry before calling OnSelect.
	m.Update(key("a"))
	if m.stub != "a" {
		t.Fatalf("precondition: stub = %q, want %q", m.stub, "a")
	}

	var callbackPath string
	m.cfg.OnSelect = func(absPath string) tea.Msg {
		callbackPath = absPath
		return nil
	}

	cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter: cmd is nil, want non-nil (callback wrapper)")
	}
	cmd() // execute the cmd to trigger the callback

	if m.IsOpen() {
		t.Error("IsOpen() = true after enter, want false")
	}
	want := filepath.Join(root, "alpha")
	if callbackPath != want {
		t.Errorf("callback path = %q, want %q", callbackPath, want)
	}
}

func TestModel_EscClosesWithoutCallback(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	called := false
	m.cfg.OnSelect = func(_ string) tea.Msg {
		called = true
		return nil
	}

	m.Update(specialKey(tea.KeyEsc))

	if m.IsOpen() {
		t.Error("IsOpen() = true after esc, want false")
	}
	if called {
		t.Error("OnSelect was called after esc, want no callback")
	}
}

func TestModel_ModeDirs_FilesNotNavigable(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	// 3 dirs in filtered (alpha=0, beta=1, gamma=2); file.txt is unfiltered.
	// Pressing down 3 times should stop at 2, never reaching file.txt.
	m.Update(specialKey(tea.KeyDown))
	m.Update(specialKey(tea.KeyDown))
	m.Update(specialKey(tea.KeyDown))

	if m.cursor > 2 {
		t.Errorf("cursor = %d after 3 downs in ModeDirs, want <= 2 (file.txt is not navigable)", m.cursor)
	}
}

func TestModel_ModeFilesAndDirs_FilesNavigable(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeFilesAndDirs)

	// All 4 entries are in filtered: alpha=0, beta=1, gamma=2, file.txt=3.
	m.Update(specialKey(tea.KeyDown))
	m.Update(specialKey(tea.KeyDown))
	m.Update(specialKey(tea.KeyDown))
	if m.cursor != 3 {
		t.Fatalf("cursor = %d after 3 downs in ModeFilesAndDirs, want 3 (file.txt)", m.cursor)
	}

	// Type 'f' so stub="f" uniquely matches file.txt; acceptSelection will
	// complete the stub before invoking OnSelect.
	m.Update(key("f"))
	if m.stub != "f" {
		t.Fatalf("precondition: stub = %q, want %q", m.stub, "f")
	}

	var callbackPath string
	m.cfg.OnSelect = func(p string) tea.Msg {
		callbackPath = p
		return nil
	}
	cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on file.txt: cmd is nil")
	}
	cmd()

	want := filepath.Join(root, "file.txt")
	if callbackPath != want {
		t.Errorf("callback path = %q, want %q", callbackPath, want)
	}
}

func TestModel_ModeFilesAndDirs_EnterOnDirNavigatesInstead(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeFilesAndDirs)

	// Select the first entry (alpha/) — cursor starts at 0.
	m.cfg.OnSelect = func(p string) tea.Msg {
		t.Errorf("OnSelect fired with %q, want navigation instead", p)
		return nil
	}

	cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd != nil {
		t.Error("enter on directory: cmd should be nil (navigation, not selection)")
	}
	if !m.IsOpen() {
		t.Error("picker closed after entering a directory, want still open")
	}
	// After navigating in, the input should end with the dir separator.
	val := m.input.Value()
	if val[len(val)-1] != os.PathSeparator {
		t.Errorf("input value after dir enter = %q, want trailing separator", val)
	}
}

func TestModel_ModeFilesAndDirs_ArrowThenEnterSelectsFile(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeFilesAndDirs)

	// Fixture order: alpha(dir), beta(dir), gamma(dir), file.txt — navigate to file.txt.
	m.Update(specialKey(tea.KeyDown))
	m.Update(specialKey(tea.KeyDown))
	m.Update(specialKey(tea.KeyDown))
	if m.cursor != 3 {
		t.Fatalf("cursor = %d after 3 downs, want 3 (file.txt)", m.cursor)
	}

	var callbackPath string
	m.cfg.OnSelect = func(p string) tea.Msg {
		callbackPath = p
		return nil
	}

	cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("arrow-nav then enter on file: cmd is nil, want selection cmd")
	}
	cmd()

	want := filepath.Join(root, "file.txt")
	if callbackPath != want {
		t.Errorf("callback path = %q, want %q", callbackPath, want)
	}
	if m.IsOpen() {
		t.Error("picker still open after file selection, want closed")
	}
}

func TestModel_EnterCreatesNonExistentDir(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	newdir := filepath.Join(root, "newdir")
	var callbackPath string
	m.cfg.OnSelect = func(p string) tea.Msg {
		callbackPath = p
		return nil
	}

	// Point the input at a non-existent directory and sync state.
	m.input.SetValue(newdir)
	m.syncFromInput()

	cmd := m.Update(specialKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on non-existent dir: cmd is nil")
	}
	cmd()

	if callbackPath != newdir {
		t.Errorf("callback path = %q, want %q", callbackPath, newdir)
	}
	if _, err := os.Stat(newdir); os.IsNotExist(err) {
		t.Errorf("newdir %q was not created on disk", newdir)
	}
}

func TestModel_GhostText(t *testing.T) {
	root := makeFixture(t)
	m := openFixture(t, root, ModeDirs)

	// With no typing, stub is "" and cursor is on alpha. The ghost suffix
	// ("alpha/") should appear somewhere in the rendered view.
	view := m.View(80)
	if !strings.Contains(view, "alpha/") {
		t.Errorf("View(80) does not contain %q; view = %q", "alpha/", view)
	}

	// After typing 'a', the stub is "a" and ghost suffix is "lpha/".
	m.Update(key("a"))
	view = m.View(80)
	if !strings.Contains(view, "lpha/") {
		t.Errorf("after typing 'a': View(80) does not contain ghost suffix %q", "lpha/")
	}
}

func TestModel_HeightWhenOpen(t *testing.T) {
	root := makeFixture(t)
	// openFixture forces the input to root+"/" and re-syncs, so the picker
	// lists root's 3 dirs (filtered) + 1 file (unfiltered) = cap 4.
	// Height = cap(4) + 4 = 8.
	m := openFixture(t, root, ModeDirs)

	h := m.Height(80)
	if h != 8 {
		t.Errorf("Height(80) = %d, want 8 (cap=4, cap+4)", h)
	}
}

func TestModel_HeightWhenClosed(t *testing.T) {
	m := New()
	if h := m.Height(80); h != 0 {
		t.Errorf("Height(80) on closed model = %d, want 0", h)
	}
}

func TestModel_HeightEmptyDir(t *testing.T) {
	// Build an isolated empty directory using the same nesting trick as makeFixture
	// so that forcing input to emptyRoot+"/" lists only its (zero) contents.
	emptyRoot := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(emptyRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	m := New()
	m.SetSize(80, 20)
	m.Open(OpenConfig{Start: emptyRoot, Mode: ModeDirs})
	m.input.SetValue(emptyRoot + "/")
	m.input.CursorEnd()
	m.syncFromInput()

	if len(m.filtered) != 0 {
		t.Fatalf("precondition: len(filtered) = %d, want 0", len(m.filtered))
	}
	// listCap == 0, so Height == 3 (top rule + input + bottom rule).
	h := m.Height(80)
	if h != 3 {
		t.Errorf("Height(80) for empty dir = %d, want 3", h)
	}
}
