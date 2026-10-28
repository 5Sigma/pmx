package mailsend

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/folders"
	"github.com/5Sigma/pmx/internal/imapauth"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/secrets"
)

type sendResult struct {
	sent   bool
	failed bool
}

// SendOne transmits a single outbox item. It resolves its own credentials and
// is safe to call from an external service daemon without any engine overhead.
func SendOne(
	ctx context.Context,
	store mail.MessageStore,
	sec *secrets.Store,
	logger *slog.Logger,
	acct config.Account,
	item *mail.OutboxItem,
	maxAttempts int,
) (sendResult, error) {
	if item.Attempts >= maxAttempts {
		return sendResult{}, nil // terminal — skip silently
	}

	pass, err := sec.Get(acct.Name)
	if err != nil {
		return sendResult{}, fmt.Errorf("resolve secret: %w", err)
	}

	return sendOneWithPass(ctx, store, pass, logger, acct, item, maxAttempts)
}

// sendOneWithPass is like SendOne but uses an already-resolved password
// instead of calling sec.Get again. Used by the Engine to avoid re-hitting
// the secrets store on every item in a batch.
func sendOneWithPass(
	ctx context.Context,
	store mail.MessageStore,
	pass string,
	logger *slog.Logger,
	acct config.Account,
	item *mail.OutboxItem,
	maxAttempts int,
) (sendResult, error) {
	if item.Attempts >= maxAttempts {
		return sendResult{}, nil
	}

	from, rcpts, err := extractEnvelope(item.RawEML)
	if err != nil {
		// Malformed EML — park immediately by bumping to maxAttempts.
		logger.Warn("sendOneWithPass: malformed EML, parking", "item_id", item.ID, "err", err)
		msg := fmt.Sprintf("malformed EML: %v", err)
		for item.Attempts < maxAttempts {
			if merr := store.MarkOutboxFailed(item.ID, msg); merr != nil {
				return sendResult{}, fmt.Errorf("mark failed: %w", merr)
			}
			item.Attempts++
		}
		return sendResult{failed: true}, nil
	}

	if err := sendSMTP(ctx, acct, pass, from, rcpts, item.RawEML); err != nil {
		logger.Warn("sendOneWithPass: SMTP failure", "item_id", item.ID, "attempts", item.Attempts+1, "err", err)
		if merr := store.MarkOutboxFailed(item.ID, err.Error()); merr != nil {
			return sendResult{}, fmt.Errorf("mark failed: %w", merr)
		}
		return sendResult{failed: true}, nil
	}

	// Record sent recipients in the contacts frequency index.
	if rTo, rCc, rBcc, rErr := extractAddressedParties(item.RawEML); rErr == nil {
		allRecipients := make([]mail.Address, 0, len(rTo)+len(rCc)+len(rBcc))
		allRecipients = append(allRecipients, rTo...)
		allRecipients = append(allRecipients, rCc...)
		allRecipients = append(allRecipients, rBcc...)
		items := make([]mail.ContactUpsert, 0, len(allRecipients))
		for _, a := range allRecipients {
			lc := strings.ToLower(a.Email)
			if lc == "" || mail.IsSystemAddress(lc) {
				continue
			}
			items = append(items, mail.ContactUpsert{
				Address: mail.Address{Name: a.Name, Email: lc},
				Kind:    mail.ContactSent,
			})
		}
		if len(items) > 0 {
			if cerr := store.UpsertContacts(item.AccountID, items, time.Now()); cerr != nil {
				logger.Warn("sent contact upsert failed", "err", cerr)
			}
		}
	}

	// SMTP accepted — APPEND to Sent folder before deleting the row.
	appendToSent(ctx, acct, pass, item.RawEML, logger)

	if err := store.MarkOutboxSent(item.ID); err != nil {
		// Row may already be gone on a retry; treat as success.
		logger.Warn("sendOneWithPass: MarkOutboxSent error (ignored)", "item_id", item.ID, "err", err)
	}
	return sendResult{sent: true}, nil
}

// appendToSent opens a fresh IMAP session and APPENDs the EML to the Sent
// mailbox. Failures are logged as warnings and never block the outbox-delete
// step (SMTP already accepted; retrying would double-send).
func appendToSent(ctx context.Context, acct config.Account, pass string, raw []byte, logger *slog.Logger) {
	c, err := imapauth.ConnectAndLogin(acct, pass, nil)
	if err != nil {
		logger.Warn("appendToSent: IMAP connect failed", "account", acct.Name, "err", err)
		return
	}
	defer func() {
		_ = c.Logout().Wait()
		_ = c.Close()
	}()

	discovered, err := folders.Discover(c)
	if err != nil {
		logger.Warn("appendToSent: SPECIAL-USE discovery failed", "account", acct.Name, "err", err)
	}
	mailbox := folders.Resolve(acct, discovered, mail.RoleSent)

	// Context cancellation watchdog.
	go func() {
		<-ctx.Done()
		_ = c.Close()
	}()

	appendCmd := c.Append(mailbox, int64(len(raw)), &imap.AppendOptions{
		Flags: []imap.Flag{imap.FlagSeen},
		Time:  time.Now(),
	})
	if _, err := appendCmd.Write(raw); err != nil {
		logger.Warn("appendToSent: IMAP APPEND write failed", "account", acct.Name, "mailbox", mailbox, "err", err)
		return
	}
	if err := appendCmd.Close(); err != nil {
		logger.Warn("appendToSent: IMAP APPEND close failed", "account", acct.Name, "mailbox", mailbox, "err", err)
		return
	}
	if _, err := appendCmd.Wait(); err != nil {
		logger.Warn("appendToSent: IMAP APPEND wait failed", "account", acct.Name, "mailbox", mailbox, "err", err)
	}
}
