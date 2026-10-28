package compose

import (
	"fmt"
	"strings"

	"github.com/5Sigma/pmx/internal/mail"
)

// QuoteReply builds the editor seed body for a reply:
//
//	<blank line>
//	<blank line>
//	On Jan 2, 2006 at 3:04 PM, Alice <a@x> wrote:
//	> quoted body line 1
//	> quoted body line 2
//
// Cursor sits above the quote because the body starts with two blank lines.
func QuoteReply(src *mail.Message, body string) string {
	date := src.DateSent.Format("Jan 2, 2006 at 3:04 PM")
	name := src.From.Name
	if name == "" {
		name = src.From.Email
	}
	sender := fmt.Sprintf("%s <%s>", name, src.From.Email)
	header := fmt.Sprintf("On %s, %s wrote:", date, sender)
	quoted := "> " + strings.ReplaceAll(strings.TrimSpace(body), "\n", "\n> ")
	return "\n\n" + header + "\n" + quoted + "\n"
}

// QuoteForward builds the editor seed body for a forward.
func QuoteForward(src *mail.Message, body string) string {
	const sep = "---------- Forwarded message ----------"
	from := src.From.Name
	if from != "" {
		from = fmt.Sprintf("%s <%s>", src.From.Name, src.From.Email)
	} else {
		from = src.From.Email
	}
	meta := fmt.Sprintf("From: %s\nDate: %s\nSubject: %s\nTo: %s",
		from,
		src.DateSent.Format("Mon, 02 Jan 2006 15:04:05 -0700"),
		src.Subject,
		formatAddresses(src.To),
	)
	return "\n\n" + sep + "\n" + meta + "\n\n" + strings.TrimSpace(body) + "\n"
}

// formatAddresses renders a []mail.Address as "Name <email>, ..." for the header block.
func formatAddresses(addrs []mail.Address) string {
	parts := make([]string, len(addrs))
	for i, a := range addrs {
		if a.Name != "" {
			parts[i] = fmt.Sprintf("%s <%s>", a.Name, a.Email)
		} else {
			parts[i] = a.Email
		}
	}
	return strings.Join(parts, ", ")
}
