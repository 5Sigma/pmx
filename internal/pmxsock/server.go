package pmxsock

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/pmxproto"
)

type serverClient struct {
	ch chan any // buffered 128; drop on full rather than block the daemon
}

// Server listens on a Unix socket, broadcasts events to all connected clients,
// and dispatches fire-and-forget commands onto the live engines.
type Server struct {
	engine *mailsync.Engine
	sender *mailsend.Engine
	cfg    *config.Config
	logger *slog.Logger

	mu      sync.Mutex
	clients map[*serverClient]struct{}
}

// NewServer creates a Server. Call Listen to start accepting connections.
func NewServer(engine *mailsync.Engine, sender *mailsend.Engine, cfg *config.Config, logger *slog.Logger) *Server {
	return &Server{
		engine:  engine,
		sender:  sender,
		cfg:     cfg,
		logger:  logger,
		clients: make(map[*serverClient]struct{}),
	}
}

// Broadcast fans ev out to all connected clients. Per-client send is
// non-blocking: if the client channel is full the event is dropped for that
// client (a stale TUI is preferable to a blocked daemon).
func (s *Server) Broadcast(ev any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		select {
		case c.ch <- ev:
		default:
			s.logger.Warn("pmxsock: client event channel full, dropping event")
		}
	}
}

// Listen starts the Unix socket listener. Blocks until ctx is cancelled.
func (s *Server) Listen(ctx context.Context, socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return err
	}
	// Remove a stale socket left by a previously crashed daemon.
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, 0600); err != nil {
		_ = ln.Close()
		return err
	}
	defer func() { _ = ln.Close() }()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown triggered by context
			}
			s.logger.Error("pmxsock: accept error", "err", err)
			continue
		}
		c := &serverClient{ch: make(chan any, 128)}
		s.mu.Lock()
		s.clients[c] = struct{}{}
		s.mu.Unlock()
		go s.handleConn(ctx, conn, c)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn, c *serverClient) {
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		close(c.ch)
	}()

	// Writer goroutine: pull events from c.ch and write them to the socket.
	go func() {
		w := bufio.NewWriter(conn)
		for ev := range c.ch {
			if err := pmxproto.EncodeEvent(w, ev); err != nil {
				return
			}
			_ = w.Flush()
		}
	}()

	// Reader (this goroutine): decode commands and dispatch to engines.
	r := bufio.NewReader(conn)
	for {
		cmd, err := pmxproto.DecodeCommand(r)
		if err != nil {
			return // EOF or closed connection
		}
		s.dispatch(c, cmd)
	}
}

func (s *Server) dispatch(reply *serverClient, cmd any) {
	switch c := cmd.(type) {
	case pmxproto.CmdRefresh:
		_ = s.engine.Refresh(c.Account)
	case pmxproto.CmdSwitchFolder:
		_ = s.engine.SwitchFolder(c.Account, c.Folder)
	case pmxproto.CmdMarkSeen:
		_ = s.engine.MarkSeen(c.Account, c.FolderID, c.UID, c.Seen)
	case pmxproto.CmdMarkFlagged:
		_ = s.engine.MarkFlagged(c.Account, c.FolderID, c.UID, c.Flagged)
	case pmxproto.CmdMoveToTrash:
		acct := s.cfg.AccountByName(c.Account)
		if acct != nil {
			_ = s.engine.MoveToTrash(*acct, c.FolderID, c.UID)
		}
	case pmxproto.CmdMoveToArchive:
		acct := s.cfg.AccountByName(c.Account)
		if acct != nil {
			_ = s.engine.MoveToArchive(*acct, c.FolderID, c.UID)
		}
	case pmxproto.CmdMoveToFolder:
		acct := s.cfg.AccountByName(c.Account)
		if acct != nil {
			_ = s.engine.MoveToFolder(*acct, c.FolderID, c.UID, c.Dst)
		}
	case pmxproto.CmdDrain:
		_ = s.sender.Trigger(c.Account)
	case pmxproto.CmdWatcherState:
		state := s.engine.WatcherState(c.Account)
		select {
		case reply.ch <- state:
		default:
			s.logger.Warn("pmxsock: client event channel full, dropping watcher state reply", "account", c.Account)
		}
	default:
		s.logger.Warn("pmxsock: unknown command type", "type", fmt.Sprintf("%T", cmd))
	}
}
