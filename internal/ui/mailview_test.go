package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/5Sigma/pmx/internal/mail"
)

func TestRewrite_StripsCIDAndMapsToInline(t *testing.T) {
	html := `<img src="cid:abc@host">`
	got := mailviewRewrite(html, map[string]string{"abc@host": "1_logo.png"})
	if !strings.Contains(got, `inline/1_logo.png`) {
		t.Errorf("expected inline/1_logo.png in %q", got)
	}
	if strings.Contains(got, "cid:") {
		t.Errorf("unexpected cid: remaining in %q", got)
	}
}

func TestRewrite_UnresolvedCIDLeftAlone(t *testing.T) {
	html := `<img src="cid:gone">`
	got := mailviewRewrite(html, map[string]string{})
	if got != html {
		t.Errorf("unresolved cid should be unchanged: got %q", got)
	}
}

func TestRewrite_HandlesSingleQuotesAndCSSUrl(t *testing.T) {
	html := `<img src='cid:a'> background:url(cid:a)`
	got := mailviewRewrite(html, map[string]string{"a": "1_img.jpg"})
	if strings.Count(got, "inline/1_img.jpg") != 2 {
		t.Errorf("expected 2 replacements, got %q", got)
	}
}

func TestRewrite_CaseInsensitiveScheme(t *testing.T) {
	html := `<img src="CID:abc">`
	got := mailviewRewrite(html, map[string]string{"abc": "1_img.png"})
	if !strings.Contains(got, "inline/1_img.png") {
		t.Errorf("expected case-insensitive match, got %q", got)
	}
}

func TestWrite_ProducesIndexAndCopies(t *testing.T) {
	// Create a real source file for the attachment.
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "logo.png")
	if err := os.WriteFile(srcFile, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	atts := []mail.Attachment{
		{ID: 1, Filename: "logo.png", ContentID: "logo@host", DiskPath: "00/sha"},
	}
	pathResolver := func(a mail.Attachment) string { return srcFile }

	dir := t.TempDir()
	index, err := mailviewWrite(dir, `<img src="cid:logo@host">`, atts, pathResolver)
	if err != nil {
		t.Fatalf("mailviewWrite: %v", err)
	}

	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatalf("ReadFile index: %v", err)
	}
	if !strings.Contains(string(data), "inline/1_logo.png") {
		t.Errorf("index.html missing rewritten ref, got:\n%s", data)
	}

	inlineFile := filepath.Join(dir, "inline", "1_logo.png")
	copied, err := os.ReadFile(inlineFile)
	if err != nil {
		t.Fatalf("inline file not written: %v", err)
	}
	if string(copied) != "PNGDATA" {
		t.Errorf("copied bytes = %q, want PNGDATA", copied)
	}
}

func TestWrite_SkipsUnreferencedAttachments(t *testing.T) {
	srcDir := t.TempDir()
	srcA := filepath.Join(srcDir, "a.png")
	srcB := filepath.Join(srcDir, "b.png")
	_ = os.WriteFile(srcA, []byte("A"), 0o600)
	_ = os.WriteFile(srcB, []byte("B"), 0o600)

	atts := []mail.Attachment{
		{ID: 1, Filename: "a.png", ContentID: "a@host", DiskPath: "00/a"},
		{ID: 2, Filename: "b.png", ContentID: "b@host", DiskPath: "00/b"},
	}
	pathResolver := func(a mail.Attachment) string {
		if a.ContentID == "a@host" {
			return srcA
		}
		return srcB
	}

	dir := t.TempDir()
	// Only reference cid:a@host in the HTML.
	_, err := mailviewWrite(dir, `<img src="cid:a@host">`, atts, pathResolver)
	if err != nil {
		t.Fatalf("mailviewWrite: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "inline", "1_a.png")); err != nil {
		t.Error("expected 1_a.png to be copied")
	}
	if _, err := os.Stat(filepath.Join(dir, "inline", "2_b.png")); err == nil {
		t.Error("expected 2_b.png NOT to be copied, but it exists")
	}
}

func TestWrite_WrapsWhenNoHTMLTag(t *testing.T) {
	dir := t.TempDir()
	index, err := mailviewWrite(dir, `<p>hello</p>`, nil, func(mail.Attachment) string { return "" })
	if err != nil {
		t.Fatalf("mailviewWrite: %v", err)
	}
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<!doctype html>") {
		t.Error("expected doctype wrapper for bare html body")
	}
	if !strings.Contains(string(data), `charset="utf-8"`) {
		t.Error("expected charset meta")
	}
}

func TestWrite_NoWrapWhenHTMLTagPresent(t *testing.T) {
	dir := t.TempDir()
	raw := `<html><body><p>hi</p></body></html>`
	index, err := mailviewWrite(dir, raw, nil, func(mail.Attachment) string { return "" })
	if err != nil {
		t.Fatalf("mailviewWrite: %v", err)
	}
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	// Should not double-wrap.
	if strings.Count(string(data), "<html") > 1 {
		t.Error("html tag duplicated by wrapper")
	}
}
