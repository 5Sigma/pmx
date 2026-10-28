ALTER TABLE attachments ADD COLUMN content_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_attachments_content_id ON attachments(message_id, content_id);
