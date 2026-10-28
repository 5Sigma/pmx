//go:build !linux

package pmxd

import (
	"log/slog"

	"github.com/gen2brain/beeep"
)

type Notifier struct {
	logger *slog.Logger
}

func NewNotifier(logger *slog.Logger) *Notifier {
	beeep.AppName = "pmx"
	return &Notifier{logger: logger}
}

func (n *Notifier) Connect() error { return nil }

func (n *Notifier) Notify(from, subject, account string) {
	if err := beeep.Notify(from, subject, ""); err != nil {
		n.logger.Debug("notification failed", "err", err)
	}
}

func (n *Notifier) Close() {}
