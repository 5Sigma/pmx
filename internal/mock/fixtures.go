package mock

import (
	"bufio"
	"bytes"
	"embed"
	"io"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

//go:embed testdata/*.eml
var fixtureFS embed.FS

// literalReader wraps a []byte so it satisfies imap.LiteralReader.
type literalReader struct {
	*bytes.Reader
}

func (l literalReader) Size() int64 { return int64(l.Len()) }

// Seed loads all .eml fixtures from the embedded testdata directory into the
// given user's mailboxes. The X-PMX-Mock-Mailbox header in each file
// determines the destination; unknown mailboxes are skipped silently.
func Seed(u *imapmemserver.User) error {
	entries, err := fixtureFS.ReadDir("testdata")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".eml") {
			continue
		}
		data, err := fixtureFS.ReadFile("testdata/" + e.Name())
		if err != nil {
			return err
		}
		mailbox, flags, body := parseFixture(data)
		lr := literalReader{bytes.NewReader(body)}
		if _, err := u.Append(mailbox, lr, &imap.AppendOptions{
			Flags: flags,
			Time:  time.Now(),
		}); err != nil {
			// Skip mailboxes that don't exist rather than failing the whole seed.
			continue
		}
	}
	return nil
}

// parseFixture reads the custom X-PMX-Mock-* headers from the leading header
// block, strips them, and returns the routing info plus clean message bytes.
func parseFixture(data []byte) (mailbox string, flags []imap.Flag, body []byte) {
	mailbox = "INBOX"
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(data)))
	h, err := r.ReadMIMEHeader()
	if err != nil && err != io.EOF {
		return mailbox, nil, data
	}
	if mb := h.Get("X-Pmx-Mock-Mailbox"); mb != "" {
		mailbox = strings.TrimSpace(mb)
	}
	for _, f := range strings.Fields(h.Get("X-Pmx-Mock-Flags")) {
		flags = append(flags, imap.Flag(f))
	}
	body = stripMockHeaders(data)
	return
}

// stripMockHeaders rewrites the raw message bytes, dropping any lines whose
// header name starts with "x-pmx-mock-". This produces a clean RFC 5322
// message for storage.
func stripMockHeaders(data []byte) []byte {
	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.ToLower(line), "x-pmx-mock-") {
			continue
		}
		out.WriteString(line + "\r\n")
	}
	return out.Bytes()
}
