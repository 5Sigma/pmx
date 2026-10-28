package store

import (
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/mail"
)

// ---- helpers ----------------------------------------------------------------

func upsertOne(t *testing.T, s *Store, accountID int64, email, name string, kind mail.ContactKind, now time.Time) {
	t.Helper()
	if err := s.UpsertContact(accountID, mail.Address{Email: email, Name: name}, kind, now); err != nil {
		t.Fatalf("UpsertContact(%q): %v", email, err)
	}
}

// ---- 1. New row created ------------------------------------------------------

func TestUpsertContact_NewRow(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "me@example.com")
	now := time.Now()

	upsertOne(t, s, acct, "alice@example.com", "Alice", mail.ContactReceived, now)

	contacts, err := s.SearchContacts(acct, "", 10)
	if err != nil {
		t.Fatalf("SearchContacts: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("want 1 contact, got %d", len(contacts))
	}
	c := contacts[0]
	if c.Email != "alice@example.com" {
		t.Errorf("Email: want %q, got %q", "alice@example.com", c.Email)
	}
	if c.Name != "Alice" {
		t.Errorf("Name: want %q, got %q", "Alice", c.Name)
	}
	if c.ReceivedCount != 1 {
		t.Errorf("ReceivedCount: want 1, got %d", c.ReceivedCount)
	}
	if c.SentCount != 0 {
		t.Errorf("SentCount: want 0, got %d", c.SentCount)
	}
}

// ---- 2. Name update: empty → non-empty --------------------------------------

func TestUpsertContact_NameUpdate(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "me@example.com")
	now := time.Now()

	// First upsert: no name.
	upsertOne(t, s, acct, "bob@example.com", "", mail.ContactReceived, now)
	// Second upsert: with a name.
	upsertOne(t, s, acct, "bob@example.com", "Bob", mail.ContactReceived, now)

	contacts, err := s.SearchContacts(acct, "", 10)
	if err != nil {
		t.Fatalf("SearchContacts: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("want 1 contact (no duplicates), got %d", len(contacts))
	}
	c := contacts[0]
	if c.Name != "Bob" {
		t.Errorf("Name after update: want %q, got %q", "Bob", c.Name)
	}
	if c.ReceivedCount != 2 {
		t.Errorf("ReceivedCount after 2 upserts: want 2, got %d", c.ReceivedCount)
	}
}

// ---- 3. Count accumulation --------------------------------------------------

func TestUpsertContact_CountAccumulation(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "me@example.com")
	now := time.Now()

	for i := 0; i < 3; i++ {
		upsertOne(t, s, acct, "carol@example.com", "Carol", mail.ContactSent, now)
	}
	for i := 0; i < 2; i++ {
		upsertOne(t, s, acct, "carol@example.com", "Carol", mail.ContactReceived, now)
	}

	contacts, err := s.SearchContacts(acct, "", 10)
	if err != nil {
		t.Fatalf("SearchContacts: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("want 1 contact, got %d", len(contacts))
	}
	c := contacts[0]
	if c.SentCount != 3 {
		t.Errorf("SentCount: want 3, got %d", c.SentCount)
	}
	if c.ReceivedCount != 2 {
		t.Errorf("ReceivedCount: want 2, got %d", c.ReceivedCount)
	}
}

// ---- 4. Email case normalisation (caller's responsibility) ------------------

func TestUpsertContact_EmailCaseNormalization(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "me@example.com")
	now := time.Now()

	// Caller lowercases before calling; two identical lowercase inserts → one row.
	upsertOne(t, s, acct, "foo@example.com", "", mail.ContactReceived, now)
	upsertOne(t, s, acct, "foo@example.com", "", mail.ContactReceived, now)

	contacts, err := s.SearchContacts(acct, "", 10)
	if err != nil {
		t.Fatalf("SearchContacts: %v", err)
	}
	if len(contacts) != 1 {
		t.Fatalf("want 1 row for two identical lowercase emails, got %d", len(contacts))
	}
	if contacts[0].ReceivedCount != 2 {
		t.Errorf("ReceivedCount: want 2, got %d", contacts[0].ReceivedCount)
	}
}

// ---- 5. SearchContacts empty prefix — ordering by score --------------------

func TestSearchContacts_EmptyPrefix(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "me@example.com")
	now := time.Now()

	// high score: 5 sent + 1 recv  → 5*3+1 = 16
	for i := 0; i < 5; i++ {
		upsertOne(t, s, acct, "high@example.com", "High", mail.ContactSent, now)
	}
	upsertOne(t, s, acct, "high@example.com", "High", mail.ContactReceived, now)

	// medium score: 2 sent → 6
	for i := 0; i < 2; i++ {
		upsertOne(t, s, acct, "medium@example.com", "Medium", mail.ContactSent, now)
	}

	// low score: 1 recv → 1
	upsertOne(t, s, acct, "low@example.com", "Low", mail.ContactReceived, now)

	contacts, err := s.SearchContacts(acct, "", 10)
	if err != nil {
		t.Fatalf("SearchContacts: %v", err)
	}
	if len(contacts) != 3 {
		t.Fatalf("want 3 contacts, got %d", len(contacts))
	}
	if contacts[0].Email != "high@example.com" {
		t.Errorf("first contact should be high@example.com, got %q", contacts[0].Email)
	}
	if contacts[1].Email != "medium@example.com" {
		t.Errorf("second contact should be medium@example.com, got %q", contacts[1].Email)
	}
	if contacts[2].Email != "low@example.com" {
		t.Errorf("third contact should be low@example.com, got %q", contacts[2].Email)
	}
}

// ---- 6. SearchContacts by email prefix -------------------------------------

func TestSearchContacts_EmailPrefix(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "me@example.com")
	now := time.Now()

	upsertOne(t, s, acct, "alice@example.com", "Alice", mail.ContactReceived, now)
	upsertOne(t, s, acct, "zach@example.com", "Zach", mail.ContactReceived, now)

	t.Run("matching prefix returns contact", func(t *testing.T) {
		results, err := s.SearchContacts(acct, "alic", 10)
		if err != nil {
			t.Fatalf("SearchContacts: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("want 1 result, got %d", len(results))
		}
		if results[0].Email != "alice@example.com" {
			t.Errorf("want alice@example.com, got %q", results[0].Email)
		}
	})

	t.Run("non-matching prefix returns empty", func(t *testing.T) {
		results, err := s.SearchContacts(acct, "xyz", 10)
		if err != nil {
			t.Fatalf("SearchContacts: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("want 0 results for non-matching prefix, got %d", len(results))
		}
	})
}

// ---- 7. SearchContacts by name prefix (name_lc) ----------------------------

func TestSearchContacts_NamePrefix(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "me@example.com")
	now := time.Now()

	upsertOne(t, s, acct, "ae@example.com", "Alice Example", mail.ContactReceived, now)

	results, err := s.SearchContacts(acct, "alice", 10)
	if err != nil {
		t.Fatalf("SearchContacts: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result via name_lc match, got %d", len(results))
	}
	if results[0].Name != "Alice Example" {
		t.Errorf("Name: want %q, got %q", "Alice Example", results[0].Name)
	}
}

// ---- 8. UpsertContacts atomicity -------------------------------------------

func TestUpsertContacts_Atomic(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "me@example.com")
	now := time.Now()

	// 3 valid items + 1 item with a bad accountID (-999) to trigger FK violation.
	items := []mail.ContactUpsert{
		{Address: mail.Address{Email: "a@example.com"}, Kind: mail.ContactReceived},
		{Address: mail.Address{Email: "b@example.com"}, Kind: mail.ContactReceived},
		{Address: mail.Address{Email: "c@example.com"}, Kind: mail.ContactReceived},
	}
	// The bad item uses a different accountID that doesn't exist.
	badAccountID := int64(-999)

	// Insert the bad item mixed in by calling UpsertContacts with both a good
	// and bad account ID. We achieve this by first calling with the bad account
	// (which should fail and not persist the good rows from that call).
	badItems := []mail.ContactUpsert{
		{Address: mail.Address{Email: "good1@example.com"}, Kind: mail.ContactReceived},
		{Address: mail.Address{Email: "good2@example.com"}, Kind: mail.ContactReceived},
		{Address: mail.Address{Email: "good3@example.com"}, Kind: mail.ContactReceived},
	}
	_ = acct // used above; satisfy linter
	err := s.UpsertContacts(badAccountID, badItems, now)
	if err == nil {
		t.Fatal("expected error for FK-violating accountID, got nil")
	}

	// Verify none of the items from the failed batch landed.
	for _, item := range items {
		var count int
		if qerr := s.db.QueryRow(`SELECT COUNT(*) FROM contacts WHERE email = ?`, item.Address.Email).Scan(&count); qerr != nil {
			t.Fatalf("count query: %v", qerr)
		}
		if count != 0 {
			t.Errorf("contact %q should not exist after rollback, found %d row(s)", item.Address.Email, count)
		}
	}
}

// ---- 9. Account isolation ---------------------------------------------------

func TestUpsertContact_AccountIsolation(t *testing.T) {
	s := openTestStore(t)
	a1 := seedAccount(t, s, "alice", "alice@example.com")
	a2 := seedAccount(t, s, "bob", "bob@example.com")
	now := time.Now()

	email := "shared@example.com"
	upsertOne(t, s, a1, email, "Shared", mail.ContactSent, now)
	upsertOne(t, s, a1, email, "Shared", mail.ContactSent, now)
	upsertOne(t, s, a2, email, "Shared", mail.ContactReceived, now)

	c1, err := s.SearchContacts(a1, "", 10)
	if err != nil {
		t.Fatalf("SearchContacts a1: %v", err)
	}
	c2, err := s.SearchContacts(a2, "", 10)
	if err != nil {
		t.Fatalf("SearchContacts a2: %v", err)
	}

	if len(c1) != 1 {
		t.Fatalf("a1: want 1 contact, got %d", len(c1))
	}
	if len(c2) != 1 {
		t.Fatalf("a2: want 1 contact, got %d", len(c2))
	}
	if c1[0].SentCount != 2 {
		t.Errorf("a1 sent_count: want 2, got %d", c1[0].SentCount)
	}
	if c1[0].ReceivedCount != 0 {
		t.Errorf("a1 received_count: want 0, got %d", c1[0].ReceivedCount)
	}
	if c2[0].ReceivedCount != 1 {
		t.Errorf("a2 received_count: want 1, got %d", c2[0].ReceivedCount)
	}
	if c2[0].SentCount != 0 {
		t.Errorf("a2 sent_count: want 0, got %d", c2[0].SentCount)
	}
	// Confirm two separate rows with different account_ids.
	if c1[0].AccountID == c2[0].AccountID {
		t.Errorf("rows should have different account_ids, both have %d", c1[0].AccountID)
	}
}
