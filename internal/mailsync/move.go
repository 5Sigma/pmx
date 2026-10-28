package mailsync

import (
	"log/slog"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// runMove performs a single IMAP MOVE for op.uid out of the currently-selected
// mailbox into op.dest. The go-imap v2 client falls back to COPY + STORE
// +\Deleted + UIDEXPUNGE on servers that don't advertise CapMove.
func runMove(c *imapclient.Client, op moveOp) error {
	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(op.uid))
	cmd := c.Move(uidSet, op.dest)
	_, err := cmd.Wait()
	return err
}

// drainMoveOps non-blockingly consumes any pending moveOps on the same wake tick.
func drainMoveOps(c *imapclient.Client, ch <-chan moveOp, log *slog.Logger) {
	for {
		select {
		case op := <-ch:
			if err := runMove(c, op); err != nil {
				log.Warn("IMAP MOVE failed", "uid", op.uid, "dest", op.dest, "err", err)
			}
		default:
			return
		}
	}
}
