package config_test

import (
	"strings"
	"testing"

	"github.com/5Sigma/pmx/internal/config"
)

func validAccount(name string) config.Account {
	return config.Account{
		Name:     name,
		Email:    name + "@example.com",
		IMAPHost: "imap.example.com",
		IMAPPort: 993,
		SMTPHost: "smtp.example.com",
		SMTPPort: 465,
		Auth:     "password",
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "clean config with no accounts",
			cfg:  config.Default(),
		},
		{
			name: "clean config with valid accounts",
			cfg: func() *config.Config {
				c := config.Default()
				c.Accounts = []config.Account{validAccount("work"), validAccount("personal")}
				c.General.DefaultAccount = "work"
				return c
			}(),
		},
		{
			name:    "duplicate account names",
			wantErr: true,
			errMsg:  "duplicate name",
			cfg: func() *config.Config {
				c := config.Default()
				c.Accounts = []config.Account{validAccount("work"), validAccount("work")}
				return c
			}(),
		},
		{
			name:    "empty account name",
			wantErr: true,
			errMsg:  "name is required",
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("")
				a.Name = ""
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
		{
			name:    "default_account references unknown account",
			wantErr: true,
			errMsg:  "does not match any account name",
			cfg: func() *config.Config {
				c := config.Default()
				c.General.DefaultAccount = "ghost"
				return c
			}(),
		},
		{
			name:    "imap_port zero",
			wantErr: true,
			errMsg:  "imap_port 0 is out of range",
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("work")
				a.IMAPPort = 0
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
		{
			name:    "imap_port too large",
			wantErr: true,
			errMsg:  "imap_port 70000 is out of range",
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("work")
				a.IMAPPort = 70000
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
		{
			name:    "smtp_port zero",
			wantErr: true,
			errMsg:  "smtp_port 0 is out of range",
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("work")
				a.SMTPPort = 0
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
		{
			name:    "empty imap_host",
			wantErr: true,
			errMsg:  "imap_host is required",
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("work")
				a.IMAPHost = ""
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
		{
			name:    "bad auth value",
			wantErr: true,
			errMsg:  `auth must be "password" or "oauth2"`,
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("work")
				a.Auth = "basic"
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
		{
			name:    "oauth2 missing client_id",
			wantErr: true,
			errMsg:  "oauth_client_id",
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("work")
				a.Auth = "oauth2"
				a.OAuthClientID = ""
				a.OAuthTokenRef = "ref"
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
		{
			name:    "oauth2 missing token_ref",
			wantErr: true,
			errMsg:  "oauth_token_keyring_ref",
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("work")
				a.Auth = "oauth2"
				a.OAuthClientID = "client-id"
				a.OAuthTokenRef = ""
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
		{
			name: "oauth2 valid",
			cfg: func() *config.Config {
				c := config.Default()
				a := validAccount("work")
				a.Auth = "oauth2"
				a.OAuthClientID = "client-id"
				a.OAuthTokenRef = "ref"
				c.Accounts = []config.Account{a}
				return c
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
