package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/5Sigma/pmx/internal/mail"
)

type OutboxItem = mail.OutboxItem

func (s *Store) EnqueueOutbox(item *OutboxItem) error {
	const q = `INSERT INTO outbox (account_id, raw_eml, queued_at, attempts)
               VALUES (?, ?, ?, 0)`
	now := time.Now().Unix()
	res, err := s.db.Exec(q, item.AccountID, item.RawEML, now)
	if err != nil {
		return fmt.Errorf("enqueue outbox: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("outbox last insert id: %w", err)
	}
	item.ID = id
	item.QueuedAt = time.Unix(now, 0)
	return nil
}

func (s *Store) ListPendingOutbox(limit int) ([]*OutboxItem, error) {
	const q = `SELECT id, account_id, raw_eml, queued_at, attempts, COALESCE(last_error,'')
               FROM outbox ORDER BY queued_at ASC LIMIT ?`
	rows, err := s.db.Query(q, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanOutboxRows(rows)
}

func (s *Store) ListPendingOutboxForAccount(accountID int64, limit int) ([]*OutboxItem, error) {
	const q = `SELECT id, account_id, raw_eml, queued_at, attempts, COALESCE(last_error,'')
               FROM outbox WHERE account_id = ? ORDER BY queued_at ASC LIMIT ?`
	rows, err := s.db.Query(q, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending outbox for account: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanOutboxRows(rows)
}

func (s *Store) MarkOutboxSent(id int64) error {
	_, err := s.db.Exec(`DELETE FROM outbox WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("mark outbox sent: %w", err)
	}
	return nil
}

func (s *Store) MarkOutboxFailed(id int64, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE outbox SET attempts = attempts + 1, last_error = ?, locked_by = NULL, locked_at = NULL WHERE id = ?`,
		errMsg, id,
	)
	if err != nil {
		return fmt.Errorf("mark outbox failed: %w", err)
	}
	return nil
}

func (s *Store) ClaimPendingOutbox(accountID int64, claimant string, limit int, now time.Time, staleAfter time.Duration) ([]*OutboxItem, error) {
	staleThreshold := now.Add(-staleAfter).Unix()
	nowUnix := now.Unix()

	// A single UPDATE...WHERE...RETURNING is atomic under SQLite's writer lock:
	// no two concurrent callers can claim the same row.
	rows, err := s.db.Query(
		`UPDATE outbox SET locked_by = ?, locked_at = ?
         WHERE id IN (
             SELECT id FROM outbox
             WHERE account_id = ? AND (locked_by IS NULL OR locked_at < ?)
             ORDER BY queued_at ASC LIMIT ?
         )
         RETURNING id, account_id, raw_eml, queued_at, attempts, COALESCE(last_error,'')`,
		claimant, nowUnix,
		accountID, staleThreshold, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("claim outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanOutboxRows(rows)
}

func (s *Store) GetOutboxItem(id int64) (*mail.OutboxItem, bool, error) {
	const q = `SELECT id, account_id, raw_eml, queued_at, attempts, COALESCE(last_error,'')
               FROM outbox WHERE id = ?`
	var item mail.OutboxItem
	var queuedAt int64
	err := s.db.QueryRow(q, id).Scan(&item.ID, &item.AccountID, &item.RawEML, &queuedAt, &item.Attempts, &item.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get outbox item: %w", err)
	}
	item.QueuedAt = time.Unix(queuedAt, 0)
	return &item, true, nil
}

// scanOutboxRows scans sql.Rows into a slice of OutboxItem pointers.
func scanOutboxRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*OutboxItem, error) {
	var items []*OutboxItem
	for rows.Next() {
		var item OutboxItem
		var queuedAt int64
		if err := rows.Scan(&item.ID, &item.AccountID, &item.RawEML, &queuedAt, &item.Attempts, &item.LastError); err != nil {
			return nil, fmt.Errorf("scan outbox row: %w", err)
		}
		item.QueuedAt = time.Unix(queuedAt, 0)
		items = append(items, &item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox rows: %w", err)
	}
	return items, nil
}
