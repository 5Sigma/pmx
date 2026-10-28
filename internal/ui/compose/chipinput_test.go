package compose

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/mail"
)

// typeString feeds a string into the chip input one rune at a time via
// synthetic KeyRunes messages — mirrors how bubbletea delivers printable
// characters at runtime without needing a tea.Program.
func typeString(c *ChipInput, s string) {
	for _, r := range s {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestChipInput_EnterCommitsValidAddress(t *testing.T) {
	c := NewChipInput("to")
	c.Focus()
	typeString(c, "alice@example.com")

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := len(c.Values()); got != 1 {
		t.Fatalf("expected 1 chip, got %d", got)
	}
	if c.Values()[0].Email != "alice@example.com" {
		t.Errorf("expected alice@example.com, got %q", c.Values()[0].Email)
	}
	if c.PendingText() != "" {
		t.Errorf("expected input cleared, got %q", c.PendingText())
	}
	if c.Err() != "" {
		t.Errorf("expected no err, got %q", c.Err())
	}
}

func TestChipInput_EnterOnInvalidTextKeepsText(t *testing.T) {
	c := NewChipInput("to")
	c.Focus()
	typeString(c, "not-an-email")

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := len(c.Values()); got != 0 {
		t.Fatalf("expected 0 chips, got %d", got)
	}
	if c.PendingText() != "not-an-email" {
		t.Errorf("expected text retained, got %q", c.PendingText())
	}
	if c.Err() == "" {
		t.Errorf("expected err set, got empty")
	}
}

func TestChipInput_CommaCommitsLikeEnter(t *testing.T) {
	c := NewChipInput("to")
	c.Focus()
	typeString(c, "bob@example.com")

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})

	if got := len(c.Values()); got != 1 {
		t.Fatalf("expected 1 chip, got %d", got)
	}
	if c.Values()[0].Email != "bob@example.com" {
		t.Errorf("expected bob@example.com, got %q", c.Values()[0].Email)
	}
	if c.PendingText() != "" {
		t.Errorf("expected input cleared, got %q", c.PendingText())
	}
}

func TestChipInput_BackspaceOnEmptyRestoresLastChip(t *testing.T) {
	c := NewChipInput("to")
	c.SetValues([]mail.Address{
		{Email: "a@example.com"},
		{Name: "Bob", Email: "b@example.com"},
	})
	c.Focus()

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if got := len(c.Values()); got != 1 {
		t.Fatalf("expected 1 chip remaining, got %d", got)
	}
	if c.Values()[0].Email != "a@example.com" {
		t.Errorf("expected a@example.com, got %q", c.Values()[0].Email)
	}
	// Named chip should round-trip as "Name <email>".
	if c.PendingText() != "Bob <b@example.com>" {
		t.Errorf("expected restored text, got %q", c.PendingText())
	}
}

func TestChipInput_BackspaceOnEmptyWithNoChipsIsNoop(t *testing.T) {
	c := NewChipInput("to")
	c.Focus()

	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	if got := len(c.Values()); got != 0 {
		t.Errorf("expected 0 chips, got %d", got)
	}
	if c.PendingText() != "" {
		t.Errorf("expected empty input, got %q", c.PendingText())
	}
}

func TestChipInput_SetValuesValuesRoundTrip(t *testing.T) {
	in := []mail.Address{
		{Name: "Alice", Email: "a@example.com"},
		{Email: "b@example.com"},
	}
	c := NewChipInput("to")
	c.SetValues(in)
	out := c.Values()
	if len(out) != len(in) {
		t.Fatalf("length mismatch: got %d want %d", len(out), len(in))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("chip %d: got %+v want %+v", i, out[i], in[i])
		}
	}
	// Returned slice must be defensive — mutating it should not touch the
	// widget's internal state.
	out[0].Email = "mutated"
	if c.Values()[0].Email == "mutated" {
		t.Errorf("Values() returned non-defensive copy")
	}
}

func TestChipInput_CommitPendingValid(t *testing.T) {
	c := NewChipInput("to")
	c.Focus()
	typeString(c, "carol@example.com")

	if ok := c.CommitPending(); !ok {
		t.Fatalf("expected CommitPending to succeed")
	}
	if got := len(c.Values()); got != 1 {
		t.Fatalf("expected 1 chip, got %d", got)
	}
	if c.PendingText() != "" {
		t.Errorf("expected input cleared, got %q", c.PendingText())
	}
	if c.Err() != "" {
		t.Errorf("expected no err, got %q", c.Err())
	}
}

func TestChipInput_CommitPendingInvalid(t *testing.T) {
	c := NewChipInput("to")
	c.Focus()
	typeString(c, "garbage")

	if ok := c.CommitPending(); ok {
		t.Fatalf("expected CommitPending to fail")
	}
	if got := len(c.Values()); got != 0 {
		t.Fatalf("expected 0 chips, got %d", got)
	}
	if c.Err() == "" {
		t.Errorf("expected err set")
	}
}

func TestChipInput_CommitPendingEmpty(t *testing.T) {
	c := NewChipInput("to")
	c.Focus()

	if ok := c.CommitPending(); !ok {
		t.Fatalf("expected CommitPending on empty to return true")
	}
	if got := len(c.Values()); got != 0 {
		t.Errorf("expected 0 chips, got %d", got)
	}
	if c.Err() != "" {
		t.Errorf("expected no err on empty commit, got %q", c.Err())
	}
}
