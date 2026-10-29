package mailsync

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/quotedprintable"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/folders"
	"github.com/5Sigma/pmx/internal/imapauth"
	"github.com/5Sigma/pmx/internal/mail"
)

const fetchBatchSize = 50

// syncFolder performs the full synchronisation algorithm for folderName:
//  1. Resolve credentials and connect.
//  2. SELECT the folder and compare UIDValidity.
//  3. Ensure account/folder rows; load existing UIDs.
//  4. Delegate to fetchFolder for the actual sync work.
func (e *Engine) syncFolder(ctx context.Context, acct config.Account, folderName string, emit func(Event)) error {
	// 1. Resolve secret.
	e.logger.Debug("resolving secret", "account", acct.Name)
	pass, err := e.secrets.Get(acct.Name)
	if err != nil {
		e.logger.Error("secret resolution failed", "account", acct.Name, "err", err)
		return fmt.Errorf("resolve secret: %w", err)
	}

	// 2. Dial + login.
	e.logger.Debug("connecting", "host", acct.IMAPHost, "port", acct.IMAPPort, "tls", acct.IMAPTLS)
	c, err := imapauth.ConnectAndLogin(acct, pass, nil)
	if err != nil {
		e.logger.Error("connect failed", "account", acct.Name, "err", err)
		return fmt.Errorf("connect: %w", err)
	}
	e.logger.Debug("connected", "account", acct.Name)
	defer func() { _ = c.Close() }()

	if discovered, err := folders.Discover(c); err != nil {
		e.logger.Warn("syncFolder: SPECIAL-USE discovery failed", "account", acct.Name, "err", err)
	} else {
		e.setSpecialUse(acct.Name, discovered)
	}

	// Context watchdog: close the connection on cancellation.
	// go-imap/v2 does not accept context on individual commands.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		_ = c.Close()
	}()

	// 3. SELECT the folder.
	selectData, err := c.Select(folderName, nil).Wait()
	if err != nil {
		e.logger.Error("SELECT failed", "folder", folderName, "account", acct.Name, "err", err)
		return fmt.Errorf("select %s: %w", folderName, err)
	}
	e.logger.Debug("folder selected", "folder", folderName, "uid_validity", selectData.UIDValidity, "num_messages", selectData.NumMessages)
	serverUIDValidity := selectData.UIDValidity

	// 4. Ensure account and folder rows.
	accountID, err := e.store.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		return err
	}
	folderID, err := e.store.EnsureFolder(accountID, folderName)
	if err != nil {
		return err
	}

	return e.fetchFolder(ctx, c, acct, folderID, accountID, serverUIDValidity, selectData.HighestModSeq, folderName, emit)
}

// shouldRecordContacts returns false for folders whose senders/recipients
// are not useful autocomplete candidates (Sent, Drafts, Trash, Junk).
func shouldRecordContacts(r mail.FolderRole) bool {
	switch r {
	case mail.RoleSent, mail.RoleDrafts, mail.RoleTrash, mail.RoleJunk:
		return false
	}
	return true
}

// fetchFolder performs one catch-up sync pass on an already-authenticated,
// folder-selected client c. serverUIDValidity is the value returned by the
// prior SELECT. serverHighestModSeq is the HIGHESTMODSEQ from the same SELECT;
// pass 0 when no fresh SELECT was performed (poke-driven calls). It emits
// FolderStarted/FolderDone via emit and respects ctx cancellation between
// batches. It does NOT login, logout, or SELECT.
func (e *Engine) fetchFolder(ctx context.Context, c *imapclient.Client, acct config.Account, folderID, accountID int64, serverUIDValidity uint32, serverHighestModSeq uint64, folderName string, emit func(Event)) error {
	folder, err := e.store.GetFolder(folderID)
	if err != nil {
		return err
	}

	recordContacts := shouldRecordContacts(folder.Role)
	var contactBuf []mail.ContactUpsert
	ownEmailLower := strings.ToLower(acct.Email)

	emit(FolderStarted{Account: acct.Name, Folder: folderName})

	// UID validity check.
	if serverUIDValidity > 0 && folder.UIDValidity > 0 && folder.UIDValidity != serverUIDValidity {
		e.logger.Info("UIDVALIDITY changed, clearing folder", "folder", folderName,
			"old", folder.UIDValidity, "new", serverUIDValidity)
		if err := e.store.ClearFolderMessages(folderID); err != nil {
			return err
		}
	}

	// Load existing UIDs from store.
	existing, err := e.store.ExistingUIDs(folderID)
	if err != nil {
		return err
	}
	e.logger.Debug("fetchFolder start", "account", acct.Name, "folder", folderName, "local_uids", len(existing))

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// UID SEARCH ALL — full set from server.
	searchData, err := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return fmt.Errorf("uid search: %w", err)
	}
	serverUIDs := searchData.AllUIDs()
	e.logger.Debug("UID search complete", "server_uids", len(serverUIDs))

	// Compute new and gone UID sets.
	serverSet := make(map[imap.UID]struct{}, len(serverUIDs))
	for _, u := range serverUIDs {
		serverSet[u] = struct{}{}
	}
	var newUIDs []imap.UID
	for _, u := range serverUIDs {
		if _, ok := existing[uint32(u)]; !ok {
			newUIDs = append(newUIDs, u)
		}
	}
	var goneUIDs []uint32
	for uid := range existing {
		if _, ok := serverSet[imap.UID(uid)]; !ok {
			goneUIDs = append(goneUIDs, uid)
		}
	}

	added, updated, deleted := 0, 0, 0

	// Interleaved fetch: for each batch of fetchBatchSize new UIDs we run both
	// sub-passes before moving on. This keeps the first messages visible in the
	// UI within a few seconds of opening any folder, regardless of total size.
	//
	// Sub-pass A: one FETCH for envelopes + BODYSTRUCTURE (fast metadata).
	// Sub-pass B: one FETCH for all text/HTML body sections in the batch.
	// Attachments: one streaming FETCH per attachment (avoids large buffers).
	// Persist: UpsertMessage + InsertAttachment for each message in the batch.
	// Emit:    FolderDone{Final: false} after every non-last batch so the UI
	//          can reload and show partial results; the true FolderDone
	//          (Final: true, with full counts) is emitted at the end.

	type msgMeta struct {
		msg      *mail.Message
		textPart *partInfo
		htmlPart *partInfo
		attParts []partInfo
	}
	for i := 0; i < len(newUIDs); i += fetchBatchSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		batch := newUIDs[i:min(i+fetchBatchSize, len(newUIDs))]

		// Sub-pass A: envelopes + body structure + References header.
		// imap.Envelope has no References field, so we request it as a
		// BODY[HEADER.FIELDS (References)] section alongside the envelope.
		refsSec := &imap.FetchItemBodySection{
			Peek:         true,
			Specifier:    imap.PartSpecifierHeader,
			HeaderFields: []string{"References"},
		}
		p1bufs, err := c.Fetch(toUIDSet(batch), &imap.FetchOptions{
			UID:           true,
			Envelope:      true,
			Flags:         true,
			RFC822Size:    true,
			InternalDate:  true,
			BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
			BodySection:   []*imap.FetchItemBodySection{refsSec},
		}).Collect()
		if err != nil {
			return fmt.Errorf("pass1 fetch: %w", err)
		}

		var batchMetas []msgMeta
		for _, buf := range p1bufs {
			msg := &mail.Message{FolderID: folderID}
			if buf.Envelope != nil {
				applyEnvelope(buf.Envelope, msg)
			}
			if raw := buf.FindBodySection(refsSec); raw != nil {
				applyReferencesHeader(raw, msg)
			}
			msg.UID = uint32(buf.UID)
			msg.Flags = parseFlags(buf.Flags)
			msg.SizeBytes = buf.RFC822Size
			msg.DateReceived = buf.InternalDate
			var text, html *partInfo
			var atts []partInfo
			if buf.BodyStructure != nil {
				text, html, atts = walkBody(buf.BodyStructure)
			}
			batchMetas = append(batchMetas, msgMeta{msg, text, html, atts})
		}

		// Sub-pass B: fetch text/HTML bodies, grouped by section-path signature.
		// A union-sections approach (requesting all paths for all UIDs) causes
		// strict servers to return "NO no such parts" when a message doesn't
		// have a requested section. Instead, messages with identical section
		// requirements are grouped together so each FETCH command only asks for
		// paths that every UID in that sub-group actually has.
		// In practice a folder contains 1–3 distinct structures (single-part,
		// multipart text+html, multipart with attachments), so the number of
		// sub-group FETCHes per batch is tiny.
		type bodyGroup struct {
			textSec *imap.FetchItemBodySection
			htmlSec *imap.FetchItemBodySection
			indices []int // into batchMetas
		}
		// Signature: "<textPath>:<textSpec>|<htmlPath>:<htmlSpec>"
		bodyGroups := make(map[string]*bodyGroup)

		for j := range batchMetas {
			m := &batchMetas[j]
			var textKey, htmlKey string
			var textSec, htmlSec *imap.FetchItemBodySection

			if m.textPart != nil {
				spec := imap.PartSpecifierNone
				if len(m.textPart.path) == 0 {
					spec = imap.PartSpecifierText
				}
				textKey = partPath(m.textPart.path) + ":" + string(spec)
				textSec = &imap.FetchItemBodySection{Peek: true, Part: m.textPart.path, Specifier: spec}
			}
			if m.htmlPart != nil {
				spec := imap.PartSpecifierNone
				if len(m.htmlPart.path) == 0 {
					spec = imap.PartSpecifierText
				}
				htmlKey = partPath(m.htmlPart.path) + ":" + string(spec)
				htmlSec = &imap.FetchItemBodySection{Peek: true, Part: m.htmlPart.path, Specifier: spec}
			}

			sig := textKey + "|" + htmlKey
			if grp, exists := bodyGroups[sig]; exists {
				grp.indices = append(grp.indices, j)
			} else {
				bodyGroups[sig] = &bodyGroup{textSec: textSec, htmlSec: htmlSec, indices: []int{j}}
			}
		}

		for _, grp := range bodyGroups {
			var sections []*imap.FetchItemBodySection
			if grp.textSec != nil {
				sections = append(sections, grp.textSec)
			}
			if grp.htmlSec != nil {
				sections = append(sections, grp.htmlSec)
			}
			if len(sections) == 0 {
				continue
			}

			grpUIDs := make([]imap.UID, len(grp.indices))
			for k, idx := range grp.indices {
				grpUIDs[k] = imap.UID(batchMetas[idx].msg.UID)
			}
			uidToIdx := make(map[imap.UID]int, len(grp.indices))
			for _, idx := range grp.indices {
				uidToIdx[imap.UID(batchMetas[idx].msg.UID)] = idx
			}

			p2bufs, err := c.Fetch(toUIDSet(grpUIDs), &imap.FetchOptions{
				UID:         true,
				BodySection: sections,
			}).Collect()
			if err != nil {
				return fmt.Errorf("pass2 body fetch: %w", err)
			}
			for _, buf := range p2bufs {
				idx, ok := uidToIdx[buf.UID]
				if !ok {
					continue
				}
				m := &batchMetas[idx]
				if grp.textSec != nil && m.textPart != nil {
					if raw := buf.FindBodySection(grp.textSec); raw != nil {
						m.msg.BodyText = decodeBody(raw, m.textPart.encoding, m.textPart.charset)
					}
				}
				if grp.htmlSec != nil && m.htmlPart != nil {
					if raw := buf.FindBodySection(grp.htmlSec); raw != nil {
						m.msg.BodyHTML = decodeBody(raw, m.htmlPart.encoding, m.htmlPart.charset)
					}
				}
			}
		}

		// Persist each message and stream its attachments to disk.
		for j := range batchMetas {
			m := &batchMetas[j]

			var mailAttachments []mail.Attachment
			for _, att := range m.attParts {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				uidSet := toUIDSet([]imap.UID{imap.UID(m.msg.UID)})
				attSections := []*imap.FetchItemBodySection{{Peek: true, Part: att.path}}
				cmd := c.Fetch(uidSet, &imap.FetchOptions{UID: true, BodySection: attSections})
				fetchErr := func() error {
					for {
						data := cmd.Next()
						if data == nil {
							break
						}
						for {
							item := data.Next()
							if item == nil {
								break
							}
							if sec, ok := item.(imapclient.FetchItemDataBodySection); ok {
								if sec.Literal == nil {
									continue
								}
								r := decodeAttachmentReader(sec.Literal, att.encoding)
								diskPath, sha, sz, werr := writeAttachment(r, e.attachs)
								if werr != nil {
									_, _ = io.Copy(io.Discard, sec.Literal)
									return fmt.Errorf("write attachment: %w", werr)
								}
								mailAttachments = append(mailAttachments, mail.Attachment{
									Filename:  att.filename,
									MIMEType:  att.mime,
									SizeBytes: sz,
									SHA256:    sha,
									ContentID: att.contentID,
									DiskPath:  diskPath,
								})
							}
						}
					}
					return nil
				}()
				if ferr := cmd.Close(); ferr != nil {
					if fetchErr == nil {
						fetchErr = fmt.Errorf("pass2 attach close: %w", ferr)
					} else {
						e.logger.Warn("pass2 attach close failed (shadowed by prior error)",
							"account", acct.Name, "uid", m.msg.UID, "close_err", ferr)
					}
				}
				if fetchErr != nil {
					return fetchErr
				}
			}
			m.msg.HasAttach = len(mailAttachments) > 0

			if err := e.store.UpsertMessage(m.msg); err != nil {
				return err
			}
			for k := range mailAttachments {
				mailAttachments[k].MessageID = m.msg.ID
				if err := e.store.InsertAttachment(&mailAttachments[k]); err != nil {
					return err
				}
			}
			if recordContacts {
				from := m.msg.From
				lc := strings.ToLower(from.Email)
				if lc != "" && lc != ownEmailLower && !mail.IsSystemAddress(lc) {
					contactBuf = append(contactBuf, mail.ContactUpsert{
						Address: mail.Address{Name: from.Name, Email: lc},
						Kind:    mail.ContactReceived,
					})
				}
			}
			added++
		}

		// Flush contact buffer accumulated during this batch.
		if len(contactBuf) > 0 {
			if err := e.store.UpsertContacts(accountID, contactBuf, time.Now()); err != nil {
				e.logger.Warn("contact upsert failed", "err", err)
			}
			contactBuf = contactBuf[:0]
		}

		// Emit incremental progress for every non-last batch. The true final
		// FolderDone (Final: true) is emitted at the end of fetchFolder after
		// flag refresh and deletion, so we skip it here to avoid a duplicate.
		if i+fetchBatchSize < len(newUIDs) {
			emit(FolderDone{Account: acct.Name, Folder: folderName, Added: added})
		}
	}

	// Flag refresh for UIDs on both server and store.
	// When the server supports CONDSTORE and we have a stored HIGHESTMODSEQ,
	// pass CHANGEDSINCE so only messages whose flags changed since the last sync
	// are returned — 0 results when nothing changed, avoiding 1000+ round trips
	// on large mailboxes.
	existingOnServer := make([]imap.UID, 0, len(existing))
	for uid := range existing {
		if _, ok := serverSet[imap.UID(uid)]; ok {
			existingOnServer = append(existingOnServer, imap.UID(uid))
		}
	}
	for i := 0; i < len(existingOnServer); i += fetchBatchSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		batch := existingOnServer[i:min(i+fetchBatchSize, len(existingOnServer))]
		uidSet := toUIDSet(batch)
		fetchOpts := &imap.FetchOptions{UID: true, Flags: true}
		if folder.HighestModSeq > 0 {
			fetchOpts.ChangedSince = folder.HighestModSeq
		}
		bufs, err := c.Fetch(uidSet, fetchOpts).Collect()
		if err != nil {
			return fmt.Errorf("flag refresh: %w", err)
		}
		for _, buf := range bufs {
			if err := e.store.UpdateFlags(folderID, uint32(buf.UID), parseFlags(buf.Flags)); err != nil {
				return err
			}
		}
		updated += len(bufs) // count actual responses, not len(batch)
	}

	// Delete UIDs that no longer exist on the server.
	for _, uid := range goneUIDs {
		if err := e.store.DeleteMessageByUID(folderID, uid); err != nil {
			return err
		}
		deleted++
	}

	// Record folder sync state. When the caller had a fresh SELECT result use its
	// HIGHESTMODSEQ; for poke-driven calls (serverHighestModSeq==0) preserve the
	// value already stored so subsequent syncs still get CONDSTORE savings.
	modSeq := serverHighestModSeq
	if modSeq == 0 {
		modSeq = folder.HighestModSeq
	}
	if err := e.store.UpdateFolderSync(folderID, serverUIDValidity, modSeq, time.Now()); err != nil {
		return err
	}

	e.logger.Info("fetchFolder done", "account", acct.Name, "folder", folderName, "added", added, "updated", updated, "deleted", deleted)
	emit(FolderDone{
		Account: acct.Name,
		Folder:  folderName,
		Added:   added, Updated: updated, Deleted: deleted,
		Final: true,
	})
	return nil
}

// reconcileDeletions performs a UID SEARCH ALL diff for folderID and removes
// locally any UIDs that no longer exist on the server. The connection c must
// already have the target folder SELECTed. It emits FolderDone{Final:true}
// with the Deleted count when done.
func (e *Engine) reconcileDeletions(ctx context.Context, c *imapclient.Client, acctName string, folderID int64, folderName string, emit func(Event)) (deleted int, err error) {
	existing, err := e.store.ExistingUIDs(folderID)
	if err != nil {
		return 0, err
	}
	if len(existing) == 0 {
		return 0, nil
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	searchData, err := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		return 0, fmt.Errorf("reconcile uid search %q: %w", folderName, err)
	}
	serverSet := make(map[imap.UID]struct{}, len(searchData.AllUIDs()))
	for _, u := range searchData.AllUIDs() {
		serverSet[u] = struct{}{}
	}

	for uid := range existing {
		select {
		case <-ctx.Done():
			return deleted, ctx.Err()
		default:
		}
		if _, ok := serverSet[imap.UID(uid)]; !ok {
			if err := e.store.DeleteMessageByUID(folderID, uid); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	if deleted > 0 {
		emit(FolderDone{Account: acctName, Folder: folderName, Deleted: deleted, Final: true})
	}
	return deleted, nil
}

// toUIDSet builds an imap.UIDSet from a slice of UIDs.
func toUIDSet(uids []imap.UID) imap.UIDSet {
	var s imap.UIDSet
	for _, u := range uids {
		s.AddNum(u)
	}
	return s
}

// decodeAttachmentReader wraps r with the appropriate Content-Transfer-Encoding
// decoder so that binary attachment data reaches the disk writer already decoded.
// 7bit/8bit/binary and unknown encodings pass through unchanged.
func decodeAttachmentReader(r io.Reader, encoding string) io.Reader {
	switch strings.ToLower(encoding) {
	case "base64":
		// IMAP base64 bodies contain CRLF line folds every 76 chars; strip them
		// before handing to the decoder so it sees a clean base64 stream.
		return base64.NewDecoder(base64.StdEncoding, &crlfStripper{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

// crlfStripper is an io.Reader that removes \r and \n from the stream.
// Used to clean up the line folds in MIME base64 bodies before base64 decoding.
type crlfStripper struct{ r io.Reader }

func (s *crlfStripper) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	j := 0
	for i := 0; i < n; i++ {
		if p[i] != '\r' && p[i] != '\n' {
			p[j] = p[i]
			j++
		}
	}
	return j, err
}

// decodeBody decodes raw IMAP body section bytes by applying transfer-encoding
// and charset conversion. Unknown encodings and charsets pass through rather
// than erroring, so a single bad message never aborts the sync; partial decode
// failures return whatever text was recovered.
func decodeBody(raw []byte, encoding, cs string) string {
	var r io.Reader = bytes.NewReader(raw)

	switch strings.ToLower(encoding) {
	case "quoted-printable":
		r = quotedprintable.NewReader(r)
	case "base64":
		// Base64 IMAP bodies may contain CRLF line folds; strip them before
		// handing to the decoder.
		cleaned := strings.ReplaceAll(string(raw), "\r\n", "")
		cleaned = strings.ReplaceAll(cleaned, "\n", "")
		r = base64.NewDecoder(base64.StdEncoding, strings.NewReader(cleaned))
		// "7bit", "8bit", "binary", "" — pass through as-is
	}

	if cs != "" && cs != "utf-8" && cs != "us-ascii" {
		if cr, err := charset.Reader(cs, r); err == nil {
			r = cr
		}
		// On error (unknown charset), fall through and use the raw bytes.
	}

	decoded, _ := io.ReadAll(r)
	return string(decoded)
}
