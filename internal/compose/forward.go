package compose

import (
	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	uicompose "github.com/5Sigma/pmx/internal/ui/compose"
)

// BuildForward builds a Seed for a forwarded message.
// attachPath is a function that returns the disk path for a given attachment
// (typically store.AttachmentPath). Pass nil to skip attachment carry-over.
func BuildForward(src *mail.Message, acct config.Account, body string,
	attachPath func(mail.Attachment) string) *uicompose.Seed {

	subject := "Fwd: " + StripAllPrefixes(src.Subject)

	var atts []uicompose.Attachment
	if attachPath != nil {
		for _, a := range src.Attachments {
			path := attachPath(a)
			if path == "" {
				continue
			}
			atts = append(atts, uicompose.Attachment{
				Path:     path,
				Filename: a.Filename,
				MIMEType: a.MIMEType,
				Size:     a.SizeBytes,
			})
		}
	}

	return &uicompose.Seed{
		From:        acct.Email,
		Subject:     subject,
		Body:        QuoteForward(src, body),
		Attachments: atts,
		InReplyTo:   src.MessageID,
		References:  dedupeRefs(append(src.References, src.MessageID)),
	}
}
