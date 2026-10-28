package store

import (
	"testing"

	"github.com/5Sigma/pmx/internal/mail"
)

// TestListFolders_Order verifies the canonical ordering: inbox first, then the
// remaining SPECIAL-USE roles in role-priority order, then user folders sorted
// alphabetically.
func TestListFolders_Order(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")

	// Insert in an arbitrary order that differs from the expected output order.
	names := []struct {
		name string
		role mail.FolderRole
	}{
		{"ZUserFolder", ""},
		{"Trash", mail.RoleTrash},
		{"INBOX", mail.RoleInbox},
		{"AUserFolder", ""},
		{"Sent", mail.RoleSent},
	}
	for _, n := range names {
		fid := seedFolder(t, s, acct, n.name)
		if n.role != "" {
			if err := s.UpsertFolderRole(fid, n.role); err != nil {
				t.Fatalf("UpsertFolderRole(%q, %q): %v", n.name, n.role, err)
			}
		}
	}

	folders, err := s.ListFolders(acct)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}

	if len(folders) != len(names) {
		t.Fatalf("ListFolders: got %d folders, want %d", len(folders), len(names))
	}

	// Expected: INBOX (inbox=0), Sent (sent=1), Trash (trash=4), then user
	// folders alphabetically: AUserFolder, ZUserFolder.
	want := []string{"INBOX", "Sent", "Trash", "AUserFolder", "ZUserFolder"}
	for i, f := range folders {
		if f.Name != want[i] {
			t.Errorf("ListFolders[%d]: got %q, want %q", i, f.Name, want[i])
		}
	}
}

// TestListFolders_FullRoleOrder exercises all six role slots to confirm the
// complete role-priority order defined in messages.go.
func TestListFolders_FullRoleOrder(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")

	// Seed all standard roles plus one plain folder, inserted in reverse order.
	seeded := []struct {
		name string
		role mail.FolderRole
	}{
		{"Junk", mail.RoleJunk},
		{"Trash", mail.RoleTrash},
		{"Archive", mail.RoleArchive},
		{"Drafts", mail.RoleDrafts},
		{"Sent", mail.RoleSent},
		{"INBOX", mail.RoleInbox},
		{"Personal", ""},
	}
	for _, n := range seeded {
		fid := seedFolder(t, s, acct, n.name)
		if n.role != "" {
			if err := s.UpsertFolderRole(fid, n.role); err != nil {
				t.Fatalf("UpsertFolderRole(%q): %v", n.name, err)
			}
		}
	}

	folders, err := s.ListFolders(acct)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}

	want := []string{"INBOX", "Sent", "Drafts", "Archive", "Trash", "Junk", "Personal"}
	if len(folders) != len(want) {
		t.Fatalf("got %d folders, want %d", len(folders), len(want))
	}
	for i, f := range folders {
		if f.Name != want[i] {
			t.Errorf("ListFolders[%d]: got %q, want %q (full order)", i, f.Name, want[i])
		}
	}
}

// TestUpsertFolderRole verifies that UpsertFolderRole sets a role on a folder
// and that a second call overwrites it.
func TestUpsertFolderRole(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "acct", "a@example.com")
	fid := seedFolder(t, s, acct, "Sent")

	// Folder starts with no role.
	f, err := s.GetFolder(fid)
	if err != nil {
		t.Fatalf("GetFolder before UpsertFolderRole: %v", err)
	}
	if f.Role != "" {
		t.Errorf("new folder: want empty role, got %q", f.Role)
	}

	// Set the role.
	if err := s.UpsertFolderRole(fid, mail.RoleSent); err != nil {
		t.Fatalf("UpsertFolderRole(RoleSent): %v", err)
	}
	f, err = s.GetFolder(fid)
	if err != nil {
		t.Fatalf("GetFolder after first UpsertFolderRole: %v", err)
	}
	if f.Role != mail.RoleSent {
		t.Errorf("after UpsertFolderRole(RoleSent): got %q, want %q", f.Role, mail.RoleSent)
	}

	// Overwrite with a different role.
	if err := s.UpsertFolderRole(fid, mail.RoleDrafts); err != nil {
		t.Fatalf("UpsertFolderRole(RoleDrafts): %v", err)
	}
	f, err = s.GetFolder(fid)
	if err != nil {
		t.Fatalf("GetFolder after second UpsertFolderRole: %v", err)
	}
	if f.Role != mail.RoleDrafts {
		t.Errorf("after UpsertFolderRole(RoleDrafts): got %q, want %q", f.Role, mail.RoleDrafts)
	}
}

// TestGetFolder_ReturnsRole verifies that GetFolder preserves the role that was
// set via UpsertFolderRole.
func TestGetFolder_ReturnsRole(t *testing.T) {
	cases := []struct {
		name string
		role mail.FolderRole
	}{
		{"INBOX", mail.RoleInbox},
		{"Sent", mail.RoleSent},
		{"Drafts", mail.RoleDrafts},
		{"Archive", mail.RoleArchive},
		{"Trash", mail.RoleTrash},
		{"Junk", mail.RoleJunk},
	}

	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			s := openTestStore(t)
			acct := seedAccount(t, s, "acct", "a@example.com")
			fid := seedFolder(t, s, acct, tc.name)

			if err := s.UpsertFolderRole(fid, tc.role); err != nil {
				t.Fatalf("UpsertFolderRole: %v", err)
			}

			f, err := s.GetFolder(fid)
			if err != nil {
				t.Fatalf("GetFolder: %v", err)
			}
			if f.Role != tc.role {
				t.Errorf("GetFolder: got role %q, want %q", f.Role, tc.role)
			}
		})
	}
}

// TestFolderByName_ReturnsRole verifies that FolderByName returns the role set
// by UpsertFolderRole.
func TestFolderByName_ReturnsRole(t *testing.T) {
	cases := []struct {
		name string
		role mail.FolderRole
	}{
		{"INBOX", mail.RoleInbox},
		{"Sent", mail.RoleSent},
		{"Drafts", mail.RoleDrafts},
		{"Archive", mail.RoleArchive},
		{"Trash", mail.RoleTrash},
		{"Junk", mail.RoleJunk},
	}

	for _, tc := range cases {
		t.Run(string(tc.role), func(t *testing.T) {
			s := openTestStore(t)
			acct := seedAccount(t, s, "acct", "a@example.com")
			fid := seedFolder(t, s, acct, tc.name)

			if err := s.UpsertFolderRole(fid, tc.role); err != nil {
				t.Fatalf("UpsertFolderRole: %v", err)
			}

			f, err := s.FolderByName(acct, tc.name)
			if err != nil {
				t.Fatalf("FolderByName(%q): %v", tc.name, err)
			}
			if f.Role != tc.role {
				t.Errorf("FolderByName(%q): got role %q, want %q", tc.name, f.Role, tc.role)
			}
		})
	}
}

// TestListFolders_IsolatedByAccount verifies that ListFolders returns only
// folders for the requested account, not those of sibling accounts.
func TestListFolders_IsolatedByAccount(t *testing.T) {
	s := openTestStore(t)
	a1 := seedAccount(t, s, "acct1", "a1@example.com")
	a2 := seedAccount(t, s, "acct2", "a2@example.com")

	for _, name := range []string{"INBOX", "Sent"} {
		seedFolder(t, s, a1, name)
	}
	seedFolder(t, s, a2, "INBOX")

	folders1, err := s.ListFolders(a1)
	if err != nil {
		t.Fatalf("ListFolders(a1): %v", err)
	}
	if len(folders1) != 2 {
		t.Errorf("acct1: want 2 folders, got %d", len(folders1))
	}

	folders2, err := s.ListFolders(a2)
	if err != nil {
		t.Fatalf("ListFolders(a2): %v", err)
	}
	if len(folders2) != 1 {
		t.Errorf("acct2: want 1 folder, got %d", len(folders2))
	}
}
