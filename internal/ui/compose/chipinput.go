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
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

// SuggestFunc fetches contact suggestions for prefix and returns a Cmd that
// resolves to a ContactSuggestionsMsg. The qID is a monotonically increasing
// token the caller echoes back so the ChipInput can discard stale results.
type SuggestFunc func(prefix string, qID uint64) tea.Cmd

// ContactSuggestionsMsg carries the result of a SuggestFunc query. It is a
// public type so the surrounding form / app can dispatch it from a Cmd
// without a parallel internal copy.
type ContactSuggestionsMsg struct {
	QueryID uint64
	Results []mail.Contact
}

// Suggestion dropdown geometry. The widget clamps to these at render time so
// a very short input doesn't yield a pencil-thin popup and a very long one
// doesn't dominate the modal.
const (
	suggestMinWidth = 36
	suggestMaxWidth = 60
	suggestMaxRows  = 6
	suggestWidthPad = 8 // extra cells added to the live input width
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

	// Contact autocomplete state.
	suggest     SuggestFunc
	suggestions []mail.Contact
	selectedIdx int
	queryID     uint64
	open        bool
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

// SetSuggestFunc installs the contact-lookup callback used to populate the
// autocomplete dropdown. Pass nil to disable suggestions entirely.
func (c *ChipInput) SetSuggestFunc(fn SuggestFunc) {
	c.suggest = fn
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
// stale validation state. Closes the suggestion dropdown so it doesn't ghost
// onto the next focused field.
func (c *ChipInput) Blur() {
	c.focused = false
	c.input.Blur()
	c.err = ""
	c.closeSuggest()
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

// SuggestOpen reports whether the autocomplete dropdown is currently showing
// results. Callers (the parent form) use this to decide whether to splice the
// overlay into the frame.
func (c *ChipInput) SuggestOpen() bool { return c.open && c.focused && len(c.suggestions) > 0 }

// closeSuggest drops the suggestion state without touching chips or text.
// Also bumps queryID so any in-flight query result arrives stale.
func (c *ChipInput) closeSuggest() {
	c.open = false
	c.suggestions = nil
	c.selectedIdx = 0
	c.queryID++
}

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
		c.closeSuggest()
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
	c.closeSuggest()
	return true
}

// commitAddress appends a pre-validated address, clears the input and closes
// the dropdown. Used when accepting an autocomplete suggestion where parsing
// is unnecessary.
func (c *ChipInput) commitAddress(a mail.Address) {
	c.chips = append(c.chips, a)
	c.input.SetValue("")
	c.err = ""
	c.closeSuggest()
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
	// Contact suggestion results arrive asynchronously. Drop stale replies
	// (QueryID mismatch) and swallow the message either way.
	if sm, ok := msg.(ContactSuggestionsMsg); ok {
		if sm.QueryID != c.queryID {
			return c, nil
		}
		if !c.focused || strings.TrimSpace(c.input.Value()) == "" {
			// Field blurred or input cleared between query and response.
			return c, nil
		}
		c.suggestions = sm.Results
		c.selectedIdx = 0
		c.open = len(sm.Results) > 0
		return c, nil
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(msg)
		return c, cmd
	}

	// Dropdown-priority keys. These take precedence over the ambient
	// chip-input bindings so the parent form sees "no, this field consumed
	// the keystroke" and does not re-interpret ↑/↓/tab as field cycling.
	if c.SuggestOpen() {
		switch km.String() {
		case "down":
			c.selectedIdx = (c.selectedIdx + 1) % len(c.suggestions)
			return c, nil
		case "up":
			c.selectedIdx = (c.selectedIdx - 1 + len(c.suggestions)) % len(c.suggestions)
			return c, nil
		case "enter":
			c.acceptSelected()
			return c, nil
		case "tab":
			c.acceptSelected()
			return c, nil
		case "esc":
			// Close dropdown but preserve typed text and keep focus. The
			// outer composeView will still see the keystroke (we return nil
			// here, not a consume signal) — however, composeView.Update
			// intercepts "esc" at the top and would normally cancel compose.
			// To prevent that, the parent form will check SuggestOpen before
			// routing esc. See compose_form.go.
			c.closeSuggest()
			return c, nil
		}
	}

	// Comma always commits typed text as a literal chip, bypassing the
	// dropdown even when it's open — matches the user's expectation that
	// typing a separator means "that's the address, move on".
	if km.Type == tea.KeyRunes && len(km.Runes) == 1 && km.Runes[0] == ',' {
		if strings.TrimSpace(c.input.Value()) != "" {
			c.commitFromInput()
			return c, nil
		}
		// Lone comma with no text to commit — swallow it so a stray key
		// doesn't produce a `,` in the buffer.
		return c, nil
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

	case tea.KeyBackspace:
		if c.input.Value() == "" && len(c.chips) > 0 {
			last := c.chips[len(c.chips)-1]
			c.chips = c.chips[:len(c.chips)-1]
			c.input.SetValue(formatAddress(last))
			c.input.CursorEnd()
			c.err = ""
			c.closeSuggest()
			return c, nil
		}
	}

	// Fall through: delegate to textinput for normal typing / navigation.
	before := c.input.Value()
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	after := c.input.Value()

	// Fire a new suggestion query whenever the buffer changed. The textinput
	// handles cursor-only keystrokes (left/right/home/end) without mutating
	// the value, so this filter avoids pointless store round-trips.
	if before != after {
		if sCmd := c.querySuggestions(after); sCmd != nil {
			cmd = tea.Batch(cmd, sCmd)
		}
	}
	return c, cmd
}

// querySuggestions kicks a new SuggestFunc call for the current buffer. If
// the buffer is empty, cleared, or looks like a committed address (contains
// " <" or "@"-plus-whitespace), the dropdown is closed instead. Returns nil
// when there's no SuggestFunc installed.
func (c *ChipInput) querySuggestions(buf string) tea.Cmd {
	trimmed := strings.TrimSpace(buf)
	if trimmed == "" {
		c.closeSuggest()
		return nil
	}
	if c.suggest == nil {
		return nil
	}
	// Bump queryID so any in-flight result is dropped as stale.
	c.queryID++
	qID := c.queryID
	// Use lowercase prefix per the backend contract.
	prefix := strings.ToLower(trimmed)
	// Strip any leading "Name <" prefix so we query by the local-part the user
	// is typing — this lets autocomplete still fire after a backspace
	// restored a chip. Treat the final whitespace-delimited token as the
	// query if the buffer contains whitespace (e.g. partial display name).
	if idx := strings.LastIndex(prefix, " "); idx >= 0 {
		candidate := strings.TrimLeftFunc(prefix[idx+1:], unicode.IsSpace)
		if candidate != "" {
			prefix = strings.TrimPrefix(candidate, "<")
		}
	}
	return c.suggest(prefix, qID)
}

// acceptSelected promotes the highlighted suggestion to a chip and clears the
// live input. No-op when the dropdown is closed or empty.
func (c *ChipInput) acceptSelected() {
	if !c.open || len(c.suggestions) == 0 {
		return
	}
	if c.selectedIdx < 0 || c.selectedIdx >= len(c.suggestions) {
		c.selectedIdx = 0
	}
	ct := c.suggestions[c.selectedIdx]
	c.commitAddress(mail.Address{Name: ct.Name, Email: ct.Email})
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
	c.closeSuggest()
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

// DropdownView renders the suggestion panel when the dropdown is open and
// returns it as a standalone string suitable for inline appending below the
// chip row. Returns "" when the dropdown is closed or empty.
//
// The panel's width is derived from the chip's drawable area (c.width) so
// it lines up under the value column when the caller prepends the form
// prefix padding. No floating-overlay or ANSI-splice logic is required.
func (c *ChipInput) DropdownView() string {
	if !c.SuggestOpen() {
		return ""
	}

	// Outer width = chip drawable area, clamped to the suggestion constants.
	w := c.width
	if w < suggestMinWidth {
		w = suggestMinWidth
	}
	if w > suggestMaxWidth {
		w = suggestMaxWidth
	}

	// Inner width = outer minus the 1-cell border on each side.
	inner := w - 2
	if inner < 8 {
		inner = 8
	}

	// How many rows to show.
	rowsCount := len(c.suggestions)
	if rowsCount > suggestMaxRows {
		rowsCount = suggestMaxRows
	}

	prefix := strings.ToLower(strings.TrimSpace(c.input.Value()))
	if idx := strings.LastIndex(prefix, " "); idx >= 0 {
		cand := strings.TrimLeftFunc(prefix[idx+1:], unicode.IsSpace)
		cand = strings.TrimPrefix(cand, "<")
		if cand != "" {
			prefix = cand
		}
	}

	var rowStrs []string
	for i := 0; i < rowsCount; i++ {
		rowStrs = append(rowStrs, c.renderSuggestionRow(c.suggestions[i], prefix, inner, i == c.selectedIdx))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorBorder).
		BorderBackground(styles.ColorBg).
		Background(styles.ColorSurface).
		Render(strings.Join(rowStrs, "\n"))
}

// renderSuggestionRow paints a single dropdown row with inner width = width.
// The layout is:
//
//	▌  Name                              <email>
//	   Alice Johnson                     <alice@foo.com>
//
// Where ▌ marks the selected row. Name is left-aligned, email right-aligned
// in angle brackets. Matching prefix inside the name or local-part is
// bolded and recoloured to ColorPrimaryHi.
func (c *ChipInput) renderSuggestionRow(ct mail.Contact, prefix string, width int, selected bool) string {
	bg := styles.ColorSurface
	fg := styles.ColorFg
	if selected {
		bg = styles.ColorAccent
		fg = styles.ColorBg
	}
	rowStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)
	hiFg := styles.ColorPrimaryHi
	if selected {
		hiFg = styles.ColorBg
	}

	// Selection glyph in the gutter — a single-cell bar when selected, a
	// space otherwise.
	gutter := rowStyle.Render(" ")
	if selected {
		gutter = lipgloss.NewStyle().
			Background(bg).
			Foreground(styles.ColorBg).
			Bold(true).
			Render("▌")
	}

	// Build name + email cells.
	emailStr := "<" + ct.Email + ">"
	name := ct.Name
	if name == "" {
		// No display name — render just the email, left-aligned.
		emailWithHi := highlightMatch(emailStr, prefix, bg, fg, hiFg)
		pad := width - 2 - lipgloss.Width(emailStr)
		if pad < 0 {
			pad = 0
		}
		return gutter +
			rowStyle.Render(" ") +
			emailWithHi +
			rowStyle.Render(strings.Repeat(" ", pad))
	}

	// Highlight the prefix in both name and the email local-part.
	nameRendered := highlightMatch(name, prefix, bg, fg, hiFg)
	emailRendered := highlightMatch(emailStr, prefix, bg, styles.ColorDimFg, hiFg)
	if selected {
		emailRendered = highlightMatch(emailStr, prefix, bg, styles.ColorBg, hiFg)
	}

	nameW := lipgloss.Width(nameRendered)
	emailW := lipgloss.Width(emailRendered)

	// Space between name and email: at least 2.
	avail := width - 2 - nameW - emailW // -2 for the leading gutter+space
	if avail < 2 {
		// Not enough room — truncate the name.
		keep := width - 2 - emailW - 2
		if keep < 1 {
			keep = 1
		}
		truncName := truncateRight(name, keep)
		nameRendered = highlightMatch(truncName, prefix, bg, fg, hiFg)
		nameW = lipgloss.Width(nameRendered)
		avail = width - 2 - nameW - emailW
		if avail < 1 {
			avail = 1
		}
	}

	spacer := rowStyle.Render(strings.Repeat(" ", avail))
	return gutter + rowStyle.Render(" ") + nameRendered + spacer + emailRendered
}

// highlightMatch renders s, bolding and recolouring to hiFg the first
// occurrence of prefix (case-insensitive). The rest of the string is painted
// in (bg, fg). When prefix is empty or not found, s is rendered uniformly.
func highlightMatch(s, prefix string, bg, fg, hiFg lipgloss.Color) string {
	plain := lipgloss.NewStyle().Background(bg).Foreground(fg)
	if prefix == "" {
		return plain.Render(s)
	}
	lower := strings.ToLower(s)
	idx := strings.Index(lower, prefix)
	if idx < 0 {
		return plain.Render(s)
	}
	hi := lipgloss.NewStyle().Background(bg).Foreground(hiFg).Bold(true)
	head := s[:idx]
	match := s[idx : idx+len(prefix)]
	tail := s[idx+len(prefix):]
	return plain.Render(head) + hi.Render(match) + plain.Render(tail)
}

// truncateRight shortens s to fit in width cells, keeping the head and
// appending an ellipsis when a cut was needed.
func truncateRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(s)
	if width-1 >= len(runes) {
		return s
	}
	return string(runes[:width-1]) + "…"
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
