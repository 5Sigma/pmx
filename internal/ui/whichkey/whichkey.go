// Package whichkey implements the leader-key popup used across pmx views.
// It is stateful (open/closed, current path), driven by a view's KeyMap, and
// renders a floating panel at the bottom of the screen.
//
// Lifecycle:
//
//  1. Caller sets a KeyMap via SetKeyMap, then calls Open() when <space>
//     is pressed in the host view.
//  2. On every key event while open, caller passes the key to Handle(). The
//     returned Status tells the caller whether to keep the popup open,
//     fire an Action (and close), or just close.
//  3. Caller renders the popup via View(width) and reserves Height() rows
//     at the bottom of the screen.
package whichkey

import (
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/ui/keymap"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

// Status is the outcome of Handle().
type Status int

const (
	// StatusIdle — the popup is closed and did nothing.
	StatusIdle Status = iota
	// StatusPending — the popup is still open, awaiting more keys.
	StatusPending
	// StatusAccepted — a leaf binding fired. Read .Result for the action.
	StatusAccepted
	// StatusCancelled — the popup closed via esc or an unknown key.
	StatusCancelled
)

// HandleResult carries the resolved action when Status == StatusAccepted.
type HandleResult struct {
	Status Status
	Result keymap.Result
}

// Model is the whichkey state. Zero value is a closed popup with no keymap.
type Model struct {
	km   keymap.KeyMap
	open bool
	// path accumulates the user's leader-key sequence (space is implicit —
	// not stored). For example after <spc> m, path == []string{"m"}.
	path []string
}

// New constructs an empty whichkey model.
func New() *Model { return &Model{} }

// SetKeyMap replaces the keymap and resets the path. Call this whenever the
// active view changes.
func (m *Model) SetKeyMap(km keymap.KeyMap) {
	m.km = km
	m.path = m.path[:0]
}

// IsOpen reports whether the popup is currently displayed.
func (m *Model) IsOpen() bool { return m.open }

// Path returns the current prefix path (not including <space>).
func (m *Model) Path() []string {
	// Return a copy to prevent external mutation.
	cp := make([]string, len(m.path))
	copy(cp, m.path)
	return cp
}

// Open shows the popup at the root (just after <space>). If the active
// keymap has no Leader entries, Open is a no-op.
func (m *Model) Open() {
	if len(m.km.Leader) == 0 {
		return
	}
	m.open = true
	m.path = m.path[:0]
}

// Close hides the popup and clears the path.
func (m *Model) Close() {
	m.open = false
	m.path = m.path[:0]
}

// Handle processes a key event. Only call this while IsOpen() is true.
// The caller should pass the raw key string as returned by tea.KeyMsg.String().
func (m *Model) Handle(key string) HandleResult {
	if !m.open {
		return HandleResult{Status: StatusIdle}
	}

	// Escape or ctrl-c cancels.
	if key == "esc" || key == "ctrl+c" {
		m.Close()
		return HandleResult{Status: StatusCancelled}
	}

	// Space at any depth is a no-op (keeps popup open).
	if key == " " || key == "space" {
		return HandleResult{Status: StatusPending}
	}

	// Backspace walks up one level.
	if key == "backspace" {
		if len(m.path) == 0 {
			m.Close()
			return HandleResult{Status: StatusCancelled}
		}
		m.path = m.path[:len(m.path)-1]
		return HandleResult{Status: StatusPending}
	}

	// Try to match the key at the current level.
	candidate := append(m.path, key)
	entry, ok := m.km.Lookup(candidate)
	if !ok {
		// Unknown key → cancel the sequence.
		m.Close()
		return HandleResult{Status: StatusCancelled}
	}
	if entry.IsGroup() {
		m.path = candidate
		return HandleResult{Status: StatusPending}
	}
	// Leaf — fire the action.
	var res keymap.Result
	if entry.Action != nil {
		res = entry.Action()
	}
	m.Close()
	return HandleResult{Status: StatusAccepted, Result: res}
}

// visibleEntries returns the children at the current depth, sorted for
// stable rendering (groups first, then alphabetically by key).
func (m *Model) visibleEntries() []keymap.Entry {
	children, _, ok := m.km.FindLeader(m.path)
	if !ok {
		return nil
	}
	cp := make([]keymap.Entry, len(children))
	copy(cp, children)
	sort.SliceStable(cp, func(i, j int) bool {
		gi, gj := cp[i].IsGroup(), cp[j].IsGroup()
		if gi != gj {
			return gi
		}
		return cp[i].Key < cp[j].Key
	})
	return cp
}

// Height returns the number of rows the popup will occupy when rendered at
// the given width. Returns 0 when closed.
//
// Layout:
//
//	row 1 — top rule
//	row 2 — title (prefix path + hint)
//	row 3 — mid rule
//	rows 4..N-1 — grid of chips
//	row N — bottom rule / status
func (m *Model) Height(width int) int {
	if !m.open || width <= 0 {
		return 0
	}
	entries := m.visibleEntries()
	if len(entries) == 0 {
		return 0
	}
	rows := gridRows(entries, width)
	// top rule + title + mid rule + rows + bottom rule
	return 1 + 1 + 1 + rows + 1
}

// View renders the popup. Returns the empty string when closed.
func (m *Model) View(width int) string {
	if !m.open || width <= 0 {
		return ""
	}
	entries := m.visibleEntries()
	if len(entries) == 0 {
		return ""
	}

	// ── Decorative rules (top / mid / bottom) ───────────────────────────
	topRule := styles.WhichKeyRuleTop.Render(strings.Repeat("─", width))
	midRule := styles.WhichKeyRuleMid.Render(strings.Repeat("┄", width))
	botRule := styles.WhichKeyRuleBot.Render(strings.Repeat("─", width))

	// ── Title row ───────────────────────────────────────────────────────
	title := m.renderTitle(width, len(entries))

	// ── Chip grid ───────────────────────────────────────────────────────
	grid := m.renderGrid(entries, width)

	parts := []string{topRule, title, midRule}
	parts = append(parts, grid...)
	parts = append(parts, botRule)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) renderTitle(width, count int) string {
	bg := styles.WhichKeyPanel

	// Prefix path: show "  <spc>" at the root, or " m › b › " deeper in.
	var prefix string
	if len(m.path) == 0 {
		prefix = styles.WhichKeyLeader.Render("  LEADER ")
	} else {
		var sb strings.Builder
		sb.WriteString(styles.WhichKeyLeader.Render("  LEADER "))
		sb.WriteString(bg.Render(" "))
		for i, p := range m.path {
			sb.WriteString(styles.WhichKeyPathKey.Render(" " + displayKey(p) + " "))
			if i < len(m.path)-1 {
				sb.WriteString(styles.WhichKeyPathSep.Render(" › "))
			}
		}
		sb.WriteString(styles.WhichKeyPathSep.Render(" › "))
		prefix = sb.String()
	}

	// Optional group label — if path's last entry has a Desc, show it.
	label := ""
	if len(m.path) > 0 {
		if entry, ok := m.km.Lookup(m.path); ok && entry.Desc != "" {
			label = bg.Render(" ") + styles.WhichKeyGroupLabel.Render(entry.Desc)
		}
	}

	// Right side: count of visible entries + cancel hint.
	countStr := styles.WhichKeyCount.Render(strconv.Itoa(count)+" keys") + bg.Render("  ")
	cancel := styles.WhichKeyCancelKey.Render(" esc ") +
		bg.Render(" ") +
		styles.WhichKeyCancelLabel.Render("cancel") + bg.Render(" ")

	left := prefix + label
	right := countStr + cancel
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		// Drop count on narrow terminals.
		right = cancel
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 0 {
		gap = 0
	}
	return left + bg.Render(strings.Repeat(" ", gap)) + right
}

func (m *Model) renderGrid(entries []keymap.Entry, width int) []string {
	cells := make([]string, len(entries))
	cellW := 0
	for i, e := range entries {
		cells[i] = renderCell(e)
		if w := lipgloss.Width(cells[i]); w > cellW {
			cellW = w
		}
	}
	// Pad every cell to the widest cell for a crisp grid.
	for i, c := range cells {
		if pad := cellW - lipgloss.Width(c); pad > 0 {
			cells[i] = c + styles.WhichKeyPanel.Render(strings.Repeat(" ", pad))
		}
	}

	cols := columnsFor(cellW, width)
	if cols < 1 {
		cols = 1
	}

	var rows []string
	leftMargin := styles.WhichKeyPanel.Render("  ")
	gutter := styles.WhichKeyPanel.Render("  ")
	for i := 0; i < len(cells); i += cols {
		end := i + cols
		if end > len(cells) {
			end = len(cells)
		}
		row := strings.Join(cells[i:end], gutter)
		row = leftMargin + row
		if pad := width - lipgloss.Width(row); pad > 0 {
			row += styles.WhichKeyPanel.Render(strings.Repeat(" ", pad))
		}
		rows = append(rows, row)
	}
	return rows
}

// renderCell formats a single `[key] description` chip.
func renderCell(e keymap.Entry) string {
	bg := styles.WhichKeyPanel

	keyStyle := styles.WhichKeyChipKey
	descStyle := styles.WhichKeyChipDesc
	arrow := ""
	switch {
	case e.Danger:
		keyStyle = styles.WhichKeyChipKeyDanger
		descStyle = styles.WhichKeyChipDescDanger
	case e.Accent:
		keyStyle = styles.WhichKeyChipKeyAccent
		descStyle = styles.WhichKeyChipDescAccent
	case e.IsGroup():
		keyStyle = styles.WhichKeyChipKeyGroup
		descStyle = styles.WhichKeyChipDescGroup
		arrow = styles.WhichKeyChipArrow.Render(" ›")
	}

	key := keyStyle.Render(" " + e.Label() + " ")
	desc := descStyle.Render(e.Desc)
	return key + bg.Render(" ") + desc + arrow
}

// columnsFor computes how many cells (of width cellW) fit per row given a
// 2-col left margin and a 2-col gutter between cells.
func columnsFor(cellW, totalW int) int {
	usable := totalW - 2 // left margin
	if usable < cellW {
		return 1
	}
	// cols*cellW + (cols-1)*2 <= usable
	//   cols <= (usable + 2) / (cellW + 2)
	cols := (usable + 2) / (cellW + 2)
	if cols < 1 {
		cols = 1
	}
	return cols
}

// gridRows is the row count that renderGrid would produce. Kept as a
// standalone helper so Height() can call it without allocating cell strings.
func gridRows(entries []keymap.Entry, width int) int {
	if len(entries) == 0 {
		return 0
	}
	cellW := 0
	for _, e := range entries {
		w := cellWidth(e)
		if w > cellW {
			cellW = w
		}
	}
	cols := columnsFor(cellW, width)
	if cols < 1 {
		cols = 1
	}
	rows := (len(entries) + cols - 1) / cols
	return rows
}

func cellWidth(e keymap.Entry) int {
	// mirror renderCell: " key " + " " + desc + optional " ›"
	w := lipgloss.Width(e.Label()) + 2 + 1 + lipgloss.Width(e.Desc)
	if e.IsGroup() {
		w += 2
	}
	return w
}

// displayKey formats a raw key string for the title path (e.g. "space" → "␣").
func displayKey(k string) string {
	switch k {
	case " ":
		return "␣"
	case "enter":
		return "↵"
	case "esc":
		return "esc"
	case "tab":
		return "⇥"
	default:
		return k
	}
}
