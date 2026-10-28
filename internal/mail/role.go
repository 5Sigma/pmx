package mail

// FolderRole identifies a well-known IMAP mailbox by its semantic purpose,
// independent of the name a specific server gives it.
type FolderRole string

const (
	RoleInbox   FolderRole = "inbox"
	RoleSent    FolderRole = "sent"
	RoleDrafts  FolderRole = "drafts"
	RoleArchive FolderRole = "archive"
	RoleTrash   FolderRole = "trash"
	RoleJunk    FolderRole = "junk"
)

// roleAttrs maps each role to its RFC 6154 SPECIAL-USE attribute string.
// RoleInbox has no SPECIAL-USE attribute — INBOX is identified by its
// reserved name per RFC 3501 §5.1, not by a mailbox attribute.
var roleAttrs = map[FolderRole]string{
	RoleSent:    `\Sent`,
	RoleDrafts:  `\Drafts`,
	RoleArchive: `\Archive`,
	RoleTrash:   `\Trash`,
	RoleJunk:    `\Junk`,
}

// roleDefaults maps each role to the conventional fallback mailbox name used
// when neither the user config nor SPECIAL-USE discovery provides an answer.
var roleDefaults = map[FolderRole]string{
	RoleInbox:   "INBOX",
	RoleSent:    "Sent",
	RoleDrafts:  "Drafts",
	RoleArchive: "Archive",
	RoleTrash:   "Trash",
	RoleJunk:    "Junk",
}

// SpecialUseAttr returns the RFC 6154 SPECIAL-USE attribute string for this
// role (e.g. `\Trash`), or "" for RoleInbox.
func (r FolderRole) SpecialUseAttr() string {
	return roleAttrs[r]
}

// DefaultMailbox returns the hardcoded conventional mailbox name for this role,
// used as a last-resort fallback when config and SPECIAL-USE discovery both
// fail to resolve the role.
func (r FolderRole) DefaultMailbox() string {
	if d, ok := roleDefaults[r]; ok {
		return d
	}
	return string(r)
}
