package mailsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/folders"
	"github.com/5Sigma/pmx/internal/imapauth"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/secrets"
	"github.com/5Sigma/pmx/internal/workerloop"
)

// ErrSyncInProgress is returned when SyncAccount is called while a sync is
// already running for this engine instance.
var ErrSyncInProgress = errors.New("sync already in progress")

// ErrNotWatching is returned by Refresh when no active watcher exists for the
// named account.
var ErrNotWatching = errors.New("no active watcher for this account")

// flagOp is an asynchronous request to flip IMAP flags on the live watcher
// connection. The watcher goroutine drains these between IDLE/poll ticks.
type flagOp struct {
	folderID int64
	uid      uint32
	add      []imap.Flag
	remove   []imap.Flag
}

// moveOp is an asynchronous request to IMAP-MOVE a single message out of the
// currently-selected mailbox into dest. The watcher goroutine drains these
// between IDLE/poll ticks.
type moveOp struct {
	folderID int64
	uid      uint32
	dest     string
}

// watcherHandle holds the channels for a running watcher goroutine, plus a
// mu-guarded snapshot of its last known state (surfaced via WatcherState).
type watcherHandle struct {
	poke         chan struct{}
	flagOps      chan flagOp
	moveOps      chan moveOp
	switchFolder chan string   // buffered 1; request to re-SELECT a new mailbox
	sweep        chan struct{} // buffered 1; sweepLoop pokes the main IDLE/poll loop

	// Live state fields read via (*Engine).WatcherState. All reads and writes
	// go through mu so the Watch goroutine and the status screen can observe
	// each other without a race on handle teardown.
	mu        sync.Mutex
	running   bool
	idle      bool
	folder    string
	lastEvent time.Time
	lastErr   string
}

// Engine performs IMAP synchronisation and persists results via a MessageStore.
// A single Engine instance serialises syncs; concurrent calls return ErrSyncInProgress.
type Engine struct {
	store   mail.MessageStore
	secrets *secrets.Store
	attachs string
	logger  *slog.Logger
	mu      sync.Mutex
	busy    bool
	// watchers maps accountName to the handle for the active Watch goroutine.
	watchers map[string]*watcherHandle
	// specialUse caches IMAP SPECIAL-USE discovery results per account.
	// Populated on every connect; queried by MoveToTrash and future role-based ops.
	specialUse   map[string]map[mail.FolderRole]string
	specialUseMu sync.RWMutex
}

// Logger returns the engine's logger.
func (e *Engine) Logger() *slog.Logger { return e.logger }

// New creates an Engine. If logger is nil, slog.Default() is used.
func New(store mail.MessageStore, sec *secrets.Store, attachsDir string, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		store:      store,
		secrets:    sec,
		attachs:    attachsDir,
		logger:     logger,
		watchers:   make(map[string]*watcherHandle),
		specialUse: make(map[string]map[mail.FolderRole]string),
	}
}

// SyncAccount synchronises a single account. It closes the events channel when
// done (or on error), making it safe to range over. Passing a nil events
// channel suppresses progress reporting.
func (e *Engine) SyncAccount(ctx context.Context, acct config.Account, events chan<- Event) error {
	e.mu.Lock()
	if e.busy {
		e.mu.Unlock()
		return ErrSyncInProgress
	}
	e.busy = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.busy = false
		e.mu.Unlock()
		if events != nil {
			close(events)
		}
	}()

	emit := func(ev Event) {
		if events != nil {
			events <- ev
		}
	}

	emit(SyncStarted{Account: acct.Name})

	if err := e.syncFolder(ctx, acct, "INBOX", emit); err != nil {
		emit(SyncFailed{Account: acct.Name, Err: err})
		return err
	}

	emit(SyncCompleted{Account: acct.Name, At: time.Now()})
	return nil
}

// Refresh sends a poke to the active watcher for accountName, triggering an
// immediate catch-up sync without waiting for the next poll tick or server
// push. Returns ErrNotWatching if no Watch is running for this account.
func (e *Engine) Refresh(accountName string) error {
	e.mu.Lock()
	h, ok := e.watchers[accountName]
	e.mu.Unlock()
	if !ok {
		return ErrNotWatching
	}
	select {
	case h.poke <- struct{}{}:
	default: // already pending
	}
	return nil
}

// MarkSeen enqueues an IMAP STORE operation on the active watcher for
// accountName to add or remove \Seen. seen=true adds it; seen=false removes
// it. Returns ErrNotWatching if no watcher is running; the op is silently
// dropped if the queue is full — the next flag-refresh sync pass reconciles.
//
// Note: the local DB update is the caller's responsibility; this method
// only dispatches the server-side STORE.
func (e *Engine) MarkSeen(accountName string, folderID int64, uid uint32, seen bool) error {
	e.mu.Lock()
	h, ok := e.watchers[accountName]
	e.mu.Unlock()
	if !ok {
		return ErrNotWatching
	}
	op := flagOp{folderID: folderID, uid: uid}
	if seen {
		op.add = []imap.Flag{imap.FlagSeen}
	} else {
		op.remove = []imap.Flag{imap.FlagSeen}
	}
	select {
	case h.flagOps <- op:
		return nil
	default:
		e.logger.Warn("MarkSeen queue full, dropping", "account", accountName, "uid", uid)
		return nil
	}
}

// MarkFlagged enqueues an IMAP STORE operation to add or remove \Flagged.
// flagged=true adds it; flagged=false removes it. Returns ErrNotWatching if
// no watcher is running; the op is silently dropped if the queue is full.
//
// Note: the local DB update is the caller's responsibility.
func (e *Engine) MarkFlagged(accountName string, folderID int64, uid uint32, flagged bool) error {
	e.mu.Lock()
	h, ok := e.watchers[accountName]
	e.mu.Unlock()
	if !ok {
		return ErrNotWatching
	}
	op := flagOp{folderID: folderID, uid: uid}
	if flagged {
		op.add = []imap.Flag{imap.FlagFlagged}
	} else {
		op.remove = []imap.Flag{imap.FlagFlagged}
	}
	select {
	case h.flagOps <- op:
		return nil
	default:
		e.logger.Warn("MarkFlagged queue full, dropping", "account", accountName, "uid", uid)
		return nil
	}
}

// setSpecialUse stores discovered SPECIAL-USE folder mappings for acctName.
func (e *Engine) setSpecialUse(acctName string, discovered map[mail.FolderRole]string) {
	e.specialUseMu.Lock()
	e.specialUse[acctName] = discovered
	e.specialUseMu.Unlock()
}

// getSpecialUse returns the cached SPECIAL-USE map for acctName (may be nil).
func (e *Engine) getSpecialUse(acctName string) map[mail.FolderRole]string {
	e.specialUseMu.RLock()
	defer e.specialUseMu.RUnlock()
	return e.specialUse[acctName]
}

// MoveToTrash enqueues an IMAP MOVE on the active watcher to the account's
// trash mailbox. See moveToRole for semantics.
func (e *Engine) MoveToTrash(acct config.Account, folderID int64, uid uint32) error {
	return e.moveToRole(acct, folderID, uid, mail.RoleTrash)
}

// MoveToArchive enqueues an IMAP MOVE on the active watcher to the account's
// archive mailbox. See moveToRole for semantics.
func (e *Engine) MoveToArchive(acct config.Account, folderID int64, uid uint32) error {
	return e.moveToRole(acct, folderID, uid, mail.RoleArchive)
}

// MoveToFolder enqueues an IMAP MOVE on the active watcher to an explicit
// destination mailbox path. Mirrors moveToRole but takes the destination as a
// literal mailbox name rather than a role. The caller is responsible for the
// local DB delete.
func (e *Engine) MoveToFolder(acct config.Account, folderID int64, uid uint32, dest string) error {
	if dest == "" {
		return nil
	}
	e.mu.Lock()
	h, ok := e.watchers[acct.Name]
	e.mu.Unlock()
	if !ok {
		return ErrNotWatching
	}
	op := moveOp{folderID: folderID, uid: uid, dest: dest}
	select {
	case h.moveOps <- op:
		return nil
	default:
		e.logger.Warn("move queue full, dropping", "account", acct.Name, "uid", uid, "dest", dest)
		return nil
	}
}

// moveToRole enqueues an IMAP MOVE on the active watcher. The destination
// mailbox is resolved via SPECIAL-USE discovery and the account's
// per-role mailbox config field. Returns ErrNotWatching if no watcher is
// running; the op is silently dropped if the queue is full — the next sync
// pass reconciles.
//
// The caller is responsible for the local DB delete; this method only
// dispatches the server-side MOVE.
func (e *Engine) moveToRole(acct config.Account, folderID int64, uid uint32, role mail.FolderRole) error {
	e.mu.Lock()
	h, ok := e.watchers[acct.Name]
	e.mu.Unlock()
	if !ok {
		return ErrNotWatching
	}
	dest := folders.Resolve(acct, e.getSpecialUse(acct.Name), role)
	op := moveOp{folderID: folderID, uid: uid, dest: dest}
	select {
	case h.moveOps <- op:
		return nil
	default:
		e.logger.Warn("move queue full, dropping", "account", acct.Name, "uid", uid, "role", role)
		return nil
	}
}

// SwitchFolder requests the active watcher for accountName to re-SELECT
// folderName and perform a catch-up sync on it. Returns ErrNotWatching if no
// watcher is running. The request is dropped silently if a switch is already
// pending — the caller may retry.
func (e *Engine) SwitchFolder(accountName, folderName string) error {
	e.mu.Lock()
	h, ok := e.watchers[accountName]
	e.mu.Unlock()
	if !ok {
		return ErrNotWatching
	}
	select {
	case h.switchFolder <- folderName:
	default: // already pending; drop
	}
	return nil
}

// Watch monitors acct for new mail, emitting events onto the events channel.
// It performs an initial catch-up sync, then either uses IMAP IDLE (if the
// server advertises it and acct.IDLEEnabled is true) or falls back to polling
// every pollInterval. The events channel is closed when Watch returns.
//
// Only one Watch per account name per Engine is permitted; a second call
// returns an error immediately.
func (e *Engine) Watch(ctx context.Context, acct config.Account, pollInterval, sweepInterval time.Duration, events chan<- Event) error {
	h := &watcherHandle{
		poke:         make(chan struct{}, 1),
		flagOps:      make(chan flagOp, 16),
		moveOps:      make(chan moveOp, 16),
		switchFolder: make(chan string, 1),
		sweep:        make(chan struct{}, 1),
	}
	e.mu.Lock()
	if _, exists := e.watchers[acct.Name]; exists {
		e.mu.Unlock()
		if events != nil {
			close(events)
		}
		return errors.New("already watching account " + acct.Name)
	}
	e.watchers[acct.Name] = h
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.watchers, acct.Name)
		e.mu.Unlock()
		if events != nil {
			close(events)
		}
	}()

	emit := func(ev Event) {
		h.observe(ev)
		if events != nil {
			events <- ev
		}
	}

	emit(WatchStarted{Account: acct.Name})

	// authFn fetches a fresh credential on every call so that OAuth2 access
	// tokens (which expire in ~1 hour) are not reused across reconnects.
	store := e.secrets
	authFn := func() (string, error) {
		return store.Get(acct.Name)
	}

	err := workerloop.Run(ctx, e.logger, "Watch "+acct.Name,
		func(ctx context.Context) error {
			e.logger.Debug("Watch: starting watchOnce", "account", acct.Name)
			return e.watchOnce(ctx, acct, authFn, h, pollInterval, sweepInterval, emit)
		},
		func(err error) { emit(WatchError{Account: acct.Name, Err: err}) },
	)
	emit(WatchStopped{Account: acct.Name, Err: err})
	return err
}

// watchOnce opens a single IMAP connection and runs either an IDLE loop or a
// poll loop until the connection fails or ctx is cancelled.
// authFn is called once per invocation to obtain a fresh credential, ensuring
// OAuth2 access tokens are never reused across reconnects.
func (e *Engine) watchOnce(ctx context.Context, acct config.Account, authFn func() (string, error), h *watcherHandle, pollInterval, sweepInterval time.Duration, emit func(Event)) error {
	e.logger.Debug("watchOnce: resolving credential", "account", acct.Name)
	token, err := authFn()
	if err != nil {
		e.logger.Error("watchOnce: credential resolution failed", "account", acct.Name, "err", err)
		return fmt.Errorf("resolve credential for %s: %w", acct.Name, err)
	}

	poke := h.poke
	flagOps := h.flagOps
	moveOps := h.moveOps
	// pokeSend receives pokes from the server's unilateral data (EXISTS/EXPUNGE).
	pokeSend := make(chan struct{}, 1)
	handler := &imapclient.UnilateralDataHandler{
		Mailbox: func(d *imapclient.UnilateralDataMailbox) {
			if d.NumMessages != nil {
				select {
				case pokeSend <- struct{}{}:
				default:
				}
			}
		},
		Expunge: func(uint32) {
			select {
			case pokeSend <- struct{}{}:
			default:
			}
		},
	}

	e.logger.Debug("watchOnce: connecting", "host", acct.IMAPHost, "port", acct.IMAPPort, "tls", acct.IMAPTLS)
	c, err := imapauth.ConnectAndLogin(acct, token, handler)
	if err != nil {
		e.logger.Error("watchOnce: connect failed", "account", acct.Name, "err", err)
		return err
	}
	e.logger.Debug("watchOnce: connected", "account", acct.Name)
	defer func() { _ = c.Close() }()

	// Discover all mailboxes and build the specialUse map from SPECIAL-USE attrs.
	mailboxList, err := folders.List(c)
	if err != nil {
		e.logger.Warn("watchOnce: mailbox list failed; aborting iteration", "account", acct.Name, "err", err)
		return fmt.Errorf("list mailboxes: %w", err)
	}

	// Build specialUse map and persist all mailbox rows.
	specialUseMap := map[mail.FolderRole]string{}
	for _, mb := range mailboxList {
		if mb.Role != "" && mb.Role != mail.RoleInbox {
			specialUseMap[mb.Role] = mb.Name
		}
	}
	if len(specialUseMap) == 0 {
		e.setSpecialUse(acct.Name, nil)
	} else {
		e.setSpecialUse(acct.Name, specialUseMap)
	}

	// Watchdog: close the connection if the context is cancelled.
	go func() {
		<-ctx.Done()
		_ = c.Close()
	}()

	selectData, err := c.Select("INBOX", nil).Wait()
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	serverUIDValidity := selectData.UIDValidity

	accountID, err := e.store.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		return err
	}

	// Reconcile local folder list against server: detects renames, new folders,
	// and disappeared folders. Also persists role for all known mailboxes.
	if err := e.reconcileFolderList(ctx, c, acct, accountID, mailboxList, sweepInterval, emit); err != nil {
		e.logger.Warn("watchOnce: folder reconcile failed", "account", acct.Name, "err", err)
	}

	folderID, err := e.store.EnsureFolder(accountID, "INBOX")
	if err != nil {
		return err
	}

	// Initial catch-up sync.
	if err := e.fetchFolder(ctx, c, acct, folderID, accountID, serverUIDValidity, "INBOX", emit); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	// Launch the sweep timer goroutine. It pokes h.sweep every sweepInterval;
	// the IDLE/poll loop drains it between operations.
	if sweepInterval > 0 {
		go sweepLoop(ctx, sweepInterval, h.sweep)
	}

	// Merge external pokes (Refresh) and server-push pokes into one channel.
	combinedPoke := mergedPoke(ctx, poke, pokeSend)

	caps := c.Caps()
	if caps.Has(imap.CapIdle) && (acct.IDLEEnabled == nil || *acct.IDLEEnabled) {
		e.logger.Debug("watchOnce: entering IDLE loop", "account", acct.Name)
		h.setIdle(true)
		return e.idleLoop(ctx, c, acct, folderID, accountID, serverUIDValidity, combinedPoke, flagOps, moveOps, h.switchFolder, h.sweep, sweepInterval, emit)
	}
	idleEnabled := acct.IDLEEnabled == nil || *acct.IDLEEnabled
	e.logger.Debug("watchOnce: entering poll loop", "account", acct.Name, "idle_cap", caps.Has(imap.CapIdle), "idle_enabled", idleEnabled, "interval", pollInterval)
	h.setIdle(false)
	return e.pollLoop(ctx, c, acct, folderID, accountID, serverUIDValidity, combinedPoke, flagOps, moveOps, h.switchFolder, h.sweep, sweepInterval, pollInterval, emit)
}

// mergedPoke fans in two poke channels into one, stopping when ctx is done.
func mergedPoke(ctx context.Context, a, b <-chan struct{}) <-chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-a:
				select {
				case ch <- struct{}{}:
				default:
				}
			case <-b:
				select {
				case ch <- struct{}{}:
				default:
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// stopIdle closes the IDLE command and waits for the server reply, mapping
// the three outcomes into a single (done, err) pair:
//
//   - (true, nil):  ctx cancelled during wait — caller should return nil.
//   - (false, err): wait returned a real error — caller should propagate.
//   - (false, nil): clean stop — caller should proceed.
func stopIdle(ctx context.Context, idle *imapclient.IdleCommand) (done bool, err error) {
	_ = idle.Close()
	if werr := idle.Wait(); werr != nil {
		if ctx.Err() != nil {
			return true, nil
		}
		return false, werr
	}
	return false, nil
}

// idleLoop enters IMAP IDLE and waits for a poke (from the server or a
// Refresh call) before doing a catch-up fetchFolder. It loops until ctx is
// cancelled or a fatal connection error occurs.
func (e *Engine) idleLoop(ctx context.Context, c *imapclient.Client, acct config.Account, initialFolderID, accountID int64, initialUIDValidity uint32, poke <-chan struct{}, flagOps <-chan flagOp, moveOps <-chan moveOp, switchFolder <-chan string, sweepCh <-chan struct{}, sweepInterval time.Duration, emit func(Event)) error {
	folderID := initialFolderID
	uidValidity := initialUIDValidity
	currentFolder := "INBOX"

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		idle, err := c.Idle()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("idle: %w", err)
		}

		select {
		case <-ctx.Done():
			_ = idle.Close()
			_ = idle.Wait()
			return nil
		case <-poke:
			if done, err := stopIdle(ctx, idle); done || err != nil {
				return err
			}
			if err := e.fetchFolder(ctx, c, acct, folderID, accountID, uidValidity, currentFolder, emit); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		case name := <-switchFolder:
			if done, err := stopIdle(ctx, idle); done || err != nil {
				return err
			}
			selectData, err := c.Select(name, nil).Wait()
			if err != nil {
				e.logger.Warn("SwitchFolder select failed", "folder", name, "err", err)
				continue
			}
			fid, err := e.store.EnsureFolder(accountID, name)
			if err != nil {
				e.logger.Warn("SwitchFolder EnsureFolder failed", "folder", name, "err", err)
				continue
			}
			folderID = fid
			uidValidity = selectData.UIDValidity
			currentFolder = name
			if err := e.fetchFolder(ctx, c, acct, folderID, accountID, uidValidity, currentFolder, emit); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		case op := <-flagOps:
			if done, err := stopIdle(ctx, idle); done || err != nil {
				return err
			}
			if err := runStore(c, op); err != nil {
				e.logger.Warn("IMAP STORE failed", "uid", op.uid, "err", err)
			}
			drainFlagOps(c, flagOps, e.logger)
			// Do not run fetchFolder — local DB already updated optimistically.
		case op := <-moveOps:
			if done, err := stopIdle(ctx, idle); done || err != nil {
				return err
			}
			if err := runMove(c, op); err != nil {
				e.logger.Warn("IMAP MOVE failed", "uid", op.uid, "dest", op.dest, "err", err)
			}
			drainMoveOps(c, moveOps, e.logger)
			// Do not run fetchFolder — local DB already updated optimistically.
		case <-sweepCh:
			if done, err := stopIdle(ctx, idle); done || err != nil {
				return err
			}
			newName, newUID, err := e.runSweep(ctx, c, acct, accountID, currentFolder, sweepInterval, emit)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				e.logger.Warn("sweep failed", "account", acct.Name, "err", err)
				// Reconnect by returning; the Watch retry loop will reconnect.
				return err
			}
			if newName != "" {
				currentFolder = newName
			}
			if newUID != 0 {
				uidValidity = newUID
			}
		}
	}
}

// pollLoop fetches on every tick of pollInterval or when a poke arrives.
func (e *Engine) pollLoop(ctx context.Context, c *imapclient.Client, acct config.Account, initialFolderID, accountID int64, initialUIDValidity uint32, poke <-chan struct{}, flagOps <-chan flagOp, moveOps <-chan moveOp, switchFolder <-chan string, sweepCh <-chan struct{}, sweepInterval, pollInterval time.Duration, emit func(Event)) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	folderID := initialFolderID
	uidValidity := initialUIDValidity
	currentFolder := "INBOX"

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-poke:
		case name := <-switchFolder:
			selectData, err := c.Select(name, nil).Wait()
			if err != nil {
				e.logger.Warn("SwitchFolder select failed", "folder", name, "err", err)
				continue
			}
			fid, err := e.store.EnsureFolder(accountID, name)
			if err != nil {
				e.logger.Warn("SwitchFolder EnsureFolder failed", "folder", name, "err", err)
				continue
			}
			folderID = fid
			uidValidity = selectData.UIDValidity
			currentFolder = name
			if err := e.fetchFolder(ctx, c, acct, folderID, accountID, uidValidity, currentFolder, emit); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			continue
		case op := <-flagOps:
			if err := runStore(c, op); err != nil {
				e.logger.Warn("IMAP STORE failed", "uid", op.uid, "err", err)
			}
			drainFlagOps(c, flagOps, e.logger)
			continue
		case op := <-moveOps:
			if err := runMove(c, op); err != nil {
				e.logger.Warn("IMAP MOVE failed", "uid", op.uid, "dest", op.dest, "err", err)
			}
			drainMoveOps(c, moveOps, e.logger)
			continue
		case <-sweepCh:
			newName, newUID, err := e.runSweep(ctx, c, acct, accountID, currentFolder, sweepInterval, emit)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				e.logger.Warn("sweep failed", "account", acct.Name, "err", err)
				return err
			}
			if newName != "" {
				currentFolder = newName
			}
			if newUID != 0 {
				uidValidity = newUID
			}
			continue
		}
		if err := e.fetchFolder(ctx, c, acct, folderID, accountID, uidValidity, currentFolder, emit); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}
