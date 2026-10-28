package accountdrawer

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// twoAccounts is the canonical fixture used across several tests.
var twoAccounts = []AccountStat{
	{Name: "work", Email: "alice@work.com"},
	{Name: "personal", Email: "alice@gmail.com"},
}

func TestNew(t *testing.T) {
	m := New(nil)
	if m == nil {
		t.Fatal("New(nil) returned nil")
	}
	if m.IsOpen() {
		t.Errorf("expected IsOpen() == false after New, got true")
	}
	if w := m.Width(); w != 0 {
		t.Errorf("expected Width() == 0 after New, got %d", w)
	}
}

func TestOpen(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.SetAccounts(twoAccounts)
	m.Open("work")

	if !m.IsOpen() {
		t.Errorf("expected IsOpen() == true after Open, got false")
	}
	if w := m.Width(); w <= 0 {
		t.Errorf("expected Width() > 0 after SetSize + Open, got %d", w)
	}
}

func TestClose(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.SetAccounts(twoAccounts)
	m.Open("work")
	m.Close()

	if m.IsOpen() {
		t.Errorf("expected IsOpen() == false after Close, got true")
	}
	if w := m.Width(); w != 0 {
		t.Errorf("expected Width() == 0 after Close, got %d", w)
	}
}

// TestSetAccounts_FilterMatchesNameAndEmail verifies that pressing "/" enters
// filter mode (IsCapturingInput true) and subsequent rune input is accepted.
func TestSetAccounts_FilterMatchesNameAndEmail(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.SetAccounts([]AccountStat{
		{Name: "work", Email: "alice@work.com"},
		{Name: "personal", Email: "alice@gmail.com"},
	})
	m.Open("work")

	// Entering "/" should activate filter mode.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	if !m.IsCapturingInput() {
		t.Errorf("expected IsCapturingInput() == true after pressing '/', got false")
	}

	// Typing a rune while in filter mode should stay in capture mode.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})

	if !m.IsCapturingInput() {
		t.Errorf("expected IsCapturingInput() == true after typing rune in filter mode, got false")
	}
}

// TestSelectActive_SoftDismiss verifies that pressing enter when the cursor is
// already on the active account closes the drawer without invoking onSelect.
func TestSelectActive_SoftDismiss(t *testing.T) {
	called := false
	onSelect := func(name string) tea.Msg {
		called = true
		return nil
	}

	m := New(onSelect)
	m.SetSize(120, 20)
	m.SetAccounts([]AccountStat{{Name: "work"}})
	m.Open("work") // cursor lands on "work", which is also active

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if called {
		t.Errorf("expected onSelect NOT to be called when selecting the active account (soft dismiss)")
	}
}

// TestSelectOther_Dispatches verifies that pressing enter on a non-active row
// invokes onSelect with the correct account name.
func TestSelectOther_Dispatches(t *testing.T) {
	var selected string
	onSelect := func(name string) tea.Msg {
		selected = name
		return nil
	}

	m := New(onSelect)
	m.SetSize(120, 20)
	m.SetAccounts(twoAccounts)
	m.Open("work") // cursor starts at "work" (index 0)

	// Navigate down to "personal" (index 1).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})

	cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a non-nil tea.Cmd when selecting a different account")
	}
	// Execute the command to trigger the callback.
	cmd()

	if selected != "personal" {
		t.Errorf("expected onSelect called with %q, got %q", "personal", selected)
	}
}

func TestView_NonEmpty(t *testing.T) {
	m := New(nil)
	m.SetSize(120, 20)
	m.SetAccounts(twoAccounts)
	m.Open("work")

	view := m.View()
	if view == "" {
		t.Errorf("expected non-empty View() when drawer is open with accounts")
	}
}

func TestStatusKeys_NonEmpty(t *testing.T) {
	m := New(nil)
	keys := m.StatusKeys()
	if !strings.Contains(keys, "ACCOUNTS") {
		t.Errorf("expected StatusKeys() to contain %q, got: %q", "ACCOUNTS", keys)
	}
}
