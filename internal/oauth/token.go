package oauth

import (
	"context"
	"encoding/json"
	"time"

	"golang.org/x/oauth2"
)

// TokenRecord is the subset of oauth2.Token fields we persist in the keyring.
type TokenRecord struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
}

// MarshalToken serialises an oauth2.Token for keyring storage.
func MarshalToken(t *oauth2.Token) (string, error) {
	r := TokenRecord{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		TokenType:    t.TokenType,
		Expiry:       t.Expiry,
	}
	b, err := json.Marshal(r)
	return string(b), err
}

// UnmarshalToken deserialises a keyring value into an oauth2.Token.
func UnmarshalToken(s string) (*oauth2.Token, error) {
	var r TokenRecord
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return nil, err
	}
	return &oauth2.Token{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		TokenType:    r.TokenType,
		Expiry:       r.Expiry,
	}, nil
}

// NewTokenSource builds a refreshing token source from a stored token.
// It wraps oauth2.ReuseTokenSource so Token() only fetches a new access token
// when the current one is within the expiry window.
func NewTokenSource(ctx context.Context, p Provider, clientID, clientSecret string, stored *oauth2.Token) oauth2.TokenSource {
	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     p.Endpoint,
		Scopes:       p.Scopes,
	}
	return oauth2.ReuseTokenSource(stored, cfg.TokenSource(ctx, stored))
}
