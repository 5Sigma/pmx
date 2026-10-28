// Package imapauth provides a shared IMAP connect+login primitive used by
// both the sync and send workers. Both workers need to open an authenticated
// IMAP session; keeping the connection/auth code in one place removes the
// drift risk of two parallel implementations.
package imapauth

import (
	"crypto/tls"
	"fmt"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/oauth"
)

// Dial opens a raw IMAP connection (TLS or plaintext) without authenticating.
// h may be nil; if non-nil it is installed as the unilateral-data handler
// (required for IDLE).
func Dial(acct config.Account, h *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", acct.IMAPHost, acct.IMAPPort)
	opts := &imapclient.Options{}
	if h != nil {
		opts.UnilateralDataHandler = h
	}
	if acct.IMAPTLS {
		if acct.IMAPTLSSkipVerify {
			opts.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec — intentional for self-signed certs (e.g. local mail bridge)
		}
		return imapclient.DialTLS(addr, opts)
	}
	return imapclient.DialInsecure(addr, opts)
}

// ConnectAndLogin dials and authenticates in one step.
// token is either a plaintext password (acct.Auth="password" or unset) or an
// OAuth2 access token (acct.Auth="oauth2"). h may be nil.
func ConnectAndLogin(acct config.Account, token string, h *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	c, err := Dial(acct, h)
	if err != nil {
		return nil, fmt.Errorf("imap dial %s: %w", acct.IMAPHost, err)
	}
	switch acct.Auth {
	case "oauth2":
		if err := c.Authenticate(&oauth.XOAuth2Client{User: acct.Email, Token: token}); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("imap xoauth2 authenticate %s: %w", acct.Email, err)
		}
	default: // "password" and unset
		if err := c.Login(acct.Email, token).Wait(); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("imap login %s: %w", acct.Email, err)
		}
	}
	// Refresh capabilities after login. Servers (including Proton Bridge) may
	// advertise a different set post-login (e.g. IDLE) than in the greeting.
	if _, err := c.Capability().Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("imap capability %s: %w", acct.IMAPHost, err)
	}
	return c, nil
}
