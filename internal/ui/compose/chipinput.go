// Package compose owns the compose-flow building blocks. chipinput.go
// implements a self-contained bubbletea component that renders a row of
// "pills" (committed addresses) followed by a live textinput for the next
// address — the kind of multi-recipient field found in modern mail clients.
//
// The component is intentionally decoupled from the surrounding form: it
// paints only its own pills + input, exposes focus/err/width hooks, and
// leaves the field label, prefix column and surface background to the
// caller.
package compose

import (
	"fmt"
	netmail "net/mail"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

// ChipInput is a multi-value email entry widget. Committed addresses are
// rendered as styled pills; the next address is typed into an embedded
// textinput. Enter / comma commits, Backspace on an empty input restores the
// last pill for editing.
//
// Zero-value is NOT usable — construct with NewChipInput.
type ChipInput struct {
	chips       []mail.Address
	input       textinput.Model
	width       int
	focused     bool
	err         string
	placeholder string
}

// chipStyle returns the pill style for a committed address. The focused
// variant uses the mint accent to read as "this field is active"; blurred
// chips sit on the overlay colour so they still contrast against the
// surface backdrop without demanding attention.
func chipStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().
			Background(styles.ColorAccent).
			Foreground(styles.ColorBg).
			Padding(0, 1)
	}
	return lipgloss.NewStyle().
		Background(styles.ColorOverlay).
		Foreground(styles.ColorFg).
		Padding(0, 1)
}

// NewChipInput builds a ChipInput with the given placeholder. The embedded
// textinput mirrors newComposeInput's styling so chip rows blend with the
// rest of the compose form.
func NewChipInput(placeholder string) *ChipInput {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = 320
	ti.TextStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFg).
		Background(styles.ColorSurface)
	ti.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFaint).
		Background(styles.ColorSurface)
	ti.Cursor.Style = lipgloss.NewStyle().
		Foreground(styles.ColorAccent).
		Background(styles.ColorSurface)
	return &ChipInput{
		input:       ti,
		placeholder: placeholder,
	}
}

// SetValues replaces the current chip list wholesale. Pending input text and
// error state are preserved — callers typically only prefill at construction
// time (e.g. seeding a reply-all draft) so touching the live buffer would be
// surprising.
func (c *ChipInput) SetValues(addrs []mail.Address) {
	if addrs == nil {
		c.chips = nil
		return
	}
	c.chips = make([]mail.Address, len(addrs))
	copy(c.chips, addrs)
}

// Values returns a defensive copy of the committed chips. Pending text in
// the live input is NOT included; call CommitPending first if the caller
// wants to flush it.
func (c *ChipInput) Values() []mail.Address {
	if len(c.chips) == 0 {
		return nil
	}
	out := make([]mail.Address, len(c.chips))
	copy(out, c.chips)
	return out
}

// Focus marks the widget as active and focuses the embedded textinput.
// Returns the blink Cmd so callers can forward it to Bubbletea.
func (c *ChipInput) Focus() tea.Cmd {
	c.focused = true
	return c.input.Focus()
}

// Blur releases focus and clears the error so the next focus doesn't inherit
// stale validation state.
func (c *ChipInput) Blur() {
	c.focused = false
	c.input.Blur()
	c.err = ""
}

// Focused reports whether the widget currently owns focus.
func (c *ChipInput) Focused() bool { return c.focused }

// Err returns the current validation message (empty when clean).
func (c *ChipInput) Err() string { return c.err }

// ClearErr wipes the validation message without touching chips or text.
func (c *ChipInput) ClearErr() { c.err = "" }

// PendingText returns the raw contents of the live textinput — the address
// the user is currently typing but has not yet committed.
func (c *ChipInput) PendingText() string { return c.input.Value() }

// CommitPending attempts to parse-and-promote whatever is in the live input.
//
//   - empty input → true (nothing to commit is not a failure)
//   - parses as a valid address → chip added, input cleared, err cleared,
//     returns true
//   - parse fails → err set, text retained for correction, returns false
//
// Callers invoke this before shifting focus so addresses don't vanish when
// the user tabs away mid-typing.
func (c *ChipInput) CommitPending() bool {
	raw := strings.TrimSpace(c.input.Value())
	if raw == "" {
		c.err = ""
		return true
	}
	addr, err := netmail.ParseAddress(raw)
	if err != nil {
		c.err = "invalid address"
		return false
	}
	c.chips = append(c.chips, mail.Address{Name: addr.Name, Email: addr.Address})
	c.input.SetValue("")
	c.err = ""
	return true
}

// SetWidth records the inner content width available for pill wrapping.
// Callers pass the already-adjusted width (minus any label / prefix columns)
// so the widget can treat c.width as its true drawable area.
func (c *ChipInput) SetWidth(w int) {
	if w < 0 {
		w = 0
	}
	c.width = w
	// Give the textinput most of the row so wide pastes don't horizontally
	// scroll inside a 4-char window. The pill wrap logic trims the input
	// line further when needed.
	if w > 0 {
		c.input.Width = w
	}
}

// Update routes incoming messages. Only tea.KeyMsg triggers chip-specific
// behaviour; everything else — including blink ticks — flows to the embedded
// textinput so the cursor animates even while the widget isn't focused.
func (c *ChipInput) Update(msg tea.Msg) (*ChipInput, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(msg)
		return c, cmd
	}

	switch km.Type {
	case tea.KeyEnter:
		if strings.TrimSpace(c.input.Value()) == "" {
			// Empty + Enter is "submit the form" — hand control back to the
			// parent without consuming the keystroke's semantics.
			return c, nil
		}
		c.commitFromInput()
		return c, nil

	case tea.KeyRunes:
		// Comma is the second commit trigger. Compare the raw runes so we
		// don't accidentally swallow Shift+<,> or other modifier combos.
		if len(km.Runes) == 1 && km.Runes[0] == ',' {
			if strings.TrimSpace(c.input.Value()) != "" {
				c.commitFromInput()
				return c, nil
			}
			// Lone comma with no text to commit — swallow it so a stray key
			// doesn't produce a `,` in the buffer.
			return c, nil
		}

	case tea.KeyBackspace:
		if c.input.Value() == "" && len(c.chips) > 0 {
			last := c.chips[len(c.chips)-1]
			c.chips = c.chips[:len(c.chips)-1]
			c.input.SetValue(formatAddress(last))
			c.input.CursorEnd()
			c.err = ""
			return c, nil
		}
	}

	// Fall through: delegate to textinput for normal typing / navigation.
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return c, cmd
}

// commitFromInput is the shared Enter / comma hot path. Parses the trimmed
// buffer, appends on success, retains on failure. Guaranteed to be called
// with a non-empty, non-whitespace buffer.
func (c *ChipInput) commitFromInput() {
	raw := strings.TrimSpace(c.input.Value())
	addr, err := netmail.ParseAddress(raw)
	if err != nil {
		c.err = "invalid address"
		return
	}
	c.chips = append(c.chips, mail.Address{Name: addr.Name, Email: addr.Address})
	c.input.SetValue("")
	c.err = ""
}

// View renders the chip row(s) + live input + optional error line. The
// outer form is responsible for painting the surface background and prefix
// column; this method emits only the widget's own content.
func (c *ChipInput) View() string {
	pillStyle := chipStyle(c.focused)
	avail := c.width
	if avail <= 0 {
		avail = 80
	}

	// Render each chip to its final styled string and record its visible
	// width so the greedy packer can measure without re-rendering.
	type cell struct {
		s string
		w int
	}
	pills := make([]cell, 0, len(c.chips))
	for _, ch := range c.chips {
		s := pillStyle.Render(formatAddress(ch))
		pills = append(pills, cell{s: s, w: lipgloss.Width(s)})
	}

	inputView := c.input.View()
	inputW := lipgloss.Width(inputView)

	// Greedy pack: fill each row until adding the next pill (+1 separator)
	// would overflow, then wrap. The live input always sits on the final
	// row — if there's no room for it beside the trailing pill, drop it to
	// a fresh row rather than clip it.
	var rows []string
	var cur strings.Builder
	curW := 0
	for i, p := range pills {
		sepW := 0
		if curW > 0 {
			sepW = 1
		}
		if curW > 0 && curW+sepW+p.w > avail {
			rows = append(rows, cur.String())
			cur.Reset()
			curW = 0
			sepW = 0
		}
		if sepW == 1 {
			cur.WriteString(" ")
			curW++
		}
		cur.WriteString(p.s)
		curW += p.w
		_ = i
	}

	// Append input to the trailing row, wrapping first if it won't fit.
	sepW := 0
	if curW > 0 {
		sepW = 1
	}
	if curW > 0 && curW+sepW+inputW > avail {
		rows = append(rows, cur.String())
		cur.Reset()
		sepW = 0
	}
	if sepW == 1 {
		cur.WriteString(" ")
	}
	cur.WriteString(inputView)
	rows = append(rows, cur.String())

	out := strings.Join(rows, "\n")
	if c.err != "" {
		errLine := lipgloss.NewStyle().Foreground(styles.ColorDanger).Render(c.err)
		out = out + "\n" + errLine
	}
	return out
}

// formatAddress renders a mail.Address for display inside a pill or when
// restoring a popped chip back into the input. The RFC-ish "Name <email>"
// form matches what net/mail.ParseAddress accepts so the round-trip through
// Backspace stays lossless.
func formatAddress(a mail.Address) string {
	if a.Name == "" {
		return a.Email
	}
	return fmt.Sprintf("%s <%s>", a.Name, a.Email)
}
