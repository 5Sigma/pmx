package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/pmxd"
	"github.com/5Sigma/pmx/internal/secrets"
	"github.com/5Sigma/pmx/internal/store"
)

func main() {
	var once bool
	flag.BoolVar(&once, "once", false, "run a single cycle then exit")
	flag.Parse()

	logger := newLogger()

	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfigFile) {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	dbPath, err := config.DatabasePath()
	if err != nil {
		logger.Error("db path", "err", err)
		os.Exit(1)
	}
	attachsDir, _ := config.AttachmentsDir()
	st, err := store.Open(dbPath, attachsDir)
	if err != nil {
		logger.Error("store open", "err", err)
		os.Exit(1)
	}
	defer func() { _ = st.Close() }()

	statePath, err := config.PmxdStatePath()
	if err != nil {
		logger.Error("state path", "err", err)
		os.Exit(1)
	}
	state, err := pmxd.LoadState(statePath)
	if err != nil {
		logger.Warn("state load failed (starting fresh)", "err", err)
	}

	sec := secrets.New(cfg)
	claimant := fmt.Sprintf("pmxd-%d", os.Getpid())

	// Satisfy the mail.MessageStore interface for store.Open return value.
	var msgStore mail.MessageStore = st

	syncEng := mailsync.New(msgStore, sec, attachsDir, logger)
	sendEng := mailsend.New(msgStore, sec, cfg.General.SendMaxAttempts, claimant, logger)

	notifier := pmxd.NewNotifier(logger)
	if err := notifier.Connect(); err != nil {
		logger.Warn("desktop notifications unavailable", "err", err)
	}
	defer notifier.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	d := pmxd.New(cfg, msgStore, syncEng, sendEng, notifier, state, logger)

	logger.Info("pmxd starting",
		"accounts", len(cfg.Accounts),
		"interval", cfg.Daemon.CheckInterval,
		"claimant", claimant,
	)

	if once {
		d.RunOnce(ctx)
		_ = state.Save()
		return
	}

	if err := d.Run(ctx); err != nil {
		logger.Error("daemon error", "err", err)
		os.Exit(1)
	}
}

func newLogger() *slog.Logger {
	logPath, err := config.PmxdLogPath()
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
