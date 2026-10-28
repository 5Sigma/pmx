package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestMarshalUnmarshalToken(t *testing.T) {
	// time.Time round-trips through JSON at second precision because the
	// stdlib RFC 3339 format truncates sub-second components.
	expiry := time.Now().Add(1 * time.Hour).Truncate(time.Second)
	orig := &oauth2.Token{
		AccessToken:  "access-abc",
		RefreshToken: "refresh-xyz",
		TokenType:    "Bearer",
		Expiry:       expiry,
	}

	raw, err := MarshalToken(orig)
	if err != nil {
		t.Fatalf("MarshalToken() unexpected error: %v", err)
	}

	got, err := UnmarshalToken(raw)
	if err != nil {
		t.Fatalf("UnmarshalToken() unexpected error: %v", err)
	}

	if got.AccessToken != orig.AccessToken {
		t.Errorf("AccessToken: want %q, got %q", orig.AccessToken, got.AccessToken)
	}
	if got.RefreshToken != orig.RefreshToken {
		t.Errorf("RefreshToken: want %q, got %q", orig.RefreshToken, got.RefreshToken)
	}
	if got.TokenType != orig.TokenType {
		t.Errorf("TokenType: want %q, got %q", orig.TokenType, got.TokenType)
	}
	if !got.Expiry.Equal(orig.Expiry) {
		t.Errorf("Expiry: want %v, got %v", orig.Expiry, got.Expiry)
	}
}

func TestNewTokenSource_UsesStoredToken(t *testing.T) {
	// sentinel server — receiving any request means the implementation tried
	// to refresh a token that was not expired.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP request to token endpoint: %s %s", r.Method, r.URL)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	stored := &oauth2.Token{
		AccessToken:  "stored-access",
		RefreshToken: "stored-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour),
	}

	p := Provider{
		Endpoint: oauth2.Endpoint{
			AuthURL:  srv.URL + "/auth",
			TokenURL: srv.URL + "/token",
		},
		Scopes: []string{"mail"},
	}

	ts := NewTokenSource(context.Background(), p, "client-id", "", stored)
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token() unexpected error: %v", err)
	}
	if tok.AccessToken != "stored-access" {
		t.Errorf("AccessToken: want %q, got %q", "stored-access", tok.AccessToken)
	}
}

func TestNewTokenSource_RefreshesExpiredToken(t *testing.T) {
	// token response the fake authorization server will return.
	tokenResp := map[string]any{
		"access_token":  "newtoken",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": "newrefresh",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(tokenResp); err != nil {
			t.Errorf("encoding token response: %v", err)
		}
	}))
	defer srv.Close()

	// expired token: Expiry in the past, no usable access token.
	expired := &oauth2.Token{
		AccessToken:  "",
		RefreshToken: "old-refresh",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-1 * time.Hour),
	}

	p := Provider{
		Endpoint: oauth2.Endpoint{
			AuthURL:  srv.URL + "/auth",
			TokenURL: srv.URL + "/token",
		},
		Scopes: []string{"mail"},
	}

	ts := NewTokenSource(context.Background(), p, "client-id", "", expired)
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token() unexpected error: %v", err)
	}
	if tok.AccessToken != "newtoken" {
		t.Errorf("AccessToken: want %q, got %q", "newtoken", tok.AccessToken)
	}
}
