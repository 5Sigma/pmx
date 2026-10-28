package pathpicker

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fsEntry struct {
	Name  string
	IsDir bool
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

// resolveDirAndStub splits the current input value into the directory to list
// and the stub (partial name) to filter against. A trailing separator means
// the user is "inside" that directory with no stub.
func resolveDirAndStub(input string) (dir, stub string) {
	if input == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "/", ""
		}
		return home, ""
	}
	// Trailing slash or separator: list the directory itself.
	if input[len(input)-1] == os.PathSeparator || input == string(os.PathSeparator) {
		clean := filepath.Clean(input)
		return clean, ""
	}
	dir = filepath.Dir(input)
	stub = filepath.Base(input)
	return dir, stub
}

// listDir reads a directory and returns its entries sorted directories-first,
// then alphabetically within each group. Symlinks are classified by their
// target (via os.Stat). Returns an empty slice on ENOENT; returns the error
// and empty slice on EACCES or other IO errors.
func listDir(dir string) ([]fsEntry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	entries := make([]fsEntry, 0, len(des))
	for _, d := range des {
		isDir := d.IsDir()
		if d.Type()&os.ModeSymlink != 0 {
			// Resolve symlink target to classify correctly.
			info, statErr := os.Stat(filepath.Join(dir, d.Name()))
			if statErr == nil {
				isDir = info.IsDir()
			}
			// Broken symlinks appear as files.
		}
		entries = append(entries, fsEntry{Name: d.Name(), IsDir: isDir})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// ensureDir creates dir and any necessary parents.
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
