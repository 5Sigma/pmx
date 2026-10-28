package mailsend_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mailsend"
	"github.com/5Sigma/pmx/internal/secrets"
)

// stubStore is a minimal mail.MessageStore where only the four outbox methods
// are implemented. Every other method panics to surface unexpected calls during
// tests. It stands in for the real SQLite store so tests stay in-process and
// fast.
type stubStore struct {
	mail.MessageStore // embed to satisfy the interface; panics on any unimplemented call

	markSentCalls   []int64
	markFailedCalls []struct {
		id  int64
		msg string
	}

	// If non-nil, MarkOutboxFailed returns this error.
	markFailedErr error
}

func (s *stubStore) MarkOutboxSent(id int64) error {
	s.markSentCalls = append(s.markSentCalls, id)
	return nil
}

func (s *stubStore) MarkOutboxFailed(id int64, msg string) error {
	s.markFailedCalls = append(s.markFailedCalls, struct {
		id  int64
		msg string
	}{id, msg})
	return s.markFailedErr
}

func (s *stubStore) ListPendingOutbox(limit int) ([]*mail.OutboxItem, error) {
	return nil, nil
}

func (s *stubStore) ListPendingOutboxForAccount(accountID int64, limit int) ([]*mail.OutboxItem, error) {
	return nil, nil
}

func (s *stubStore) EnsureAccount(name, email string) (int64, error) { return 1, nil }

func (s *stubStore) Close() error { return nil }

// buildSecrets creates a secrets.Store that resolves the given account's
// password via the fakeKeyring injected in engine_test.go's fakeKeyring type.
func buildSecrets(t *testing.T, acct config.Account, pass string) *secrets.Store {
	t.Helper()
	cfg := config.Default()
	cfg.Accounts = []config.Account{acct}
	return secrets.NewWithKeyring(cfg, &fakeKeyring{pass: pass})
}

// TestSendOne_SkipsTerminalItem verifies that SendOne exits early without
// calling MarkOutboxSent or MarkOutboxFailed when the item has already hit
// maxAttempts.
func TestSendOne_SkipsTerminalItem(t *testing.T) {
	const maxAttempts = 3
	acct := config.Account{Name: "test", Email: "user@localhost"}
	sec := buildSecrets(t, acct, "pass")
	stub := &stubStore{}

	item := &mail.OutboxItem{
		ID:        42,
		AccountID: 1,
		Attempts:  maxAttempts, // already at the limit
		RawEML:    []byte("From: user@localhost\r\nTo: recv@localhost\r\n\r\nBody"),
		QueuedAt:  time.Now(),
	}

	if _, err := mailsend.SendOne(
		context.Background(),
		stub,
		sec,
		slog.Default(),
		acct,
		item,
		maxAttempts,
	); err != nil {
		t.Fatalf("SendOne returned unexpected error: %v", err)
	}
	if len(stub.markSentCalls) != 0 {
		t.Errorf("want 0 MarkOutboxSent calls for terminal item, got %d", len(stub.markSentCalls))
	}
	if len(stub.markFailedCalls) != 0 {
		t.Errorf("want 0 MarkOutboxFailed calls for terminal item, got %d", len(stub.markFailedCalls))
	}
}

// TestSendOne_MalformedEML verifies that SendOne calls MarkOutboxFailed
// (parking the item) when the RawEML cannot be parsed, even if an SMTP server
// is available.
func TestSendOne_MalformedEML(t *testing.T) {
	_, acct := startSendServer(t)
	sec := buildSecrets(t, acct, "testpass")
	stub := &stubStore{}

	item := &mail.OutboxItem{
		ID:        7,
		AccountID: 1,
		Attempts:  0,
		RawEML:    []byte("not an eml"), // deliberately malformed
		QueuedAt:  time.Now(),
	}

	const maxAttempts = 3
	if _, err := mailsend.SendOne(
		context.Background(),
		stub,
		sec,
		slog.Default(),
		acct,
		item,
		maxAttempts,
	); err != nil {
		t.Fatalf("SendOne returned unexpected error: %v", err)
	}
	// sendOneWithPass calls MarkOutboxFailed once per remaining attempt slot
	// to park the item at maxAttempts.
	if len(stub.markFailedCalls) == 0 {
		t.Error("want MarkOutboxFailed called at least once for malformed EML, got 0 calls")
	}
	if len(stub.markSentCalls) != 0 {
		t.Errorf("want 0 MarkOutboxSent calls for malformed EML, got %d", len(stub.markSentCalls))
	}
}
