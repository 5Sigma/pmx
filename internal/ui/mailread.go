package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"jaytaylor.com/html2text"

	"github.com/5Sigma/pmx/internal/compose"
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/ui/keymap"
	"github.com/5Sigma/pmx/internal/ui/pathpicker"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

var (
	reHTMLHead    = regexp.MustCompile(`(?is)<head[^>]*>.*?</head>`)
	reHTMLStyle   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reHTMLScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)
)

type bodyKind int

const (
	bodyKindNone bodyKind = iota
	bodyKindText
	bodyKindHTML
)

type openHTMLMsg struct{ id int64 }
type htmlOpenedMsg struct{ note string }

// bodyLoadedMsg is sent by the async body loader when conversion is done.
type bodyLoadedMsg struct {
	id          int64
	md          string // markdown source, cached for re-render on resize
	content     string // glamour-rendered ANSI, ready to display
	kind        bodyKind
	attachments []mail.Attachment
}

type mailRead struct {
	item          mailItem
	store         mail.MessageStore
	cfg           *config.Config
	sender        *mailsend.Engine
	keys          config.Keymap
	viewport      viewport.Model
	width         int
	height        int
	ready         bool
	renderer      *glamour.TermRenderer
	bodySource    string // markdown source, cached after first async load
	kind          bodyKind
	attachments   []mail.Attachment
	attachmentIdx int    // -1 when no attachment is selected
	openNote      string // shown in the attachment row after an open attempt
}

func (mr mailRead) infoBarRows() int {
	h := 5
	if len(mr.attachments) > 0 {
		h += 2
	}
	return h
}

func newMailRead(item mailItem, store mail.MessageStore, cfg *config.Config, sender *mailsend.Engine, keys config.Keymap) mailRead {
	return mailRead{item: item, store: store, cfg: cfg, sender: sender, keys: keys, attachmentIdx: -1}
}

// Init starts the async body load. SetSize is called before Init by the
// pushMsg handler in app.go, so mr.renderer is already constructed here.
func (mr mailRead) Init() tea.Cmd {
	if mr.item.messageID <= 0 || mr.store == nil {
		return nil
	}
	id := mr.item.messageID
	store := mr.store
	renderer := mr.renderer

	bodyLoad := func() tea.Msg {
		md, kind := buildBodyMD(id, store)
		content := renderMD(renderer, md)
		atts, err := store.ListAttachments(id)
		if err != nil {
			atts = nil
		}
		return bodyLoadedMsg{id: id, md: md, content: content, kind: kind, attachments: atts}
	}

	if mr.item.unread && mr.item.uid > 0 && mr.item.folderID > 0 {
		markSeen := func() tea.Msg {
			return markSeenMsg{
				folderID: mr.item.folderID,
				uid:      mr.item.uid,
				flags:    mr.item.flags,
				seen:     true,
			}
		}
		return tea.Batch(bodyLoad, markSeen)
	}
	return bodyLoad
}

func (mr mailRead) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		hasAtt := len(mr.attachments) > 0
		s := msg.String()
		switch {
		case s == "q":
			return mr, tea.Quit
		case s == "esc":
			if mr.attachmentIdx >= 0 {
				mr.attachmentIdx = -1
				return mr, nil
			}
			return mr, Pop()
		case hasAtt && s == "tab":
			mr.attachmentIdx = (mr.attachmentIdx + 1) % len(mr.attachments)
			mr.openNote = ""
			return mr, nil
		case hasAtt && s == "shift+tab":
			n := len(mr.attachments)
			if mr.attachmentIdx <= 0 {
				mr.attachmentIdx = n - 1
			} else {
				mr.attachmentIdx--
			}
			mr.openNote = ""
			return mr, nil
		case hasAtt && mr.attachmentIdx >= 0 && (s == "enter" || mr.keys.Matches("open", msg)):
			a := mr.attachments[mr.attachmentIdx]
			path := mr.store.AttachmentPath(a)
			if _, statErr := os.Stat(path); statErr != nil {
				mr.openNote = "not on disk — re-sync needed"
			} else if err := openFn(path); err != nil {
				mr.openNote = err.Error()
			} else {
				mr.openNote = ""
			}
			return mr, nil
		case hasAtt && mr.attachmentIdx >= 0 && s == "s":
			if mr.store == nil {
				return mr, nil
			}
			a := mr.attachments[mr.attachmentIdx]
			src := mr.store.AttachmentPath(a)
			filename := a.Filename
			cfg := pathpicker.OpenConfig{
				Prompt: "Save " + filename + " to…",
				Mode:   pathpicker.ModeDirs,
				OnSelect: func(dir string) tea.Msg {
					return saveAttachmentMsg{
						src: src,
						dst: filepath.Join(dir, filename),
					}
				},
			}
			return mr, func() tea.Msg { return openPathPickerMsg{cfg: cfg} }
		}
	case bodyLoadedMsg:
		if msg.id == mr.item.messageID {
			mr.bodySource = msg.md
			mr.kind = msg.kind
			mr.attachments = msg.attachments
			mr.attachmentIdx = -1
			vpH := mr.height - mr.infoBarRows()
			if vpH < 1 {
				vpH = 1
			}
			mr.viewport.Width = mr.width
			mr.viewport.Height = vpH
			mr.viewport.SetContent(msg.content)
		}
		return mr, nil
	case openHTMLMsg:
		id := msg.id
		store := mr.store
		atts := mr.attachments
		return mr, func() tea.Msg {
			return htmlOpenedMsg{note: buildAndOpenHTML(id, store, atts)}
		}
	case htmlOpenedMsg:
		mr.openNote = msg.note
		return mr, nil
	case attachmentSavedMsg:
		mr.openNote = msg.note
		return mr, nil
	}

	var cmd tea.Cmd
	mr.viewport, cmd = mr.viewport.Update(msg)
	return mr, cmd
}

func (mr mailRead) View() string {
	if !mr.ready {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Left, mr.renderInfoBar(), mr.viewport.View())
}

func (mr mailRead) renderInfoBar() string {
	w := mr.width
	if w <= 0 {
		return ""
	}
	bg := styles.InfoBar

	// ── Subject row ─────────────────────────────────────────────────────
	accent := styles.InfoAccent.Render("▎ ")
	subject := styles.InfoSubject.Render(mr.item.subject)

	// Right-side flags + tags on the subject row.
	right := ""
	if mr.item.flagged {
		right += styles.InfoAccent.Foreground(styles.ColorDanger).Render(" ") + bg.Render(" ")
	}
	if mr.item.hasAtt {
		right += styles.InfoAccent.Foreground(styles.ColorWarn).Render(" ") + bg.Render(" ")
	}
	for _, t := range mr.item.tags {
		right += bg.Foreground(styles.ColorPrimaryHi).Bold(true).Render("#"+t) + bg.Render(" ")
	}

	left := bg.Render(" ") + accent + subject
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// Truncate the subject to make room.
		room := w - lipgloss.Width(bg.Render(" ")+accent) - lipgloss.Width(right) - 1
		if room < 4 {
			room = 4
		}
		subject = styles.InfoSubject.Render(truncate(mr.item.subject, room))
		left = bg.Render(" ") + accent + subject
		gap = w - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 0 {
			gap = 0
		}
	}
	subjRow := left + bg.Render(strings.Repeat(" ", gap)) + right

	// ── Meta rows ───────────────────────────────────────────────────────
	fromLine := mr.metaRow("", "From", formatSender(mr.item))
	toLine := mr.metaRow("", "To", "user@example.com")
	dateLine := mr.metaRow("", "Date", formatDateTime(mr.item.receivedAt))

	plainRule := styles.InfoRule.Render(strings.Repeat("─", w))

	rows := []string{subjRow, fromLine, toLine, dateLine}
	if len(mr.attachments) > 0 {
		rows = append(rows, plainRule, mr.renderAttachmentsRow())
	}
	rows = append(rows, mr.renderRule())
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (mr mailRead) renderRule() string {
	w := mr.width
	chip := ""
	switch mr.kind {
	case bodyKindText:
		chip = "text/plain"
	case bodyKindHTML:
		chip = "html→text"
	}
	if chip == "" {
		return styles.InfoRule.Render(strings.Repeat("─", w))
	}
	chipRendered := styles.InfoBodyKind.Render("  " + chip + "  ")
	dashesW := w - lipgloss.Width(chipRendered)
	if dashesW < 1 {
		return styles.InfoRule.Render(strings.Repeat("─", w))
	}
	return styles.InfoRule.Render(strings.Repeat("─", dashesW)) + chipRendered
}

func (mr mailRead) renderAttachmentsRow() string {
	bg := styles.InfoBar
	w := mr.width

	icon := styles.InfoAccent.Foreground(styles.ColorWarn).Render("")
	prefix := bg.Render("  ") + icon + bg.Render(" ")
	prefixW := lipgloss.Width(prefix)
	ellipsis := styles.InfoMeta.Render("…")
	ellipsisW := lipgloss.Width(ellipsis)

	sep := "  ·  "
	sepStyled := styles.InfoMeta.Render(sep)
	sepW := lipgloss.Width(sepStyled)

	selStyle := lipgloss.NewStyle().
		Background(styles.ColorRaised).
		Foreground(styles.ColorPrimaryHi).
		Bold(true)

	var body string
	used := 0
	truncated := false

	for i, a := range mr.attachments {
		plain := a.Filename + " (" + FormatBytes(a.SizeBytes) + ")"
		partW := lipgloss.Width(plain)
		addW := partW
		if i > 0 {
			addW += sepW
		}
		if prefixW+used+addW > w {
			truncated = true
			break
		}
		if i > 0 {
			body += sepStyled
			used += sepW
		}
		if i == mr.attachmentIdx {
			body += selStyle.Render(plain)
		} else {
			body += styles.InfoValue.Render(a.Filename) + styles.InfoMeta.Render(" ("+FormatBytes(a.SizeBytes)+")")
		}
		used += partW
	}

	line := prefix + body
	if truncated {
		if lipgloss.Width(line)+ellipsisW > w {
			line = prefix + ellipsis
		} else {
			line += ellipsis
		}
	}

	if mr.openNote != "" {
		note := styles.InfoBar.Foreground(styles.ColorDanger).Render("  " + mr.openNote)
		noteW := lipgloss.Width(note)
		if lipgloss.Width(line)+noteW <= w {
			line += note
		}
	}

	if pad := w - lipgloss.Width(line); pad > 0 {
		line += bg.Render(strings.Repeat(" ", pad))
	}
	return line
}

// metaRow renders a single meta line: "  icon  Label  value" padded to width.
func (mr mailRead) metaRow(icon, label, value string) string {
	bg := styles.InfoBar
	w := mr.width

	iconPart := bg.Foreground(styles.ColorPrimaryHi).Render(icon)
	labelPart := styles.InfoLabel.Render(fmt.Sprintf("%-5s", label))
	valuePart := styles.InfoValue.Render(value)

	line := bg.Render("   ") + iconPart + bg.Render("  ") + labelPart + bg.Render("  ") + valuePart
	if pad := w - lipgloss.Width(line); pad > 0 {
		line += bg.Render(strings.Repeat(" ", pad))
	}
	return line
}

func (mr mailRead) SetSize(width, height int) View {
	mr.width = width
	mr.height = height
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width-4),
	)
	if err == nil {
		mr.renderer = r
	}
	vpH := height - mr.infoBarRows()
	if vpH < 1 {
		vpH = 1
	}
	mr.viewport = viewport.New(width, vpH)
	if mr.bodySource != "" {
		// Already loaded — re-render at new width without re-fetching.
		mr.viewport.SetContent(renderMD(mr.renderer, mr.bodySource))
	} else if mr.item.messageID > 0 && mr.store != nil {
		mr.viewport.SetContent(styles.InfoMeta.Background(styles.ColorBg).Render("  Loading…"))
	} else {
		mr.viewport.SetContent(mr.placeholderBody())
	}
	mr.ready = true
	return mr
}

// buildBodyMD fetches body text or HTML from the store and returns a markdown
// string suitable for passing to glamour. Runs in a goroutine via Init().
func buildBodyMD(messageID int64, store mail.MessageStore) (string, bodyKind) {
	if text, err := store.Body(messageID); err == nil && text != "" {
		return text, bodyKindText
	}
	if html, err := store.BodyHTML(messageID); err == nil && html != "" {
		clean := reHTMLHead.ReplaceAllString(html, "")
		clean = reHTMLStyle.ReplaceAllString(clean, "")
		clean = reHTMLScript.ReplaceAllString(clean, "")
		clean = reHTMLComment.ReplaceAllString(clean, "")
		if text, err := html2text.FromString(clean, html2text.Options{}); err == nil {
			return text, bodyKindHTML
		}
	}
	return "", bodyKindNone
}

// renderMD applies glamour to a markdown string. Package-level so it can be
// called from the async body-load goroutine without a mailRead receiver.
func renderMD(r *glamour.TermRenderer, md string) string {
	if r == nil || md == "" {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}

// placeholderBody returns a styled fixture body for demo items with no store.
func (mr mailRead) placeholderBody() string {
	indent := "  "
	lines := []string{
		"",
		indent + styles.InfoValue.Render("Hi Joe,"),
		"",
		indent + styles.InfoValue.Render(mr.item.preview),
		"",
		indent + styles.InfoValue.Render("Let me know what works for you — happy to be flexible."),
		"",
		indent + styles.InfoValue.Render("Thanks,"),
		indent + styles.InfoAccent.Background(styles.ColorBg).Render(firstName(mr.item.from)),
		"",
		indent + styles.InfoMeta.Background(styles.ColorBg).Render("— placeholder body · wire up IMAP fetch here —"),
	}
	return strings.Join(lines, "\n")
}

func (mr mailRead) Title() string { return mr.item.subject }

func (mr mailRead) StatusKeys() string {
	return keyHintsFromKeyMap(mr.KeyMap())
}

// KeyMap declares the mail-read bindings. Primary keys (esc/scroll/q) are
// handled directly by Update(); the Leader tree powers the whichkey popup.
func (mr mailRead) KeyMap() keymap.KeyMap {
	stub := func(note string) keymap.Action {
		return func() keymap.Result { return keymap.Result{Note: note} }
	}

	primary := []keymap.Binding{
		{Key: "esc", Desc: "back"},
		{Key: "↑↓", Desc: "scroll"},
	}
	if len(mr.attachments) > 0 {
		primary = append(primary, keymap.Binding{Key: "tab", Desc: "attach"})
		if mr.attachmentIdx >= 0 {
			primary = append(primary,
				keymap.Binding{Key: "⏎", Desc: "open"},
				keymap.Binding{Key: "s", Desc: "save"},
			)
		}
	}
	primary = append(primary, keymap.Binding{Key: "q", Desc: "quit"})

	return keymap.KeyMap{
		Primary: primary,
		Leader: []keymap.Entry{
			{
				Key:  "m",
				Desc: "mail",
				Children: []keymap.Entry{
					{Key: "r", Desc: "reply", Accent: true, Action: func() keymap.Result {
						src, err := mr.store.ByUID(mr.item.folderID, mr.item.uid)
						if err != nil || src == nil {
							return keymap.Result{Note: "could not load message"}
						}
						body, _ := buildBodyMD(src.ID, mr.store)
						acct := mr.cfg.AccountForMessage(src)
						seed := compose.BuildReply(src, acct, body)
						return keymap.Result{Push: newComposeViewSeeded(mr.cfg, mr.store, mr.sender, seed)}
					}},
					{Key: "R", Desc: "reply all", Action: func() keymap.Result {
						src, err := mr.store.ByUID(mr.item.folderID, mr.item.uid)
						if err != nil || src == nil {
							return keymap.Result{Note: "could not load message"}
						}
						body, _ := buildBodyMD(src.ID, mr.store)
						acct := mr.cfg.AccountForMessage(src)
						seed := compose.BuildReplyAll(src, acct, body)
						return keymap.Result{Push: newComposeViewSeeded(mr.cfg, mr.store, mr.sender, seed)}
					}},
					{Key: "f", Desc: "forward", Action: func() keymap.Result {
						src, err := mr.store.ByUID(mr.item.folderID, mr.item.uid)
						if err != nil || src == nil {
							return keymap.Result{Note: "could not load message"}
						}
						if atts, err := mr.store.ListAttachments(src.ID); err == nil {
							src.Attachments = atts
						}
						body, _ := buildBodyMD(src.ID, mr.store)
						acct := mr.cfg.AccountForMessage(src)
						seed := compose.BuildForward(src, acct, body, func(a mail.Attachment) string {
							return mr.store.AttachmentPath(a)
						})
						return keymap.Result{Push: newComposeViewSeeded(mr.cfg, mr.store, mr.sender, seed)}
					}},
					{Key: "d", Desc: "delete", Danger: true, Action: func() keymap.Result {
						if mr.item.uid == 0 {
							return keymap.Result{Note: "no imap coords"}
						}
						return keymap.Result{Message: deleteMsg{
							folderID: mr.item.folderID,
							uid:      mr.item.uid,
							popAfter: true,
						}}
					}},
					{Key: "a", Desc: "archive", Action: func() keymap.Result {
						if mr.item.uid == 0 {
							return keymap.Result{Note: "no imap coords"}
						}
						return keymap.Result{Message: archiveMsg{
							folderID: mr.item.folderID,
							uid:      mr.item.uid,
							popAfter: true,
						}}
					}},
					{Key: "u", Desc: "mark unread", Action: func() keymap.Result {
						if mr.item.uid == 0 {
							return keymap.Result{Note: "no imap coords"}
						}
						// OR in FlagSeen because our captured item.flags may be stale
						// (the auto-mark-on-open cmd ran after construction).
						flags := mr.item.flags | mail.FlagSeen
						return keymap.Result{Message: markSeenMsg{
							folderID: mr.item.folderID,
							uid:      mr.item.uid,
							flags:    flags,
							seen:     false,
							popAfter: true,
						}}
					}},
					{Key: "m", Desc: "move to folder", Action: func() keymap.Result {
						if mr.item.uid == 0 {
							return keymap.Result{Note: "no imap coords"}
						}
						return keymap.Result{Message: openMoveToFolderMsg{
							folderID: mr.item.folderID,
							uid:      mr.item.uid,
							popAfter: true,
						}}
					}},
					{Key: "F", Desc: "flag", Action: func() keymap.Result {
						if mr.item.uid == 0 {
							return keymap.Result{Note: "no imap coords"}
						}
						return keymap.Result{Message: markFlaggedMsg{
							folderID: mr.item.folderID,
							uid:      mr.item.uid,
							flags:    mr.item.flags,
							flagged:  mr.item.flags&mail.FlagFlagged == 0,
						}}
					}},
				},
			},
			{
				Key:  "v",
				Desc: "view",
				Children: []keymap.Entry{
					{Key: "v", Desc: "html view", Accent: true, Action: func() keymap.Result {
						return keymap.Result{Message: openHTMLMsg{id: mr.item.messageID}}
					}},
					{Key: "h", Desc: "toggle headers", Action: stub("toggle headers")},
					{Key: "s", Desc: "source", Action: stub("show source")},
					{Key: "p", Desc: "print", Action: stub("print")},
				},
			},
			{
				Key: "b", Desc: "back", Accent: true,
				Action: func() keymap.Result { return keymap.Result{Pop: true} },
			},
			{
				Key: "q", Desc: "quit", Danger: true,
				Action: func() keymap.Result { return keymap.Result{Quit: true} },
			},
		},
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func formatSender(mi mailItem) string {
	if mi.email == "" {
		return mi.from
	}
	if mi.from == "" {
		return mi.email
	}
	return mi.from + "  <" + mi.email + ">"
}

func formatDateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Mon, Jan 2 2006  ·  15:04")
}

func firstName(s string) string {
	if s == "" {
		return "—"
	}
	if i := strings.Index(s, " "); i > 0 {
		return s[:i]
	}
	return s
}

func buildAndOpenHTML(id int64, store mail.MessageStore, atts []mail.Attachment) string {
	html, err := store.BodyHTML(id)
	if err != nil || html == "" {
		return "no html body"
	}
	dir, err := tempDirFn()
	if err != nil {
		return "temp dir: " + err.Error()
	}
	indexPath, err := mailviewWrite(dir, html, atts, store.AttachmentPath)
	if err != nil {
		return "write: " + err.Error()
	}
	if err := openFn(indexPath); err != nil {
		return err.Error()
	}
	return ""
}
