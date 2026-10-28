package compose

import (
	"testing"
	"time"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
)

func TestBuildForward(t *testing.T) {
	acct := config.Account{Email: "me@example.com"}
	baseTime := time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)

	t.Run("subject has Fwd: prefix", func(t *testing.T) {
		src := &mail.Message{Subject: "Original", DateSent: baseTime}
		seed := BuildForward(src, acct, "body", nil)
		if seed.Subject != "Fwd: Original" {
			t.Errorf("Subject = %q, want %q", seed.Subject, "Fwd: Original")
		}
	})

	t.Run("input Re: prefix stripped before Fwd:", func(t *testing.T) {
		src := &mail.Message{Subject: "Re: Fwd: Old", DateSent: baseTime}
		seed := BuildForward(src, acct, "body", nil)
		if seed.Subject != "Fwd: Old" {
			t.Errorf("Subject = %q, want %q", seed.Subject, "Fwd: Old")
		}
	})

	t.Run("attachments mapped via attachPath", func(t *testing.T) {
		src := &mail.Message{
			DateSent: baseTime,
			Attachments: []mail.Attachment{
				{Filename: "report.pdf", MIMEType: "application/pdf", SizeBytes: 1024},
				{Filename: "image.png", MIMEType: "image/png", SizeBytes: 2048},
			},
		}
		attachPath := func(a mail.Attachment) string { return "/tmp/" + a.Filename }
		seed := BuildForward(src, acct, "body", attachPath)
		if len(seed.Attachments) != 2 {
			t.Fatalf("expected 2 attachments, got %d", len(seed.Attachments))
		}
		if seed.Attachments[0].Path != "/tmp/report.pdf" {
			t.Errorf("Attachments[0].Path = %q", seed.Attachments[0].Path)
		}
		if seed.Attachments[0].Filename != "report.pdf" {
			t.Errorf("Attachments[0].Filename = %q", seed.Attachments[0].Filename)
		}
		if seed.Attachments[0].Size != 1024 {
			t.Errorf("Attachments[0].Size = %d, want 1024", seed.Attachments[0].Size)
		}
		if seed.Attachments[1].MIMEType != "image/png" {
			t.Errorf("Attachments[1].MIMEType = %q", seed.Attachments[1].MIMEType)
		}
	})

	t.Run("nil attachPath produces nil or empty Attachments", func(t *testing.T) {
		src := &mail.Message{
			DateSent: baseTime,
			Attachments: []mail.Attachment{
				{Filename: "file.txt", SizeBytes: 100},
			},
		}
		seed := BuildForward(src, acct, "body", nil)
		if len(seed.Attachments) != 0 {
			t.Errorf("expected no attachments with nil attachPath, got %d", len(seed.Attachments))
		}
	})

	t.Run("attachPath returning empty string skips attachment", func(t *testing.T) {
		src := &mail.Message{
			DateSent: baseTime,
			Attachments: []mail.Attachment{
				{Filename: "missing.txt"},
				{Filename: "present.txt"},
			},
		}
		attachPath := func(a mail.Attachment) string {
			if a.Filename == "missing.txt" {
				return ""
			}
			return "/tmp/" + a.Filename
		}
		seed := BuildForward(src, acct, "body", attachPath)
		if len(seed.Attachments) != 1 {
			t.Fatalf("expected 1 attachment, got %d", len(seed.Attachments))
		}
		if seed.Attachments[0].Filename != "present.txt" {
			t.Errorf("expected present.txt, got %q", seed.Attachments[0].Filename)
		}
	})

	t.Run("InReplyTo set to src.MessageID", func(t *testing.T) {
		src := &mail.Message{MessageID: "fwd-parent-id", DateSent: baseTime}
		seed := BuildForward(src, acct, "body", nil)
		if seed.InReplyTo != "fwd-parent-id" {
			t.Errorf("InReplyTo = %q, want %q", seed.InReplyTo, "fwd-parent-id")
		}
	})

	t.Run("To is empty for a forward", func(t *testing.T) {
		src := &mail.Message{DateSent: baseTime}
		seed := BuildForward(src, acct, "body", nil)
		if len(seed.To) != 0 {
			t.Errorf("To = %v, want empty for a fresh forward", seed.To)
		}
	})
}
