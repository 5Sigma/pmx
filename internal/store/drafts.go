package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/5Sigma/pmx/internal/mail"
)

// Draft is an alias so store-internal code can reference mail.Draft cleanly.
type Draft = mail.Draft

func (s *Store) SaveDraft(d *Draft) error {
	toJSON, err := json.Marshal(d.To)
	if err != nil {
		return fmt.Errorf("marshal to: %w", err)
	}
	ccJSON, err := json.Marshal(d.CC)
	if err != nil {
		return fmt.Errorf("marshal cc: %w", err)
	}
	bccJSON, err := json.Marshal(d.BCC)
	if err != nil {
		return fmt.Errorf("marshal bcc: %w", err)
	}

	d.UpdatedAt = time.Now()

	if d.ID != 0 {
		_, err = s.db.Exec(`
			INSERT OR REPLACE INTO drafts(id, account_id, subject, to_addrs, cc_addrs, bcc_addrs, body_text, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.AccountID, d.Subject,
			string(toJSON), string(ccJSON), string(bccJSON),
			d.BodyText, d.UpdatedAt.Unix(),
		)
	} else {
		res, err := s.db.Exec(`
			INSERT INTO drafts(account_id, subject, to_addrs, cc_addrs, bcc_addrs, body_text, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			d.AccountID, d.Subject,
			string(toJSON), string(ccJSON), string(bccJSON),
			d.BodyText, d.UpdatedAt.Unix(),
		)
		if err != nil {
			return fmt.Errorf("insert draft: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("draft last insert id: %w", err)
		}
		d.ID = id
		return nil
	}
	if err != nil {
		return fmt.Errorf("save draft %d: %w", d.ID, err)
	}
	return nil
}

func (s *Store) Drafts(accountID int64) ([]*Draft, error) {
	rows, err := s.db.Query(`
		SELECT id, account_id, subject, to_addrs, cc_addrs, bcc_addrs, body_text, updated_at
		FROM drafts
		WHERE account_id = ?
		ORDER BY updated_at DESC`,
		accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("list drafts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var drafts []*Draft
	for rows.Next() {
		var (
			d         Draft
			toJSON    string
			ccJSON    string
			bccJSON   string
			updatedAt int64
		)
		if err := rows.Scan(
			&d.ID, &d.AccountID, &d.Subject,
			&toJSON, &ccJSON, &bccJSON,
			&d.BodyText, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan draft: %w", err)
		}
		if err := json.Unmarshal([]byte(toJSON), &d.To); err != nil {
			return nil, fmt.Errorf("unmarshal draft to: %w", err)
		}
		if err := json.Unmarshal([]byte(ccJSON), &d.CC); err != nil {
			return nil, fmt.Errorf("unmarshal draft cc: %w", err)
		}
		if err := json.Unmarshal([]byte(bccJSON), &d.BCC); err != nil {
			return nil, fmt.Errorf("unmarshal draft bcc: %w", err)
		}
		d.UpdatedAt = time.Unix(updatedAt, 0)
		drafts = append(drafts, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("drafts rows error: %w", err)
	}
	return drafts, nil
}
