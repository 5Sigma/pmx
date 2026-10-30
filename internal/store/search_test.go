package store

import (
	"testing"
	"time"
)

func TestSanitizeFTSQuery(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \t\n", ""},
		{"single word", "foo", `"foo"*`},
		{"two words", "foo bar", `"foo"* "bar"*`},
		{"strips specials", `foo* "bar" (baz) qux:quux`, `"foo"* "bar"* "baz"* "qux"* "quux"*`},
		{"specials only yields empty", `*"():+-^`, ""},
		{"caps tokens at 8", "a b c d e f g h i j", `"a"* "b"* "c"* "d"* "e"* "f"* "g"* "h"*`},
		{"collapses extra spaces", "foo   bar", `"foo"* "bar"*`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeFTSQuery(tc.in)
			if got != tc.want {
				t.Errorf("SanitizeFTSQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSearchInAccount_ScopesByAccount(t *testing.T) {
	s := openTestStore(t)

	acctA := seedAccount(t, s, "A", "a@example.com")
	acctB := seedAccount(t, s, "B", "b@example.com")

	folA := seedFolder(t, s, acctA, "INBOX")
	folB := seedFolder(t, s, acctB, "INBOX")

	now := time.Now()
	msgs := []struct {
		folderID int64
		uid      uint32
		subject  string
		body     string
	}{
		{folA, 1, "Lunch plans", "wrapping up at noon"},
		{folA, 2, "Project update", "shipping on Friday"},
		{folB, 1, "Lunch plans", "catering reminder"},
		{folB, 2, "Random", "unrelated content"},
	}
	for _, m := range msgs {
		msg := newMessage(m.folderID, m.uid, m.subject, now)
		msg.BodyText = m.body
		if err := s.UpsertMessage(msg); err != nil {
			t.Fatalf("UpsertMessage: %v", err)
		}
	}

	q := SanitizeFTSQuery("lunch")
	gotA, err := s.SearchInAccount(acctA, q, 10)
	if err != nil {
		t.Fatalf("SearchInAccount A: %v", err)
	}
	if len(gotA) != 1 {
		t.Fatalf("acctA matches = %d, want 1 (subjects: %v)", len(gotA), subjects(gotA))
	}
	if gotA[0].FolderID != folA {
		t.Errorf("acctA result folder = %d, want %d", gotA[0].FolderID, folA)
	}

	gotB, err := s.SearchInAccount(acctB, q, 10)
	if err != nil {
		t.Fatalf("SearchInAccount B: %v", err)
	}
	if len(gotB) != 1 {
		t.Fatalf("acctB matches = %d, want 1 (subjects: %v)", len(gotB), subjects(gotB))
	}
	if gotB[0].FolderID != folB {
		t.Errorf("acctB result folder = %d, want %d", gotB[0].FolderID, folB)
	}
}

func TestSearchInAccount_EmptyQueryReturnsNil(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "A", "a@example.com")

	got, err := s.SearchInAccount(acct, "", 10)
	if err != nil {
		t.Fatalf("SearchInAccount: %v", err)
	}
	if got != nil {
		t.Errorf("empty query returned %d results, want nil", len(got))
	}
}

func TestSearchInAccount_BodyMatch(t *testing.T) {
	s := openTestStore(t)
	acct := seedAccount(t, s, "A", "a@example.com")
	fol := seedFolder(t, s, acct, "INBOX")

	msg := newMessage(fol, 1, "Agenda", time.Now())
	msg.BodyText = "retrospective notes on Q3 deliverables"
	if err := s.UpsertMessage(msg); err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	got, err := s.SearchInAccount(acct, SanitizeFTSQuery("retrospective"), 10)
	if err != nil {
		t.Fatalf("SearchInAccount: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("body-match results = %d, want 1", len(got))
	}
}

func subjects(ms []*Message) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Subject)
	}
	return out
}
