// Package folders provides IMAP folder-role resolution: mapping semantic roles
// (Sent, Trash, Archive, etc.) to the mailbox names a specific server uses.
// Resolution order: per-account config override → IMAP SPECIAL-USE discovery
// → conventional hardcoded default.
package folders

import (
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
)

// Mailbox is a single IMAP mailbox with its optional role.
// Role is "" when no SPECIAL-USE attribute is present and the name is not
// "INBOX"; it is mail.RoleInbox for the RFC-reserved INBOX name.
type Mailbox struct {
	Name string
	Role mail.FolderRole
}

// attrToRole maps RFC 6154 SPECIAL-USE attributes to pmx roles.
var attrToRole = map[imap.MailboxAttr]mail.FolderRole{
	imap.MailboxAttrSent:    mail.RoleSent,
	imap.MailboxAttrDrafts:  mail.RoleDrafts,
	imap.MailboxAttrArchive: mail.RoleArchive,
	imap.MailboxAttrTrash:   mail.RoleTrash,
	imap.MailboxAttrJunk:    mail.RoleJunk,
}

// List queries the server for all mailboxes, attaching a role to each one that
// carries an RFC 6154 SPECIAL-USE attribute or whose name is "INBOX".
// ReturnSpecialUse is only requested when the server advertises the SPECIAL-USE
// capability; servers that lack it respond with a malformed response that
// corrupts the IMAP session.
func List(c *imapclient.Client) ([]Mailbox, error) {
	opts := &imap.ListOptions{}
	if c.Caps().Has(imap.CapSpecialUse) {
		opts.ReturnSpecialUse = true
	}
	data, err := c.List("", "*", opts).Collect()
	if err != nil {
		return nil, err
	}

	out := make([]Mailbox, 0, len(data))
	for _, d := range data {
		mb := Mailbox{Name: d.Mailbox}

		// INBOX is identified by its reserved name (RFC 3501 §5.1); it carries
		// no SPECIAL-USE attribute, so we assign its role explicitly.
		if strings.EqualFold(d.Mailbox, "INBOX") {
			mb.Role = mail.RoleInbox
		} else {
			for _, a := range d.Attrs {
				if role, ok := attrToRole[a]; ok {
					// On misconfigured servers multiple mailboxes may claim the same
					// attribute; last one wins, consistent with Discover behavior.
					mb.Role = role
				}
			}
		}

		out = append(out, mb)
	}
	return out, nil
}

// MailboxStatus is the combined result of LIST + STATUS for one mailbox.
type MailboxStatus struct {
	Name          string
	Role          mail.FolderRole
	UIDValidity   uint32
	UIDNext       uint32
	NumMessages   uint32
	HighestModSeq uint64
}

// statusOpts is the set of STATUS items the sweep uses.
var statusOpts = &imap.StatusOptions{
	NumMessages:   true,
	UIDNext:       true,
	UIDValidity:   true,
	HighestModSeq: true,
}

// ListStatus returns all mailboxes with their current STATUS values. It uses
// one LIST-STATUS command when the server advertises CapListStatus; otherwise
// it issues individual STATUS commands per mailbox.
func ListStatus(c *imapclient.Client) ([]MailboxStatus, error) {
	opts := &imap.ListOptions{ReturnStatus: statusOpts}
	if c.Caps().Has(imap.CapSpecialUse) {
		opts.ReturnSpecialUse = true
	}

	var useListStatus bool
	caps := c.Caps()
	if caps.Has(imap.CapListStatus) {
		useListStatus = true
	}

	if useListStatus {
		data, err := c.List("", "*", opts).Collect()
		if err != nil {
			return nil, err
		}
		out := make([]MailboxStatus, 0, len(data))
		for _, d := range data {
			ms := MailboxStatus{Name: d.Mailbox}
			if strings.EqualFold(d.Mailbox, "INBOX") {
				ms.Role = mail.RoleInbox
			} else {
				for _, a := range d.Attrs {
					if role, ok := attrToRole[a]; ok {
						ms.Role = role
					}
				}
			}
			if d.Status != nil {
				ms.UIDValidity = d.Status.UIDValidity
				ms.UIDNext = uint32(d.Status.UIDNext)
				ms.HighestModSeq = d.Status.HighestModSeq
				if d.Status.NumMessages != nil {
					ms.NumMessages = *d.Status.NumMessages
				}
			}
			out = append(out, ms)
		}
		return out, nil
	}

	// Fallback: LIST then one STATUS per mailbox.
	mailboxes, err := List(c)
	if err != nil {
		return nil, err
	}
	out := make([]MailboxStatus, 0, len(mailboxes))
	for _, mb := range mailboxes {
		ms := MailboxStatus{Name: mb.Name, Role: mb.Role}
		sd, err := c.Status(mb.Name, statusOpts).Wait()
		if err != nil {
			// Non-fatal: skip this folder; sweep will catch it next cycle.
			continue
		}
		ms.UIDValidity = sd.UIDValidity
		ms.UIDNext = uint32(sd.UIDNext)
		ms.HighestModSeq = sd.HighestModSeq
		if sd.NumMessages != nil {
			ms.NumMessages = *sd.NumMessages
		}
		out = append(out, ms)
	}
	return out, nil
}

// Discover queries the server for SPECIAL-USE mailbox attributes (RFC 6154)
// and returns a map from each recognized role to the mailbox name the server
// assigned to it. Returns a nil map without error when the server does not
// advertise SPECIAL-USE support; callers should treat a nil map as "nothing
// discovered" and fall through to config/defaults.
func Discover(c *imapclient.Client) (map[mail.FolderRole]string, error) {
	mailboxes, err := List(c)
	if err != nil {
		return nil, err
	}

	out := map[mail.FolderRole]string{}
	for _, mb := range mailboxes {
		// Discover only returns non-inbox special-use roles; INBOX is excluded
		// to preserve the original semantics (RoleInbox never appeared in the map).
		if mb.Role != "" && mb.Role != mail.RoleInbox {
			out[mb.Role] = mb.Name
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// Resolve returns the mailbox name for role on acct. The resolution order is:
//  1. Per-account config override (e.g. acct.TrashMailbox)
//  2. SPECIAL-USE discovery result (from Discover, may be nil)
//  3. role.DefaultMailbox() — conventional fallback, never empty
//
// The returned string is always non-empty and safe to pass directly to an IMAP
// SELECT or MOVE command.
func Resolve(acct config.Account, discovered map[mail.FolderRole]string, role mail.FolderRole) string {
	if override := configOverride(acct, role); override != "" {
		return override
	}
	if discovered != nil {
		if name, ok := discovered[role]; ok {
			return name
		}
	}
	return role.DefaultMailbox()
}

// configOverride reads the per-role config field from acct. Returns "" when
// the user has not set an override for the given role.
func configOverride(acct config.Account, role mail.FolderRole) string {
	switch role {
	case mail.RoleInbox:
		return "" // INBOX is RFC-reserved; no override supported
	case mail.RoleSent:
		return acct.SentMailbox
	case mail.RoleDrafts:
		return acct.DraftsMailbox
	case mail.RoleArchive:
		return acct.ArchiveMailbox
	case mail.RoleTrash:
		return acct.TrashMailbox
	case mail.RoleJunk:
		return acct.JunkMailbox
	}
	return ""
}
