package oauth

import "golang.org/x/oauth2"

// Provider holds OAuth2 endpoint URLs and required scopes for a mail provider.
type Provider struct {
	Endpoint oauth2.Endpoint
	Scopes   []string
}

// O365Provider returns the OAuth2 provider config for Office 365 Exchange Online.
// tenant is usually "common" for multi-tenant or a specific tenant ID/domain.
func O365Provider(tenant string) Provider {
	return Provider{
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/authorize",
			TokenURL: "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token",
		},
		Scopes: []string{
			"https://outlook.office365.com/IMAP.AccessAsUser.All",
			"https://outlook.office365.com/SMTP.Send",
			"offline_access",
		},
	}
}

// GmailProvider returns the OAuth2 provider config for Gmail.
func GmailProvider() Provider {
	return Provider{
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
		Scopes: []string{
			"https://mail.google.com/",
		},
	}
}
