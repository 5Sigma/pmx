package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/pmxd"
	"github.com/5Sigma/pmx/internal/pmxlock"
	"github.com/5Sigma/pmx/internal/pmxsock"
	"github.com/5Sigma/pmx/internal/version"
)

var daemonOnce bool

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the pmx mail sync daemon",
	Long:  `Syncs mail in the background and sends desktop notifications for new messages.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon(daemonOnce)
	},
}

func init() {
	daemonCmd.Flags().BoolVar(&daemonOnce, "once", false, "run a single sync cycle then exit")
	rootCmd.AddCommand(daemonCmd)
}

func runDaemon(once bool) error {
	lockPath, err := config.PmxdLockPath()
	if err != nil {
		return fmt.Errorf("lock path: %w", err)
	}
	lock, err := pmxlock.TryAcquire(lockPath)
	if err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("another pmx daemon is already running")
		}
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	claimant := fmt.Sprintf("pmx-daemon-%d", os.Getpid())
	app, err := openApp(claimant, config.PmxdLogPath)
	if err != nil {
		return err
	}
	defer app.Close()

	statePath, err := config.PmxdStatePath()
	if err != nil {
		return fmt.Errorf("state path: %w", err)
	}
	state, err := pmxd.LoadState(statePath)
	if err != nil {
		app.logger.Warn("state load failed (starting fresh)", "err", err)
	}

	notifier := pmxd.NewNotifier(app.logger)
	if err := notifier.Connect(); err != nil {
		app.logger.Warn("desktop notifications unavailable", "err", err)
	}
	defer notifier.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Start the Unix socket server for TUI ↔ daemon IPC before the daemon
	// loop begins, so clients can connect as soon as the daemon is ready.
	srv := pmxsock.NewServer(app.syncEng, app.sendEng, app.cfg, app.logger)
	sockPath, _ := config.PmxdSocketPath()
	go func() {
		if err := srv.Listen(ctx, sockPath); err != nil && ctx.Err() == nil {
			app.logger.Error("pmxsock server exited", "err", err)
		}
	}()

	d := pmxd.New(app.cfg, app.store, app.syncEng, app.sendEng, notifier, state, app.logger)
	d.SetBroadcaster(srv)

	app.logger.Info("pmx daemon starting",
		"version", version.String(),
		"accounts", len(app.cfg.Accounts),
		"interval", app.cfg.Daemon.CheckInterval,
		"claimant", claimant,
	)

	if once {
		d.RunOnce(ctx)
		_ = state.Save()
		return nil
	}

	if err := d.Run(ctx); err != nil {
		return fmt.Errorf("daemon error: %w", err)
	}
	return nil
}
