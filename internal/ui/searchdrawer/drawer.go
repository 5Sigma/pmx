// Package searchdrawer implements the full-text search drawer for pmx.
//
// Visually it is a full-width panel that sits between the application header
// and the mail list, compressing the content area below (the same way the
// bottom pathpicker panel does, mirrored to the top). The drawer owns a text
// input, a cursor-driven results list, and a monotonic generation counter
// that lets the host app drop stale debounced search dispatches.
//
// The drawer is presentation-agnostic in two respects: it stores results as
// *mail.Message and delegates row rendering to a caller-supplied RowRenderer
// closure. This keeps mailItem/mailDelegate unexported from the top-level ui
// package while still giving every result row a pixel-identical look to the
// mail list.
package searchdrawer

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

// debounceInterval is how long after the last keystroke a QueryTickMsg will
// fire. It is a var (not const) so tests can shrink it.
var debounceInterval = 150 * time.Millisecond

// rowHeight is the number of terminal lines each result row occupies. Must
// match the mail-list delegate height so results align with the list.
const rowHeight = 3

// Nominal number of result slots the drawer tries to show when the host has
// room. Four rows is the sweet spot: enough to scan common queries at a
// glance without overwhelming the mail list below.
const nominalResultRows = 4

// Chrome rows: title + input + inner rule + footer + bottom edge rule.
const chromeRows = 5

// searchGlyph is the Nerd Font magnifying-glass used in the title
// strip and the empty-state hint. The user is guaranteed to have a
// Nerd Font installed (NixOS workstation).
const searchGlyph = "" // nf-fa-search

// RowRenderer renders a single search-result row as a (rowHeight * width)
// character block. The caller is responsible for background/selection styling.
type RowRenderer func(msg *mail.Message, width int, selected bool) string

// QueryTickMsg is dispatched by the drawer after the debounce interval. The
// host app should compare Gen against m.PendingGen(); if equal, run the
// search and return a SetResults call.
type QueryTickMsg struct {
	Gen   uint64
	Query string
}

// Model is the search drawer's state. Zero value is a closed drawer with no
// renderer; always construct via New().
type Model struct {
	input      textinput.Model
	results    []*mail.Message
	cursor     int
	pendingGen uint64
	latestGen  uint64
	loading    bool
	width      int
	height     int
	open       bool
	renderRow  RowRenderer
	onSelect   func(*mail.Message) tea.Msg
}

// New constructs a search drawer. renderRow is required; onSelect is invoked
// when the user confirms a result with <enter>. The returned tea.Msg is
// forwarded as a tea.Cmd by Update.
func New(renderRow RowRenderer, onSelect func(*mail.Message) tea.Msg) *Model {
	ti := textinput.New()
	// The prompt is painted manually in renderInputRow so it can carry its
	// own styling (icon + separator); keep the bubble's own prompt empty.
	ti.Prompt = ""
	ti.Placeholder = "search mail by subject, sender, or body…"
	ti.CharLimit = 128
	ti.TextStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFg).
		Background(styles.ColorSurface)
	ti.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(styles.ColorFaint).
		Background(styles.ColorSurface).
		Italic(true)
	ti.PromptStyle = lipgloss.NewStyle().
		Background(styles.ColorSurface)
	ti.Cursor.Style = lipgloss.NewStyle().
		Foreground(styles.ColorAccent).
		Background(styles.ColorSurface)
	return &Model{
		input:     ti,
		renderRow: renderRow,
		onSelect:  onSelect,
	}
}

// IsOpen reports whether the drawer is currently visible.
func (m *Model) IsOpen() bool { return m != nil && m.open }

// IsCapturingInput reports whether free-text keystrokes should be routed to
// the drawer. Returns true whenever the drawer is open — the search input is
// always focused while visible.
func (m *Model) IsCapturingInput() bool { return m.IsOpen() }

// Open shows the drawer, focuses the input, and clears any prior results and
// query.
func (m *Model) Open() {
	m.open = true
	m.cursor = 0
	m.loading = false
	m.results = nil
	m.pendingGen = 0
	m.latestGen = 0
	m.input.SetValue("")
	m.input.Focus()
}

// Close hides the drawer and blurs the input. Results are cleared so the
// next Open starts fresh.
func (m *Model) Close() {
	m.open = false
	m.input.Blur()
	m.input.SetValue("")
	m.results = nil
	m.cursor = 0
	m.loading = false
}

// SetSize records the available space for the drawer. The drawer's height
// derives from the given totalContentHeight (clamped so the mail list below
// still has room to render). Width is always the host's full width.
func (m *Model) SetSize(width, totalContentHeight int) {
	m.width = width
	// Reserve at least 6 rows for the mail list below.
	maxH := totalContentHeight - 6
	if maxH < 6 {
		maxH = 6
	}
	// Nominal shape: chrome (5) + N result rows * 3 lines.
	nominal := chromeRows + rowHeight*nominalResultRows
	h := nominal
	if h > maxH {
		h = maxH
	}
	if h < 6 {
		h = 6
	}
	m.height = h
	// Input width: reserve 4 cells on the left (accent bar + icon + gap) and
	// a couple of trailing cells of breathing room on the right.
	inputW := width - 6
	if inputW < 8 {
		inputW = 8
	}
	m.input.Width = inputW
}

// Height returns the number of rows the drawer currently occupies, or 0 when
// closed.
func (m *Model) Height() int {
	if !m.IsOpen() {
		return 0
	}
	return m.height
}

// Width returns the drawer's width when open, or 0 when closed.
func (m *Model) Width() int {
	if !m.IsOpen() {
		return 0
	}
	return m.width
}

// Query returns the current search input text (trimmed).
func (m *Model) Query() string {
	return strings.TrimSpace(m.input.Value())
}

// PendingGen returns the current pending generation counter. The host app
// compares a QueryTickMsg.Gen against this to drop stale ticks.
func (m *Model) PendingGen() uint64 { return m.pendingGen }

// SetResults updates the result list, provided gen is still current. Stale
// responses (gen < latestGen) are dropped. The cursor is clamped into the new
// range.
func (m *Model) SetResults(gen uint64, results []*mail.Message) {
	if gen < m.latestGen {
		return
	}
	m.latestGen = gen
	m.results = results
	m.loading = false
	if m.cursor < 0 || m.cursor >= len(results) {
		m.cursor = 0
	}
}

// SetLoading toggles the loading indicator shown next to the results count.
func (m *Model) SetLoading(b bool) { m.loading = b }

// Selected returns the currently highlighted message, or nil when the result
// list is empty.
func (m *Model) Selected() *mail.Message {
	if m.cursor < 0 || m.cursor >= len(m.results) {
		return nil
	}
	return m.results[m.cursor]
}

// Update handles a single bubbletea message. Returns a tea.Cmd that may
// contain a QueryTickMsg (after a keystroke) or the onSelect result (after
// <enter>).
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	if !m.IsOpen() {
		return nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	k := key.String()

	switch k {
	case "esc":
		m.Close()
		return nil
	case "enter":
		sel := m.Selected()
		if sel == nil || m.onSelect == nil {
			return nil
		}
		cb := m.onSelect
		m.Close()
		return func() tea.Msg { return cb(sel) }
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
		return nil
	case "down", "ctrl+n":
		if m.cursor < len(m.results)-1 {
			m.cursor++
		}
		return nil
	case "pgup":
		m.cursor -= 5
		if m.cursor < 0 {
			m.cursor = 0
		}
		return nil
	case "pgdown":
		m.cursor += 5
		if m.cursor >= len(m.results) {
			m.cursor = len(m.results) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return nil
	}

	// Any other key: forward to the textinput and schedule a debounced tick.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.pendingGen++
	m.loading = m.Query() != ""
	gen := m.pendingGen
	q := m.Query()
	tick := tea.Tick(debounceInterval, func(time.Time) tea.Msg {
		return QueryTickMsg{Gen: gen, Query: q}
	})
	if cmd == nil {
		return tick
	}
	return tea.Batch(cmd, tick)
}

// StatusKeys returns the status-bar hint string shown while the drawer owns
// input.
func (m *Model) StatusKeys() string {
	bar := styles.StatusBar
	sep := bar.Render("  ") + styles.StatusSep.Render("·") + bar.Render("  ")

	hint := func(k, label string) string {
		return styles.StatusKey.Render(k) +
			bar.Render(" ") +
			styles.StatusKeyLabel.Render(label)
	}

	chip := styles.StatusKey.Render(" SEARCH ")
	pairs := []string{
		hint("↵", "open"),
		hint("↑↓", "move"),
		hint("esc", "close"),
	}
	return bar.Render("  ") + chip + sep + strings.Join(pairs, sep)
}

// View renders the drawer as a width x height block. When closed it returns
// the empty string.
//
// Layout, top to bottom:
//
//	row 0        title strip (accent bar + icon + "Search" + right-side meta)
//	row 1        input strip (accent bar + search glyph + text input)
//	row 2        inner thin rule (ColorBorder on ColorSurface)
//	rows 3..k    result rows (each rowHeight tall) or centered empty-state
//	row k+1      footer (result count on left, inline key hints on right)
//	row n-1      bottom edge rule (ColorBorder on ColorBg) — marks the handoff
//	             to the mail list below
//
// The footer intentionally duplicates the key hints from StatusKeys() in a
// subdued, bar-less style. The repetition is deliberate: the global status bar
// still carries the SEARCH mode chip (which identifies the active overlay),
// and the drawer-local footer keeps those hints in the user's immediate field
// of view so they don't have to glance to the bottom of the screen while
// typing a query.
func (m *Model) View() string {
	if !m.IsOpen() || m.width <= 0 || m.height <= 0 {
		return ""
	}

	surface := lipgloss.NewStyle().Background(styles.ColorSurface)

	rows := make([]string, 0, m.height)
	rows = append(rows, m.renderTitleRow(surface))
	rows = append(rows, m.renderInputRow(surface))
	rows = append(rows, m.renderInnerRule())

	// Reserve the last two rows for the footer and the bottom-edge rule.
	// Everything in between is the results region.
	resultsBudget := m.height - len(rows) - 2
	if resultsBudget < rowHeight {
		resultsBudget = rowHeight
	}
	rowsPerPage := resultsBudget / rowHeight
	if rowsPerPage < 1 {
		rowsPerPage = 1
	}

	// Scroll window.
	start := 0
	if len(m.results) > rowsPerPage && rowsPerPage > 0 {
		if m.cursor >= rowsPerPage {
			start = m.cursor - rowsPerPage + 1
		}
		if start+rowsPerPage > len(m.results) {
			start = len(m.results) - rowsPerPage
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + rowsPerPage
	if end > len(m.results) {
		end = len(m.results)
	}

	linesUsed := 0
	for i := start; i < end; i++ {
		block := m.renderRow(m.results[i], m.width, i == m.cursor)
		for _, ln := range strings.SplitN(block, "\n", rowHeight) {
			rows = append(rows, ln)
			linesUsed++
		}
	}

	// Pad the remaining results region with either a centered empty-state
	// hint or plain surface rows. The hint is placed on roughly the vertical
	// middle of the padded block so the drawer never feels blank.
	padLines := resultsBudget - linesUsed
	if padLines < 0 {
		padLines = 0
	}
	emptyHint := m.emptyStateText()
	midIdx := padLines / 2
	for i := 0; i < padLines; i++ {
		if emptyHint != "" && i == midIdx {
			rows = append(rows, m.renderCenteredHint(emptyHint, surface))
		} else {
			rows = append(rows, surface.Render(strings.Repeat(" ", m.width)))
		}
	}

	rows = append(rows, m.renderFooter(surface))
	rows = append(rows, m.renderBottomEdge())

	// Final safety pad / clip to exact height.
	for len(rows) < m.height {
		rows = append(rows, surface.Render(strings.Repeat(" ", m.width)))
	}
	if len(rows) > m.height {
		rows = rows[:m.height]
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderTitleRow draws the drawer's header strip. Leading accent bar + icon,
// then a bold "Search" label, right-aligned context meta (loading indicator,
// result count, or placeholder hint).
func (m *Model) renderTitleRow(surface lipgloss.Style) string {
	accentBar := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorAccent).
		Bold(true).
		Render("▌")

	icon := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorAccent).
		Render(searchGlyph)

	title := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorPrimaryHi).
		Bold(true).
		Render("Search")

	left := accentBar + surface.Render(" ") + icon + surface.Render("  ") + title

	right := surface.Render(m.renderTitleMeta()) + surface.Render(" ")

	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}
	return left + surface.Render(strings.Repeat(" ", pad)) + right
}

// renderTitleMeta returns the pre-styled meta string shown on the right side
// of the title row.
func (m *Model) renderTitleMeta() string {
	metaBase := lipgloss.NewStyle().Background(styles.ColorSurface)

	switch {
	case m.loading:
		return metaBase.
			Foreground(styles.ColorAccent).
			Italic(true).
			Render("searching…")

	case m.Query() == "":
		return metaBase.
			Foreground(styles.ColorFaint).
			Italic(true).
			Render("type to search")

	case len(m.results) == 0:
		return metaBase.
			Foreground(styles.ColorDimFg).
			Italic(true).
			Render("no matches")

	default:
		// "cursor+1 of total results" — compact and informative.
		pos := metaBase.
			Foreground(styles.ColorFgStrong).
			Bold(true).
			Render(fmt.Sprintf("%d", m.cursor+1))
		of := metaBase.Foreground(styles.ColorDimFg).Render(" of ")
		total := metaBase.
			Foreground(styles.ColorPrimaryHi).
			Bold(true).
			Render(fmt.Sprintf("%d", len(m.results)))
		label := metaBase.Foreground(styles.ColorDimFg).Render(" results")
		return pos + of + total + label
	}
}

// renderInputRow paints the text input strip. Mirrors the title row's leading
// chrome (accent bar + icon) so the two header lines read as one block.
func (m *Model) renderInputRow(surface lipgloss.Style) string {
	// Accent bar continues the title strip's left edge.
	accentBar := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorPrimaryHi).
		Bold(true).
		Render("▌")

	// Prompt glyph: a dim chevron. The search icon lives in the title row;
	// here the chevron is the "insertion cursor" affordance.
	prompt := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorAccent).
		Bold(true).
		Render("❯")

	left := accentBar + surface.Render(" ") + prompt + surface.Render(" ") + m.input.View()
	pad := m.width - lipgloss.Width(left)
	if pad < 0 {
		pad = 0
	}
	return left + surface.Render(strings.Repeat(" ", pad))
}

// renderInnerRule paints the thin divider between the input row and the
// results region. It uses the surface background so it visually belongs to
// the drawer (not to the content area below).
func (m *Model) renderInnerRule() string {
	s := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorBorder)
	return s.Render(strings.Repeat("─", m.width))
}

// renderBottomEdge paints the drawer's bottom boundary. Painted on ColorBg
// (same as the mail list background), so it reads as a hairline resting on
// the mail list, not as part of the drawer surface.
func (m *Model) renderBottomEdge() string {
	s := lipgloss.NewStyle().
		Background(styles.ColorBg).
		Foreground(styles.ColorBorder)
	return s.Render(strings.Repeat("─", m.width))
}

// renderFooter paints the drawer's bottom row (above the bottom-edge rule):
// result count on the left, inline key hints on the right. Hints are rendered
// bare (no bar background) so they feel like an aside, not a second status
// bar.
func (m *Model) renderFooter(surface lipgloss.Style) string {
	footStyle := lipgloss.NewStyle().Background(styles.ColorSurface)

	// Left side: short status note.
	var left string
	switch {
	case m.loading:
		left = footStyle.
			Foreground(styles.ColorFaint).
			Italic(true).
			Render("  searching the index…")
	case m.Query() == "":
		left = footStyle.
			Foreground(styles.ColorFaint).
			Italic(true).
			Render("  waiting for input")
	case len(m.results) == 0:
		left = footStyle.
			Foreground(styles.ColorDimFg).
			Italic(true).
			Render("  no matches")
	default:
		idx := footStyle.
			Foreground(styles.ColorPrimaryHi).
			Bold(true).
			Render(fmt.Sprintf("%d", m.cursor+1))
		of := footStyle.Foreground(styles.ColorDimFg).Render("/")
		total := footStyle.
			Foreground(styles.ColorFg).
			Bold(true).
			Render(fmt.Sprintf("%d", len(m.results)))
		left = footStyle.Render("  ") + idx + of + total
	}

	// Right side: compact key hints.
	right := m.renderFooterHints() + footStyle.Render(" ")

	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 0 {
		pad = 0
	}
	return left + footStyle.Render(strings.Repeat(" ", pad)) + right
}

// renderFooterHints returns the inline key-hint strip used on the right side
// of the footer. The hints echo StatusKeys() but with a surface background
// (so they tile with the rest of the drawer) and no leading mode chip.
func (m *Model) renderFooterHints() string {
	keyStyle := lipgloss.NewStyle().
		Background(styles.ColorRaised).
		Foreground(styles.ColorPrimaryHi).
		Bold(true).
		Padding(0, 1)
	labelStyle := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorSubdued)
	sepStyle := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorBorder)
	gap := lipgloss.NewStyle().Background(styles.ColorSurface).Render(" ")
	sep := gap + sepStyle.Render("·") + gap

	hint := func(k, label string) string {
		return keyStyle.Render(k) + gap + labelStyle.Render(label)
	}

	return hint("↵", "open") + sep + hint("↑↓", "move") + sep + hint("esc", "close")
}

// renderCenteredHint produces a single row whose contents are horizontally
// centered inside the drawer width, painted on ColorSurface.
func (m *Model) renderCenteredHint(hint string, surface lipgloss.Style) string {
	hintW := lipgloss.Width(hint)
	if hintW >= m.width {
		return hint
	}
	left := (m.width - hintW) / 2
	right := m.width - hintW - left
	return surface.Render(strings.Repeat(" ", left)) + hint + surface.Render(strings.Repeat(" ", right))
}

// emptyStateText returns the pre-styled hint shown in the vertical middle of
// an empty results region, or "" when a hint is not warranted (e.g. there are
// results to show).
func (m *Model) emptyStateText() string {
	base := lipgloss.NewStyle().Background(styles.ColorSurface)

	switch {
	case m.loading:
		return base.
			Foreground(styles.ColorFaint).
			Italic(true).
			Render("searching mail index…")

	case m.Query() == "":
		// Inviting first-use hint. Icon + italic prompt in faint tone so the
		// drawer never reads as empty.
		icon := base.Foreground(styles.ColorAccent).Render(searchGlyph)
		text := base.
			Foreground(styles.ColorFaint).
			Italic(true).
			Render("  type to search mail…")
		return icon + text

	case len(m.results) == 0:
		q := m.Query()
		if len(q) > 48 {
			q = q[:47] + "…"
		}
		pre := base.
			Foreground(styles.ColorDimFg).
			Italic(true).
			Render("no matches for ")
		quoted := base.
			Foreground(styles.ColorFg).
			Bold(true).
			Render("“" + q + "”")
		return pre + quoted

	default:
		return ""
	}
}
