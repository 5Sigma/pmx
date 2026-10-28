package store

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/5Sigma/pmx/internal/mail"
)

// Compile-time check that *Store satisfies mail.MessageStore.
var _ mail.MessageStore = (*Store)(nil)

type Store struct {
	db         *sql.DB
	attachsDir string
}

// Open opens (or creates) the SQLite database at dbPath, runs any pending
// migrations, and sets pragmas for WAL mode and performance.
func Open(dbPath, attachsDir string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous  = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA temp_store   = MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}

	return &Store{db: db, attachsDir: attachsDir}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) AttachmentPath(a mail.Attachment) string {
	return filepath.Join(s.attachsDir, a.DiskPath)
}
