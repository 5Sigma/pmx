package compose

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/store"
)

// openIntegrationStore opens a real SQLite store backed by a temp directory.
// It stands in for the production store that the UI wires together at runtime;
// using a real store (instead of a fake) exercises the actual SQL paths.
func openIntegrationStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"), filepath.Join(dir, "attachments"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestBuildForwardAttachmentsAfterByUID pins the integration contract that
// prompted the bug fix: ByUID does not hydrate Message.Attachments, so callers
// must call ListAttachments and assign the result before passing the message to
// BuildForward.  After that hydration step the forward seed must carry the
// attachment.
func TestBuildForwardAttachmentsAfterByUID(t *testing.T) {
	s := openIntegrationStore(t)

	acctID, err := s.EnsureAccount("work", "me@example.com")
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}

	folderID, err := s.EnsureFolder(acctID, "INBOX")
	if err != nil {
		t.Fatalf("EnsureFolder: %v", err)
	}

	msg := &mail.Message{
		FolderID:     folderID,
		UID:          1,
		Subject:      "Budget Q4",
		From:         mail.Address{Name: "Alice", Email: "alice@example.com"},
		To:           []mail.Address{{Name: "Bob", Email: "bob@example.com"}},
		CC:           []mail.Address{},
		BCC:          []mail.Address{},
		HasAttach:    true,
		DateReceived: time.Now(),
	}
	if err := s.UpsertMessage(msg); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	att := &mail.Attachment{
		MessageID: msg.ID,
		Filename:  "budget.xlsx",
		MIMEType:  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		SizeBytes: 8192,
		SHA256:    "deadbeef01234567",
		DiskPath:  "de/deadbeef01234567",
	}
	if err := s.InsertAttachment(att); err != nil {
		t.Fatalf("InsertAttachment: %v", err)
	}

	// Step 1: ByUID alone does not populate Attachments — this is the
	// pre-condition that caused the original bug.
	src, err := s.ByUID(folderID, 1)
	if err != nil {
		t.Fatalf("ByUID: %v", err)
	}
	if len(src.Attachments) != 0 {
		t.Errorf("pre-condition: ByUID should not hydrate Attachments, got %d", len(src.Attachments))
	}

	// Step 2: explicit hydration — the pattern inserted by the fix.
	atts, err := s.ListAttachments(src.ID)
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	src.Attachments = atts

	acct := config.Account{Email: "me@example.com"}
	seed := BuildForward(src, acct, "body text", func(a mail.Attachment) string {
		return s.AttachmentPath(a)
	})

	if len(seed.Attachments) != 1 {
		t.Fatalf("seed.Attachments: want 1, got %d", len(seed.Attachments))
	}
	if seed.Attachments[0].Filename != "budget.xlsx" {
		t.Errorf("Attachments[0].Filename = %q, want %q", seed.Attachments[0].Filename, "budget.xlsx")
	}
	if seed.Attachments[0].MIMEType != att.MIMEType {
		t.Errorf("Attachments[0].MIMEType = %q, want %q", seed.Attachments[0].MIMEType, att.MIMEType)
	}
	if seed.Attachments[0].Size != att.SizeBytes {
		t.Errorf("Attachments[0].Size = %d, want %d", seed.Attachments[0].Size, att.SizeBytes)
	}
	if seed.Subject != "Fwd: Budget Q4" {
		t.Errorf("Subject = %q, want %q", seed.Subject, "Fwd: Budget Q4")
	}
}

// TestBuildForwardWithoutHydrationProducesNoAttachments documents the broken
// state: if the caller forgets the ListAttachments step, BuildForward silently
// produces zero attachments even though the message HasAttach flag is true.
func TestBuildForwardWithoutHydrationProducesNoAttachments(t *testing.T) {
	s := openIntegrationStore(t)

	acctID, err := s.EnsureAccount("work", "me@example.com")
	if err != nil {
		t.Fatalf("EnsureAccount: %v", err)
	}
	folderID, err := s.EnsureFolder(acctID, "INBOX")
	if err != nil {
		t.Fatalf("EnsureFolder: %v", err)
	}

	msg := &mail.Message{
		FolderID:     folderID,
		UID:          2,
		Subject:      "Slides",
		From:         mail.Address{Name: "Carol", Email: "carol@example.com"},
		To:           []mail.Address{{Email: "me@example.com"}},
		CC:           []mail.Address{},
		BCC:          []mail.Address{},
		HasAttach:    true,
		DateReceived: time.Now(),
	}
	if err := s.UpsertMessage(msg); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}
	if err := s.InsertAttachment(&mail.Attachment{
		MessageID: msg.ID,
		Filename:  "deck.pptx",
		MIMEType:  "application/vnd.ms-powerpoint",
		SizeBytes: 2048,
		SHA256:    "aabbccdd",
		DiskPath:  "aa/aabbccdd",
	}); err != nil {
		t.Fatalf("InsertAttachment: %v", err)
	}

	// Intentionally omit ListAttachments — simulates the pre-fix code path.
	src, err := s.ByUID(folderID, 2)
	if err != nil {
		t.Fatalf("ByUID: %v", err)
	}

	acct := config.Account{Email: "me@example.com"}
	seed := BuildForward(src, acct, "body", func(a mail.Attachment) string {
		return s.AttachmentPath(a)
	})

	if len(seed.Attachments) != 0 {
		t.Errorf("without hydration expected 0 attachments in seed, got %d — pre-condition violated", len(seed.Attachments))
	}
}
