package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

const keyringService = "cw"

var ErrNoToken = errors.New("no token saved")

// SecretStore keeps one token per platform URL.
type SecretStore interface {
	Get(url string) (string, error)
	// Set returns where the token went, for the login message.
	Set(url, token string) (string, error)
	Delete(url string) error
}

// KeychainStore uses the OS keychain and falls back to a 0600 file where
// there is none (a headless Linux box without a secret service).
type KeychainStore struct{ Fallback FileStore }

func (s KeychainStore) Get(u string) (string, error) {
	if token, err := keyring.Get(keyringService, u); err == nil {
		return token, nil
	}
	return s.Fallback.Get(u)
}

func (s KeychainStore) Set(u, token string) (string, error) {
	if err := keyring.Set(keyringService, u, token); err == nil {
		_ = s.Fallback.Delete(u)
		return "the system keychain", nil
	}
	return s.Fallback.Set(u, token)
}

func (s KeychainStore) Delete(u string) error {
	err := keyring.Delete(keyringService, u)
	if fileErr := s.Fallback.Delete(u); fileErr != nil {
		return fileErr
	}
	if err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return fmt.Errorf("keychain: %w", err)
	}
	return nil
}

// FileStore keeps tokens in a JSON file only this user can read.
type FileStore struct{ Path string }

func (s FileStore) read() map[string]string {
	tokens := map[string]string{}
	if raw, err := os.ReadFile(s.Path); err == nil {
		_ = json.Unmarshal(raw, &tokens)
	}
	return tokens
}

func (s FileStore) Get(u string) (string, error) {
	if token := s.read()[u]; token != "" {
		return token, nil
	}
	return "", ErrNoToken
}

func (s FileStore) Set(u, token string) (string, error) {
	tokens := s.read()
	tokens[u] = token
	return s.Path, WritePrivateJSON(s.Path, tokens)
}

func (s FileStore) Delete(u string) error {
	tokens := s.read()
	if _, ok := tokens[u]; !ok {
		return nil
	}
	delete(tokens, u)
	return WritePrivateJSON(s.Path, tokens)
}
