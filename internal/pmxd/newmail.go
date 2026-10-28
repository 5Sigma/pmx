package pmxd

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
)

// notifyNewMail checks for unseen messages that arrived since the last cursor
// position for each configured notify folder.
func notifyNewMail(
	ctx context.Context,
	st mail.MessageStore,
	acct config.Account,
	cfg *config.Config,
	state *State,
	notifier *Notifier,
	logger *slog.Logger,
) {
	if !cfg.Daemon.NotificationsEnabled {
		return
	}
	accountID, err := st.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		logger.Warn("new mail: ensure account", "acct", acct.Name, "err", err)
		return
	}

	for _, folderRole := range cfg.Daemon.NotifyFolders {
		folder, err := st.FolderByRole(accountID, mail.FolderRole(folderRole))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // folder not synced yet
			}
			logger.Warn("new mail: folder by role", "acct", acct.Name, "role", folderRole, "err", err)
			continue
		}

		cursor, exists := state.GetCursor(acct.Name, folder.Name)
		if !exists {
			// First run: seed cursor from folder's current UIDNext minus 1 so we
			// don't retroactively notify all existing messages.
			var seed uint32
			if folder.UIDNext > 0 {
				seed = folder.UIDNext - 1
			}
			state.SetCursor(acct.Name, folder.Name, seed)
			continue
		}

		msgs, err := st.NewMessagesSince(folder.ID, cursor, 20)
		if err != nil {
			logger.Warn("new mail: query failed", "acct", acct.Name, "folder", folder.Name, "err", err)
			continue
		}

		for _, m := range msgs {
			if ctx.Err() != nil {
				return
			}
			from := m.From.Name
			if from == "" {
				from = m.From.Email
			}
			notifier.Notify(from, m.Subject, acct.Name)
			if m.UID > cursor {
				cursor = m.UID
			}
		}
		state.SetCursor(acct.Name, folder.Name, cursor)
	}
}
