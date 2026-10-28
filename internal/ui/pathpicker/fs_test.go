package pathpicker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome_Empty(t *testing.T) {
	if got := ExpandHome(""); got != "" {
		t.Errorf("ExpandHome(\"\") = %q, want %q", got, "")
	}
}

func TestExpandHome_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	if got := ExpandHome("~"); got != home {
		t.Errorf("ExpandHome(\"~\") = %q, want %q", got, home)
	}
}

func TestExpandHome_TildeSlash(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	want := filepath.Join(home, "foo")
	if got := ExpandHome("~/foo"); got != want {
		t.Errorf("ExpandHome(\"~/foo\") = %q, want %q", got, want)
	}
}

func TestExpandHome_TildeSlashNested(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	want := filepath.Join(home, "a/b")
	if got := ExpandHome("~/a/b"); got != want {
		t.Errorf("ExpandHome(\"~/a/b\") = %q, want %q", got, want)
	}
}

func TestExpandHome_AbsPath(t *testing.T) {
	input := "/usr/local/bin"
	if got := ExpandHome(input); got != input {
		t.Errorf("ExpandHome(%q) = %q, want unchanged %q", input, got, input)
	}
}

func TestExpandHome_RelPath(t *testing.T) {
	input := "some/relative/path"
	if got := ExpandHome(input); got != input {
		t.Errorf("ExpandHome(%q) = %q, want unchanged %q", input, got, input)
	}
}

func TestResolveDirAndStub_TrailingSlash(t *testing.T) {
	dir, stub := resolveDirAndStub("/foo/bar/")
	if dir != "/foo/bar" {
		t.Errorf("dir = %q, want %q", dir, "/foo/bar")
	}
	if stub != "" {
		t.Errorf("stub = %q, want %q", stub, "")
	}
}

func TestResolveDirAndStub_NoStub(t *testing.T) {
	dir, stub := resolveDirAndStub("/")
	if dir != "/" {
		t.Errorf("dir = %q, want %q", dir, "/")
	}
	if stub != "" {
		t.Errorf("stub = %q, want %q", stub, "")
	}
}

func TestResolveDirAndStub_WithStub(t *testing.T) {
	dir, stub := resolveDirAndStub("/foo/bar")
	if dir != "/foo" {
		t.Errorf("dir = %q, want %q", dir, "/foo")
	}
	if stub != "bar" {
		t.Errorf("stub = %q, want %q", stub, "bar")
	}
}

func TestResolveDirAndStub_Empty(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	dir, stub := resolveDirAndStub("")
	if dir != home {
		t.Errorf("dir = %q, want home %q", dir, home)
	}
	if stub != "" {
		t.Errorf("stub = %q, want %q", stub, "")
	}
}

func TestListDir_SortedDirsFirst(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"bravo", "alpha"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"zeta", "mango"} {
		if err := os.WriteFile(filepath.Join(root, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := listDir(root)
	if err != nil {
		t.Fatalf("listDir returned error: %v", err)
	}

	want := []struct {
		name  string
		isDir bool
	}{
		{"alpha", true},
		{"bravo", true},
		{"mango", false},
		{"zeta", false},
	}

	if len(entries) != len(want) {
		t.Fatalf("len(entries) = %d, want %d; entries = %v", len(entries), len(want), entries)
	}
	for i, w := range want {
		if entries[i].Name != w.name || entries[i].IsDir != w.isDir {
			t.Errorf("entries[%d] = {%q, isDir=%v}, want {%q, isDir=%v}",
				i, entries[i].Name, entries[i].IsDir, w.name, w.isDir)
		}
	}
}

func TestListDir_ENOENT(t *testing.T) {
	entries, err := listDir("/this/path/does/not/exist/at/all")
	if err != nil {
		t.Errorf("listDir(nonexistent) error = %v, want nil", err)
	}
	if entries != nil {
		t.Errorf("listDir(nonexistent) entries = %v, want nil", entries)
	}
}

func TestListDir_Empty(t *testing.T) {
	root := t.TempDir()
	entries, err := listDir(root)
	if err != nil {
		t.Fatalf("listDir(empty dir) error = %v, want nil", err)
	}
	if len(entries) != 0 {
		t.Errorf("listDir(empty dir) len = %d, want 0; entries = %v", len(entries), entries)
	}
}

func TestEnsureDir_Creates(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a", "b", "c")
	if err := ensureDir(target); err != nil {
		t.Fatalf("ensureDir(%q) error = %v", target, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after ensureDir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q exists but is not a directory", target)
	}
}

func TestEnsureDir_AlreadyExists(t *testing.T) {
	target := filepath.Join(t.TempDir(), "x", "y")
	if err := ensureDir(target); err != nil {
		t.Fatalf("first ensureDir error: %v", err)
	}
	if err := ensureDir(target); err != nil {
		t.Errorf("second ensureDir (idempotent) error = %v, want nil", err)
	}
}
