// Package pathpicker implements a transient bottom-panel filesystem path
// picker for pmx. It follows the folderdrawer API shape and uses the same
// styles palette so it tiles cleanly with the rest of the chrome.
package pathpicker

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/ui/styles"
)

// Mode controls which filesystem entries are selectable.
type Mode int

const (
	// ModeDirs makes only directories selectable; files are shown greyed-out
	// but cannot be navigated to or accepted.
	ModeDirs Mode = iota
	// ModeFilesAndDirs makes both files and directories selectable.
	ModeFilesAndDirs
)

// OpenConfig carries the configuration for a single picker session.
type OpenConfig struct {
	Prompt   string
	Mode     Mode
	Start    string                       // initial path; "" → user home dir
	OnSelect func(absPath string) tea.Msg // invoked with the resolved path on accept
}

// Model is the pathpicker state. Construct with New(); mutate via pointer
// receiver methods.
type Model struct {
	cfg       OpenConfig
	open      bool
	width     int
	maxHeight int

	input      textinput.Model
	listedDir  string    // directory currently reflected in entries
	entries    []fsEntry // full listing of listedDir
	filtered   []int     // indices into entries; selectable entries
	unfiltered []int     // file-only indices shown greyed in ModeDirs
	stub       string    // trailing partial segment typed so far
	cursor     int       // index into filtered
	scroll     int       // first visible row
	errMsg     string
}

// New constructs a Model with a styled textinput ready for use.
func New() *Model {
	ti := textinput.New()
	ti.CharLimit = 512
	ti.TextStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFg).
		Background(styles.ColorOverlay)
	ti.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFaint).
		Background(styles.ColorOverlay)
	ti.PromptStyle = lipgloss.NewStyle().
		Foreground(styles.ColorPrimaryHi).
		Background(styles.ColorOverlay).
		Bold(true)
	ti.Cursor.Style = lipgloss.NewStyle().
		Foreground(styles.ColorAccent).
		Background(styles.ColorOverlay)
	return &Model{input: ti}
}

// IsOpen reports whether the picker is currently visible.
func (m *Model) IsOpen() bool { return m.open }

// IsCapturingInput reports whether keystrokes should be routed to the picker.
func (m *Model) IsCapturingInput() bool { return m.open }

// Open shows the picker with the given configuration.
func (m *Model) Open(cfg OpenConfig) {
	m.cfg = cfg
	m.open = true
	m.errMsg = ""

	start := cfg.Start
	if start == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			start = home
		} else {
			start = "/"
		}
	}
	start = ExpandHome(start)
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}

	m.input.Prompt = cfg.Prompt + " "
	// Set the input width accounting for the prompt prefix (2 indent + prompt + space).
	promptW := lipgloss.Width(m.input.Prompt)
	inputW := m.width - 2 - promptW
	if inputW < 6 {
		inputW = 6
	}
	m.input.Width = inputW

	m.input.SetValue(start)
	m.input.CursorEnd()
	m.input.Focus()
	m.syncFromInput()
}

// Close hides the picker and resets transient state.
func (m *Model) Close() {
	m.open = false
	m.entries = nil
	m.filtered = nil
	m.unfiltered = nil
	m.listedDir = ""
	m.stub = ""
	m.cursor = 0
	m.scroll = 0
	m.errMsg = ""
	m.input.SetValue("")
	m.input.Blur()
}

// SetSize records the available panel dimensions. Call this whenever the
// terminal is resized; it must be called before Open.
func (m *Model) SetSize(width, maxHeight int) {
	m.width = width
	m.maxHeight = maxHeight
	// Keep the input width in sync if already open.
	if m.open {
		promptW := lipgloss.Width(m.input.Prompt)
		inputW := width - 2 - promptW
		if inputW < 6 {
			inputW = 6
		}
		m.input.Width = inputW
	}
}

const maxListRows = 8

// listCap returns the number of completion rows that will be rendered.
func (m *Model) listCap() int {
	if !m.open {
		return 0
	}
	total := len(m.filtered)
	if m.cfg.Mode == ModeDirs {
		total += len(m.unfiltered)
	}
	if total > maxListRows {
		total = maxListRows
	}
	avail := m.maxHeight - 3 // top rule + input + bottom rule
	if avail < 0 {
		avail = 0
	}
	if total > avail {
		total = avail
	}
	return total
}

// Height returns the number of terminal rows the picker currently occupies.
// Returns 0 when closed. The host must subtract this from its content height.
func (m *Model) Height(width int) int {
	if !m.open {
		return 0
	}
	cap := m.listCap()
	if cap == 0 {
		return 3 // top rule + input + bottom rule
	}
	return cap + 4 // + mid rule between input and completions
}

// adjustScroll keeps the cursor visible within the current list cap.
func (m *Model) adjustScroll(cap int) {
	if cap <= 0 {
		m.scroll = 0
		return
	}
	if m.cursor >= m.scroll+cap {
		m.scroll = m.cursor - cap + 1
	}
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
}

// syncFromInput re-reads the directory implied by the current input value and
// rebuilds filtered/unfiltered.
func (m *Model) syncFromInput() {
	raw := m.input.Value()
	raw = ExpandHome(raw)
	if raw == "" {
		if home, err := os.UserHomeDir(); err == nil {
			raw = home
		} else {
			raw = "/"
		}
	}

	prevStub := m.stub
	dir, stub := resolveDirAndStub(raw)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = filepath.Clean(abs)
	} else {
		dir = filepath.Clean(dir)
	}
	m.stub = stub

	if dir != m.listedDir {
		entries, err := listDir(dir)
		if err != nil {
			m.entries = nil
			m.errMsg = err.Error()
		} else {
			m.entries = entries
			m.errMsg = ""
		}
		m.listedDir = dir
	}

	// Rebuild filtered / unfiltered from entries + stub.
	m.filtered = m.filtered[:0]
	m.unfiltered = m.unfiltered[:0]
	for i, e := range m.entries {
		if !strings.HasPrefix(strings.ToLower(e.Name), strings.ToLower(stub)) {
			continue
		}
		if m.cfg.Mode == ModeDirs {
			if e.IsDir {
				m.filtered = append(m.filtered, i)
			} else {
				m.unfiltered = append(m.unfiltered, i)
			}
		} else {
			m.filtered = append(m.filtered, i)
		}
	}

	// Reset cursor when the stub changes; otherwise clamp.
	if stub != prevStub {
		m.cursor = 0
		m.scroll = 0
	} else {
		if m.cursor >= len(m.filtered) {
			if len(m.filtered) > 0 {
				m.cursor = len(m.filtered) - 1
			} else {
				m.cursor = 0
			}
		}
		m.adjustScroll(m.listCap())
	}
}

// acceptCompletion fills the input with the currently highlighted entry.
func (m *Model) acceptCompletion() tea.Cmd {
	if len(m.filtered) == 0 {
		return nil
	}
	e := m.entries[m.filtered[m.cursor]]
	newVal := filepath.Join(m.listedDir, e.Name)
	if e.IsDir {
		newVal += string(os.PathSeparator)
	}
	m.input.SetValue(newVal)
	m.input.CursorEnd()
	m.syncFromInput()
	return nil
}

// acceptSelection resolves the current input (optionally completing against
// the highlighted entry) and invokes cfg.OnSelect.
func (m *Model) acceptSelection() tea.Cmd {
	raw := ExpandHome(m.input.Value())
	if raw == "" {
		if home, err := os.UserHomeDir(); err == nil {
			raw = home
		} else {
			raw = "/"
		}
	}

	// If the typed stub uniquely prefixes a filtered entry (but doesn't fully
	// match it), treat it as an implicit completion.
	// In ModeFilesAndDirs, also apply when the stub is empty (arrow-key
	// navigation): the highlighted entry is the intended target.
	if len(m.filtered) > 0 {
		e := m.entries[m.filtered[m.cursor]]
		partialMatch := m.stub != "" &&
			strings.HasPrefix(strings.ToLower(e.Name), strings.ToLower(m.stub)) &&
			!strings.EqualFold(e.Name, m.stub)
		arrowNav := m.cfg.Mode == ModeFilesAndDirs && m.stub == ""
		if partialMatch || arrowNav {
			raw = filepath.Join(m.listedDir, e.Name)
		}
	}

	abs, _ := filepath.Abs(ExpandHome(raw))
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	if err == nil {
		if !info.IsDir() && m.cfg.Mode == ModeDirs {
			m.errMsg = "not a directory"
			return nil
		}
		// In ModeFilesAndDirs, enter on a directory navigates into it; only
		// files are valid selection targets.
		if info.IsDir() && m.cfg.Mode == ModeFilesAndDirs {
			m.input.SetValue(abs + string(os.PathSeparator))
			m.input.CursorEnd()
			m.syncFromInput()
			return nil
		}
	} else if os.IsNotExist(err) {
		if m.cfg.Mode == ModeDirs {
			if mkErr := ensureDir(abs); mkErr != nil {
				m.errMsg = mkErr.Error()
				return nil
			}
		}
		// ModeFilesAndDirs: accept non-existent paths as-is.
	} else {
		m.errMsg = err.Error()
		return nil
	}

	cb := m.cfg.OnSelect
	m.Close()
	return func() tea.Msg { return cb(abs) }
}

// Update processes a single message. Only key messages are handled; all
// others are forwarded to the textinput.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if !m.open {
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		switch k {
		case "esc":
			m.Close()
			return nil
		case "enter":
			return m.acceptSelection()
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			m.adjustScroll(m.listCap())
			return nil
		case "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			m.adjustScroll(m.listCap())
			return nil
		case "tab":
			return m.acceptCompletion()
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.syncFromInput()
			return cmd
		}
	}
	return nil
}

// View renders the picker panel as a string of exactly width columns. Returns
// "" when closed.
func (m *Model) View(width int) string {
	if !m.open {
		return ""
	}

	panelBg := lipgloss.NewStyle().Background(styles.ColorOverlay)
	cap := m.listCap()

	rows := make([]string, 0, m.Height(width))

	// ── Top rule ──────────────────────────────────────────────────────────────
	rows = append(rows, styles.WhichKeyRuleTop.Render(strings.Repeat("─", width)))

	// ── Input row ─────────────────────────────────────────────────────────────
	inputRow := m.renderInputRow(width, panelBg)
	rows = append(rows, inputRow)

	// ── Mid rule + completions (only when list is non-empty) ──────────────────
	if cap > 0 {
		midRule := lipgloss.NewStyle().
			Background(styles.ColorOverlay).
			Foreground(styles.ColorBorder).
			Render(strings.Repeat("─", width))
		rows = append(rows, midRule)
		rows = append(rows, m.renderCompletionRows(width, cap, panelBg)...)
	}

	// ── Bottom rule ───────────────────────────────────────────────────────────
	rows = append(rows, styles.WhichKeyRuleBot.Render(strings.Repeat("─", width)))

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderInputRow builds the single input line including the ghost suffix and
// optional right-aligned error message.
func (m *Model) renderInputRow(width int, panelBg lipgloss.Style) string {
	indent := panelBg.Render("  ")

	// Prompt is baked into the textinput via ti.Prompt, but we also want it
	// bold/coloured independent of the input widget. Since ti.PromptStyle
	// already handles that, just take m.input.View() directly.
	inputView := m.input.View()

	// Ghost suffix: append after the input view when cursor is at end and the
	// highlighted completion extends the stub.
	ghost := ""
	if len(m.filtered) > 0 && m.cursor < len(m.filtered) &&
		m.input.Position() == len(m.input.Value()) {
		e := m.entries[m.filtered[m.cursor]]
		if strings.HasPrefix(strings.ToLower(e.Name), strings.ToLower(m.stub)) &&
			len(m.stub) < len(e.Name) {
			suffix := e.Name[len(m.stub):]
			if e.IsDir {
				suffix += "/"
			}
			ghost = lipgloss.NewStyle().
				Foreground(styles.ColorFaint).
				Italic(true).
				Background(styles.ColorOverlay).
				Render(suffix)
		}
	}

	left := indent + inputView + ghost

	// Error message: right-aligned in ColorDanger if present.
	if m.errMsg != "" {
		errStr := lipgloss.NewStyle().
			Foreground(styles.ColorDanger).
			Background(styles.ColorOverlay).
			Render(" " + m.errMsg + " ")
		available := width - lipgloss.Width(left) - lipgloss.Width(errStr)
		if available > 0 {
			left = left + panelBg.Render(strings.Repeat(" ", available)) + errStr
		} else {
			// Not enough room — just show the error in place of padding.
			left = left + errStr
		}
		pad := width - lipgloss.Width(left)
		if pad > 0 {
			left += panelBg.Render(strings.Repeat(" ", pad))
		}
		return left
	}

	pad := width - lipgloss.Width(left)
	if pad < 0 {
		pad = 0
	}
	return left + panelBg.Render(strings.Repeat(" ", pad))
}

// renderCompletionRows returns the list rows that fill the completion area.
func (m *Model) renderCompletionRows(width, cap int, panelBg lipgloss.Style) []string {
	rows := make([]string, 0, cap)

	rowSelBg := lipgloss.NewStyle().Background(styles.ColorRaised)

	// Combine filtered (selectable) and unfiltered (display-only) into a
	// unified view list for scroll-window purposes.
	type viewEntry struct {
		idx      int // index into m.entries
		selIndex int // index into m.filtered; -1 for file-only rows
	}

	view := make([]viewEntry, 0, len(m.filtered)+len(m.unfiltered))
	for si, ei := range m.filtered {
		view = append(view, viewEntry{idx: ei, selIndex: si})
	}
	for _, ei := range m.unfiltered {
		view = append(view, viewEntry{idx: ei, selIndex: -1})
	}

	// Empty directory marker.
	if len(m.entries) == 0 {
		empty := panelBg.Render("    ") +
			lipgloss.NewStyle().
				Foreground(styles.ColorFaint).
				Background(styles.ColorOverlay).
				Render("— empty —")
		pad := width - lipgloss.Width(empty)
		if pad < 0 {
			pad = 0
		}
		empty += panelBg.Render(strings.Repeat(" ", pad))
		rows = append(rows, empty)
		return rows
	}

	end := m.scroll + cap
	if end > len(view) {
		end = len(view)
	}

	for i := m.scroll; i < end; i++ {
		ve := view[i]
		e := m.entries[ve.idx]
		selected := ve.selIndex == m.cursor && ve.selIndex >= 0

		var row string
		if ve.selIndex < 0 {
			// File-only entry in ModeDirs — greyed out, never selected.
			name := panelBg.Render("    ") +
				lipgloss.NewStyle().
					Foreground(styles.ColorFaint).
					Background(styles.ColorOverlay).
					Render(e.Name)
			pad := width - lipgloss.Width(name)
			if pad < 0 {
				pad = 0
			}
			row = name + panelBg.Render(strings.Repeat(" ", pad))
		} else {
			// Selectable entry.
			rowBg := panelBg
			if selected {
				rowBg = rowSelBg
			}

			var selBar string
			if selected {
				selBar = lipgloss.NewStyle().
					Foreground(styles.ColorPrimaryHi).
					Background(styles.ColorRaised).
					Bold(true).
					Render("▌") + rowBg.Render(" ")
			} else {
				selBar = rowBg.Render("  ")
			}

			displayName := e.Name
			if e.IsDir {
				displayName += "/"
			}

			var nameStyle lipgloss.Style
			if selected {
				nameStyle = rowBg.Foreground(styles.ColorPrimaryHi).Bold(true)
			} else {
				nameStyle = rowBg.Foreground(styles.ColorFg)
			}

			nameCell := nameStyle.Render(displayName)
			row = selBar + nameCell
			pad := width - lipgloss.Width(row)
			if pad < 0 {
				pad = 0
			}
			row += rowBg.Render(strings.Repeat(" ", pad))
		}

		rows = append(rows, row)
	}

	return rows
}

// StatusKeys returns a status-bar hint string for the picker.
func (m *Model) StatusKeys() string {
	bar := styles.StatusBar
	sep := bar.Render("  ") + styles.StatusSep.Render("·") + bar.Render("  ")
	hint := func(k, label string) string {
		return styles.StatusKey.Render(k) + bar.Render(" ") + styles.StatusKeyLabel.Render(label)
	}
	pairs := []string{
		hint("↑↓", "navigate"),
		hint("tab", "complete"),
		hint("↵", "select"),
		hint("esc", "cancel"),
	}
	return bar.Render("  ") + strings.Join(pairs, sep)
}
