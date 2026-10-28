package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/5Sigma/pmx/internal/mail"
)

var (
	reCID     = regexp.MustCompile(`(?i)(cid:)([^"'\s)>]+)`)
	reHasHTML = regexp.MustCompile(`(?i)<html`)
	htmlShell = `<!doctype html><html><head><meta charset="utf-8"><base target="_blank"><title>pmx mail view</title></head><body>`
)

var tempDirFn = func() (string, error) { return os.MkdirTemp("", "pmx-mailview-") }

// mailviewRewrite replaces cid:<id> occurrences in html with inline/<local>
// for each id present in cidToFile. Unmatched cid: refs are left untouched.
func mailviewRewrite(html string, cidToFile map[string]string) string {
	return reCID.ReplaceAllStringFunc(html, func(match string) string {
		sub := reCID.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		id := sub[2]
		if local, ok := cidToFile[id]; ok {
			return "inline/" + local
		}
		return match
	})
}

// mailviewWrite assembles a self-contained bundle at dir:
//
//	dir/index.html          — rewritten and wrapped HTML
//	dir/inline/<name>...    — copies of cid-referenced attachments
//
// pathResolver maps an Attachment to its on-disk absolute path.
// Returns the path to index.html.
func mailviewWrite(dir, html string, atts []mail.Attachment, pathResolver func(mail.Attachment) string) (string, error) {
	cidToFile := make(map[string]string, len(atts))
	for _, a := range atts {
		if a.ContentID != "" {
			cidToFile[a.ContentID] = safeName(a)
		}
	}

	rewritten := mailviewRewrite(html, cidToFile)

	if !reHasHTML.MatchString(rewritten) {
		rewritten = htmlShell + rewritten + "</body></html>"
	}

	inlineDir := filepath.Join(dir, "inline")
	if err := os.MkdirAll(inlineDir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir inline: %w", err)
	}

	for _, a := range atts {
		if a.ContentID == "" {
			continue
		}
		local, ok := cidToFile[a.ContentID]
		if !ok {
			continue
		}
		if !strings.Contains(rewritten, "inline/"+local) {
			continue
		}
		src := pathResolver(a)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(inlineDir, local)); err != nil {
			return "", fmt.Errorf("copy inline %s: %w", local, err)
		}
	}

	indexPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(indexPath, []byte(rewritten), 0o600); err != nil {
		return "", fmt.Errorf("write index.html: %w", err)
	}
	return indexPath, nil
}

func safeName(a mail.Attachment) string {
	base := filepath.Base(a.Filename)
	base = strings.NewReplacer("/", "_", "\\", "_").Replace(base)
	return fmt.Sprintf("%d_%s", a.ID, base)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}
