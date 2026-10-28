ALTER TABLE folders ADD COLUMN role TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_folders_account_role ON folders(account_id, role);
