package mailsend

import (
	"bytes"
	"fmt"
	"strings"

	gomail "github.com/emersion/go-message/mail"
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
