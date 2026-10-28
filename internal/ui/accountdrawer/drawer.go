// Package accountdrawer implements the account-switcher column for pmx.
//
// Visually it mirrors the folder drawer (same width formula, same filter UX,
// same selection-bar prefix) but trades the single-line folder row for a two-
// line account row: an icon + display name + unread badge on the first line
// and the account's email address on the second. The two lines read as a
// single stacked block when the cursor is on the row (both lines share the
// raised selection background).
//
// The drawer is modal in the same sense as folderdrawer — it slides over the
// content area, owns its own cursor and optional filter, and dispatches a
// user-supplied callback as a tea.Cmd when the user confirms a row.
package accountdrawer

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/ui/styles"
)

// AccountStat holds the display data for one row in the account drawer.
type AccountStat struct {
	Name   string
	Email  string
	Unread int
}

// Model is the account drawer's state. Zero value is a closed drawer with no
// accounts; use New() to construct a usable instance.
type Model struct {
	accounts   []AccountStat
	visible    []AccountStat
	cursor     int
	width      int
	height     int
	open       bool
	filter     textinput.Model
	filterOn   bool
	onSelect   func(name string) tea.Msg
	activeName string
}

// New constructs an account drawer. onSelect is invoked when the user confirms
// an account with <enter>; the returned tea.Msg is forwarded as a tea.Cmd by
// the drawer's Update. onSelect is skipped (soft dismiss) when the cursor is
// already on the active account.
func New(onSelect func(name string) tea.Msg) *Model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Width = 20
	ti.CharLimit = 50
	ti.TextStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFg).
		Background(styles.ColorSurface)
	ti.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFaint).
		Background(styles.ColorSurface)
	ti.PromptStyle = lipgloss.NewStyle().
		Foreground(styles.ColorPrimaryHi).
		Background(styles.ColorSurface).
		Bold(true)
	ti.Cursor.Style = lipgloss.NewStyle().
		Foreground(styles.ColorAccent).
		Background(styles.ColorSurface)
	return &Model{
		filter:   ti,
		onSelect: onSelect,
	}
}

// IsOpen reports whether the drawer is currently visible.
func (m *Model) IsOpen() bool { return m.open }

// Open shows the drawer and positions the cursor on the row matching
// activeAccountName, falling back to row 0 when no match is found. The passed
// name is also remembered so the "active" indicator (filled dot + accented
// name) continues to point at that account even if the cursor moves.
func (m *Model) Open(activeAccountName string) {
	m.open = true
	m.activeName = activeAccountName
	m.filterOn = false
	m.filter.SetValue("")
	m.filter.Blur()
	m.recomputeVisible()
	m.positionCursor()
}

// Close hides the drawer and clears any active filter.
func (m *Model) Close() {
	m.open = false
	m.filterOn = false
	m.filter.SetValue("")
	m.filter.Blur()
	m.recomputeVisible()
}

// SetAccounts replaces the full account list and re-applies the current
// filter. When the drawer is open the cursor is repositioned to the active
// account; otherwise the cursor is clamped to the new visible range.
func (m *Model) SetAccounts(accounts []AccountStat) {
	m.accounts = accounts
	m.recomputeVisible()
	if m.open {
		m.positionCursor()
	} else {
		m.clampCursor()
	}
}

// SetSize records the available content size. The drawer's own width is
// derived from totalWidth (same formula as folderdrawer); height is kept as
// given so the column always fills the content area exactly.
func (m *Model) SetSize(totalWidth, contentHeight int) {
	m.height = contentHeight
	w := totalWidth / 3
	if w < 20 {
		w = 20
	}
	if w > 28 {
		w = 28
	}
	m.width = w
	// Reserve 4 cells for the "[selBar][space][icon][space]" prefix.
	inputW := w - 4
	if inputW < 6 {
		inputW = 6
	}
	m.filter.Width = inputW
}

// Width returns the number of columns the drawer currently occupies, or 0
// when closed.
func (m *Model) Width() int {
	if !m.open {
		return 0
	}
	return m.width
}

// IsCapturingInput reports whether free-text keystrokes should be routed to
// the drawer's filter instead of the host view's leader hotkey.
func (m *Model) IsCapturingInput() bool { return m.open && m.filterOn }

// Update processes a key event and returns an optional command (the account-
// select callback is dispatched this way). The drawer only responds while
// open; the host is responsible for routing keys only when IsOpen().
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	k := key.String()

	// Filter-active input handling takes precedence over navigation so the
	// user can type freely without hijacked letters.
	if m.filterOn {
		switch k {
		case "esc":
			m.filterOn = false
			m.filter.SetValue("")
			m.filter.Blur()
			m.recomputeVisible()
			m.clampCursor()
			return nil
		case "enter":
			if len(m.visible) == 0 {
				return nil
			}
			sel := m.visible[m.cursor]
			cb := m.onSelect
			active := m.activeName
			m.Close()
			if cb != nil && sel.Name != active {
				name := sel.Name
				return func() tea.Msg { return cb(name) }
			}
			return nil
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return nil
		case "down":
			if m.cursor < len(m.visible)-1 {
				m.cursor++
			}
			return nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.recomputeVisible()
		m.cursor = 0
		return cmd
	}

	switch k {
	case "j", "down":
		if m.cursor < len(m.visible)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		if n := len(m.visible); n > 0 {
			m.cursor = n - 1
		}
	case "enter":
		if len(m.visible) == 0 {
			return nil
		}
		sel := m.visible[m.cursor]
		cb := m.onSelect
		active := m.activeName
		m.Close()
		if cb != nil && sel.Name != active {
			name := sel.Name
			return func() tea.Msg { return cb(name) }
		}
	case "esc":
		m.Close()
	case "/":
		m.filterOn = true
		m.filter.Focus()
	}
	return nil
}

// StatusKeys returns a status-bar hint string for the drawer's current mode.
// The leading chip identifies the drawer so it's clear which overlay owns the
// keybindings currently in effect.
func (m *Model) StatusKeys() string {
	bar := styles.StatusBar
	sep := bar.Render("  ") + styles.StatusSep.Render("·") + bar.Render("  ")

	hint := func(k, label string) string {
		return styles.StatusKey.Render(k) +
			bar.Render(" ") +
			styles.StatusKeyLabel.Render(label)
	}

	chip := styles.StatusKey.Render(" ACCOUNTS ")

	pairs := []string{
		hint("j/k", "navigate"),
		hint("↵", "switch"),
		hint("/", "filter"),
		hint("esc", "close"),
	}
	return bar.Render("  ") + chip + sep + strings.Join(pairs, sep)
}

// View renders the drawer as a single column of exactly Width() x height
// characters. When closed it returns the empty string. Each account occupies
// two rows (name/icon/badge + email), so the row budget is halved relative to
// folderdrawer.
func (m *Model) View() string {
	if !m.open || m.width <= 0 || m.height <= 0 {
		return ""
	}

	surface := lipgloss.NewStyle().Background(styles.ColorSurface)
	rowSelBg := lipgloss.NewStyle().Background(styles.ColorRaised)

	// Row layout:
	//   row 0: title
	//   row 1: top rule
	//   rows 2..: account rows (2 lines per account)
	//   trailing: separator + filter input when filter is active
	//   remaining: filler
	title := m.renderTitle(surface)
	topRule := m.renderRule(styles.ColorBorder)

	rowsForFilter := 0
	if m.filterOn {
		rowsForFilter = 2 // separator + input
	}

	// Each account row is 2 terminal lines tall.
	accountCap := (m.height - 2 - rowsForFilter) / 2
	if accountCap < 0 {
		accountCap = 0
	}

	// Clamp cursor into visible range before rendering.
	m.clampCursor()

	// Scroll window: keep cursor in view when visible exceeds accountCap.
	start := 0
	if len(m.visible) > accountCap && accountCap > 0 {
		if m.cursor >= accountCap {
			start = m.cursor - accountCap + 1
		}
		if start+accountCap > len(m.visible) {
			start = len(m.visible) - accountCap
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + accountCap
	if end > len(m.visible) {
		end = len(m.visible)
	}

	rows := make([]string, 0, m.height)
	rows = append(rows, title, topRule)

	linesUsed := 0
	for i := start; i < end; i++ {
		a := m.visible[i]
		line1, line2 := m.renderRow(a, i == m.cursor, surface, rowSelBg)
		rows = append(rows, line1, line2)
		linesUsed += 2
	}
	// Pad the account region with empty surface rows so the column keeps its
	// height even when the list is short.
	cap2 := accountCap * 2
	for linesUsed < cap2 {
		rows = append(rows, surface.Render(strings.Repeat(" ", m.width)))
		linesUsed++
	}

	if m.filterOn {
		rows = append(rows, m.renderRule(styles.ColorBorder))
		rows = append(rows, m.renderFilterRow(surface))
	}

	// Final safety pad — guarantee exact height.
	for len(rows) < m.height {
		rows = append(rows, surface.Render(strings.Repeat(" ", m.width)))
	}
	if len(rows) > m.height {
		rows = rows[:m.height]
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderTitle paints the "Accounts" header. The glyph on the left mirrors the
// list icon used in the main nav; the count on the right matches the folder
// drawer so the two overlays read as siblings.
func (m *Model) renderTitle(surface lipgloss.Style) string {
	label := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorPrimaryHi).
		Bold(true).
		Render(" " + styles.AccountIconList + "  Accounts")
	count := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorDimFg).
		Render(" " + strconv.Itoa(len(m.visible)))
	left := label + count
	pad := m.width - lipgloss.Width(left)
	if pad < 0 {
		pad = 0
	}
	return left + surface.Render(strings.Repeat(" ", pad))
}

// renderRule paints a one-row divider inside the drawer column.
func (m *Model) renderRule(fg lipgloss.Color) string {
	s := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(fg)
	return s.Render(strings.Repeat("─", m.width))
}

// renderRow draws a single account as two terminal lines. The first line
// carries the selection bar + icon + name + badge; the second line is a
// 4-cell indent followed by the email address. When the cursor is on the row
// both lines share the raised background so the pair reads as a block.
func (m *Model) renderRow(a AccountStat, selected bool, surface, rowSelBg lipgloss.Style) (string, string) {
	rowBg := surface
	if selected {
		rowBg = rowSelBg
	}
	active := a.Name == m.activeName

	// --- Line A: selection bar + icon + name + badge --------------------

	var selBar string
	if selected {
		selBar = styles.SelectionBar.Render("▌") + rowBg.Render(" ")
	} else {
		selBar = rowBg.Render("  ")
	}

	// Icon: filled dot for the active account, user glyph otherwise.
	var iconCell string
	if active {
		iconCell = rowBg.Inherit(styles.AccountActive).Render("●") + rowBg.Render(" ")
	} else {
		iconCell = rowBg.Inherit(styles.AccountInactive).Render(styles.AccountIconUser) + rowBg.Render(" ")
	}

	// Badge takes 4 cells on the right edge; name claims the remaining span.
	badge := renderBadge(a.Unread, selected, rowBg)
	badgeW := lipgloss.Width(badge)

	nameW := m.width - 4 - badgeW
	if nameW < 1 {
		nameW = 1
	}
	name := truncate(a.Name, nameW)
	var nameStyle lipgloss.Style
	switch {
	case selected:
		nameStyle = rowBg.Inherit(styles.AccountNameSelected)
	case active:
		nameStyle = rowBg.Inherit(styles.AccountNameActive)
	default:
		nameStyle = rowBg.Inherit(styles.AccountName)
	}
	nameCell := nameStyle.Render(name)

	line1 := selBar + iconCell + nameCell
	pad := m.width - lipgloss.Width(line1) - badgeW
	if pad < 0 {
		pad = 0
	}
	line1 += rowBg.Render(strings.Repeat(" ", pad)) + badge

	// --- Line B: 4-space indent + email --------------------------------

	// First two cells of the gutter use the row background so the stacked
	// selection block reads as one solid shape; the next two cells mirror
	// the icon column (also row background, since no glyph here).
	indent := rowBg.Render("    ")

	emailW := m.width - 4
	if emailW < 1 {
		emailW = 1
	}
	email := truncate(a.Email, emailW)
	var emailStyle lipgloss.Style
	if selected {
		emailStyle = rowBg.Inherit(styles.AccountEmailSelected)
	} else {
		emailStyle = rowBg.Inherit(styles.AccountEmail)
	}
	emailCell := emailStyle.Render(email)

	line2 := indent + emailCell
	pad2 := m.width - lipgloss.Width(line2)
	if pad2 < 0 {
		pad2 = 0
	}
	line2 += rowBg.Render(strings.Repeat(" ", pad2))

	return line1, line2
}

// renderBadge returns the right-aligned unread indicator, always exactly 4
// cells wide. 0 → faint dot, 1–9 → bare numeral, 10–99 → padded pill, ≥100 →
// "99+" pill. Pill colours invert on the cursor row so the badge contrasts
// against the raised background.
func renderBadge(n int, selected bool, rowBg lipgloss.Style) string {
	switch {
	case n <= 0:
		// "   ·" — right-aligned faint dot in a 4-cell field.
		return rowBg.Render("   ") + rowBg.Inherit(styles.AccountBadgeFaint).Render("·")
	case n < 10:
		// "   N" — right-aligned bare numeral in a 4-cell field.
		return rowBg.Render("   ") + rowBg.Inherit(styles.AccountBadge).Render(strconv.Itoa(n))
	case n < 100:
		// " NN " — padded pill, 4 cells wide. Padding(0,1) + 2-digit label.
		label := strconv.Itoa(n)
		if len(label) < 2 {
			label = " " + label
		}
		if selected {
			return styles.AccountBadgePillSelected.Render(label)
		}
		return styles.AccountBadgePill.Render(label)
	default:
		// " 99+" cap — same 4-cell pill but with the overflow glyph.
		if selected {
			return styles.AccountBadgePillSelected.Render("99+")
		}
		return styles.AccountBadgePill.Render("99+")
	}
}

// renderFilterRow draws the filter input in the drawer's footer area.
func (m *Model) renderFilterRow(surface lipgloss.Style) string {
	// Reuse row prefix shape (2 cells of surface, "/" acts as its own icon
	// via the input's Prompt).
	left := surface.Render("  ") + m.filter.View()
	pad := m.width - lipgloss.Width(left)
	if pad < 0 {
		pad = 0
	}
	return left + surface.Render(strings.Repeat(" ", pad))
}

// recomputeVisible applies the current filter query against m.accounts, by
// case-insensitive substring on both Name and Email. An empty filter copies
// the full list.
func (m *Model) recomputeVisible() {
	q := strings.TrimSpace(strings.ToLower(m.filter.Value()))
	if q == "" || !m.filterOn {
		m.visible = append(m.visible[:0], m.accounts...)
		return
	}
	m.visible = m.visible[:0]
	for _, a := range m.accounts {
		if strings.Contains(strings.ToLower(a.Name), q) ||
			strings.Contains(strings.ToLower(a.Email), q) {
			m.visible = append(m.visible, a)
		}
	}
}

// positionCursor moves the cursor to the account matching m.activeName.
// Resets to 0 when no name is set or no match is found.
func (m *Model) positionCursor() {
	if m.activeName != "" {
		for i, a := range m.visible {
			if a.Name == m.activeName {
				m.cursor = i
				return
			}
		}
	}
	m.cursor = 0
}

// clampCursor ensures the cursor index is within [0, len(visible)-1].
func (m *Model) clampCursor() {
	n := len(m.visible)
	if n == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
}

// truncate shortens s to at most max runes, appending an ellipsis when the
// cut happens inside the string.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
