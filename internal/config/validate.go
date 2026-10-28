package config

import (
	"errors"
	"fmt"
	"time"
)

func (c *Config) Validate() error {
	var errs []error

	if c.General.PollInterval > 0 && c.General.PollInterval < 10*time.Second {
		errs = append(errs, fmt.Errorf("general.poll_interval must be at least 10s, got %s", c.General.PollInterval))
	}
	if c.General.SweepInterval > 0 && c.General.SweepInterval < time.Minute {
		errs = append(errs, fmt.Errorf("general.sweep_interval must be at least 1m, got %s", c.General.SweepInterval))
	}
	if c.General.SendInterval > 0 && c.General.SendInterval < 10*time.Second {
		errs = append(errs, fmt.Errorf("general.send_interval must be at least 10s, got %s", c.General.SendInterval))
	}
	if c.General.SendMaxAttempts < 1 {
		errs = append(errs, fmt.Errorf("general.send_max_attempts must be at least 1"))
	}

	seen := map[string]bool{}
	for i, a := range c.Accounts {
		if a.Name == "" {
			errs = append(errs, fmt.Errorf("accounts[%d]: name is required", i))
		} else if seen[a.Name] {
			errs = append(errs, fmt.Errorf("accounts[%d]: duplicate name %q", i, a.Name))
		}
		seen[a.Name] = true

		if a.IMAPHost == "" {
			errs = append(errs, fmt.Errorf("account %q: imap_host is required", a.Name))
		}
		if a.SMTPHost == "" {
			errs = append(errs, fmt.Errorf("account %q: smtp_host is required", a.Name))
		}
		if a.SMTPTLS && a.SMTPStartTLS {
			errs = append(errs, fmt.Errorf("account %q: smtp_tls and smtp_starttls are mutually exclusive", a.Name))
		}
		if a.IMAPPort < 1 || a.IMAPPort > 65535 {
			errs = append(errs, fmt.Errorf("account %q: imap_port %d is out of range", a.Name, a.IMAPPort))
		}
		if a.SMTPPort < 1 || a.SMTPPort > 65535 {
			errs = append(errs, fmt.Errorf("account %q: smtp_port %d is out of range", a.Name, a.SMTPPort))
		}
		if a.Auth != "password" && a.Auth != "oauth2" {
			errs = append(errs, fmt.Errorf("account %q: auth must be \"password\" or \"oauth2\", got %q", a.Name, a.Auth))
		}
		if a.Auth == "oauth2" {
			if a.OAuthClientID == "" {
				errs = append(errs, fmt.Errorf("account %q: oauth_client_id is required when auth = \"oauth2\"", a.Name))
			}
			if a.OAuthTokenRef == "" {
				errs = append(errs, fmt.Errorf("account %q: oauth_token_keyring_ref is required when auth = \"oauth2\"", a.Name))
			}
			if a.IMAPHost == "imap.gmail.com" && a.OAuthClientSecret == "" {
				errs = append(errs, fmt.Errorf("account %q: oauth_client_secret is required for Gmail accounts", a.Name))
			}
		}
	}

	if c.General.DefaultAccount != "" && !seen[c.General.DefaultAccount] {
		errs = append(errs, fmt.Errorf("general.default_account %q does not match any account name", c.General.DefaultAccount))
	}

	return errors.Join(errs...)
}
