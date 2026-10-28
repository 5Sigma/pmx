package config

import "time"

type Config struct {
	General  General   `koanf:"general"`
	Daemon   Daemon    `koanf:"daemon"`
	Theme    Theme     `koanf:"theme"`
	Features Features  `koanf:"features"`
	Keys     Keys      `koanf:"keys"`
	Accounts []Account `koanf:"accounts"`
}

type Daemon struct {
	CheckInterval        time.Duration `koanf:"check_interval"`
	NotificationsEnabled bool          `koanf:"notifications_enabled"`
	NotifyFolders        []string      `koanf:"notify_folders"`
	IDLEEnabled          *bool         `koanf:"idle_enabled"`
}

type General struct {
	DefaultAccount  string        `koanf:"default_account"`
	LogLevel        string        `koanf:"log_level"`
	PollInterval    time.Duration `koanf:"poll_interval"`
	SweepInterval   time.Duration `koanf:"sweep_interval"`
	SendInterval    time.Duration `koanf:"send_interval"`
	SendMaxAttempts int           `koanf:"send_max_attempts"`
}

type Theme struct {
	Primary     string `koanf:"primary"`
	Accent      string `koanf:"accent"`
	Muted       string `koanf:"muted"`
	Fg          string `koanf:"fg"`
	DimFg       string `koanf:"dim_fg"`
	Error       string `koanf:"error"`
	Warning     string `koanf:"warning"`
	Success     string `koanf:"success"`
	SelectionBg string `koanf:"selection_bg"`
	SelectionFg string `koanf:"selection_fg"`
	UnreadFg    string `koanf:"unread_fg"`
}

type Features struct {
	AltScreen     bool `koanf:"alt_screen"`
	Mouse         bool `koanf:"mouse"`
	FilterEnabled bool `koanf:"filter_enabled"`
	ShowStatusBar bool `koanf:"show_status_bar"`
	ConfirmQuit   bool `koanf:"confirm_quit"`
}

type Keys struct {
	Quit       []string `koanf:"quit"`
	Open       []string `koanf:"open"`
	Back       []string `koanf:"back"`
	ScrollUp   []string `koanf:"scroll_up"`
	ScrollDown []string `koanf:"scroll_down"`
	Filter     []string `koanf:"filter"`
	Refresh    []string `koanf:"refresh"`
}

type Account struct {
	Name              string `koanf:"name"`
	FullName          string `koanf:"full_name"`
	Email             string `koanf:"email"`
	IMAPHost          string `koanf:"imap_host"`
	IMAPPort          int    `koanf:"imap_port"`
	IMAPTLS           bool   `koanf:"imap_tls"`
	IMAPTLSSkipVerify bool   `koanf:"imap_tls_skip_verify"`
	IDLEEnabled       *bool  `koanf:"idle_enabled"`
	SMTPHost          string `koanf:"smtp_host"`
	SMTPPort          int    `koanf:"smtp_port"`
	SMTPTLS           bool   `koanf:"smtp_tls"`
	SMTPStartTLS      bool   `koanf:"smtp_starttls"`
	SMTPTLSSkipVerify bool   `koanf:"smtp_tls_skip_verify"`
	SentMailbox       string `koanf:"sent_mailbox"`
	TrashMailbox      string `koanf:"trash_mailbox"`
	DraftsMailbox     string `koanf:"drafts_mailbox"`
	ArchiveMailbox    string `koanf:"archive_mailbox"`
	JunkMailbox       string `koanf:"junk_mailbox"`
	Auth              string `koanf:"auth"`
	PasswordCommand   string `koanf:"password_command"`
	OAuthClientID     string `koanf:"oauth_client_id"`
	OAuthClientSecret string `koanf:"oauth_client_secret"`
	OAuthTenant       string `koanf:"oauth_tenant"`
	OAuthTokenRef     string `koanf:"oauth_token_keyring_ref"`
}
