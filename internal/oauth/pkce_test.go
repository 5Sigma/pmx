package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"regexp"
	"testing"

	"golang.org/x/oauth2"
)

// urlSafeBase64 matches the unreserved base64url alphabet (no padding).
var urlSafeBase64 = regexp.MustCompile(`^[A-Za-z0-9\-._~]+$`)

func TestGenerateVerifier(t *testing.T) {
	v := GenerateVerifier()

	// RFC 7636 requires verifiers to be between 43 and 128 chars.
	if len(v) < 43 {
		t.Errorf("verifier length %d is below minimum 43", len(v))
	}
	if len(v) > 128 {
		t.Errorf("verifier length %d exceeds maximum 128", len(v))
	}

	// Verifier must use only URL-safe base64url characters (no padding '=').
	if !urlSafeBase64.MatchString(v) {
		t.Errorf("verifier %q contains characters outside URL-safe base64url alphabet", v)
	}

	// Two consecutive calls must not produce the same value.
	v2 := GenerateVerifier()
	if v == v2 {
		t.Error("GenerateVerifier() returned identical values on consecutive calls (not random)")
	}
}

func TestS256Challenge(t *testing.T) {
	verifier := GenerateVerifier()

	// Compute expected challenge: BASE64URL(SHA256(ASCII(verifier))).
	h := sha256.Sum256([]byte(verifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	// S256ChallengeOption is an AuthCodeOption; extract the code_challenge it
	// injects by calling AuthCodeURL and parsing the resulting query string.
	cfg := &oauth2.Config{
		ClientID:    "dummy",
		RedirectURL: "http://localhost",
		Endpoint: oauth2.Endpoint{
			AuthURL:  "http://example.com/auth",
			TokenURL: "http://example.com/token",
		},
	}
	authURL := cfg.AuthCodeURL("state", S256ChallengeOption(verifier))

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parsing AuthCodeURL output: %v", err)
	}
	gotChallenge := parsed.Query().Get("code_challenge")
	if gotChallenge == "" {
		t.Fatal("code_challenge parameter not found in AuthCodeURL output")
	}
	if gotChallenge != wantChallenge {
		t.Errorf("code_challenge mismatch:\n  want: %q\n   got: %q", wantChallenge, gotChallenge)
	}

	method := parsed.Query().Get("code_challenge_method")
	if method != "S256" {
		t.Errorf("code_challenge_method: want %q, got %q", "S256", method)
	}
}
