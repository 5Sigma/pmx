package cmd

import (
	"errors"
	"fmt"
	"log/slog"
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

// newLogger opens (or creates) the pmx log file and returns a slog.Logger
// writing to it. Falls back to stderr if the file can't be opened so the
// logger is always non-nil.
func newLogger() *slog.Logger {
	logPath, err := config.LogPath()
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

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

	logger := newLogger()
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

	// Runner is created before the program so ui.New can hold a reference.
	// SetProgram is called after tea.NewProgram to break the circular dependency.
	runner := accountrunner.New(engine, sender, cfg, logger, nil)

	p := tea.NewProgram(
		ui.New(st, engine, sender, initialAcct, cfg, accountID, inboxFolderID, runner),
		opts...,
	)
	runner.SetProgram(p)

	if initialAcct != nil {
		if err := runner.Start(*initialAcct); err != nil {
			logger.Warn("failed to start initial account goroutines",
				"account", initialAcct.Name, "err", err)
		}
	}
	defer func() { _ = runner.Stop() }()

	_, err = p.Run()
	return err
}
