package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
)

// withTempDataHome redirects the XDG data home to a fresh temp dir for the
// duration of a test so Load/SaveUIState hit an isolated path.
func withTempDataHome(t *testing.T) string {
	t.Helper()
	orig := xdg.DataHome
	dir := t.TempDir()
	xdg.DataHome = dir
	t.Cleanup(func() { xdg.DataHome = orig })
	return dir
}

func TestLoadUIState_MissingFile(t *testing.T) {
	withTempDataHome(t)
	s, err := LoadUIState()
	if err != nil {
		t.Fatalf("LoadUIState: %v", err)
	}
	if s != (UIState{}) {
		t.Errorf("want zero UIState for missing file, got %+v", s)
	}
}

func TestLoadUIState_MalformedFile(t *testing.T) {
	withTempDataHome(t)
	path, err := UIStatePath()
	if err != nil {
		t.Fatalf("UIStatePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write malformed file: %v", err)
	}

	s, err := LoadUIState()
	if err != nil {
		t.Fatalf("LoadUIState on malformed: %v", err)
	}
	if s != (UIState{}) {
		t.Errorf("want zero UIState for malformed file, got %+v", s)
	}
}

func TestSaveLoadUIState_RoundTrip(t *testing.T) {
	withTempDataHome(t)
	want := UIState{LastActiveAccount: "work"}
	if err := SaveUIState(want); err != nil {
		t.Fatalf("SaveUIState: %v", err)
	}
	got, err := LoadUIState()
	if err != nil {
		t.Fatalf("LoadUIState: %v", err)
	}
	if got != want {
		t.Errorf("round-trip: got %+v, want %+v", got, want)
	}
}

func TestResolveInitialAccount(t *testing.T) {
	cfg := &Config{
		General: General{DefaultAccount: "work"},
		Accounts: []Account{
			{Name: "work", Email: "w@example.com"},
			{Name: "personal", Email: "p@example.com"},
			{Name: "labs", Email: "l@example.com"},
		},
	}

	t.Run("last-active wins when valid", func(t *testing.T) {
		a, ok := ResolveInitialAccount(cfg, UIState{LastActiveAccount: "personal"})
		if !ok || a == nil || a.Name != "personal" {
			t.Fatalf("got %+v (ok=%v), want personal", a, ok)
		}
	})

	t.Run("stale last-active falls back to default", func(t *testing.T) {
		a, ok := ResolveInitialAccount(cfg, UIState{LastActiveAccount: "ghost"})
		if !ok || a.Name != "work" {
			t.Fatalf("got %+v (ok=%v), want work", a, ok)
		}
	})

	t.Run("missing default falls back to first", func(t *testing.T) {
		c := &Config{
			Accounts: []Account{
				{Name: "alpha", Email: "a@example.com"},
				{Name: "beta", Email: "b@example.com"},
			},
		}
		a, ok := ResolveInitialAccount(c, UIState{})
		if !ok || a.Name != "alpha" {
			t.Fatalf("got %+v (ok=%v), want alpha", a, ok)
		}
	})

	t.Run("stale default and empty last-active falls back to first", func(t *testing.T) {
		c := &Config{
			General: General{DefaultAccount: "ghost"},
			Accounts: []Account{
				{Name: "alpha", Email: "a@example.com"},
			},
		}
		a, ok := ResolveInitialAccount(c, UIState{})
		if !ok || a.Name != "alpha" {
			t.Fatalf("got %+v (ok=%v), want alpha", a, ok)
		}
	})

	t.Run("no accounts yields no match", func(t *testing.T) {
		c := &Config{}
		a, ok := ResolveInitialAccount(c, UIState{LastActiveAccount: "x"})
		if ok || a != nil {
			t.Fatalf("got %+v (ok=%v), want nil, false", a, ok)
		}
	})

	t.Run("nil config safe", func(t *testing.T) {
		a, ok := ResolveInitialAccount(nil, UIState{})
		if ok || a != nil {
			t.Fatalf("got %+v (ok=%v), want nil, false", a, ok)
		}
	})
}
