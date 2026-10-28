package mailsend

import (
	"bytes"
	"fmt"
	"strings"

	gomail "github.com/emersion/go-message/mail"

	"github.com/5Sigma/pmx/internal/mail"
)

// extractEnvelope returns the SMTP envelope sender and recipient list from a
// stored EML. All of To, Cc, and Bcc headers are included in rcpts; addresses
// are deduped case-insensitively.
func extractEnvelope(raw []byte) (from string, rcpts []string, err error) {
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return "", nil, fmt.Errorf("parse EML: %w", err)
	}
	h := r.Header

	fromAddrs, _ := h.AddressList("From")
	if len(fromAddrs) == 0 {
		return "", nil, fmt.Errorf("EML missing From header")
	}
	from = fromAddrs[0].Address

	seen := map[string]bool{}
	for _, hdr := range []string{"To", "Cc", "Bcc"} {
		addrs, _ := h.AddressList(hdr)
		for _, a := range addrs {
			key := strings.ToLower(a.Address)
			if !seen[key] {
				seen[key] = true
				rcpts = append(rcpts, a.Address)
			}
		}
	}
	if len(rcpts) == 0 {
		return "", nil, fmt.Errorf("EML has no recipients")
	}
	return from, rcpts, nil
}

// extractAddressedParties parses To, Cc, Bcc from a raw EML and returns them
// as mail.Address slices with display names preserved.
func extractAddressedParties(raw []byte) (to, cc, bcc []mail.Address, err error) {
	msg, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse EML: %w", err)
	}
	h := msg.Header
	to, _ = parseAddressHeader(h, "To")
	cc, _ = parseAddressHeader(h, "Cc")
	bcc, _ = parseAddressHeader(h, "Bcc")
	return to, cc, bcc, nil
}

// parseAddressHeader parses a named address-list header and converts the
// entries to mail.Address (pmx domain type).
func parseAddressHeader(h gomail.Header, name string) ([]mail.Address, error) {
	addrs, err := h.AddressList(name)
	if err != nil || len(addrs) == 0 {
		return nil, err
	}
	out := make([]mail.Address, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, mail.Address{Name: a.Name, Email: a.Address})
	}
	return out, nil
}
