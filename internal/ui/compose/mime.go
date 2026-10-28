package compose

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	gomail "github.com/emersion/go-message/mail"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"

	"github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/ui/compose/themes"
)

// BuildOpts holds all inputs required to construct a RFC 5322 EML.
type BuildOpts struct {
	FromAddr, FromName string
	To, Cc, Bcc        []mail.Address
	Subject, Body      string
	UseMarkdown        bool
	CSS                string
	Attachments        []Attachment
	// InReplyTo is the bare message-id (no angle brackets) of the message
	// being replied to. Empty string omits the header.
	InReplyTo string
	// References is the ordered list of bare message-ids for the thread chain.
	// Nil or empty omits the References header.
	References []string
}

// BuildEML constructs a raw RFC 5322 EML from opts.
// When UseMarkdown is true the body is stored as multipart/alternative
// with text/plain (raw markdown) + text/html (rendered). CSS is the
// stylesheet to inline into the HTML part; pass "" for no styling.
// Attachments may be nil or empty for no attachments.
func BuildEML(opts BuildOpts) ([]byte, error) {
	var buf bytes.Buffer

	var h gomail.Header
	h.SetAddressList("From", []*gomail.Address{{Address: opts.FromAddr, Name: opts.FromName}})
	h.SetAddressList("To", toGoMailAddrs(opts.To))
	if len(opts.Cc) > 0 {
		h.SetAddressList("Cc", toGoMailAddrs(opts.Cc))
	}
	if len(opts.Bcc) > 0 {
		h.SetAddressList("Bcc", toGoMailAddrs(opts.Bcc))
	}
	h.SetSubject(opts.Subject)
	h.SetDate(time.Now())
	h.Set("Message-ID", newMessageID(opts.FromAddr))
	if opts.InReplyTo != "" {
		h.Set("In-Reply-To", "<"+opts.InReplyTo+">")
	}
	if len(opts.References) > 0 {
		parts := make([]string, len(opts.References))
		for i, id := range opts.References {
			parts[i] = "<" + id + ">"
		}
		h.Set("References", strings.Join(parts, " "))
	}

	w, err := gomail.CreateWriter(&buf, h)
	if err != nil {
		return nil, fmt.Errorf("create mail writer: %w", err)
	}

	if opts.UseMarkdown {
		iw, err := w.CreateInline()
		if err != nil {
			return nil, fmt.Errorf("create inline writer: %w", err)
		}

		// Part 1: text/plain (raw markdown)
		var ph1 gomail.InlineHeader
		ph1.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		pw1, err := iw.CreatePart(ph1)
		if err != nil {
			return nil, fmt.Errorf("create plain part: %w", err)
		}
		if _, err := pw1.Write([]byte(opts.Body)); err != nil {
			return nil, fmt.Errorf("write plain body: %w", err)
		}
		if err := pw1.Close(); err != nil {
			return nil, err
		}

		// Part 2: text/html (rendered markdown)
		var ph2 gomail.InlineHeader
		ph2.SetContentType("text/html", map[string]string{"charset": "utf-8"})
		pw2, err := iw.CreatePart(ph2)
		if err != nil {
			return nil, fmt.Errorf("create html part: %w", err)
		}
		htmlBytes, err := renderMarkdownHTML(opts.Body, opts.CSS)
		if err != nil {
			return nil, fmt.Errorf("render markdown: %w", err)
		}
		if _, err := pw2.Write(htmlBytes); err != nil {
			return nil, fmt.Errorf("write html body: %w", err)
		}
		if err := pw2.Close(); err != nil {
			return nil, err
		}

		if err := iw.Close(); err != nil {
			return nil, err
		}
	} else {
		var ih gomail.InlineHeader
		ih.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		bw, err := w.CreateSingleInline(ih)
		if err != nil {
			return nil, fmt.Errorf("create body writer: %w", err)
		}
		if _, err := bw.Write([]byte(opts.Body)); err != nil {
			return nil, fmt.Errorf("write body: %w", err)
		}
		if err := bw.Close(); err != nil {
			return nil, err
		}
	}

	for _, a := range opts.Attachments {
		if err := appendAttachment(w, a); err != nil {
			return nil, err
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close mail writer: %w", err)
	}
	return buf.Bytes(), nil
}

// toGoMailAddrs converts a slice of mail.Address to the go-message type.
func toGoMailAddrs(addrs []mail.Address) []*gomail.Address {
	out := make([]*gomail.Address, len(addrs))
	for i, a := range addrs {
		out[i] = &gomail.Address{Address: a.Email, Name: a.Name}
	}
	return out
}

// newMessageID generates a unique RFC 5322 Message-ID header value. The domain
// is taken from the sender address; a random 4-byte hex suffix is appended so
// IDs remain unique within the same nanosecond.
func newMessageID(fromAddr string) string {
	domain := "pmx"
	if at := strings.LastIndex(fromAddr, "@"); at >= 0 && at < len(fromAddr)-1 {
		domain = fromAddr[at+1:]
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b) // crypto/rand; discard error — zero bytes are still unique enough
	return fmt.Sprintf("<%d.%x@%s>", time.Now().UnixNano(), b, domain)
}

// appendAttachment opens the file at a.Path, streams it into a MIME attachment
// part, and closes both the writer and the file. The caller's EML build is
// aborted on any error — file-not-found, permission denied, or write failure.
func appendAttachment(w *gomail.Writer, a Attachment) error {
	f, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("open attachment %q: %w", a.Filename, err)
	}
	defer func() { _ = f.Close() }()

	var ah gomail.AttachmentHeader
	ah.SetContentType(a.MIMEType, nil)
	ah.SetFilename(a.Filename)

	pw, err := w.CreateAttachment(ah)
	if err != nil {
		return fmt.Errorf("create attachment part %q: %w", a.Filename, err)
	}
	if _, err := io.Copy(pw, f); err != nil {
		_ = pw.Close()
		return fmt.Errorf("write attachment %q: %w", a.Filename, err)
	}
	return pw.Close()
}

func renderMarkdownHTML(src, css string) ([]byte, error) {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs
	p := parser.NewWithExtensions(extensions)
	flags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: flags}
	renderer := html.NewRenderer(opts)
	bodyHTML := markdown.ToHTML([]byte(src), p, renderer)
	result, err := themes.Apply(css, string(bodyHTML))
	if err != nil {
		// Fallback to raw HTML fragment on error — never block a send.
		return bodyHTML, nil
	}
	return []byte(result), nil
}

// ParseFromDisplay splits a "Name <email@host>" display string into name and address.
// If the input is just an email address, name is empty.
func ParseFromDisplay(display string) (name, addr string) {
	display = strings.TrimSpace(display)
	if i := strings.Index(display, "<"); i >= 0 {
		name = strings.TrimSpace(display[:i])
		addr = strings.Trim(display[i+1:], "> ")
		return
	}
	return "", display
}
