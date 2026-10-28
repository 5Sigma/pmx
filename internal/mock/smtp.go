package mock

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
)

// smtpBackend delivers incoming SMTP messages straight into the user's INBOX.
type smtpBackend struct {
	user *imapmemserver.User
	log  *log.Logger
}

func newSMTPBackend(user *imapmemserver.User, logger *log.Logger) *smtpBackend {
	return &smtpBackend{user: user, log: logger}
}

func (b *smtpBackend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &smtpSession{user: b.user, log: b.log}, nil
}

type smtpSession struct {
	user  *imapmemserver.User
	log   *log.Logger
	from  string
	rcpts []string
}

// AuthMechanisms and Auth implement gosmtp.AuthSession, advertising PLAIN auth
// and accepting any credentials — this is a mock server.
func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *smtpSession) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, username, password string) error {
		return nil
	}), nil
}

func (s *smtpSession) Mail(from string, _ *gosmtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *smtpSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.rcpts = append(s.rcpts, to)
	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	lr := literalReader{bytes.NewReader(b)}
	if _, err := s.user.Append("INBOX", lr, &imap.AppendOptions{
		Flags: []imap.Flag{},
		Time:  time.Now(),
	}); err != nil {
		return fmt.Errorf("imap append: %w", err)
	}
	s.log.Printf("smtp: delivered %s → %v (%d bytes)", s.from, s.rcpts, len(b))
	return nil
}

func (s *smtpSession) Reset() {
	s.from = ""
	s.rcpts = nil
}

func (s *smtpSession) Logout() error { return nil }
