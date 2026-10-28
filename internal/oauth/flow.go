package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
)

// RunAuthCodeFlow performs the OAuth2 authorization code flow with PKCE using
// a loopback HTTP server to capture the redirect. It opens the system browser
// (or prints the URL if exec fails) and blocks until the user completes
// sign-in or ctx is cancelled.
func RunAuthCodeFlow(ctx context.Context, p Provider, clientID, clientSecret string) (*oauth2.Token, error) {
	// Bind an ephemeral port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("binding loopback port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     p.Endpoint,
		Scopes:       p.Scopes,
		RedirectURL:  redirectURL,
	}

	verifier := GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	authURL := cfg.AuthCodeURL(state,
		S256ChallengeOption(verifier),
		oauth2.AccessTypeOffline,
	)

	type result struct {
		tok *oauth2.Token
		err error
	}
	ch := make(chan result, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			ch <- result{err: errors.New("invalid OAuth state")}
			return
		}
		code := r.URL.Query().Get("code")
		tok, err := cfg.Exchange(ctx, code, VerifierOption(verifier))
		if err != nil {
			http.Error(w, "token exchange failed", http.StatusInternalServerError)
			ch <- result{err: fmt.Errorf("token exchange: %w", err)}
			return
		}
		_, _ = fmt.Fprint(w, "<html><body><p>Sign-in complete. You can close this tab and return to pmx.</p></body></html>")
		ch <- result{tok: tok}
	})

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case ch <- result{err: err}:
			default:
			}
		}
	}()

	fmt.Printf("Opening browser for sign-in...\n%s\n\n", authURL)
	_ = openBrowser(authURL)

	var res result
	select {
	case res = <-ch:
	case <-ctx.Done():
		res.err = ctx.Err()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	return res.tok, res.err
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "linux":
		cmd, args = "xdg-open", []string{url}
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported OS")
	}
	return exec.Command(cmd, args...).Start()
}
