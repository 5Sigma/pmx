package compose

import (
	"strings"

	"github.com/5Sigma/pmx/internal/config"
	"github.com/5Sigma/pmx/internal/mail"
	uicompose "github.com/5Sigma/pmx/internal/ui/compose"
)

// BuildReply builds a Seed for a standard reply (reply to sender only).
// body is the plain-text body of src, already decoded (use the caller's
// text-first / html2text logic to produce it).
func BuildReply(src *mail.Message, acct config.Account, body string) *uicompose.Seed {
	subject := "Re: " + StripReplyPrefixes(src.Subject)
	return &uicompose.Seed{
		From:       acct.Email,
		To:         []mail.Address{src.From},
		Subject:    subject,
		Body:       QuoteReply(src, body),
		InReplyTo:  src.MessageID,
		References: dedupeRefs(append(src.References, src.MessageID)),
	}
}

// BuildReplyAll builds a Seed for reply-all.
// To = original sender; Cc = original To+CC minus self and original sender.
func BuildReplyAll(src *mail.Message, acct config.Account, body string) *uicompose.Seed {
	subject := "Re: " + StripReplyPrefixes(src.Subject)
	to, cc := replyAllRecipients(src, acct)
	return &uicompose.Seed{
		From:       acct.Email,
		To:         to,
		Cc:         cc,
		Subject:    subject,
		Body:       QuoteReply(src, body),
		InReplyTo:  src.MessageID,
		References: dedupeRefs(append(src.References, src.MessageID)),
	}
}

// replyAllRecipients computes To and Cc for a reply-all.
// To: the original sender (if non-empty email).
// Cc: everyone in src.To + src.CC except self (acct.Email) and src.From.
func replyAllRecipients(src *mail.Message, acct config.Account) (to, cc []mail.Address) {
	if src.From.Email != "" {
		to = []mail.Address{src.From}
	}
	seen := map[string]bool{
		strings.ToLower(acct.Email):     true,
		strings.ToLower(src.From.Email): true,
	}
	for _, a := range append(src.To, src.CC...) {
		k := strings.ToLower(a.Email)
		if seen[k] {
			continue
		}
		seen[k] = true
		cc = append(cc, a)
	}
	return
}

// dedupeRefs removes duplicate message-IDs, preserving order (first occurrence wins).
func dedupeRefs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
