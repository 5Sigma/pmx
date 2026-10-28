package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/oauth"
	"github.com/5Sigma/pmx/internal/secrets"
)

var accountCmd = &cobra.Command{
	Use:   "account",
	Short: "Manage pmx accounts",
}

var accountAddProvider string

var accountAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new mail account",
	RunE:  runAccountAdd,
}

func init() {
	accountAddCmd.Flags().StringVar(&accountAddProvider, "provider", "", "Mail provider (o365 or gmail)")
	_ = accountAddCmd.MarkFlagRequired("provider")
	accountCmd.AddCommand(accountAddCmd)
	rootCmd.AddCommand(accountCmd)
}

func runAccountAdd(cmd *cobra.Command, args []string) error {
	switch accountAddProvider {
	case "o365":
		return runAccountAddO365()
	case "gmail":
		return runAccountAddGmail()
	default:
		return fmt.Errorf("unsupported provider %q — supported providers: \"o365\", \"gmail\"", accountAddProvider)
	}
}

func runAccountAddO365() error {
	r := bufio.NewReader(os.Stdin)

	email, err := prompt(r, "Email address: ")
	if err != nil {
		return err
	}
	if email == "" {
		return fmt.Errorf("email address is required")
	}

	fullName, err := prompt(r, "Full name (for From header): ")
	if err != nil {
		return err
	}

	clientID, err := prompt(r, "Azure AD client ID: ")
	if err != nil {
		return err
	}
	if clientID == "" {
		return fmt.Errorf("client ID is required")
	}

	tenant, err := prompt(r, "Azure AD tenant [common]: ")
	if err != nil {
		return err
	}
	if tenant == "" {
		tenant = "common"
	}

	// Default account name to the local-part of the email address.
	localPart := email
	if idx := strings.Index(email, "@"); idx >= 0 {
		localPart = email[:idx]
	}
	accountName, err := promptDefault(r, fmt.Sprintf("Account name [%s]: ", localPart), localPart)
	if err != nil {
		return err
	}

	acct := config.Account{
		Name:          accountName,
		Email:         email,
		FullName:      fullName,
		Auth:          "oauth2",
		OAuthClientID: clientID,
		OAuthTenant:   tenant,
		OAuthTokenRef: accountName,
		// Office 365 connection settings — no prompting required.
		IMAPHost:     "outlook.office365.com",
		IMAPPort:     993,
		IMAPTLS:      true,
		SMTPHost:     "smtp.office365.com",
		SMTPPort:     587,
		SMTPStartTLS: true,
	}

	// Run the browser-based OAuth2 authorization code flow.
	ctx := context.Background()
	fmt.Fprintf(os.Stderr, "\nStarting browser sign-in for %s...\n", email)
	tok, err := oauth.RunAuthCodeFlow(ctx, oauth.O365Provider(tenant), clientID, "")
	if err != nil {
		return fmt.Errorf("OAuth sign-in failed: %w", err)
	}

	return finishAccountAdd(acct, accountName, tok)
}

func runAccountAddGmail() error {
	r := bufio.NewReader(os.Stdin)

	email, err := prompt(r, "Email address: ")
	if err != nil {
		return err
	}
	if email == "" {
		return fmt.Errorf("email address is required")
	}

	fullName, err := prompt(r, "Full name (for From header): ")
	if err != nil {
		return err
	}

	clientID, err := prompt(r, "Google OAuth client ID: ")
	if err != nil {
		return err
	}
	if clientID == "" {
		return fmt.Errorf("client ID is required")
	}

	clientSecret, err := prompt(r, "Google OAuth client secret: ")
	if err != nil {
		return err
	}
	if clientSecret == "" {
		return fmt.Errorf("client secret is required")
	}

	// Default account name to the local-part of the email address.
	localPart := email
	if idx := strings.Index(email, "@"); idx >= 0 {
		localPart = email[:idx]
	}
	accountName, err := promptDefault(r, fmt.Sprintf("Account name [%s]: ", localPart), localPart)
	if err != nil {
		return err
	}

	acct := config.Account{
		Name:              accountName,
		Email:             email,
		FullName:          fullName,
		Auth:              "oauth2",
		OAuthClientID:     clientID,
		OAuthClientSecret: clientSecret,
		OAuthTokenRef:     accountName,
		// Gmail connection settings — no prompting required.
		IMAPHost:     "imap.gmail.com",
		IMAPPort:     993,
		IMAPTLS:      true,
		SMTPHost:     "smtp.gmail.com",
		SMTPPort:     587,
		SMTPStartTLS: true,
	}

	// Run the browser-based OAuth2 authorization code flow.
	ctx := context.Background()
	fmt.Fprintf(os.Stderr, "\nStarting browser sign-in for %s...\n", email)
	tok, err := oauth.RunAuthCodeFlow(ctx, oauth.GmailProvider(), clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("OAuth sign-in failed: %w", err)
	}

	return finishAccountAdd(acct, accountName, tok)
}

func finishAccountAdd(acct config.Account, accountName string, tok *oauth2.Token) error {
	// Persist the token to the keyring. secrets.Store.SetOAuthToken requires the
	// account to already exist in Config.Accounts, so we build a minimal config
	// just for this call.
	tmpCfg := &config.Config{Accounts: []config.Account{acct}}
	sec := secrets.New(tmpCfg)
	if err := sec.SetOAuthToken(accountName, tok); err != nil {
		return fmt.Errorf("storing OAuth token: %w", err)
	}

	// Append the TOML block to the config file with rollback on validation failure.
	cfgPath, err := config.ConfigPath()
	if err != nil {
		return fmt.Errorf("resolving config path: %w", err)
	}

	// Ensure the config file exists (first-run scenario).
	if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
		defaults := config.Default()
		if saveErr := config.SaveTo(defaults, cfgPath); saveErr != nil {
			return fmt.Errorf("creating default config: %w", saveErr)
		}
	}

	f, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening config file: %w", err)
	}

	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat config file: %w", err)
	}
	originalSize := fi.Size()

	block := accountTOMLBlock(acct)
	if _, err := fmt.Fprint(f, block); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing account config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing config file: %w", err)
	}

	// Reload and validate; truncate back on failure.
	if _, loadErr := config.Load(); loadErr != nil {
		if truncErr := os.Truncate(cfgPath, originalSize); truncErr != nil {
			return fmt.Errorf("config validation failed (%w) and rollback failed: %v", loadErr, truncErr)
		}
		return fmt.Errorf("config validation failed (changes rolled back): %w", loadErr)
	}

	fmt.Fprintf(os.Stderr, "\nAccount %q added successfully.\nRun 'pmx sync' to start syncing.\n", accountName)
	return nil
}

// prompt prints a label to stderr and reads a trimmed line from r.
func prompt(r *bufio.Reader, label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// promptDefault is like prompt but returns defaultVal when the user presses Enter.
func promptDefault(r *bufio.Reader, label, defaultVal string) (string, error) {
	val, err := prompt(r, label)
	if err != nil {
		return "", err
	}
	if val == "" {
		return defaultVal, nil
	}
	return val, nil
}

// accountTOMLBlock renders an [[accounts]] TOML section for the given account.
// A blank line is prepended so the block reads cleanly when appended to an
// existing file.
func accountTOMLBlock(a config.Account) string {
	var b strings.Builder
	b.WriteString("\n[[accounts]]\n")
	writeStr(&b, "name", a.Name)
	writeStr(&b, "email", a.Email)
	writeStr(&b, "full_name", a.FullName)
	writeStr(&b, "auth", a.Auth)
	writeStr(&b, "oauth_client_id", a.OAuthClientID)
	if a.OAuthClientSecret != "" {
		writeStr(&b, "oauth_client_secret", a.OAuthClientSecret)
	}
	writeStr(&b, "oauth_tenant", a.OAuthTenant)
	writeStr(&b, "oauth_token_keyring_ref", a.OAuthTokenRef)
	writeStr(&b, "imap_host", a.IMAPHost)
	writef(&b, "imap_port = %d\n", a.IMAPPort)
	writef(&b, "imap_tls = %t\n", a.IMAPTLS)
	writeStr(&b, "smtp_host", a.SMTPHost)
	writef(&b, "smtp_port = %d\n", a.SMTPPort)
	writef(&b, "smtp_starttls = %t\n", a.SMTPStartTLS)
	return b.String()
}

func writeStr(b *strings.Builder, key, val string) {
	// %q produces a Go-quoted string, which is valid TOML basic-string syntax.
	fmt.Fprintf(b, "%s = %q\n", key, val)
}

func writef(b *strings.Builder, format string, args ...any) {
	fmt.Fprintf(b, format, args...)
}
