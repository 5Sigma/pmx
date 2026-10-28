package pmxd

import (
	"log/slog"

	"github.com/gen2brain/beeep"
)

// Notifier sends cross-platform desktop notifications via beeep.
// On Linux it uses org.freedesktop.Notifications over D-Bus.
// On macOS it uses osascript (always available, no extra dependencies).
type Notifier struct {
	logger *slog.Logger
}

func NewNotifier(logger *slog.Logger) *Notifier {
	return &Notifier{logger: logger}
}

func (n *Notifier) Connect() error { return nil }

func (n *Notifier) Notify(from, subject string) {
	if err := beeep.Notify(from, subject, ""); err != nil {
		n.logger.Debug("notification failed", "err", err)
	}
}

func (n *Notifier) Close() {}
