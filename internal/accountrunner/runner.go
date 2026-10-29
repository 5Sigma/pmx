package accountrunner

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
)

// ProgramSender is the minimal bubbletea interface Runner needs.
type ProgramSender interface {
	Send(msg tea.Msg)
}

// Runner owns the per-account goroutine lifecycle for the active account.
// It ensures that Watch and Run goroutines are cancelled and drained before
// new ones are started when the user switches accounts.
type Runner struct {
	engine *mailsync.Engine
	sender *mailsend.Engine
	cfg    *config.Config
	logger *slog.Logger

	mu     sync.Mutex
	prog   ProgramSender
	active string
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a Runner. prog may be nil at construction and set later via
// SetProgram.
func New(engine *mailsync.Engine, sender *mailsend.Engine, cfg *config.Config, logger *slog.Logger, prog ProgramSender) *Runner {
	return &Runner{
		engine: engine,
		sender: sender,
		cfg:    cfg,
		logger: logger,
		prog:   prog,
	}
}

// SetProgram sets the program sender after construction. This breaks the
// initialization cycle between tea.NewProgram and ui.New.
func (r *Runner) SetProgram(prog ProgramSender) {
	r.mu.Lock()
	r.prog = prog
	r.mu.Unlock()
}

// Active returns the name of the currently active account, or "" if none.
func (r *Runner) Active() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// Start begins Watch + Run goroutines for acct. Idempotent if acct.Name
// equals the currently active account.
func (r *Runner) Start(acct config.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.start(acct)
}

// stop signals active goroutines to stop by cancelling the context and clears
// the active account name. It does NOT wait for goroutines to finish — callers
// must release r.mu and then call r.wg.Wait() themselves so that Stop and
// Switch never hold r.mu while blocking on I/O teardown.
// Caller must hold r.mu.
func (r *Runner) stop() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.active = ""
}

// start launches goroutines for acct without acquiring r.mu.
// Caller must hold r.mu.
func (r *Runner) start(acct config.Account) error {
	if r.active == acct.Name {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	syncEvents := make(chan mailsync.Event, 32)
	sendEvents := make(chan mailsend.Event, 32)

	// Three goroutines: engine lifecycle + two forwarders. Watch and Run close
	// their events channel on return, so the forwarders unblock automatically.
	r.wg.Add(3)
	go func() {
		defer r.wg.Done()
		RunEngines(ctx, acct, r.engine, r.sender, r.cfg, r.logger, syncEvents, sendEvents)
	}()
	go r.forwardSyncEvents(syncEvents)
	go r.forwardSendEvents(sendEvents)

	r.cancel = cancel
	r.active = acct.Name
	return nil
}

// Stop cancels the active goroutines and waits up to 5 seconds for them to
// finish. The lock is released before waiting so that a concurrent Switch
// cannot deadlock with this call. If goroutines are still mid-sync after the
// timeout (e.g. fetching a large folder), Stop returns anyway — the process
// is exiting and the OS will clean up.
func (r *Runner) Stop() error {
	r.mu.Lock()
	r.stop()
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		r.logger.Warn("Runner.Stop: goroutines did not finish within 5s, forcing exit")
	}
	return nil
}

// forwardSyncEvents drains syncEvents and forwards each to the program sender.
// Returns when the channel is closed.
func (r *Runner) forwardSyncEvents(ch <-chan mailsync.Event) {
	defer r.wg.Done()
	for ev := range ch {
		r.sendToProgram(ev)
	}
}

// forwardSendEvents drains sendEvents and forwards each to the program sender.
// Returns when the channel is closed.
func (r *Runner) forwardSendEvents(ch <-chan mailsend.Event) {
	defer r.wg.Done()
	for ev := range ch {
		r.sendToProgram(ev)
	}
}

// sendToProgram forwards one event to the current program sender under the
// runner's lock, so a concurrent SetProgram is safe.
func (r *Runner) sendToProgram(ev tea.Msg) {
	r.mu.Lock()
	prog := r.prog
	r.mu.Unlock()
	if prog != nil {
		prog.Send(ev)
	}
}

// Switch stops the current account and starts the new one. The lock is
// released before wg.Wait() so that a concurrent Stop() call (e.g. from
// cmd/root.go defer after tea.Quit) can proceed without deadlocking.
// If Start(newAcct) fails and a previous account was running, it attempts
// to restart the previous account as best-effort recovery. The start error
// is returned even when recovery succeeds.
func (r *Runner) Switch(acct config.Account) error {
	r.mu.Lock()
	oldName := r.active
	r.stop()      // cancel + clear active; does NOT wait
	r.mu.Unlock() // release before blocking I/O teardown

	r.wg.Wait() // wait for old goroutines without holding the lock

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.start(acct); err != nil {
		if oldName != "" {
			old := r.cfg.AccountByName(oldName)
			if old != nil {
				if recoverErr := r.start(*old); recoverErr != nil {
					r.logger.Error("Switch: recovery start failed",
						"account", oldName, "err", recoverErr)
				}
			}
		}
		return fmt.Errorf("switch to %s: %w", acct.Name, err)
	}
	return nil
}
