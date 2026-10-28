package mailsync

import (
	"bytes"
	netmail "net/mail"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"

	"github.com/5Sigma/pmx/internal/mail"
)

// parseFlags converts a slice of IMAP flag strings into our bitmask type.
func parseFlags(imapFlags []imap.Flag) mail.Flag {
	var f mail.Flag
	for _, fl := range imapFlags {
		switch fl {
		case imap.FlagSeen:
			f |= mail.FlagSeen
		case imap.FlagFlagged:
			f |= mail.FlagFlagged
		case imap.FlagAnswered:
			f |= mail.FlagAnswered
		case imap.FlagDeleted:
			f |= mail.FlagDeleted
		case imap.FlagDraft:
			f |= mail.FlagDraft
		}
	}
	return f
}

func parseAddress(a imap.Address) mail.Address {
	email := a.Mailbox
	if a.Host != "" {
		email += "@" + a.Host
	}
	return mail.Address{Name: a.Name, Email: email}
}

func parseAddresses(as []imap.Address) []mail.Address {
	if len(as) == 0 {
		return []mail.Address{}
	}
	out := make([]mail.Address, len(as))
	for i, a := range as {
		out[i] = parseAddress(a)
	}
	return out
}

// applyEnvelope copies fields from an IMAP envelope into a mail.Message.
// It sets ThreadRoot to MessageID as a baseline; applyReferencesHeader may
// overwrite it with a more accurate value once the References header is parsed.
func applyEnvelope(env *imap.Envelope, m *mail.Message) {
	m.Subject = env.Subject
	m.MessageID = env.MessageID
	// env.InReplyTo is []string; take the first entry if present.
	if len(env.InReplyTo) > 0 {
		m.InReplyTo = env.InReplyTo[0]
	}
	m.DateSent = env.Date
	if len(env.From) > 0 {
		m.From = parseAddress(env.From[0])
	}
	m.To = parseAddresses(env.To)
	m.CC = parseAddresses(env.Cc)
	m.BCC = parseAddresses(env.Bcc)
	// Baseline ThreadRoot — may be overwritten by applyReferencesHeader.
	m.ThreadRoot = m.MessageID
}

// applyReferencesHeader parses raw MIME header bytes from a
// BODY[HEADER.FIELDS (References)] fetch and populates m.References and
// m.ThreadRoot. It is a no-op on any parse failure so a single bad header
// never aborts the sync.
func applyReferencesHeader(raw []byte, m *mail.Message) {
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return
	}
	refsVal := msg.Header.Get("References")
	if refsVal == "" {
		return
	}
	m.References = parseMessageIDs(refsVal)
	// ThreadRoot: first Reference → oldest known ancestor. Fall back through
	// InReplyTo and MessageID if the list is somehow empty after parsing.
	if len(m.References) > 0 {
		m.ThreadRoot = m.References[0]
	} else if m.InReplyTo != "" {
		m.ThreadRoot = m.InReplyTo
	} else {
		m.ThreadRoot = m.MessageID
	}
}

// parseMessageIDs extracts bare message IDs from a References or In-Reply-To
// header value. Tokens are delimited by angle brackets; the brackets are
// stripped from the returned slice.
func parseMessageIDs(s string) []string {
	var ids []string
	for {
		start := strings.Index(s, "<")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], ">")
		if end < 0 {
			break
		}
		id := strings.TrimSpace(s[start+1 : start+end])
		if id != "" {
			ids = append(ids, id)
		}
		s = s[start+end+1:]
	}
	return ids
}

// partInfo describes one body section of a message.
type partInfo struct {
	path      []int  // e.g. [1] → "1", [2,1] → "2.1"
	mime      string // "text/plain", "image/jpeg", etc.
	disp      string // "attachment", "inline", ""
	filename  string
	contentID string // bare Content-ID, angle brackets stripped
	size      uint32
	encoding  string // MIME transfer encoding (e.g. "quoted-printable", "base64", "7bit")
	charset   string // character set (e.g. "utf-8", "iso-8859-1")
}

// partPath converts a []int section path into the dot-joined string used as a
// map key and passed to imap.FetchItemBodySection.Part.
func partPath(path []int) string {
	if len(path) == 0 {
		return ""
	}
	parts := make([]string, len(path))
	for i, n := range path {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// walkBody recursively walks a BODYSTRUCTURE and returns the first text/plain
// part, the first text/html part, and any attachment parts.
func walkBody(bs imap.BodyStructure) (text, html *partInfo, attachs []partInfo) {
	walkPart(bs, nil, &text, &html, &attachs)
	return
}

func walkPart(bs imap.BodyStructure, path []int, text, html **partInfo, attachs *[]partInfo) {
	switch b := bs.(type) {
	case *imap.BodyStructureSinglePart:
		mime := strings.ToLower(b.Type + "/" + b.Subtype)
		disp := ""
		filename := b.Filename() // uses Extended.Disposition.Params["filename"] and Params["name"]
		if d := b.Disposition(); d != nil {
			disp = strings.ToLower(d.Value)
		}

		contentID := strings.Trim(b.ID, "<>")

		pi := partInfo{
			path:      path,
			mime:      mime,
			disp:      disp,
			filename:  filename,
			contentID: contentID,
			size:      b.Size,
			encoding:  strings.ToLower(b.Encoding),
			charset:   strings.ToLower(b.Params["charset"]),
		}

		if disp == "attachment" {
			*attachs = append(*attachs, pi)
			return
		}

		switch mime {
		case "text/plain":
			if *text == nil {
				cp := pi
				*text = &cp
			}
		case "text/html":
			if *html == nil {
				cp := pi
				*html = &cp
			}
		default:
			// Keep as attachment if it has a filename, a Content-ID (inline image
			// referenced via cid: in HTML), or an explicit inline disposition.
			if filename != "" || contentID != "" || disp == "inline" {
				if filename == "" {
					// Synthesize a filename from the content-id and mime subtype.
					ext := "." + b.Subtype
					pi.filename = "inline-" + contentID + ext
				}
				*attachs = append(*attachs, pi)
			}
		}

	case *imap.BodyStructureMultiPart:
		for i, child := range b.Children {
			childPath := make([]int, len(path)+1)
			copy(childPath, path)
			childPath[len(path)] = i + 1 // IMAP part indices are 1-based
			walkPart(child, childPath, text, html, attachs)
		}
	}
}
