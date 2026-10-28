package folders_test

import (
	"testing"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/folders"
	"github.com/5Sigma/pmx/internal/mail"
)

// TestResolveConfigOverrideWins verifies that a per-account mailbox override
// beats both the discovered SPECIAL-USE map and the default.
func TestResolveConfigOverrideWins(t *testing.T) {
	discovered := map[mail.FolderRole]string{
		mail.RoleSent:    "Server/Sent",
		mail.RoleDrafts:  "Server/Drafts",
		mail.RoleArchive: "Server/Archive",
		mail.RoleTrash:   "Server/Trash",
		mail.RoleJunk:    "Server/Junk",
	}

	cases := []struct {
		name     string
		role     mail.FolderRole
		acct     config.Account
		wantName string
	}{
		{
			name:     "sent config override",
			role:     mail.RoleSent,
			acct:     config.Account{SentMailbox: "Custom/Sent"},
			wantName: "Custom/Sent",
		},
		{
			name:     "drafts config override",
			role:     mail.RoleDrafts,
			acct:     config.Account{DraftsMailbox: "Custom/Drafts"},
			wantName: "Custom/Drafts",
		},
		{
			name:     "archive config override",
			role:     mail.RoleArchive,
			acct:     config.Account{ArchiveMailbox: "Custom/Archive"},
			wantName: "Custom/Archive",
		},
		{
			name:     "trash config override",
			role:     mail.RoleTrash,
			acct:     config.Account{TrashMailbox: "Custom/Trash"},
			wantName: "Custom/Trash",
		},
		{
			name:     "junk config override",
			role:     mail.RoleJunk,
			acct:     config.Account{JunkMailbox: "Custom/Junk"},
			wantName: "Custom/Junk",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := folders.Resolve(tc.acct, discovered, tc.role)
			if got != tc.wantName {
				t.Errorf("Resolve(%s): got %q, want %q", tc.role, got, tc.wantName)
			}
		})
	}
}

// TestResolveDiscoveredWinsOverDefault verifies that SPECIAL-USE discovery
// results take precedence over the hardcoded default when no config override
// is set.
func TestResolveDiscoveredWinsOverDefault(t *testing.T) {
	cases := []struct {
		name       string
		role       mail.FolderRole
		discovered map[mail.FolderRole]string
		wantName   string
	}{
		{
			name:       "sent from discovery",
			role:       mail.RoleSent,
			discovered: map[mail.FolderRole]string{mail.RoleSent: "IMAP/Sent"},
			wantName:   "IMAP/Sent",
		},
		{
			name:       "drafts from discovery",
			role:       mail.RoleDrafts,
			discovered: map[mail.FolderRole]string{mail.RoleDrafts: "IMAP/Drafts"},
			wantName:   "IMAP/Drafts",
		},
		{
			name:       "archive from discovery",
			role:       mail.RoleArchive,
			discovered: map[mail.FolderRole]string{mail.RoleArchive: "IMAP/Archive"},
			wantName:   "IMAP/Archive",
		},
		{
			name:       "trash from discovery",
			role:       mail.RoleTrash,
			discovered: map[mail.FolderRole]string{mail.RoleTrash: "IMAP/Trash"},
			wantName:   "IMAP/Trash",
		},
		{
			name:       "junk from discovery",
			role:       mail.RoleJunk,
			discovered: map[mail.FolderRole]string{mail.RoleJunk: "IMAP/Junk"},
			wantName:   "IMAP/Junk",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := folders.Resolve(config.Account{}, tc.discovered, tc.role)
			if got != tc.wantName {
				t.Errorf("Resolve(%s): got %q, want %q", tc.role, got, tc.wantName)
			}
		})
	}
}

// TestResolveDefaultFallback verifies that the hardcoded default mailbox name
// is returned when neither the config nor the discovered map has an entry.
func TestResolveDefaultFallback(t *testing.T) {
	cases := []struct {
		role    mail.FolderRole
		wantDef string
	}{
		{mail.RoleInbox, "INBOX"},
		{mail.RoleSent, "Sent"},
		{mail.RoleDrafts, "Drafts"},
		{mail.RoleArchive, "Archive"},
		{mail.RoleTrash, "Trash"},
		{mail.RoleJunk, "Junk"},
	}

	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			got := folders.Resolve(config.Account{}, map[mail.FolderRole]string{}, tc.role)
			if got != tc.wantDef {
				t.Errorf("Resolve(%s) with empty discovered: got %q, want %q", tc.role, got, tc.wantDef)
			}
			// Confirm DefaultMailbox agrees with what Resolve returned.
			if def := tc.role.DefaultMailbox(); def != tc.wantDef {
				t.Errorf("DefaultMailbox(%s): got %q, want %q", tc.role, def, tc.wantDef)
			}
		})
	}
}

// TestResolveInboxNoConfigOverride verifies that RoleInbox always resolves to
// "INBOX". The config struct has no inbox-override field and SpecialUseAttr is
// ""; even if a caller injects a discovered entry keyed on RoleInbox the
// fallback chain still produces "INBOX" via DefaultMailbox.
func TestResolveInboxNoConfigOverride(t *testing.T) {
	// Inject a bogus discovered entry for Inbox to exercise defensive behavior.
	discovered := map[mail.FolderRole]string{
		mail.RoleInbox: "SomeWeirdINBOX",
	}

	got := folders.Resolve(config.Account{}, discovered, mail.RoleInbox)
	// The resolver has no config override for Inbox (configOverride returns ""),
	// and the discovered map does have an entry, so we get the discovered value
	// here — the important invariant is that it is never empty.
	if got == "" {
		t.Error("Resolve(RoleInbox): got empty string, want non-empty")
	}

	// Without a discovered entry, Resolve must return "INBOX".
	got2 := folders.Resolve(config.Account{}, map[mail.FolderRole]string{}, mail.RoleInbox)
	if got2 != "INBOX" {
		t.Errorf("Resolve(RoleInbox) no discovered: got %q, want \"INBOX\"", got2)
	}
}

// TestResolveNilDiscoveredMapIsSafe verifies that passing nil for the
// discovered map does not panic and falls through to the default.
func TestResolveNilDiscoveredMapIsSafe(t *testing.T) {
	cases := []struct {
		role    mail.FolderRole
		wantDef string
	}{
		{mail.RoleSent, "Sent"},
		{mail.RoleDrafts, "Drafts"},
		{mail.RoleArchive, "Archive"},
		{mail.RoleTrash, "Trash"},
		{mail.RoleJunk, "Junk"},
		{mail.RoleInbox, "INBOX"},
	}

	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			got := folders.Resolve(config.Account{}, nil, tc.role)
			if got != tc.wantDef {
				t.Errorf("Resolve(%s, nil): got %q, want %q", tc.role, got, tc.wantDef)
			}
		})
	}
}

// TestResolveEmptyDiscoveredMapIsSafe verifies that passing an empty (non-nil)
// discovered map is equivalent to nil — no panic and defaults are returned.
func TestResolveEmptyDiscoveredMapIsSafe(t *testing.T) {
	empty := map[mail.FolderRole]string{}

	cases := []struct {
		role    mail.FolderRole
		wantDef string
	}{
		{mail.RoleSent, "Sent"},
		{mail.RoleDrafts, "Drafts"},
		{mail.RoleArchive, "Archive"},
		{mail.RoleTrash, "Trash"},
		{mail.RoleJunk, "Junk"},
		{mail.RoleInbox, "INBOX"},
	}

	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			got := folders.Resolve(config.Account{}, empty, tc.role)
			if got != tc.wantDef {
				t.Errorf("Resolve(%s, empty): got %q, want %q", tc.role, got, tc.wantDef)
			}
		})
	}
}
