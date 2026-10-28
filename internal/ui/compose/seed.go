package compose

import "github.com/5Sigma/pmx/internal/mail"

// Seed carries prefill data for reply, reply-all, and forward workflows.
type Seed struct {
	From        string // account email to preselect; "" = use default
	To, Cc, Bcc []mail.Address
	Subject     string
	Body        string       // initial editor content (quoted reply / forward body)
	Attachments []Attachment // compose.Attachment, not mail.Attachment
	InReplyTo   string       // RFC 5322 Message-ID of parent, bare (no angle brackets)
	References  []string     // RFC 5322 References chain, bare IDs
}
