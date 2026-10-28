// Package folderdrawer implements the left-side folder navigator for pmx.
// It is a self-contained, modal column that slides over the content area
// (via horizontal composition in the host app). The drawer owns its own
// cursor, optional filter, and selection callback so it can be reused as a
// move-to-folder picker.
package folderdrawer

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

// Model is the folder drawer's state. Zero value is a closed drawer with no
// folders. Use New() to construct one.
type Model struct {
	folders         []*mail.Folder
	visible         []*mail.Folder
	cursor          int
	width           int
	height          int
	open            bool
	filter          textinput.Model
	filterOn        bool
	onSelect        func(*mail.Folder) tea.Msg
	currentFolderID int64
}

// New constructs a folder drawer. onSelect is invoked when the user confirms
// a folder with <enter>; the returned tea.Msg is forwarded as a tea.Cmd by
// the drawer's Update.
func New(onSelect func(*mail.Folder) tea.Msg) *Model {
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

// Open shows the drawer with the cursor on the folder matching currentFolderID,
// or at the top if no match is found.
func (m *Model) Open(currentFolderID int64) {
	m.open = true
	m.currentFolderID = currentFolderID
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

// SetFolders replaces the full folder list and re-applies the current filter.
// When the drawer is open the cursor is repositioned to the current folder;
// otherwise the cursor is clamped to the new visible range.
func (m *Model) SetFolders(folders []*mail.Folder) {
	m.folders = folders
	m.recomputeVisible()
	if m.open {
		m.positionCursor()
	} else {
		m.clampCursor()
	}
}

// SetOnSelect reprograms the callback invoked on <enter>. Useful when the
// same drawer instance is repurposed (e.g. as a move-to-folder picker).
func (m *Model) SetOnSelect(cb func(*mail.Folder) tea.Msg) {
	m.onSelect = cb
}

// SetSize records the available content size. The drawer's own width is
// derived from totalWidth; height is kept as given so the column always fills
// the content area exactly.
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

// Update processes a key event and returns an optional command (the folder-
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
			m.Close()
			if cb != nil {
				return func() tea.Msg { return cb(sel) }
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
		m.Close()
		if cb != nil {
			return func() tea.Msg { return cb(sel) }
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
func (m *Model) StatusKeys() string {
	bar := styles.StatusBar
	sep := bar.Render("  ") + styles.StatusSep.Render("·") + bar.Render("  ")

	hint := func(k, label string) string {
		return styles.StatusKey.Render(k) +
			bar.Render(" ") +
			styles.StatusKeyLabel.Render(label)
	}

	pairs := []string{
		hint("j/k", "navigate"),
		hint("↵", "select"),
		hint("/", "filter"),
		hint("esc", "close"),
	}
	return bar.Render("  ") + strings.Join(pairs, sep)
}

// View renders the drawer as a single column of exactly Width() x height
// characters. When closed it returns the empty string.
func (m *Model) View() string {
	if !m.open || m.width <= 0 || m.height <= 0 {
		return ""
	}

	surface := lipgloss.NewStyle().Background(styles.ColorSurface)
	rowSelBg := lipgloss.NewStyle().Background(styles.ColorRaised)

	// Rows allocated:
	//   row 0: title
	//   row 1: top rule
	//   rows 2..k: folder rows (k = folderRows)
	//   row k+1: separator (if filter visible)
	//   row k+2: filter input (if filter visible)
	//   remaining: filler
	title := m.renderTitle(surface)
	topRule := m.renderRule(styles.ColorBorder)

	rowsForFilter := 0
	if m.filterOn {
		rowsForFilter = 2 // separator + input
	}

	folderCap := m.height - 2 - rowsForFilter // minus title + top rule
	if folderCap < 0 {
		folderCap = 0
	}

	// Clamp cursor into visible range before rendering.
	m.clampCursor()

	// Scroll window: keep cursor in view when visible exceeds folderCap.
	start := 0
	if len(m.visible) > folderCap && folderCap > 0 {
		if m.cursor >= folderCap {
			start = m.cursor - folderCap + 1
		}
		if start+folderCap > len(m.visible) {
			start = len(m.visible) - folderCap
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + folderCap
	if end > len(m.visible) {
		end = len(m.visible)
	}

	rows := make([]string, 0, m.height)
	rows = append(rows, title, topRule)

	for i := start; i < end; i++ {
		f := m.visible[i]
		rows = append(rows, m.renderRow(f, i == m.cursor, surface, rowSelBg))
	}
	// Pad the folder region with empty surface rows so the column keeps
	// its height even when the list is short.
	for i := end - start; i < folderCap; i++ {
		rows = append(rows, surface.Render(strings.Repeat(" ", m.width)))
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

// renderTitle paints the "Folders" header. The title uses the ListFolder
// style so it matches the list header on the right pane.
func (m *Model) renderTitle(surface lipgloss.Style) string {
	label := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorPrimaryHi).
		Bold(true).
		Render(" " + styles.FolderIconDefault + "  Folders")
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

// renderRow draws a single folder row with icon + name. Selected rows paint
// a raised background and leading selection bar; idle rows paint the surface
// background so the drawer reads as one continuous column.
func (m *Model) renderRow(f *mail.Folder, selected bool, surface, rowSelBg lipgloss.Style) string {
	rowBg := surface
	if selected {
		rowBg = rowSelBg
	}

	// Selection bar (2 cells) + icon (1 cell) + space (1 cell).
	var selBar string
	if selected {
		selBar = styles.SelectionBar.Render("▌") + rowBg.Render(" ")
	} else {
		selBar = rowBg.Render("  ")
	}

	icon := styles.FolderIconDefault
	if ic, ok := styles.FolderIcons[f.Role]; ok {
		icon = ic
	}

	// Icon style — accent for inbox so it reads as "home", subdued for others
	// unless this row is selected.
	var iconStyle lipgloss.Style
	switch f.Role {
	case mail.RoleInbox:
		iconStyle = rowBg.Foreground(styles.ColorAccent).Bold(true)
	case mail.RoleTrash:
		iconStyle = rowBg.Foreground(styles.ColorDanger).Bold(true)
	case mail.RoleJunk:
		iconStyle = rowBg.Foreground(styles.ColorWarn).Bold(true)
	case mail.RoleSent, mail.RoleDrafts, mail.RoleArchive:
		iconStyle = rowBg.Foreground(styles.ColorPrimaryHi).Bold(true)
	default:
		iconStyle = rowBg.Foreground(styles.ColorDimFg)
	}
	iconCell := iconStyle.Render(icon) + rowBg.Render(" ")

	// Name — bold+bright when selected, muted otherwise.
	nameW := m.width - 4
	if nameW < 1 {
		nameW = 1
	}
	name := truncate(f.Name, nameW)
	nameStyle := rowBg.Foreground(styles.ColorSubdued)
	if selected {
		nameStyle = rowBg.Foreground(styles.ColorFgStrong).Bold(true)
	} else if f.Role == mail.RoleInbox {
		nameStyle = rowBg.Foreground(styles.ColorFg).Bold(true)
	}
	nameCell := nameStyle.Render(name)

	row := selBar + iconCell + nameCell
	pad := m.width - lipgloss.Width(row)
	if pad < 0 {
		pad = 0
	}
	return row + rowBg.Render(strings.Repeat(" ", pad))
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

// recomputeVisible applies the current filter query against m.folders. An
// empty filter copies the full list.
func (m *Model) recomputeVisible() {
	q := strings.TrimSpace(strings.ToLower(m.filter.Value()))
	if q == "" || !m.filterOn {
		m.visible = append(m.visible[:0], m.folders...)
		return
	}
	m.visible = m.visible[:0]
	for _, f := range m.folders {
		if f == nil {
			continue
		}
		if strings.Contains(strings.ToLower(f.Name), q) {
			m.visible = append(m.visible, f)
		}
	}
}

// positionCursor moves the cursor to the folder matching m.currentFolderID.
// Resets to 0 when no ID is set or no match is found.
func (m *Model) positionCursor() {
	if m.currentFolderID != 0 {
		for i, f := range m.visible {
			if f.ID == m.currentFolderID {
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
