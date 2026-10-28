package mock_test

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/5Sigma/pmx/internal/mock"
)

func TestMockServersStartAndClose(t *testing.T) {
	srv, err := mock.Start(mock.Options{
		IMAPAddr: "127.0.0.1:0",
		SMTPAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	// --- IMAP: connect, login, select INBOX, count seeded messages ---
	c, err := imapclient.DialInsecure(srv.IMAPAddr(), nil)
	if err != nil {
		t.Fatalf("imap dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Login("user", "pass").Wait(); err != nil {
		t.Fatalf("imap login: %v", err)
	}

	selectData, err := c.Select("INBOX", nil).Wait()
	if err != nil {
		t.Fatalf("imap select INBOX: %v", err)
	}
	if selectData.NumMessages == 0 {
		t.Fatal("expected seeded messages in INBOX, got 0")
	}
	initialCount := selectData.NumMessages
	t.Logf("INBOX has %d seeded messages", initialCount)

	// --- SMTP: send a message without auth (AllowInsecureAuth is set) ---
	smtpC, err := gosmtp.Dial(srv.SMTPAddr())
	if err != nil {
		t.Fatalf("smtp dial: %v", err)
	}
	defer func() { _ = smtpC.Close() }()

	body := "Subject: Test\r\nFrom: test@localhost\r\nTo: user@localhost\r\n\r\nHello from integration test.\r\n"
	if err := smtpC.SendMail("test@localhost", []string{"user@localhost"}, strings.NewReader(body)); err != nil {
		t.Fatalf("smtp send: %v", err)
	}

	// --- IMAP: second connection, verify message count increased ---
	c2, err := imapclient.DialInsecure(srv.IMAPAddr(), nil)
	if err != nil {
		t.Fatalf("imap dial2: %v", err)
	}
	defer func() { _ = c2.Close() }()

	if err := c2.Login("user", "pass").Wait(); err != nil {
		t.Fatalf("imap login2: %v", err)
	}

	selectData2, err := c2.Select("INBOX", nil).Wait()
	if err != nil {
		t.Fatalf("imap select2: %v", err)
	}
	if selectData2.NumMessages != initialCount+1 {
		t.Fatalf("expected %d messages after SMTP delivery, got %d", initialCount+1, selectData2.NumMessages)
	}

	// Fetch the last message and verify the envelope is readable.
	seqSet := imap.SeqSetNum(selectData2.NumMessages)
	msgs, err := c2.Fetch(seqSet, &imap.FetchOptions{Envelope: true}).Collect()
	if err != nil {
		t.Fatalf("imap fetch: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected fetched message, got none")
	}
	t.Logf("last message envelope: %+v", msgs[0].Envelope)
}

func TestArchiveMailboxSeeded(t *testing.T) {
	srv, err := mock.Start(mock.Options{
		IMAPAddr: "127.0.0.1:0",
		SMTPAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	c, err := imapclient.DialInsecure(srv.IMAPAddr(), nil)
	if err != nil {
		t.Fatalf("imap dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Login("user", "pass").Wait(); err != nil {
		t.Fatalf("imap login: %v", err)
	}

	selectData, err := c.Select("Archive", nil).Wait()
	if err != nil {
		t.Fatalf("imap select Archive: %v", err)
	}
	if selectData.NumMessages == 0 {
		t.Fatal("expected seeded message in Archive, got 0")
	}
	t.Logf("Archive has %d message(s)", selectData.NumMessages)
}
