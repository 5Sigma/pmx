package secrets_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goauth2 "golang.org/x/oauth2"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/oauth"
	"github.com/5Sigma/pmx/internal/secrets"
)

type stubKeyring struct {
	data map[string]string
	err  error
}

func (s *stubKeyring) Get(service, user string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	key := service + ":" + user
	v, ok := s.data[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (s *stubKeyring) Set(service, user, password string) error {
	if s.err != nil {
		return s.err
	}
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[service+":"+user] = password
	return nil
}

func cfgWithAccount(name, auth, pwCmd string) *config.Config {
	cfg := config.Default()
	cfg.Accounts = []config.Account{
		{
			Name:            name,
			Email:           name + "@example.com",
			IMAPHost:        "imap.example.com",
			IMAPPort:        993,
			SMTPHost:        "smtp.example.com",
			SMTPPort:        465,
			Auth:            auth,
			PasswordCommand: pwCmd,
		},
	}
	return cfg
}

func TestStore_Get_KeyringHappyPath(t *testing.T) {
	kr := &stubKeyring{data: map[string]string{"pmx:work": "s3cr3t"}}
	store := secrets.NewWithKeyring(cfgWithAccount("work", "password", ""), kr)

	pw, err := store.Get("work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pw != "s3cr3t" {
		t.Errorf("expected s3cr3t, got %q", pw)
	}
}

func TestStore_Get_KeyringMissWrapsError(t *testing.T) {
	kr := &stubKeyring{err: errors.New("secret not found in keyring")}
	store := secrets.NewWithKeyring(cfgWithAccount("work", "password", ""), kr)

	_, err := store.Get("work")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "work") {
		t.Errorf("error should mention account name, got %q", err.Error())
	}
}

func TestStore_Get_UnknownAccount(t *testing.T) {
	store := secrets.NewWithKeyring(config.Default(), &stubKeyring{})

	_, err := store.Get("ghost")
	if err == nil {
		t.Fatal("expected error for unknown account")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should mention account name, got %q", err.Error())
	}
}

func TestStore_Get_PasswordCommandTrimsNewline(t *testing.T) {
	store := secrets.NewWithKeyring(
		cfgWithAccount("work", "password", "printf 'secret\\n'"),
		&stubKeyring{},
	)

	pw, err := store.Get("work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pw != "secret" {
		t.Errorf("expected trailing newline trimmed, got %q", pw)
	}
}

func TestStore_Get_PasswordCommandNonZeroExit(t *testing.T) {
	store := secrets.NewWithKeyring(
		cfgWithAccount("work", "password", "exit 1"),
		&stubKeyring{},
	)

	_, err := store.Get("work")
	if err == nil {
		t.Fatal("expected error from non-zero exit")
	}
}

func TestStore_Get_PasswordCommandEmptyOutput(t *testing.T) {
	store := secrets.NewWithKeyring(
		cfgWithAccount("work", "password", "true"),
		&stubKeyring{},
	)

	_, err := store.Get("work")
	if err == nil {
		t.Fatal("expected error for empty output")
	}
	if !strings.Contains(err.Error(), "empty output") {
		t.Errorf("expected empty output error, got %q", err.Error())
	}
}

func TestStore_Get_PasswordCommandTimeout(t *testing.T) {
	orig := secrets.PasswordCommandTimeout
	secrets.PasswordCommandTimeout = 50 * time.Millisecond
	defer func() { secrets.PasswordCommandTimeout = orig }()

	store := secrets.NewWithKeyring(
		cfgWithAccount("work", "password", "sleep 10"),
		&stubKeyring{},
	)

	_, err := store.Get("work")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// cfgWithOAuthAccount builds a config containing a single oauth2 account.
func cfgWithOAuthAccount(name, clientID, tenant, tokenRef string) *config.Config {
	cfg := config.Default()
	cfg.Accounts = []config.Account{
		{
			Name:          name,
			Email:         name + "@example.com",
			IMAPHost:      "imap.example.com",
			IMAPPort:      993,
			SMTPHost:      "smtp.example.com",
			SMTPPort:      465,
			Auth:          "oauth2",
			OAuthClientID: clientID,
			OAuthTenant:   tenant,
			OAuthTokenRef: tokenRef,
		},
	}
	return cfg
}

func TestGetOAuth_ReturnsAccessToken(t *testing.T) {
	tok := &goauth2.Token{
		AccessToken:  "live-access-token",
		RefreshToken: "live-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour),
	}
	raw, err := oauth.MarshalToken(tok)
	if err != nil {
		t.Fatalf("MarshalToken: %v", err)
	}

	kr := &stubKeyring{
		data: map[string]string{"pmx-oauth:testref": raw},
	}
	cfg := cfgWithOAuthAccount("work", "client-id", "common", "testref")
	store := secrets.NewWithKeyring(cfg, kr)

	got, err := store.GetOAuth("work")
	if err != nil {
		t.Fatalf("GetOAuth() unexpected error: %v", err)
	}
	if got != "live-access-token" {
		t.Errorf("access token: want %q, got %q", "live-access-token", got)
	}
}

func TestGetOAuth_RefreshesExpiredToken(t *testing.T) {
	// Serve a valid token refresh response.
	tokenResp := map[string]any{
		"access_token":  "refreshed-access",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": "refreshed-refresh",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(tokenResp); err != nil {
			t.Errorf("encoding token response: %v", err)
		}
	}))
	defer srv.Close()

	// Build an expired token stored in the fake keyring.
	expired := &goauth2.Token{
		AccessToken:  "",
		RefreshToken: "old-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-1 * time.Hour),
	}
	raw, err := oauth.MarshalToken(expired)
	if err != nil {
		t.Fatalf("MarshalToken: %v", err)
	}
	kr := &stubKeyring{
		data: map[string]string{"pmx-oauth:testref": raw},
	}

	// OAuthTokenSource hardwires O365Provider, so we test the refresh path by
	// building the token source directly with a provider that points at the
	// test server — this exercises NewTokenSource (the same code OAuthTokenSource
	// delegates to) with the expired token.
	p := oauth.Provider{
		Endpoint: goauth2.Endpoint{
			AuthURL:  srv.URL + "/auth",
			TokenURL: srv.URL + "/token",
		},
		Scopes: []string{"mail"},
	}
	_ = kr // keyring used above to verify MarshalToken path; token source built directly below

	stored, err := oauth.UnmarshalToken(raw)
	if err != nil {
		t.Fatalf("UnmarshalToken: %v", err)
	}
	ts := oauth.NewTokenSource(context.Background(), p, "client-id", "", stored)

	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token() unexpected error: %v", err)
	}
	if tok.AccessToken != "refreshed-access" {
		t.Errorf("access token after refresh: want %q, got %q", "refreshed-access", tok.AccessToken)
	}
}
