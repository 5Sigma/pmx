-- contacts is a frequency index of email addresses, not a full address book.
-- Rows are scoped per account and cascade-deleted when the account is removed.
CREATE TABLE IF NOT EXISTS contacts (
    id              INTEGER PRIMARY KEY,
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    email           TEXT    NOT NULL,
    name            TEXT    NOT NULL DEFAULT '',
    name_lc         TEXT    NOT NULL DEFAULT '',
    sent_count      INTEGER NOT NULL DEFAULT 0,
    received_count  INTEGER NOT NULL DEFAULT 0,
    first_seen      INTEGER NOT NULL,
    last_seen       INTEGER NOT NULL,
    UNIQUE (account_id, email)
);

CREATE INDEX IF NOT EXISTS idx_contacts_account_email
    ON contacts(account_id, email);
CREATE INDEX IF NOT EXISTS idx_contacts_account_name_lc
    ON contacts(account_id, name_lc);
CREATE INDEX IF NOT EXISTS idx_contacts_account_rank
    ON contacts(account_id, sent_count DESC, received_count DESC, last_seen DESC);
