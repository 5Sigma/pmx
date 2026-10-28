ALTER TABLE folders ADD COLUMN uid_next     INTEGER NOT NULL DEFAULT 0;
ALTER TABLE folders ADD COLUMN num_messages INTEGER NOT NULL DEFAULT 0;
ALTER TABLE folders ADD COLUMN hidden_since INTEGER;
ALTER TABLE folders ADD COLUMN mailbox_id   TEXT    NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_folders_mailbox_id
    ON folders(account_id, mailbox_id)
    WHERE mailbox_id != '';
