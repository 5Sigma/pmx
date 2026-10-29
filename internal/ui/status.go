// Package ui — status / debug screen
//
// A read-only, tabbed modal pushed over the mail list by <space>?. Mirrors
// the compose dialog's panel shape (title bar → tab strip → separator →
// surface-filled body, all wrapped in a RoundedBorder) but takes up most of
// the screen because the content is dense: process mode, config paths, sync
// intervals, and per-account connection + live-watcher + folder stats.
//
// Everything expensive (store calls, path lookups) happens inside tea.Cmd
// closures so a tab's body renders a "loading…" placeholder on first paint
// and swaps in when its statusTabLoadedMsg arrives. Live watcher updates
// come in as mailsync.WatcherState / FolderDone events on the shared stream
// and merge into the local cache; the owning tab is invalidated and refetched
// so folder counts stay fresh after a sync tick.
package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailops"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/ui/keymap"
	"github.com/5Sigma/pmx/internal/ui/styles"
	"github.com/5Sigma/pmx/internal/version"
)

// statusTabLoadedMsg carries the fully-materialized payload for one tab.
// Published by the per-tab fetch tea.Cmd so the view can swap its loading
// placeholder for real data without ever blocking Update().
type statusTabLoadedMsg struct {
	tabIdx int
	data   tabData
}

// tabData is the discriminated payload for a loaded tab. Exactly one of
// General / Account is populated; the other side is zero-valued.
type tabData struct {
	kind    tabKind
	general *generalData
	account *accountData
}

type tabKind int

const (
	tabKindGeneral tabKind = iota
	tabKindAccount
)

// generalData is the loaded payload for the first tab.
type generalData struct {
	mode        string
	modeAccent  bool // true → iris (daemon); false → mint (standalone)
	socketPath  string
	versionStr  string
	logPath     string
	pmxdLogPath string
	dbPath      string
	cfgPath     string

	pollInterval    time.Duration
	sweepInterval   time.Duration
	sendInterval    time.Duration
	sendMaxAttempts int
	logLevel        string
	accountCount    int

	daemonCheckInterval time.Duration
	daemonNotifEnabled  bool
	daemonNotifFolders  []string
	daemonIDLE          *bool
}

// folderStat is one row of the per-account folder table.
type folderStat struct {
	name     string
	role     mail.FolderRole
	total    int
	unread   int
	lastSync time.Time
}

// accountData is the loaded payload for an account tab.
type accountData struct {
	acct      config.Account
	accountID int64

	watcher mailsync.WatcherState

	folders        []folderStat
	outboxPending  int
	storeAvailable bool
	errMsg         string
}

// statusView is the modal. Pointer-receiver View so the long-lived tab
// cache + width/height can be mutated without reassignment.
type statusView struct {
	cfg          *config.Config
	store        mail.MessageStore
	status       mailops.StatusOps
	daemonMode   bool
	watcherCache map[string]mailsync.WatcherState

	tabs    []statusTab
	loaded  []bool
	data    []tabData
	active  int
	width   int
	height  int
	spinner int // rotating index for the "loading…" glyph
}

// statusTab is one entry in the horizontal tab strip. The account index
// (-1 for General) is captured so the fetch cmd can resolve the right
// config.Account without re-indexing on every message.
type statusTab struct {
	label    string
	kind     tabKind
	acctIdx  int // -1 for general
	acctName string
	icon     string
}

// newStatusView builds a status modal seeded with a snapshot of the caller's
// watcher map. The snapshot is copied so later caller mutations don't leak
// into the view.
func newStatusView(
	cfg *config.Config,
	store mail.MessageStore,
	status mailops.StatusOps,
	daemonMode bool,
	watcherStates map[string]mailsync.WatcherState,
) View {
	sv := &statusView{
		cfg:          cfg,
		store:        store,
		status:       status,
		daemonMode:   daemonMode,
		watcherCache: make(map[string]mailsync.WatcherState, len(watcherStates)),
	}
	for k, v := range watcherStates {
		sv.watcherCache[k] = v
	}

	// Build the tab list. General is always first. The dashboard glyph
	// (nf-fa-tachometer U+F0E4) matches the status/diagnostics framing.
	sv.tabs = []statusTab{{
		label:   "General",
		kind:    tabKindGeneral,
		acctIdx: -1,
		icon:    "",
	}}
	if cfg != nil {
		for i, a := range cfg.Accounts {
			sv.tabs = append(sv.tabs, statusTab{
				label:    a.Name,
				kind:     tabKindAccount,
				acctIdx:  i,
				acctName: a.Name,
				icon:     styles.AccountIconUser,
			})
		}
	}

	sv.loaded = make([]bool, len(sv.tabs))
	sv.data = make([]tabData, len(sv.tabs))
	return sv
}

// Init kicks off a parallel fetch for every tab. The host program routes
// each statusTabLoadedMsg back into Update().
func (sv *statusView) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(sv.tabs))
	for i := range sv.tabs {
		cmds = append(cmds, sv.fetchTab(i))
	}
	return tea.Batch(cmds...)
}

// fetchTab returns a tea.Cmd that computes one tab's payload off the event
// loop. All store reads must flow through here so the view never blocks.
func (sv *statusView) fetchTab(idx int) tea.Cmd {
	if idx < 0 || idx >= len(sv.tabs) {
		return nil
	}
	t := sv.tabs[idx]

	// Snapshot every closure-captured value by copy so the goroutine can run
	// independently of later state mutation.
	cfg := sv.cfg
	store := sv.store
	daemonMode := sv.daemonMode

	switch t.kind {
	case tabKindGeneral:
		return func() tea.Msg {
			return statusTabLoadedMsg{tabIdx: idx, data: tabData{
				kind:    tabKindGeneral,
				general: buildGeneralData(cfg, daemonMode),
			}}
		}
	case tabKindAccount:
		acctIdx := t.acctIdx
		return func() tea.Msg {
			if cfg == nil || acctIdx < 0 || acctIdx >= len(cfg.Accounts) {
				return statusTabLoadedMsg{tabIdx: idx, data: tabData{
					kind:    tabKindAccount,
					account: &accountData{errMsg: "account unavailable"},
				}}
			}
			acct := cfg.Accounts[acctIdx]
			ad := &accountData{acct: acct}
			if store == nil {
				ad.errMsg = "mail store unavailable"
				return statusTabLoadedMsg{tabIdx: idx, data: tabData{kind: tabKindAccount, account: ad}}
			}
			ad.storeAvailable = true
			id, err := store.EnsureAccount(acct.Name, acct.Email)
			if err != nil {
				ad.errMsg = "account lookup: " + err.Error()
				return statusTabLoadedMsg{tabIdx: idx, data: tabData{kind: tabKindAccount, account: ad}}
			}
			ad.accountID = id

			folders, err := store.ListFolders(id)
			if err == nil {
				ad.folders = make([]folderStat, 0, len(folders))
				for _, f := range folders {
					total, _ := store.CountMessages(f.ID)
					unread, _ := store.CountUnread(f.ID)
					ad.folders = append(ad.folders, folderStat{
						name:     f.Name,
						role:     f.Role,
						total:    total,
						unread:   unread,
						lastSync: f.LastSyncAt,
					})
				}
			}

			if pending, err := store.ListPendingOutboxForAccount(id, 100); err == nil {
				ad.outboxPending = len(pending)
			}

			return statusTabLoadedMsg{tabIdx: idx, data: tabData{kind: tabKindAccount, account: ad}}
		}
	}
	return nil
}

// buildGeneralData assembles the General tab payload. Pulled out so the
// fetch cmd can call it in a goroutine without touching the view.
func buildGeneralData(cfg *config.Config, daemonMode bool) *generalData {
	g := &generalData{}
	if daemonMode {
		g.mode = "Daemon"
		g.modeAccent = true
		if s, err := config.PmxdSocketPath(); err == nil {
			g.socketPath = s
		} else {
			g.socketPath = "(unavailable)"
		}
		if p, err := config.PmxdLogPath(); err == nil {
			g.pmxdLogPath = p
		}
	} else {
		g.mode = "Standalone"
	}

	g.versionStr = version.String()
	if p, err := config.LogPath(); err == nil {
		g.logPath = p
	}
	if p, err := config.DatabasePath(); err == nil {
		g.dbPath = p
	}
	if p, err := config.ConfigPath(); err == nil {
		g.cfgPath = p
	}

	if cfg != nil {
		g.pollInterval = cfg.General.PollInterval
		g.sweepInterval = cfg.General.SweepInterval
		g.sendInterval = cfg.General.SendInterval
		g.sendMaxAttempts = cfg.General.SendMaxAttempts
		g.logLevel = cfg.General.LogLevel
		g.accountCount = len(cfg.Accounts)

		g.daemonCheckInterval = cfg.Daemon.CheckInterval
		g.daemonNotifEnabled = cfg.Daemon.NotificationsEnabled
		g.daemonNotifFolders = cfg.Daemon.NotifyFolders
		g.daemonIDLE = cfg.Daemon.IDLEEnabled
	}
	return g
}

// Update dispatches keystrokes, incoming tab payloads, and live watcher
// events. Esc pops the modal; r invalidates & refetches the active tab; tab
// / shift+tab cycle. WatcherState / FolderDone events refresh their account
// tab in-place.
func (sv *statusView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return sv, Pop()
		case "tab", "right", "l":
			if len(sv.tabs) > 0 {
				sv.active = (sv.active + 1) % len(sv.tabs)
			}
			return sv, nil
		case "shift+tab", "left", "h":
			if len(sv.tabs) > 0 {
				sv.active = (sv.active - 1 + len(sv.tabs)) % len(sv.tabs)
			}
			return sv, nil
		case "r":
			if sv.active >= 0 && sv.active < len(sv.loaded) {
				sv.loaded[sv.active] = false
				sv.data[sv.active] = tabData{}
			}
			return sv, sv.fetchTab(sv.active)
		}

	case statusTabLoadedMsg:
		if msg.tabIdx >= 0 && msg.tabIdx < len(sv.loaded) {
			sv.loaded[msg.tabIdx] = true
			sv.data[msg.tabIdx] = msg.data
		}
		return sv, nil

	case mailsync.WatcherState:
		sv.watcherCache[msg.Account] = msg
		if idx := sv.tabIndexForAccount(msg.Account); idx >= 0 {
			if ad := sv.data[idx].account; ad != nil {
				ad.watcher = msg
			}
		}
		return sv, nil

	case mailsync.FolderDone:
		if idx := sv.tabIndexForAccount(msg.Account); idx >= 0 {
			sv.loaded[idx] = false
			sv.data[idx] = tabData{}
			return sv, sv.fetchTab(idx)
		}
		return sv, nil
	}
	return sv, nil
}

// tabIndexForAccount resolves an account name to its tab index, or -1.
func (sv *statusView) tabIndexForAccount(name string) int {
	for i, t := range sv.tabs {
		if t.kind == tabKindAccount && t.acctName == name {
			return i
		}
	}
	return -1
}

func (sv *statusView) SetSize(width, height int) View {
	sv.width = width
	sv.height = height
	return sv
}

func (sv *statusView) Title() string { return "status" }

func (sv *statusView) StatusKeys() string {
	return keyHintsFromKeyMap(sv.KeyMap())
}

func (sv *statusView) KeyMap() keymap.KeyMap {
	return keymap.KeyMap{
		Primary: []keymap.Binding{
			{Key: "tab", Desc: "next tab"},
			{Key: "shift+tab", Desc: "prev tab"},
			{Key: "r", Desc: "refresh"},
			{Key: "esc", Desc: "close"},
		},
	}
}

// Panel-local styles ---------------------------------------------------------
//
// All of these paint on either ColorBg (panel chrome) or ColorSurface /
// ColorOverlay (body sections). Keeping them in named vars at the top of the
// file keeps renderBody and friends scannable.
var (
	// statusTitleBar paints the top chrome rail — a dark overlay strip that
	// hosts a solid ColorPrimary brand chip on the left and a mode chip on
	// the right. Deliberately NOT ColorPrimary so the active tab pill reads
	// as a distinct element rather than bleeding into the title.
	statusTitleBar = lipgloss.NewStyle().
			Background(styles.ColorOverlay).
			Foreground(styles.ColorFg)

	statusTitleBrand = lipgloss.NewStyle().
				Background(styles.ColorPrimary).
				Foreground(styles.ColorFgStrong).
				Bold(true).
				Padding(0, 1)

	statusTitleLabel = lipgloss.NewStyle().
				Background(styles.ColorOverlay).
				Foreground(styles.ColorFgStrong).
				Bold(true)

	statusTitleSep = lipgloss.NewStyle().
			Background(styles.ColorOverlay).
			Foreground(styles.ColorBorder)

	statusTitleModeStandalone = lipgloss.NewStyle().
					Background(styles.ColorAccent).
					Foreground(styles.ColorBg).
					Bold(true).
					Padding(0, 1)

	statusTitleModeDaemon = lipgloss.NewStyle().
				Background(styles.ColorPrimaryHi).
				Foreground(styles.ColorBg).
				Bold(true).
				Padding(0, 1)

	// statusTitleUnderline is the thin accent hairline that separates the
	// title rail from the tab strip. Painted on ColorBg so the ColorPrimary
	// stroke reads as a brand underline rather than a block.
	statusTitleUnderline = lipgloss.NewStyle().
				Background(styles.ColorBg).
				Foreground(styles.ColorPrimary)

	statusTabActive = lipgloss.NewStyle().
			Background(styles.ColorPrimary).
			Foreground(styles.ColorFgStrong).
			Bold(true).
			Padding(0, 2)

	statusTabInactive = lipgloss.NewStyle().
				Background(styles.ColorSurface).
				Foreground(styles.ColorDimFg).
				Padding(0, 2)

	statusTabFiller = lipgloss.NewStyle().
			Background(styles.ColorSurface)

	statusTabRule = lipgloss.NewStyle().
			Background(styles.ColorBg).
			Foreground(styles.ColorBorder)

	statusBody = lipgloss.NewStyle().
			Background(styles.ColorSurface).
			Foreground(styles.ColorFg)

	statusSectionHeader = lipgloss.NewStyle().
				Background(styles.ColorOverlay).
				Foreground(styles.ColorAccent).
				Bold(true)

	statusSectionHeaderFiller = lipgloss.NewStyle().
					Background(styles.ColorOverlay)

	statusSectionHeaderGlyph = lipgloss.NewStyle().
					Background(styles.ColorOverlay).
					Foreground(styles.ColorPrimaryHi).
					Bold(true)

	statusLabel = lipgloss.NewStyle().
			Background(styles.ColorSurface).
			Foreground(styles.ColorDimFg)

	statusValue = lipgloss.NewStyle().
			Background(styles.ColorSurface).
			Foreground(styles.ColorFg)

	statusValueStrong = lipgloss.NewStyle().
				Background(styles.ColorSurface).
				Foreground(styles.ColorFgStrong).
				Bold(true)

	statusValueMuted = lipgloss.NewStyle().
				Background(styles.ColorSurface).
				Foreground(styles.ColorFaint).
				Italic(true)

	statusChipPrimary = lipgloss.NewStyle().
				Background(styles.ColorPrimary).
				Foreground(styles.ColorFgStrong).
				Bold(true).
				Padding(0, 1)

	statusChipAccent = lipgloss.NewStyle().
				Background(styles.ColorAccent).
				Foreground(styles.ColorBg).
				Bold(true).
				Padding(0, 1)

	statusChipWarn = lipgloss.NewStyle().
			Background(styles.ColorWarn).
			Foreground(styles.ColorBg).
			Bold(true).
			Padding(0, 1)

	statusChipDanger = lipgloss.NewStyle().
				Background(styles.ColorDanger).
				Foreground(styles.ColorBg).
				Bold(true).
				Padding(0, 1)

	statusChipMuted = lipgloss.NewStyle().
			Background(styles.ColorRaised).
			Foreground(styles.ColorDimFg).
			Padding(0, 1)

	statusCalloutDanger = lipgloss.NewStyle().
				Background(styles.ColorSurface).
				Foreground(styles.ColorDanger).
				Bold(true)

	statusTableHeader = lipgloss.NewStyle().
				Background(styles.ColorOverlay).
				Foreground(styles.ColorDimFg).
				Bold(true)
)

// View renders the whole modal. The outline is a vertical stack of:
//
//	title bar  →  tab strip  →  separator  →  body
//
// wrapped in a RoundedBorder with ColorPrimary chrome and centered on a
// ColorBg wash so the terminal background doesn't bleed through.
func (sv *statusView) View() string {
	w, h := sv.width, sv.height
	if w <= 0 || h <= 0 {
		return ""
	}

	panelW := w - 4
	if panelW < 40 {
		panelW = w - 2
	}
	if panelW < 24 {
		panelW = 24
	}
	panelH := h - 2
	if panelH < 10 {
		panelH = h
	}
	if panelH < 8 {
		panelH = 8
	}

	innerW := panelW - 2 // minus left + right border cells

	// Title bar — brand strip with a glyph + label on the left and a small
	// mode chip on the right.
	title := sv.renderTitleBar(innerW)

	// Thin iris hairline that visually separates the title rail from the
	// tab strip so the active tab doesn't bleed into the title.
	titleRule := statusTitleUnderline.Width(innerW).Render(strings.Repeat("▔", innerW))

	// Tab strip (ColorSurface tiling behind the pills).
	tabs := sv.renderTabStrip(innerW)

	// Thin separator between the tab strip and the body.
	sep := statusTabRule.Width(innerW).Render(strings.Repeat("─", innerW))

	// Body — remaining vertical space; filled on ColorSurface.
	chromeH := lipgloss.Height(title) + lipgloss.Height(titleRule) + lipgloss.Height(tabs) + lipgloss.Height(sep)
	bodyH := panelH - chromeH - 2 // minus top + bottom border cells
	if bodyH < 3 {
		bodyH = 3
	}
	body := sv.renderBody(innerW, bodyH)

	inner := lipgloss.JoinVertical(lipgloss.Left, title, titleRule, tabs, sep, body)

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorPrimary).
		BorderBackground(styles.ColorBg).
		Render(inner)

	return lipgloss.Place(w, h,
		lipgloss.Center, lipgloss.Center,
		panel,
		lipgloss.WithWhitespaceBackground(styles.ColorBg),
	)
}

func (sv *statusView) renderTitleBar(innerW int) string {
	brand := statusTitleBrand.Render(" pmx ")
	label := statusTitleBar.Render(" ") +
		statusTitleLabel.Render("status") +
		statusTitleBar.Render("  ") +
		statusTitleSep.Render("·") +
		statusTitleBar.Render("  ") +
		statusTitleBar.Foreground(styles.ColorDimFg).Italic(true).Render("diagnostics")

	modeChip := ""
	if sv.cfg != nil {
		if sv.daemonMode {
			modeChip = statusTitleModeDaemon.Render("DAEMON")
		} else {
			modeChip = statusTitleModeStandalone.Render("STANDALONE")
		}
	}

	left := brand + label
	right := modeChip + statusTitleBar.Render(" ")
	gap := innerW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + statusTitleBar.Render(strings.Repeat(" ", gap)) + right
}

func (sv *statusView) renderTabStrip(innerW int) string {
	if len(sv.tabs) == 0 {
		return statusTabFiller.Width(innerW).Render(strings.Repeat(" ", innerW))
	}

	// Build each tab chip at full fidelity first; if the total exceeds
	// innerW, replace the overflow suffix with a muted "… N more" chip.
	pills := make([]string, 0, len(sv.tabs))
	widths := make([]int, 0, len(sv.tabs))
	for i, t := range sv.tabs {
		label := t.label
		if label == "" {
			label = "(unnamed)"
		}
		// Icon prefix for account tabs. General has no icon on the strip —
		// the title bar already brands it.
		if t.icon != "" {
			label = t.icon + "  " + label
		}
		var chip string
		if i == sv.active {
			chip = statusTabActive.Render(label)
		} else {
			chip = statusTabInactive.Render(label)
		}
		pills = append(pills, chip)
		widths = append(widths, lipgloss.Width(chip))
	}

	// Compose a prefix of chips that fits, respecting that the active tab
	// must always be visible. We greedily include tabs left-to-right; if
	// that would hide the active tab, we drop earlier ones and surface the
	// active chip at the leading edge instead.
	rendered, used := packTabPills(pills, widths, sv.active, innerW)
	gap := innerW - used
	if gap < 0 {
		gap = 0
	}
	strip := rendered + statusTabFiller.Render(strings.Repeat(" ", gap))
	return strip
}

// packTabPills chooses which tab chips fit in width cells while keeping the
// active chip visible. Returns the composed string and the total width
// consumed. The "+N more" ghost is appended on the surface filler side when
// anything is hidden.
func packTabPills(pills []string, widths []int, active, width int) (string, int) {
	if len(pills) == 0 {
		return "", 0
	}
	// Fast path: everything fits.
	total := 0
	for _, w := range widths {
		total += w
	}
	if total <= width {
		return strings.Join(pills, ""), total
	}

	// Reserve space for a trailing "… N" indicator so overflow is explicit.
	indicatorStyle := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorDimFg).
		Italic(true).
		Padding(0, 2)

	// Walk from the start, including as many pills as fit (always including
	// the active chip). If the active chip comes late and gets cut, restart
	// from the active position.
	fit := func(start int) (int, int, int) {
		used := 0
		end := start
		for end < len(pills) {
			remaining := len(pills) - end - 1
			indicator := ""
			if remaining > 0 || start > 0 {
				indicator = indicatorStyle.Render(fmt.Sprintf("… %d more", remaining+start))
			}
			if used+widths[end]+lipgloss.Width(indicator) > width {
				break
			}
			used += widths[end]
			end++
		}
		return start, end, used
	}

	start, end, used := fit(0)
	if active >= end {
		start, end, used = fit(active)
	}

	parts := make([]string, 0, end-start+1)
	parts = append(parts, pills[start:end]...)
	hidden := (len(pills) - end) + start
	if hidden > 0 {
		ind := indicatorStyle.Render(fmt.Sprintf("… %d more", hidden))
		parts = append(parts, ind)
		used += lipgloss.Width(ind)
	}
	return strings.Join(parts, ""), used
}

// renderBody dispatches to the per-tab renderer for the active tab, padding
// the output to exactly bodyH lines of ColorSurface.
func (sv *statusView) renderBody(innerW, bodyH int) string {
	var content string
	if sv.active < 0 || sv.active >= len(sv.tabs) {
		content = ""
	} else if !sv.loaded[sv.active] {
		content = sv.renderLoading(innerW)
	} else {
		d := sv.data[sv.active]
		switch d.kind {
		case tabKindGeneral:
			if d.general != nil {
				content = sv.renderGeneral(d.general, innerW)
			}
		case tabKindAccount:
			if d.account != nil {
				content = sv.renderAccount(d.account, innerW)
			}
		}
	}
	return padBodyToHeight(content, innerW, bodyH)
}

// padBodyToHeight ensures the body occupies exactly h lines, every one
// painted edge-to-edge on ColorSurface so there are no uncolored gaps.
func padBodyToHeight(content string, w, h int) string {
	lines := strings.Split(content, "\n")
	// Pad every existing line to full width.
	for i, l := range lines {
		if gap := w - lipgloss.Width(l); gap > 0 {
			lines[i] = l + statusBody.Render(strings.Repeat(" ", gap))
		}
	}
	// Add filler rows below.
	for len(lines) < h {
		lines = append(lines, statusBody.Render(strings.Repeat(" ", w)))
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func (sv *statusView) renderLoading(innerW int) string {
	glyphs := []string{"⣾", "⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽"}
	g := glyphs[sv.spinner%len(glyphs)]
	msg := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorPrimaryHi).
		Bold(true).
		Render(g + "  loading…")
	// Vertically breathe the message a few rows down from the separator.
	pad := statusBody.Render(strings.Repeat(" ", innerW))
	return pad + "\n" + statusBody.Render("  ") + msg
}

// -- General tab -------------------------------------------------------------

func (sv *statusView) renderGeneral(g *generalData, innerW int) string {
	var b strings.Builder

	// Mode / version banner.
	b.WriteString(renderSection(innerW, "", "Process"))
	b.WriteString("\n")

	modeChip := statusChipAccent.Render(strings.ToUpper(g.mode))
	if g.modeAccent {
		modeChip = statusChipPrimary.Render(strings.ToUpper(g.mode))
	}
	ver := statusValueStrong.Render(g.versionStr)
	b.WriteString(renderRow(innerW,
		"Mode", "", modeChip+statusBody.Render(" ")+statusLabel.Render("v")+ver,
	))
	b.WriteString("\n")

	if g.modeAccent && g.socketPath != "" {
		b.WriteString(renderKV(innerW, "Socket", g.socketPath, statusValue))
		b.WriteString("\n")
	}
	b.WriteString(renderKV(innerW, "Log level", strOrDefault(g.logLevel, "info"), statusValueStrong))
	b.WriteString("\n")
	b.WriteString(renderKV(innerW, "Accounts", fmt.Sprintf("%d configured", g.accountCount), statusValue))
	b.WriteString("\n")

	b.WriteString(blank(innerW))
	b.WriteString("\n")

	// Paths section.
	b.WriteString(renderSection(innerW, "", "Paths"))
	b.WriteString("\n")
	b.WriteString(renderKV(innerW, "Database", strOrDefault(g.dbPath, "(unavailable)"), statusValue))
	b.WriteString("\n")
	b.WriteString(renderKV(innerW, "Config", strOrDefault(g.cfgPath, "(unavailable)"), statusValue))
	b.WriteString("\n")
	b.WriteString(renderKV(innerW, "pmx log", strOrDefault(g.logPath, "(unavailable)"), statusValue))
	b.WriteString("\n")
	if g.modeAccent {
		b.WriteString(renderKV(innerW, "pmxd log", strOrDefault(g.pmxdLogPath, "(unavailable)"), statusValue))
		b.WriteString("\n")
	}

	b.WriteString(blank(innerW))
	b.WriteString("\n")

	// Sync intervals.
	b.WriteString(renderSection(innerW, "", "Sync intervals"))
	b.WriteString("\n")
	b.WriteString(renderTwoCol(innerW,
		"Poll", fmtDur(g.pollInterval),
		"Sweep", fmtDur(g.sweepInterval),
	))
	b.WriteString("\n")
	b.WriteString(renderTwoCol(innerW,
		"Send", fmtDur(g.sendInterval),
		"Max attempts", fmt.Sprintf("%d", g.sendMaxAttempts),
	))
	b.WriteString("\n")

	b.WriteString(blank(innerW))
	b.WriteString("\n")

	// Daemon section.
	b.WriteString(renderSection(innerW, "", "Daemon"))
	b.WriteString("\n")
	b.WriteString(renderKV(innerW, "Check every", fmtDur(g.daemonCheckInterval), statusValue))
	b.WriteString("\n")

	var idleTxt string
	switch {
	case g.daemonIDLE == nil:
		idleTxt = "inherit (per-account)"
	case *g.daemonIDLE:
		idleTxt = "forced on"
	default:
		idleTxt = "forced off"
	}
	b.WriteString(renderKV(innerW, "IDLE (global)", idleTxt, statusValue))
	b.WriteString("\n")

	notifChip := statusChipMuted.Render(" OFF ")
	if g.daemonNotifEnabled {
		notifChip = statusChipAccent.Render(" ON  ")
	}
	b.WriteString(renderRow(innerW, "Notifications", "", notifChip))
	b.WriteString("\n")

	folders := strings.Join(g.daemonNotifFolders, ", ")
	if folders == "" {
		folders = "(all)"
	}
	b.WriteString(renderKV(innerW, "Notify folders", folders, statusValueMuted))
	b.WriteString("\n")

	return b.String()
}

// -- Account tab -------------------------------------------------------------

func (sv *statusView) renderAccount(ad *accountData, innerW int) string {
	var b strings.Builder

	// Pull live watcher snapshot: prefer freshly-cached state over the
	// payload baked into ad (the fetch cmd ran before some events may have
	// arrived). Fall back to a direct StatusOps query when the account isn't
	// in the cache yet.
	w := ad.watcher
	if cached, ok := sv.watcherCache[ad.acct.Name]; ok {
		w = cached
	} else if sv.status != nil {
		w = sv.status.WatcherState(ad.acct.Name)
	}

	if ad.errMsg != "" {
		b.WriteString(renderSection(innerW, "", "Account"))
		b.WriteString("\n")
		b.WriteString(statusBody.Render("  "))
		b.WriteString(statusCalloutDanger.Render(ad.errMsg))
		b.WriteString("\n")
	}

	// Identity -------------------------------------------------------------
	b.WriteString(renderSection(innerW, "", "Identity & Connection"))
	b.WriteString("\n")

	colW := (innerW - 4) / 2
	if colW < 20 {
		colW = innerW
	}

	identity := renderIdentityBlock(ad.acct, colW)
	connections := renderConnectionBlock(ad.acct, colW)

	// Lay them side-by-side when there's room; otherwise stack vertically.
	if colW < innerW {
		gapW := innerW - colW - colW
		rows := lipgloss.Height(identity)
		if h := lipgloss.Height(connections); h > rows {
			rows = h
		}
		gap := statusBody.Width(gapW).Height(rows).Render("")
		block := lipgloss.JoinHorizontal(lipgloss.Top,
			identity,
			gap,
			connections,
		)
		b.WriteString(block)
	} else {
		b.WriteString(identity)
		b.WriteString("\n")
		b.WriteString(blank(innerW))
		b.WriteString("\n")
		b.WriteString(connections)
	}
	b.WriteString("\n")

	// Auth
	b.WriteString(blank(innerW))
	b.WriteString("\n")
	authKind := strOrDefault(ad.acct.Auth, "password")
	authDetail := ""
	switch strings.ToLower(authKind) {
	case "oauth", "oauth2":
		authKind = "OAuth2"
		if ad.acct.OAuthTokenRef != "" {
			authDetail = "keyring: " + ad.acct.OAuthTokenRef
		}
	default:
		authKind = capitalize(authKind)
		if ad.acct.PasswordCommand != "" {
			authDetail = "password_command: set"
		} else {
			authDetail = "password_command: unset"
		}
	}
	authChip := statusChipMuted.Render(" " + authKind + " ")
	b.WriteString(renderRow(innerW, "Auth", "",
		authChip+statusBody.Render("  ")+statusLabel.Render(authDetail)))
	b.WriteString("\n")

	// Special folders
	b.WriteString(blank(innerW))
	b.WriteString("\n")
	b.WriteString(renderSection(innerW, "", "Special folders"))
	b.WriteString("\n")
	b.WriteString(renderTwoCol(innerW,
		"Sent", strOrDefault(ad.acct.SentMailbox, "(default)"),
		"Drafts", strOrDefault(ad.acct.DraftsMailbox, "(default)"),
	))
	b.WriteString("\n")
	b.WriteString(renderTwoCol(innerW,
		"Trash", strOrDefault(ad.acct.TrashMailbox, "(default)"),
		"Archive", strOrDefault(ad.acct.ArchiveMailbox, "(default)"),
	))
	b.WriteString("\n")
	b.WriteString(renderKV(innerW, "Junk", strOrDefault(ad.acct.JunkMailbox, "(default)"), statusValue))
	b.WriteString("\n")

	// Live watcher ---------------------------------------------------------
	b.WriteString(blank(innerW))
	b.WriteString("\n")
	b.WriteString(renderSection(innerW, "", "Live watcher"))
	b.WriteString("\n")
	b.WriteString(renderWatcherBlock(w, ad.acct, innerW))
	b.WriteString("\n")

	// Folders table --------------------------------------------------------
	if ad.storeAvailable {
		b.WriteString(blank(innerW))
		b.WriteString("\n")
		b.WriteString(renderSection(innerW, "", fmt.Sprintf("Folders (%d)", len(ad.folders))))
		b.WriteString("\n")
		b.WriteString(renderFolderTable(ad.folders, innerW))
		b.WriteString("\n")

		b.WriteString(blank(innerW))
		b.WriteString("\n")
		var outboxChip string
		switch {
		case ad.outboxPending == 0:
			outboxChip = statusChipMuted.Render(" 0 pending ")
		case ad.outboxPending < 5:
			outboxChip = statusChipAccent.Render(fmt.Sprintf(" %d pending ", ad.outboxPending))
		default:
			outboxChip = statusChipWarn.Render(fmt.Sprintf(" %d pending ", ad.outboxPending))
		}
		b.WriteString(renderRow(innerW, "Outbox", "", outboxChip))
		b.WriteString("\n")
	}

	return b.String()
}

// renderIdentityBlock paints the name / full-name / email trio as its own
// labeled sub-block, suitable for a column half.
func renderIdentityBlock(a config.Account, w int) string {
	var b strings.Builder
	b.WriteString(renderKV(w, "Name", strOrDefault(a.Name, "—"), statusValueStrong))
	b.WriteString("\n")
	b.WriteString(renderKV(w, "Display", strOrDefault(a.FullName, "—"), statusValue))
	b.WriteString("\n")
	b.WriteString(renderKV(w, "Email", strOrDefault(a.Email, "—"), statusValue))
	return b.String()
}

func renderConnectionBlock(a config.Account, w int) string {
	imap := fmt.Sprintf("%s:%d", strOrDefault(a.IMAPHost, "—"), a.IMAPPort)
	imapFlags := []string{}
	if a.IMAPTLS {
		imapFlags = append(imapFlags, "TLS")
	}
	if a.IMAPTLSSkipVerify {
		imapFlags = append(imapFlags, "skip-verify")
	}
	if len(imapFlags) > 0 {
		imap += "  " + strings.Join(imapFlags, " · ")
	}

	smtp := fmt.Sprintf("%s:%d", strOrDefault(a.SMTPHost, "—"), a.SMTPPort)
	smtpFlags := []string{}
	if a.SMTPTLS {
		smtpFlags = append(smtpFlags, "TLS")
	}
	if a.SMTPStartTLS {
		smtpFlags = append(smtpFlags, "STARTTLS")
	}
	if a.SMTPTLSSkipVerify {
		smtpFlags = append(smtpFlags, "skip-verify")
	}
	if len(smtpFlags) > 0 {
		smtp += "  " + strings.Join(smtpFlags, " · ")
	}

	var idleTxt string
	switch {
	case a.IDLEEnabled == nil:
		idleTxt = "inherit"
	case *a.IDLEEnabled:
		idleTxt = "enabled"
	default:
		idleTxt = "disabled"
	}

	var b strings.Builder
	b.WriteString(renderKV(w, "IMAP", imap, statusValue))
	b.WriteString("\n")
	b.WriteString(renderKV(w, "SMTP", smtp, statusValue))
	b.WriteString("\n")
	b.WriteString(renderKV(w, "IDLE", idleTxt, statusValue))
	return b.String()
}

// renderWatcherBlock paints the live-state chip + folder + last-event line,
// then an error callout when LastError is non-empty.
func renderWatcherBlock(w mailsync.WatcherState, acct config.Account, width int) string {
	var chip string
	switch {
	case !w.Running:
		chip = statusChipMuted.Render(" NOT STARTED ")
	case w.LastError != "":
		chip = statusChipDanger.Render(" DISCONNECTED ")
	case w.IDLE:
		chip = statusChipAccent.Render(" IDLE ACTIVE ")
	default:
		chip = statusChipWarn.Render(" POLLING ")
	}

	folder := strOrDefault(w.Folder, "—")
	lastEvent := "—"
	if !w.LastEvent.IsZero() {
		lastEvent = relTime(w.LastEvent)
	}

	var b strings.Builder
	line1 := statusBody.Render("  ") + chip +
		statusBody.Render("   ") +
		statusLabel.Render("folder ") +
		statusValueStrong.Render(folder) +
		statusBody.Render("   ") +
		statusLabel.Render("last event ") +
		statusValue.Render(lastEvent)
	b.WriteString(line1)
	b.WriteString("\n")

	// Last error callout
	errLine := statusBody.Render("  ") +
		statusLabel.Render("error ") +
		statusValueMuted.Render("none")
	if w.LastError != "" {
		errLine = statusBody.Render("  ") +
			statusLabel.Render("error ") +
			statusCalloutDanger.Render(w.LastError)
	}
	b.WriteString(errLine)
	return b.String()
}

// renderFolderTable builds the per-account mini-table of folder stats.
func renderFolderTable(folders []folderStat, width int) string {
	if len(folders) == 0 {
		return statusBody.Render("  ") + statusValueMuted.Render("no folders synced yet")
	}

	// Fixed column widths (role + total + unread + lastSync) with the name
	// column soaking the remainder.
	const (
		roleW     = 10
		totalW    = 7
		unreadW   = 8
		lastSyncW = 12
		gap       = 2
	)
	// Left margin of 2 for the body gutter.
	nameW := width - 2 - roleW - totalW - unreadW - lastSyncW - 4*gap
	if nameW < 10 {
		nameW = 10
	}

	// Header.
	header := statusBody.Render("  ") +
		statusTableHeader.Render(statusPadRight("NAME", nameW)) +
		statusBody.Render(strings.Repeat(" ", gap)) +
		statusTableHeader.Render(statusPadRight("ROLE", roleW)) +
		statusBody.Render(strings.Repeat(" ", gap)) +
		statusTableHeader.Render(statusPadLeft("TOTAL", totalW)) +
		statusBody.Render(strings.Repeat(" ", gap)) +
		statusTableHeader.Render(statusPadLeft("UNREAD", unreadW)) +
		statusBody.Render(strings.Repeat(" ", gap)) +
		statusTableHeader.Render(statusPadRight("LAST SYNC", lastSyncW))

	rows := []string{header}

	// Cap at a reasonable number of rows so the table doesn't push the
	// Outbox chip off-screen on dense accounts. "+N more" chip at the end.
	const maxRows = 10
	display := folders
	hidden := 0
	if len(display) > maxRows {
		hidden = len(display) - maxRows
		display = display[:maxRows]
	}

	for _, f := range display {
		roleTxt := string(f.role)
		if roleTxt == "" {
			roleTxt = "—"
		}
		roleStyle := statusLabel
		switch f.role {
		case mail.RoleInbox, mail.RoleSent, mail.RoleDrafts:
			roleStyle = statusValue
		}

		unreadStyle := statusLabel
		if f.unread > 0 {
			unreadStyle = lipgloss.NewStyle().
				Background(styles.ColorSurface).
				Foreground(styles.ColorAccent).
				Bold(true)
		}

		lastSync := "—"
		if !f.lastSync.IsZero() {
			lastSync = relTime(f.lastSync)
		}

		row := statusBody.Render("  ") +
			statusValue.Render(statusPadRight(statusTruncRunes(f.name, nameW), nameW)) +
			statusBody.Render(strings.Repeat(" ", gap)) +
			roleStyle.Render(statusPadRight(statusTruncRunes(roleTxt, roleW), roleW)) +
			statusBody.Render(strings.Repeat(" ", gap)) +
			statusValue.Render(statusPadLeft(fmt.Sprintf("%d", f.total), totalW)) +
			statusBody.Render(strings.Repeat(" ", gap)) +
			unreadStyle.Render(statusPadLeft(fmt.Sprintf("%d", f.unread), unreadW)) +
			statusBody.Render(strings.Repeat(" ", gap)) +
			statusLabel.Render(statusPadRight(lastSync, lastSyncW))
		rows = append(rows, row)
	}

	if hidden > 0 {
		rows = append(rows, statusBody.Render("  ")+statusValueMuted.Render(fmt.Sprintf("… %d more folder(s) not shown", hidden)))
	}

	return strings.Join(rows, "\n")
}

// -- Section / row helpers ---------------------------------------------------

// renderSection paints a section header bar with a glyph + uppercased label.
// Painted on ColorOverlay so sections visually float on the ColorSurface
// body — the shift in luminance reads as a stacked card.
func renderSection(width int, glyph, label string) string {
	left := statusBody.Render("  ")
	g := ""
	if glyph != "" {
		g = statusSectionHeaderGlyph.Render(glyph + " ")
	}
	title := statusSectionHeader.Render(strings.ToUpper(label))
	bar := left + g + title
	gap := width - lipgloss.Width(bar)
	if gap < 0 {
		gap = 0
	}
	return bar + statusSectionHeaderFiller.Render(strings.Repeat(" ", gap))
}

// renderRow paints a two-column row: a left label (fixed width) and a free
// right value. Trailing slack fills with ColorSurface.
func renderRow(width int, label, _ string, value string) string {
	const labelW = 16
	left := statusBody.Render("  ") +
		statusLabel.Render(statusPadRight(label, labelW)) +
		statusBody.Render("  ")
	row := left + value
	gap := width - lipgloss.Width(row)
	if gap < 0 {
		gap = 0
	}
	return row + statusBody.Render(strings.Repeat(" ", gap))
}

// renderKV is renderRow with a styled value cell.
func renderKV(width int, label, value string, valStyle lipgloss.Style) string {
	return renderRow(width, label, "", valStyle.Render(value))
}

// renderTwoCol lays out two label/value pairs side-by-side.
func renderTwoCol(width int, l1, v1, l2, v2 string) string {
	halfW := width / 2
	left := renderKV(halfW, l1, v1, statusValue)
	right := renderKV(width-halfW, l2, v2, statusValue)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// blank returns a blank row filled with the body background so stacked
// sections don't leak the panel's chrome color through the gap.
func blank(width int) string {
	return statusBody.Render(strings.Repeat(" ", width))
}

// -- Formatting primitives ---------------------------------------------------

// capitalize upper-cases the first rune of s and leaves the rest alone.
// Used for display-only labels; strings.Title is deprecated.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func strOrDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func fmtDur(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	// Round to the nearest second for display stability.
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		m := int(d / time.Minute)
		s := int((d % time.Minute) / time.Second)
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		h := int(d / time.Hour)
		m := int((d % time.Hour) / time.Minute)
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
}

func statusPadRight(s string, w int) string {
	s = statusTruncRunes(s, w)
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func statusPadLeft(s string, w int) string {
	s = statusTruncRunes(s, w)
	if gap := w - lipgloss.Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

// truncRunes shortens s to w display cells by rune count, inserting an
// ellipsis when it has to cut. Safe for the simple ASCII text used across
// the status screen.
func statusTruncRunes(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	runes := []rune(s)
	if w == 1 {
		return "…"
	}
	// Trim from the right until the ellipsis + remainder fits.
	for i := len(runes) - 1; i >= 0; i-- {
		cand := string(runes[:i]) + "…"
		if lipgloss.Width(cand) <= w {
			return cand
		}
	}
	return "…"
}

// Compile-time check that *statusView satisfies View.
var _ View = (*statusView)(nil)
