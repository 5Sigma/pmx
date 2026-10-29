package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/mailsync"
	"github.com/5Sigma/pmx/internal/secrets"
	"github.com/5Sigma/pmx/internal/store"
)

type appState struct {
	cfg      *config.Config
	store    mail.MessageStore
	rawStore *store.Store
	sec      *secrets.Store
	syncEng  *mailsync.Engine
	sendEng  *mailsend.Engine
	logger   *slog.Logger
}

func (a *appState) Close() {
	if a.store != nil {
		_ = a.store.Close()
	}
}

// openApp loads config, opens the store, and constructs sync/send engines.
// claimant is the per-process string used for outbox ownership.
// logPathFn resolves the log file path (e.g. config.LogPath or config.PmxdLogPath).
func openApp(claimant string, logPathFn func() (string, error)) (*appState, error) {
	cfg, err := config.Load()
	if err != nil && !errors.Is(err, config.ErrNoConfigFile) {
		return nil, fmt.Errorf("config: %w", err)
	}

	dbPath, err := config.DatabasePath()
	if err != nil {
		return nil, fmt.Errorf("database path: %w", err)
	}
	attachsDir, _ := config.AttachmentsDir()

	st, err := store.Open(dbPath, attachsDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	logger := newLoggerFrom(logPathFn)
	sec := secrets.New(cfg)

	var msgStore mail.MessageStore = st
	syncEng := mailsync.New(msgStore, sec, attachsDir, logger)
	sendEng := mailsend.New(msgStore, sec, cfg.General.SendMaxAttempts, claimant, logger)

	return &appState{
		cfg:      cfg,
		store:    st,
		rawStore: st,
		sec:      sec,
		syncEng:  syncEng,
		sendEng:  sendEng,
		logger:   logger,
	}, nil
}

// newLoggerFrom opens the log file returned by pathFn, falling back to stderr.
func newLoggerFrom(pathFn func() (string, error)) *slog.Logger {
	if pathFn != nil {
		if logPath, err := pathFn(); err == nil {
			if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
				return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
			}
		}
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
