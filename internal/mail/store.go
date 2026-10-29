package mail

import "time"

// MessageStore is the persistence interface the UI and sync logic depend on.
type MessageStore interface {
	// EnsureAccount creates or returns an account row by config name.
	EnsureAccount(name, email string) (int64, error)
	// EnsureFolder creates or returns a folder row.
	EnsureFolder(accountID int64, name string) (int64, error)
	// GetFolder returns a folder by its primary key.
	GetFolder(folderID int64) (*Folder, error)
	// FolderByName returns a folder by account + name.
	FolderByName(accountID int64, name string) (*Folder, error)
	// ListFolders returns all folders for an account ordered by role priority:
	// inbox first, then sent, drafts, archive, trash, junk, then the rest
	// alphabetically.
	ListFolders(accountID int64) ([]*Folder, error)
	// UpsertFolderRole sets the role for a folder row.
	UpsertFolderRole(folderID int64, role FolderRole) error
	// UpsertMessage inserts or updates a message (keyed by folder_id + uid).
	UpsertMessage(m *Message) error
	// ListFolder returns messages for a folder, newest first.
	ListFolder(folderID int64, limit, offset int) ([]*Message, error)
	// CountUnread returns the number of messages in a folder that lack the
	// \Seen flag. Returns 0, nil for an unknown folder.
	CountUnread(folderID int64) (int, error)
	// CountMessages returns the number of messages stored locally in a folder.
	CountMessages(folderID int64) (int, error)
	// ByUID fetches a single message by IMAP UID.
	ByUID(folderID int64, uid uint32) (*Message, error)
	// Body returns the plain-text body of a message.
	Body(messageID int64) (string, error)
	// BodyHTML returns the raw HTML body of a message (may be empty).
	BodyHTML(messageID int64) (string, error)
	// Search performs an FTS5 full-text search across all messages.
	Search(query string, limit int) ([]*Message, error)
	// ExistingUIDs returns the set of UIDs stored for a folder.
	ExistingUIDs(folderID int64) (map[uint32]struct{}, error)
	// ClearFolderMessages deletes all messages in a folder (used on UID validity reset).
	ClearFolderMessages(folderID int64) error
	// UpdateFlags updates the flag bitmask for a single message.
	UpdateFlags(folderID int64, uid uint32, flags Flag) error
	// DeleteMessageByUID deletes a single message by UID.
	DeleteMessageByUID(folderID int64, uid uint32) error
	// UpdateFolderSync records the folder's IMAP sync state.
	UpdateFolderSync(folderID int64, uidValidity uint32, highestModSeq uint64, at time.Time) error
	// DeleteFolder removes a folder row; messages and attachments cascade.
	DeleteFolder(folderID int64) error
	// RenameFolder updates the folder name while preserving folder_id and messages.
	RenameFolder(folderID int64, newName string) error
	// UpdateFolderStatus records STATUS values observed during a background sweep.
	UpdateFolderStatus(folderID int64, uidValidity uint32, uidNext uint32, numMessages uint32, highestModSeq uint64, at time.Time) error
	// FolderByUIDValidity looks up a folder by (account_id, uid_validity).
	// Returns nil, nil when no row matches.
	FolderByUIDValidity(accountID int64, uidValidity uint32) (*Folder, error)
	// MarkFolderHidden records hidden_since; the folder stays queryable.
	MarkFolderHidden(folderID int64, at time.Time) error
	// UnmarkFolderHidden clears hidden_since (folder reappeared on server).
	UnmarkFolderHidden(folderID int64) error
	// ListHiddenFolders returns folders whose hidden_since is non-null.
	ListHiddenFolders(accountID int64) ([]*Folder, error)
	// InsertAttachment stores an attachment record; back-fills a.ID.
	InsertAttachment(a *Attachment) error
	// ListAttachments returns all attachments for a message.
	ListAttachments(messageID int64) ([]Attachment, error)
	// AttachmentPath returns the absolute on-disk path for an attachment.
	AttachmentPath(a Attachment) string
	// SaveDraft creates or updates a draft.
	SaveDraft(d *Draft) error
	// Drafts lists all drafts for an account.
	Drafts(accountID int64) ([]*Draft, error)
	// EnqueueOutbox adds a message to the outbox for deferred sending.
	EnqueueOutbox(item *OutboxItem) error
	// ListPendingOutbox returns all outbox rows ordered by queued_at ASC.
	ListPendingOutbox(limit int) ([]*OutboxItem, error)
	// ListPendingOutboxForAccount returns outbox rows for a specific account_id, ordered by queued_at ASC.
	ListPendingOutboxForAccount(accountID int64, limit int) ([]*OutboxItem, error)
	// MarkOutboxSent deletes the outbox row (message was successfully transmitted).
	MarkOutboxSent(id int64) error
	// MarkOutboxFailed increments attempts and records the error string.
	MarkOutboxFailed(id int64, errMsg string) error
	// GetOutboxItem returns the outbox row for id, or (nil, false, nil) if deleted.
	GetOutboxItem(id int64) (*OutboxItem, bool, error)
	// ClaimPendingOutbox atomically claims up to limit unclaimed outbox rows for
	// accountID and returns them. Rows locked_at more than staleAfter ago are
	// considered abandoned and eligible for re-claiming.
	ClaimPendingOutbox(accountID int64, claimant string, limit int, now time.Time, staleAfter time.Duration) ([]*OutboxItem, error)
	// FolderByRole returns the first folder with the given role for an account,
	// or nil, nil when no such folder exists yet.
	FolderByRole(accountID int64, role FolderRole) (*Folder, error)
	// NewMessagesSince returns up to limit unseen messages in the folder whose
	// UID is strictly greater than afterUID, ordered by UID ASC.
	NewMessagesSince(folderID int64, afterUID uint32, limit int) ([]*Message, error)
	// UpsertContact records a single address interaction for accountID.
	UpsertContact(accountID int64, addr Address, kind ContactKind, now time.Time) error
	// UpsertContacts records a batch of address interactions in a single transaction.
	UpsertContacts(accountID int64, items []ContactUpsert, now time.Time) error
	// SearchContacts returns contacts matching prefix, ranked by interaction frequency.
	SearchContacts(accountID int64, prefix string, limit int) ([]Contact, error)
	// Close closes the store.
	Close() error
}
