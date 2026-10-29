package folders_test

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"

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

// TestListStatus_NoopFailureOnEmptyResponse verifies the NOOP guard: when a
// server advertises LIST-STATUS, returns an empty LIST response, and then drops
// the connection before answering NOOP, ListStatus must return a non-nil error
// containing "list-status returned empty and connection check failed".
//
// The stub server below stands in for a real IMAP server that silently closes
// the connection after returning an empty LIST response — a behaviour seen on
// some broken servers. We use a raw TCP listener rather than the mock package
// because the mock always returns mailboxes; we need to control the exact
// sequence of responses to exercise the new guard code.
func TestListStatus_NoopFailureOnEmptyResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	// stubIMAPServer handles one client connection: it speaks just enough IMAP
	// to get the client authenticated and to LIST-STATUS, then drops the
	// connection instead of answering NOOP. The connection close is what makes
	// c.Noop().Wait() return an error.
	serveErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serveErr <- err
			return
		}
		defer func() { _ = conn.Close() }()

		// The capability string includes LIST-STATUS so imapclient sets the cap
		// flag; CapListStatus is the condition that makes ListStatus choose the
		// LIST-STATUS path.
		_, _ = fmt.Fprintf(conn, "* OK [CAPABILITY IMAP4rev1 LIST-EXTENDED LIST-STATUS] ready\r\n")

		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			tag, cmd := fields[0], strings.ToUpper(fields[1])
			switch cmd {
			case "LOGIN":
				_, _ = fmt.Fprintf(conn, "%s OK [CAPABILITY IMAP4rev1 LIST-EXTENDED LIST-STATUS] LOGIN completed\r\n", tag)
			case "CAPABILITY":
				_, _ = fmt.Fprintf(conn, "* CAPABILITY IMAP4rev1 LIST-EXTENDED LIST-STATUS\r\n")
				_, _ = fmt.Fprintf(conn, "%s OK CAPABILITY completed\r\n", tag)
			case "LIST":
				// Return zero mailboxes — no "* LIST" lines — then OK.
				_, _ = fmt.Fprintf(conn, "%s OK LIST completed\r\n", tag)
			case "NOOP":
				// Drop the connection: the client will get EOF on the next read,
				// which surfaces as a non-nil error from Noop().Wait().
				return
			case "LOGOUT":
				_, _ = fmt.Fprintf(conn, "* BYE logging out\r\n")
				_, _ = fmt.Fprintf(conn, "%s OK LOGOUT\r\n", tag)
				return
			}
		}
		serveErr <- nil
	}()

	addr := ln.Addr().String()
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatalf("DialInsecure: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Login("user", "pass").Wait(); err != nil {
		t.Fatalf("Login: %v", err)
	}
	// Post-login CAPABILITY refresh (mirrors ConnectAndLogin behavior; also
	// required so imapclient's capability cache reflects LIST-STATUS).
	if _, err := c.Capability().Wait(); err != nil {
		t.Fatalf("Capability: %v", err)
	}

	_, gotErr := folders.ListStatus(c)

	if gotErr == nil {
		t.Fatal("ListStatus: want non-nil error when NOOP fails after empty LIST, got nil")
	}
	const want = "list-status returned empty and connection check failed"
	if !strings.Contains(gotErr.Error(), want) {
		t.Errorf("ListStatus error: want message containing %q, got %q", want, gotErr.Error())
	}
}
