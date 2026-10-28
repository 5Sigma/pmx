package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/compose"
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/ui/keymap"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

// Column header + count line heights above the list proper.
const (
	listHeaderHeight = 1 // folder title + counts
	listColHeight    = 1 // column labels
	listChromeHeight = listHeaderHeight + listColHeight
)

type mailList struct {
	list       list.Model
	store      mail.MessageStore
	cfg        *config.Config
	sender     *mailsend.Engine
	folderID   int64
	folderName string
	folderRole mail.FolderRole
	width      int
	height     int
}

// messagesLoadedMsg is dispatched asynchronously once the store has returned
// a batch of messages for the active folder. The payload replaces the current
// list items. An empty slice renders an empty list.
type messagesLoadedMsg []*mail.Message

func newMailList(store mail.MessageStore, cfg *config.Config, sender *mailsend.Engine, folderID int64, folderName string, folderRole mail.FolderRole) mailList {
	items := []list.Item{}

	l := list.New(items, mailDelegate{}, 0, 0)
	if folderName == "" {
		folderName = "Inbox"
	}
	l.Title = folderName
	l.SetShowStatusBar(false)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	// Disable the list's built-in filter UI. bubbles v0.21+ renders the title
	// area whenever showFilter||filteringEnabled, producing a blank row even
	// when not filtering. We draw the filter input ourselves in
	// renderListHeader so filtering still works visually.
	l.SetShowFilter(false)
	l.FilterInput.Prompt = "/ "
	l.Styles.Title = styles.ListTitle

	return mailList{
		list:       l,
		store:      store,
		cfg:        cfg,
		sender:     sender,
		folderID:   folderID,
		folderName: folderName,
		folderRole: folderRole,
	}
}

func (ml mailList) Init() tea.Cmd {
	if ml.store == nil {
		return nil
	}
	store := ml.store
	folderID := ml.folderID
	return func() tea.Msg {
		msgs, err := store.ListFolder(folderID, 200, 0)
		if err != nil {
			return nil
		}
		return messagesLoadedMsg(msgs)
	}
}

func (ml mailList) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case messagesLoadedMsg:
		items := make([]list.Item, len(msg))
		for i, m := range msg {
			items[i] = messageToItem(m)
		}
		return ml, ml.list.SetItems(items)
	case tea.KeyMsg:
		switch {
		case msg.String() == "q":
			if ml.list.FilterState() == list.Unfiltered {
				return ml, tea.Quit
			}
		case msg.String() == "enter":
			if ml.list.FilterState() != list.Filtering {
				if item, ok := ml.list.SelectedItem().(mailItem); ok {
					return ml, Push(newMailRead(item, ml.store, ml.cfg, ml.sender, ml.cfg.Keymap()))
				}
			}
		case msg.String() == "f":
			if ml.list.FilterState() != list.Filtering {
				return ml, func() tea.Msg { return openFolderDrawerMsg{} }
			}
		case msg.String() == "esc":
			if ml.list.FilterState() != list.Filtering {
				return ml, nil
			}
		}
	}

	var cmd tea.Cmd
	ml.list, cmd = ml.list.Update(msg)
	return ml, cmd
}

// renderListHeader paints the folder title line and the column label line
// that sit directly above the scrollable rows.
func (ml mailList) renderListHeader() string {
	w := ml.width
	if w <= 0 {
		return ""
	}

	// Count unread.
	var unread, total int
	for _, it := range ml.list.Items() {
		if mi, ok := it.(mailItem); ok {
			total++
			if mi.unread {
				unread++
			}
		}
	}

	// Row 1: folder title + counts, with a trailing filler so the surface
	// paints across the full width.
	icon := styles.FolderIconDefault
	if ic, ok := styles.FolderIcons[ml.folderRole]; ok {
		icon = ic
	}
	folderIcon := styles.ListFolder.Render(icon + " ")
	name := ml.folderName
	if name == "" {
		name = "Inbox"
	}
	folder := styles.ListFolder.Render(name)
	countBlock := styles.ListCount.Render(fmt.Sprintf(" %d messages", total))
	if unread > 0 {
		countBlock += styles.ListCount.Render(" · ") +
			styles.ListCountAccent.Render(fmt.Sprintf("%d unread", unread))
	}

	// Filter chip / input: show the live textinput while typing, a static chip
	// when a filter is applied. We render the filter input here because the
	// bubbles list's built-in filter title row is disabled (SetShowFilter(false)).
	filterChip := ""
	switch ml.list.FilterState() {
	case list.Filtering:
		filterChip = styles.ListCount.Render(" · ") + ml.list.FilterInput.View()
	case list.FilterApplied:
		fv := ml.list.FilterValue()
		if fv == "" {
			fv = "…"
		}
		filterChip = styles.ListCount.Render(" · ") + styles.StatusKey.Render(" / "+fv+" ")
	}

	surface := lipgloss.NewStyle().Background(styles.ColorBg)
	left := surface.Render(" ") + folderIcon + folder + countBlock + filterChip
	gap := w - lipgloss.Width(left)
	if gap < 0 {
		gap = 0
	}
	row1 := left + surface.Render(strings.Repeat(" ", gap))

	// Row 2: column labels.
	// Layout mirrors the delegate: 2 col sel-bar, 2 col unread-dot, inner.
	const prefixWidth = 4
	inner := w - prefixWidth
	if inner < 10 {
		inner = 10
	}
	dateW := 10
	fromW := 24
	if w < 100 {
		fromW = 18
	}
	if w < 80 {
		fromW = 14
	}

	labelFrom := styles.ListColHeader.Render("FROM")
	labelSubj := styles.ListColHeader.Render("SUBJECT")
	labelDate := styles.ListColHeader.Render("DATE")

	// Compose: [prefix][FROM padded to fromW][SUBJECT fills remainder][DATE right]
	subjW := inner - fromW - dateW - 2
	if subjW < 4 {
		subjW = 4
	}

	pad := func(n int) string { return surface.Render(strings.Repeat(" ", n)) }
	fromCol := labelFrom + pad(maxInt(0, fromW-lipgloss.Width(labelFrom)))
	subjCol := labelSubj + pad(maxInt(0, subjW-lipgloss.Width(labelSubj)))
	dateCol := pad(maxInt(0, dateW-lipgloss.Width(labelDate))) + labelDate

	row2 := pad(prefixWidth) + fromCol + pad(1) + subjCol + pad(1) + dateCol
	// Pad to full width so the background spans edge-to-edge.
	if extra := w - lipgloss.Width(row2); extra > 0 {
		row2 += pad(extra)
	}

	return lipgloss.JoinVertical(lipgloss.Left, row1, row2)
}

func (ml mailList) View() string {
	header := ml.renderListHeader()
	return lipgloss.JoinVertical(lipgloss.Left, header, ml.list.View())
}

func (ml mailList) SetSize(width, height int) View {
	ml.width = width
	ml.height = height
	listH := height - listChromeHeight
	if listH < 1 {
		listH = 1
	}
	ml.list.SetSize(width, listH)
	return ml
}

func (ml mailList) Title() string {
	if ml.folderName == "" {
		return "Inbox"
	}
	return ml.folderName
}

func (ml mailList) StatusKeys() string {
	return keyHintsFromKeyMap(ml.KeyMap())
}

// KeyMap declares the mail-list bindings. Primary keys surface in the hint
// bar and are handled by Update() natively; the Leader tree drives the
// whichkey popup.
//
// Note that leaf Actions here return keymap.Result values — they do NOT
// execute tea.Cmds directly. The root model (app.go) translates those
// results into pushes / pops / quits. This keeps the keymap package free
// of bubbletea deps.
func (ml mailList) KeyMap() keymap.KeyMap {
	// A stub action — most leaves are not yet wired to real logic.
	stub := func(note string) keymap.Action {
		return func() keymap.Result { return keymap.Result{Note: note} }
	}

	return keymap.KeyMap{
		Primary: []keymap.Binding{
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter", DisplayKey: "↵", Desc: "open"},
			{Key: "f", Desc: "folders"},
			{Key: "/", Desc: "filter"},
			{Key: "q", Desc: "quit"},
		},
		Leader: func() []keymap.Entry {
			entries := []keymap.Entry{
				{
					Key:  "m",
					Desc: "mail",
					Children: []keymap.Entry{
						{Key: "a", Desc: "archive", Accent: true, Action: func() keymap.Result {
							item, ok := ml.list.SelectedItem().(mailItem)
							if !ok || item.uid == 0 {
								return keymap.Result{Note: "no selection"}
							}
							return keymap.Result{Message: archiveMsg{
								folderID: item.folderID,
								uid:      item.uid,
							}}
						}},
						{Key: "d", Desc: "delete", Danger: true, Action: func() keymap.Result {
							item, ok := ml.list.SelectedItem().(mailItem)
							if !ok || item.uid == 0 {
								return keymap.Result{Note: "no selection"}
							}
							return keymap.Result{Message: deleteMsg{
								folderID: item.folderID,
								uid:      item.uid,
							}}
						}},
						{Key: "f", Desc: "flag", Action: func() keymap.Result {
							item, ok := ml.list.SelectedItem().(mailItem)
							if !ok || item.uid == 0 {
								return keymap.Result{Note: "no selection"}
							}
							return keymap.Result{Message: markFlaggedMsg{
								folderID: item.folderID,
								uid:      item.uid,
								flags:    item.flags,
								flagged:  item.flags&mail.FlagFlagged == 0,
							}}
						}},
						{Key: "u", Desc: "mark unread", Action: func() keymap.Result {
							item, ok := ml.list.SelectedItem().(mailItem)
							if !ok || item.uid == 0 {
								return keymap.Result{Note: "no selection"}
							}
							return keymap.Result{Message: markSeenMsg{
								folderID: item.folderID,
								uid:      item.uid,
								flags:    item.flags,
								seen:     false,
							}}
						}},
					},
				},
				{
					Key:  "c",
					Desc: "compose",
					Children: []keymap.Entry{
						{Key: "c", Desc: "new message", Accent: true, Action: func() keymap.Result {
							return keymap.Result{Push: newComposeView(ml.cfg, ml.store, ml.sender)}
						}},
						{Key: "r", Desc: "reply", Action: func() keymap.Result {
							item, ok := ml.list.SelectedItem().(mailItem)
							if !ok || item.uid == 0 {
								return keymap.Result{Note: "no selection"}
							}
							src, err := ml.store.ByUID(item.folderID, item.uid)
							if err != nil || src == nil {
								return keymap.Result{Note: "could not load message"}
							}
							body, _ := buildBodyMD(src.ID, ml.store)
							acct := ml.cfg.AccountForMessage(src)
							seed := compose.BuildReply(src, acct, body)
							return keymap.Result{Push: newComposeViewSeeded(ml.cfg, ml.store, ml.sender, seed)}
						}},
						{Key: "R", Desc: "reply all", Action: func() keymap.Result {
							item, ok := ml.list.SelectedItem().(mailItem)
							if !ok || item.uid == 0 {
								return keymap.Result{Note: "no selection"}
							}
							src, err := ml.store.ByUID(item.folderID, item.uid)
							if err != nil || src == nil {
								return keymap.Result{Note: "could not load message"}
							}
							body, _ := buildBodyMD(src.ID, ml.store)
							acct := ml.cfg.AccountForMessage(src)
							seed := compose.BuildReplyAll(src, acct, body)
							return keymap.Result{Push: newComposeViewSeeded(ml.cfg, ml.store, ml.sender, seed)}
						}},
						{Key: "f", Desc: "forward", Action: func() keymap.Result {
							item, ok := ml.list.SelectedItem().(mailItem)
							if !ok || item.uid == 0 {
								return keymap.Result{Note: "no selection"}
							}
							src, err := ml.store.ByUID(item.folderID, item.uid)
							if err != nil || src == nil {
								return keymap.Result{Note: "could not load message"}
							}
							if atts, err := ml.store.ListAttachments(src.ID); err == nil {
								src.Attachments = atts
							}
							body, _ := buildBodyMD(src.ID, ml.store)
							acct := ml.cfg.AccountForMessage(src)
							seed := compose.BuildForward(src, acct, body, func(a mail.Attachment) string {
								return ml.store.AttachmentPath(a)
							})
							return keymap.Result{Push: newComposeViewSeeded(ml.cfg, ml.store, ml.sender, seed)}
						}},
					},
				},
				{
					Key:  "g",
					Desc: "go to",
					Children: []keymap.Entry{
						{Key: "g", Desc: "top", Action: stub("go top")},
						{Key: "G", Desc: "bottom", Action: stub("go bottom")},
						{Key: "n", Desc: "next unread", Accent: true, Action: stub("next unread")},
					},
				},
				{
					Key: "q", Desc: "quit", Danger: true,
					Action: func() keymap.Result { return keymap.Result{Quit: true} },
				},
				{
					Key: "r", Desc: "refresh / sync", Accent: true,
					Action: func() keymap.Result { return keymap.Result{Message: refreshMsg{}} },
				},
			}
			if ml.cfg != nil && len(ml.cfg.Accounts) > 1 {
				entries = append(entries, keymap.Entry{
					Key:  "a",
					Desc: "accounts",
					Children: []keymap.Entry{
						{Key: "a", Desc: "open account drawer", Accent: true, Action: func() keymap.Result {
							return keymap.Result{Message: openAccountDrawerMsg{}}
						}},
						{Key: "n", Desc: "next account", Action: func() keymap.Result {
							return keymap.Result{Message: cycleAccountMsg{delta: +1}}
						}},
						{Key: "p", Desc: "prev account", Action: func() keymap.Result {
							return keymap.Result{Message: cycleAccountMsg{delta: -1}}
						}},
					},
				})
			}
			return entries
		}(),
	}
}

// mailItem implements list.Item.
type mailItem struct {
	messageID  int64     // 0 for fixture items; >0 for store-backed items
	folderID   int64     // IMAP folder PK
	uid        uint32    // IMAP UID
	flags      mail.Flag // current full flag bitmask
	from       string
	email      string
	subject    string
	preview    string
	date       string    // terse formatted date for the list column
	receivedAt time.Time // raw timestamp for detail views
	tags       []string
	unread     bool
	hasAtt     bool
	flagged    bool
}

// messageToItem adapts a persisted mail.Message into the UI's mailItem shape.
// Formatting rules (date column in particular) match what the delegate expects
// so visual rendering stays identical regardless of the data source.
func messageToItem(m *mail.Message) mailItem {
	from := m.From.Name
	if from == "" {
		from = m.From.Email
	}
	d := m.DateReceived
	if d.IsZero() {
		d = m.DateSent
	}
	date := ""
	if !d.IsZero() {
		now := time.Now()
		switch {
		case d.Year() == now.Year() && d.YearDay() == now.YearDay():
			date = d.Format("15:04")
		case d.Year() == now.Year():
			date = d.Format("Jan 2")
		default:
			date = d.Format("Jan 2 2006")
		}
	}
	return mailItem{
		messageID:  m.ID,
		folderID:   m.FolderID,
		uid:        m.UID,
		flags:      m.Flags,
		from:       from,
		email:      m.From.Email,
		subject:    m.Subject,
		preview:    truncate(strings.Join(strings.Fields(m.BodyText), " "), 120),
		date:       date,
		receivedAt: d,
		unread:     !m.IsRead(),
		hasAtt:     m.HasAttach,
		flagged:    m.IsFlagged(),
	}
}

func (i mailItem) Title() string       { return i.subject }
func (i mailItem) Description() string { return i.from }
func (i mailItem) FilterValue() string { return i.subject + " " + i.from + " " + i.email }

// mailDelegate renders 3-line email items. The entire row paints a background
// (surface for idle, raised for selected) so the list reads as a continuous
// surface rather than isolated text blocks.
type mailDelegate struct{}

func (d mailDelegate) Height() int                             { return 3 }
func (d mailDelegate) Spacing() int                            { return 0 }
func (d mailDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d mailDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	mi, ok := item.(mailItem)
	if !ok {
		return
	}

	selected := index == m.Index()
	width := m.Width()

	const prefixWidth = 4 // selBar(1) + space(1) + unreadDot(1) + space(1)
	if width <= prefixWidth+6 {
		_, _ = fmt.Fprintf(w, "%s\n%s\n%s", mi.from, mi.subject, mi.preview)
		return
	}
	inner := width - prefixWidth

	// Row background style.
	rowBg := styles.ItemBg
	if selected {
		rowBg = styles.ItemBgSelected
	}

	// Selection bar: thick bar when selected, blank gutter otherwise. The bar
	// keeps the surface background so it reads as part of the row.
	var selBar string
	if selected {
		selBar = styles.SelectionBar.Render("▌") + rowBg.Render(" ")
	} else {
		selBar = rowBg.Render("  ")
	}

	// Unread indicator — bright accent dot for unread, hollow glyph for read.
	var unreadDot string
	switch {
	case mi.unread:
		unreadDot = rowBg.Foreground(styles.ColorAccent).Bold(true).Render("●") + rowBg.Render(" ")
	default:
		unreadDot = rowBg.Foreground(styles.ColorFaint).Render("·") + rowBg.Render(" ")
	}

	prefix := selBar + unreadDot

	// Column widths for line 1 (sender + date right-aligned).
	dateW := lipgloss.Width(mi.date)
	fromMax := inner - dateW - 1 - flagsWidth(mi)
	if fromMax < 1 {
		fromMax = 1
	}

	// Sender — unread is bright + bold, read is subdued.
	fromText := truncate(mi.from, fromMax)
	var fromStr string
	if mi.unread {
		fromStr = rowBg.Foreground(styles.ColorFgStrong).Bold(true).Render(fromText)
	} else {
		fromStr = rowBg.Foreground(styles.ColorSubdued).Render(fromText)
	}

	// Flags (attachment, flagged) sit between sender and date.
	flags := renderFlags(mi, rowBg)

	// Date.
	var dateStr string
	if mi.unread {
		dateStr = rowBg.Foreground(styles.ColorPrimaryHi).Bold(true).Render(mi.date)
	} else {
		dateStr = rowBg.Foreground(styles.ColorDimFg).Render(mi.date)
	}

	gap := inner - lipgloss.Width(fromStr) - lipgloss.Width(flags) - lipgloss.Width(dateStr)
	if gap < 1 {
		gap = 1
	}
	line1 := prefix + fromStr + rowBg.Render(strings.Repeat(" ", gap)) + flags + dateStr

	// Line 2: subject + optional tag chips.
	tagStrip := renderTags(mi.tags, rowBg)
	subjMax := inner - lipgloss.Width(tagStrip) - 1
	if subjMax < 4 {
		subjMax = inner
		tagStrip = ""
	}
	var subjStr string
	if mi.unread {
		subjStr = rowBg.Foreground(styles.ColorFg).Bold(true).Render(truncate(mi.subject, subjMax))
	} else {
		subjStr = rowBg.Foreground(styles.ColorDimFg).Render(truncate(mi.subject, subjMax))
	}
	subjGap := inner - lipgloss.Width(subjStr) - lipgloss.Width(tagStrip)
	if subjGap < 0 {
		subjGap = 0
	}
	// Rebuild indentation for line 2 — selection bar + spacing (no dot).
	var line2Prefix string
	if selected {
		line2Prefix = styles.SelectionBar.Render("▌") + rowBg.Render("   ")
	} else {
		line2Prefix = rowBg.Render("    ")
	}
	line2 := line2Prefix + subjStr + rowBg.Render(strings.Repeat(" ", subjGap)) + tagStrip

	// Line 3: preview snippet, italic + faint.
	prevStr := rowBg.Foreground(styles.ColorFaint).Italic(true).Render(truncate(mi.preview, inner))
	prevGap := inner - lipgloss.Width(prevStr)
	if prevGap < 0 {
		prevGap = 0
	}
	line3 := line2Prefix + prevStr + rowBg.Render(strings.Repeat(" ", prevGap))

	// Pad all lines to the full width so the row background covers edge-to-edge.
	line1 = padRight(line1, width, rowBg)
	line2 = padRight(line2, width, rowBg)
	line3 = padRight(line3, width, rowBg)

	_, _ = fmt.Fprintf(w, "%s\n%s\n%s", line1, line2, line3)
}

// flagsWidth returns the display width of the flag glyphs that will be
// prepended to the date column, including any separators.
func flagsWidth(mi mailItem) int {
	n := 0
	if mi.hasAtt {
		n += 2 // glyph + space
	}
	if mi.flagged {
		n += 2 // glyph + space
	}
	return n
}

func renderFlags(mi mailItem, rowBg lipgloss.Style) string {
	parts := []string{}
	if mi.flagged {
		parts = append(parts, rowBg.Foreground(styles.ColorDanger).Bold(true).Render(""))
	}
	if mi.hasAtt {
		parts = append(parts, rowBg.Foreground(styles.ColorWarn).Render(""))
	}
	if len(parts) == 0 {
		return ""
	}
	return rowBg.Render(" ") + strings.Join(parts, rowBg.Render(" ")) + rowBg.Render(" ")
}

func renderTags(tags []string, rowBg lipgloss.Style) string {
	if len(tags) == 0 {
		return ""
	}
	chipStyle := rowBg.
		Foreground(styles.ColorPrimaryHi).
		Bold(true)
	parts := make([]string, 0, len(tags))
	for _, t := range tags {
		parts = append(parts, chipStyle.Render("#"+t))
	}
	return strings.Join(parts, rowBg.Render(" "))
}

func padRight(s string, width int, bg lipgloss.Style) string {
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + bg.Render(strings.Repeat(" ", pad))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
