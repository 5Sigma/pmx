package config_test

import (
	"errors"
	"testing"

	"github.com/5Sigma/pmx/internal/config"
)

func TestLoad_FullFixture(t *testing.T) {
	cfg, err := config.LoadFrom("testdata/full.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.General.DefaultAccount != "work" {
		t.Errorf("expected default_account=work, got %q", cfg.General.DefaultAccount)
	}
	if cfg.Theme.Primary != "#FF0000" {
		t.Errorf("expected primary=#FF0000, got %q", cfg.Theme.Primary)
	}
	if cfg.Features.AltScreen != false {
		t.Error("expected alt_screen=false")
	}
	if cfg.Keys.Quit[0] != "x" {
		t.Errorf("expected first quit binding=x, got %q", cfg.Keys.Quit[0])
	}
	if len(cfg.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(cfg.Accounts))
	}
	if cfg.Accounts[1].PasswordCommand != "echo secret" {
		t.Errorf("expected password_command set, got %q", cfg.Accounts[1].PasswordCommand)
	}
}

func TestLoad_PartialOverridesDefaults(t *testing.T) {
	cfg, err := config.LoadFrom("testdata/partial.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Theme.Primary != "#000000" {
		t.Errorf("expected overridden primary, got %q", cfg.Theme.Primary)
	}
	// Fields not in the partial file should retain defaults.
	if cfg.Theme.Accent != "#73F59F" {
		t.Errorf("expected default accent, got %q", cfg.Theme.Accent)
	}
	if !cfg.Features.AltScreen {
		t.Error("expected default alt_screen=true")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	cfg, err := config.LoadFrom("/nonexistent/path/config.toml")
	if !errors.Is(err, config.ErrNoConfigFile) {
		t.Fatalf("expected ErrNoConfigFile, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected defaults to be returned with ErrNoConfigFile")
	}
}

func TestLoad_PascalCaseKeys(t *testing.T) {
	cfg, err := config.LoadFrom("testdata/pascalcase.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.General.DefaultAccount != "work" {
		t.Errorf("expected DefaultAccount=work, got %q (PascalCase section headers not normalized)", cfg.General.DefaultAccount)
	}
	if cfg.Theme.Primary != "#112233" {
		t.Errorf("expected Primary=#112233, got %q", cfg.Theme.Primary)
	}
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].Name != "work" {
		t.Errorf("expected 1 account named work, got %v", cfg.Accounts)
	}
}

func TestLoad_MalformedTOML(t *testing.T) {
	_, err := config.LoadFrom("testdata/malformed.toml")
	if err == nil {
		t.Fatal("expected error for malformed TOML, got nil")
	}
	if errors.Is(err, config.ErrNoConfigFile) {
		t.Fatal("should not be ErrNoConfigFile for malformed file")
	}
}
