package mailsync

import (
	"testing"

	"github.com/5Sigma/pmx/internal/mail"
)

func TestApplyReferencesHeader(t *testing.T) {
	t.Run("two references ids", func(t *testing.T) {
		raw := []byte("References: <id1@example.com> <id2@example.com>\r\n\r\n")
		m := &mail.Message{MessageID: "self@example.com"}
		applyReferencesHeader(raw, m)

		want := []string{"id1@example.com", "id2@example.com"}
		if len(m.References) != len(want) {
			t.Fatalf("References: got %v, want %v", m.References, want)
		}
		for i, v := range want {
			if m.References[i] != v {
				t.Errorf("References[%d]: got %q, want %q", i, m.References[i], v)
			}
		}
		if m.ThreadRoot != "id1@example.com" {
			t.Errorf("ThreadRoot: got %q, want %q", m.ThreadRoot, "id1@example.com")
		}
	})

	t.Run("empty references value", func(t *testing.T) {
		// Header present but blank — no IDs extracted; ThreadRoot untouched.
		raw := []byte("References: \r\n\r\n")
		m := &mail.Message{MessageID: "self@example.com", ThreadRoot: "self@example.com"}
		applyReferencesHeader(raw, m)

		if len(m.References) != 0 {
			t.Errorf("References: expected nil/empty, got %v", m.References)
		}
		if m.ThreadRoot != "self@example.com" {
			t.Errorf("ThreadRoot: got %q, want %q", m.ThreadRoot, "self@example.com")
		}
	})

	t.Run("no references header", func(t *testing.T) {
		// Only CRLF — ReadMessage sees an empty header block; should be a no-op.
		raw := []byte("\r\n")
		m := &mail.Message{MessageID: "self@example.com", ThreadRoot: "self@example.com"}
		applyReferencesHeader(raw, m)

		if len(m.References) != 0 {
			t.Errorf("References: expected nil/empty, got %v", m.References)
		}
		if m.ThreadRoot != "self@example.com" {
			t.Errorf("ThreadRoot: got %q, want %q", m.ThreadRoot, "self@example.com")
		}
	})

	t.Run("completely empty raw bytes", func(t *testing.T) {
		// Empty input; ReadMessage should error; applyReferencesHeader must not panic.
		m := &mail.Message{MessageID: "self@example.com", ThreadRoot: "self@example.com"}
		applyReferencesHeader([]byte{}, m)

		if len(m.References) != 0 {
			t.Errorf("References: expected nil/empty, got %v", m.References)
		}
		if m.ThreadRoot != "self@example.com" {
			t.Errorf("ThreadRoot: got %q, want %q", m.ThreadRoot, "self@example.com")
		}
	})
}

func TestParseMessageIDs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single id",
			input: "<foo@bar.com>",
			want:  []string{"foo@bar.com"},
		},
		{
			name:  "multiple ids with whitespace",
			input: "<a@b.com>  <c@d.com>\t<e@f.com>",
			want:  []string{"a@b.com", "c@d.com", "e@f.com"},
		},
		{
			name:  "no ids",
			input: "plain text no angle brackets",
			want:  nil,
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "empty angle brackets skipped",
			input: "<> <real@example.com>",
			want:  []string{"real@example.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMessageIDs(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i, v := range tt.want {
				if got[i] != v {
					t.Errorf("[%d] got %q, want %q", i, got[i], v)
				}
			}
		})
	}
}
