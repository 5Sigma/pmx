CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    subject,
    from_addr,
    to_addrs,
    body_text,
    content='messages',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, subject, from_addr, to_addrs, body_text)
    VALUES (new.id, new.subject, new.from_addr, new.to_addrs, new.body_text);
END;

CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, from_addr, to_addrs, body_text)
    VALUES ('delete', old.id, old.subject, old.from_addr, old.to_addrs, old.body_text);
END;

CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, from_addr, to_addrs, body_text)
    VALUES ('delete', old.id, old.subject, old.from_addr, old.to_addrs, old.body_text);
    INSERT INTO messages_fts(rowid, subject, from_addr, to_addrs, body_text)
    VALUES (new.id, new.subject, new.from_addr, new.to_addrs, new.body_text);
END;
