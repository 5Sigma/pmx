package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/5Sigma/pmx/internal/accountrunner"
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/mcpserver"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start an MCP stdio server exposing mail functionality to AI agents",
	Long: `Serves the MCP (Model Context Protocol) over stdin/stdout.
Configure desktop agents (Claude Code, Claude Desktop, Cursor, Cline, etc.) with:

  pmx mcp

Example ~/.claude/mcp.json entry:
  { "pmx": { "command": "pmx", "args": ["mcp"] } }`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMCP()
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP() error {
	fmt.Fprintln(os.Stderr, "pmx mcp: starting")

	app, err := openApp(fmt.Sprintf("pmx-mcp-%d", os.Getpid()), config.LogPath)
	if err != nil {
		return err
	}
	defer app.Close()

	if len(app.cfg.Accounts) == 0 {
		return fmt.Errorf("no accounts configured; add at least one account to config.toml")
	}
	fmt.Fprintf(os.Stderr, "pmx mcp: config loaded, %d accounts\n", len(app.cfg.Accounts))
	fmt.Fprintln(os.Stderr, "pmx mcp: store opened")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mode := resolveClientMode(app.syncEng, app.sendEng, app.logger)

	var wg sync.WaitGroup
	if mode.DaemonClient != nil {
		// Daemon detected: proxy all mutations over the socket. No local
		// IMAP/SMTP workers — the daemon already owns those connections.
		defer func() { _ = mode.DaemonClient.Close() }()
		fmt.Fprintln(os.Stderr, "pmx mcp: using daemon via socket")
	} else {
		// Standalone mode: start Watch + sender.Run per account. Events are
		// drained but ignored — MCP is request/response, not push.
		for _, acct := range app.cfg.Accounts {
			acct := acct
			syncEvents := make(chan mailsync.Event, 32)
			sendEvents := make(chan mailsend.Event, 32)

			wg.Add(3)
			go func() {
				defer wg.Done()
				accountrunner.RunEngines(ctx, acct, app.syncEng, app.sendEng, app.cfg, app.logger, syncEvents, sendEvents)
			}()
			go func() {
				defer wg.Done()
				for range syncEvents {
				}
			}()
			go func() {
				defer wg.Done()
				for range sendEvents {
				}
			}()
		}
	}

	attachsDir, _ := config.AttachmentsDir()
	fmt.Fprintln(os.Stderr, "pmx mcp: serving on stdio")
	s := mcpserver.New(app.store, mode.Ops, mode.Send, app.cfg, app.sec, attachsDir, app.logger)
	if err := s.Serve(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "pmx mcp: serve error: %v\n", err)
		return err
	}
	fmt.Fprintln(os.Stderr, "pmx mcp: serve returned (stdin closed or context cancelled)")

	cancel()
	wg.Wait()
	return nil
}
