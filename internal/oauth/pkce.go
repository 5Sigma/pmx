package oauth

import "golang.org/x/oauth2"

// GenerateVerifier returns a new cryptographically-random PKCE code verifier.
func GenerateVerifier() string {
	return oauth2.GenerateVerifier()
}

// S256ChallengeOption returns the oauth2.AuthCodeOption that sets the S256
// code_challenge from a given verifier. Pass the returned option to
// oauth2.Config.AuthCodeURL.
func S256ChallengeOption(verifier string) oauth2.AuthCodeOption {
	return oauth2.S256ChallengeOption(verifier)
}

// VerifierOption returns the oauth2.AuthCodeOption that sends the
// code_verifier during the token exchange. Pass to oauth2.Config.Exchange.
func VerifierOption(verifier string) oauth2.AuthCodeOption {
	return oauth2.VerifierOption(verifier)
}
