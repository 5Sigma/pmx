package mailsync

import (
	"strings"
	"testing"
)

// TestDecodeBody exercises decodeBody across all combinations of
// transfer-encoding and charset that the sync engine may encounter in real
// IMAP bodies. The function is expected to degrade gracefully on unknown input
// rather than propagate errors.
func TestDecodeBody(t *testing.T) {
	cases := []struct {
		name     string
		raw      []byte
		encoding string
		charset  string
		want     string
	}{
		{
			name:     "quoted-printable UTF-8 café",
			raw:      []byte("caf=C3=A9"),
			encoding: "quoted-printable",
			charset:  "",
			want:     "café",
		},
		{
			name:     "base64 plaintext",
			raw:      []byte("SGVsbG8sIFdvcmxkIQ=="),
			encoding: "base64",
			charset:  "",
			want:     "Hello, World!",
		},
		{
			name:     "empty encoding UTF-8 passthrough",
			raw:      []byte("plain UTF-8 text"),
			encoding: "",
			charset:  "utf-8",
			want:     "plain UTF-8 text",
		},
		{
			name:     "empty encoding ASCII passthrough",
			raw:      []byte("plain ascii text"),
			encoding: "",
			charset:  "us-ascii",
			want:     "plain ascii text",
		},
		{
			name:     "7bit passthrough",
			raw:      []byte("seven bit content"),
			encoding: "7bit",
			charset:  "",
			want:     "seven bit content",
		},
		{
			name:     "QUOTED-PRINTABLE uppercase encoding is case-insensitive",
			raw:      []byte("caf=C3=A9"),
			encoding: "QUOTED-PRINTABLE",
			charset:  "",
			want:     "café",
		},
		{
			name:     "unknown charset falls back to raw bytes without error",
			raw:      []byte("raw bytes"),
			encoding: "",
			charset:  "unknown-charset-xyz",
			want:     "raw bytes",
		},
		{
			name:     "quoted-printable then ISO-8859-1 charset conversion",
			raw:      []byte("=E9l=E8ve"),
			encoding: "quoted-printable",
			charset:  "iso-8859-1",
			want:     "élève",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeBody(tc.raw, tc.encoding, tc.charset)
			// Trim trailing whitespace: quoted-printable may add a trailing newline.
			got = strings.TrimRight(got, "\r\n")
			if got != tc.want {
				t.Errorf("decodeBody(%q, %q, %q):\n  want: %q\n   got: %q",
					tc.raw, tc.encoding, tc.charset, tc.want, got)
			}
		})
	}
}
