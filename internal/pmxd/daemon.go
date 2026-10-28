package pmxd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
)

// Daemon orchestrates periodic mail sync, outbox drain, and desktop notifications.
type Daemon struct {
	cfg      *config.Config
	store    mail.MessageStore
	syncEng  *mailsync.Engine
	sendEng  *mailsend.Engine
	notifier *Notifier
	state    *State
	logger   *slog.Logger
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

// Run starts the daemon loop. Blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
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

// RunOnce performs a single cycle (for --once flag / debugging).
func (d *Daemon) RunOnce(ctx context.Context) {
	d.cycle(ctx)
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
