package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/5Sigma/pmx/internal/accountrunner"
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/secrets"
	"github.com/5Sigma/pmx/internal/store"
	"github.com/5Sigma/pmx/internal/ui"
	"github.com/5Sigma/pmx/internal/version"
)

var rootCmd = &cobra.Command{
	Use:     "pmx",
	Short:   "Phreakmx — a terminal email client",
	Long:    `Phreakmx (pmx) is a keyboard-driven terminal email client.`,
	Version: version.String(),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runTUI() error {
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfigFile) {
		return fmt.Errorf("invalid config: %w", err)
	}
	if errors.Is(err, config.ErrNoConfigFile) {
		// First run: write defaults and continue.
		if saveErr := config.Save(cfg); saveErr != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write default config: %v\n", saveErr)
		}
	}

	var opts []tea.ProgramOption
	if cfg.Features.AltScreen {
		opts = append(opts, tea.WithAltScreen())
	}
	if cfg.Features.Mouse {
		opts = append(opts, tea.WithMouseCellMotion())
	}

	dbPath, dbErr := config.DatabasePath()
	attachsDir, _ := config.AttachmentsDir()
	var st mail.MessageStore
	if dbErr == nil {
		opened, err := store.Open(dbPath, attachsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not open mail store: %v\n", err)
		} else {
			st = opened
			defer func() { _ = st.Close() }()
		}
	}

	logger := newLoggerFrom(config.LogPath)
	logger.Info("pmx starting",
		"db", dbPath,
		"store_ok", st != nil,
		"num_accounts", len(cfg.Accounts),
	)

	sec := secrets.New(cfg)

	var engine *mailsync.Engine
	if st != nil {
		engine = mailsync.New(st, sec, attachsDir, logger)
	} else {
		logger.Warn("store unavailable, sync disabled", "db_err", dbErr)
	}

	var sender *mailsend.Engine
	if st != nil {
		sender = mailsend.New(st, sec, cfg.General.SendMaxAttempts, fmt.Sprintf("pmx-ui-%d", os.Getpid()), logger)
	}

	// Resolve the initial account via UIState → DefaultAccount → first account.
	uiState, _ := config.LoadUIState()
	initialAcct, _ := config.ResolveInitialAccount(cfg, uiState)

	if initialAcct != nil {
		logger.Info("initial account resolved",
			"name", initialAcct.Name,
			"imap_host", initialAcct.IMAPHost,
			"imap_port", initialAcct.IMAPPort,
			"imap_tls", initialAcct.IMAPTLS,
			"idle_enabled", initialAcct.IDLEEnabled == nil || *initialAcct.IDLEEnabled,
		)
	} else {
		logger.Warn("no account resolved; sync disabled")
	}

	// Resolve the account PK and the INBOX folder PK so the TUI starts with
	// a stable folder context instead of a magic ID. Both default to zero on
	// error — the mail list tolerates that and simply renders empty until a
	// sync populates the store.
	var accountID, inboxFolderID int64
	if st != nil && initialAcct != nil {
		if id, err := st.EnsureAccount(initialAcct.Name, initialAcct.Email); err == nil {
			accountID = id
			if fid, err := st.EnsureFolder(id, "INBOX"); err == nil {
				inboxFolderID = fid
				_ = st.UpsertFolderRole(fid, mail.RoleInbox)
			}
		}
	}

	// Probe for the pmx daemon via its Unix socket. If reachable, the TUI
	// becomes a thin client: ops are proxied over the socket, events are pushed
	// in from the daemon. If unreachable, the TUI starts its own accountrunner.
	mode := resolveClientMode(engine, sender, logger)

	// The accountrunner owns per-account IMAP goroutines in standalone mode.
	// In daemon mode the daemon owns those goroutines so the runner is unused;
	// the UI's switch logic tolerates a nil runner.
	var runner *accountrunner.Runner
	if mode.DaemonClient == nil {
		runner = accountrunner.New(engine, sender, cfg, logger, nil)
		defer func() { _ = runner.Stop() }()
	}

	p := tea.NewProgram(
		ui.New(st, mode.Ops, mode.Send, mode.Status, initialAcct, cfg, accountID, inboxFolderID, runner, mode.DaemonClient != nil),
		opts...,
	)
	if runner != nil {
		runner.SetProgram(p)
	}

	if mode.DaemonClient != nil {
		// Start the event reader goroutine. It forwards all daemon events to
		// the bubbletea program and sends DaemonDisconnectedMsg on closure.
		go mode.DaemonClient.Events(context.Background(), p)
		defer func() { _ = mode.DaemonClient.Close() }()
	} else if initialAcct != nil {
		if err := runner.Start(*initialAcct); err != nil {
			logger.Warn("failed to start initial account goroutines",
				"account", initialAcct.Name, "err", err)
		}
	}

	_, err = p.Run()
	return err
}
