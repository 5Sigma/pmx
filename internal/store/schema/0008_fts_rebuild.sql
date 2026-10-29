-- Rebuild the FTS5 index from the messages content table.
-- Migration 0002 created the table and triggers but did not populate the index
-- for messages that already existed, leaving them unsearchable.
INSERT INTO messages_fts(messages_fts) VALUES('rebuild');
