package store

import (
	"database/sql"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/mail"
)

// openTestStore creates an isolated Store backed by a temp directory.
// The store is automatically closed when the test ends.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "attachments"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedAccount creates an account and fails the test on error.
func seedAccount(t *testing.T, s *Store, name, email string) int64 {
	t.Helper()
	id, err := s.EnsureAccount(name, email)
	if err != nil {
		t.Fatalf("EnsureAccount(%q): %v", name, err)
	}
	return id
}

// seedFolder creates a folder under accountID and fails the test on error.
func seedFolder(t *testing.T, s *Store, accountID int64, name string) int64 {
	t.Helper()
	id, err := s.EnsureFolder(accountID, name)
	if err != nil {
		t.Fatalf("EnsureFolder(%q): %v", name, err)
	}
	return id
}

// newMessage builds a minimal valid Message ready for UpsertMessage.
func newMessage(folderID int64, uid uint32, subject string, received time.Time) *Message {
	return &Message{
		FolderID:     folderID,
		UID:          uid,
		Subject:      subject,
		From:         mail.Address{Name: "Sender", Email: "sender@example.com"},
		To:           []mail.Address{{Name: "Recip", Email: "recip@example.com"}},
		CC:           []mail.Address{},
		BCC:          []mail.Address{},
		DateReceived: received,
	}
}

// ---- 1. Migration idempotency -----------------------------------------------

func TestMigrationIdempotency(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "idem.db")
	attachsDir := filepath.Join(dir, "att")

	s1, err := Open(dbPath, attachsDir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	_ = s1.Close()

	s2, err := Open(dbPath, attachsDir)
	if err != nil {
		t.Fatalf("second Open should not error: %v", err)
	}
	defer func() { _ = s2.Close() }()

	var count int
	if err := s2.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != len(migrations) {
		t.Errorf("want %d migration rows, got %d", len(migrations), count)
	}
}

// ---- 2. EnsureAccount -------------------------------------------------------

func TestEnsureAccount(t *testing.T) {
	t.Run("returns positive ID on creation", func(t *testing.T) {
		s := openTestStore(t)
		id, err := s.EnsureAccount("work", "work@example.com")
		if err != nil {
			t.Fatalf("EnsureAccount: %v", err)
		}
		if id <= 0 {
			t.Errorf("want id > 0, got %d", id)
		}
	})

	t.Run("idempotent — same name returns same ID", func(t *testing.T) {
		s := openTestStore(t)
		id1, err := s.EnsureAccount("personal", "me@example.com")
		if err != nil {
			t.Fatalf("first EnsureAccount: %v", err)
		}
		id2, err := s.EnsureAccount("personal", "me@example.com")
		if err != nil {
			t.Fatalf("second EnsureAccount: %v", err)
		}
		if id1 != id2 {
			t.Errorf("want same ID on repeat call: first=%d second=%d", id1, id2)
		}
	})

	t.Run("distinct names get distinct IDs", func(t *testing.T) {
		s := openTestStore(t)
		id1 := seedAccount(t, s, "alpha", "a@example.com")
		id2 := seedAccount(t, s, "beta", "b@example.com")
		if id1 == id2 {
			t.Errorf("distinct accounts should not share ID, both got %d", id1)
		}
	})
}

// ---- 3. EnsureFolder --------------------------------------------------------

func TestEnsureFolder(t *testing.T) {
	t.Run("returns positive ID on creation", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		id, err := s.EnsureFolder(acct, "INBOX")
		if err != nil {
			t.Fatalf("EnsureFolder: %v", err)
		}
		if id <= 0 {
			t.Errorf("want id > 0, got %d", id)
		}
	})

	t.Run("idempotent — same account+name returns same ID", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		id1, err := s.EnsureFolder(acct, "Sent")
		if err != nil {
			t.Fatalf("first EnsureFolder: %v", err)
		}
		id2, err := s.EnsureFolder(acct, "Sent")
		if err != nil {
			t.Fatalf("second EnsureFolder: %v", err)
		}
		if id1 != id2 {
			t.Errorf("want same ID on repeat call: first=%d second=%d", id1, id2)
		}
	})

	t.Run("same folder name under different accounts gets different IDs", func(t *testing.T) {
		s := openTestStore(t)
		a1 := seedAccount(t, s, "acct1", "a1@example.com")
		a2 := seedAccount(t, s, "acct2", "a2@example.com")
		fid1, err := s.EnsureFolder(a1, "INBOX")
		if err != nil {
			t.Fatalf("EnsureFolder acct1: %v", err)
		}
		fid2, err := s.EnsureFolder(a2, "INBOX")
		if err != nil {
			t.Fatalf("EnsureFolder acct2: %v", err)
		}
		if fid1 == fid2 {
			t.Errorf("INBOX under different accounts should not share folder ID, both got %d", fid1)
		}
	})
}

// ---- 4. UpsertMessage + ListFolder ordering ---------------------------------

func TestUpsertMessageAndListFolderOrdering(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	now := time.Now().Truncate(time.Second)
	subjects := []string{"oldest", "4h ago", "3h ago", "2h ago", "newest"}
	for i, subj := range subjects {
		// i=0 is oldest (4h ago), i=4 is newest (0h ago)
		received := now.Add(time.Duration(i-4) * time.Hour)
		m := newMessage(folder, uint32(i+1), subj, received)
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage(%q): %v", subj, err)
		}
		if m.ID <= 0 {
			t.Errorf("UpsertMessage should set m.ID > 0, got %d for %q", m.ID, subj)
		}
	}

	t.Run("ListFolder returns exactly 5 messages", func(t *testing.T) {
		msgs, err := s.ListFolder(folder, 10, 0)
		if err != nil {
			t.Fatalf("ListFolder: %v", err)
		}
		if len(msgs) != 5 {
			t.Fatalf("want 5 messages, got %d", len(msgs))
		}
	})

	t.Run("ListFolder orders newest first", func(t *testing.T) {
		msgs, err := s.ListFolder(folder, 10, 0)
		if err != nil {
			t.Fatalf("ListFolder: %v", err)
		}
		for i := 1; i < len(msgs); i++ {
			if msgs[i].DateReceived.After(msgs[i-1].DateReceived) {
				t.Errorf(
					"messages not newest-first at index %d: %s (%v) comes after %s (%v)",
					i,
					msgs[i].Subject, msgs[i].DateReceived,
					msgs[i-1].Subject, msgs[i-1].DateReceived,
				)
			}
		}
		if msgs[0].Subject != "newest" {
			t.Errorf("first message should be %q, got %q", "newest", msgs[0].Subject)
		}
	})

	t.Run("upsert on duplicate UID updates without adding a row", func(t *testing.T) {
		dup := newMessage(folder, 1, "updated subject", now.Add(-4*time.Hour))
		if err := s.UpsertMessage(dup); err != nil {
			t.Fatalf("UpsertMessage duplicate UID: %v", err)
		}

		msgs, err := s.ListFolder(folder, 20, 0)
		if err != nil {
			t.Fatalf("ListFolder after upsert: %v", err)
		}
		if len(msgs) != 5 {
			t.Errorf("want 5 rows after upsert, got %d", len(msgs))
		}

		// Verify the subject was updated.
		got, err := s.ByUID(folder, 1)
		if err != nil {
			t.Fatalf("ByUID after upsert: %v", err)
		}
		if got.Subject != "updated subject" {
			t.Errorf("want subject %q after upsert, got %q", "updated subject", got.Subject)
		}
	})

	t.Run("ListFolder respects limit", func(t *testing.T) {
		msgs, err := s.ListFolder(folder, 3, 0)
		if err != nil {
			t.Fatalf("ListFolder(limit=3): %v", err)
		}
		if len(msgs) != 3 {
			t.Errorf("want 3 messages with limit=3, got %d", len(msgs))
		}
	})

	t.Run("ListFolder respects offset", func(t *testing.T) {
		all, _ := s.ListFolder(folder, 10, 0)
		paged, err := s.ListFolder(folder, 10, 2)
		if err != nil {
			t.Fatalf("ListFolder(offset=2): %v", err)
		}
		if len(paged) != 3 {
			t.Errorf("want 3 messages with offset=2 from 5, got %d", len(paged))
		}
		if paged[0].ID != all[2].ID {
			t.Errorf("offset page should start at all[2]: want ID %d, got %d", all[2].ID, paged[0].ID)
		}
	})
}

// ---- 5. ByUID ---------------------------------------------------------------

func TestByUID(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	want := &Message{
		FolderID:     folder,
		UID:          42,
		Subject:      "Round-trip subject",
		From:         mail.Address{Name: "Alice", Email: "alice@example.com"},
		To:           []mail.Address{{Name: "Bob", Email: "bob@example.com"}},
		CC:           []mail.Address{},
		BCC:          []mail.Address{},
		DateReceived: time.Now().Truncate(time.Second),
	}
	if err := s.UpsertMessage(want); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	got, err := s.ByUID(folder, 42)
	if err != nil {
		t.Fatalf("ByUID(42): %v", err)
	}

	if got.Subject != want.Subject {
		t.Errorf("Subject: want %q, got %q", want.Subject, got.Subject)
	}
	if got.From.Email != want.From.Email {
		t.Errorf("From.Email: want %q, got %q", want.From.Email, got.From.Email)
	}
	if got.From.Name != want.From.Name {
		t.Errorf("From.Name: want %q, got %q", want.From.Name, got.From.Name)
	}
	if len(got.To) != 1 || got.To[0].Email != "bob@example.com" {
		t.Errorf("To: want [{Bob bob@example.com}], got %v", got.To)
	}

	t.Run("missing UID returns ErrNoRows", func(t *testing.T) {
		_, err := s.ByUID(folder, 9999)
		if err != sql.ErrNoRows {
			t.Errorf("want sql.ErrNoRows for missing UID, got %v", err)
		}
	})
}

// ---- 6. Body ----------------------------------------------------------------

func TestBody(t *testing.T) {
	cases := []struct {
		name     string
		bodyText string
	}{
		{"non-empty body", "hello world"},
		{"empty body", ""},
		{"multiline body", "line one\nline two\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			acct := seedAccount(t, s, "acct", "a@example.com")
			folder := seedFolder(t, s, acct, "INBOX")

			m := newMessage(folder, 1, "subject", time.Now())
			m.BodyText = tc.bodyText
			if err := s.UpsertMessage(m); err != nil {
				t.Fatalf("UpsertMessage: %v", err)
			}

			got, err := s.Body(m.ID)
			if err != nil {
				t.Fatalf("Body(%d): %v", m.ID, err)
			}
			if got != tc.bodyText {
				t.Errorf("Body: want %q, got %q", tc.bodyText, got)
			}
		})
	}

	t.Run("unknown ID returns error", func(t *testing.T) {
		s := openTestStore(t)
		_, err := s.Body(99999)
		if err == nil {
			t.Error("want error for unknown message ID, got nil")
		}
	})
}

// ---- 7. BodyHTML ------------------------------------------------------------

func TestBodyHTML(t *testing.T) {
	cases := []struct {
		name     string
		bodyHTML string
	}{
		{"non-empty HTML", "<p>Hello <b>World</b></p>"},
		{"empty HTML", ""},
		{"multiline HTML", "<p>line one</p>\n<p>line two</p>\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			acct := seedAccount(t, s, "acct", "a@example.com")
			folder := seedFolder(t, s, acct, "INBOX")

			m := newMessage(folder, 1, "subject", time.Now())
			m.BodyHTML = tc.bodyHTML
			if err := s.UpsertMessage(m); err != nil {
				t.Fatalf("UpsertMessage: %v", err)
			}

			got, err := s.BodyHTML(m.ID)
			if err != nil {
				t.Fatalf("BodyHTML(%d): %v", m.ID, err)
			}
			if got != tc.bodyHTML {
				t.Errorf("BodyHTML: want %q, got %q", tc.bodyHTML, got)
			}
		})
	}

	t.Run("unknown ID returns error", func(t *testing.T) {
		s := openTestStore(t)
		_, err := s.BodyHTML(99999)
		if err == nil {
			t.Error("want error for unknown message ID, got nil")
		}
	})
}

// ---- 8. FTS5 Search ---------------------------------------------------------

func TestSearch(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	type fixture struct {
		uid      uint32
		subject  string
		bodyText string
	}
	fixtures := []fixture{
		{1, "Invoice for October", ""},
		{2, "Weekend plans", ""},
		{3, "Team update", "project kickoff meeting notes"},
	}
	for _, f := range fixtures {
		m := newMessage(folder, f.uid, f.subject, time.Now())
		m.BodyText = f.bodyText
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage uid=%d: %v", f.uid, err)
		}
	}

	cases := []struct {
		query   string
		wantLen int
		wantUID uint32 // 0 means don't check
		desc    string
	}{
		{"Invoice", 1, 1, "subject match on first message"},
		{"kickoff", 1, 3, "body_text match on third message"},
		{"notexistent", 0, 0, "no match returns empty slice"},
		{"October", 1, 1, "partial word in subject"},
		{"plans", 1, 2, "subject match on second message"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			results, err := s.Search(tc.query, 10)
			if err != nil {
				t.Fatalf("Search(%q): %v", tc.query, err)
			}
			if len(results) != tc.wantLen {
				t.Errorf("Search(%q): want %d results, got %d", tc.query, tc.wantLen, len(results))
			}
			if tc.wantUID != 0 && len(results) > 0 && results[0].UID != tc.wantUID {
				t.Errorf("Search(%q): want UID %d, got UID %d", tc.query, tc.wantUID, results[0].UID)
			}
		})
	}

	t.Run("Search limit is respected", func(t *testing.T) {
		// Insert additional messages all containing "common" in subject.
		for i := uint32(10); i < 15; i++ {
			m := newMessage(folder, i, "common subject", time.Now())
			if err := s.UpsertMessage(m); err != nil {
				t.Fatalf("UpsertMessage uid=%d: %v", i, err)
			}
		}
		results, err := s.Search("common", 3)
		if err != nil {
			t.Fatalf("Search(limit): %v", err)
		}
		if len(results) > 3 {
			t.Errorf("Search with limit=3 returned %d results", len(results))
		}
	})
}

// ---- 9. FK cascade on folder delete -----------------------------------------

func TestFKCascadeOnFolderDelete(t *testing.T) {
	// TODO: test FK cascade once DeleteFolder is implemented on *Store.
	// For now we verify the cascade works at the DB level by accessing
	// the internal db field (same package — store_test.go is in package store).

	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "Trash")

	now := time.Now()
	for i := uint32(1); i <= 3; i++ {
		m := newMessage(folder, i, "to be cascaded", now)
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage uid=%d: %v", i, err)
		}
	}

	var beforeCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE folder_id = ?`, folder).Scan(&beforeCount); err != nil {
		t.Fatalf("count messages before delete: %v", err)
	}
	if beforeCount != 3 {
		t.Fatalf("want 3 messages before delete, got %d", beforeCount)
	}

	if _, err := s.db.Exec(`DELETE FROM folders WHERE id = ?`, folder); err != nil {
		t.Fatalf("DELETE folder: %v", err)
	}

	var afterCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE folder_id = ?`, folder).Scan(&afterCount); err != nil {
		t.Fatalf("count messages after delete: %v", err)
	}
	if afterCount != 0 {
		t.Errorf("want 0 messages after folder delete (FK cascade), got %d", afterCount)
	}
}

// ---- 10. Attachment sha256 dedupe check -------------------------------------

func TestAttachmentSHA256Dedupe(t *testing.T) {
	// Attachment deduplication is a content-addressed file-system concern, not
	// a DB uniqueness constraint. Two messages can reference the same sha256 —
	// they each get their own attachments row (one per message).

	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	now := time.Now()
	m1 := newMessage(folder, 1, "msg with attach", now)
	m2 := newMessage(folder, 2, "another msg with same attach", now)

	if err := s.UpsertMessage(m1); err != nil {
		t.Fatalf("UpsertMessage m1: %v", err)
	}
	if err := s.UpsertMessage(m2); err != nil {
		t.Fatalf("UpsertMessage m2: %v", err)
	}

	const sharedSHA = "abc123deadbeef"
	insertAttach := func(msgID int64, sha string) {
		t.Helper()
		_, err := s.db.Exec(
			`INSERT INTO attachments(message_id, filename, mime_type, size_bytes, sha256, disk_path)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			msgID, "report.pdf", "application/pdf", 1024, sha, "content/"+sha+".pdf",
		)
		if err != nil {
			t.Fatalf("insert attachment for msg %d: %v", msgID, err)
		}
	}

	insertAttach(m1.ID, sharedSHA)
	insertAttach(m2.ID, sharedSHA)

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM attachments WHERE sha256 = ?`, sharedSHA).Scan(&count); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	// Two rows — one per message — even though they share the same sha256.
	if count != 2 {
		t.Errorf("want 2 attachment rows for shared sha256, got %d", count)
	}
}

// ---- 10. SaveDraft + Drafts -------------------------------------------------

func TestSaveDraftAndDrafts(t *testing.T) {
	t.Run("saved draft round-trips subject", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")

		d := &Draft{
			AccountID: acct,
			Subject:   "Re: project proposal",
			To:        []mail.Address{{Name: "Boss", Email: "boss@example.com"}},
			CC:        []mail.Address{},
			BCC:       []mail.Address{},
			BodyText:  "Sounds good.",
		}
		if err := s.SaveDraft(d); err != nil {
			t.Fatalf("SaveDraft: %v", err)
		}
		if d.ID <= 0 {
			t.Fatalf("SaveDraft should set d.ID > 0, got %d", d.ID)
		}

		drafts, err := s.Drafts(acct)
		if err != nil {
			t.Fatalf("Drafts: %v", err)
		}
		if len(drafts) != 1 {
			t.Fatalf("want 1 draft, got %d", len(drafts))
		}
		if drafts[0].Subject != d.Subject {
			t.Errorf("Subject: want %q, got %q", d.Subject, drafts[0].Subject)
		}
		if len(drafts[0].To) != 1 || drafts[0].To[0].Email != "boss@example.com" {
			t.Errorf("To: want [{Boss boss@example.com}], got %v", drafts[0].To)
		}
	})

	t.Run("re-saving with same ID updates without adding a row", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")

		d := &Draft{
			AccountID: acct,
			Subject:   "Initial subject",
			To:        []mail.Address{},
			CC:        []mail.Address{},
			BCC:       []mail.Address{},
		}
		if err := s.SaveDraft(d); err != nil {
			t.Fatalf("first SaveDraft: %v", err)
		}
		originalID := d.ID

		d.Subject = "Updated subject"
		if err := s.SaveDraft(d); err != nil {
			t.Fatalf("second SaveDraft: %v", err)
		}

		drafts, err := s.Drafts(acct)
		if err != nil {
			t.Fatalf("Drafts after update: %v", err)
		}
		if len(drafts) != 1 {
			t.Errorf("want 1 draft after update, got %d", len(drafts))
		}
		if drafts[0].ID != originalID {
			t.Errorf("ID changed after update: was %d, now %d", originalID, drafts[0].ID)
		}
		if drafts[0].Subject != "Updated subject" {
			t.Errorf("Subject after update: want %q, got %q", "Updated subject", drafts[0].Subject)
		}
	})

	t.Run("drafts are isolated by account", func(t *testing.T) {
		s := openTestStore(t)
		a1 := seedAccount(t, s, "alice", "alice@example.com")
		a2 := seedAccount(t, s, "bob", "bob@example.com")

		if err := s.SaveDraft(&Draft{AccountID: a1, Subject: "Alice draft", To: []mail.Address{}, CC: []mail.Address{}, BCC: []mail.Address{}}); err != nil {
			t.Fatalf("SaveDraft alice: %v", err)
		}
		if err := s.SaveDraft(&Draft{AccountID: a2, Subject: "Bob draft", To: []mail.Address{}, CC: []mail.Address{}, BCC: []mail.Address{}}); err != nil {
			t.Fatalf("SaveDraft bob: %v", err)
		}

		aliceDrafts, err := s.Drafts(a1)
		if err != nil {
			t.Fatalf("Drafts(alice): %v", err)
		}
		if len(aliceDrafts) != 1 || aliceDrafts[0].Subject != "Alice draft" {
			t.Errorf("alice should have exactly 1 draft, got %d: %v", len(aliceDrafts), aliceDrafts)
		}

		bobDrafts, err := s.Drafts(a2)
		if err != nil {
			t.Fatalf("Drafts(bob): %v", err)
		}
		if len(bobDrafts) != 1 || bobDrafts[0].Subject != "Bob draft" {
			t.Errorf("bob should have exactly 1 draft, got %d: %v", len(bobDrafts), bobDrafts)
		}
	})

	t.Run("Drafts returns newest-updated first", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")

		// Use explicit UpdatedAt so ordering is deterministic.
		subjects := []string{"alpha", "beta", "gamma"}
		for i, subj := range subjects {
			d := &Draft{
				AccountID: acct,
				Subject:   subj,
				To:        []mail.Address{},
				CC:        []mail.Address{},
				BCC:       []mail.Address{},
			}
			if err := s.SaveDraft(d); err != nil {
				t.Fatalf("SaveDraft %q: %v", subj, err)
			}
			// Backdate updated_at so gamma is newest.
			offset := int64(len(subjects)-1-i) * 3600
			if _, err := s.db.Exec(`UPDATE drafts SET updated_at = updated_at - ? WHERE id = ?`, offset, d.ID); err != nil {
				t.Fatalf("backdate draft %q: %v", subj, err)
			}
		}

		drafts, err := s.Drafts(acct)
		if err != nil {
			t.Fatalf("Drafts: %v", err)
		}
		if len(drafts) != 3 {
			t.Fatalf("want 3 drafts, got %d", len(drafts))
		}
		// gamma was inserted last with the smallest subtracted offset, so newest.
		if drafts[0].Subject != "gamma" {
			t.Errorf("first draft should be %q (newest updated_at), got %q", "gamma", drafts[0].Subject)
		}
	})
}

// ---- edge cases / cross-cutting ---------------------------------------------

func TestListFolderEmptyFolder(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	msgs, err := s.ListFolder(folder, 10, 0)
	if err != nil {
		t.Fatalf("ListFolder on empty folder: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("want 0 messages for empty folder, got %d", len(msgs))
	}
}

func TestUpsertMessageSetsID(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	m := newMessage(folder, 77, "id check", time.Now())
	if err := s.UpsertMessage(m); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	if m.ID <= 0 {
		t.Errorf("want m.ID > 0 after UpsertMessage, got %d", m.ID)
	}

	// Re-upsert — ID must remain stable.
	prevID := m.ID
	if err := s.UpsertMessage(m); err != nil {
		t.Fatalf("re-UpsertMessage: %v", err)
	}
	if m.ID != prevID {
		t.Errorf("ID changed on re-upsert: was %d, now %d", prevID, m.ID)
	}
}

func TestMessageReferencesRoundTrip(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	want := []string{"<aaa@example.com>", "<bbb@example.com>", "<ccc@example.com>"}
	m := newMessage(folder, 1, "refs", time.Now())
	m.References = want
	if err := s.UpsertMessage(m); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	got, err := s.ByUID(folder, 1)
	if err != nil {
		t.Fatalf("ByUID: %v", err)
	}

	sort.Strings(got.References)
	sort.Strings(want)
	if len(got.References) != len(want) {
		t.Fatalf("References len: want %d, got %d", len(want), len(got.References))
	}
	for i := range want {
		if got.References[i] != want[i] {
			t.Errorf("References[%d]: want %q, got %q", i, want[i], got.References[i])
		}
	}
}

func TestFlagsPersist(t *testing.T) {
	cases := []struct {
		name  string
		flags mail.Flag
	}{
		{"seen", mail.FlagSeen},
		{"flagged", mail.FlagFlagged},
		{"seen+flagged", mail.FlagSeen | mail.FlagFlagged},
		{"answered", mail.FlagAnswered},
		{"zero", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			acct := seedAccount(t, s, "acct", "a@example.com")
			folder := seedFolder(t, s, acct, "INBOX")

			m := newMessage(folder, 1, "flags test", time.Now())
			m.Flags = tc.flags
			if err := s.UpsertMessage(m); err != nil {
				t.Fatalf("UpsertMessage: %v", err)
			}

			got, err := s.ByUID(folder, 1)
			if err != nil {
				t.Fatalf("ByUID: %v", err)
			}
			if got.Flags != tc.flags {
				t.Errorf("Flags: want %b, got %b", tc.flags, got.Flags)
			}
		})
	}
}

// ---- 11. GetFolder ----------------------------------------------------------

func TestGetFolder(t *testing.T) {
	t.Run("returns folder by ID", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "INBOX")

		f, err := s.GetFolder(fid)
		if err != nil {
			t.Fatalf("GetFolder: %v", err)
		}
		if f.ID != fid {
			t.Errorf("ID: want %d, got %d", fid, f.ID)
		}
		if f.Name != "INBOX" {
			t.Errorf("Name: want INBOX, got %q", f.Name)
		}
		if f.AccountID != acct {
			t.Errorf("AccountID: want %d, got %d", acct, f.AccountID)
		}
	})

	t.Run("missing ID returns error", func(t *testing.T) {
		s := openTestStore(t)
		_, err := s.GetFolder(99999)
		if err == nil {
			t.Error("want error for unknown folder ID, got nil")
		}
	})
}

// ---- 12. FolderByName -------------------------------------------------------

func TestFolderByName(t *testing.T) {
	t.Run("returns folder by account+name", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "Sent")

		f, err := s.FolderByName(acct, "Sent")
		if err != nil {
			t.Fatalf("FolderByName: %v", err)
		}
		if f.ID != fid {
			t.Errorf("ID: want %d, got %d", fid, f.ID)
		}
		if f.Name != "Sent" {
			t.Errorf("Name: want Sent, got %q", f.Name)
		}
	})

	t.Run("wrong account returns error", func(t *testing.T) {
		s := openTestStore(t)
		acct1 := seedAccount(t, s, "acct1", "a@example.com")
		acct2 := seedAccount(t, s, "acct2", "b@example.com")
		seedFolder(t, s, acct1, "INBOX")

		_, err := s.FolderByName(acct2, "INBOX")
		if err == nil {
			t.Error("want error when folder belongs to different account, got nil")
		}
	})
}

// ---- 13. ExistingUIDs -------------------------------------------------------

func TestExistingUIDs(t *testing.T) {
	t.Run("returns empty map for empty folder", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		folder := seedFolder(t, s, acct, "INBOX")

		uids, err := s.ExistingUIDs(folder)
		if err != nil {
			t.Fatalf("ExistingUIDs: %v", err)
		}
		if len(uids) != 0 {
			t.Errorf("want empty map, got %d entries", len(uids))
		}
	})

	t.Run("returns correct UIDs after inserts", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		folder := seedFolder(t, s, acct, "INBOX")

		for _, uid := range []uint32{10, 20, 30} {
			m := newMessage(folder, uid, "msg", time.Now())
			if err := s.UpsertMessage(m); err != nil {
				t.Fatalf("UpsertMessage uid=%d: %v", uid, err)
			}
		}

		uids, err := s.ExistingUIDs(folder)
		if err != nil {
			t.Fatalf("ExistingUIDs: %v", err)
		}
		if len(uids) != 3 {
			t.Fatalf("want 3 UIDs, got %d", len(uids))
		}
		for _, want := range []uint32{10, 20, 30} {
			if _, ok := uids[want]; !ok {
				t.Errorf("UID %d missing from map", want)
			}
		}
	})
}

// ---- 14. ClearFolderMessages ------------------------------------------------

func TestClearFolderMessages(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	for _, uid := range []uint32{1, 2, 3} {
		m := newMessage(folder, uid, "msg", time.Now())
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}
	}

	if err := s.ClearFolderMessages(folder); err != nil {
		t.Fatalf("ClearFolderMessages: %v", err)
	}

	msgs, err := s.ListFolder(folder, 100, 0)
	if err != nil {
		t.Fatalf("ListFolder after clear: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("want 0 messages after clear, got %d", len(msgs))
	}
}

// ---- 15. UpdateFlags --------------------------------------------------------

func TestUpdateFlags(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	m := newMessage(folder, 5, "flag test", time.Now())
	m.Flags = 0
	if err := s.UpsertMessage(m); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	if err := s.UpdateFlags(folder, 5, mail.FlagSeen|mail.FlagFlagged); err != nil {
		t.Fatalf("UpdateFlags: %v", err)
	}

	got, err := s.ByUID(folder, 5)
	if err != nil {
		t.Fatalf("ByUID: %v", err)
	}
	if got.Flags != mail.FlagSeen|mail.FlagFlagged {
		t.Errorf("Flags: want %b, got %b", mail.FlagSeen|mail.FlagFlagged, got.Flags)
	}
}

// ---- 16. DeleteMessageByUID -------------------------------------------------

func TestDeleteMessageByUID(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	folder := seedFolder(t, s, acct, "INBOX")

	for _, uid := range []uint32{1, 2, 3} {
		m := newMessage(folder, uid, "msg", time.Now())
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage uid=%d: %v", uid, err)
		}
	}

	if err := s.DeleteMessageByUID(folder, 2); err != nil {
		t.Fatalf("DeleteMessageByUID: %v", err)
	}

	msgs, err := s.ListFolder(folder, 100, 0)
	if err != nil {
		t.Fatalf("ListFolder: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages after delete, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.UID == 2 {
			t.Error("deleted UID 2 still present in store")
		}
	}
}

// ---- 17. UpdateFolderSync ---------------------------------------------------

func TestUpdateFolderSync(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	fid := seedFolder(t, s, acct, "INBOX")

	syncAt := time.Now().Truncate(time.Second)
	if err := s.UpdateFolderSync(fid, 12345, 67890, syncAt); err != nil {
		t.Fatalf("UpdateFolderSync: %v", err)
	}

	f, err := s.GetFolder(fid)
	if err != nil {
		t.Fatalf("GetFolder: %v", err)
	}
	if f.UIDValidity != 12345 {
		t.Errorf("UIDValidity: want 12345, got %d", f.UIDValidity)
	}
	if f.HighestModSeq != 67890 {
		t.Errorf("HighestModSeq: want 67890, got %d", f.HighestModSeq)
	}
	if !f.LastSyncAt.Equal(syncAt) {
		t.Errorf("LastSyncAt: want %v, got %v", syncAt, f.LastSyncAt)
	}
}

// ---- 18. InsertAttachment + ListAttachments ---------------------------------

func TestInsertAndListAttachments(t *testing.T) {
	t.Run("round-trips attachment fields", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		folder := seedFolder(t, s, acct, "INBOX")

		m := newMessage(folder, 1, "with attach", time.Now())
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}

		a := &mail.Attachment{
			MessageID: m.ID,
			Filename:  "report.pdf",
			MIMEType:  "application/pdf",
			SizeBytes: 4096,
			SHA256:    "deadbeef",
			DiskPath:  "de/deadbeef",
		}
		if err := s.InsertAttachment(a); err != nil {
			t.Fatalf("InsertAttachment: %v", err)
		}
		if a.ID <= 0 {
			t.Errorf("want a.ID > 0 after insert, got %d", a.ID)
		}

		list, err := s.ListAttachments(m.ID)
		if err != nil {
			t.Fatalf("ListAttachments: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("want 1 attachment, got %d", len(list))
		}
		got := list[0]
		if got.Filename != "report.pdf" {
			t.Errorf("Filename: want %q, got %q", "report.pdf", got.Filename)
		}
		if got.MIMEType != "application/pdf" {
			t.Errorf("MIMEType: want %q, got %q", "application/pdf", got.MIMEType)
		}
		if got.SHA256 != "deadbeef" {
			t.Errorf("SHA256: want %q, got %q", "deadbeef", got.SHA256)
		}
		if got.SizeBytes != 4096 {
			t.Errorf("SizeBytes: want 4096, got %d", got.SizeBytes)
		}
		if got.DiskPath != "de/deadbeef" {
			t.Errorf("DiskPath: want %q, got %q", "de/deadbeef", got.DiskPath)
		}
	})

	t.Run("empty list for message with no attachments", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		folder := seedFolder(t, s, acct, "INBOX")

		m := newMessage(folder, 1, "no attach", time.Now())
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}

		list, err := s.ListAttachments(m.ID)
		if err != nil {
			t.Fatalf("ListAttachments: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("want 0 attachments, got %d", len(list))
		}
	})

	t.Run("multiple attachments per message", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		folder := seedFolder(t, s, acct, "INBOX")

		m := newMessage(folder, 1, "multi attach", time.Now())
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}

		for i, name := range []string{"a.pdf", "b.jpg"} {
			a := &mail.Attachment{
				MessageID: m.ID,
				Filename:  name,
				MIMEType:  "application/octet-stream",
				SizeBytes: int64(i + 1),
				SHA256:    name + "-sha",
				DiskPath:  "00/" + name,
			}
			if err := s.InsertAttachment(a); err != nil {
				t.Fatalf("InsertAttachment %q: %v", name, err)
			}
		}

		list, err := s.ListAttachments(m.ID)
		if err != nil {
			t.Fatalf("ListAttachments: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("want 2 attachments, got %d", len(list))
		}
	})

	t.Run("content_id round-trips through insert and list", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		folder := seedFolder(t, s, acct, "INBOX")

		m := newMessage(folder, 1, "inline img", time.Now())
		if err := s.UpsertMessage(m); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}

		a := &mail.Attachment{
			MessageID: m.ID,
			Filename:  "logo.png",
			MIMEType:  "image/png",
			SizeBytes: 512,
			SHA256:    "logo-sha",
			ContentID: "logo@example.com",
			DiskPath:  "00/logo-sha",
		}
		if err := s.InsertAttachment(a); err != nil {
			t.Fatalf("InsertAttachment: %v", err)
		}

		list, err := s.ListAttachments(m.ID)
		if err != nil {
			t.Fatalf("ListAttachments: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("want 1 attachment, got %d", len(list))
		}
		if list[0].ContentID != "logo@example.com" {
			t.Errorf("ContentID = %q, want %q", list[0].ContentID, "logo@example.com")
		}
	})
}

// ---- CountUnread ------------------------------------------------------------

func TestCountUnread(t *testing.T) {
	t.Run("empty folder returns 0", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		folder := seedFolder(t, s, acct, "INBOX")
		n, err := s.CountUnread(folder)
		if err != nil {
			t.Fatalf("CountUnread: %v", err)
		}
		if n != 0 {
			t.Errorf("empty folder: got %d, want 0", n)
		}
	})

	t.Run("counts only messages without FlagSeen", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		folder := seedFolder(t, s, acct, "INBOX")

		mk := func(uid uint32, seen bool) {
			m := newMessage(folder, uid, "subj", time.Now())
			if seen {
				m.Flags = mail.FlagSeen
			}
			if err := s.UpsertMessage(m); err != nil {
				t.Fatalf("UpsertMessage(uid=%d): %v", uid, err)
			}
		}

		mk(1, false)
		mk(2, true)
		mk(3, false)
		mk(4, false)
		mk(5, true)

		n, err := s.CountUnread(folder)
		if err != nil {
			t.Fatalf("CountUnread: %v", err)
		}
		if n != 3 {
			t.Errorf("got %d, want 3", n)
		}
	})

	t.Run("nonexistent folder returns 0 without error", func(t *testing.T) {
		s := openTestStore(t)
		n, err := s.CountUnread(99999)
		if err != nil {
			t.Fatalf("CountUnread on unknown folder: %v", err)
		}
		if n != 0 {
			t.Errorf("got %d, want 0", n)
		}
	})

	t.Run("isolated by folder", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		inbox := seedFolder(t, s, acct, "INBOX")
		other := seedFolder(t, s, acct, "Other")

		m1 := newMessage(inbox, 1, "a", time.Now())
		m2 := newMessage(other, 1, "b", time.Now())
		if err := s.UpsertMessage(m1); err != nil {
			t.Fatalf("UpsertMessage inbox: %v", err)
		}
		if err := s.UpsertMessage(m2); err != nil {
			t.Fatalf("UpsertMessage other: %v", err)
		}

		if n, _ := s.CountUnread(inbox); n != 1 {
			t.Errorf("inbox unread: got %d, want 1", n)
		}
		if n, _ := s.CountUnread(other); n != 1 {
			t.Errorf("other unread: got %d, want 1", n)
		}
	})
}
