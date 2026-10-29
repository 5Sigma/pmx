package pmxd

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/5Sigma/pmx/internal/accountrunner"
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
)

// Broadcaster receives every event the daemon emits and fans it out to
// connected clients. Implemented by pmxsock.Server. Kept as an interface here
// so pmxd does not import pmxsock (avoids a cycle).
type Broadcaster interface {
	Broadcast(ev any)
}

// Daemon orchestrates periodic mail sync, outbox drain, and desktop notifications.
type Daemon struct {
	cfg         *config.Config
	store       mail.MessageStore
	syncEng     *mailsync.Engine
	sendEng     *mailsend.Engine
	notifier    *Notifier
	state       *State
	logger      *slog.Logger
	broadcaster Broadcaster // may be nil; set via SetBroadcaster
}

// New creates a Daemon. Call Run to start the cycle.
func New(cfg *config.Config, store mail.MessageStore, syncEng *mailsync.Engine, sendEng *mailsend.Engine, notifier *Notifier, state *State, logger *slog.Logger) *Daemon {
	return &Daemon{
		cfg:      cfg,
		store:    store,
		syncEng:  syncEng,
		sendEng:  sendEng,
		notifier: notifier,
		state:    state,
		logger:   logger,
	}
}

// SetBroadcaster installs a Broadcaster that receives every event the daemon
// handles. Call before Run. Safe to call with nil (disables broadcasting).
func (d *Daemon) SetBroadcaster(b Broadcaster) { d.broadcaster = b }

// Run starts the daemon loop. Blocks until ctx is cancelled.
// When idle_enabled is true (the default), it uses IMAP IDLE via Watch().
// When false, it falls back to the ticker-based polling loop.
func (d *Daemon) Run(ctx context.Context) error {
	if d.cfg.Daemon.IDLEEnabled != nil && !*d.cfg.Daemon.IDLEEnabled {
		return d.runPoll(ctx)
	}
	return d.runIdle(ctx)
}

// RunOnce performs a single cycle (for --once flag / debugging).
func (d *Daemon) RunOnce(ctx context.Context) {
	d.cycle(ctx)
}

// runPoll is the original ticker-based loop. Used when idle_enabled = false.
func (d *Daemon) runPoll(ctx context.Context) error {
	t := time.NewTicker(d.cfg.Daemon.CheckInterval)
	defer t.Stop()

	d.cycle(ctx) // immediate first pass
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			d.cycle(ctx)
		}
	}
}

// runIdle uses IMAP IDLE (via Watch) for push-based sync. For each account,
// accountrunner.RunEngines launches a Watch + sender.Run pair; two forwarders
// fan the typed event channels into a shared chan any so broadcast and
// handleWatchEvent see every event in one place.
func (d *Daemon) runIdle(ctx context.Context) error {
	// Seed the DB and fire initial notifications before Watch takes over.
	d.cycle(ctx)
	if ctx.Err() != nil {
		return nil
	}

	events := make(chan any, 64)
	var wg sync.WaitGroup

	for _, acct := range d.cfg.Accounts {
		acct := acct
		syncCh := make(chan mailsync.Event, 32)
		sendCh := make(chan mailsend.Event, 32)

		wg.Add(3)
		go func() {
			defer wg.Done()
			accountrunner.RunEngines(ctx, acct, d.syncEng, d.sendEng, d.cfg, d.logger, syncCh, sendCh)
		}()
		go func() {
			defer wg.Done()
			for ev := range syncCh {
				select {
				case events <- ev:
				case <-ctx.Done():
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for ev := range sendCh {
				select {
				case events <- ev:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(events)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if d.broadcaster != nil {
				d.broadcaster.Broadcast(ev)
			}
			if syncEv, ok := ev.(mailsync.Event); ok {
				d.handleWatchEvent(ctx, syncEv)
			}
		}
	}
}

// handleWatchEvent processes a single event emitted by Watch. On any event
// that indicates new messages (FolderDone with Added > 0, or SyncCompleted),
// it calls notifyNewMail for the relevant account and persists state.
func (d *Daemon) handleWatchEvent(ctx context.Context, ev mailsync.Event) {
	var acctName string
	var notify bool

	switch e := ev.(type) {
	case mailsync.FolderDone:
		acctName = e.Account
		notify = e.Added > 0
	case mailsync.SyncCompleted:
		acctName = e.Account
		notify = true
	default:
		return
	}

	if !notify {
		return
	}

	acct := d.cfg.AccountByName(acctName)
	if acct == nil {
		return
	}

	notifyNewMail(ctx, d.store, *acct, d.cfg, d.state, d.notifier, d.logger)
	if err := d.state.Save(); err != nil {
		d.logger.Warn("state save failed", "err", err)
	}
}

func (d *Daemon) cycle(ctx context.Context) {
	d.logger.Info("starting cycle", "accounts", len(d.cfg.Accounts))
	for _, acct := range d.cfg.Accounts {
		if ctx.Err() != nil {
			return
		}
		d.syncOne(ctx, acct)
		d.sendOne(ctx, acct)
		notifyNewMail(ctx, d.store, acct, d.cfg, d.state, d.notifier, d.logger)
	}
	if err := d.state.Save(); err != nil {
		d.logger.Warn("state save failed", "err", err)
	}
	d.logger.Info("cycle complete")
}

func (d *Daemon) syncOne(ctx context.Context, acct config.Account) {
	d.logger.Debug("syncing", "acct", acct.Name)
	events := make(chan mailsync.Event, 32)
	go func() {
		for range events {
		}
	}()
	if err := d.syncEng.SyncAccount(ctx, acct, events); err != nil {
		d.logger.Warn("sync failed", "acct", acct.Name, "err", err)
	}
}

func (d *Daemon) sendOne(ctx context.Context, acct config.Account) {
	d.logger.Debug("draining outbox", "acct", acct.Name)
	if err := d.sendEng.DrainOnce(ctx, acct); err != nil {
		d.logger.Warn("drain failed", "acct", acct.Name, "err", fmt.Sprintf("%v", err))
	}
}
