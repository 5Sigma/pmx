package mailsync

import (
	"context"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/folders"
	"github.com/5Sigma/pmx/internal/mail"
)

// sweepLoop fires a poke on ch every interval until ctx is cancelled.
// The channel is buffered 1; ticks are dropped when the main loop hasn't
// drained the previous one yet.
func sweepLoop(ctx context.Context, interval time.Duration, ch chan<- struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

// reconcileFolderList compares the server mailbox list against local folder
// rows and applies renames, additions, and disappearances. It is called on
// every (re)connect from watchOnce.
//
// Rename detection: if a local folder is absent from the server list AND a new
// server folder has the same UIDValidity (non-zero), it is treated as a rename.
// Disappeared folders are marked hidden_since; they are cascade-deleted after
// two sweep intervals have elapsed.
func (e *Engine) reconcileFolderList(
	ctx context.Context,
	c *imapclient.Client,
	acct config.Account,
	accountID int64,
	serverMailboxes []folders.Mailbox,
	sweepInterval time.Duration,
	emit func(Event),
) error {
	// Build a set of server names for quick lookup.
	serverByName := make(map[string]folders.Mailbox, len(serverMailboxes))
	for _, mb := range serverMailboxes {
		serverByName[mb.Name] = mb
	}

	// Update special-use map from server list.
	specialUseMap := map[mail.FolderRole]string{}
	for _, mb := range serverMailboxes {
		if mb.Role != "" && mb.Role != mail.RoleInbox {
			specialUseMap[mb.Role] = mb.Name
		}
	}
	if len(specialUseMap) == 0 {
		e.setSpecialUse(acct.Name, nil)
	} else {
		e.setSpecialUse(acct.Name, specialUseMap)
	}

	// Load all local folders (visible only; ListFolders excludes hidden).
	localFolders, err := e.store.ListFolders(accountID)
	if err != nil {
		return err
	}
	localByName := make(map[string]*mail.Folder, len(localFolders))
	for _, f := range localFolders {
		localByName[f.Name] = f
	}

	// An empty server list paired with existing local folders almost certainly
	// means the mailbox LIST failed upstream. Skip the whole reconcile rather than
	// hiding every local folder.
	if len(serverMailboxes) == 0 && len(localFolders) > 0 {
		e.logger.Warn("reconcileFolderList: server returned no mailboxes while local folders exist; skipping",
			"account", acct.Name, "local", len(localFolders))
		return nil
	}

	// Classify: removed = local names absent from server.
	//           added   = server names absent from local.
	var removed []*mail.Folder
	for _, f := range localFolders {
		if _, ok := serverByName[f.Name]; !ok {
			removed = append(removed, f)
		}
	}
	var added []folders.Mailbox
	for _, mb := range serverMailboxes {
		if _, ok := localByName[mb.Name]; !ok {
			added = append(added, mb)
		}
	}

	// Rename pass: match removed folders to added ones by UIDValidity.
	// We need UIDValidity for the added folders; fetch it via STATUS.
	type addedStatus struct {
		mb          folders.Mailbox
		uidValidity uint32
	}
	var addedWithStatus []addedStatus
	for _, mb := range added {
		sd, err := c.Status(mb.Name, &statusOptsForRename).Wait()
		if err != nil {
			e.logger.Warn("reconcileFolderList: STATUS failed for new folder",
				"folder", mb.Name, "err", err)
			addedWithStatus = append(addedWithStatus, addedStatus{mb, 0})
			continue
		}
		addedWithStatus = append(addedWithStatus, addedStatus{mb, sd.UIDValidity})
	}

	renamedOldIDs := make(map[int64]bool)
	renamedNewNames := make(map[string]bool)
	for _, as := range addedWithStatus {
		if as.uidValidity == 0 {
			continue
		}
		for _, old := range removed {
			if renamedOldIDs[old.ID] {
				continue
			}
			if old.UIDValidity != 0 && old.UIDValidity == as.uidValidity {
				// It's a rename.
				if err := e.store.RenameFolder(old.ID, as.mb.Name); err != nil {
					e.logger.Warn("reconcileFolderList: RenameFolder failed",
						"old", old.Name, "new", as.mb.Name, "err", err)
					continue
				}
				if as.mb.Role != "" {
					_ = e.store.UpsertFolderRole(old.ID, as.mb.Role)
				}
				renamedOldIDs[old.ID] = true
				renamedNewNames[as.mb.Name] = true
				e.logger.Info("folder renamed", "account", acct.Name, "old", old.Name, "new", as.mb.Name)
				emit(FolderRenamed{Account: acct.Name, OldName: old.Name, NewName: as.mb.Name, FolderID: old.ID})
				break
			}
		}
	}

	// Add pass: new server folders not matched as renames.
	for _, as := range addedWithStatus {
		if renamedNewNames[as.mb.Name] {
			continue
		}
		fid, err := e.store.EnsureFolder(accountID, as.mb.Name)
		if err != nil {
			e.logger.Warn("reconcileFolderList: EnsureFolder failed", "folder", as.mb.Name, "err", err)
			continue
		}
		if as.mb.Role != "" {
			_ = e.store.UpsertFolderRole(fid, as.mb.Role)
		}
		e.logger.Info("folder added", "account", acct.Name, "name", as.mb.Name)
		emit(FolderAdded{Account: acct.Name, Name: as.mb.Name, FolderID: fid})
	}

	// Ensure roles are current for folders that remain on the server.
	for _, mb := range serverMailboxes {
		if mb.Role == "" {
			continue
		}
		if f, ok := localByName[mb.Name]; ok {
			_ = e.store.UpsertFolderRole(f.ID, mb.Role)
		}
	}

	// Hide pass: newly-removed folders not matched as renames get hidden.
	gracePeriod := 2 * sweepInterval
	if gracePeriod == 0 {
		gracePeriod = 10 * time.Minute
	}
	now := time.Now()
	for _, old := range removed {
		if renamedOldIDs[old.ID] {
			continue
		}
		// old.HiddenSince is always 0 here because ListFolders excludes hidden
		// folders. Mark first-time disappearances hidden and give them a grace period.
		if err := e.store.MarkFolderHidden(old.ID, now); err != nil {
			e.logger.Warn("reconcileFolderList: MarkFolderHidden failed", "folder", old.Name, "err", err)
			continue
		}
		e.logger.Info("folder disappeared (grace)", "account", acct.Name, "name", old.Name)
		emit(FolderDisappeared{Account: acct.Name, Name: old.Name, FolderID: old.ID, Grace: true})
	}

	// Hidden-folder pass: process folders that were hidden in a prior cycle.
	// Un-hide any that reappeared; cascade-delete any whose grace period elapsed.
	hidden, err := e.store.ListHiddenFolders(accountID)
	if err != nil {
		return err
	}
	for _, f := range hidden {
		if _, ok := serverByName[f.Name]; ok {
			// Folder reappeared on the server.
			if err := e.store.UnmarkFolderHidden(f.ID); err != nil {
				e.logger.Warn("reconcileFolderList: UnmarkFolderHidden failed", "folder", f.Name, "err", err)
			}
			continue
		}
		// Still absent from server. Delete if grace period has elapsed.
		if now.Unix()-f.HiddenSince >= int64(gracePeriod.Seconds()) {
			if err := e.store.DeleteFolder(f.ID); err != nil {
				e.logger.Warn("reconcileFolderList: DeleteFolder failed", "folder", f.Name, "err", err)
				continue
			}
			e.logger.Info("folder deleted (grace elapsed)", "account", acct.Name, "name", f.Name)
			emit(FolderDisappeared{Account: acct.Name, Name: f.Name, FolderID: f.ID, Grace: false})
		}
	}

	return nil
}

// statusOptsForRename is the minimal STATUS request used to detect renames.
var statusOptsForRename = imap.StatusOptions{ //nolint:gochecknoglobals
	UIDValidity: true,
}

// runSweep performs a full STATUS-driven sweep across all folders for
// accountID. It re-runs reconcileFolderList (to catch mid-session renames),
// then for each folder whose cached STATUS differs from the server, applies
// reconcileDeletions. It must be called from the IDLE/poll loop while the
// connection is between operations.
//
// Returns the new active folder name (non-empty only if it was renamed) and
// its fresh UIDValidity (non-zero only if changed), plus any fatal error.
func (e *Engine) runSweep(
	ctx context.Context,
	c *imapclient.Client,
	acct config.Account,
	accountID int64,
	activeFolderName string,
	sweepInterval time.Duration,
	emit func(Event),
) (newActiveName string, newActiveUIDValidity uint32, retErr error) {
	e.logger.Debug("sweep: starting", "account", acct.Name)

	// Step 1: LIST-STATUS for all folders.
	statuses, err := folders.ListStatus(c)
	if err != nil {
		return "", 0, err
	}

	// Build a simple Mailbox slice for reconcileFolderList (which does rename
	// detection). We pass only the info it needs (Name + Role).
	mailboxList := make([]folders.Mailbox, 0, len(statuses))
	for _, ms := range statuses {
		mailboxList = append(mailboxList, folders.Mailbox{Name: ms.Name, Role: ms.Role})
	}

	// Step 2: reconcile folder list (renames, new, disappeared).
	if err := e.reconcileFolderList(ctx, c, acct, accountID, mailboxList, sweepInterval, emit); err != nil {
		e.logger.Warn("sweep: reconcileFolderList failed", "err", err)
	}

	// After reconcile, the active folder may have been renamed. Resolve the
	// current active folder name from the store if a FolderRenamed event was
	// emitted. We detect this cheaply by attempting FolderByName; if not found,
	// fall through (the event handler in the UI will catch up).
	currentActive := activeFolderName

	// Step 3: per-folder deletion sweep using STATUS diffs.
	localFolders, err := e.store.ListFolders(accountID)
	if err != nil {
		return newActiveName, 0, err
	}

	serverByName := make(map[string]folders.MailboxStatus, len(statuses))
	for _, ms := range statuses {
		serverByName[ms.Name] = ms
	}

	for _, f := range localFolders {
		select {
		case <-ctx.Done():
			return newActiveName, 0, ctx.Err()
		default:
		}

		ms, ok := serverByName[f.Name]
		if !ok {
			continue // folder gone; reconcileFolderList handles it
		}

		// Skip the active folder — the poke-driven fetchFolder handles it.
		if f.Name == currentActive {
			// But update its cached STATUS so next sweep is accurate.
			_ = e.store.UpdateFolderStatus(f.ID, ms.UIDValidity, ms.UIDNext,
				ms.NumMessages, ms.HighestModSeq, time.Now())
			continue
		}

		// Nothing changed since last sweep.
		if ms.UIDValidity == f.UIDValidity &&
			ms.UIDNext == f.UIDNext &&
			ms.NumMessages == f.NumMessages {
			continue
		}

		e.logger.Debug("sweep: folder changed, reconciling",
			"folder", f.Name,
			"old_num", f.NumMessages, "new_num", ms.NumMessages,
			"old_next", f.UIDNext, "new_next", ms.UIDNext,
		)

		if ms.UIDValidity != f.UIDValidity && ms.UIDValidity != 0 && f.UIDValidity != 0 {
			// UIDVALIDITY changed: full re-fetch. SELECT + fetchFolder.
			selectData, err := c.Select(f.Name, nil).Wait()
			if err != nil {
				e.logger.Warn("sweep: SELECT for full refetch failed", "folder", f.Name, "err", err)
				continue
			}
			if err := e.fetchFolder(ctx, c, acct, f.ID, accountID, selectData.UIDValidity, selectData.HighestModSeq, f.Name, emit); err != nil {
				if ctx.Err() != nil {
					return newActiveName, 0, ctx.Err()
				}
				e.logger.Warn("sweep: fetchFolder failed", "folder", f.Name, "err", err)
			}
		} else {
			// Only counts changed — run a deletion-only reconcile.
			selectData, err := c.Select(f.Name, nil).Wait()
			if err != nil {
				e.logger.Warn("sweep: SELECT for reconcile failed", "folder", f.Name, "err", err)
				continue
			}
			if _, err := e.reconcileDeletions(ctx, c, acct.Name, f.ID, f.Name, emit); err != nil {
				if ctx.Err() != nil {
					return newActiveName, 0, ctx.Err()
				}
				e.logger.Warn("sweep: reconcileDeletions failed", "folder", f.Name, "err", err)
			}
			_ = e.store.UpdateFolderStatus(f.ID, selectData.UIDValidity, ms.UIDNext,
				ms.NumMessages, ms.HighestModSeq, time.Now())
		}
	}

	// Step 4: restore the active folder SELECT so the IDLE loop can resume.
	selectData, err := c.Select(currentActive, nil).Wait()
	if err != nil {
		// Active folder vanished or errored — fall back to INBOX.
		e.logger.Warn("sweep: could not restore active folder SELECT, falling back to INBOX",
			"folder", currentActive, "err", err)
		sd, err2 := c.Select("INBOX", nil).Wait()
		if err2 != nil {
			return "", 0, err2
		}
		return "INBOX", sd.UIDValidity, nil
	}

	// Return updated active-folder state only when it actually changed.
	if newActiveName == "" {
		newActiveName = currentActive
	}
	return newActiveName, selectData.UIDValidity, nil
}
