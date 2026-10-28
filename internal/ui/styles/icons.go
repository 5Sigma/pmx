package styles

import "github.com/5Sigma/pmx/internal/mail"

// FolderIcons maps a mail.FolderRole to a Nerd Fonts glyph used across list
// headers, breadcrumbs, and the folder drawer. Glyphs are single-width
// characters that assume a Nerd Font is installed in the user's terminal.
//
// Codepoints (nerd-fonts cheat-sheet):
//
//	inbox   U+F01C  nf-fa-inbox
//	sent    U+F1D8  nf-fa-paper_plane
//	drafts  U+F040  nf-fa-pencil
//	archive U+F187  nf-fa-archive
//	trash   U+F1F8  nf-fa-trash
//	junk    U+F071  nf-fa-exclamation_triangle (spam / junk)
//	folder  U+F07B  nf-fa-folder (default)
var FolderIcons = map[mail.FolderRole]string{
	mail.RoleInbox:   "",
	mail.RoleSent:    "",
	mail.RoleDrafts:  "",
	mail.RoleArchive: "",
	mail.RoleTrash:   "",
	mail.RoleJunk:    "",
}

// FolderIconDefault is used for folders without a known role (e.g. custom
// user-created mailboxes).
const FolderIconDefault = ""

// AccountIconUser is the glyph for a non-active account row in the account
// drawer (nf-fa-user U+F007).
const AccountIconUser = ""

// AccountIconList is the glyph for the account drawer title bar
// (nf-fa-users U+F0C0).
const AccountIconList = ""

// AccountChevron is the dim right-chevron appended to the header account tag
// when multiple accounts are configured, signalling the drawer is available.
const AccountChevron = "›"
