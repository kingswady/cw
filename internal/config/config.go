// Package config is what cw keeps on this machine: the platforms logged in to,
// the one in use, and one token per platform (keychain, or a 0600 file).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/kingswady/cw/internal/platform"
)

const DefaultURL = "https://www.cloudwady.com"

type Config struct {
	URL string `json:"url"`
	// Platforms are the ones logged in to, for cw use.
	Platforms []string `json:"platforms,omitempty"`
}

func (cfg *Config) Remember(base string) {
	for _, known := range cfg.Platforms {
		if known == base {
			return
		}
	}
	cfg.Platforms = append(cfg.Platforms, base)
}

func (cfg *Config) Forget(base string) {
	kept := cfg.Platforms[:0]
	for _, known := range cfg.Platforms {
		if known != base {
			kept = append(kept, known)
		}
	}
	cfg.Platforms = kept
}

func path(dir string) string { return filepath.Join(dir, "config.json") }

// Load reads dir's config; a missing or unreadable one is empty.
func Load(dir string) Config {
	var cfg Config
	if raw, err := os.ReadFile(path(dir)); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	return cfg
}

func Save(dir string, cfg Config) error {
	return WritePrivateJSON(path(dir), cfg)
}

// Where the platform URL came from, so login can say why it picked it.
const (
	FromFlag    = "flag"
	FromEnv     = "env"
	FromSaved   = "saved"
	FromDefault = "default"
)

// Resolve is the platform to talk to: an explicit value, then CW_URL, then the
// one saved at login, then the default — and where it came from.
func Resolve(explicit string, getenv func(string) string, dir string) (base, source string, err error) {
	candidates := []struct{ value, source string }{
		{explicit, FromFlag},
		{getenv("CW_URL"), FromEnv},
		{Load(dir).URL, FromSaved},
	}
	for _, candidate := range candidates {
		if candidate.value != "" {
			base, err := platform.NormalizeURL(candidate.value)
			return base, candidate.source, err
		}
	}
	return DefaultURL, FromDefault, nil
}

// WritePrivateJSON writes value to path readable by this user only.
func WritePrivateJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
