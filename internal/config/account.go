package config

import (
	"strings"

	"github.com/5Sigma/pmx/internal/mail"
)

// AccountForMessage picks the best account to use when replying to src.
// It checks each account's Email against the To and CC fields of src (case-insensitive).
// If no match, falls back to the DefaultAccount by Name, then the first account.
func (c *Config) AccountForMessage(src *mail.Message) Account {
	// Check all To+CC addresses against configured accounts.
	candidates := append(src.To, src.CC...)
	for _, addr := range candidates {
		lower := strings.ToLower(addr.Email)
		for _, acct := range c.Accounts {
			if strings.ToLower(acct.Email) == lower {
				return acct
			}
		}
	}
	// Fall back to default account.
	if c.General.DefaultAccount != "" {
		for _, acct := range c.Accounts {
			if acct.Name == c.General.DefaultAccount {
				return acct
			}
		}
	}
	// Fall back to first.
	if len(c.Accounts) > 0 {
		return c.Accounts[0]
	}
	return Account{}
}
