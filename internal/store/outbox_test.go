package store

import (
	"testing"

	"github.com/5Sigma/pmx/internal/mail"
)

// ---- EnqueueOutbox ----------------------------------------------------------

func TestEnqueueOutbox(t *testing.T) {
	t.Run("back-fills ID and QueuedAt", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item := &mail.OutboxItem{
			AccountID: acctID,
			RawEML:    []byte("From: sender@example.com\r\nTo: recv@example.com\r\n\r\nHello"),
		}

		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		if item.ID <= 0 {
			t.Errorf("want item.ID > 0 after enqueue, got %d", item.ID)
		}
		if item.QueuedAt.IsZero() {
			t.Error("want item.QueuedAt to be non-zero after enqueue")
		}
	})

	t.Run("row is persisted in the outbox table", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item := &mail.OutboxItem{
			AccountID: acctID,
			RawEML:    []byte("From: sender@example.com\r\nTo: recv@example.com\r\n\r\nHello"),
		}

		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&count); err != nil {
			t.Fatalf("count outbox rows: %v", err)
		}
		if count != 1 {
			t.Errorf("want 1 outbox row, got %d", count)
		}
	})

	t.Run("stored raw_eml round-trips correctly", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		wantEML := []byte("From: sender@example.com\r\nTo: recv@example.com\r\n\r\nBody text here")
		item := &mail.OutboxItem{
			AccountID: acctID,
			RawEML:    wantEML,
		}

		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		var gotEML []byte
		if err := s.db.QueryRow(`SELECT raw_eml FROM outbox WHERE id = ?`, item.ID).Scan(&gotEML); err != nil {
			t.Fatalf("SELECT raw_eml: %v", err)
		}
		if string(gotEML) != string(wantEML) {
			t.Errorf("raw_eml: want %q, got %q", wantEML, gotEML)
		}
	})

	t.Run("attempts column initialised to zero", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item := &mail.OutboxItem{
			AccountID: acctID,
			RawEML:    []byte("stub eml"),
		}
		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		var attempts int
		if err := s.db.QueryRow(`SELECT attempts FROM outbox WHERE id = ?`, item.ID).Scan(&attempts); err != nil {
			t.Fatalf("SELECT attempts: %v", err)
		}
		if attempts != 0 {
			t.Errorf("want attempts = 0 on fresh enqueue, got %d", attempts)
		}
	})

	t.Run("multiple items each get distinct IDs", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item1 := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml1")}
		item2 := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml2")}

		if err := s.EnqueueOutbox(item1); err != nil {
			t.Fatalf("EnqueueOutbox item1: %v", err)
		}
		if err := s.EnqueueOutbox(item2); err != nil {
			t.Fatalf("EnqueueOutbox item2: %v", err)
		}

		if item1.ID == item2.ID {
			t.Errorf("two enqueued items share the same ID %d", item1.ID)
		}

		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&count); err != nil {
			t.Fatalf("count outbox rows: %v", err)
		}
		if count != 2 {
			t.Errorf("want 2 outbox rows, got %d", count)
		}
	})
}

// ---- ListPendingOutbox ------------------------------------------------------

func TestListPendingOutbox(t *testing.T) {
	t.Run("empty table returns empty slice", func(t *testing.T) {
		s := openTestStore(t)

		items, err := s.ListPendingOutbox(100)
		if err != nil {
			t.Fatalf("ListPendingOutbox: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("want empty slice, got %d items", len(items))
		}
	})

	t.Run("rows returned in queued_at ASC order", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		// Enqueue three items and manually backdate their queued_at so order is deterministic.
		ids := make([]int64, 3)
		for i := range ids {
			item := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml")}
			if err := s.EnqueueOutbox(item); err != nil {
				t.Fatalf("EnqueueOutbox %d: %v", i, err)
			}
			ids[i] = item.ID
			// Offset: item 0 is oldest (queued_at - 200), item 2 is newest (queued_at).
			offset := int64((2 - i) * 100)
			if _, err := s.db.Exec(`UPDATE outbox SET queued_at = queued_at - ? WHERE id = ?`, offset, item.ID); err != nil {
				t.Fatalf("backdate item %d: %v", i, err)
			}
		}

		items, err := s.ListPendingOutbox(100)
		if err != nil {
			t.Fatalf("ListPendingOutbox: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("want 3 items, got %d", len(items))
		}
		// Oldest is ids[0], newest is ids[2]; ascending order = ids[0], ids[1], ids[2].
		for i, item := range items {
			if item.ID != ids[i] {
				t.Errorf("position %d: want ID %d, got %d", i, ids[i], item.ID)
			}
		}
		// Verify timestamps are non-decreasing.
		for i := 1; i < len(items); i++ {
			if items[i].QueuedAt.Before(items[i-1].QueuedAt) {
				t.Errorf("items not in ASC order at position %d: %v before %v",
					i, items[i].QueuedAt, items[i-1].QueuedAt)
			}
		}
	})

	t.Run("limit is respected", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		for i := 0; i < 5; i++ {
			item := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml")}
			if err := s.EnqueueOutbox(item); err != nil {
				t.Fatalf("EnqueueOutbox %d: %v", i, err)
			}
		}

		items, err := s.ListPendingOutbox(3)
		if err != nil {
			t.Fatalf("ListPendingOutbox(limit=3): %v", err)
		}
		if len(items) != 3 {
			t.Errorf("want 3 items with limit=3, got %d", len(items))
		}
	})
}

// ---- ListPendingOutboxForAccount --------------------------------------------

func TestListPendingOutboxForAccount(t *testing.T) {
	t.Run("returns only rows for the specified account", func(t *testing.T) {
		s := openTestStore(t)
		acct1 := seedAccount(t, s, "alice", "alice@example.com")
		acct2 := seedAccount(t, s, "bob", "bob@example.com")

		for i := 0; i < 2; i++ {
			if err := s.EnqueueOutbox(&mail.OutboxItem{AccountID: acct1, RawEML: []byte("alice-eml")}); err != nil {
				t.Fatalf("EnqueueOutbox alice %d: %v", i, err)
			}
		}
		if err := s.EnqueueOutbox(&mail.OutboxItem{AccountID: acct2, RawEML: []byte("bob-eml")}); err != nil {
			t.Fatalf("EnqueueOutbox bob: %v", err)
		}

		items, err := s.ListPendingOutboxForAccount(acct1, 100)
		if err != nil {
			t.Fatalf("ListPendingOutboxForAccount(alice): %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("want 2 items for alice, got %d", len(items))
		}
		for _, item := range items {
			if item.AccountID != acct1 {
				t.Errorf("got item with account_id %d, want %d", item.AccountID, acct1)
			}
		}
	})

	t.Run("returns empty slice for account with no rows", func(t *testing.T) {
		s := openTestStore(t)
		acct1 := seedAccount(t, s, "alice", "alice@example.com")
		acct2 := seedAccount(t, s, "bob", "bob@example.com")

		if err := s.EnqueueOutbox(&mail.OutboxItem{AccountID: acct1, RawEML: []byte("eml")}); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		items, err := s.ListPendingOutboxForAccount(acct2, 100)
		if err != nil {
			t.Fatalf("ListPendingOutboxForAccount(bob): %v", err)
		}
		if len(items) != 0 {
			t.Errorf("want 0 items for bob, got %d", len(items))
		}
	})
}

// ---- MarkOutboxSent ---------------------------------------------------------

func TestMarkOutboxSent(t *testing.T) {
	t.Run("row is deleted", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml")}
		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		if err := s.MarkOutboxSent(item.ID); err != nil {
			t.Fatalf("MarkOutboxSent: %v", err)
		}

		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM outbox WHERE id = ?`, item.ID).Scan(&count); err != nil {
			t.Fatalf("count outbox: %v", err)
		}
		if count != 0 {
			t.Errorf("want row deleted after MarkOutboxSent, got count=%d", count)
		}
	})

	t.Run("non-existent ID returns nil (idempotent)", func(t *testing.T) {
		s := openTestStore(t)

		if err := s.MarkOutboxSent(999999); err != nil {
			t.Errorf("want nil for non-existent ID, got %v", err)
		}
	})
}

// ---- MarkOutboxFailed -------------------------------------------------------

func TestMarkOutboxFailed(t *testing.T) {
	t.Run("increments attempts by 1", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml")}
		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		if err := s.MarkOutboxFailed(item.ID, "connection refused"); err != nil {
			t.Fatalf("MarkOutboxFailed: %v", err)
		}

		var attempts int
		if err := s.db.QueryRow(`SELECT attempts FROM outbox WHERE id = ?`, item.ID).Scan(&attempts); err != nil {
			t.Fatalf("SELECT attempts: %v", err)
		}
		if attempts != 1 {
			t.Errorf("want attempts=1 after one MarkOutboxFailed, got %d", attempts)
		}
	})

	t.Run("sets last_error to the given string", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml")}
		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		wantErr := "dial tcp: connection refused"
		if err := s.MarkOutboxFailed(item.ID, wantErr); err != nil {
			t.Fatalf("MarkOutboxFailed: %v", err)
		}

		var gotErr string
		if err := s.db.QueryRow(`SELECT COALESCE(last_error,'') FROM outbox WHERE id = ?`, item.ID).Scan(&gotErr); err != nil {
			t.Fatalf("SELECT last_error: %v", err)
		}
		if gotErr != wantErr {
			t.Errorf("last_error: want %q, got %q", wantErr, gotErr)
		}
	})

	t.Run("two calls accumulate attempts", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml")}
		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		for i := 0; i < 2; i++ {
			if err := s.MarkOutboxFailed(item.ID, "err"); err != nil {
				t.Fatalf("MarkOutboxFailed call %d: %v", i+1, err)
			}
		}

		var attempts int
		if err := s.db.QueryRow(`SELECT attempts FROM outbox WHERE id = ?`, item.ID).Scan(&attempts); err != nil {
			t.Fatalf("SELECT attempts: %v", err)
		}
		if attempts != 2 {
			t.Errorf("want attempts=2 after two calls, got %d", attempts)
		}
	})

	t.Run("row already at attempts=2 goes to 3 after a third call", func(t *testing.T) {
		s := openTestStore(t)
		acctID := seedAccount(t, s, "sender", "sender@example.com")

		item := &mail.OutboxItem{AccountID: acctID, RawEML: []byte("eml")}
		if err := s.EnqueueOutbox(item); err != nil {
			t.Fatalf("EnqueueOutbox: %v", err)
		}

		// Set attempts to 2 directly.
		if _, err := s.db.Exec(`UPDATE outbox SET attempts = 2 WHERE id = ?`, item.ID); err != nil {
			t.Fatalf("UPDATE attempts: %v", err)
		}

		if err := s.MarkOutboxFailed(item.ID, "retry"); err != nil {
			t.Fatalf("MarkOutboxFailed: %v", err)
		}

		var attempts int
		if err := s.db.QueryRow(`SELECT attempts FROM outbox WHERE id = ?`, item.ID).Scan(&attempts); err != nil {
			t.Fatalf("SELECT attempts: %v", err)
		}
		if attempts != 3 {
			t.Errorf("want attempts=3 after increment from 2, got %d", attempts)
		}
	})
}
