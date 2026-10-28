package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	compose "github.com/5Sigma/pmx/internal/ui/compose"
	"github.com/5Sigma/pmx/internal/ui/compose/themes"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

// composeForm is a hand-crafted replacement for the previous huh-based form.
// huh ships with a theme that fights pmx's palette (bright teal title chips,
// > prompt indicators, a "/ filter" affordance on Select widgets) so the
// compose dialog gets its own small state machine instead.
//
// The form owns four fields in a fixed order: an account picker that cycles
// via left/right (no text input), two textinput.Models for To and Subject,
// and a boolean toggle for the markdown flag. All layout work happens in
// View(); Update() just routes keystrokes to the focused row.

type formField int

const (
	fieldFrom formField = iota
	fieldTo
	fieldCc
	fieldBcc
	fieldSubject
	fieldMarkdown
	fieldTheme
)

// formState tracks whether the user is still editing, submitted, or cancelled.
// composeView.Update() inspects this on every tick to decide whether to
// launch the editor or pop the view.
type formState int

const (
	formStateActive formState = iota
	formStateSubmitted
	formStateCancelled
)

// Row layout constants. The accent gutter is 2 cells (either "▌ " when focused
// or two spaces when not); the label column is 10 cells; the gap is 2 cells.
// Together they form a 14-cell prefix on every row.
const (
	formAccentW = 2
	formLabelW  = 10
	formGapW    = 2
	formPrefixW = formAccentW + formLabelW + formGapW
)

// composeForm is the custom form model. Nothing is bound by pointer; the
// outer composeView reads values back through FromAccount / ToAddr / etc.
type composeForm struct {
	fields       []formField
	focused      int
	state        formState
	accounts     []config.Account
	fromIdx      int
	to           *compose.ChipInput
	cc           *compose.ChipInput
	bcc          *compose.ChipInput
	subject      textinput.Model
	markdown     bool
	showAdvanced bool
	themes       []themes.Theme
	themeIdx     int
	width        int
	err          string
}

// newComposeForm wires up the fields and resolves the default-account index
// (matched by Name against cfg.General.DefaultAccount, falling back to the
// first configured account). Theme state is restored from the last session.
// suggest is forwarded to each recipient ChipInput to drive the contact
// autocomplete dropdown; pass nil to disable suggestions.
func newComposeForm(cfg *config.Config, suggest compose.SuggestFunc) *composeForm {
	f := &composeForm{
		focused: 1, // start on the To field — the most common first keystroke
	}

	if cfg != nil {
		f.accounts = cfg.Accounts
		if cfg.General.DefaultAccount != "" {
			for i, a := range cfg.Accounts {
				if a.Name == cfg.General.DefaultAccount {
					f.fromIdx = i
					break
				}
			}
		}
	}

	f.to = compose.NewChipInput("recipient@example.com")
	f.cc = compose.NewChipInput("cc@example.com")
	f.bcc = compose.NewChipInput("bcc@example.com")
	f.to.SetSuggestFunc(suggest)
	f.cc.SetSuggestFunc(suggest)
	f.bcc.SetSuggestFunc(suggest)
	f.subject = newComposeInput("(no subject)")

	// Load available themes and restore the last selection.
	f.themes = themes.Load(config.MarkdownThemesDir())
	if state, err := config.LoadComposeState(); err == nil && state.LastMarkdownTheme != "" {
		for i, t := range f.themes {
			if t.Name == state.LastMarkdownTheme {
				f.themeIdx = i
				break
			}
		}
	}

	f.fields = f.computeFields()

	// Focus the starting field so its cursor paints on first render.
	f.applyFocus()

	return f
}

// newComposeInput builds a pmx-styled textinput painted on ColorSurface so
// the input tiles seamlessly with the surrounding field area. The critical
// bit is Prompt = "" — huh's "> " prefix was the single biggest visual
// mismatch in the previous form.
func newComposeInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = 512
	ti.TextStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFg).
		Background(styles.ColorSurface)
	ti.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFaint).
		Background(styles.ColorSurface)
	ti.Cursor.Style = lipgloss.NewStyle().
		Foreground(styles.ColorAccent).
		Background(styles.ColorSurface)
	return ti
}

// Init focuses the current field and returns the resulting blink command so
// the cursor animates immediately on view entry.
func (f *composeForm) Init() tea.Cmd {
	return f.applyFocus()
}

// applyFocus synchronises textinput focus state with f.focused. Returns the
// blink Cmd emitted by Focus() so Init / Update can forward it to Bubbletea.
func (f *composeForm) applyFocus() tea.Cmd {
	// Default to blurred on every input; only the focused row gets Focus().
	f.to.Blur()
	f.cc.Blur()
	f.bcc.Blur()
	f.subject.Blur()
	switch f.fields[f.focused] {
	case fieldTo:
		return f.to.Focus()
	case fieldCc:
		return f.cc.Focus()
	case fieldBcc:
		return f.bcc.Focus()
	case fieldSubject:
		return f.subject.Focus()
	}
	return nil
}

// moveFocus advances (delta=+1) or retreats (delta=-1) the focus index,
// wrapping at both ends so tab-cycling always lands on a real field.
func (f *composeForm) moveFocus(delta int) tea.Cmd {
	// Auto-commit any pending address in a chip field when leaving it so
	// half-typed recipients survive a tab.
	switch f.fields[f.focused] {
	case fieldTo:
		f.to.CommitPending()
	case fieldCc:
		f.cc.CommitPending()
	case fieldBcc:
		f.bcc.CommitPending()
	}
	n := len(f.fields)
	f.focused = (f.focused + delta + n) % n
	f.err = ""
	return f.applyFocus()
}

// Update routes a Bubbletea message to the focused field. Non-key messages
// (e.g. blink ticks) are forwarded to both text inputs so the cursor keeps
// animating regardless of which row holds focus.
func (f *composeForm) Update(msg tea.Msg) (*composeForm, tea.Cmd) {
	if f.state != formStateActive {
		return f, nil
	}

	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		var cmds []tea.Cmd
		var cmd tea.Cmd
		f.to, cmd = f.to.Update(msg)
		cmds = append(cmds, cmd)
		f.cc, cmd = f.cc.Update(msg)
		cmds = append(cmds, cmd)
		f.bcc, cmd = f.bcc.Update(msg)
		cmds = append(cmds, cmd)
		f.subject, cmd = f.subject.Update(msg)
		cmds = append(cmds, cmd)
		return f, tea.Batch(cmds...)
	}

	current := f.fields[f.focused]

	// When a chip-input's suggestion dropdown is open, hand the keystroke
	// straight to the chip input so its priority bindings (↑/↓ selection,
	// Tab/Enter accept, Esc close) win over the ambient form navigation.
	// This must run BEFORE the generic esc→Cancelled branch below so a
	// first esc only dismisses the dropdown, not the whole compose view.
	// Comma is intentionally NOT caught here — the chip input handles it
	// itself and commits typed text as a literal chip.
	if chip := f.activeDropdownChip(); chip != nil {
		switch key.String() {
		case "up", "down", "enter", "tab", "esc":
			updated, cmd := chip.Update(msg)
			f.applyChipUpdate(current, updated)
			return f, cmd
		case "shift+tab":
			// Close the dropdown first (so it doesn't ghost onto the next
			// field) then cycle focus backwards.
			updated, _ := chip.Update(tea.KeyMsg{Type: tea.KeyEsc})
			f.applyChipUpdate(current, updated)
			return f, f.moveFocus(-1)
		}
	}

	// Note: esc is intercepted by composeView.Update() before this is called
	// (except when a dropdown is open, handled above), so we don't normally
	// see it here. Keeping formStateCancelled wired up for completeness in
	// case something ever synthesises an esc at this level.
	if key.String() == "esc" {
		f.state = formStateCancelled
		return f, nil
	}

	// Keys handled uniformly across every field. j/k only cycle focus when
	// the current field is NOT a text input — otherwise they'd eat literal
	// 'j' / 'k' keystrokes the user is typing into To or Subject.
	switch key.String() {
	case "tab":
		return f, f.moveFocus(+1)
	case "shift+tab":
		return f, f.moveFocus(-1)
	case "down":
		return f, f.moveFocus(+1)
	case "up":
		return f, f.moveFocus(-1)
	case "ctrl+a":
		prevField := f.fields[f.focused]
		f.showAdvanced = !f.showAdvanced
		f.fields = f.computeFields()
		// Snap focus off Cc/Bcc when they're being hidden.
		if !f.showAdvanced && (prevField == fieldCc || prevField == fieldBcc) {
			for i, fl := range f.fields {
				if fl == fieldSubject {
					f.focused = i
					break
				}
			}
			_ = f.applyFocus()
		}
		return f, nil
	}

	// Per-field dispatch.
	switch current {
	case fieldFrom:
		switch key.String() {
		case "left", "h":
			f.cycleAccount(-1)
			return f, nil
		case "right", "l":
			f.cycleAccount(+1)
			return f, nil
		case "j":
			return f, f.moveFocus(+1)
		case "k":
			return f, f.moveFocus(-1)
		case "enter":
			// Enter from the From row submits the form so the user doesn't
			// have to tab all the way down.
			f.submit()
			return f, nil
		}

	case fieldTo:
		if key.String() == "enter" && f.to.PendingText() == "" {
			f.submit()
			return f, nil
		}
		var cmd tea.Cmd
		f.to, cmd = f.to.Update(msg)
		return f, cmd

	case fieldCc:
		if key.String() == "enter" && f.cc.PendingText() == "" {
			f.submit()
			return f, nil
		}
		var cmd tea.Cmd
		f.cc, cmd = f.cc.Update(msg)
		return f, cmd

	case fieldBcc:
		if key.String() == "enter" && f.bcc.PendingText() == "" {
			f.submit()
			return f, nil
		}
		var cmd tea.Cmd
		f.bcc, cmd = f.bcc.Update(msg)
		return f, cmd

	case fieldSubject:
		if key.String() == "enter" {
			f.submit()
			return f, nil
		}
		var cmd tea.Cmd
		f.subject, cmd = f.subject.Update(msg)
		return f, cmd

	case fieldMarkdown:
		switch key.String() {
		case " ":
			f.markdown = !f.markdown
			f.fields = f.computeFields()
			if f.focused >= len(f.fields) {
				// Snap back to the Markdown row by index-lookup — its position
				// shifts with the number of recipient fields enabled above.
				for i, fl := range f.fields {
					if fl == fieldMarkdown {
						f.focused = i
						break
					}
				}
				_ = f.applyFocus()
			}
			return f, nil
		case "j":
			return f, f.moveFocus(+1)
		case "k":
			return f, f.moveFocus(-1)
		case "enter":
			f.submit()
			return f, nil
		}

	case fieldTheme:
		switch key.String() {
		case "left", "h":
			f.cycleTheme(-1)
			return f, nil
		case "right", "l":
			f.cycleTheme(+1)
			return f, nil
		case "j":
			return f, f.moveFocus(+1)
		case "k":
			return f, f.moveFocus(-1)
		case "enter":
			f.submit()
			return f, nil
		}
	}

	return f, nil
}

// activeDropdownChip returns the ChipInput whose suggestion dropdown is
// currently open, or nil when no dropdown is active. Only the focused field
// is considered — a dropdown can only be visible while its field has focus.
func (f *composeForm) activeDropdownChip() *compose.ChipInput {
	if f.focused < 0 || f.focused >= len(f.fields) {
		return nil
	}
	switch f.fields[f.focused] {
	case fieldTo:
		if f.to != nil && f.to.SuggestOpen() {
			return f.to
		}
	case fieldCc:
		if f.cc != nil && f.cc.SuggestOpen() {
			return f.cc
		}
	case fieldBcc:
		if f.bcc != nil && f.bcc.SuggestOpen() {
			return f.bcc
		}
	}
	return nil
}

// SuggestOpen reports whether any of the recipient fields currently has its
// suggestion dropdown open. Used by composeView to suppress the global esc
// cancel so the first esc only closes the dropdown.
func (f *composeForm) SuggestOpen() bool {
	return f.activeDropdownChip() != nil
}

// applyChipUpdate replaces the slot for field with the updated ChipInput
// returned from Update. Keeps the three chip pointers in sync when
// delegating to the active dropdown handler.
func (f *composeForm) applyChipUpdate(field formField, updated *compose.ChipInput) {
	switch field {
	case fieldTo:
		f.to = updated
	case fieldCc:
		f.cc = updated
	case fieldBcc:
		f.bcc = updated
	}
}

// cycleAccount moves the From selection by delta, wrapping at both ends.
// A no-op when there are fewer than two accounts.
func (f *composeForm) cycleAccount(delta int) {
	n := len(f.accounts)
	if n <= 1 {
		return
	}
	f.fromIdx = (f.fromIdx + delta + n) % n
}

// cycleTheme moves the theme selection by delta, wrapping at both ends.
// A no-op when there are fewer than two themes.
func (f *composeForm) cycleTheme(delta int) {
	n := len(f.themes)
	if n <= 1 {
		return
	}
	f.themeIdx = (f.themeIdx + delta + n) % n
}

// computeFields returns the ordered field list. Cc and Bcc are only included
// when showAdvanced is true; Theme is only appended when markdown is enabled.
func (f *composeForm) computeFields() []formField {
	fields := []formField{fieldFrom, fieldTo}
	if f.showAdvanced {
		fields = append(fields, fieldCc, fieldBcc)
	}
	fields = append(fields, fieldSubject, fieldMarkdown)
	if f.markdown {
		fields = append(fields, fieldTheme)
	}
	return fields
}

// SetShowAdvanced controls whether the Cc/Bcc rows are visible.
func (f *composeForm) SetShowAdvanced(v bool) {
	f.showAdvanced = v
	f.fields = f.computeFields()
}

// SetSuggestFunc installs the contact-suggestion callback on all three
// recipient chip inputs. Safe to call after construction when the SuggestFunc
// closure is only known once the compose view has been pushed (which is how
// the root model wires it up).
func (f *composeForm) SetSuggestFunc(fn compose.SuggestFunc) {
	if f.to != nil {
		f.to.SetSuggestFunc(fn)
	}
	if f.cc != nil {
		f.cc.SetSuggestFunc(fn)
	}
	if f.bcc != nil {
		f.bcc.SetSuggestFunc(fn)
	}
}

// submit validates and transitions to formStateSubmitted when the form is
// complete. Returns false (and populates f.err) on validation failure so the
// caller can keep the form active.
func (f *composeForm) submit() bool {
	// Auto-commit any pending input in each chip field before counting.
	f.to.CommitPending()
	f.cc.CommitPending()
	f.bcc.CommitPending()

	// Check for chip-level validation errors. On failure, surface the chip's
	// message in the form banner and bounce focus back to the offending row.
	if f.to.Err() != "" {
		f.err = "invalid address in To: " + f.to.Err()
		f.focusField(fieldTo)
		return false
	}
	if f.cc.Err() != "" {
		f.err = "invalid address in Cc: " + f.cc.Err()
		f.focusField(fieldCc)
		return false
	}
	if f.bcc.Err() != "" {
		f.err = "invalid address in Bcc: " + f.bcc.Err()
		f.focusField(fieldBcc)
		return false
	}

	// Require at least one recipient total across To/Cc/Bcc.
	if len(f.to.Values())+len(f.cc.Values())+len(f.bcc.Values()) == 0 {
		f.err = "at least one recipient is required"
		f.focusField(fieldTo)
		return false
	}

	f.err = ""
	f.state = formStateSubmitted
	return true
}

// focusField moves focus to the row for the given field. No-op when the field
// isn't currently in the field list.
func (f *composeForm) focusField(field formField) {
	for i, fl := range f.fields {
		if fl == field {
			f.focused = i
			_ = f.applyFocus()
			return
		}
	}
}

// SetWidth propagates the panel's inner content width to the text inputs so
// they size their horizontal viewport to match. Called by composeView.renderForm
// on every frame — cheap enough that we don't bother caching.
func (f *composeForm) SetWidth(w int) {
	f.width = w
	// 2 cells of trailing slack so the cursor never kisses the right border.
	inner := w - formPrefixW - 2
	if inner < 8 {
		inner = 8
	}
	f.to.SetWidth(inner)
	f.cc.SetWidth(inner)
	f.bcc.SetWidth(inner)
	f.subject.Width = inner
}

// Accessors ------------------------------------------------------------------

// FromAccount returns the account the user currently has selected, with
// graceful fall-backs for an empty configuration.
func (f *composeForm) FromAccount() config.Account {
	if f.fromIdx >= 0 && f.fromIdx < len(f.accounts) {
		return f.accounts[f.fromIdx]
	}
	if len(f.accounts) > 0 {
		return f.accounts[0]
	}
	return config.Account{}
}

// ToAddrs / CcAddrs / BccAddrs return the committed recipient chips for each
// field. Pending (uncommitted) text in the live input is NOT included; call
// submit() or let the user press Tab/Enter to flush pending text first.
func (f *composeForm) ToAddrs() []mail.Address  { return f.to.Values() }
func (f *composeForm) CcAddrs() []mail.Address  { return f.cc.Values() }
func (f *composeForm) BccAddrs() []mail.Address { return f.bcc.Values() }

func (f *composeForm) Subject() string   { return f.subject.Value() }
func (f *composeForm) UseMarkdown() bool { return f.markdown }

// Prefill setters for reply / forward flows. Must be called before the form
// is shown so initial focus (f.focused = 1 → To) is still meaningful.
func (f *composeForm) SetTo(addrs []mail.Address)  { f.to.SetValues(addrs) }
func (f *composeForm) SetCc(addrs []mail.Address)  { f.cc.SetValues(addrs) }
func (f *composeForm) SetBcc(addrs []mail.Address) { f.bcc.SetValues(addrs) }
func (f *composeForm) SetSubject(s string)         { f.subject.SetValue(s) }

// SetFromAccount selects the account whose Email (case-insensitive) matches
// email. No-op (keeps current selection) when there's no match — the caller
// shouldn't crash on an unknown prefill account.
func (f *composeForm) SetFromAccount(email string) {
	lower := strings.ToLower(email)
	for i, a := range f.accounts {
		if strings.ToLower(a.Email) == lower {
			f.fromIdx = i
			return
		}
	}
}

// SelectedThemeName returns the name of the active theme, or "" when markdown
// is off or no themes are loaded.
func (f *composeForm) SelectedThemeName() string {
	if !f.markdown {
		return ""
	}
	if f.themeIdx >= 0 && f.themeIdx < len(f.themes) {
		return f.themes[f.themeIdx].Name
	}
	return ""
}

// SelectedThemeCSS returns the CSS for the active theme, or "" when markdown
// is off or no themes are loaded.
func (f *composeForm) SelectedThemeCSS() string {
	if !f.markdown {
		return ""
	}
	if f.themeIdx >= 0 && f.themeIdx < len(f.themes) {
		return f.themes[f.themeIdx].CSS
	}
	return ""
}

// Rendering ------------------------------------------------------------------

// View draws the four form rows plus (when present) a validation error line.
// The footer is rendered separately by composeView.renderForm() so it can be
// painted on its own background section.
//
// When a chip-input's suggestion dropdown is open, the dropdown is spliced
// into the finished frame as a floating overlay — it does not occupy flow
// space, so toggling it never shifts the position of any other row.
func (f *composeForm) View() string {
	rowStrs := make([]string, 0, len(f.fields))
	for i, field := range f.fields {
		rowStrs = append(rowStrs, f.renderRow(field, i == f.focused))
	}
	if f.err != "" {
		rowStrs = append(rowStrs, f.renderError())
	}
	return strings.Join(rowStrs, "\n")
}

// renderRow is the shared row shell: a focus accent in the gutter, a
// fixed-width label column, and whatever value renderer the field type
// provides. Every element is painted on ColorSurface so the row tiles
// seamlessly with the field area's background.
func (f *composeForm) renderRow(field formField, focused bool) string {
	surface := lipgloss.NewStyle().Background(styles.ColorSurface)

	// Accent gutter. "▌ " in primary when focused, two blank cells otherwise.
	var accent string
	if focused {
		accent = lipgloss.NewStyle().
			Foreground(styles.ColorPrimary).
			Background(styles.ColorSurface).
			Bold(true).
			Render("▌") +
			surface.Render(" ")
	} else {
		accent = surface.Render("  ")
	}

	// Label column.
	labelFg := styles.ColorDimFg
	if focused {
		labelFg = styles.ColorPrimaryHi
	}
	label := lipgloss.NewStyle().
		Foreground(labelFg).
		Background(styles.ColorSurface).
		Bold(focused).
		Width(formLabelW).
		Render(f.fieldName(field))

	gap := surface.Render(strings.Repeat(" ", formGapW))

	value := f.renderValue(field, focused)

	return accent + label + gap + value
}

// fieldName returns the user-facing label for a field.
func (f *composeForm) fieldName(field formField) string {
	switch field {
	case fieldFrom:
		return "From"
	case fieldTo:
		return "To"
	case fieldCc:
		return "Cc"
	case fieldBcc:
		return "Bcc"
	case fieldSubject:
		return "Subject"
	case fieldMarkdown:
		return "Markdown"
	case fieldTheme:
		return "Theme"
	}
	return ""
}

// renderValue dispatches on field type. Each field has a slightly different
// value renderer: From draws a bracketed cycler, To/Subject embed a
// textinput, Markdown draws a chip toggle, Theme draws an arrow cycler.
func (f *composeForm) renderValue(field formField, focused bool) string {
	switch field {
	case fieldFrom:
		return f.renderFromValue(focused)
	case fieldTo:
		return f.renderChipValue(f.to, focused)
	case fieldCc:
		return f.renderChipValue(f.cc, focused)
	case fieldBcc:
		return f.renderChipValue(f.bcc, focused)
	case fieldSubject:
		return f.renderInputValue(f.subject.View(), focused)
	case fieldMarkdown:
		return f.renderMarkdownValue(focused)
	case fieldTheme:
		return f.renderThemeValue(focused)
	}
	return ""
}

// renderChipValue wraps the chip-input widget in the surface backdrop so
// its pills + live input tile seamlessly with the rest of the field area,
// matching what renderInputValue does for plain textinputs.
//
// ChipInput.View() may emit a multi-line string when chips wrap to a second
// row or the placeholder needs its own row. The row shell (renderRow) only
// prepends the 14-cell prefix once, so we manually indent every continuation
// line to align it with the value column.
//
// When the chip's suggestion dropdown is open, the panel is appended as
// additional inline rows below the input — indented by the form prefix width
// so it sits under the value column. The dropdown uses its own lipgloss
// styling, so it is NOT wrapped by surf.Render.
func (f *composeForm) renderChipValue(chip *compose.ChipInput, _ bool) string {
	surf := lipgloss.NewStyle().Background(styles.ColorSurface)
	pad := surf.Render(strings.Repeat(" ", formPrefixW))
	lines := strings.Split(chip.View(), "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = surf.Render(l)
		} else {
			lines[i] = pad + surf.Render(l)
		}
	}
	result := strings.Join(lines, "\n")

	if dv := chip.DropdownView(); dv != "" {
		for _, dl := range strings.Split(dv, "\n") {
			result += "\n" + pad + dl
		}
	}
	return result
}

// renderFromValue draws the `◀ <label> ▶` cycler. Arrows are mint when
// multiple accounts exist and faint otherwise so the key hint is honest
// about whether the control does anything.
func (f *composeForm) renderFromValue(focused bool) string {
	acct := f.FromAccount()
	label := acct.Email
	if acct.Name != "" {
		label = fmt.Sprintf("%s <%s>", acct.Name, acct.Email)
	}
	if label == "" {
		label = "(no accounts configured)"
	}

	arrowFg := styles.ColorFaint
	if len(f.accounts) > 1 {
		arrowFg = styles.ColorAccent
	}

	labelFg := styles.ColorSubdued
	if focused {
		labelFg = styles.ColorFgStrong
	}

	surf := lipgloss.NewStyle().Background(styles.ColorSurface)
	return surf.Foreground(arrowFg).Render("◀  ") +
		surf.Foreground(labelFg).Bold(focused).Render(label) +
		surf.Foreground(arrowFg).Render("  ▶")
}

// renderInputValue wraps a bubbles/textinput View() in a surface backdrop so
// the field blends with the panel. The textinput prints its own cursor and
// placeholder styling (both configured in newComposeInput to use ColorSurface).
func (f *composeForm) renderInputValue(input string, _ bool) string {
	return lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Render(input)
}

// renderMarkdownValue draws a single `[ on  ]` / `[ off ]` chip. Width is
// fixed so toggling doesn't shift the trailing hint sideways.
func (f *composeForm) renderMarkdownValue(focused bool) string {
	var chip string
	if f.markdown {
		chip = lipgloss.NewStyle().
			Background(styles.ColorAccent).
			Foreground(styles.ColorBg).
			Bold(true).
			Render(" on  ")
	} else {
		chip = lipgloss.NewStyle().
			Background(styles.ColorOverlay).
			Foreground(styles.ColorSubdued).
			Render(" off ")
	}

	surf := lipgloss.NewStyle().Background(styles.ColorSurface)
	bracketFg := styles.ColorFaint
	if focused {
		bracketFg = styles.ColorPrimaryHi
	}
	bracket := surf.Foreground(bracketFg)

	hint := ""
	if focused {
		hint = surf.
			Foreground(styles.ColorDimFg).
			Italic(true).
			Render("   space to toggle")
	}

	return bracket.Render("[") + chip + bracket.Render("]") + hint
}

// renderThemeValue draws a `◀  <name>  ▶` cycler. Arrows are accent-colored
// when multiple themes are available and faint otherwise.
func (f *composeForm) renderThemeValue(focused bool) string {
	name := "(none)"
	if f.themeIdx >= 0 && f.themeIdx < len(f.themes) {
		name = f.themes[f.themeIdx].Name
	}

	arrowFg := styles.ColorFaint
	if len(f.themes) > 1 {
		arrowFg = styles.ColorAccent
	}

	labelFg := styles.ColorSubdued
	if focused {
		labelFg = styles.ColorFgStrong
	}

	surf := lipgloss.NewStyle().Background(styles.ColorSurface)
	return surf.Foreground(arrowFg).Render("◀  ") +
		surf.Foreground(labelFg).Bold(focused).Render(name) +
		surf.Foreground(arrowFg).Render("  ▶")
}

// renderError paints the validation banner on the surface background so it
// reads as part of the field area, not the surrounding chrome.
func (f *composeForm) renderError() string {
	return lipgloss.NewStyle().
		Foreground(styles.ColorDanger).
		Background(styles.ColorSurface).
		Bold(true).
		Padding(0, 2).
		Render("✗ " + f.err)
}
