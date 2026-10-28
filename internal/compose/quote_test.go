package compose

import (
	"strings"
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/mail"
)

func TestQuoteReply(t *testing.T) {
	t.Run("two-line body is prefixed with >", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Name: "Alice", Email: "alice@example.com"},
			DateSent: time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC),
		}
		result := QuoteReply(src, "line1\nline2")
		if !strings.Contains(result, "> line1\n> line2") {
			t.Errorf("expected quoted lines, got:\n%s", result)
		}
	})

	t.Run("starts with two blank lines for cursor placement", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Name: "Bob", Email: "bob@example.com"},
			DateSent: time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC),
		}
		result := QuoteReply(src, "hello")
		if !strings.HasPrefix(result, "\n\n") {
			t.Errorf("expected result to start with two newlines, got:\n%q", result)
		}
	})

	t.Run("sender without Name uses email in header", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Name: "", Email: "noname@example.com"},
			DateSent: time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC),
		}
		result := QuoteReply(src, "body")
		if !strings.Contains(result, "noname@example.com <noname@example.com>") {
			t.Errorf("expected email used as name fallback, got:\n%s", result)
		}
	})

	t.Run("header contains date and wrote:", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Name: "Carol", Email: "carol@example.com"},
			DateSent: time.Date(2024, 3, 15, 9, 30, 0, 0, time.UTC),
		}
		result := QuoteReply(src, "test")
		if !strings.Contains(result, "wrote:") {
			t.Errorf("expected 'wrote:' in header, got:\n%s", result)
		}
		if !strings.Contains(result, "Mar 15, 2024") {
			t.Errorf("expected formatted date in header, got:\n%s", result)
		}
	})
}

func TestQuoteForward(t *testing.T) {
	t.Run("contains separator line", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Name: "Dave", Email: "dave@example.com"},
			Subject:  "Original subject",
			DateSent: time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC),
			To:       []mail.Address{{Email: "eve@example.com"}},
		}
		result := QuoteForward(src, "forwarded body")
		if !strings.Contains(result, "---------- Forwarded message ----------") {
			t.Errorf("expected separator, got:\n%s", result)
		}
	})

	t.Run("contains From and Subject meta", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Name: "Dave", Email: "dave@example.com"},
			Subject:  "My Subject",
			DateSent: time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC),
			To:       []mail.Address{{Email: "eve@example.com"}},
		}
		result := QuoteForward(src, "body")
		if !strings.Contains(result, "From: Dave <dave@example.com>") {
			t.Errorf("expected From line, got:\n%s", result)
		}
		if !strings.Contains(result, "Subject: My Subject") {
			t.Errorf("expected Subject line, got:\n%s", result)
		}
	})

	t.Run("sender without Name uses email only", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Name: "", Email: "noname@example.com"},
			Subject:  "Hi",
			DateSent: time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC),
			To:       []mail.Address{{Email: "to@example.com"}},
		}
		result := QuoteForward(src, "body")
		if !strings.Contains(result, "From: noname@example.com") {
			t.Errorf("expected bare email in From line, got:\n%s", result)
		}
	})

	t.Run("body content appears after meta", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Email: "a@b.com"},
			DateSent: time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC),
			To:       []mail.Address{{Email: "c@d.com"}},
		}
		result := QuoteForward(src, "the original content")
		if !strings.Contains(result, "the original content") {
			t.Errorf("expected body in forward, got:\n%s", result)
		}
	})
}
