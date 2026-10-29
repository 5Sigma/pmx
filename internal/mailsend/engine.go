package mailsend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/secrets"
	"github.com/5Sigma/pmx/internal/workerloop"
)

// ErrNotRunning is returned by Trigger when no active send worker exists for
// the named account.
var ErrNotRunning = errors.New("no active send worker for this account")

// Engine manages send worker goroutines and drains the outbox on behalf of
// each account. Mirror of mailsync.Engine.
type Engine struct {
	store       mail.MessageStore
	secrets     *secrets.Store
	logger      *slog.Logger
	maxAttempts int
	claimant    string
	mu          sync.Mutex
	workers     map[string]chan struct{} // accountName → poke channel
}

// New creates an Engine. If logger is nil, slog.Default() is used.
func New(store mail.MessageStore, sec *secrets.Store, maxAttempts int, claimant string, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		store:       store,
		secrets:     sec,
		logger:      logger,
		maxAttempts: maxAttempts,
		claimant:    claimant,
		workers:     make(map[string]chan struct{}),
	}
}

// Logger returns the engine's logger.
func (e *Engine) Logger() *slog.Logger { return e.logger }

// Trigger pokes the active worker for accountName to drain immediately.
// Returns ErrNotRunning if no worker is active for this account.
func (e *Engine) Trigger(accountName string) error {
	e.mu.Lock()
	poke, ok := e.workers[accountName]
	e.mu.Unlock()
	if !ok {
		return ErrNotRunning
	}
	select {
	case poke <- struct{}{}:
	default: // already pending
	}
	return nil
}

// Run starts a send worker for acct. Only one Run per account name per Engine
// is permitted; a second call returns an error immediately. The events channel
// is closed when Run returns.
func (e *Engine) Run(ctx context.Context, acct config.Account, pollInterval time.Duration, events chan<- Event) error {
	poke := make(chan struct{}, 1)
	e.mu.Lock()
	if _, exists := e.workers[acct.Name]; exists {
		e.mu.Unlock()
		if events != nil {
			close(events)
		}
		return errors.New("send worker already running for account " + acct.Name)
	}
	e.workers[acct.Name] = poke
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.workers, acct.Name)
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

	emit(WorkerStarted{Account: acct.Name})

	runErr := workerloop.Run(ctx, e.logger, "send "+acct.Name,
		func(ctx context.Context) error {
			return e.drainLoop(ctx, acct, poke, pollInterval, emit)
		},
		func(err error) { emit(WorkerError{Account: acct.Name, Err: err}) },
	)
	emit(WorkerStopped{Account: acct.Name, Err: runErr})
	return runErr
}

// drainLoop polls and drains the outbox on every tick or poke until ctx is
// cancelled or a worker-level error (e.g. DB failure) occurs.
func (e *Engine) drainLoop(ctx context.Context, acct config.Account, poke <-chan struct{}, pollInterval time.Duration, emit func(Event)) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Drain immediately on first entry.
	if err := e.drainQueue(ctx, acct, emit); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-poke:
		}
		if err := e.drainQueue(ctx, acct, emit); err != nil {
			return err
		}
	}
}

// drainQueue fetches all pending outbox rows for acct and calls sendOneWithPass
// for each. Per-item SMTP errors are recorded on the row and do not propagate;
// only store-level errors are returned.
func (e *Engine) drainQueue(ctx context.Context, acct config.Account, emit func(Event)) error {
	// Always force-refresh OAuth2 tokens so a server-side-revoked token is never
	// reused across drain cycles (ReuseTokenSource would otherwise serve a cached
	// access token until its local Expiry passes).
	pass, err := e.secrets.GetFresh(acct.Name)
	if err != nil {
		return fmt.Errorf("resolve secret: %w", err)
	}

	accountID, err := e.store.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		return fmt.Errorf("ensure account: %w", err)
	}

	items, err := e.store.ClaimPendingOutbox(accountID, e.claimant, 100, time.Now(), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("claim pending: %w", err)
	}

	var sent, failed int
	for _, item := range items {
		if ctx.Err() != nil {
			return nil
		}
		if item.Attempts >= e.maxAttempts {
			continue
		}

		emit(SendStarted{Account: acct.Name, ItemID: item.ID})

		res, err := sendOneWithPass(ctx, e.store, pass, e.logger, acct, item, e.maxAttempts)
		if err != nil {
			// Worker-level error — propagate to trigger backoff.
			return fmt.Errorf("send item %d: %w", item.ID, err)
		}
		switch {
		case res.sent:
			sent++
			emit(SendSucceeded{Account: acct.Name, ItemID: item.ID})
		case res.failed:
			failed++
			emit(SendFailed{Account: acct.Name, ItemID: item.ID, Attempts: item.Attempts + 1, Err: nil})
		}
	}

	if sent > 0 || failed > 0 {
		emit(QueueDrained{Account: acct.Name, At: time.Now(), Sent: sent, Failed: failed})
	}
	return nil
}

// DrainOnce performs a single outbox drain for acct. Used by pmxd.
func (e *Engine) DrainOnce(ctx context.Context, acct config.Account) error {
	return e.drainQueue(ctx, acct, func(Event) {})
}
