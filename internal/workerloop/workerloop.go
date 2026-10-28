// Package workerloop provides the exponential-backoff retry loop shared by
// the mailsync and mailsend worker engines. Both open a long-lived connection
// per account and must reconnect on transient failures with identical
// semantics; keeping the retry policy in one place prevents drift.
package workerloop

import (
	"context"
	"log/slog"
	"time"
)

const (
	// InitialBackoff is the sleep after the first failure.
	InitialBackoff = 1 * time.Second
	// MaxBackoff caps the exponential growth so a long outage still retries
	// at least every 30s.
	MaxBackoff = 30 * time.Second
)

// Run executes attempt in an exponential-backoff retry loop until ctx is
// cancelled or attempt returns nil. On each failure onError is invoked
// (useful for emitting a *Error event) before sleeping; the backoff doubles
// (capped at MaxBackoff) between retries.
//
// Returns ctx.Err() (nil on clean cancellation, context.Canceled when ctx
// was cancelled mid-attempt).
func Run(
	ctx context.Context,
	logger *slog.Logger,
	label string,
	attempt func(context.Context) error,
	onError func(error),
) error {
	backoff := InitialBackoff
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		err := attempt(ctx)
		if err == nil || ctx.Err() != nil {
			return ctx.Err()
		}
		if logger != nil {
			logger.Warn(label+": error, will retry", "err", err, "backoff", backoff)
		}
		if onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > MaxBackoff {
			backoff = MaxBackoff
		}
	}
}
