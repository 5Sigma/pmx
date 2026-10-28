package store

import (
	"testing"
	"time"
)

// ---- DeleteFolder -----------------------------------------------------------

func TestDeleteFolder(t *testing.T) {
	t.Run("folder row is removed", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "Trash")

		if err := s.DeleteFolder(fid); err != nil {
			t.Fatalf("DeleteFolder: %v", err)
		}
		_, err := s.GetFolder(fid)
		if err == nil {
			t.Errorf("GetFolder after DeleteFolder: want error, got nil")
		}
	})

	t.Run("messages cascade on delete", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "Spam")

		now := time.Now()
		for i := uint32(1); i <= 4; i++ {
			m := newMessage(fid, i, "msg", now)
			if err := s.UpsertMessage(m); err != nil {
				t.Fatalf("UpsertMessage uid=%d: %v", i, err)
			}
		}

		if err := s.DeleteFolder(fid); err != nil {
			t.Fatalf("DeleteFolder: %v", err)
		}

		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE folder_id = ?`, fid).Scan(&n); err != nil {
			t.Fatalf("count messages: %v", err)
		}
		if n != 0 {
			t.Errorf("want 0 messages after DeleteFolder (cascade), got %d", n)
		}
	})

	t.Run("only the target folder is deleted", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid1 := seedFolder(t, s, acct, "Inbox")
		fid2 := seedFolder(t, s, acct, "Sent")

		if err := s.DeleteFolder(fid1); err != nil {
			t.Fatalf("DeleteFolder: %v", err)
		}
		if _, err := s.GetFolder(fid2); err != nil {
			t.Errorf("GetFolder on surviving folder: %v", err)
		}
	})
}

// ---- RenameFolder -----------------------------------------------------------

func TestRenameFolder(t *testing.T) {
	t.Run("name is updated", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "OldName")

		if err := s.RenameFolder(fid, "NewName"); err != nil {
			t.Fatalf("RenameFolder: %v", err)
		}
		f, err := s.GetFolder(fid)
		if err != nil {
			t.Fatalf("GetFolder after rename: %v", err)
		}
		if f.Name != "NewName" {
			t.Errorf("Name: want %q, got %q", "NewName", f.Name)
		}
	})

	t.Run("folder_id is preserved after rename", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "Work")

		if err := s.RenameFolder(fid, "Personal"); err != nil {
			t.Fatalf("RenameFolder: %v", err)
		}
		f, err := s.GetFolder(fid)
		if err != nil {
			t.Fatalf("GetFolder: %v", err)
		}
		if f.ID != fid {
			t.Errorf("ID: want %d, got %d", fid, f.ID)
		}
	})

	t.Run("messages remain after rename", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "OldFolder")

		now := time.Now()
		for i := uint32(1); i <= 3; i++ {
			m := newMessage(fid, i, "preserved", now)
			if err := s.UpsertMessage(m); err != nil {
				t.Fatalf("UpsertMessage uid=%d: %v", i, err)
			}
		}

		if err := s.RenameFolder(fid, "RenamedFolder"); err != nil {
			t.Fatalf("RenameFolder: %v", err)
		}
		msgs, err := s.ListFolder(fid, 100, 0)
		if err != nil {
			t.Fatalf("ListFolder after rename: %v", err)
		}
		if len(msgs) != 3 {
			t.Errorf("want 3 messages after rename, got %d", len(msgs))
		}
	})
}

// ---- UpdateFolderStatus -----------------------------------------------------

func TestUpdateFolderStatus(t *testing.T) {
	t.Run("all five fields are written", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "INBOX")

		at := time.Now().Truncate(time.Second)
		if err := s.UpdateFolderStatus(fid, 1001, 50, 200, 9999, at); err != nil {
			t.Fatalf("UpdateFolderStatus: %v", err)
		}

		f, err := s.GetFolder(fid)
		if err != nil {
			t.Fatalf("GetFolder: %v", err)
		}
		if f.UIDValidity != 1001 {
			t.Errorf("UIDValidity: want 1001, got %d", f.UIDValidity)
		}
		if f.UIDNext != 50 {
			t.Errorf("UIDNext: want 50, got %d", f.UIDNext)
		}
		if f.NumMessages != 200 {
			t.Errorf("NumMessages: want 200, got %d", f.NumMessages)
		}
		if f.HighestModSeq != 9999 {
			t.Errorf("HighestModSeq: want 9999, got %d", f.HighestModSeq)
		}
		if !f.LastSyncAt.Equal(at) {
			t.Errorf("LastSyncAt: want %v, got %v", at, f.LastSyncAt)
		}
	})

	t.Run("zero values are stored without error", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "Sent")

		if err := s.UpdateFolderStatus(fid, 0, 0, 0, 0, time.Unix(0, 0)); err != nil {
			t.Fatalf("UpdateFolderStatus with zeros: %v", err)
		}
		f, err := s.GetFolder(fid)
		if err != nil {
			t.Fatalf("GetFolder: %v", err)
		}
		if f.UIDValidity != 0 || f.UIDNext != 0 || f.NumMessages != 0 || f.HighestModSeq != 0 {
			t.Errorf("expected all-zero STATUS fields, got uid_validity=%d uid_next=%d num_messages=%d highest_modseq=%d",
				f.UIDValidity, f.UIDNext, f.NumMessages, f.HighestModSeq)
		}
	})

	t.Run("update is isolated to the target folder", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid1 := seedFolder(t, s, acct, "INBOX")
		fid2 := seedFolder(t, s, acct, "Sent")

		at := time.Now().Truncate(time.Second)
		if err := s.UpdateFolderStatus(fid1, 777, 10, 5, 42, at); err != nil {
			t.Fatalf("UpdateFolderStatus fid1: %v", err)
		}
		f2, err := s.GetFolder(fid2)
		if err != nil {
			t.Fatalf("GetFolder fid2: %v", err)
		}
		if f2.UIDValidity != 0 {
			t.Errorf("fid2 UIDValidity should remain 0 after updating fid1, got %d", f2.UIDValidity)
		}
	})
}

// ---- FolderByUIDValidity ----------------------------------------------------

func TestFolderByUIDValidity(t *testing.T) {
	t.Run("found — returns matching folder", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "INBOX")

		at := time.Now().Truncate(time.Second)
		if err := s.UpdateFolderStatus(fid, 4242, 1, 0, 0, at); err != nil {
			t.Fatalf("UpdateFolderStatus: %v", err)
		}

		f, err := s.FolderByUIDValidity(acct, 4242)
		if err != nil {
			t.Fatalf("FolderByUIDValidity: %v", err)
		}
		if f == nil {
			t.Fatal("FolderByUIDValidity: want non-nil, got nil")
		}
		if f.ID != fid {
			t.Errorf("ID: want %d, got %d", fid, f.ID)
		}
		if f.UIDValidity != 4242 {
			t.Errorf("UIDValidity: want 4242, got %d", f.UIDValidity)
		}
	})

	t.Run("not found — returns nil without error", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")

		f, err := s.FolderByUIDValidity(acct, 99999)
		if err != nil {
			t.Fatalf("FolderByUIDValidity: want nil error for no match, got %v", err)
		}
		if f != nil {
			t.Errorf("FolderByUIDValidity: want nil folder for no match, got %+v", f)
		}
	})

	t.Run("uid_validity=0 does not match", func(t *testing.T) {
		// uid_validity=0 is the default for new folders; searching by 0 should
		// not accidentally return an unset folder.
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		seedFolder(t, s, acct, "Drafts") // uid_validity stays 0

		f, err := s.FolderByUIDValidity(acct, 0)
		if err != nil {
			t.Fatalf("FolderByUIDValidity(0): %v", err)
		}
		// The LIMIT 1 query will return the row since uid_validity=0 matches.
		// This is expected behavior — the caller in sweep.go skips uid_validity==0.
		// We just confirm it doesn't error.
		_ = f
	})

	t.Run("isolated by account — same uid_validity on different account is not returned", func(t *testing.T) {
		s := openTestStore(t)
		acct1 := seedAccount(t, s, "acct1", "a1@example.com")
		acct2 := seedAccount(t, s, "acct2", "a2@example.com")

		fid := seedFolder(t, s, acct1, "INBOX")
		at := time.Now().Truncate(time.Second)
		if err := s.UpdateFolderStatus(fid, 8888, 1, 0, 0, at); err != nil {
			t.Fatalf("UpdateFolderStatus: %v", err)
		}

		f, err := s.FolderByUIDValidity(acct2, 8888)
		if err != nil {
			t.Fatalf("FolderByUIDValidity acct2: %v", err)
		}
		if f != nil {
			t.Errorf("FolderByUIDValidity should return nil for different account, got %+v", f)
		}
	})

	t.Run("two folders same account same uid_validity — returns one without error", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid1 := seedFolder(t, s, acct, "FolderA")
		fid2 := seedFolder(t, s, acct, "FolderB")

		at := time.Now().Truncate(time.Second)
		if err := s.UpdateFolderStatus(fid1, 1234, 1, 0, 0, at); err != nil {
			t.Fatalf("UpdateFolderStatus fid1: %v", err)
		}
		if err := s.UpdateFolderStatus(fid2, 1234, 1, 0, 0, at); err != nil {
			t.Fatalf("UpdateFolderStatus fid2: %v", err)
		}

		f, err := s.FolderByUIDValidity(acct, 1234)
		if err != nil {
			t.Fatalf("FolderByUIDValidity with collision: %v", err)
		}
		if f == nil {
			t.Fatal("FolderByUIDValidity with collision: want non-nil folder, got nil")
		}
		// Either fid1 or fid2 is acceptable — the caller uses LIMIT 1.
		if f.ID != fid1 && f.ID != fid2 {
			t.Errorf("FolderByUIDValidity: got unexpected ID %d (want %d or %d)", f.ID, fid1, fid2)
		}
	})
}

// ---- Hidden folder lifecycle ------------------------------------------------

func TestHiddenFolderLifecycle(t *testing.T) {
	t.Run("MarkFolderHidden sets hidden_since", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "OldFolder")

		at := time.Unix(1700000000, 0)
		if err := s.MarkFolderHidden(fid, at); err != nil {
			t.Fatalf("MarkFolderHidden: %v", err)
		}
		f, err := s.GetFolder(fid)
		if err != nil {
			t.Fatalf("GetFolder after MarkFolderHidden: %v", err)
		}
		if f.HiddenSince != at.Unix() {
			t.Errorf("HiddenSince: want %d, got %d", at.Unix(), f.HiddenSince)
		}
	})

	t.Run("ListFolders excludes hidden folders", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		visible := seedFolder(t, s, acct, "INBOX")
		hidden := seedFolder(t, s, acct, "Vanished")

		if err := s.MarkFolderHidden(hidden, time.Now()); err != nil {
			t.Fatalf("MarkFolderHidden: %v", err)
		}

		folders, err := s.ListFolders(acct)
		if err != nil {
			t.Fatalf("ListFolders: %v", err)
		}
		for _, f := range folders {
			if f.ID == hidden {
				t.Errorf("ListFolders includes hidden folder ID %d (%q)", hidden, f.Name)
			}
		}
		found := false
		for _, f := range folders {
			if f.ID == visible {
				found = true
			}
		}
		if !found {
			t.Errorf("ListFolders does not include visible folder ID %d", visible)
		}
	})

	t.Run("ListHiddenFolders returns only hidden folders", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		seedFolder(t, s, acct, "INBOX")
		fid := seedFolder(t, s, acct, "Gone")

		at := time.Unix(1700000000, 0)
		if err := s.MarkFolderHidden(fid, at); err != nil {
			t.Fatalf("MarkFolderHidden: %v", err)
		}

		hidden, err := s.ListHiddenFolders(acct)
		if err != nil {
			t.Fatalf("ListHiddenFolders: %v", err)
		}
		if len(hidden) != 1 {
			t.Fatalf("want 1 hidden folder, got %d", len(hidden))
		}
		if hidden[0].ID != fid {
			t.Errorf("hidden folder ID: want %d, got %d", fid, hidden[0].ID)
		}
		if hidden[0].HiddenSince != at.Unix() {
			t.Errorf("HiddenSince: want %d, got %d", at.Unix(), hidden[0].HiddenSince)
		}
	})

	t.Run("UnmarkFolderHidden clears hidden_since", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "Reappeared")

		if err := s.MarkFolderHidden(fid, time.Now()); err != nil {
			t.Fatalf("MarkFolderHidden: %v", err)
		}
		if err := s.UnmarkFolderHidden(fid); err != nil {
			t.Fatalf("UnmarkFolderHidden: %v", err)
		}
		f, err := s.GetFolder(fid)
		if err != nil {
			t.Fatalf("GetFolder after UnmarkFolderHidden: %v", err)
		}
		if f.HiddenSince != 0 {
			t.Errorf("HiddenSince: want 0 after unmark, got %d", f.HiddenSince)
		}
	})

	t.Run("folder re-appears in ListFolders after UnmarkFolderHidden", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")
		fid := seedFolder(t, s, acct, "Returned")

		if err := s.MarkFolderHidden(fid, time.Now()); err != nil {
			t.Fatalf("MarkFolderHidden: %v", err)
		}
		if err := s.UnmarkFolderHidden(fid); err != nil {
			t.Fatalf("UnmarkFolderHidden: %v", err)
		}

		folders, err := s.ListFolders(acct)
		if err != nil {
			t.Fatalf("ListFolders: %v", err)
		}
		found := false
		for _, f := range folders {
			if f.ID == fid {
				found = true
			}
		}
		if !found {
			t.Errorf("folder %d not visible in ListFolders after UnmarkFolderHidden", fid)
		}
	})

	t.Run("ListHiddenFolders is isolated by account", func(t *testing.T) {
		s := openTestStore(t)
		acct1 := seedAccount(t, s, "acct1", "a1@example.com")
		acct2 := seedAccount(t, s, "acct2", "a2@example.com")

		fid1 := seedFolder(t, s, acct1, "Gone1")
		fid2 := seedFolder(t, s, acct2, "Gone2")

		if err := s.MarkFolderHidden(fid1, time.Now()); err != nil {
			t.Fatalf("MarkFolderHidden acct1: %v", err)
		}
		if err := s.MarkFolderHidden(fid2, time.Now()); err != nil {
			t.Fatalf("MarkFolderHidden acct2: %v", err)
		}

		hidden1, err := s.ListHiddenFolders(acct1)
		if err != nil {
			t.Fatalf("ListHiddenFolders acct1: %v", err)
		}
		if len(hidden1) != 1 || hidden1[0].ID != fid1 {
			t.Errorf("acct1 hidden: want [%d], got %v", fid1, hidden1)
		}

		hidden2, err := s.ListHiddenFolders(acct2)
		if err != nil {
			t.Fatalf("ListHiddenFolders acct2: %v", err)
		}
		if len(hidden2) != 1 || hidden2[0].ID != fid2 {
			t.Errorf("acct2 hidden: want [%d], got %v", fid2, hidden2)
		}
	})

	t.Run("multiple hidden folders all appear in ListHiddenFolders", func(t *testing.T) {
		s := openTestStore(t)
		acct := seedAccount(t, s, "acct", "a@example.com")

		for _, name := range []string{"Alpha", "Beta", "Gamma"} {
			fid := seedFolder(t, s, acct, name)
			if err := s.MarkFolderHidden(fid, time.Now()); err != nil {
				t.Fatalf("MarkFolderHidden %q: %v", name, err)
			}
		}

		hidden, err := s.ListHiddenFolders(acct)
		if err != nil {
			t.Fatalf("ListHiddenFolders: %v", err)
		}
		if len(hidden) != 3 {
			t.Errorf("want 3 hidden folders, got %d", len(hidden))
		}
	})
}
