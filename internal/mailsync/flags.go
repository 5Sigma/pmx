package mailsync

import (
	"log/slog"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// runStore executes an IMAP UID STORE command for a single flagOp.
func runStore(c *imapclient.Client, op flagOp) error {
	var set imap.UIDSet
	set.AddNum(imap.UID(op.uid))
	if len(op.add) > 0 {
		if err := c.Store(set, &imap.StoreFlags{
			Op:     imap.StoreFlagsAdd,
			Flags:  op.add,
			Silent: true,
		}, nil).Close(); err != nil {
			return err
		}
	}
	if len(op.remove) > 0 {
		if err := c.Store(set, &imap.StoreFlags{
			Op:     imap.StoreFlagsDel,
			Flags:  op.remove,
			Silent: true,
		}, nil).Close(); err != nil {
			return err
		}
	}
	return nil
}

// drainFlagOps non-blockingly drains and executes any pending flagOps from ch.
func drainFlagOps(c *imapclient.Client, ch <-chan flagOp, log *slog.Logger) {
	for {
		select {
		case op := <-ch:
			if err := runStore(c, op); err != nil {
				log.Warn("IMAP STORE failed", "uid", op.uid, "err", err)
			}
		default:
			return
		}
	}
}
