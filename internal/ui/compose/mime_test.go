package compose

import (
	"bytes"
	"io"
	"net/mail"
	"os"
	"strings"
	"testing"

	gomail "github.com/emersion/go-message/mail"

	pmxmail "github.com/5Sigma/pmx/internal/mail"
)

// parsedPart holds the content-type, filename, and body bytes for a single
// MIME part. The body must be read before the next call to NextPart, so we
// materialise it immediately rather than deferring.
type parsedPart struct {
	contentType  string
	filename     string
	isAttachment bool
	body         []byte
}

// collectParts drains all parts from a Reader, eagerly reading each body.
// go-message discards a part's body as soon as NextPart is called again, so
// all reads happen inside the iteration loop.
func collectParts(t *testing.T, r *gomail.Reader) []parsedPart {
	t.Helper()
	var parts []parsedPart
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		ct := partContentType(t, p)
		fn := partFilename(p)
		attach := isAttachment(p)
		b, readErr := io.ReadAll(p.Body)
		if readErr != nil {
			t.Fatalf("read part body: %v", readErr)
		}
		parts = append(parts, parsedPart{
			contentType:  ct,
			filename:     fn,
			isAttachment: attach,
			body:         b,
		})
	}
	return parts
}

// partContentType extracts the media type (without parameters) from a Part header.
func partContentType(t *testing.T, p *gomail.Part) string {
	t.Helper()
	switch h := p.Header.(type) {
	case *gomail.InlineHeader:
		ct, _, err := h.ContentType()
		if err != nil {
			t.Fatalf("InlineHeader.ContentType: %v", err)
		}
		return ct
	case *gomail.AttachmentHeader:
		ct, _, err := h.ContentType()
		if err != nil {
			t.Fatalf("AttachmentHeader.ContentType: %v", err)
		}
		return ct
	default:
		t.Fatalf("unexpected header type %T", p.Header)
		return ""
	}
}

// partFilename extracts the attachment filename; returns "" for inline parts.
func partFilename(p *gomail.Part) string {
	ah, ok := p.Header.(*gomail.AttachmentHeader)
	if !ok {
		return ""
	}
	name, _ := ah.Filename()
	return name
}

// isAttachment reports whether the part has an AttachmentHeader.
func isAttachment(p *gomail.Part) bool {
	_, ok := p.Header.(*gomail.AttachmentHeader)
	return ok
}

// parseHeaders parses the top-level headers of an EML using net/mail, which
// gives us easy access to raw header values without needing a full MIME walk.
func parseHeaders(t *testing.T, eml []byte) mail.Header {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(eml))
	if err != nil {
		t.Fatalf("net/mail.ReadMessage: %v", err)
	}
	return msg.Header
}

// ---- TestBuildEML_PlainText -------------------------------------------------

func TestBuildEML_PlainText(t *testing.T) {
	eml, err := BuildEML(BuildOpts{
		FromAddr: "alice@example.com",
		FromName: "Alice",
		To:       []pmxmail.Address{{Email: "bob@example.com"}},
		Subject:  "Hello",
		Body:     "plain body",
	})
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}

	r, err := gomail.CreateReader(bytes.NewReader(eml))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}
	defer func() { _ = r.Close() }()

	t.Run("From header contains sender address", func(t *testing.T) {
		addrs, err := r.Header.AddressList("From")
		if err != nil {
			t.Fatalf("AddressList(From): %v", err)
		}
		if len(addrs) == 0 {
			t.Fatal("From header is empty")
		}
		if !strings.Contains(addrs[0].Address, "alice@example.com") {
			t.Errorf("From address: want to contain %q, got %q", "alice@example.com", addrs[0].Address)
		}
	})

	t.Run("Subject header matches", func(t *testing.T) {
		subj, err := r.Header.Subject()
		if err != nil {
			t.Fatalf("Subject: %v", err)
		}
		if subj != "Hello" {
			t.Errorf("Subject: want %q, got %q", "Hello", subj)
		}
	})

	parts := collectParts(t, r)
	if len(parts) != 1 {
		t.Fatalf("want 1 body part, got %d", len(parts))
	}

	t.Run("single part is text/plain", func(t *testing.T) {
		if parts[0].contentType != "text/plain" {
			t.Errorf("content-type: want %q, got %q", "text/plain", parts[0].contentType)
		}
	})

	t.Run("body content contains expected text", func(t *testing.T) {
		if !strings.Contains(string(parts[0].body), "plain body") {
			t.Errorf("body does not contain %q; got: %q", "plain body", string(parts[0].body))
		}
	})
}

// ---- TestBuildEML_Multipart -------------------------------------------------

func TestBuildEML_Multipart(t *testing.T) {
	eml, err := BuildEML(BuildOpts{
		FromAddr:    "alice@example.com",
		FromName:    "Alice",
		To:          []pmxmail.Address{{Email: "bob@example.com"}},
		Subject:     "Markdown Test",
		Body:        "**bold**",
		UseMarkdown: true,
	})
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}

	r, err := gomail.CreateReader(bytes.NewReader(eml))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}
	defer func() { _ = r.Close() }()

	parts := collectParts(t, r)
	if len(parts) != 2 {
		t.Fatalf("want 2 body parts (text/plain + text/html), got %d", len(parts))
	}

	t.Run("part 1 is text/plain containing raw markdown", func(t *testing.T) {
		if parts[0].contentType != "text/plain" {
			t.Errorf("part 1 content-type: want %q, got %q", "text/plain", parts[0].contentType)
		}
		if !strings.Contains(string(parts[0].body), "**bold**") {
			t.Errorf("part 1 body does not contain raw markdown %q; got: %q", "**bold**", string(parts[0].body))
		}
	})

	t.Run("part 2 is text/html containing rendered markup", func(t *testing.T) {
		if parts[1].contentType != "text/html" {
			t.Errorf("part 2 content-type: want %q, got %q", "text/html", parts[1].contentType)
		}
		if !strings.Contains(string(parts[1].body), "<strong>") {
			t.Errorf("part 2 body does not contain <strong> tag; got: %q", string(parts[1].body))
		}
	})
}

// ---- TestBuildEML_MarkdownWithTheme ----------------------------------------

func TestBuildEML_MarkdownWithTheme(t *testing.T) {
	css := "p { color: red; }"
	eml, err := BuildEML(BuildOpts{
		FromAddr:    "from@example.com",
		FromName:    "Sender",
		To:          []pmxmail.Address{{Email: "to@example.com"}},
		Subject:     "Themed",
		Body:        "**bold**",
		UseMarkdown: true,
		CSS:         css,
	})
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}

	r, err := gomail.CreateReader(bytes.NewReader(eml))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}
	defer func() { _ = r.Close() }()

	parts := collectParts(t, r)
	if len(parts) != 2 {
		t.Fatalf("want 2 body parts, got %d", len(parts))
	}

	htmlPart := parts[1]
	if htmlPart.contentType != "text/html" {
		t.Fatalf("part 2 content-type: want %q, got %q", "text/html", htmlPart.contentType)
	}
	if !strings.Contains(string(htmlPart.body), "style=") {
		t.Errorf("HTML part: CSS was not inlined — want style= attribute; got body: %q", string(htmlPart.body))
	}
}

// ---- TestBuildEML_ErrorPaths ------------------------------------------------

func TestBuildEML_EmptyFields(t *testing.T) {
	cases := []struct {
		name        string
		from        string
		fromName    string
		to          string
		subject     string
		body        string
		useMarkdown bool
	}{
		{
			name:    "empty subject is allowed",
			from:    "a@example.com",
			to:      "b@example.com",
			subject: "",
			body:    "hello",
		},
		{
			name:    "empty body is allowed",
			from:    "a@example.com",
			to:      "b@example.com",
			subject: "Sub",
			body:    "",
		},
		{
			name:        "empty markdown body produces valid multipart",
			from:        "a@example.com",
			to:          "b@example.com",
			subject:     "Sub",
			body:        "",
			useMarkdown: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eml, err := BuildEML(BuildOpts{
				FromAddr:    tc.from,
				FromName:    tc.fromName,
				To:          []pmxmail.Address{{Email: tc.to}},
				Subject:     tc.subject,
				Body:        tc.body,
				UseMarkdown: tc.useMarkdown,
			})
			if err != nil {
				t.Fatalf("BuildEML: unexpected error: %v", err)
			}
			if len(eml) == 0 {
				t.Error("BuildEML returned empty byte slice")
			}
		})
	}
}

// ---- TestBuildEML_WithAttachments ------------------------------------------

func TestBuildEML_WithAttachments(t *testing.T) {
	// Create two temporary files to use as attachments.
	txtFile, err := os.CreateTemp(t.TempDir(), "attach-*.txt")
	if err != nil {
		t.Fatalf("create temp txt: %v", err)
	}
	txtContent := []byte("hello from attachment one")
	if _, err := txtFile.Write(txtContent); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	_ = txtFile.Close()

	binFile, err := os.CreateTemp(t.TempDir(), "attach-*.bin")
	if err != nil {
		t.Fatalf("create temp bin: %v", err)
	}
	binContent := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03}
	if _, err := binFile.Write(binContent); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	_ = binFile.Close()

	attachments := []Attachment{
		{Path: txtFile.Name(), Filename: "notes.txt", MIMEType: "text/plain", Size: int64(len(txtContent))},
		{Path: binFile.Name(), Filename: "data.bin", MIMEType: "application/octet-stream", Size: int64(len(binContent))},
	}

	eml, err := BuildEML(BuildOpts{
		FromAddr:    "a@example.com",
		FromName:    "Alice",
		To:          []pmxmail.Address{{Email: "b@example.com"}},
		Subject:     "With Attachments",
		Body:        "body text",
		Attachments: attachments,
	})
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}

	r, err := gomail.CreateReader(bytes.NewReader(eml))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}
	defer func() { _ = r.Close() }()

	parts := collectParts(t, r)
	// Expect: 1 text/plain body + 2 attachments = 3 parts.
	if len(parts) != 3 {
		t.Fatalf("want 3 parts (body + 2 attachments), got %d", len(parts))
	}

	t.Run("first part is inline body", func(t *testing.T) {
		if parts[0].isAttachment {
			t.Error("part[0] should be inline, got attachment")
		}
		if parts[0].contentType != "text/plain" {
			t.Errorf("part[0] content-type: want text/plain, got %q", parts[0].contentType)
		}
		if !strings.Contains(string(parts[0].body), "body text") {
			t.Errorf("part[0] body: want to contain %q, got %q", "body text", string(parts[0].body))
		}
	})

	t.Run("second part is first attachment", func(t *testing.T) {
		if !parts[1].isAttachment {
			t.Error("part[1] should be attachment")
		}
		if parts[1].filename != "notes.txt" {
			t.Errorf("part[1] filename: want %q, got %q", "notes.txt", parts[1].filename)
		}
		if !bytes.Equal(bytes.TrimSpace(parts[1].body), txtContent) {
			t.Errorf("part[1] body mismatch: want %q, got %q", txtContent, parts[1].body)
		}
	})

	t.Run("third part is second attachment", func(t *testing.T) {
		if !parts[2].isAttachment {
			t.Error("part[2] should be attachment")
		}
		if parts[2].filename != "data.bin" {
			t.Errorf("part[2] filename: want %q, got %q", "data.bin", parts[2].filename)
		}
		if !bytes.Equal(parts[2].body, binContent) {
			t.Errorf("part[2] body mismatch: want %v, got %v", binContent, parts[2].body)
		}
	})
}

// TestBuildEML_NoAttachments_Regression verifies nil attachments produce the
// same MIME structure as before the attachment feature was added.
func TestBuildEML_NoAttachments_Regression(t *testing.T) {
	eml, err := BuildEML(BuildOpts{
		FromAddr: "a@example.com",
		FromName: "A",
		To:       []pmxmail.Address{{Email: "b@example.com"}},
		Subject:  "Regression",
		Body:     "hello",
	})
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}
	r, err := gomail.CreateReader(bytes.NewReader(eml))
	if err != nil {
		t.Fatalf("CreateReader: %v", err)
	}
	defer func() { _ = r.Close() }()

	parts := collectParts(t, r)
	if len(parts) != 1 {
		t.Fatalf("want 1 part for plain text with no attachments, got %d", len(parts))
	}
	if parts[0].contentType != "text/plain" {
		t.Errorf("want text/plain, got %q", parts[0].contentType)
	}
}

// TestBuildEML_AttachmentFileNotFound verifies BuildEML returns an error when
// an attachment path doesn't exist, preventing a partial EML from being queued.
func TestBuildEML_AttachmentFileNotFound(t *testing.T) {
	attachments := []Attachment{
		{Path: "/nonexistent/path/ghost.pdf", Filename: "ghost.pdf", MIMEType: "application/pdf"},
	}
	_, err := BuildEML(BuildOpts{
		FromAddr:    "a@example.com",
		FromName:    "A",
		To:          []pmxmail.Address{{Email: "b@example.com"}},
		Subject:     "Ghost",
		Body:        "body",
		Attachments: attachments,
	})
	if err == nil {
		t.Fatal("expected error for missing attachment file, got nil")
	}
}

// ---- TestBuildEML_ThreadingHeaders -----------------------------------------

func TestBuildEML_ThreadingHeaders(t *testing.T) {
	t.Run("InReplyTo header present when set", func(t *testing.T) {
		eml, err := BuildEML(BuildOpts{
			FromAddr:  "a@example.com",
			To:        []pmxmail.Address{{Email: "b@example.com"}},
			Subject:   "Re: Hello",
			Body:      "reply body",
			InReplyTo: "msgid123",
		})
		if err != nil {
			t.Fatalf("BuildEML: %v", err)
		}
		hdr := parseHeaders(t, eml)
		got := hdr.Get("In-Reply-To")
		if got != "<msgid123>" {
			t.Errorf("In-Reply-To: want %q, got %q", "<msgid123>", got)
		}
	})

	t.Run("InReplyTo header absent when empty", func(t *testing.T) {
		eml, err := BuildEML(BuildOpts{
			FromAddr: "a@example.com",
			To:       []pmxmail.Address{{Email: "b@example.com"}},
			Subject:  "Hello",
			Body:     "body",
		})
		if err != nil {
			t.Fatalf("BuildEML: %v", err)
		}
		hdr := parseHeaders(t, eml)
		if got := hdr.Get("In-Reply-To"); got != "" {
			t.Errorf("In-Reply-To: want empty, got %q", got)
		}
	})

	t.Run("References header with multiple IDs", func(t *testing.T) {
		eml, err := BuildEML(BuildOpts{
			FromAddr:   "a@example.com",
			To:         []pmxmail.Address{{Email: "b@example.com"}},
			Subject:    "Re: Thread",
			Body:       "body",
			References: []string{"id1", "id2"},
		})
		if err != nil {
			t.Fatalf("BuildEML: %v", err)
		}
		hdr := parseHeaders(t, eml)
		got := hdr.Get("References")
		if got != "<id1> <id2>" {
			t.Errorf("References: want %q, got %q", "<id1> <id2>", got)
		}
	})

	t.Run("References header absent when nil", func(t *testing.T) {
		eml, err := BuildEML(BuildOpts{
			FromAddr: "a@example.com",
			To:       []pmxmail.Address{{Email: "b@example.com"}},
			Subject:  "Hello",
			Body:     "body",
		})
		if err != nil {
			t.Fatalf("BuildEML: %v", err)
		}
		hdr := parseHeaders(t, eml)
		if got := hdr.Get("References"); got != "" {
			t.Errorf("References: want empty, got %q", got)
		}
	})
}

// ---- TestBuildEML_CcHeader -------------------------------------------------

func TestBuildEML_CcHeader(t *testing.T) {
	t.Run("Cc header present when non-empty", func(t *testing.T) {
		eml, err := BuildEML(BuildOpts{
			FromAddr: "a@example.com",
			To:       []pmxmail.Address{{Email: "b@example.com"}},
			Cc:       []pmxmail.Address{{Name: "Carol", Email: "carol@example.com"}},
			Subject:  "CC Test",
			Body:     "body",
		})
		if err != nil {
			t.Fatalf("BuildEML: %v", err)
		}
		hdr := parseHeaders(t, eml)
		got := hdr.Get("Cc")
		if !strings.Contains(got, "carol@example.com") {
			t.Errorf("Cc header: want to contain %q, got %q", "carol@example.com", got)
		}
	})

	t.Run("Cc header absent when nil", func(t *testing.T) {
		eml, err := BuildEML(BuildOpts{
			FromAddr: "a@example.com",
			To:       []pmxmail.Address{{Email: "b@example.com"}},
			Subject:  "No CC",
			Body:     "body",
		})
		if err != nil {
			t.Fatalf("BuildEML: %v", err)
		}
		hdr := parseHeaders(t, eml)
		if got := hdr.Get("Cc"); got != "" {
			t.Errorf("Cc header: want empty, got %q", got)
		}
	})
}

// ---- TestBuildEML_MessageID ------------------------------------------------

func TestBuildEML_MessageID(t *testing.T) {
	eml, err := BuildEML(BuildOpts{
		FromAddr: "sender@example.com",
		To:       []pmxmail.Address{{Email: "recv@example.com"}},
		Subject:  "ID Test",
		Body:     "body",
	})
	if err != nil {
		t.Fatalf("BuildEML: %v", err)
	}
	hdr := parseHeaders(t, eml)
	mid := hdr.Get("Message-Id")
	if mid == "" {
		// net/mail may lowercase the header name
		mid = hdr.Get("Message-ID")
	}
	if mid == "" {
		t.Fatal("Message-ID header is missing")
	}
	if !strings.HasPrefix(mid, "<") || !strings.HasSuffix(mid, ">") {
		t.Errorf("Message-ID should be wrapped in angle brackets, got %q", mid)
	}
}
