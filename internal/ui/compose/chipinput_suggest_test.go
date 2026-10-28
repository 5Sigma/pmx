package compose

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/mail"
)

// recordingSuggest returns a SuggestFunc that records every (prefix, qID)
// pair it is called with. The underlying tea.Cmd it returns is a no-op; the
// tests dispatch ContactSuggestionsMsg manually to control timing.
func recordingSuggest() (SuggestFunc, *[]struct {
	Prefix string
	QID    uint64
}) {
	var calls []struct {
		Prefix string
		QID    uint64
	}
	fn := func(prefix string, qID uint64) tea.Cmd {
		calls = append(calls, struct {
			Prefix string
			QID    uint64
		}{Prefix: prefix, QID: qID})
		return nil
	}
	return fn, &calls
}

// seedDropdown primes a chip input with two typed runes, dispatches a
// ContactSuggestionsMsg so the dropdown opens, and returns the input and the
// qID the chip advertised. The caller gets a dropdown with exactly len(results)
// rows open on index 0.
func seedDropdown(t *testing.T, results []mail.Contact) (*ChipInput, uint64) {
	t.Helper()
	c := NewChipInput("to")
	fn, calls := recordingSuggest()
	c.SetSuggestFunc(fn)
	c.Focus()

	typeString(c, "al")
	if len(*calls) == 0 {
		t.Fatalf("expected SuggestFunc to be called while typing, got 0 calls")
	}
	qID := (*calls)[len(*calls)-1].QID

	c, _ = c.Update(ContactSuggestionsMsg{QueryID: qID, Results: results})
	return c, qID
}

func TestChipInput_SuggestionsOpenOnMatchingQueryID(t *testing.T) {
	c := NewChipInput("to")
	fn, _ := recordingSuggest()
	c.SetSuggestFunc(fn)
	c.Focus()
	typeString(c, "al")

	c, _ = c.Update(ContactSuggestionsMsg{QueryID: c.queryID, Results: []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
		{Name: "Alan", Email: "alan@bar.com"},
	}})

	if !c.SuggestOpen() {
		t.Fatalf("expected dropdown to be open")
	}
	if len(c.suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(c.suggestions))
	}
	if c.selectedIdx != 0 {
		t.Errorf("expected selectedIdx=0, got %d", c.selectedIdx)
	}
}

func TestChipInput_StaleSuggestionsIgnored(t *testing.T) {
	c, qID := seedDropdown(t, []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
	})
	if !c.SuggestOpen() {
		t.Fatalf("precondition failed: dropdown should be open")
	}

	// A stale reply (older qID) arrives — must be dropped.
	c, _ = c.Update(ContactSuggestionsMsg{QueryID: qID - 1, Results: []mail.Contact{
		{Name: "Stale", Email: "stale@old.com"},
	}})

	if len(c.suggestions) != 1 || c.suggestions[0].Name != "Alice" {
		t.Errorf("stale reply overwrote current suggestions: %+v", c.suggestions)
	}
}

func TestChipInput_DownWrapsFromLastToFirst(t *testing.T) {
	c, _ := seedDropdown(t, []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
		{Name: "Alan", Email: "alan@bar.com"},
	})

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c.selectedIdx != 1 {
		t.Fatalf("expected selectedIdx=1 after one down, got %d", c.selectedIdx)
	}
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown})
	if c.selectedIdx != 0 {
		t.Errorf("expected wrap to 0, got %d", c.selectedIdx)
	}
}

func TestChipInput_UpWrapsFromFirstToLast(t *testing.T) {
	c, _ := seedDropdown(t, []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
		{Name: "Alan", Email: "alan@bar.com"},
		{Name: "Alex", Email: "alex@baz.com"},
	})

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyUp})
	if c.selectedIdx != 2 {
		t.Errorf("expected wrap to 2, got %d", c.selectedIdx)
	}
}

func TestChipInput_EnterCommitsSelectedSuggestionAndClosesDropdown(t *testing.T) {
	c, _ := seedDropdown(t, []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
		{Name: "Alan", Email: "alan@bar.com"},
	})
	// Move down so a non-default row is selected to prove index is honored.
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyDown})

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if c.SuggestOpen() {
		t.Errorf("expected dropdown closed after Enter")
	}
	if len(c.Values()) != 1 {
		t.Fatalf("expected 1 chip, got %d", len(c.Values()))
	}
	got := c.Values()[0]
	if got.Email != "alan@bar.com" || got.Name != "Alan" {
		t.Errorf("expected Alan chip, got %+v", got)
	}
	if c.PendingText() != "" {
		t.Errorf("expected input cleared, got %q", c.PendingText())
	}
	if !c.Focused() {
		t.Errorf("expected field to remain focused after accept")
	}
}

func TestChipInput_TabCommitsSelectedAndStaysFocused(t *testing.T) {
	c, _ := seedDropdown(t, []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
	})

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab})

	if c.SuggestOpen() {
		t.Errorf("expected dropdown closed after Tab")
	}
	if len(c.Values()) != 1 {
		t.Fatalf("expected 1 chip, got %d", len(c.Values()))
	}
	if c.Values()[0].Email != "alice@foo.com" {
		t.Errorf("expected alice@foo.com, got %+v", c.Values()[0])
	}
	if !c.Focused() {
		t.Errorf("expected field still focused after Tab")
	}
}

func TestChipInput_EscClosesDropdownButDoesNotCommit(t *testing.T) {
	c, _ := seedDropdown(t, []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
	})

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if c.SuggestOpen() {
		t.Errorf("expected dropdown closed")
	}
	if len(c.Values()) != 0 {
		t.Errorf("expected no chips committed, got %d", len(c.Values()))
	}
	// Typed text should still be preserved.
	if c.PendingText() != "al" {
		t.Errorf("expected typed text preserved, got %q", c.PendingText())
	}
	if !c.Focused() {
		t.Errorf("expected field still focused after Esc")
	}
}

func TestChipInput_CommaCommitsLiteralEvenWithDropdownOpen(t *testing.T) {
	c, _ := seedDropdown(t, []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
	})
	// Replace the two-letter prefix with a full address and open dropdown
	// again before testing comma. The seeded prefix "al" is not a valid
	// address so comma would set an err instead of committing — type the
	// rest of a real address first.
	typeString(c, "ex@foo.com")
	// After typing, the dropdown either stays open or re-queries; force it
	// open explicitly so we can prove comma bypasses it.
	c.open = true
	c.suggestions = []mail.Contact{{Name: "Alice", Email: "alice@foo.com"}}
	c.selectedIdx = 0

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})

	if len(c.Values()) != 1 {
		t.Fatalf("expected 1 chip from literal commit, got %d", len(c.Values()))
	}
	got := c.Values()[0]
	if got.Email != "alex@foo.com" {
		t.Errorf("expected literal typed address, got %+v", got)
	}
	if c.SuggestOpen() {
		t.Errorf("expected dropdown closed after literal commit")
	}
}

func TestChipInput_SuggestionCommitClosesDropdown(t *testing.T) {
	c, _ := seedDropdown(t, []mail.Contact{
		{Name: "Alice", Email: "alice@foo.com"},
	})
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if c.open {
		t.Errorf("expected c.open=false after accept, got true")
	}
	if len(c.suggestions) != 0 {
		t.Errorf("expected suggestions cleared, got %d", len(c.suggestions))
	}
}
