package cmd

import (
	"log/slog"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mailops"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/pmxsock"
)

// clientMode holds the resolved MailOps / SendOps and an optional daemon
// client when the pmx daemon was detected via its Unix socket.
type clientMode struct {
	Ops          mailops.MailOps
	Send         mailops.SendOps
	DaemonClient *pmxsock.Client
}

// resolveClientMode dials the daemon's socket. On success it returns a mode
// backed by the socket client. On failure it falls back to the in-process
// engines, which may be nil when the mail store is unavailable.
func resolveClientMode(engine *mailsync.Engine, sender *mailsend.Engine, logger *slog.Logger) clientMode {
	sockPath, _ := config.PmxdSocketPath()
	if dc, err := pmxsock.Dial(sockPath, logger); err == nil {
		logger.Info("pmx daemon detected via socket; proxying ops over it")
		return clientMode{Ops: dc, Send: dc, DaemonClient: dc}
	}
	return clientMode{Ops: engine, Send: sender}
}
