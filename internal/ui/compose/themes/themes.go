package themes

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aymerick/douceur/inliner"
)

// Theme holds a named CSS stylesheet for HTML email rendering.
type Theme struct {
	Name string
	CSS  string
}

//go:embed builtin/*.css
var builtinFS embed.FS

// Load returns built-ins first (basic, professional) then user themes
// alphabetically. User themes with the same name as a built-in override the
// built-in. Malformed user CSS files are logged and skipped. userDir may be
// empty or nonexistent — treated as "no user themes".
func Load(userDir string) []Theme {
	builtinNames := []string{"basic", "professional"}
	builtinMap := make(map[string]Theme, len(builtinNames))
	for _, name := range builtinNames {
		data, err := builtinFS.ReadFile(fmt.Sprintf("builtin/%s.css", name))
		if err != nil {
			log.Printf("themes: failed to read built-in %s: %v", name, err)
			continue
		}
		builtinMap[name] = Theme{Name: name, CSS: string(data)}
	}

	userMap := make(map[string]Theme)
	if userDir != "" {
		entries, err := os.ReadDir(userDir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				fname := e.Name()
				if !strings.HasSuffix(strings.ToLower(fname), ".css") {
					continue
				}
				name := strings.TrimSuffix(fname, filepath.Ext(fname))
				if strings.ContainsAny(name, "/\\") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(userDir, fname))
				if err != nil {
					log.Printf("themes: failed to read user theme %s: %v", fname, err)
					continue
				}
				userMap[name] = Theme{Name: name, CSS: string(data)}
			}
		}
	}

	// Merge: built-ins in fixed order, user override if same name.
	var result []Theme
	for _, name := range builtinNames {
		if ut, ok := userMap[name]; ok {
			result = append(result, ut)
		} else if bt, ok := builtinMap[name]; ok {
			result = append(result, bt)
		}
	}

	// Append user themes not in built-ins, sorted alphabetically.
	var extraNames []string
	for name := range userMap {
		isBuiltin := false
		for _, bn := range builtinNames {
			if name == bn {
				isBuiltin = true
				break
			}
		}
		if !isBuiltin {
			extraNames = append(extraNames, name)
		}
	}
	sort.Strings(extraNames)
	for _, name := range extraNames {
		result = append(result, userMap[name])
	}

	return result
}

// Find looks up a theme by name. Falls back to the first theme ("basic") when
// not found. The second return value is false when the name was not found.
func Find(ts []Theme, name string) (Theme, bool) {
	for _, t := range ts {
		if t.Name == name {
			return t, true
		}
	}
	if len(ts) > 0 {
		return ts[0], false
	}
	return Theme{Name: "basic"}, false
}

// Apply wraps bodyHTML in a full HTML document with the given CSS in <head>,
// then inlines it into style="" attributes via douceur. On inliner error,
// returns the wrapped document without inlining so sends never fail.
func Apply(css, bodyHTML string) (string, error) {
	var head string
	if css != "" {
		head = fmt.Sprintf("<style>%s</style>", css)
	}
	full := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8">%s</head><body>%s</body></html>`, head, bodyHTML)
	if css == "" {
		return full, nil
	}
	inlined, err := inliner.Inline(full)
	if err != nil {
		// Don't fail the send — return the wrapped but non-inlined doc.
		return full, nil
	}
	return inlined, nil
}
