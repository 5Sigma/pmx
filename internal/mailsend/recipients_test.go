// Package mailsend provides the outbound send engine.
// This file contains unit tests for extractEnvelope. It is in package mailsend
// (not mailsend_test) so it can call the unexported function directly.
package mailsend

import (
	"sort"
	"strings"
	"testing"

	pmxmail "github.com/5Sigma/pmx/internal/mail"
	"github.com/5Sigma/pmx/internal/ui/compose"
)

func TestExtractEnvelope(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantFrom   string
		wantRcpts  []string // sorted for deterministic comparison
		wantErr    string   // non-empty substring expected in error message
		wantAnyErr bool     // any non-nil error is acceptable
	}{
		{
			name:      "single To address",
			raw:       "From: alice@example.com\r\nTo: bob@example.com\r\n\r\nHello",
			wantFrom:  "alice@example.com",
			wantRcpts: []string{"bob@example.com"},
		},
		{
			name:      "multiple To addresses",
			raw:       "From: alice@example.com\r\nTo: bob@example.com, carol@example.com\r\n\r\nHello",
			wantFrom:  "alice@example.com",
			wantRcpts: []string{"bob@example.com", "carol@example.com"},
		},
		{
			name:      "To + Cc both appear in rcpts",
			raw:       "From: alice@example.com\r\nTo: bob@example.com\r\nCc: carol@example.com\r\n\r\nHello",
			wantFrom:  "alice@example.com",
			wantRcpts: []string{"bob@example.com", "carol@example.com"},
		},
		{
			name:      "To + Cc + Bcc all merged",
			raw:       "From: alice@example.com\r\nTo: bob@example.com\r\nCc: carol@example.com\r\nBcc: dave@example.com\r\n\r\nHello",
			wantFrom:  "alice@example.com",
			wantRcpts: []string{"bob@example.com", "carol@example.com", "dave@example.com"},
		},
		{
			name: "case-insensitive dedup removes duplicate address",
			// bob@example.com appears in both To and Cc with different casing.
			raw:       "From: alice@example.com\r\nTo: Bob@Example.COM\r\nCc: bob@example.com\r\n\r\nHello",
			wantFrom:  "alice@example.com",
			wantRcpts: []string{"Bob@Example.COM"}, // first occurrence kept
		},
		{
			name:    "missing From header returns error",
			raw:     "To: bob@example.com\r\n\r\nHello",
			wantErr: "From",
		},
		{
			name:    "empty To/Cc/Bcc returns error",
			raw:     "From: alice@example.com\r\n\r\nHello",
			wantErr: "recipient",
		},
		{
			name:       "malformed EML bytes returns error",
			raw:        "not an eml at all \x00\x01\x02",
			wantAnyErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, rcpts, err := extractEnvelope([]byte(tc.raw))

			if tc.wantAnyErr {
				if err == nil {
					t.Fatalf("want any error, got nil (from=%q, rcpts=%v)", from, rcpts)
				}
				return
			}
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil (from=%q, rcpts=%v)", tc.wantErr, from, rcpts)
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErr)) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if from != tc.wantFrom {
				t.Errorf("from: want %q, got %q", tc.wantFrom, from)
			}
			got := make([]string, len(rcpts))
			copy(got, rcpts)
			sort.Strings(got)
			want := make([]string, len(tc.wantRcpts))
			copy(want, tc.wantRcpts)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("rcpts: want %v, got %v", want, got)
			}
		})
	}

	t.Run("real EML built via compose.BuildEML", func(t *testing.T) {
		eml, err := compose.BuildEML(compose.BuildOpts{
			FromAddr: "sender@pmx.test",
			FromName: "Sender Name",
			To:       []pmxmail.Address{{Email: "recv@pmx.test"}},
			Subject:  "Hello",
			Body:     "Body text",
		})
		if err != nil {
			t.Fatalf("compose.BuildEML: %v", err)
		}

		from, rcpts, err := extractEnvelope(eml)
		if err != nil {
			t.Fatalf("extractEnvelope on BuildEML output: %v", err)
		}
		if from != "sender@pmx.test" {
			t.Errorf("from: want %q, got %q", "sender@pmx.test", from)
		}
		if len(rcpts) != 1 || rcpts[0] != "recv@pmx.test" {
			t.Errorf("rcpts: want [recv@pmx.test], got %v", rcpts)
		}
	})

	t.Run("Cc addresses included in envelope recipients", func(t *testing.T) {
		eml, err := compose.BuildEML(compose.BuildOpts{
			FromAddr: "sender@pmx.test",
			To:       []pmxmail.Address{{Email: "to@pmx.test"}},
			Cc:       []pmxmail.Address{{Email: "cc@pmx.test"}},
			Subject:  "CC Test",
			Body:     "Body",
		})
		if err != nil {
			t.Fatalf("compose.BuildEML: %v", err)
		}

		_, rcpts, err := extractEnvelope(eml)
		if err != nil {
			t.Fatalf("extractEnvelope: %v", err)
		}
		found := false
		for _, r := range rcpts {
			if r == "cc@pmx.test" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Cc address not in envelope rcpts; got %v", rcpts)
		}
	})
}
