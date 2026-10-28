// Package ui — compose view
//
// The compose flow is a three-stage pushed view:
//
//  1. stageForm     — a centered, floating form modal (From / To /
//     Subject / Markdown) rendered over a dimmed overlay.
//     The form is hand-rolled (bubbles/textinput + a small
//     state machine) to match pmx's palette exactly.
//  2. (editor)      — ephemeral $EDITOR handoff via tea.ExecProcess. The
//     compose view stays on the stack but has no visible
//     state here; bubbletea restores the terminal when
//     the editor exits and delivers an EditorDoneMsg.
//  3. stagePreview  — full-screen viewport that renders a header strip
//     (From / To / Subject + optional markdown badge)
//     plus the body (glamour-rendered when markdown is on).
//
// Confirming the preview with <enter> builds an RFC 5322 MIME message,
// enqueues it to the outbox table, and pops the view back to the mail
// list. <e> re-enters the editor; <esc> cancels without sending.
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/ui/compose"
	"github.com/5Sigma/pmx/internal/ui/keymap"
	"github.com/5Sigma/pmx/internal/ui/pathpicker"
	"github.com/5Sigma/pmx/internal/ui/styles"
)

// composeStage enumerates the flow positions. The editor is NOT a visible
// stage — bubbletea takes over the terminal while $EDITOR runs — but it has
// its own slot in the enum so Update() can short-circuit messages that
// arrive between the form submit and the EditorDoneMsg (otherwise the next
// tick would see the form still in formStateSubmitted and launch a second
// editor).
type composeStage int

const (
	composeStageForm composeStage = iota
	composeStageEditing
	composeStagePreview
)

// attachFileMsg is dispatched when the user selects a file in the picker.
type attachFileMsg struct{ path string }

// composeView is the pmx compose modal. Pointer-receiver View so the custom
// form and viewport can be updated in place without copy / reassign
// gymnastics.
type composeView struct {
	stage  composeStage
	form   *composeForm
	cfg    *config.Config
	store  mail.MessageStore
	sender *mailsend.Engine

	// Editor + preview state.
	body     string
	bodyPath string

	// Attachments selected by the user on the preview stage.
	attachments   []compose.Attachment
	attachmentIdx int

	vp     viewport.Model
	width  int
	height int

	// Error banner shown at the top of the preview on send-failure. Cleared
	// on every state transition.
	err string

	// seed is non-nil for reply/forward flows; nil for fresh compose.
	seed *compose.Seed
}

// newComposeView constructs a fresh compose modal.
func newComposeView(cfg *config.Config, store mail.MessageStore, sender *mailsend.Engine) *composeView {
	return newComposeViewSeeded(cfg, store, sender, nil)
}

// newComposeViewSeeded creates a compose view pre-populated from seed.
// Pass nil for seed to get the same behaviour as newComposeView.
func newComposeViewSeeded(cfg *config.Config, store mail.MessageStore, sender *mailsend.Engine, seed *compose.Seed) *composeView {
	c := &composeView{cfg: cfg, store: store, sender: sender, seed: seed}
	c.form = newComposeForm(cfg)
	c.vp = viewport.New(0, 0)

	if seed != nil {
		// Prefill recipient fields and subject.
		if len(seed.To) > 0 {
			c.form.SetTo(seed.To)
		}
		if len(seed.Cc) > 0 {
			c.form.SetCc(seed.Cc)
		}
		if len(seed.Bcc) > 0 {
			c.form.SetBcc(seed.Bcc)
		}
		if len(seed.Cc) > 0 || len(seed.Bcc) > 0 {
			c.form.SetShowAdvanced(true)
		}
		if seed.Subject != "" {
			c.form.SetSubject(seed.Subject)
		}
		if seed.From != "" {
			c.form.SetFromAccount(seed.From)
		}
		// Attachments are applied directly.
		c.attachments = seed.Attachments
	}

	return c
}

// IsCapturingInput reports whether the compose view currently swallows
// free-text keystrokes. The form stage always captures; the preview stage
// does not (unless the file picker is open). The root model consults this to
// suppress the <space> leader popup while the user is typing or picking.
func (c *composeView) IsCapturingInput() bool {
	return c.stage == composeStageForm || c.stage == composeStageEditing
}

func (c *composeView) Init() tea.Cmd {
	return c.form.Init()
}

func (c *composeView) Update(msg tea.Msg) (View, tea.Cmd) {
	// File selected from the picker.
	if am, ok := msg.(attachFileMsg); ok {
		c.addAttachment(am.path)
		return c, nil
	}

	switch msg := msg.(type) {
	case compose.EditorDoneMsg:
		if msg.Err != nil {
			c.err = fmt.Sprintf("editor: %v", msg.Err)
			c.stage = composeStagePreview
			c.setupPreview()
			return c, nil
		}
		c.body = msg.Body
		if c.bodyPath != "" {
			_ = os.Remove(c.bodyPath)
			c.bodyPath = ""
		}
		c.stage = composeStagePreview
		c.setupPreview()
		return c, nil

	case tea.KeyMsg:
		// Esc always cancels compose at any stage. We handle it here (before
		// routing to the form or preview sub-updates) so a stuck form state
		// can never prevent the user from backing out.
		if msg.String() == "esc" {
			if c.bodyPath != "" {
				_ = os.Remove(c.bodyPath)
				c.bodyPath = ""
			}
			return c, Pop()
		}
		if c.stage == composeStagePreview {
			return c.handlePreviewKey(msg)
		}
	}

	// Drop everything while the editor is in flight. The compose view is
	// invisible during this window — bubbletea has handed the terminal to
	// $EDITOR — so there is no legitimate work to do until EditorDoneMsg
	// arrives.
	if c.stage == composeStageEditing {
		return c, nil
	}

	if c.stage == composeStageForm {
		var cmd tea.Cmd
		c.form, cmd = c.form.Update(msg)
		switch c.form.state {
		case formStateCancelled:
			return c, Pop()
		case formStateSubmitted:
			// Transition BEFORE returning the launch command so the next
			// bubbletea tick (which still sees formStateSubmitted) does not
			// relaunch the editor.
			launch := c.launchEditor()
			c.stage = composeStageEditing
			return c, launch
		}
		return c, cmd
	}

	if c.stage == composeStagePreview {
		var cmd tea.Cmd
		c.vp, cmd = c.vp.Update(msg)
		return c, cmd
	}

	return c, nil
}

// handlePreviewKey dispatches the preview-stage key bindings. Any key that
// is not consumed here is forwarded to the viewport so pageup / pgdn / j / k
// still scroll the body.
//
// Note: esc is intentionally NOT handled here. The outer Update() intercepts
// it before this function is ever called so it works uniformly across every
// compose stage.
func (c *composeView) handlePreviewKey(msg tea.KeyMsg) (View, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return c.confirmSend()
	case "e":
		c.stage = composeStageEditing
		return c, c.reEnterEditor()
	case "a":
		home, _ := os.UserHomeDir()
		cfg := pathpicker.OpenConfig{
			Prompt: "Attach file…",
			Mode:   pathpicker.ModeFilesAndDirs,
			Start:  home,
			OnSelect: func(abs string) tea.Msg {
				return attachFileMsg{path: abs}
			},
		}
		return c, func() tea.Msg { return openPathPickerMsg{cfg: cfg} }
	case "tab":
		if len(c.attachments) > 0 {
			c.attachmentIdx = (c.attachmentIdx + 1) % len(c.attachments)
		}
		return c, nil
	case "shift+tab":
		if len(c.attachments) > 0 {
			c.attachmentIdx = (c.attachmentIdx - 1 + len(c.attachments)) % len(c.attachments)
		}
		return c, nil
	case "d":
		if len(c.attachments) > 0 {
			c.attachments = append(c.attachments[:c.attachmentIdx], c.attachments[c.attachmentIdx+1:]...)
			if c.attachmentIdx >= len(c.attachments) && c.attachmentIdx > 0 {
				c.attachmentIdx = len(c.attachments) - 1
			}
			// Recalculate body height since strip rows may have changed.
			c.SetSize(c.width, c.height)
		}
		return c, nil
	}
	var cmd tea.Cmd
	c.vp, cmd = c.vp.Update(msg)
	return c, cmd
}

// addAttachment resolves a path into an Attachment and appends it to the list.
// Duplicate paths (by filepath.Clean) are silently ignored. Errors resolving
// the path are shown in the preview error banner.
func (c *composeView) addAttachment(path string) {
	a, err := compose.NewAttachment(path)
	if err != nil {
		c.err = err.Error()
		return
	}
	for _, existing := range c.attachments {
		if existing.Path == a.Path {
			return
		}
	}
	c.err = ""
	c.attachments = append(c.attachments, a)
	c.attachmentIdx = len(c.attachments) - 1
	// Recalculate body height since the strip is now visible.
	c.SetSize(c.width, c.height)
}

// writeTempAndLaunch creates a temp file with the appropriate extension,
// writes initial content if non-empty, and hands it to the editor via
// compose.LaunchEditor. Both launchEditor and reEnterEditor delegate here
// so the temp-file logic lives in exactly one place.
func (c *composeView) writeTempAndLaunch(initial string) tea.Cmd {
	ext := "txt"
	if c.form.UseMarkdown() {
		ext = "md"
	}
	f, err := os.CreateTemp("", "pmx-compose-*."+ext)
	if err != nil {
		c.err = "could not create temp file: " + err.Error()
		return nil
	}
	if initial != "" {
		if _, werr := f.WriteString(initial); werr != nil {
			_ = f.Close()
			c.err = "could not write temp file: " + werr.Error()
			return nil
		}
	}
	c.bodyPath = f.Name()
	_ = f.Close()
	return compose.LaunchEditor(c.bodyPath)
}

// launchEditor opens the editor with the seed body as initial content (for
// reply/forward), or a blank file for a fresh compose.
func (c *composeView) launchEditor() tea.Cmd {
	c.err = ""
	initial := ""
	if c.seed != nil {
		initial = c.seed.Body
	}
	return c.writeTempAndLaunch(initial)
}

// reEnterEditor rewrites the current body back to a temp file so the user
// can continue editing from where they left off.
func (c *composeView) reEnterEditor() tea.Cmd {
	c.err = ""
	return c.writeTempAndLaunch(c.body)
}

// confirmSend builds the EML, ensures the account row exists, enqueues the
// message, and pops back on success. Any failure leaves the view on the
// preview stage with an error banner.
func (c *composeView) confirmSend() (View, tea.Cmd) {
	if c.store == nil {
		c.err = "no mail store available"
		return c, nil
	}

	acct := c.selectedAccount()
	fromAddr := acct.Email
	fromName := acct.FullName
	if fromName == "" {
		fromName = acct.Name
	}

	inReplyTo := ""
	var references []string
	if c.seed != nil {
		inReplyTo = c.seed.InReplyTo
		references = c.seed.References
	}

	toAddrs := c.form.ToAddrs()
	ccAddrs := c.form.CcAddrs()
	bccAddrs := c.form.BccAddrs()
	eml, err := compose.BuildEML(compose.BuildOpts{
		FromAddr:    fromAddr,
		FromName:    fromName,
		To:          toAddrs,
		Cc:          ccAddrs,
		Bcc:         bccAddrs,
		Subject:     c.form.Subject(),
		Body:        c.body,
		UseMarkdown: c.form.UseMarkdown(),
		CSS:         c.form.SelectedThemeCSS(),
		Attachments: c.attachments,
		InReplyTo:   inReplyTo,
		References:  references,
	})
	if err != nil {
		c.err = fmt.Sprintf("build EML: %v", err)
		return c, nil
	}

	accountID, err := c.store.EnsureAccount(acct.Name, fromAddr)
	if err != nil {
		c.err = fmt.Sprintf("account lookup: %v", err)
		return c, nil
	}

	item := &mail.OutboxItem{
		AccountID: accountID,
		RawEML:    eml,
	}
	if err := c.store.EnqueueOutbox(item); err != nil {
		c.err = fmt.Sprintf("enqueue: %v", err)
		return c, nil
	}

	// Persist the chosen theme so the next compose session starts on it.
	if c.form.UseMarkdown() {
		if name := c.form.SelectedThemeName(); name != "" {
			_ = config.SaveComposeState(config.ComposeState{LastMarkdownTheme: name})
		}
	}

	// Poke the send worker so it drains immediately instead of waiting for the
	// next poll tick. Best-effort: ErrNotRunning is silently ignored.
	if c.sender != nil {
		_ = c.sender.Trigger(acct.Name)
	}

	return c, Pop()
}

// selectedAccount returns the account the user picked in the From field,
// delegating to the form so the preview and send paths see the same value.
func (c *composeView) selectedAccount() config.Account {
	return c.form.FromAccount()
}

// setupPreview populates the viewport with the message body only. The
// header strip is rendered separately by renderPreview() every frame so it
// stays pinned to the top as the user scrolls.
func (c *composeView) setupPreview() {
	body := c.body
	if c.form.UseMarkdown() && body != "" && c.width > 0 {
		// WithAutoStyle calls termenv.HasDarkBackground which queries the terminal
		// via OSC sequences — doing that inside bubbletea's event loop after
		// ExecProcess restores the terminal corrupts the input stream until a key
		// press. Hard-code "dark" to match pmx's palette and avoid the query.
		if r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(c.width),
		); err == nil {
			if rendered, err := r.Render(body); err == nil {
				// Glamour prepends a leading newline; trim it so the viewport
				// starts on the first line of content rather than a blank row.
				body = strings.TrimLeft(rendered, "\n")
			}
		}
	}
	c.vp.GotoTop()
	c.vp.SetContent(body)
}

// renderPreviewHeader builds the "From / To / Subject" strip that sits
// above the scrollable body. Painted on ColorOverlay with a bottom rule so
// it reads as a persistent info bar.
func (c *composeView) renderPreviewHeader(acct config.Account) string {
	w := c.width
	if w <= 0 {
		return ""
	}

	bar := styles.ComposePreviewBar

	row := func(key, val string, valStyle lipgloss.Style, trailing string) string {
		k := styles.ComposeMetaKey.Render(fmt.Sprintf("  %-9s ", key))
		v := valStyle.Render(val)
		trailW := lipgloss.Width(trailing)
		gap := w - lipgloss.Width(k) - lipgloss.Width(v) - trailW
		if gap < 0 {
			gap = 0
		}
		return k + v + bar.Render(strings.Repeat(" ", gap)) + trailing
	}

	fromDisplay := acct.Email
	if acct.Name != "" {
		fromDisplay = fmt.Sprintf("%s <%s>", acct.Name, acct.Email)
	}
	toDisplay := formatAddrList(c.form.ToAddrs())
	if toDisplay == "" {
		toDisplay = "(none)"
	}
	ccDisplay := formatAddrList(c.form.CcAddrs())
	bccDisplay := formatAddrList(c.form.BccAddrs())
	subjectDisplay := c.form.Subject()
	if subjectDisplay == "" {
		subjectDisplay = "(no subject)"
	}

	// Markdown badge sits at the far right of the subject row.
	trailing := ""
	if c.form.UseMarkdown() {
		trailing = styles.ComposeMarkdownBadge.Render(" MD ") + bar.Render(" ")
	}

	var errRow string
	if c.err != "" {
		errRow = styles.ComposeError.Render("  "+c.err) + "\n"
	}

	rule := styles.ComposeMetaRule.Render(strings.Repeat("─", w))

	out := row("From", fromDisplay, styles.ComposeMetaValue, "") + "\n" +
		row("To", toDisplay, styles.ComposeMetaValue, "") + "\n"
	if ccDisplay != "" {
		out += row("Cc", ccDisplay, styles.ComposeMetaValue, "") + "\n"
	}
	if bccDisplay != "" {
		out += row("Bcc", bccDisplay, styles.ComposeMetaValue, "") + "\n"
	}
	out += row("Subject", subjectDisplay, styles.ComposeMetaSubject, trailing) + "\n" +
		rule + "\n" +
		errRow
	return out
}

// formatAddrList renders a comma-separated "Name <email>" list, falling back
// to the bare email when Name is empty. Returns "" for an empty slice so
// callers can distinguish absent vs. present-but-blank.
func formatAddrList(addrs []mail.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name == "" {
			parts = append(parts, a.Email)
		} else {
			parts = append(parts, fmt.Sprintf("%s <%s>", a.Name, a.Email))
		}
	}
	return strings.Join(parts, ", ")
}

func (c *composeView) View() string {
	switch c.stage {
	case composeStageForm:
		return c.renderForm()
	case composeStageEditing:
		// tea.ExecProcess owns the terminal while the editor runs. This path
		// is only rendered in the brief moment between the editor exiting and
		// EditorDoneMsg being delivered. Show a neutral placeholder so the
		// user doesn't see a blank screen flash.
		return lipgloss.NewStyle().
			Foreground(styles.ColorDimFg).
			Padding(1, 2).
			Render("Waiting for editor…")
	case composeStagePreview:
		return c.renderPreview()
	}
	return ""
}

// renderForm paints the compose dialog as a stack of four full-width
// sections — title bar, separator, surface-filled field area, footer —
// wrapped in a single rounded border. Every section explicitly fills the
// inner width so there are no uncolored gaps between content and border.
func (c *composeView) renderForm() string {
	w, h := c.width, c.height
	if w <= 0 || h <= 0 {
		return ""
	}

	panelW := 62
	if w < 74 {
		panelW = w - 8
	}
	if panelW < 28 {
		panelW = 28
	}

	// Inner content width = panel width minus the 1-cell border on each side.
	innerW := panelW - 2

	// Title bar — a solid primary-colored strip with a mail glyph.
	titleBar := lipgloss.NewStyle().
		Background(styles.ColorPrimary).
		Foreground(styles.ColorFgStrong).
		Bold(true).
		Width(innerW).
		Padding(0, 2).
		Render("  Compose Message")

	// Thin separator between sections. Painted on ColorBg so it reads as a
	// hairline inside the border frame.
	sep := lipgloss.NewStyle().
		Foreground(styles.ColorBorder).
		Background(styles.ColorBg).
		Width(innerW).
		Render(strings.Repeat("─", innerW))

	// Field area — the form rows, blanket-painted on ColorSurface so the
	// unused horizontal slack to the right of every value is still the same
	// color as the row background.
	c.form.SetWidth(innerW)
	fieldArea := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Width(innerW).
		Padding(1, 0).
		Render(c.form.View())

	inner := lipgloss.JoinVertical(lipgloss.Left,
		titleBar,
		sep,
		fieldArea,
	)

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

// Attachment card geometry. Each card is a 2-tall block decorated with a
// thick left-side rule in place of a full surround border, plus 1 cell of
// left padding inside the rule. The widths below are the total outer card
// width — left-side rule and left pad included.
const (
	attachCardOuterH  = 2  // outer height: name row + meta row (no border rows)
	attachCardMinW    = 18 // smallest card we'll render — truncates aggressively
	attachCardMaxW    = 28 // comfortable card width when there's room
	attachCardGap     = 1  // horizontal gap between cards
	attachMaxCardRows = 2  // never let the strip grow beyond this many rows of cards
)

// layoutAttachments plans the card grid for the current attachments and
// terminal width. Returns the per-card width used and the slice of row
// slices (indices into c.attachments). If some attachments don't fit, the
// returned flag indicates how many were truncated so the caller can render
// a "+N more" chip on the final row.
func (c *composeView) layoutAttachments() (cardW int, rows [][]int, truncated int) {
	n := len(c.attachments)
	if n == 0 || c.width <= 0 {
		return 0, nil, 0
	}

	// The strip starts at column 0; leave 2 cells on the right so the final
	// card doesn't butt against the terminal edge.
	avail := c.width - 2
	if avail < attachCardMinW {
		avail = attachCardMinW
	}

	// Pick a card width that lets at least a couple of cards fit when
	// possible. Try the max first; shrink to the min until something fits
	// or we hit the floor.
	cardW = attachCardMaxW
	if cardW > avail {
		cardW = avail
	}
	// How many cards per row at the current width?
	perRow := (avail + attachCardGap) / (cardW + attachCardGap)
	if perRow < 1 {
		perRow = 1
	}

	// If we only have one card per row but there would be room for more at
	// a slightly smaller card width, prefer packing more.
	if perRow == 1 && n > 1 {
		tryW := (avail - attachCardGap) / 2
		if tryW >= attachCardMinW {
			cardW = tryW
			perRow = 2
		}
	}

	// Build the rows up to the row cap.
	for i := 0; i < n; {
		if len(rows) >= attachMaxCardRows {
			truncated = n - i
			break
		}
		end := i + perRow
		if end > n {
			end = n
		}
		row := make([]int, 0, end-i)
		for j := i; j < end; j++ {
			row = append(row, j)
		}
		// If this is the final row we'll render AND there'd be more cards
		// after it we can't fit, make room for a "+N more" chip by dropping
		// the last card.
		if len(rows)+1 == attachMaxCardRows && end < n {
			// Count how much space a "+N more" chip needs — one card slot.
			// Only drop a card if we have more than one on this row.
			if len(row) > 1 {
				row = row[:len(row)-1]
				truncated = n - (i + len(row))
				rows = append(rows, row)
				break
			}
			truncated = n - end
		}
		rows = append(rows, row)
		i = end
	}
	return cardW, rows, truncated
}

// attachmentsStripHeight returns the number of rows the attachments strip
// occupies. 0 when there are no attachments; otherwise 1 row for the
// caption rule plus the vertical space taken by the card grid (each card
// row is attachCardOuterH lines tall with no inter-row gap).
func (c *composeView) attachmentsStripHeight() int {
	_, rows, _ := c.layoutAttachments()
	if len(rows) == 0 {
		return 0
	}
	return 1 + len(rows)*attachCardOuterH + 1 // +1 spacer row above status bar
}

// attachmentIcon maps a MIME type (and/or extension) to a Nerd Font glyph.
// Icons use the file-type outlined set so they share a consistent weight.
func attachmentIcon(a compose.Attachment) string {
	mt := strings.ToLower(a.MIMEType)
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(a.Filename), "."))

	switch {
	case strings.HasPrefix(mt, "image/"):
		return "" // nf-fa-file_image_o
	case mt == "application/pdf" || ext == "pdf":
		return "" // nf-fa-file_pdf_o
	case strings.HasPrefix(mt, "audio/"):
		return "" // nf-fa-file_audio_o
	case strings.HasPrefix(mt, "video/"):
		return "" // nf-fa-file_video_o
	case strings.Contains(mt, "zip") || strings.Contains(mt, "tar") ||
		strings.Contains(mt, "gzip") || strings.Contains(mt, "compressed") ||
		ext == "zip" || ext == "tar" || ext == "gz" || ext == "7z" || ext == "rar":
		return "" // nf-fa-file_archive_o
	case strings.Contains(mt, "spreadsheet") || strings.Contains(mt, "excel") ||
		strings.Contains(mt, "csv") || ext == "csv" || ext == "xls" || ext == "xlsx":
		return "" // nf-fa-file_excel_o
	case strings.Contains(mt, "msword") || strings.Contains(mt, "wordprocessing") ||
		ext == "doc" || ext == "docx":
		return "" // nf-fa-file_word_o
	case strings.Contains(mt, "presentation") || strings.Contains(mt, "powerpoint") ||
		ext == "ppt" || ext == "pptx":
		return "" // nf-fa-file_powerpoint_o
	case strings.HasPrefix(mt, "text/") || mt == "application/json" ||
		mt == "application/xml" || ext == "md" || ext == "txt" ||
		ext == "json" || ext == "xml" || ext == "yaml" || ext == "yml":
		return "" // nf-fa-file_text_o
	case strings.Contains(mt, "javascript") || ext == "go" || ext == "py" ||
		ext == "rs" || ext == "ts" || ext == "js" || ext == "c" || ext == "h" ||
		ext == "cpp" || ext == "sh":
		return "" // nf-fa-file_code_o
	default:
		return "" // nf-fa-file_o
	}
}

// truncateMiddle shortens s to fit in width cells by keeping the head and
// tail and inserting an ellipsis in the middle. Preserves the extension so
// "very_long_report_name.pdf" becomes "very_lo…rt.pdf" rather than
// "very_long_rep…". Operates on runes so it won't slice multi-byte UTF-8
// characters in half. Falls back to right-truncation when there's no room.
func truncateMiddle(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	ext := filepath.Ext(s)
	baseRunes := []rune(s[:len(s)-len(ext)])
	extW := lipgloss.Width(ext)
	// Keep at least 1 rune from base + ellipsis + extension.
	keep := width - extW - 1 // room for '…'
	if keep < 2 {
		runes := []rune(s)
		if width-1 >= len(runes) {
			return s
		}
		return string(runes[:width-1]) + "…"
	}
	head := keep / 2
	tail := keep - head
	if head < 1 {
		head = 1
	}
	if tail < 0 {
		tail = 0
	}
	if head+tail > len(baseRunes) {
		runes := []rune(s)
		if width-1 >= len(runes) {
			return s
		}
		return string(runes[:width-1]) + "…"
	}
	return string(baseRunes[:head]) + "…" + string(baseRunes[len(baseRunes)-tail:]) + ext
}

// renderAttachmentCard draws a single attachment card for a. The card has
// no surround border — just a thick left-side rule in place of one — and 1
// cell of inner left padding after the rule. Width is the outer width
// including the rule. When selected is true the rule and name render in the
// accent color; otherwise a dimmer border color is used.
func renderAttachmentCard(a compose.Attachment, outerW int, selected bool) string {
	// Inner width = outer - 1 (left-side rule) - 1 (left padding after the rule).
	innerW := outerW - 2
	if innerW < 6 {
		innerW = 6
	}

	iconGlyph := attachmentIcon(a)
	// Reserve "<icon> " (2 cells) at the front of the name row.
	nameRoom := innerW - 2
	if nameRoom < 3 {
		nameRoom = 3
	}
	name := truncateMiddle(a.Filename, nameRoom)
	size := FormatBytes(a.Size)
	// Truncate size if (very) narrow.
	if lipgloss.Width(size) > innerW {
		size = truncateMiddle(size, innerW)
	}

	var (
		nameStyle lipgloss.Style
		metaStyle lipgloss.Style
		iconStyle lipgloss.Style
		borderFg  lipgloss.Color
	)
	if selected {
		nameStyle = lipgloss.NewStyle().
			Background(styles.ColorRaised).
			Foreground(styles.ColorFgStrong).
			Bold(true)
		metaStyle = lipgloss.NewStyle().
			Background(styles.ColorRaised).
			Foreground(styles.ColorAccent)
		iconStyle = lipgloss.NewStyle().
			Background(styles.ColorRaised).
			Foreground(styles.ColorPrimaryHi).
			Bold(true)
		borderFg = styles.ColorAccent
	} else {
		nameStyle = lipgloss.NewStyle().
			Background(styles.ColorSurface).
			Foreground(styles.ColorFg)
		metaStyle = lipgloss.NewStyle().
			Background(styles.ColorSurface).
			Foreground(styles.ColorDimFg)
		iconStyle = lipgloss.NewStyle().
			Background(styles.ColorSurface).
			Foreground(styles.ColorWarn)
		borderFg = styles.ColorBorder
	}

	// Row backgrounds: paint the full inner width so the card is a solid
	// block rather than leaking the overlay color through unused slack.
	rowBg := styles.ColorSurface
	if selected {
		rowBg = styles.ColorRaised
	}

	iconCell := iconStyle.Render(iconGlyph)
	// " " between icon and name, on the row background.
	gap := lipgloss.NewStyle().Background(rowBg).Render(" ")

	nameRendered := nameStyle.Render(name)
	// Pad the remainder of the name row to innerW.
	pad := innerW - lipgloss.Width(iconGlyph) - 1 - lipgloss.Width(name)
	if pad < 0 {
		pad = 0
	}
	nameRow := iconCell + gap + nameRendered +
		lipgloss.NewStyle().Background(rowBg).Render(strings.Repeat(" ", pad))

	// Meta row — right-aligned size inside the inner width.
	sizeRendered := metaStyle.Render(size)
	spacer := innerW - lipgloss.Width(size)
	if spacer < 0 {
		spacer = 0
	}
	metaRow := lipgloss.NewStyle().Background(rowBg).Render(strings.Repeat(" ", spacer)) + sizeRendered

	inner := lipgloss.JoinVertical(lipgloss.Left, nameRow, metaRow)

	// Left-only rule in place of a full border. Padding(0, 0, 0, 1) adds a
	// single cell of inner left padding (painted on the row background) so
	// the content doesn't butt against the rule.
	card := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(borderFg).
		BorderBackground(styles.ColorBg).
		Background(rowBg).
		Padding(0, 0, 0, 1).
		Render(inner)

	return card
}

// renderAttachmentsStrip renders the attachments row shown below the body
// viewport. Returns an empty string when there are no attachments. The
// strip layout is:
//
//	<top rule with "<paperclip> N file(s)" caption>
//	<row of cards>  (optional second row)  (optional "+N more" chip)
func (c *composeView) renderAttachmentsStrip() string {
	cardW, rows, truncated := c.layoutAttachments()
	if len(rows) == 0 {
		return ""
	}
	w := c.width

	// --- Top rule / caption --------------------------------------------------
	//
	// The caption sits on the ColorBg rail so it reads as a separator above
	// the cards rather than a continuation of the overlay-colored header.
	captionBase := lipgloss.NewStyle().
		Background(styles.ColorBg).
		Foreground(styles.ColorDimFg)
	captionAccent := lipgloss.NewStyle().
		Background(styles.ColorBg).
		Foreground(styles.ColorWarn).
		Bold(true)
	captionRule := lipgloss.NewStyle().
		Background(styles.ColorBg).
		Foreground(styles.ColorBorder)

	label := fmt.Sprintf("Attachments  %d", len(c.attachments))
	captionText := captionAccent.Render("") + captionBase.Render("  "+label) + "  "
	captionTextW := lipgloss.Width(captionText)
	ruleLen := w - captionTextW
	if ruleLen < 0 {
		ruleLen = 0
	}
	topRule := captionText + captionRule.Render(strings.Repeat("─", ruleLen))

	// --- Card rows -----------------------------------------------------------
	rail := lipgloss.NewStyle().Background(styles.ColorBg)

	// Build each row by joining cards horizontally, then pad the row to the
	// full terminal width so the ColorBg background tiles cleanly.
	rowStrings := make([]string, 0, len(rows))
	for ri, idxs := range rows {
		cards := make([]string, 0, len(idxs)+1)
		for i, idx := range idxs {
			cards = append(cards, renderAttachmentCard(c.attachments[idx], cardW, idx == c.attachmentIdx))
			if i < len(idxs)-1 {
				// Column-equivalent gap: a vertical stack of blank cells so
				// JoinHorizontal lines everything up to the card height.
				cards = append(cards, buildVerticalGap(attachCardGap, attachCardOuterH))
			}
		}
		// Append "+N more" chip on the last row when needed.
		if ri == len(rows)-1 && truncated > 0 {
			if len(idxs) > 0 {
				cards = append(cards, buildVerticalGap(attachCardGap, attachCardOuterH))
			}
			cards = append(cards, renderMoreChip(truncated, cardW))
		}

		joined := lipgloss.JoinHorizontal(lipgloss.Top, cards...)

		// Right-pad every line of the row to width w so the ColorBg rail
		// tiles cleanly to the terminal edge.
		lines := strings.Split(joined, "\n")
		for li, line := range lines {
			if pad := w - lipgloss.Width(line); pad > 0 {
				lines[li] = line + rail.Render(strings.Repeat(" ", pad))
			}
		}
		rowStrings = append(rowStrings, strings.Join(lines, "\n"))
	}

	return topRule + "\n" + strings.Join(rowStrings, "\n")
}

// buildVerticalGap returns a width×height block of blank cells painted on
// ColorBg. Used to compose horizontal gutters between cards and the left
// padding column so JoinHorizontal aligns them to the full card height.
func buildVerticalGap(width, height int) string {
	rail := lipgloss.NewStyle().Background(styles.ColorBg)
	line := rail.Render(strings.Repeat(" ", width))
	rows := make([]string, height)
	for i := range rows {
		rows[i] = line
	}
	return strings.Join(rows, "\n")
}

// renderMoreChip renders the "+N more" placeholder card shown when the
// attachment list overflows the card grid. Mirrors the attachment card
// decoration — thick left rule in place of a surround border.
func renderMoreChip(n, outerW int) string {
	innerW := outerW - 2
	if innerW < 4 {
		innerW = 4
	}
	label := fmt.Sprintf("+%d", n)
	more := "more"
	if lipgloss.Width(more) > innerW {
		more = ""
	}

	labelStyle := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorPrimaryHi).
		Bold(true)
	metaStyle := lipgloss.NewStyle().
		Background(styles.ColorSurface).
		Foreground(styles.ColorDimFg)
	rowBg := lipgloss.NewStyle().Background(styles.ColorSurface)

	topPad := innerW - lipgloss.Width(label)
	if topPad < 0 {
		topPad = 0
	}
	nameRow := labelStyle.Render(label) + rowBg.Render(strings.Repeat(" ", topPad))

	botPad := innerW - lipgloss.Width(more)
	if botPad < 0 {
		botPad = 0
	}
	metaRow := metaStyle.Render(more) + rowBg.Render(strings.Repeat(" ", botPad))

	inner := lipgloss.JoinVertical(lipgloss.Left, nameRow, metaRow)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(styles.ColorBorder).
		BorderBackground(styles.ColorBg).
		Background(styles.ColorSurface).
		Padding(0, 0, 0, 1).
		Render(inner)
}

// renderPreview paints the preview-stage layout: fixed header strip at the
// top, scrollable viewport in the middle, attachments strip pinned to the
// bottom. The viewport height is sized in SetSize so the attachments strip
// always sits flush under the body.
func (c *composeView) renderPreview() string {
	w, h := c.width, c.height
	if w <= 0 || h <= 0 {
		return ""
	}

	acct := c.selectedAccount()
	header := c.renderPreviewHeader(acct)

	parts := []string{header, c.vp.View()}
	if strip := c.renderAttachmentsStrip(); strip != "" {
		parts = append(parts, strip)
		parts = append(parts, styles.ComposeOverlay.Width(w).Render(""))
	}

	rendered := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Pad to the configured height so the content area paints edge-to-edge.
	gap := h - lipgloss.Height(rendered)
	if gap > 0 {
		filler := styles.ComposeOverlay.Width(w).Render(strings.Repeat(" ", 0))
		for i := 0; i < gap; i++ {
			rendered += "\n" + filler
		}
	}
	return rendered
}

func (c *composeView) SetSize(width, height int) View {
	c.width = width
	c.height = height

	// Header strip = 3 info rows + 1 rule + 1 error line (always reserved so
	// the body never jumps when an error appears/disappears).
	const headerH = 5
	bodyH := height - headerH - c.attachmentsStripHeight()
	if bodyH < 1 {
		bodyH = 1
	}
	c.vp.Width = width
	c.vp.Height = bodyH

	if c.stage == composeStagePreview {
		c.setupPreview()
	}
	return c
}

func (c *composeView) Title() string { return "compose" }

func (c *composeView) StatusKeys() string {
	return keyHintsFromKeyMap(c.KeyMap())
}

func (c *composeView) KeyMap() keymap.KeyMap {
	switch c.stage {
	case composeStageForm:
		return keymap.KeyMap{
			Primary: []keymap.Binding{
				{Key: "tab", Desc: "next field"},
				{Key: "ctrl+a", Desc: "advanced"},
				{Key: "enter", DisplayKey: "↵", Desc: "open editor"},
				{Key: "esc", Desc: "cancel"},
			},
		}
	case composeStageEditing:
		return keymap.KeyMap{}
	default:
		bindings := []keymap.Binding{
			{Key: "enter", DisplayKey: "↵", Desc: "send", Accent: true},
			{Key: "e", Desc: "edit"},
			{Key: "a", Desc: "attach"},
			{Key: "j/k", Desc: "scroll"},
			{Key: "esc", Desc: "cancel", Danger: true},
		}
		if len(c.attachments) > 0 {
			bindings = append(bindings,
				keymap.Binding{Key: "tab", Desc: "cycle"},
				keymap.Binding{Key: "d", Desc: "detach"},
			)
		}
		return keymap.KeyMap{Primary: bindings}
	}
}

// Compile-time check that *composeView satisfies View.
var _ View = (*composeView)(nil)
