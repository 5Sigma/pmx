package mcpserver

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
)

// AccountJSON is the safe public shape of a config.Account (no credentials).
type AccountJSON struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

// FolderJSON is the public shape of a folder with live counts.
type FolderJSON struct {
	Name   string `json:"name"`
	Role   string `json:"role,omitempty"`
	Unread int    `json:"unread"`
	Total  int    `json:"total"`
}

// AddressJSON mirrors mail.Address for JSON output.
type AddressJSON struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// AttachmentMetaJSON is attachment metadata without bytes.
type AttachmentMetaJSON struct {
	ID       int64  `json:"id"`
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
	Size     int64  `json:"size_bytes"`
}

// MessageJSON is the public shape of a message.
type MessageJSON struct {
	Account     string               `json:"account"`
	Folder      string               `json:"folder"`
	UID         uint32               `json:"uid"`
	MessageID   string               `json:"message_id,omitempty"`
	InReplyTo   string               `json:"in_reply_to,omitempty"`
	Subject     string               `json:"subject"`
	From        AddressJSON          `json:"from"`
	To          []AddressJSON        `json:"to"`
	CC          []AddressJSON        `json:"cc,omitempty"`
	BCC         []AddressJSON        `json:"bcc,omitempty"`
	Date        string               `json:"date"`
	Unread      bool                 `json:"unread"`
	Flagged     bool                 `json:"flagged"`
	HasAttach   bool                 `json:"has_attachments"`
	BodyText    string               `json:"body_text,omitempty"`
	BodyHTML    string               `json:"body_html,omitempty"`
	Attachments []AttachmentMetaJSON `json:"attachments,omitempty"`
}

// SearchResultJSON is a compact message summary for search results.
type SearchResultJSON struct {
	Account string      `json:"account"`
	Folder  string      `json:"folder"`
	UID     uint32      `json:"uid"`
	Subject string      `json:"subject"`
	From    AddressJSON `json:"from"`
	Date    string      `json:"date"`
	Snippet string      `json:"snippet"`
	Unread  bool        `json:"unread"`
	Flagged bool        `json:"flagged"`
}

// AttachmentGetJSON is the result of get_attachment.
type AttachmentGetJSON struct {
	Filename  string `json:"filename"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Base64    string `json:"base64,omitempty"`
	Path      string `json:"path,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// SendResultJSON is the result of compose_message / reply / forward.
type SendResultJSON struct {
	Status    string `json:"status"`
	MessageID string `json:"message_id,omitempty"`
}

const attachmentSizeCap = 10 * 1024 * 1024 // 10 MB

func toAccountJSON(a config.Account) AccountJSON {
	return AccountJSON{Name: a.Name, Email: a.Email, FullName: a.FullName}
}

func toAddressJSON(a mail.Address) AddressJSON {
	return AddressJSON{Name: a.Name, Email: a.Email}
}

func toAddressesJSON(addrs []mail.Address) []AddressJSON {
	out := make([]AddressJSON, len(addrs))
	for i, a := range addrs {
		out[i] = toAddressJSON(a)
	}
	return out
}

func toAttachMetaJSON(a mail.Attachment) AttachmentMetaJSON {
	return AttachmentMetaJSON{
		ID:       a.ID,
		Filename: a.Filename,
		MIMEType: a.MIMEType,
		Size:     a.SizeBytes,
	}
}

func toMessageJSON(msg *mail.Message, accountName, folderName, bodyFormat string) MessageJSON {
	mj := MessageJSON{
		Account:   accountName,
		Folder:    folderName,
		UID:       msg.UID,
		MessageID: msg.MessageID,
		InReplyTo: msg.InReplyTo,
		Subject:   msg.Subject,
		From:      toAddressJSON(msg.From),
		To:        toAddressesJSON(msg.To),
		CC:        toAddressesJSON(msg.CC),
		BCC:       toAddressesJSON(msg.BCC),
		Date:      msg.DateSent.Format("2006-01-02T15:04:05Z07:00"),
		Unread:    !msg.IsRead(),
		Flagged:   msg.IsFlagged(),
		HasAttach: msg.HasAttach,
	}
	switch bodyFormat {
	case "html":
		mj.BodyHTML = msg.BodyHTML
	case "both":
		mj.BodyText = msg.BodyText
		mj.BodyHTML = msg.BodyHTML
	default: // "text" or unspecified
		mj.BodyText = msg.BodyText
	}
	for _, a := range msg.Attachments {
		mj.Attachments = append(mj.Attachments, toAttachMetaJSON(a))
	}
	return mj
}

func toSearchResultJSON(msg *mail.Message, accountName, folderName string) SearchResultJSON {
	snippet := msg.BodyText
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	return SearchResultJSON{
		Account: accountName,
		Folder:  folderName,
		UID:     msg.UID,
		Subject: msg.Subject,
		From:    toAddressJSON(msg.From),
		Date:    msg.DateSent.Format("2006-01-02T15:04:05Z07:00"),
		Snippet: snippet,
		Unread:  !msg.IsRead(),
		Flagged: msg.IsFlagged(),
	}
}

// resolveAccount looks up a config.Account by name and returns it along with
// its DB row ID.
func resolveAccount(cfg *config.Config, st mail.MessageStore, name string) (config.Account, int64, error) {
	acct := cfg.AccountByName(name)
	if acct == nil {
		return config.Account{}, 0, fmt.Errorf("account %q not found", name)
	}
	id, err := st.EnsureAccount(acct.Name, acct.Email)
	if err != nil {
		return config.Account{}, 0, fmt.Errorf("ensure account: %w", err)
	}
	return *acct, id, nil
}

// resolveFolder returns the Folder for the given account. role takes priority
// when non-empty; falls back to folderName lookup. Returns an error when
// neither resolves.
func resolveFolder(st mail.MessageStore, accountID int64, folderName, roleName string) (*mail.Folder, error) {
	if roleName != "" {
		f, err := st.FolderByRole(accountID, mail.FolderRole(roleName))
		if err != nil {
			return nil, fmt.Errorf("folder by role: %w", err)
		}
		if f == nil {
			return nil, fmt.Errorf("no folder found for role %q", roleName)
		}
		return f, nil
	}
	if folderName != "" {
		f, err := st.FolderByName(accountID, folderName)
		if err != nil {
			return nil, fmt.Errorf("folder by name: %w", err)
		}
		if f == nil {
			return nil, fmt.Errorf("folder %q not found", folderName)
		}
		return f, nil
	}
	return nil, fmt.Errorf("either folder or role is required")
}

// readAttachmentBytes reads the attachment file and returns base64 content or
// the on-disk path when the file is larger than attachmentSizeCap.
func readAttachmentBytes(path string, size int64) (b64 string, truncated bool, err error) {
	if size > attachmentSizeCap {
		return "", true, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read attachment: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), false, nil
}

// writeTempAttachment writes data to a temp file in dir (falls back to os.TempDir).
// Returns the temp file path; the caller must remove it when done.
func writeTempAttachment(dir, filename string, data []byte) (string, error) {
	pattern := "mcp-attach-*-" + filepath.Base(filename)
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		f, err = os.CreateTemp("", pattern)
		if err != nil {
			return "", fmt.Errorf("create temp file: %w", err)
		}
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write temp file: %w", err)
	}
	return path, f.Close()
}
