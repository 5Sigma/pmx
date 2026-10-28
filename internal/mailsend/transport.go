package mailsend

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/oauth"
)

// sendSMTP transmits body via SMTP using acct's connection settings.
// from and to are the SMTP envelope addresses (may differ from message headers for Bcc).
func sendSMTP(ctx context.Context, acct config.Account, password, from string, to []string, body []byte) error {
	addr := fmt.Sprintf("%s:%d", acct.SMTPHost, acct.SMTPPort)

	var tlsCfg *tls.Config
	if acct.SMTPTLSSkipVerify {
		tlsCfg = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	var (
		c   *gosmtp.Client
		err error
	)
	switch {
	case acct.SMTPTLS:
		c, err = gosmtp.DialTLS(addr, tlsCfg)
	case acct.SMTPStartTLS:
		c, err = gosmtp.DialStartTLS(addr, tlsCfg)
	default:
		c, err = gosmtp.Dial(addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()

	// Context cancellation watchdog — mirrors mailsync/engine.go:224-228.
	go func() {
		<-ctx.Done()
		_ = c.Close()
	}()

	var auth sasl.Client
	switch acct.Auth {
	case "password":
		auth = sasl.NewPlainClient("", acct.Email, password)
	case "oauth2":
		auth = &oauth.XOAuth2Client{User: acct.Email, Token: password}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	return c.SendMail(from, to, bytes.NewReader(body))
}
