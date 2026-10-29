package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/5Sigma/pmx/internal/mail"
)

func (s *Store) GetAccountID(name string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM accounts WHERE name = ?`, name).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get account %q: %w", name, err)
	}
	return id, true, nil
}

func (s *Store) EnsureAccount(name, email string) (int64, error) {
	_, err := s.db.Exec(
		`INSERT INTO accounts(name, email) VALUES (?, ?) ON CONFLICT(name) DO UPDATE SET email = excluded.email`,
		name, email,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert account %q: %w", name, err)
	}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM accounts WHERE name = ?`, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("select account %q: %w", name, err)
	}
	return id, nil
}

func (s *Store) EnsureFolder(accountID int64, name string) (int64, error) {
	_, err := s.db.Exec(
		`INSERT INTO folders(account_id, name) VALUES (?, ?) ON CONFLICT(account_id, name) DO NOTHING`,
		accountID, name,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert folder %q: %w", name, err)
	}
	var id int64
	if err := s.db.QueryRow(
		`SELECT id FROM folders WHERE account_id = ? AND name = ?`, accountID, name,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("select folder %q: %w", name, err)
	}
	return id, nil
}

func (s *Store) UpsertMessage(m *Message) error {
	fromJSON, err := json.Marshal(m.From)
	if err != nil {
		return fmt.Errorf("marshal from: %w", err)
	}
	toJSON, err := json.Marshal(m.To)
	if err != nil {
		return fmt.Errorf("marshal to: %w", err)
	}
	ccJSON, err := json.Marshal(m.CC)
	if err != nil {
		return fmt.Errorf("marshal cc: %w", err)
	}
	bccJSON, err := json.Marshal(m.BCC)
	if err != nil {
		return fmt.Errorf("marshal bcc: %w", err)
	}
	refsStr := strings.Join(m.References, " ")

	hasAttach := 0
	if m.HasAttach {
		hasAttach = 1
	}

	res, err := s.db.Exec(`
		INSERT INTO messages(
			folder_id, uid, message_id, in_reply_to, references_,
			thread_root, subject, from_addr, to_addrs, cc_addrs, bcc_addrs,
			date_sent, date_received, flags, size_bytes,
			body_text, body_html, has_attach, raw_path
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(folder_id, uid) DO UPDATE SET
			message_id    = excluded.message_id,
			in_reply_to   = excluded.in_reply_to,
			references_   = excluded.references_,
			thread_root   = excluded.thread_root,
			subject       = excluded.subject,
			from_addr     = excluded.from_addr,
			to_addrs      = excluded.to_addrs,
			cc_addrs      = excluded.cc_addrs,
			bcc_addrs     = excluded.bcc_addrs,
			date_sent     = excluded.date_sent,
			date_received = excluded.date_received,
			flags         = excluded.flags,
			size_bytes    = excluded.size_bytes,
			body_text     = excluded.body_text,
			body_html     = excluded.body_html,
			has_attach    = excluded.has_attach,
			raw_path      = excluded.raw_path`,
		m.FolderID, m.UID, m.MessageID, m.InReplyTo, refsStr,
		m.ThreadRoot, m.Subject, string(fromJSON), string(toJSON), string(ccJSON), string(bccJSON),
		m.DateSent.Unix(), m.DateReceived.Unix(), m.Flags, m.SizeBytes,
		m.BodyText, m.BodyHTML, hasAttach, m.RawPath,
	)
	if err != nil {
		return fmt.Errorf("upsert message uid=%d: %w", m.UID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("last insert id: %w", err)
	}
	// LastInsertId returns the existing rowid on conflict; back-fill the ID.
	if id == 0 {
		if err := s.db.QueryRow(
			`SELECT id FROM messages WHERE folder_id = ? AND uid = ?`, m.FolderID, m.UID,
		).Scan(&id); err != nil {
			return fmt.Errorf("select message id: %w", err)
		}
	}
	m.ID = id
	return nil
}

func (s *Store) ListFolder(folderID int64, limit, offset int) ([]*Message, error) {
	rows, err := s.db.Query(`
		SELECT id, folder_id, uid, message_id, in_reply_to, references_,
		       thread_root, subject, from_addr, to_addrs, cc_addrs, bcc_addrs,
		       date_sent, date_received, flags, size_bytes,
		       body_text, body_html, has_attach, raw_path
		FROM messages
		WHERE folder_id = ?
		ORDER BY date_sent DESC
		LIMIT ? OFFSET ?`,
		folderID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list folder %d: %w", folderID, err)
	}
	defer func() { _ = rows.Close() }()
	return scanMessages(rows)
}

// CountUnread returns the number of rows in the folder whose flags bitmask
// does not include mail.FlagSeen. Unknown folders yield 0 without an error.
func (s *Store) CountUnread(folderID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE folder_id = ? AND (flags & ?) = 0`,
		folderID, int(mail.FlagSeen),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unread folder %d: %w", folderID, err)
	}
	return n, nil
}

func (s *Store) CountMessages(folderID int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE folder_id = ?`, folderID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count messages folder %d: %w", folderID, err)
	}
	return n, nil
}

func (s *Store) ByUID(folderID int64, uid uint32) (*Message, error) {
	row := s.db.QueryRow(`
		SELECT id, folder_id, uid, message_id, in_reply_to, references_,
		       thread_root, subject, from_addr, to_addrs, cc_addrs, bcc_addrs,
		       date_sent, date_received, flags, size_bytes,
		       body_text, body_html, has_attach, raw_path
		FROM messages
		WHERE folder_id = ? AND uid = ?`,
		folderID, uid,
	)
	msgs, err := scanMessages(singleRow{row})
	if err != nil {
		return nil, fmt.Errorf("by uid %d: %w", uid, err)
	}
	if len(msgs) == 0 {
		return nil, sql.ErrNoRows
	}
	return msgs[0], nil
}

func (s *Store) Body(messageID int64) (string, error) {
	var body sql.NullString
	if err := s.db.QueryRow(
		`SELECT body_text FROM messages WHERE id = ?`, messageID,
	).Scan(&body); err != nil {
		return "", fmt.Errorf("body %d: %w", messageID, err)
	}
	return body.String, nil
}

func (s *Store) BodyHTML(messageID int64) (string, error) {
	var body sql.NullString
	if err := s.db.QueryRow(
		`SELECT body_html FROM messages WHERE id = ?`, messageID,
	).Scan(&body); err != nil {
		return "", fmt.Errorf("body_html %d: %w", messageID, err)
	}
	return body.String, nil
}

func (s *Store) Search(query string, limit int) ([]*Message, error) {
	rows, err := s.db.Query(`
		SELECT m.id, m.folder_id, m.uid, m.message_id, m.in_reply_to, m.references_,
		       m.thread_root, m.subject, m.from_addr, m.to_addrs, m.cc_addrs, m.bcc_addrs,
		       m.date_sent, m.date_received, m.flags, m.size_bytes,
		       m.body_text, m.body_html, m.has_attach, m.raw_path
		FROM messages m
		JOIN messages_fts f ON m.id = f.rowid
		WHERE messages_fts MATCH ?
		ORDER BY rank
		LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	defer func() { _ = rows.Close() }()
	return scanMessages(rows)
}

// folderColumns is the canonical SELECT list for the folders table. All
// scanFolder* helpers assume this column order; keep SQL and scanners in sync.
const folderColumns = `id, account_id, name, role, uid_validity, highest_modseq,
	       uid_next, num_messages, hidden_since, mailbox_id, last_sync_at`

// folderScanner matches both *sql.Row and *sql.Rows.
type folderScanner interface {
	Scan(dest ...any) error
}

// scanFolder populates f from a single-row scanner using folderColumns order.
func scanFolder(sc folderScanner, f *mail.Folder) error {
	var lastSyncAt, hiddenSince sql.NullInt64
	if err := sc.Scan(&f.ID, &f.AccountID, &f.Name, &f.Role, &f.UIDValidity, &f.HighestModSeq,
		&f.UIDNext, &f.NumMessages, &hiddenSince, &f.MailboxID, &lastSyncAt); err != nil {
		return err
	}
	if lastSyncAt.Valid {
		f.LastSyncAt = time.Unix(lastSyncAt.Int64, 0)
	}
	if hiddenSince.Valid {
		f.HiddenSince = hiddenSince.Int64
	}
	return nil
}

func (s *Store) GetFolder(folderID int64) (*mail.Folder, error) {
	var f mail.Folder
	row := s.db.QueryRow(`SELECT `+folderColumns+` FROM folders WHERE id = ?`, folderID)
	if err := scanFolder(row, &f); err != nil {
		return nil, fmt.Errorf("get folder %d: %w", folderID, err)
	}
	return &f, nil
}

func (s *Store) FolderByName(accountID int64, name string) (*mail.Folder, error) {
	var f mail.Folder
	row := s.db.QueryRow(
		`SELECT `+folderColumns+` FROM folders WHERE account_id = ? AND name = ?`,
		accountID, name,
	)
	if err := scanFolder(row, &f); err != nil {
		return nil, fmt.Errorf("folder by name %q: %w", name, err)
	}
	return &f, nil
}

func (s *Store) ListFolders(accountID int64) ([]*mail.Folder, error) {
	rows, err := s.db.Query(`
		SELECT `+folderColumns+`
		FROM folders
		WHERE account_id = ? AND hidden_since IS NULL
		ORDER BY
			CASE role
				WHEN 'inbox'   THEN 0
				WHEN 'sent'    THEN 1
				WHEN 'drafts'  THEN 2
				WHEN 'archive' THEN 3
				WHEN 'trash'   THEN 4
				WHEN 'junk'    THEN 5
				ELSE                6
			END,
			name ASC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("list folders account=%d: %w", accountID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []*mail.Folder
	for rows.Next() {
		var f mail.Folder
		if err := scanFolder(rows, &f); err != nil {
			return nil, fmt.Errorf("scan folder: %w", err)
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (s *Store) UpsertFolderRole(folderID int64, role mail.FolderRole) error {
	_, err := s.db.Exec(
		`UPDATE folders SET role = ? WHERE id = ?`, role, folderID,
	)
	if err != nil {
		return fmt.Errorf("upsert folder role folder=%d: %w", folderID, err)
	}
	return nil
}

func (s *Store) ExistingUIDs(folderID int64) (map[uint32]struct{}, error) {
	rows, err := s.db.Query(`SELECT uid FROM messages WHERE folder_id = ?`, folderID)
	if err != nil {
		return nil, fmt.Errorf("existing uids folder %d: %w", folderID, err)
	}
	defer func() { _ = rows.Close() }()
	m := make(map[uint32]struct{})
	for rows.Next() {
		var uid uint32
		if err := rows.Scan(&uid); err != nil {
			return nil, fmt.Errorf("scan uid: %w", err)
		}
		m[uid] = struct{}{}
	}
	return m, rows.Err()
}

func (s *Store) ClearFolderMessages(folderID int64) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE folder_id = ?`, folderID)
	if err != nil {
		return fmt.Errorf("clear folder %d: %w", folderID, err)
	}
	return nil
}

func (s *Store) UpdateFlags(folderID int64, uid uint32, flags mail.Flag) error {
	_, err := s.db.Exec(
		`UPDATE messages SET flags = ? WHERE folder_id = ? AND uid = ?`,
		flags, folderID, uid,
	)
	if err != nil {
		return fmt.Errorf("update flags folder=%d uid=%d: %w", folderID, uid, err)
	}
	return nil
}

func (s *Store) DeleteMessageByUID(folderID int64, uid uint32) error {
	_, err := s.db.Exec(
		`DELETE FROM messages WHERE folder_id = ? AND uid = ?`,
		folderID, uid,
	)
	if err != nil {
		return fmt.Errorf("delete message folder=%d uid=%d: %w", folderID, uid, err)
	}
	return nil
}

func (s *Store) UpdateFolderSync(folderID int64, uidValidity uint32, highestModSeq uint64, at time.Time) error {
	_, err := s.db.Exec(
		`UPDATE folders SET uid_validity = ?, highest_modseq = ?, last_sync_at = ? WHERE id = ?`,
		uidValidity, highestModSeq, at.Unix(), folderID,
	)
	if err != nil {
		return fmt.Errorf("update folder sync %d: %w", folderID, err)
	}
	return nil
}

func (s *Store) InsertAttachment(a *mail.Attachment) error {
	res, err := s.db.Exec(
		`INSERT INTO attachments(message_id, filename, mime_type, size_bytes, sha256, content_id, disk_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.MessageID, a.Filename, a.MIMEType, a.SizeBytes, a.SHA256, a.ContentID, a.DiskPath,
	)
	if err != nil {
		return fmt.Errorf("insert attachment msg=%d: %w", a.MessageID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("last insert id attachment: %w", err)
	}
	a.ID = id
	return nil
}

func (s *Store) ListAttachments(messageID int64) ([]mail.Attachment, error) {
	rows, err := s.db.Query(
		`SELECT id, message_id, filename, mime_type, size_bytes, sha256, content_id, disk_path
		 FROM attachments WHERE message_id = ?`, messageID,
	)
	if err != nil {
		return nil, fmt.Errorf("list attachments msg=%d: %w", messageID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []mail.Attachment
	for rows.Next() {
		var a mail.Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.MIMEType, &a.SizeBytes, &a.SHA256, &a.ContentID, &a.DiskPath); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteFolder(folderID int64) error {
	_, err := s.db.Exec(`DELETE FROM folders WHERE id = ?`, folderID)
	if err != nil {
		return fmt.Errorf("delete folder %d: %w", folderID, err)
	}
	return nil
}

func (s *Store) RenameFolder(folderID int64, newName string) error {
	_, err := s.db.Exec(`UPDATE folders SET name = ? WHERE id = ?`, newName, folderID)
	if err != nil {
		return fmt.Errorf("rename folder %d to %q: %w", folderID, newName, err)
	}
	return nil
}

func (s *Store) UpdateFolderStatus(folderID int64, uidValidity uint32, uidNext uint32, numMessages uint32, highestModSeq uint64, at time.Time) error {
	_, err := s.db.Exec(
		`UPDATE folders SET uid_validity = ?, uid_next = ?, num_messages = ?,
		 highest_modseq = ?, last_sync_at = ? WHERE id = ?`,
		uidValidity, uidNext, numMessages, highestModSeq, at.Unix(), folderID,
	)
	if err != nil {
		return fmt.Errorf("update folder status %d: %w", folderID, err)
	}
	return nil
}

func (s *Store) FolderByUIDValidity(accountID int64, uidValidity uint32) (*mail.Folder, error) {
	var f mail.Folder
	row := s.db.QueryRow(
		`SELECT `+folderColumns+` FROM folders WHERE account_id = ? AND uid_validity = ? LIMIT 1`,
		accountID, uidValidity,
	)
	if err := scanFolder(row, &f); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("folder by uid_validity account=%d uid_validity=%d: %w", accountID, uidValidity, err)
	}
	return &f, nil
}

func (s *Store) MarkFolderHidden(folderID int64, at time.Time) error {
	_, err := s.db.Exec(`UPDATE folders SET hidden_since = ? WHERE id = ?`, at.Unix(), folderID)
	if err != nil {
		return fmt.Errorf("mark folder hidden %d: %w", folderID, err)
	}
	return nil
}

func (s *Store) UnmarkFolderHidden(folderID int64) error {
	_, err := s.db.Exec(`UPDATE folders SET hidden_since = NULL WHERE id = ?`, folderID)
	if err != nil {
		return fmt.Errorf("unmark folder hidden %d: %w", folderID, err)
	}
	return nil
}

func (s *Store) ListHiddenFolders(accountID int64) ([]*mail.Folder, error) {
	rows, err := s.db.Query(
		`SELECT `+folderColumns+` FROM folders WHERE account_id = ? AND hidden_since IS NOT NULL`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("list hidden folders account=%d: %w", accountID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []*mail.Folder
	for rows.Next() {
		var f mail.Folder
		if err := scanFolder(rows, &f); err != nil {
			return nil, fmt.Errorf("scan hidden folder: %w", err)
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (s *Store) FolderByRole(accountID int64, role mail.FolderRole) (*mail.Folder, error) {
	var f mail.Folder
	row := s.db.QueryRow(
		`SELECT `+folderColumns+` FROM folders WHERE account_id = ? AND role = ? LIMIT 1`,
		accountID, role,
	)
	if err := scanFolder(row, &f); err != nil {
		return nil, fmt.Errorf("folder by role %q: %w", role, err)
	}
	return &f, nil
}

func (s *Store) NewMessagesSince(folderID int64, afterUID uint32, limit int) ([]*mail.Message, error) {
	rows, err := s.db.Query(`
		SELECT id, folder_id, uid, message_id, in_reply_to, references_,
		       thread_root, subject, from_addr, to_addrs, cc_addrs, bcc_addrs,
		       date_sent, date_received, flags, size_bytes,
		       body_text, body_html, has_attach, raw_path
		FROM messages
		WHERE folder_id = ? AND uid > ? AND (flags & ?) = 0
		ORDER BY uid ASC
		LIMIT ?`,
		folderID, afterUID, int(mail.FlagSeen), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("new messages since uid=%d: %w", afterUID, err)
	}
	defer func() { _ = rows.Close() }()
	return scanMessages(rows)
}

// ---- helpers ----------------------------------------------------------------

// Message is an alias so store-internal code can refer to mail.Message cleanly.
type Message = mail.Message

// rowScanner abstracts *sql.Rows and the single-row shim so scanMessages works
// for both ListFolder and ByUID.
type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// singleRow wraps *sql.Row to satisfy rowScanner.
type singleRow struct{ r *sql.Row }

func (s singleRow) Next() bool             { return true }
func (s singleRow) Scan(dest ...any) error { return s.r.Scan(dest...) }
func (s singleRow) Err() error             { return nil }

func scanMessages(rows rowScanner) ([]*Message, error) {
	var msgs []*Message
	for rows.Next() {
		var (
			m            Message
			fromJSON     string
			toJSON       string
			ccJSON       string
			bccJSON      string
			refsStr      string
			dateSentUnix sql.NullInt64
			dateRecvUnix int64
			hasAttach    int
		)
		if err := rows.Scan(
			&m.ID, &m.FolderID, &m.UID, &m.MessageID, &m.InReplyTo, &refsStr,
			&m.ThreadRoot, &m.Subject, &fromJSON, &toJSON, &ccJSON, &bccJSON,
			&dateSentUnix, &dateRecvUnix, &m.Flags, &m.SizeBytes,
			&m.BodyText, &m.BodyHTML, &hasAttach, &m.RawPath,
		); err != nil {
			if err == sql.ErrNoRows {
				break
			}
			return nil, fmt.Errorf("scan message: %w", err)
		}

		if err := json.Unmarshal([]byte(fromJSON), &m.From); err != nil {
			return nil, fmt.Errorf("unmarshal from: %w", err)
		}
		if err := json.Unmarshal([]byte(toJSON), &m.To); err != nil {
			return nil, fmt.Errorf("unmarshal to: %w", err)
		}
		if err := json.Unmarshal([]byte(ccJSON), &m.CC); err != nil {
			return nil, fmt.Errorf("unmarshal cc: %w", err)
		}
		if err := json.Unmarshal([]byte(bccJSON), &m.BCC); err != nil {
			return nil, fmt.Errorf("unmarshal bcc: %w", err)
		}

		if refsStr != "" {
			m.References = strings.Split(refsStr, " ")
		}
		if dateSentUnix.Valid {
			m.DateSent = time.Unix(dateSentUnix.Int64, 0)
		}
		m.DateReceived = time.Unix(dateRecvUnix, 0)
		m.HasAttach = hasAttach != 0

		msgs = append(msgs, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return msgs, nil
}
