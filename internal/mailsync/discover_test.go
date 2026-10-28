package mailsync_test

import (
	"fmt"
	"testing"

	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/5Sigma/pmx/internal/folders"
	"github.com/5Sigma/pmx/internal/mail"
)

// TestDiscoverSpecialUse starts the mock IMAP server (which seeds mailboxes
// with RFC 6154 SPECIAL-USE attributes), connects using DialInsecure, and
// verifies that folders.Discover returns exactly the five expected role→name
// mappings. It uses the startMockServer helper from engine_test.go, which is
// in the same external test package.
func TestDiscoverSpecialUse(t *testing.T) {
	_, acct := startMockServer(t)

	// Dial without TLS — the mock server is InsecureAuth with no TLS listener.
	addr := fmt.Sprintf("%s:%d", acct.IMAPHost, acct.IMAPPort)
	c, err := imapclient.DialInsecure(addr, nil)
	if err != nil {
		t.Fatalf("DialInsecure %s: %v", addr, err)
	}
	defer func() { _ = c.Logout().Wait() }()

	if err := c.Login(acct.Email, "pass").Wait(); err != nil {
		t.Fatalf("Login: %v", err)
	}

	got, err := folders.Discover(c)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got == nil {
		t.Fatal("Discover returned nil map; expected SPECIAL-USE entries from mock server")
	}

	want := map[mail.FolderRole]string{
		mail.RoleSent:    "Sent",
		mail.RoleDrafts:  "Drafts",
		mail.RoleArchive: "Archive",
		mail.RoleTrash:   "Trash",
		mail.RoleJunk:    "Junk",
	}

	if len(got) != len(want) {
		t.Errorf("Discover: got %d entries, want %d; map: %v", len(got), len(want), got)
	}

	for role, wantMailbox := range want {
		gotMailbox, ok := got[role]
		if !ok {
			t.Errorf("Discover: missing role %s, want mailbox %q", role, wantMailbox)
			continue
		}
		if gotMailbox != wantMailbox {
			t.Errorf("Discover role %s: got %q, want %q", role, gotMailbox, wantMailbox)
		}
	}

	// RoleInbox must not appear — INBOX carries no SPECIAL-USE attribute.
	if name, ok := got[mail.RoleInbox]; ok {
		t.Errorf("Discover: unexpected RoleInbox entry %q; INBOX has no SPECIAL-USE attr", name)
	}
}
