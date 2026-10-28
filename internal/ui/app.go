package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/5Sigma/pmx/internal/accountrunner"
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/ui/accountdrawer"
	"github.com/5Sigma/pmx/internal/ui/compose"
	"github.com/5Sigma/pmx/internal/ui/folderdrawer"
	"github.com/5Sigma/pmx/internal/ui/keymap"
	"github.com/5Sigma/pmx/internal/ui/pathpicker"
	"github.com/5Sigma/pmx/internal/ui/styles"
	"github.com/5Sigma/pmx/internal/ui/whichkey"
)

// Chrome height reserved by the shell:
//
//	row 1: header row (brand + context merged)
//	row 2: hairline rule
//	row 3: content … (variable)
//	row N: status bar
//
// When the whichkey popup is open it steals additional rows from the content
// region (see Model.popupHeight / Model.contentHeight).
const (
	headerRow     = 1
	headerRuleRow = 1
	statusBarRow  = 1

	chromeHeight = headerRow + headerRuleRow + statusBarRow
)

// refreshMsg is sent by the "r" key binding to poke the active watcher.
type refreshMsg struct{}

// clearSendStatusMsg clears the transient send-status chip in the header.
type clearSendStatusMsg struct{}

func clearSendStatusAfter(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return clearSendStatusMsg{}
	}
}

// markSeenMsg flips the \Seen flag for a single message. The root handler
// does the local DB write, calls engine.MarkSeen, optionally pops the stack,
// and reloads the mail list + folder counts.
type markSeenMsg struct {
	folderID int64
	uid      uint32
	flags    mail.Flag // current full bitmask — used to compute new value
	seen     bool      // desired new value of the FlagSeen bit
	popAfter bool      // if true, pop the top view after marking
}

// markFlaggedMsg toggles the \Flagged flag for a single message. Mirrors markSeenMsg.
type markFlaggedMsg struct {
	folderID int64
	uid      uint32
	flags    mail.Flag
	flagged  bool
	popAfter bool
}

// deleteMsg removes a single message from the local store and dispatches an
// IMAP MOVE to the account's configured trash mailbox. The server move is
// non-blocking; the local DB row is removed synchronously.
type deleteMsg struct {
	folderID int64
	uid      uint32
	popAfter bool
}

// archiveMsg removes a single message from the current folder locally and
// dispatches an IMAP MOVE to the account's configured archive mailbox. The
// server move is non-blocking; the local DB row is removed synchronously.
type archiveMsg struct {
	folderID int64
	uid      uint32
	popAfter bool
}

// openMoveToFolderMsg asks the shell to open the folder drawer in picker mode
// so the user can choose a destination for the given message.
type openMoveToFolderMsg struct {
	folderID int64
	uid      uint32
	popAfter bool
}

// moveMessageMsg is dispatched by the folder drawer when the user picks a
// destination during a move operation. The shell deletes the local row and
// enqueues an IMAP MOVE to dst.Name.
type moveMessageMsg struct {
	srcFolderID int64
	uid         uint32
	dst         *mail.Folder
	popAfter    bool
}

// folderCountsMsg carries fresh counts for the header chrome, loaded from the
// store after a sync cycle completes.
type folderCountsMsg struct {
	total  int
	unread int
}

// openFolderDrawerMsg asks the shell to reveal the left-side folder drawer.
// Emitted by the mail list ("f" primary binding and the <spc> b leader).
type openFolderDrawerMsg struct{}

// foldersLoadedMsg carries the folder list queried from the store after the
// drawer opens. accountID tags the response so stale replies for a previous
// account are silently discarded.
type foldersLoadedMsg struct {
	accountID int64
	folders   []*mail.Folder
}

// folderSelectedMsg is dispatched by the drawer when the user confirms a
// folder with <enter>. The shell swaps the mail list's folder context,
// reloads messages, and asks the sync engine to re-SELECT on the IMAP side.
type folderSelectedMsg struct {
	folderID   int64
	folderName string
	folderRole mail.FolderRole
}

// openAccountDrawerMsg asks the shell to reveal the account drawer.
type openAccountDrawerMsg struct{}

// openPathPickerMsg asks the shell to reveal the path-picker bottom panel.
// Any view may emit this message; the root model opens the picker and routes
// keys to it until the user accepts or cancels.
type openPathPickerMsg struct {
	cfg pathpicker.OpenConfig
}

// saveAttachmentMsg is emitted by the mailread view's path-picker callback
// when the user confirms a save destination. The root model copies the source
// attachment file to the chosen path and then emits attachmentSavedMsg back.
type saveAttachmentMsg struct {
	src string
	dst string
}

// attachmentSavedMsg carries the result of a save attempt back to the view
// that requested it. note is either "saved to <dst>" or an error description.
type attachmentSavedMsg struct {
	note string
}

// accountsLoadedMsg carries fresh unread stats for all configured accounts.
type accountsLoadedMsg []accountdrawer.AccountStat

// accountSelectedMsg is dispatched when the user picks a new account in the drawer.
type accountSelectedMsg struct{ name string }

// accountSwitchedMsg signals a successful account switch.
type accountSwitchedMsg struct {
	acct          *config.Account
	accountID     int64
	inboxFolderID int64
}

// accountSwitchFailedMsg signals a failed account switch.
type accountSwitchFailedMsg struct {
	name string
	err  error
}

// clearFlashMsg clears the post-switch accent flash from the header.
type clearFlashMsg struct{}

// cycleAccountMsg cycles the active account by delta (+1 next, -1 prev).
type cycleAccountMsg struct{ delta int }

// clearFlashAfter clears the header flash after d.
func clearFlashAfter(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return clearFlashMsg{}
	}
}

// loadAccountStatsCmd queries unread counts for every configured account and
// returns an accountsLoadedMsg. Per-account errors yield zero unread rather
// than failing the batch.
func loadAccountStatsCmd(store mail.MessageStore, cfg *config.Config) tea.Cmd {
	if store == nil || cfg == nil {
		return nil
	}
	return func() tea.Msg {
		stats := make([]accountdrawer.AccountStat, 0, len(cfg.Accounts))
		for _, acct := range cfg.Accounts {
			stat := accountdrawer.AccountStat{Name: acct.Name, Email: acct.Email}
			if id, err := store.EnsureAccount(acct.Name, acct.Email); err == nil {
				if f, err := store.FolderByName(id, "INBOX"); err == nil && f != nil {
					stat.Unread, _ = store.CountUnread(f.ID)
				}
			}
			stats = append(stats, stat)
		}
		return accountsLoadedMsg(stats)
	}
}

// doSwitchAccountCmd runs the account switch off the UI goroutine. It calls
// runner.Switch (which blocks while tearing down the old goroutines and
// starting new ones), resolves the new account's PKs in the store, persists
// the last-active preference, and returns an accountSwitchedMsg or
// accountSwitchFailedMsg.
func doSwitchAccountCmd(
	runner *accountrunner.Runner,
	store mail.MessageStore,
	cfg *config.Config,
	newName string,
) tea.Cmd {
	return func() tea.Msg {
		var newAcct *config.Account
		for i := range cfg.Accounts {
			if cfg.Accounts[i].Name == newName {
				cp := cfg.Accounts[i]
				newAcct = &cp
				break
			}
		}
		if newAcct == nil {
			return accountSwitchFailedMsg{name: newName, err: fmt.Errorf("account %q not found in config", newName)}
		}
		if runner != nil {
			if err := runner.Switch(*newAcct); err != nil {
				return accountSwitchFailedMsg{name: newName, err: err}
			}
		}
		var accountID, inboxFolderID int64
		if store != nil {
			if id, err := store.EnsureAccount(newAcct.Name, newAcct.Email); err == nil {
				accountID = id
				if fid, err := store.EnsureFolder(id, "INBOX"); err == nil {
					inboxFolderID = fid
					_ = store.UpsertFolderRole(fid, mail.RoleInbox)
				}
			}
		}
		_ = config.SaveUIState(config.UIState{LastActiveAccount: newName})
		return accountSwitchedMsg{acct: newAcct, accountID: accountID, inboxFolderID: inboxFolderID}
	}
}

// loadFoldersCmd queries the store for all folders in an account and emits a
// foldersLoadedMsg. Returns nil when the store is unavailable.
func loadFoldersCmd(store mail.MessageStore, accountID int64) tea.Cmd {
	if store == nil {
		return nil
	}
	return func() tea.Msg {
		folders, err := store.ListFolders(accountID)
		if err != nil {
			return nil
		}
		return foldersLoadedMsg{accountID: accountID, folders: folders}
	}
}

// loadFolderCountsCmd queries the store for the given folder and returns a
// folderCountsMsg with total and unread tallies. Returns nil (no-op) when the
// store is unavailable.
func loadFolderCountsCmd(store mail.MessageStore, folderID int64) tea.Cmd {
	if store == nil {
		return nil
	}
	return func() tea.Msg {
		msgs, err := store.ListFolder(folderID, 9999, 0)
		if err != nil {
			return nil
		}
		var unread int
		for _, msg := range msgs {
			if !msg.IsRead() {
				unread++
			}
		}
		return folderCountsMsg{total: len(msgs), unread: unread}
	}
}

// Model is the root bubbletea model. It owns a stack of Views, the whichkey
// popup, and delegates rendering and input to the top-most view.
type Model struct {
	stack  []View
	width  int
	height int

	wk            *whichkey.Model
	folderDrawer  *folderdrawer.Model
	accountDrawer *accountdrawer.Model
	pathPicker    *pathpicker.Model

	// store is the shared message store, available to any view that needs
	// to read or persist mail. May be nil — all paths must tolerate that.
	store mail.MessageStore

	// Sync engine and the account to use for background syncs.
	engine     *mailsync.Engine
	sender     *mailsend.Engine
	runner     *accountrunner.Runner
	activeAcct *config.Account
	cfg        *config.Config

	syncingAcct string // name of account being switched to; "" when idle
	flashAcct   bool   // true for 400ms after a successful switch

	// accountID is the primary key of the active account; used for folder
	// drawer queries. curFolder is the folder PK the mail list currently
	// shows (replaces the previous hardcoded `1`). curFolderName is the IMAP
	// name of that folder (e.g. "INBOX", "Sent"), used to retry SwitchFolder
	// when the watcher connects after the folder was already selected.
	accountID     int64
	curFolder     int64
	curFolderName string
	// pendingFolderSwitch is set to the desired IMAP folder name when
	// SwitchFolder fails (watcher not yet connected). It is cleared once
	// the switch is successfully enqueued.
	pendingFolderSwitch string

	// Context displayed in the header.
	account    string
	accountTag string
	unread     int
	total      int
	synced     time.Time

	// watchStatus is shown briefly in the chrome when the watcher changes state.
	watchStatus string
	// sendStatus is shown briefly in the chrome when the sender changes state.
	sendStatus string

	// syncingFolder is the IMAP folder name currently being pulled from the
	// server. Empty when idle. Set on mailsync.FolderStarted; cleared on
	// mailsync.FolderDone (the engine re-sets it via the next FolderStarted
	// when the next folder begins). Scoped to the active account.
	syncingFolder string
	// syncedCount is the cumulative number of messages added so far for the
	// in-flight folder sync. Updated from FolderDone.Added (which is
	// cumulative across the batched 50-at-a-time fetches).
	syncedCount int
	// syncSpinner animates the in-progress chip in the header. Only ticked
	// while syncingFolder != "".
	syncSpinner spinner.Model
}

// New constructs the root Model. engine, sender, and activeAcct may be nil
// when no account is configured; all code paths tolerate that.
//
// accountID and inboxFolderID resolve the initial mail list context. When the
// caller cannot resolve either (no store / no account) they may pass 0; the
// mail list will render empty until a sync populates the store.
func New(store mail.MessageStore, engine *mailsync.Engine, sender *mailsend.Engine, activeAcct *config.Account, cfg *config.Config, accountID, inboxFolderID int64, runner *accountrunner.Runner) Model {
	wk := whichkey.New()

	folderDrawer := folderdrawer.New(nil)

	acctDrawer := accountdrawer.New(func(name string) tea.Msg {
		return accountSelectedMsg{name: name}
	})

	sp := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().
			Background(styles.ColorOverlay).
			Foreground(styles.ColorAccent).
			Bold(true)),
	)

	m := Model{
		stack:         []View{newMailList(store, cfg, sender, inboxFolderID, "Inbox", mail.RoleInbox)},
		wk:            wk,
		folderDrawer:  folderDrawer,
		accountDrawer: acctDrawer,
		pathPicker:    pathpicker.New(),
		store:         store,
		engine:        engine,
		sender:        sender,
		runner:        runner,
		activeAcct:    activeAcct,
		cfg:           cfg,
		accountID:     accountID,
		curFolder:     inboxFolderID,
		synced:        time.Now(),
		syncSpinner:   sp,
	}
	if activeAcct != nil {
		m.account = activeAcct.Email
		m.accountTag = activeAcct.Name
	}
	folderDrawer.SetOnSelect(m.defaultFolderOnSelect())
	wk.SetKeyMap(m.top().KeyMap())
	return m
}

// defaultFolderOnSelect returns the folder drawer callback used in normal
// navigate mode. Called at construction time and re-installed after a move
// operation to restore navigate behaviour.
func (m *Model) defaultFolderOnSelect() func(*mail.Folder) tea.Msg {
	return func(f *mail.Folder) tea.Msg {
		return folderSelectedMsg{
			folderID:   f.ID,
			folderName: f.Name,
			folderRole: f.Role,
		}
	}
}

func (m Model) top() View {
	return m.stack[len(m.stack)-1]
}

// popupHeight is the number of rows the whichkey popup consumes right now.
func (m Model) popupHeight() int {
	if m.wk == nil {
		return 0
	}
	return m.wk.Height(m.width)
}

// contentHeight returns the current height available to the top view's body.
// It is always a function of m.height and the live popup height so views
// resize smoothly as the popup opens/closes.
func (m Model) contentHeight() int {
	h := m.height - chromeHeight - m.popupHeight()
	if m.pathPicker != nil {
		h -= m.pathPicker.Height(m.width)
	}
	if h < 1 {
		return 1
	}
	return h
}

// contentWidth returns the horizontal space available to the top view after
// subtracting the folder drawer column (when open).
func (m Model) contentWidth() int {
	w := m.width
	if m.folderDrawer != nil {
		w -= m.folderDrawer.Width()
	}
	if m.accountDrawer != nil {
		w -= m.accountDrawer.Width()
	}
	if w < 10 {
		return 10
	}
	return w
}

// closeOtherOverlays closes any left-column drawer other than `keep`. Pass
// one of "folder", "account", or "" (close both). This enforces the mutual
// exclusion between the folder drawer and the account drawer; the path
// picker lives on a separate row so it is unaffected.
func (m *Model) closeOtherOverlays(keep string) {
	if keep != "folder" && m.folderDrawer != nil && m.folderDrawer.IsOpen() {
		m.folderDrawer.Close()
	}
	if keep != "account" && m.accountDrawer != nil && m.accountDrawer.IsOpen() {
		m.accountDrawer.Close()
	}
}

// buildContactSuggestFunc returns a compose.SuggestFunc closure bound to the
// current store and active accountID. The closure runs the SearchContacts
// call off the UI goroutine (as a tea.Cmd) and wraps the result in a
// compose.ContactSuggestionsMsg that the chip input will consume.
//
// Returns nil when there's no store or no active account yet — the chip
// input treats a nil SuggestFunc as "autocomplete disabled" and renders
// normally without a dropdown.
func (m Model) buildContactSuggestFunc() compose.SuggestFunc {
	if m.store == nil || m.accountID == 0 {
		return nil
	}
	store := m.store
	accountID := m.accountID
	return func(prefix string, qID uint64) tea.Cmd {
		return func() tea.Msg {
			const limit = 8
			results, err := store.SearchContacts(accountID, prefix, limit)
			if err != nil {
				return compose.ContactSuggestionsMsg{QueryID: qID}
			}
			return compose.ContactSuggestionsMsg{QueryID: qID, Results: results}
		}
	}
}

// popIfRequested pops the top view when the stack has more than one entry
// and `want` is true. Called by markSeen/delete/archive handlers to leave
// the mail-read view after acting on a message.
func (m *Model) popIfRequested(want bool) {
	if !want || len(m.stack) <= 1 {
		return
	}
	m.stack = m.stack[:len(m.stack)-1]
	m.wk.SetKeyMap(m.top().KeyMap())
	m.resizeTop()
}

// reloadMailListsCmd rebuilds the batch of commands needed after a mutating
// operation on a message: refresh folder counts and reinitialise every
// mail-list view in the stack so they re-query the store.
func (m Model) reloadMailListsCmd(folderID int64) tea.Cmd {
	cmds := []tea.Cmd{loadFolderCountsCmd(m.store, folderID)}
	for _, v := range m.stack {
		if _, ok := v.(mailList); ok {
			cmds = append(cmds, v.Init())
		}
	}
	return tea.Batch(cmds...)
}

// resizeDrawer forwards the current terminal size to the drawer. Called on
// window resize and whenever the drawer opens / closes so its column keeps
// its height.
func (m *Model) resizeDrawer() {
	h := m.contentHeight()
	if m.folderDrawer != nil {
		m.folderDrawer.SetSize(m.width, h)
	}
	if m.accountDrawer != nil {
		m.accountDrawer.SetSize(m.width, h)
	}
}

func (m Model) Init() tea.Cmd {
	// Watch is started by cmd/root.go; no sync command needed here.
	return m.top().Init()
}

// resizeTop re-applies the current content size to the top view only. Used
// when the popup opens / closes so the top view can collapse / expand.
func (m *Model) resizeTop() {
	if len(m.stack) == 0 || m.width == 0 {
		return
	}
	top := m.stack[len(m.stack)-1]
	m.stack[len(m.stack)-1] = top.SetSize(m.contentWidth(), m.contentHeight())
}

// inputCapturer is implemented by views that have a mode in which they
// swallow free-text keystrokes. The compose view implements this via its
// form stage; the mail list uses its filter state (checked explicitly
// below). When any top view reports capturing, <space> is NOT intercepted
// to open the whichkey popup.
type inputCapturer interface {
	IsCapturingInput() bool
}

// viewIsCapturingInput reports whether the top view is in a mode where it
// swallows free-text keystrokes (e.g. the list's filter prompt or the
// compose form). While true, the whichkey hotkey (<space>) is deliberately
// NOT intercepted.
func (m Model) viewIsCapturingInput() bool {
	if m.pathPicker != nil && m.pathPicker.IsCapturingInput() {
		return true
	}
	if m.folderDrawer != nil && m.folderDrawer.IsOpen() && m.folderDrawer.IsCapturingInput() {
		return true
	}
	if m.accountDrawer != nil && m.accountDrawer.IsOpen() && m.accountDrawer.IsCapturingInput() {
		return true
	}
	if ml, ok := m.top().(mailList); ok {
		fs := ml.list.FilterState()
		if fs == list.Filtering {
			return true
		}
	}
	if cap, ok := m.top().(inputCapturer); ok {
		if cap.IsCapturingInput() {
			return true
		}
	}
	return false
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeDrawer()
		for i, v := range m.stack {
			m.stack[i] = v.SetSize(m.contentWidth(), m.contentHeight())
		}
		return m, nil

	case pushMsg:
		// Inject compose-time dependencies before the view is sized or
		// initialised. The compose view needs a SuggestFunc bound to the
		// live store + active account so its recipient fields can query
		// contact suggestions. This is wired here (not at construction)
		// because the stack is built across several packages that don't
		// know about accountID.
		if cv, ok := msg.view.(*composeView); ok {
			cv.SetSuggestFunc(m.buildContactSuggestFunc())
			if m.activeAcct != nil {
				cv.form.SetFromAccount(m.activeAcct.Email)
			}
		}
		v := msg.view.SetSize(m.contentWidth(), m.contentHeight())
		m.stack = append(m.stack, v)
		m.wk.SetKeyMap(v.KeyMap())
		return m, v.Init()

	case popMsg:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
			m.wk.SetKeyMap(m.top().KeyMap())
		}
		return m, nil

	case mailsync.FolderStarted:
		// The engine began pulling a folder from the server. Light up the
		// header indicator and kick the spinner. The Added count resets to 0
		// — the next FolderDone will populate it.
		wasIdle := m.syncingFolder == ""
		m.syncingFolder = msg.Folder
		m.syncedCount = 0
		if wasIdle {
			return m, m.syncSpinner.Tick
		}
		return m, nil

	case mailsync.FolderDone:
		// Reload the mail list to show newly arrived messages, refresh
		// the header counts from the store, and refresh the folder drawer.
		m.synced = time.Now()
		// Keep the spinner running until the engine signals it is truly done
		// (Final == true). Intermediate batched emissions just update the count.
		if m.syncingFolder == msg.Folder && msg.Added > m.syncedCount {
			m.syncedCount = msg.Added
		}
		if msg.Final {
			m.syncingFolder = ""
		}
		// If a folder switch was pending (SwitchFolder failed on select because
		// the watcher wasn't connected yet), retry now that the watcher is live.
		if m.pendingFolderSwitch != "" && m.engine != nil && m.activeAcct != nil {
			if err := m.engine.SwitchFolder(m.activeAcct.Name, m.pendingFolderSwitch); err == nil {
				m.pendingFolderSwitch = ""
			}
		}
		cmds := []tea.Cmd{
			m.top().Init(),
			loadFolderCountsCmd(m.store, m.curFolder),
		}
		if msg.Final {
			cmds = append(cmds, loadFoldersCmd(m.store, m.accountID))
		}
		return m, tea.Batch(cmds...)

	case spinner.TickMsg:
		// Only advance the spinner while a sync is in flight. When idle we
		// drop the tick on the floor so the ticker chain naturally stops.
		if m.syncingFolder == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.syncSpinner, cmd = m.syncSpinner.Update(msg)
		return m, cmd

	case folderCountsMsg:
		m.total = msg.total
		m.unread = msg.unread
		return m, nil

	case openFolderDrawerMsg:
		if m.folderDrawer == nil {
			return m, nil
		}
		m.folderDrawer.SetOnSelect(m.defaultFolderOnSelect())
		m.folderDrawer.SetBlockCurrent(false)
		m.closeOtherOverlays("folder")
		m.folderDrawer.Open(m.curFolder)
		m.resizeDrawer()
		m.resizeTop()
		return m, loadFoldersCmd(m.store, m.accountID)

	case openMoveToFolderMsg:
		if m.folderDrawer == nil || msg.uid == 0 || msg.folderID == 0 {
			return m, nil
		}
		srcFolderID, uid, popAfter := msg.folderID, msg.uid, msg.popAfter
		m.folderDrawer.SetOnSelect(func(f *mail.Folder) tea.Msg {
			return moveMessageMsg{
				srcFolderID: srcFolderID,
				uid:         uid,
				dst:         f,
				popAfter:    popAfter,
			}
		})
		m.folderDrawer.SetBlockCurrent(true)
		m.closeOtherOverlays("folder")
		m.folderDrawer.Open(srcFolderID)
		m.resizeDrawer()
		m.resizeTop()
		return m, loadFoldersCmd(m.store, m.accountID)

	case openAccountDrawerMsg:
		// Compose guard — don't open while composing.
		if _, isCompose := m.top().(*composeView); isCompose {
			m.watchStatus = "finish compose before switching"
			return m, nil
		}
		if m.accountDrawer == nil {
			return m, nil
		}
		m.closeOtherOverlays("account")
		activeName := ""
		if m.activeAcct != nil {
			activeName = m.activeAcct.Name
		}
		m.accountDrawer.Open(activeName)
		m.resizeDrawer()
		m.resizeTop()
		return m, loadAccountStatsCmd(m.store, m.cfg)

	case accountsLoadedMsg:
		if m.accountDrawer != nil {
			m.accountDrawer.SetAccounts([]accountdrawer.AccountStat(msg))
		}
		return m, nil

	case accountSelectedMsg:
		// Soft-dismiss if already on this account.
		if m.activeAcct != nil && m.activeAcct.Name == msg.name {
			return m, nil
		}
		// Optimistic header update.
		for i := range m.cfg.Accounts {
			if m.cfg.Accounts[i].Name == msg.name {
				m.account = m.cfg.Accounts[i].Email
				m.accountTag = m.cfg.Accounts[i].Name
				break
			}
		}
		m.syncingAcct = msg.name
		m.watchStatus = "connecting…"
		return m, doSwitchAccountCmd(m.runner, m.store, m.cfg, msg.name)

	case accountSwitchedMsg:
		m.activeAcct = msg.acct
		m.accountID = msg.accountID
		m.curFolder = msg.inboxFolderID
		m.curFolderName = "INBOX"
		m.pendingFolderSwitch = ""
		if msg.acct != nil {
			m.account = msg.acct.Email
			m.accountTag = msg.acct.Name
		}
		m.syncingAcct = ""
		m.unread = 0
		m.total = 0
		m.synced = time.Now()
		m.syncingFolder = ""
		m.syncedCount = 0
		m.flashAcct = true
		// Reset stack to a fresh mail list; old views hold stale PKs.
		m.stack = []View{newMailList(m.store, m.cfg, m.sender, msg.inboxFolderID, "Inbox", mail.RoleInbox)}
		m.stack[0] = m.stack[0].SetSize(m.contentWidth(), m.contentHeight())
		m.wk.SetKeyMap(m.top().KeyMap())
		return m, tea.Batch(
			m.top().Init(),
			loadFolderCountsCmd(m.store, msg.inboxFolderID),
			loadFoldersCmd(m.store, msg.accountID),
			clearFlashAfter(400*time.Millisecond),
		)

	case accountSwitchFailedMsg:
		m.syncingAcct = ""
		m.watchStatus = msg.name + ": connection failed"
		return m, nil

	case clearFlashMsg:
		m.flashAcct = false
		return m, nil

	case cycleAccountMsg:
		if len(m.cfg.Accounts) < 2 || m.activeAcct == nil {
			return m, nil
		}
		cur := -1
		for i, a := range m.cfg.Accounts {
			if a.Name == m.activeAcct.Name {
				cur = i
				break
			}
		}
		if cur < 0 {
			cur = 0
		}
		next := (cur + msg.delta + len(m.cfg.Accounts)) % len(m.cfg.Accounts)
		return m, func() tea.Msg { return accountSelectedMsg{name: m.cfg.Accounts[next].Name} }

	case foldersLoadedMsg:
		if msg.accountID != m.accountID {
			return m, nil // stale response for a previous account; discard
		}
		if m.folderDrawer != nil {
			m.folderDrawer.SetFolders(msg.folders)
		}
		return m, nil

	case folderSelectedMsg:
		m.curFolder = msg.folderID
		m.curFolderName = msg.folderName
		if m.folderDrawer != nil {
			m.folderDrawer.Close()
			m.resizeDrawer()
		}
		for i, v := range m.stack {
			if ml, ok := v.(mailList); ok {
				ml.folderID = msg.folderID
				ml.folderName = msg.folderName
				ml.folderRole = msg.folderRole
				m.stack[i] = ml
			}
		}
		m.resizeTop()
		cmds := []tea.Cmd{
			m.top().Init(),
			loadFolderCountsCmd(m.store, msg.folderID),
		}
		if m.engine != nil && m.activeAcct != nil && msg.folderName != "" {
			if err := m.engine.SwitchFolder(m.activeAcct.Name, msg.folderName); err != nil {
				// Watcher not yet connected (scheduling race after account switch).
				// Retry when WatchStarted or FolderDone arrives.
				m.pendingFolderSwitch = msg.folderName
			}
		}
		return m, tea.Batch(cmds...)

	case mailsync.FolderAdded:
		if m.activeAcct == nil || msg.Account != m.activeAcct.Name {
			return m, nil
		}
		return m, loadFoldersCmd(m.store, m.accountID)

	case mailsync.FolderRenamed:
		if m.activeAcct == nil || msg.Account != m.activeAcct.Name {
			return m, nil
		}
		if m.curFolder == msg.FolderID {
			m.curFolderName = msg.NewName
			for i, v := range m.stack {
				if ml, ok := v.(mailList); ok && ml.folderID == msg.FolderID {
					ml.folderName = msg.NewName
					m.stack[i] = ml
				}
			}
		}
		if m.pendingFolderSwitch == msg.OldName {
			m.pendingFolderSwitch = ""
		}
		return m, loadFoldersCmd(m.store, m.accountID)

	case mailsync.FolderDisappeared:
		if m.activeAcct == nil || msg.Account != m.activeAcct.Name {
			return m, nil
		}
		cmds := []tea.Cmd{loadFoldersCmd(m.store, m.accountID)}
		if !msg.Grace && m.curFolder == msg.FolderID && m.store != nil {
			if f, err := m.store.FolderByName(m.accountID, "INBOX"); err == nil && f != nil {
				inboxID := f.ID
				inboxName := f.Name
				inboxRole := f.Role
				cmds = append(cmds, func() tea.Msg {
					return folderSelectedMsg{
						folderID:   inboxID,
						folderName: inboxName,
						folderRole: inboxRole,
					}
				})
			}
		}
		return m, tea.Batch(cmds...)

	case mailsync.WatchStarted:
		m.watchStatus = "connecting…"
		// Retry any pending folder switch — the watcher handle is now registered
		// so SwitchFolder can enqueue the folder name for when the poll loop starts.
		if m.pendingFolderSwitch != "" && m.engine != nil && m.activeAcct != nil {
			if err := m.engine.SwitchFolder(m.activeAcct.Name, m.pendingFolderSwitch); err == nil {
				m.pendingFolderSwitch = ""
			}
		}
		return m, nil

	case mailsync.WatchStopped:
		m.watchStatus = "disconnected"
		return m, nil

	case mailsync.WatchError:
		// Non-fatal: log and let the watcher retry. Store last error string
		// for display in the chrome.
		m.watchStatus = "reconnecting…"
		m.engine.Logger().Info("watch error", "account", msg.Account, "err", msg.Err)
		return m, nil

	case mailsend.WorkerStarted:
		return m, nil

	case mailsend.WorkerStopped:
		m.sendStatus = ""
		return m, nil

	case mailsend.WorkerError:
		m.sendStatus = "send error, retrying…"
		return m, nil

	case mailsend.SendSucceeded:
		m.sendStatus = "sent"
		return m, clearSendStatusAfter(4 * time.Second)

	case mailsend.SendFailed:
		m.sendStatus = "send failed"
		return m, nil

	case clearSendStatusMsg:
		m.sendStatus = ""
		return m, nil

	case refreshMsg:
		// Poke the active watcher — it will emit FolderDone which triggers reload.
		if m.engine != nil && m.activeAcct != nil {
			_ = m.engine.Refresh(m.activeAcct.Name)
		}
		return m, nil

	case markSeenMsg:
		if msg.uid == 0 || msg.folderID == 0 || m.store == nil {
			return m, nil
		}
		newFlags := msg.flags
		if msg.seen {
			newFlags |= mail.FlagSeen
		} else {
			newFlags &^= mail.FlagSeen
		}
		changed := newFlags != msg.flags
		if changed {
			_ = m.store.UpdateFlags(msg.folderID, msg.uid, newFlags)
			if m.engine != nil && m.activeAcct != nil {
				_ = m.engine.MarkSeen(m.activeAcct.Name, msg.folderID, msg.uid, msg.seen)
			}
		}
		m.popIfRequested(msg.popAfter)
		if !changed {
			return m, nil
		}
		return m, m.reloadMailListsCmd(msg.folderID)

	case markFlaggedMsg:
		if msg.uid == 0 || msg.folderID == 0 || m.store == nil {
			return m, nil
		}
		newFlags := msg.flags
		if msg.flagged {
			newFlags |= mail.FlagFlagged
		} else {
			newFlags &^= mail.FlagFlagged
		}
		changed := newFlags != msg.flags
		if changed {
			_ = m.store.UpdateFlags(msg.folderID, msg.uid, newFlags)
			if m.engine != nil && m.activeAcct != nil {
				_ = m.engine.MarkFlagged(m.activeAcct.Name, msg.folderID, msg.uid, msg.flagged)
			}
			// Update cached item in any mailRead in the stack so the info-bar
			// reflects the new flag state without a round-trip reload.
			for i, v := range m.stack {
				if mr, ok := v.(mailRead); ok && mr.item.uid == msg.uid && mr.item.folderID == msg.folderID {
					mr.item.flags = newFlags
					mr.item.flagged = msg.flagged
					m.stack[i] = mr
					break
				}
			}
		}
		m.popIfRequested(msg.popAfter)
		if !changed {
			return m, nil
		}
		return m, m.reloadMailListsCmd(msg.folderID)

	case messagesLoadedMsg:
		// Route to the mailList in the stack regardless of which view is on top.
		// Without this, messagesLoadedMsg dispatched from a background Init() would
		// be silently dropped by mailRead (which sits on top when auto-marking read).
		for i, v := range m.stack {
			if ml, ok := v.(mailList); ok {
				updated, cmd := ml.Update(msg)
				m.stack[i] = updated
				return m, cmd
			}
		}

	case deleteMsg:
		if msg.uid == 0 || msg.folderID == 0 || m.store == nil {
			return m, nil
		}
		_ = m.store.DeleteMessageByUID(msg.folderID, msg.uid)
		if m.engine != nil && m.activeAcct != nil {
			_ = m.engine.MoveToTrash(*m.activeAcct, msg.folderID, msg.uid)
		}
		m.popIfRequested(msg.popAfter)
		return m, m.reloadMailListsCmd(msg.folderID)

	case archiveMsg:
		if msg.uid == 0 || msg.folderID == 0 || m.store == nil {
			return m, nil
		}
		_ = m.store.DeleteMessageByUID(msg.folderID, msg.uid)
		if m.engine != nil && m.activeAcct != nil {
			_ = m.engine.MoveToArchive(*m.activeAcct, msg.folderID, msg.uid)
		}
		m.popIfRequested(msg.popAfter)
		return m, m.reloadMailListsCmd(msg.folderID)

	case moveMessageMsg:
		// Restore navigate-mode callback regardless of outcome.
		if m.folderDrawer != nil {
			m.folderDrawer.SetOnSelect(m.defaultFolderOnSelect())
			m.folderDrawer.SetBlockCurrent(false)
			m.resizeDrawer()
		}
		if msg.dst == nil || msg.uid == 0 || msg.srcFolderID == 0 || m.store == nil {
			return m, nil
		}
		if msg.dst.ID == msg.srcFolderID {
			return m, nil
		}
		_ = m.store.DeleteMessageByUID(msg.srcFolderID, msg.uid)
		if m.engine != nil && m.activeAcct != nil {
			_ = m.engine.MoveToFolder(*m.activeAcct, msg.srcFolderID, msg.uid, msg.dst.Name)
		}
		m.popIfRequested(msg.popAfter)
		m.resizeTop()
		return m, m.reloadMailListsCmd(msg.srcFolderID)

	case openPathPickerMsg:
		if m.pathPicker == nil {
			return m, nil
		}
		m.closeOtherOverlays("")
		m.pathPicker.SetSize(m.width, m.contentHeight()+m.pathPicker.Height(m.width))
		m.pathPicker.Open(msg.cfg)
		m.resizeDrawer()
		m.resizeTop()
		return m, nil

	case saveAttachmentMsg:
		note := ""
		if _, err := os.Stat(msg.src); err != nil {
			note = "source missing — re-sync needed"
		} else if err := copyFile(msg.src, msg.dst); err != nil {
			note = "save failed: " + err.Error()
		} else {
			note = "saved to " + msg.dst
		}
		return m, func() tea.Msg { return attachmentSavedMsg{note: note} }

	case attachmentSavedMsg:
		// Forward to the top view (mailread will handle it via its openNote field).
		top, cmd := m.top().Update(msg)
		m.stack[len(m.stack)-1] = top
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	top, cmd := m.top().Update(msg)
	m.stack[len(m.stack)-1] = top
	return m, cmd
}

// handleKey is the key-event entry point. The whichkey popup takes priority
// when open; otherwise <space> opens it (unless the view is capturing text),
// and everything else falls through to the top view.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Account drawer takes precedence when open.
	if m.accountDrawer != nil && m.accountDrawer.IsOpen() {
		cmd := m.accountDrawer.Update(msg)
		m.resizeDrawer()
		m.resizeTop()
		return m, cmd
	}

	// Folder drawer takes precedence when open — it owns navigation and
	// filter input while visible.
	if m.folderDrawer != nil && m.folderDrawer.IsOpen() {
		cmd := m.folderDrawer.Update(msg)
		m.resizeDrawer()
		m.resizeTop()
		return m, cmd
	}

	// Path picker takes full input focus when open.
	if m.pathPicker != nil && m.pathPicker.IsOpen() {
		cmd := m.pathPicker.Update(msg)
		m.resizeTop()
		return m, cmd
	}

	// Popup already open — route every key to it.
	if m.wk.IsOpen() {
		res := m.wk.Handle(key)
		switch res.Status {
		case whichkey.StatusPending, whichkey.StatusCancelled:
			// Re-sync top-view size (popup height likely changed).
			m.resizeTop()
			return m, nil
		case whichkey.StatusAccepted:
			m.resizeTop()
			return m.applyResult(res.Result)
		}
		return m, nil
	}

	// Open the popup on <space>, unless the view is in text-capture mode.
	if (key == " " || key == "space") && !m.viewIsCapturingInput() {
		if len(m.top().KeyMap().Leader) > 0 {
			m.wk.SetKeyMap(m.top().KeyMap())
			m.wk.Open()
			m.resizeTop()
			return m, nil
		}
	}

	// Fall through to the view.
	top, cmd := m.top().Update(msg)
	m.stack[len(m.stack)-1] = top
	return m, cmd
}

// applyResult translates a keymap.Result from a whichkey leaf into bubbletea
// state changes (push / pop / quit / passthrough).
func (m Model) applyResult(r keymap.Result) (tea.Model, tea.Cmd) {
	if r.Quit {
		return m, tea.Quit
	}
	if r.Pop {
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
			m.wk.SetKeyMap(m.top().KeyMap())
			m.resizeTop()
		}
		return m, nil
	}
	if r.Push != nil {
		if v, ok := r.Push.(View); ok {
			return m, Push(v)
		}
	}
	// Forward any non-nil message result as a tea.Cmd.
	if r.Message != nil {
		msg := r.Message
		return m, func() tea.Msg { return msg }
	}
	// Note-only results (stubs) and empty results do nothing visible yet.
	return m, nil
}

// renderHeader paints a single full-width header row plus a thin rule.
//
//	row 1 — header:  pmx │ user@example.com [work] │ Inbox > Weekend plans?   247 msgs · 3 unread · synced 2m ago
//	row 2 — rule:    ────────────────────────────────────────────
func (m Model) renderHeader() string {
	w := m.width
	if w <= 0 {
		return ""
	}

	// ── Row 1: merged header ────────────────────────────────────────────
	brand := styles.HeaderAppName.Render(" pmx ")

	accIcon := styles.HeaderAccountIcon.Render(" ")
	acc := styles.HeaderAccount.Render(m.account)
	accTag := ""
	if m.accountTag != "" {
		tagStyle := styles.HeaderMeta
		if m.flashAcct {
			tagStyle = styles.HeaderAccent
		}
		accTag = styles.HeaderBar.Render(" ") + tagStyle.Render("["+m.accountTag+"]")
	}
	accChevron := ""
	if m.cfg != nil && len(m.cfg.Accounts) > 1 {
		accChevron = styles.HeaderMeta.Render(" " + styles.AccountChevron)
	}

	// Breadcrumb: last segment emphasized, earlier ones subdued.
	titles := make([]string, len(m.stack))
	for i, v := range m.stack {
		titles[i] = v.Title()
	}
	var crumb string
	for i, t := range titles {
		if i == len(titles)-1 {
			crumb += styles.HeaderBreadcrumbCur.Render(t)
		} else {
			crumb += styles.HeaderBreadcrumb.Render(t) +
				styles.HeaderSep.Render("  ›  ")
		}
	}

	sep := styles.HeaderSep.Render("  │  ")
	left := brand + sep + accIcon + acc + accTag + accChevron + sep + crumb

	// Right side: counts + sync time.
	countStr := styles.HeaderMetaValue.Render(strconv.Itoa(m.total)) +
		styles.HeaderMeta.Render(" msgs")
	unreadStr := ""
	if m.unread > 0 {
		unreadStr = styles.HeaderMeta.Render(" · ") +
			styles.HeaderAccent.Render("● ") +
			styles.HeaderMetaValue.Render(strconv.Itoa(m.unread)) +
			styles.HeaderMeta.Render(" unread")
	}
	// Sync chip: either a live "pulling …" indicator with a spinner and
	// cumulative message count, or the idle "synced <rel> ago" timestamp.
	var syncStr string
	if m.syncingFolder != "" {
		spin := m.syncSpinner.View()
		chip := styles.HeaderMeta.Render(" · ") +
			styles.HeaderBar.Render(spin) +
			styles.HeaderBar.Render(" ") +
			styles.HeaderAccent.Render(m.syncingFolder)
		if m.syncedCount > 0 {
			chip += styles.HeaderMeta.Render("  ") +
				styles.HeaderMetaValue.Render(strconv.Itoa(m.syncedCount)) +
				styles.HeaderMeta.Render(" msgs")
		}
		syncStr = chip
	} else {
		syncStr = styles.HeaderMeta.Render(" · ") +
			styles.HeaderMeta.Render("synced ") +
			styles.HeaderMetaValue.Render(relTime(m.synced))
	}

	sendStr := ""
	if m.sendStatus != "" {
		sendStr = styles.HeaderMeta.Render(" · ") +
			styles.HeaderMetaValue.Render(m.sendStatus)
	}

	right := countStr + unreadStr + syncStr + sendStr + styles.HeaderBar.Render(" ")

	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		// Soft-degrade: on narrow widths drop the sync chip first (whether
		// it's the live indicator or the idle timestamp), then the counts.
		right = countStr + unreadStr + sendStr + styles.HeaderBar.Render(" ")
		gap = w - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 0 {
		right = countStr + unreadStr + styles.HeaderBar.Render(" ")
		gap = w - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 0 {
		right = unreadStr + styles.HeaderBar.Render(" ")
		gap = w - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 0 {
		gap = 0
	}
	row1 := left + styles.HeaderBar.Render(strings.Repeat(" ", gap)) + right

	// ── Row 2: hairline rule ────────────────────────────────────────────
	row2 := styles.HeaderRule.Render(strings.Repeat("─", w))

	return lipgloss.JoinVertical(lipgloss.Left, row1, row2)
}

// renderStatusBar paints the bottom row. The top view supplies key hints via
// a lightweight DSL (see keyHint helpers); we add a mode chip on the left
// and a context chip on the right. When the whichkey popup is open the hints
// are replaced with a leader-sequence breadcrumb.
func (m Model) renderStatusBar() string {
	w := m.width
	if w <= 0 {
		return ""
	}

	var modeStr, hints string
	switch {
	case m.pathPicker != nil && m.pathPicker.IsOpen():
		modeStr = styles.StatusMode.Render(" PATH ")
		hints = m.pathPicker.StatusKeys()
	case m.accountDrawer != nil && m.accountDrawer.IsOpen():
		modeStr = lipgloss.NewStyle().
			Background(styles.ColorPrimaryHi).
			Foreground(styles.ColorBg).
			Bold(true).
			Padding(0, 1).
			Render(" ACCOUNTS ")
		hints = m.accountDrawer.StatusKeys()
	case m.folderDrawer != nil && m.folderDrawer.IsOpen():
		modeStr = lipgloss.NewStyle().
			Background(styles.ColorPrimaryHi).
			Foreground(styles.ColorBg).
			Bold(true).
			Padding(0, 1).
			Render(" FOLDERS ")
		hints = m.folderDrawer.StatusKeys()
	case m.wk.IsOpen():
		modeStr = styles.StatusModeLeader.Render(" LEADER ")
		hints = m.renderLeaderStatus()
	default:
		modeStr = styles.StatusMode.Render(" NORMAL ")
		hints = m.top().StatusKeys()
	}
	modeSpacer := styles.StatusBar.Render(" ")

	// Right: version / position chip.
	right := styles.StatusSep.Render("│ ") +
		styles.StatusKeyLabel.Render("pmx ") +
		styles.StatusBar.Render(" ") +
		styles.StatusKey.Render("v0.1")
	rightPad := styles.StatusBar.Render(" ")

	left := modeStr + modeSpacer + hints

	gap := w - lipgloss.Width(left) - lipgloss.Width(right) - lipgloss.Width(rightPad)
	if gap < 1 {
		// Narrow — drop version chip.
		right = ""
		rightPad = ""
		gap = w - lipgloss.Width(left)
	}
	if gap < 0 {
		gap = 0
	}

	return styles.StatusBar.Render("") +
		left +
		styles.StatusBar.Render(strings.Repeat(" ", gap)) +
		right + rightPad
}

// renderLeaderStatus formats the status-bar hint shown while the whichkey
// popup is open: "esc  cancel  ·  ⌫  up  ·  keep typing…"
func (m Model) renderLeaderStatus() string {
	bar := styles.StatusBar
	sep := bar.Render("  ") + styles.StatusSep.Render("·") + bar.Render("  ")

	esc := styles.StatusKey.Render("esc") + bar.Render(" ") +
		styles.StatusKeyLabel.Render("cancel")
	back := styles.StatusKey.Render("⌫") + bar.Render(" ") +
		styles.StatusKeyLabel.Render("up one")
	typing := styles.StatusKeyLabel.Italic(true).Render("keep typing…")

	return bar.Render("  ") + esc + sep + back + sep + typing
}

func (m Model) View() string {
	if m.width == 0 {
		return "initializing…"
	}

	content := m.top().View()
	if m.syncingAcct != "" {
		content = m.renderSyncingPlaceholder()
	}
	if m.folderDrawer != nil && m.folderDrawer.IsOpen() {
		content = lipgloss.JoinHorizontal(lipgloss.Top, m.folderDrawer.View(), content)
	} else if m.accountDrawer != nil && m.accountDrawer.IsOpen() {
		content = lipgloss.JoinHorizontal(lipgloss.Top, m.accountDrawer.View(), content)
	}
	parts := []string{m.renderHeader(), content}
	if m.pathPicker != nil && m.pathPicker.IsOpen() {
		if p := m.pathPicker.View(m.width); p != "" {
			parts = append(parts, p)
		}
	}
	if m.wk.IsOpen() {
		if pop := m.wk.View(m.width); pop != "" {
			parts = append(parts, pop)
		}
	}
	parts = append(parts, m.renderStatusBar())

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// keyHint renders one key+label pair for the status bar.
func keyHint(k, label string) string {
	return styles.StatusKey.Render(k) +
		styles.StatusBar.Render(" ") +
		styles.StatusKeyLabel.Render(label)
}

// keyHintGroup joins key hints with a bullet separator.
func keyHintGroup(pairs ...[2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, keyHint(p[0], p[1]))
	}
	sep := styles.StatusBar.Render("  ") + styles.StatusSep.Render("·") + styles.StatusBar.Render("  ")
	return styles.StatusBar.Render("  ") + strings.Join(parts, sep)
}

// keyHintsFromKeyMap derives the status-bar hint string directly from a
// view's KeyMap primary bindings. This is the preferred way to populate
// StatusKeys() — no duplication between the hint bar and the popup.
func keyHintsFromKeyMap(km keymap.KeyMap) string {
	if len(km.Primary) == 0 {
		// Fallback — at least show the leader hint.
		return keyHintGroup([2]string{"␣", "leader"})
	}
	pairs := make([][2]string, 0, len(km.Primary)+1)
	for _, b := range km.Primary {
		pairs = append(pairs, [2]string{b.Label(), b.Desc})
	}
	// Append the leader hint if there are leader bindings — tells users
	// about the <spc> popup without crowding each view's declaration.
	if len(km.Leader) > 0 {
		pairs = append(pairs, [2]string{"␣", "leader"})
	}
	return keyHintGroup(pairs...)
}

// relTime returns a short human-readable duration since t ("just now", "2m",
// "1h", "3d"). Good enough for the sync indicator.
func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 30*time.Second:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}
