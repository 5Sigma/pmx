package secrets

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/oauth"
)

var ErrNotImplemented = errors.New("not implemented")

// KeyringClient is a testable abstraction over go-keyring.
type KeyringClient interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
}

// Store resolves credentials for named accounts.
type Store struct {
	cfg     *config.Config
	keyring KeyringClient
}

const keyringService = "pmx"

func New(cfg *config.Config) *Store {
	return &Store{cfg: cfg, keyring: realKeyring{}}
}

// NewWithKeyring injects a stub keyring for tests.
func NewWithKeyring(cfg *config.Config, k KeyringClient) *Store {
	return &Store{cfg: cfg, keyring: k}
}

// Get returns the plaintext password for the named account.
// Resolution order: password_command (if set), keyring.
func (s *Store) Get(accountName string) (string, error) {
	acct, ok := s.account(accountName)
	if !ok {
		return "", fmt.Errorf("unknown account %q", accountName)
	}

	if acct.Auth == "oauth2" {
		return s.GetOAuth(accountName)
	}

	if acct.PasswordCommand != "" {
		return runPasswordCommand(acct.PasswordCommand)
	}

	pw, err := s.keyring.Get(keyringService, accountName)
	if err != nil {
		return "", fmt.Errorf("keyring lookup for account %q: %w", accountName, err)
	}
	return pw, nil
}

// GetOAuth resolves a fresh OAuth2 access token for the given account name.
// It loads the stored token from the keyring, refreshes it if expired, persists
// any rotated refresh token back to the keyring, and returns the access token.
func (s *Store) GetOAuth(accountName string) (string, error) {
	ts, err := s.OAuthTokenSource(accountName)
	if err != nil {
		return "", err
	}
	tok, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("refreshing OAuth token for %q: %w — re-run `pmx account add --provider o365` to re-authenticate", accountName, err)
	}
	// Persist a rotated refresh token.
	if err := s.persistRefreshToken(accountName, tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// GetFresh is like Get but forces an OAuth2 refresh grant regardless of whether
// the stored access token is still within its expiry window. Use this for SMTP
// sends where a server-side-revoked token (e.g. after a session invalidation)
// would otherwise be served from cache until the local Expiry timestamp passes.
// For non-OAuth2 accounts it behaves identically to Get.
func (s *Store) GetFresh(accountName string) (string, error) {
	acct, ok := s.account(accountName)
	if !ok {
		return "", fmt.Errorf("unknown account %q", accountName)
	}
	if acct.Auth != "oauth2" {
		return s.Get(accountName)
	}

	raw, err := s.keyring.Get("pmx-oauth", acct.OAuthTokenRef)
	if err != nil {
		return "", fmt.Errorf("reading OAuth token for %q: %w", accountName, err)
	}
	stored, err := oauth.UnmarshalToken(raw)
	if err != nil {
		return "", fmt.Errorf("parsing stored OAuth token for %q: %w", accountName, err)
	}

	var p oauth.Provider
	if acct.IMAPHost == "imap.gmail.com" {
		p = oauth.GmailProvider()
	} else {
		p = oauth.O365Provider(acct.OAuthTenant)
	}

	// Clear the access token so ReuseTokenSource sees an invalid token and
	// is forced to perform a refresh grant rather than returning the cached one.
	refresh := &oauth2.Token{RefreshToken: stored.RefreshToken}
	ts := oauth.NewTokenSource(context.Background(), p, acct.OAuthClientID, acct.OAuthClientSecret, refresh)
	tok, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("force-refreshing OAuth token for %q: %w — re-run `pmx account add --provider o365` to re-authenticate", accountName, err)
	}
	if err := s.persistRefreshToken(accountName, tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// OAuthTokenSource builds an oauth2.TokenSource that auto-refreshes for the
// given account. Callers that hold long-lived IMAP sessions (e.g. IDLE watcher)
// should hold the TokenSource and call Token() on each reconnect.
func (s *Store) OAuthTokenSource(accountName string) (oauth2.TokenSource, error) {
	acct := s.cfg.AccountByName(accountName)
	if acct == nil {
		return nil, fmt.Errorf("account %q not found", accountName)
	}
	raw, err := s.keyring.Get("pmx-oauth", acct.OAuthTokenRef)
	if err != nil {
		return nil, fmt.Errorf("reading OAuth token for %q: %w — re-run `pmx account add --provider o365` to authenticate", accountName, err)
	}
	stored, err := oauth.UnmarshalToken(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing stored OAuth token for %q: %w", accountName, err)
	}
	var p oauth.Provider
	if acct.IMAPHost == "imap.gmail.com" {
		p = oauth.GmailProvider()
	} else {
		p = oauth.O365Provider(acct.OAuthTenant)
	}
	ctx := context.Background()
	return oauth.NewTokenSource(ctx, p, acct.OAuthClientID, acct.OAuthClientSecret, stored), nil
}

// SetOAuthToken persists a new OAuth token (from initial sign-in) to the keyring.
func (s *Store) SetOAuthToken(accountName string, tok *oauth2.Token) error {
	acct := s.cfg.AccountByName(accountName)
	if acct == nil {
		return fmt.Errorf("account %q not found", accountName)
	}
	raw, err := oauth.MarshalToken(tok)
	if err != nil {
		return err
	}
	return s.keyring.Set("pmx-oauth", acct.OAuthTokenRef, raw)
}

func (s *Store) persistRefreshToken(accountName string, tok *oauth2.Token) error {
	if tok.RefreshToken == "" {
		return nil
	}
	acct := s.cfg.AccountByName(accountName)
	if acct == nil {
		return nil
	}
	raw, err := oauth.MarshalToken(tok)
	if err != nil {
		return err
	}
	return s.keyring.Set("pmx-oauth", acct.OAuthTokenRef, raw)
}

func (s *Store) account(name string) (config.Account, bool) {
	for _, a := range s.cfg.Accounts {
		if a.Name == name {
			return a, true
		}
	}
	return config.Account{}, false
}
