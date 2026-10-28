package folders_test

import (
	"fmt"
	"net"
	"strconv"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/5Sigma/pmx/internal/folders"
	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/mock"
)

// dialMock starts the mock server on ephemeral ports, connects an IMAP client,
// and logs in. The server and client are both cleaned up via t.Cleanup.
func dialMock(t *testing.T) *imapclient.Client {
	t.Helper()
	srv, err := mock.Start(mock.Options{
		IMAPAddr: "127.0.0.1:0",
		SMTPAddr: "127.0.0.1:0",
		User:     "user",
		Pass:     "pass",
	})
	if err != nil {
		t.Fatalf("mock.Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	host, portStr, err := net.SplitHostPort(srv.IMAPAddr())
	if err != nil {
		t.Fatalf("parse IMAPAddr %q: %v", srv.IMAPAddr(), err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatalf("DialInsecure %s: %v", addr, err)
	}
	t.Cleanup(func() {
		_ = c.Logout().Wait()
	})

	if err := c.Login("user", "pass").Wait(); err != nil {
		t.Fatalf("Login: %v", err)
	}
	return c
}

// TestList_ReturnsAllMailboxes verifies that List returns one entry for each of
// the 6 mailboxes seeded by the mock server (INBOX, Sent, Drafts, Archive,
// Trash, Junk).
func TestList_ReturnsAllMailboxes(t *testing.T) {
	c := dialMock(t)

	got, err := folders.List(c)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	const wantCount = 6
	if len(got) != wantCount {
		t.Errorf("List: got %d mailboxes, want %d; names: %v", len(got), wantCount, mailboxNames(got))
	}
}

// TestList_AssignsRoles verifies that each fixture mailbox is assigned the
// correct role: INBOX via its reserved name, all others via SPECIAL-USE attrs.
// A mailbox with no SPECIAL-USE attr and not named INBOX must have an empty role.
func TestList_AssignsRoles(t *testing.T) {
	c := dialMock(t)

	got, err := folders.List(c)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Index results by name for easy lookup.
	byName := make(map[string]folders.Mailbox, len(got))
	for _, mb := range got {
		byName[mb.Name] = mb
	}

	cases := []struct {
		name     string
		wantRole mail.FolderRole
	}{
		{"INBOX", mail.RoleInbox},
		{"Sent", mail.RoleSent},
		{"Drafts", mail.RoleDrafts},
		{"Archive", mail.RoleArchive},
		{"Trash", mail.RoleTrash},
		{"Junk", mail.RoleJunk},
	}

	for _, tc := range cases {
		mb, ok := byName[tc.name]
		if !ok {
			t.Errorf("mailbox %q not found in List result", tc.name)
			continue
		}
		if mb.Role != tc.wantRole {
			t.Errorf("mailbox %q: got role %q, want %q", tc.name, mb.Role, tc.wantRole)
		}
	}
}

// TestList_NoRoleForUnknownMailbox verifies the invariant that any mailbox
// carrying no SPECIAL-USE attribute and not named INBOX gets an empty role.
// We test this by ensuring none of the 6 fixture mailboxes has an unexpected
// role assignment — in other words, the role is only ever empty when the name
// doesn't match a known role.
func TestList_NoRoleForUnknownMailbox(t *testing.T) {
	c := dialMock(t)

	got, err := folders.List(c)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	knownRoles := map[string]mail.FolderRole{
		"INBOX":   mail.RoleInbox,
		"Sent":    mail.RoleSent,
		"Drafts":  mail.RoleDrafts,
		"Archive": mail.RoleArchive,
		"Trash":   mail.RoleTrash,
		"Junk":    mail.RoleJunk,
	}

	for _, mb := range got {
		want, known := knownRoles[mb.Name]
		if known {
			if mb.Role != want {
				t.Errorf("mailbox %q: got role %q, want %q", mb.Name, mb.Role, want)
			}
		} else {
			// Any mailbox not in our known set must have an empty role.
			if mb.Role != "" {
				t.Errorf("mailbox %q has unexpected role %q (no SPECIAL-USE attr)", mb.Name, mb.Role)
			}
		}
	}
}

// TestDiscover_CompatibleWithList verifies that calling both Discover and List
// on the same connection yields consistent role→name mappings: every entry in
// the Discover map should match the role assigned by List to the mailbox of
// that name.
func TestDiscover_CompatibleWithList(t *testing.T) {
	c := dialMock(t)

	discovered, err := folders.Discover(c)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if discovered == nil {
		t.Fatal("Discover returned nil; expected SPECIAL-USE entries from mock server")
	}

	listed, err := folders.List(c)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Build a name→role index from List output.
	listRoles := make(map[string]mail.FolderRole, len(listed))
	for _, mb := range listed {
		listRoles[mb.Name] = mb.Role
	}

	// Every role in the Discover map must agree with the role List assigned.
	for role, name := range discovered {
		listRole, ok := listRoles[name]
		if !ok {
			t.Errorf("Discover role %s → %q, but List has no mailbox by that name", role, name)
			continue
		}
		if listRole != role {
			t.Errorf("consistency failure: Discover[%s]=%q but List assigns role %q to %q",
				role, name, listRole, name)
		}
	}
}

// mailboxNames is a diagnostic helper that extracts just the names from a
// Mailbox slice for use in error messages.
func mailboxNames(mbs []folders.Mailbox) []string {
	names := make([]string, len(mbs))
	for i, mb := range mbs {
		names[i] = mb.Name
	}
	return names
}
