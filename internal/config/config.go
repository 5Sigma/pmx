package config

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

var ErrNoConfigFile = errors.New("config file not found")

// Load resolves the XDG config path and loads it, falling back to defaults if
// no file exists.
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom loads from an explicit path. Returns (defaults, ErrNoConfigFile)
// when the file doesn't exist.
func LoadFrom(path string) (*Config, error) {
	k := koanf.New(".")

	// Seed koanf with defaults first so partial files inherit them.
	if err := k.Load(confmap.Provider(defaultsMap(), "."), nil); err != nil {
		return nil, err
	}

	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		cfg := Default()
		return cfg, ErrNoConfigFile
	}

	// Load the TOML file into a scratch koanf instance, normalize all keys to
	// snake_case, then merge the result into k (on top of defaults).
	// This makes config files case-insensitive — [General] and [general] both work.
	fileK := koanf.New(".")
	if err := fileK.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, err
	}
	if err := k.Load(confmap.Provider(lowerKeys(fileK.Raw()), "."), nil); err != nil {
		return nil, err
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}

	applyAccountDefaults(&cfg)
	applyDaemonDefaults(&cfg)
	return &cfg, cfg.Validate()
}

// Save writes the config to the resolved XDG path, creating the directory if needed.
func Save(cfg *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	return SaveTo(cfg, path)
}

// camelToSnake converts CamelCase / PascalCase keys to snake_case so config
// files are case-insensitive. Already-snake_case keys are unchanged.
// Examples: "DefaultAccount" → "default_account", "imap_host" → "imap_host".
var (
	camelRE1 = regexp.MustCompile(`(.)([A-Z][a-z]+)`)
	camelRE2 = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

func normalizeKey(k string) string {
	s := camelRE1.ReplaceAllString(k, `${1}_${2}`)
	s = camelRE2.ReplaceAllString(s, `${1}_${2}`)
	return strings.ToLower(s)
}

func lowerKeys(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		nk := normalizeKey(k)
		switch val := v.(type) {
		case map[string]any:
			out[nk] = lowerKeys(val)
		case []any:
			out[nk] = lowerSlice(val)
		default:
			out[nk] = v
		}
	}
	return out
}

func lowerSlice(s []any) []any {
	out := make([]any, len(s))
	for i, v := range s {
		if m, ok := v.(map[string]any); ok {
			out[i] = lowerKeys(m)
		} else {
			out[i] = v
		}
	}
	return out
}

// AccountByName returns the first account with the given name, or nil.
func (c *Config) AccountByName(name string) *Account {
	for i := range c.Accounts {
		if c.Accounts[i].Name == name {
			return &c.Accounts[i]
		}
	}
	return nil
}

func SaveTo(cfg *Config, path string) error {
	data, err := marshalTOML(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
