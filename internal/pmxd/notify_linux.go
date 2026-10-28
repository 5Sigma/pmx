//go:build linux

package pmxd

import (
	"log/slog"

	"github.com/esiqveland/notify"
	"github.com/godbus/dbus/v5"
)

type Notifier struct {
	logger *slog.Logger
	conn   *dbus.Conn
}

func NewNotifier(logger *slog.Logger) *Notifier {
	return &Notifier{logger: logger}
}

func (n *Notifier) Connect() error {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return err
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return err
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return err
	}
	n.conn = conn
	return nil
}

func (n *Notifier) Notify(from, subject, account string) {
	if n.conn == nil {
		return
	}
	body := subject
	if account != "" {
		body = subject + "\n" + account
	}
	note := notify.Notification{
		AppName:       "pmx",
		AppIcon:       "mail-message-new",
		Summary:       from,
		Body:          body,
		ExpireTimeout: notify.ExpireTimeoutSetByNotificationServer,
		Hints: map[string]dbus.Variant{
			"category": dbus.MakeVariant("email.arrived"),
			"urgency":  dbus.MakeVariant(byte(notify.UrgencyNormal)),
		},
	}
	if _, err := notify.SendNotification(n.conn, note); err != nil {
		n.logger.Debug("notification failed", "err", err)
	}
}

func (n *Notifier) Close() {
	if n.conn != nil {
		_ = n.conn.Close()
		n.conn = nil
	}
}
