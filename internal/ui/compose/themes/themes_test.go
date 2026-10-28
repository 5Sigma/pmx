package themes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- TestLoad ---------------------------------------------------------------

func TestLoad_NoUserDir(t *testing.T) {
	cases := []struct {
		name    string
		userDir string
	}{
		{name: "empty string", userDir: ""},
		{name: "nonexistent path", userDir: "/tmp/pmx-themes-does-not-exist-ever"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Load(tc.userDir)
			if len(got) != 2 {
				t.Fatalf("want 2 built-in themes, got %d", len(got))
			}
			if got[0].Name != "basic" {
				t.Errorf("theme[0]: want name %q, got %q", "basic", got[0].Name)
			}
			if got[1].Name != "professional" {
				t.Errorf("theme[1]: want name %q, got %q", "professional", got[1].Name)
			}
		})
	}
}

func TestLoad_UserDirAddsNewTheme(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "custom.css"), []byte("body { font-size: 14px; }"), 0600); err != nil {
		t.Fatalf("write custom.css: %v", err)
	}

	got := Load(dir)
	if len(got) != 3 {
		t.Fatalf("want 3 themes (basic, professional, custom), got %d: %v", len(got), themeNames(got))
	}
	if got[0].Name != "basic" {
		t.Errorf("theme[0]: want %q, got %q", "basic", got[0].Name)
	}
	if got[1].Name != "professional" {
		t.Errorf("theme[1]: want %q, got %q", "professional", got[1].Name)
	}
	if got[2].Name != "custom" {
		t.Errorf("theme[2]: want %q, got %q", "custom", got[2].Name)
	}
}

func TestLoad_UserOverridesBuiltin(t *testing.T) {
	const userCSS = "body { background: pink; }"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "professional.css"), []byte(userCSS), 0600); err != nil {
		t.Fatalf("write professional.css: %v", err)
	}

	got := Load(dir)
	if len(got) != 2 {
		t.Fatalf("want exactly 2 themes (override, not append), got %d: %v", len(got), themeNames(got))
	}
	if got[1].Name != "professional" {
		t.Errorf("theme[1]: want name %q, got %q", "professional", got[1].Name)
	}
	if got[1].CSS != userCSS {
		t.Errorf("theme[1].CSS: want user content %q, got %q", userCSS, got[1].CSS)
	}
}

func TestLoad_NonCSSFileIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a theme"), 0600); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	got := Load(dir)
	if len(got) != 2 {
		t.Fatalf("want 2 built-in themes (non-CSS file ignored), got %d: %v", len(got), themeNames(got))
	}
	for _, th := range got {
		if th.Name == "notes" {
			t.Error("notes.txt was incorrectly included as a theme")
		}
	}
}

// ---- TestFind ---------------------------------------------------------------

func TestFind_ExistingName(t *testing.T) {
	ts := Load("")
	th, found := Find(ts, "professional")
	if !found {
		t.Fatalf("Find(\"professional\"): want found=true, got false")
	}
	if th.Name != "professional" {
		t.Errorf("Find returned theme %q, want %q", th.Name, "professional")
	}
}

func TestFind_MissingNameFallsBackToFirst(t *testing.T) {
	ts := Load("")
	th, found := Find(ts, "nonexistent")
	if found {
		t.Errorf("Find(\"nonexistent\"): want found=false, got true")
	}
	if th.Name != ts[0].Name {
		t.Errorf("fallback: want first theme %q, got %q", ts[0].Name, th.Name)
	}
}

// ---- TestApply --------------------------------------------------------------

func TestApply_EmptyCSS(t *testing.T) {
	result, err := Apply("", "<p>hello</p>")
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if !strings.Contains(result, "<p>hello</p>") {
		t.Errorf("result does not contain original body; got: %q", result)
	}
	if !strings.Contains(result, "<body>") {
		t.Errorf("result not wrapped in <body>; got: %q", result)
	}
	if strings.Contains(result, `style="`) {
		t.Errorf("empty CSS should not produce style= attributes; got: %q", result)
	}
}

func TestApply_WithCSS_InlinesStyles(t *testing.T) {
	result, err := Apply("p { color: red; }", "<p>hello</p>")
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if !strings.Contains(result, "style=") {
		t.Errorf("CSS inlining: want style= attribute on <p>, got: %q", result)
	}
}

func TestApply_WrapsInFullDocument(t *testing.T) {
	cases := []struct {
		name string
		css  string
		body string
	}{
		{name: "empty CSS", css: "", body: "<p>text</p>"},
		{name: "with CSS", css: "p { margin: 0; }", body: "<p>text</p>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Apply(tc.css, tc.body)
			if err != nil {
				t.Fatalf("Apply: unexpected error: %v", err)
			}
			if !strings.Contains(strings.ToLower(result), "<!doctype html") {
				t.Errorf("result missing <!doctype html>; got: %q", result)
			}
			if !strings.Contains(strings.ToLower(result), "<html") {
				t.Errorf("result missing <html>; got: %q", result)
			}
		})
	}
}

// themeNames is a helper for failure messages.
func themeNames(ts []Theme) []string {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name
	}
	return names
}
