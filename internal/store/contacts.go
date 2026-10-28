package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/5Sigma/pmx/internal/mail"
)

// Contact and ContactUpsert are aliases so store-internal code can reference
// the mail package types cleanly.
type Contact = mail.Contact
type ContactUpsert = mail.ContactUpsert

// UpsertContact records a single address interaction for accountID.
func (s *Store) UpsertContact(accountID int64, addr mail.Address, kind mail.ContactKind, now time.Time) error {
	items := []mail.ContactUpsert{{Address: addr, Kind: kind}}
	return s.UpsertContacts(accountID, items, now)
}

// UpsertContacts records a batch of address interactions in a single
// transaction. If any insert fails the entire transaction is rolled back.
func (s *Store) UpsertContacts(accountID int64, items []mail.ContactUpsert, now time.Time) error {
	if len(items) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("contacts: begin tx: %w", err)
	}

	const upsertSQL = `
INSERT INTO contacts(account_id, email, name, name_lc, sent_count, received_count, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id, email) DO UPDATE SET
    name           = CASE WHEN excluded.name <> '' AND contacts.name = ''
                          THEN excluded.name ELSE contacts.name END,
    name_lc        = CASE WHEN excluded.name <> '' AND contacts.name = ''
                          THEN excluded.name_lc ELSE contacts.name_lc END,
    sent_count     = contacts.sent_count     + excluded.sent_count,
    received_count = contacts.received_count + excluded.received_count,
    last_seen      = MAX(contacts.last_seen, excluded.last_seen)`

	stmt, err := tx.Prepare(upsertSQL)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("contacts: prepare upsert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	ts := now.Unix()
	for _, item := range items {
		var sentInc, recvInc int64
		if item.Kind == mail.ContactSent {
			sentInc = 1
		} else {
			recvInc = 1
		}
		nameLc := strings.ToLower(item.Address.Name)
		if _, err := stmt.Exec(
			accountID,
			item.Address.Email,
			item.Address.Name,
			nameLc,
			sentInc,
			recvInc,
			ts,
			ts,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("contacts: upsert %q: %w", item.Address.Email, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("contacts: commit: %w", err)
	}
	return nil
}

// SearchContacts returns up to limit contacts for accountID whose email or
// name_lc begins with prefix (case-insensitive). When prefix is empty the
// top contacts by composite score are returned. Always returns a non-nil slice.
func (s *Store) SearchContacts(accountID int64, prefix string, limit int) ([]mail.Contact, error) {
	var (
		rows interface {
			Next() bool
			Scan(...any) error
			Close() error
			Err() error
		}
		err error
	)

	const selectCols = `SELECT id, account_id, email, name, sent_count, received_count, first_seen, last_seen FROM contacts`
	const orderClause = `ORDER BY (sent_count * 3 + received_count) DESC, last_seen DESC LIMIT ?`

	if prefix == "" {
		rows, err = s.db.Query(
			selectCols+` WHERE account_id = ? `+orderClause,
			accountID, limit,
		)
	} else {
		lc := strings.ToLower(prefix) + "%"
		rows, err = s.db.Query(
			selectCols+` WHERE account_id = ? AND (email LIKE ? OR name_lc LIKE ?) `+orderClause,
			accountID, lc, lc, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("contacts: search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]mail.Contact, 0)
	for rows.Next() {
		var c mail.Contact
		var firstSeen, lastSeen int64
		if err := rows.Scan(
			&c.ID, &c.AccountID, &c.Email, &c.Name,
			&c.SentCount, &c.ReceivedCount,
			&firstSeen, &lastSeen,
		); err != nil {
			return nil, fmt.Errorf("contacts: scan: %w", err)
		}
		c.FirstSeen = time.Unix(firstSeen, 0)
		c.LastSeen = time.Unix(lastSeen, 0)
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("contacts: rows error: %w", err)
	}
	return result, nil
}
