package accountrunner

import (
	"context"
	"log/slog"
	"sync"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
)

// RunEngines starts engine.Watch and sender.Run for acct and writes their
// events to syncOut and sendOut respectively. Blocks until both engine
// goroutines return (ctx cancellation or fatal error).
//
// Watch and Run each close their events channel on return, so callers do
// NOT need to close syncOut / sendOut themselves. A nil engine or sender
// skips the corresponding goroutine; the matching output channel is closed
// immediately so forwarders can detect "no engine configured" and exit.
func RunEngines(
	ctx context.Context,
	acct config.Account,
	engine *mailsync.Engine,
	sender *mailsend.Engine,
	cfg *config.Config,
	logger *slog.Logger,
	syncOut chan<- mailsync.Event,
	sendOut chan<- mailsend.Event,
) {
	var wg sync.WaitGroup
	if engine != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := engine.Watch(ctx, acct,
				cfg.General.PollInterval,
				cfg.General.SweepInterval,
				syncOut); err != nil {
				logger.Warn("Watch exited", "account", acct.Name, "err", err)
			}
		}()
	} else {
		close(syncOut)
	}
	if sender != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sender.Run(ctx, acct,
				cfg.General.SendInterval,
				sendOut); err != nil {
				logger.Warn("sender exited", "account", acct.Name, "err", err)
			}
		}()
	} else {
		close(sendOut)
	}
	wg.Wait()
}
