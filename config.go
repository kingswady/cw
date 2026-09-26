package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	defaultURL     = "https://www.cloudwady.com"
	keyringService = "cw"
)

var errNoToken = errors.New("no token saved")

type config struct {
	URL string `json:"url"`
	// Platforms are the ones logged in to, for cw use.
	Platforms []string `json:"platforms,omitempty"`
}

func (cfg *config) remember(base string) {
	for _, known := range cfg.Platforms {
		if known == base {
			return
		}
	}
	cfg.Platforms = append(cfg.Platforms, base)
}

func (cfg *config) forget(base string) {
	kept := cfg.Platforms[:0]
	for _, known := range cfg.Platforms {
		if known != base {
			kept = append(kept, known)
		}
	}
	cfg.Platforms = kept
}

func (a *app) configPath() string { return filepath.Join(a.configDir, "config.json") }

func (a *app) loadConfig() config {
	var cfg config
	if raw, err := os.ReadFile(a.configPath()); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	return cfg
}

func (a *app) saveConfig(cfg config) error {
	return writePrivateJSON(a.configPath(), cfg)
}

// baseURL is the platform to talk to: an explicit value, then CW_URL, then
// the one saved at login, then the default.
func (a *app) baseURL(explicit string) (string, error) {
	base, _, err := a.platform(explicit)
	return base, err
}

// Where the platform URL came from, so login can say why it picked it.
const (
	fromFlag    = "flag"
	fromEnv     = "env"
	fromSaved   = "saved"
	fromDefault = "default"
)

func (a *app) platform(explicit string) (string, string, error) {
	candidates := []struct{ value, source string }{
		{explicit, fromFlag},
		{a.getenv("CW_URL"), fromEnv},
		{a.loadConfig().URL, fromSaved},
	}
	for _, candidate := range candidates {
		if candidate.value != "" {
			base, err := normalizeURL(candidate.value)
			return base, candidate.source, err
		}
	}
	return defaultURL, fromDefault, nil
}

// platformNote explains a URL the user did not type on this command line.
func platformNote(source string) string {
	switch source {
	case fromEnv:
		return " (from CW_URL)"
	case fromSaved:
		return " (your last platform — use --url for another)"
	}
	return ""
}

// normalizeURL accepts "host", "https://host/" and "http://localhost:8069";
// a token never travels over plain http to anything but this machine.
func normalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", usagef("not a URL: %q", raw)
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !isLoopback(parsed.Hostname()) {
			return "", usagef("refusing to send a token over plain http to %s — use https", parsed.Host)
		}
	default:
		return "", usagef("unsupported URL scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String(), nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writePrivateJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

// secretStore keeps one token per platform URL.
type secretStore interface {
	Get(url string) (string, error)
	// Set returns where the token went, for the login message.
	Set(url, token string) (string, error)
	Delete(url string) error
}

// keychainStore uses the OS keychain and falls back to a 0600 file where
// there is none (a headless Linux box without a secret service).
type keychainStore struct{ fallback fileStore }

func (s keychainStore) Get(u string) (string, error) {
	if token, err := keyring.Get(keyringService, u); err == nil {
		return token, nil
	}
	return s.fallback.Get(u)
}

func (s keychainStore) Set(u, token string) (string, error) {
	if err := keyring.Set(keyringService, u, token); err == nil {
		_ = s.fallback.Delete(u)
		return "the system keychain", nil
	}
	return s.fallback.Set(u, token)
}

func (s keychainStore) Delete(u string) error {
	err := keyring.Delete(keyringService, u)
	if fileErr := s.fallback.Delete(u); fileErr != nil {
		return fileErr
	}
	if err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return fmt.Errorf("keychain: %w", err)
	}
	return nil
}

type fileStore struct{ path string }

func (s fileStore) read() map[string]string {
	tokens := map[string]string{}
	if raw, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(raw, &tokens)
	}
	return tokens
}

func (s fileStore) Get(u string) (string, error) {
	if token := s.read()[u]; token != "" {
		return token, nil
	}
	return "", errNoToken
}

func (s fileStore) Set(u, token string) (string, error) {
	tokens := s.read()
	tokens[u] = token
	return s.path, writePrivateJSON(s.path, tokens)
}

func (s fileStore) Delete(u string) error {
	tokens := s.read()
	if _, ok := tokens[u]; !ok {
		return nil
	}
	delete(tokens, u)
	return writePrivateJSON(s.path, tokens)
}
