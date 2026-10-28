CREATE TABLE IF NOT EXISTS accounts (
    id    INTEGER PRIMARY KEY,
    name  TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS folders (
    id              INTEGER PRIMARY KEY,
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    uid_validity    INTEGER NOT NULL DEFAULT 0,
    highest_modseq  INTEGER NOT NULL DEFAULT 0,
    last_sync_at    INTEGER,
    UNIQUE (account_id, name)
);

CREATE TABLE IF NOT EXISTS messages (
    id            INTEGER PRIMARY KEY,
    folder_id     INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    uid           INTEGER NOT NULL,
    message_id    TEXT,
    in_reply_to   TEXT,
    references_   TEXT,
    thread_root   TEXT,
    subject       TEXT,
    from_addr     TEXT NOT NULL DEFAULT '',
    to_addrs      TEXT NOT NULL DEFAULT '[]',
    cc_addrs      TEXT NOT NULL DEFAULT '[]',
    bcc_addrs     TEXT NOT NULL DEFAULT '[]',
    date_sent     INTEGER,
    date_received INTEGER NOT NULL,
    flags         INTEGER NOT NULL DEFAULT 0,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    body_text     TEXT,
    body_html     TEXT,
    has_attach    INTEGER NOT NULL DEFAULT 0,
    raw_path      TEXT,
    UNIQUE (folder_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_messages_folder_date ON messages(folder_id, date_received DESC);
CREATE INDEX IF NOT EXISTS idx_messages_thread      ON messages(thread_root);
CREATE INDEX IF NOT EXISTS idx_messages_msgid       ON messages(message_id);

CREATE TABLE IF NOT EXISTS attachments (
    id         INTEGER PRIMARY KEY,
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    filename   TEXT,
    mime_type  TEXT,
    size_bytes INTEGER NOT NULL,
    sha256     TEXT NOT NULL,
    disk_path  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_attachments_message ON attachments(message_id);
CREATE INDEX IF NOT EXISTS idx_attachments_sha     ON attachments(sha256);

CREATE TABLE IF NOT EXISTS drafts (
    id         INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    subject    TEXT,
    to_addrs   TEXT NOT NULL DEFAULT '[]',
    cc_addrs   TEXT NOT NULL DEFAULT '[]',
    bcc_addrs  TEXT NOT NULL DEFAULT '[]',
    body_text  TEXT,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS outbox (
    id         INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    raw_eml    BLOB NOT NULL,
    queued_at  INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT
);

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);
