package compose

import (
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
)

var testTime = time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)

func TestBuildReply(t *testing.T) {
	acct := config.Account{Email: "me@example.com"}

	t.Run("subject has Re: prefix", func(t *testing.T) {
		src := &mail.Message{Subject: "Hello", From: mail.Address{Email: "sender@x.com"}, DateSent: testTime}
		seed := BuildReply(src, acct, "body")
		if seed.Subject != "Re: Hello" {
			t.Errorf("Subject = %q, want %q", seed.Subject, "Re: Hello")
		}
	})

	t.Run("double Re: stripped before adding prefix", func(t *testing.T) {
		src := &mail.Message{Subject: "Re: Re: Hello", From: mail.Address{Email: "sender@x.com"}, DateSent: testTime}
		seed := BuildReply(src, acct, "body")
		if seed.Subject != "Re: Hello" {
			t.Errorf("Subject = %q, want %q", seed.Subject, "Re: Hello")
		}
	})

	t.Run("To is src.From", func(t *testing.T) {
		src := &mail.Message{
			Subject:  "Hi",
			From:     mail.Address{Name: "Alice", Email: "alice@x.com"},
			DateSent: testTime,
		}
		seed := BuildReply(src, acct, "body")
		if len(seed.To) != 1 || seed.To[0].Email != "alice@x.com" {
			t.Errorf("To = %v, want [{alice@x.com}]", seed.To)
		}
	})

	t.Run("InReplyTo set to src.MessageID", func(t *testing.T) {
		src := &mail.Message{MessageID: "abc123", From: mail.Address{Email: "a@b.com"}, DateSent: testTime}
		seed := BuildReply(src, acct, "")
		if seed.InReplyTo != "abc123" {
			t.Errorf("InReplyTo = %q, want %q", seed.InReplyTo, "abc123")
		}
	})

	t.Run("References = [src.MessageID] when src.References empty", func(t *testing.T) {
		src := &mail.Message{MessageID: "msg1", From: mail.Address{Email: "a@b.com"}, DateSent: testTime}
		seed := BuildReply(src, acct, "")
		if len(seed.References) != 1 || seed.References[0] != "msg1" {
			t.Errorf("References = %v, want [msg1]", seed.References)
		}
	})

	t.Run("References deduped when src.MessageID already in src.References", func(t *testing.T) {
		src := &mail.Message{
			MessageID:  "msg2",
			References: []string{"msg1", "msg2"},
			From:       mail.Address{Email: "a@b.com"},
			DateSent:   testTime,
		}
		seed := BuildReply(src, acct, "")
		for i, r := range seed.References {
			for j, s := range seed.References {
				if i != j && r == s {
					t.Errorf("References contains duplicate %q: %v", r, seed.References)
				}
			}
		}
		// msg2 should appear exactly once
		count := 0
		for _, r := range seed.References {
			if r == "msg2" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected msg2 once in References, got %d: %v", count, seed.References)
		}
	})

	t.Run("From set to account email", func(t *testing.T) {
		src := &mail.Message{Subject: "Hi", From: mail.Address{Email: "a@b.com"}, DateSent: testTime}
		seed := BuildReply(src, acct, "body")
		if seed.From != "me@example.com" {
			t.Errorf("From = %q, want %q", seed.From, "me@example.com")
		}
	})
}

func TestBuildReplyAll(t *testing.T) {
	acct := config.Account{Email: "me@example.com"}

	t.Run("To is src.From", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Email: "sender@x.com"},
			To:       []mail.Address{{Email: "me@example.com"}, {Email: "other@x.com"}},
			DateSent: testTime,
		}
		seed := BuildReplyAll(src, acct, "body")
		if len(seed.To) != 1 || seed.To[0].Email != "sender@x.com" {
			t.Errorf("To = %v, want [sender@x.com]", seed.To)
		}
	})

	t.Run("self excluded from Cc", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Email: "sender@x.com"},
			To:       []mail.Address{{Email: "me@example.com"}, {Email: "other@x.com"}},
			DateSent: testTime,
		}
		seed := BuildReplyAll(src, acct, "body")
		for _, a := range seed.Cc {
			if a.Email == "me@example.com" {
				t.Error("self should be excluded from Cc")
			}
		}
	})

	t.Run("src.From excluded from Cc", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Email: "sender@x.com"},
			To:       []mail.Address{{Email: "other@x.com"}},
			CC:       []mail.Address{{Email: "sender@x.com"}},
			DateSent: testTime,
		}
		seed := BuildReplyAll(src, acct, "body")
		for _, a := range seed.Cc {
			if a.Email == "sender@x.com" {
				t.Error("src.From should be excluded from Cc")
			}
		}
	})

	t.Run("case-insensitive self match", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Email: "sender@x.com"},
			To:       []mail.Address{{Email: "ME@EXAMPLE.COM"}, {Email: "other@x.com"}},
			DateSent: testTime,
		}
		seed := BuildReplyAll(src, acct, "body")
		for _, a := range seed.Cc {
			if a.Email == "ME@EXAMPLE.COM" {
				t.Error("case-insensitive self match should exclude ME@EXAMPLE.COM from Cc")
			}
		}
	})

	t.Run("empty src.From email → To is empty slice not nil", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Email: ""},
			To:       []mail.Address{{Email: "other@x.com"}},
			DateSent: testTime,
		}
		seed := BuildReplyAll(src, acct, "body")
		if len(seed.To) != 0 {
			t.Errorf("To = %v, want empty/nil when From.Email is empty", seed.To)
		}
	})

	t.Run("Cc contains src.To minus self", func(t *testing.T) {
		src := &mail.Message{
			From:     mail.Address{Email: "sender@x.com"},
			To:       []mail.Address{{Email: "me@example.com"}, {Email: "colleague@x.com"}},
			DateSent: testTime,
		}
		seed := BuildReplyAll(src, acct, "body")
		found := false
		for _, a := range seed.Cc {
			if a.Email == "colleague@x.com" {
				found = true
			}
		}
		if !found {
			t.Errorf("Cc = %v, want to contain colleague@x.com", seed.Cc)
		}
	})
}
