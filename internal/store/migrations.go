package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"
)

//go:embed schema/0001_init.sql
var migration0001 string

//go:embed schema/0002_fts.sql
var migration0002 string

//go:embed schema/0003_attachment_content_id.sql
var migration0003 string

//go:embed schema/0004_folder_role.sql
var migration0004 string

//go:embed schema/0005_folder_sync_cache.sql
var migration0005 string

//go:embed schema/0006_outbox_claim.sql
var migration0006 string

//go:embed schema/0007_contacts.sql
var migration0007 string

//go:embed schema/0008_fts_rebuild.sql
var migration0008 string

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{1, migration0001},
	{2, migration0002},
	{3, migration0003},
	{4, migration0004},
	{5, migration0005},
	{6, migration0006},
	{7, migration0007},
	{8, migration0008},
}

func runMigrations(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        version    INTEGER PRIMARY KEY,
        applied_at INTEGER NOT NULL
    )`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range migrations {
		var exists int
		err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.version, err)
		}
		if exists > 0 {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().Unix()); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
	}
	return nil
}
