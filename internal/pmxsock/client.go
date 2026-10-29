package pmxsock

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/pmxproto"
)

// ProgramSender is the bubbletea interface Client needs to forward events.
type ProgramSender interface {
	Send(msg tea.Msg)
}

// Client is a TUI-side connection to the pmx daemon over a Unix socket.
// It satisfies both mailops.MailOps and mailops.SendOps.
type Client struct {
	conn    net.Conn
	w       *bufio.Writer
	writeMu sync.Mutex
	logger  *slog.Logger

	closeMu sync.Mutex
	closed  bool
}

// Dial connects to the Unix socket at socketPath.
// Returns an error if the daemon is not reachable.
func Dial(socketPath string, logger *slog.Logger) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:   conn,
		w:      bufio.NewWriter(conn),
		logger: logger,
	}, nil
}

// Events reads events from the daemon and forwards them to prog.Send.
// Blocks until the connection is closed; sends DaemonDisconnectedMsg on closure.
func (c *Client) Events(ctx context.Context, prog ProgramSender) {
	r := bufio.NewReader(c.conn)
	for {
		msg, err := pmxproto.DecodeEvent(r)
		if err != nil {
			prog.Send(pmxproto.DaemonDisconnectedMsg{})
			return
		}
		if msg == nil {
			continue // unknown or skipped event type
		}
		prog.Send(msg)
	}
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	c.closed = true
	return c.conn.Close()
}

// write encodes cmd as a JSON line and flushes. Fire-and-forget: all
// failures are logged and nil is returned, matching the MailOps contract.
func (c *Client) write(cmd any) error {
	c.closeMu.Lock()
	if c.closed {
		c.closeMu.Unlock()
		return nil
	}
	c.closeMu.Unlock()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := pmxproto.EncodeCommand(c.w, cmd); err != nil {
		c.logger.Warn("pmxsock: encode command failed", "err", err)
		return nil
	}
	if err := c.w.Flush(); err != nil {
		c.logger.Warn("pmxsock: flush command failed", "err", err)
	}
	return nil
}

// MailOps implementation — satisfies internal/mailops.MailOps.

func (c *Client) Refresh(acct string) error {
	return c.write(pmxproto.CmdRefresh{Account: acct})
}

func (c *Client) SwitchFolder(acct, folder string) error {
	return c.write(pmxproto.CmdSwitchFolder{Account: acct, Folder: folder})
}

func (c *Client) MarkSeen(acct string, folderID int64, uid uint32, seen bool) error {
	return c.write(pmxproto.CmdMarkSeen{Account: acct, FolderID: folderID, UID: uid, Seen: seen})
}

func (c *Client) MarkFlagged(acct string, folderID int64, uid uint32, flagged bool) error {
	return c.write(pmxproto.CmdMarkFlagged{Account: acct, FolderID: folderID, UID: uid, Flagged: flagged})
}

func (c *Client) MoveToTrash(acct config.Account, folderID int64, uid uint32) error {
	return c.write(pmxproto.CmdMoveToTrash{Account: acct.Name, FolderID: folderID, UID: uid})
}

func (c *Client) MoveToArchive(acct config.Account, folderID int64, uid uint32) error {
	return c.write(pmxproto.CmdMoveToArchive{Account: acct.Name, FolderID: folderID, UID: uid})
}

func (c *Client) MoveToFolder(acct config.Account, folderID int64, uid uint32, dest string) error {
	return c.write(pmxproto.CmdMoveToFolder{Account: acct.Name, FolderID: folderID, UID: uid, Dst: dest})
}

// SendOps implementation — satisfies internal/mailops.SendOps.

func (c *Client) Trigger(acct string) error {
	return c.write(pmxproto.CmdDrain{Account: acct})
}
