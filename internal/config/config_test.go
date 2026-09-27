package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	found, _ := filepath.Glob(filepath.Join(dir, ".*"))
	return found
}

func TestWritePrivateJSONTightensALooseFileAndItsDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes")
	}
	dir := filepath.Join(t.TempDir(), "cw")
	path := filepath.Join(dir, "credentials.json")
	// A copy restored from a backup: world-readable, in a world-readable directory.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"https://old":"cwk_old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o755)
	if err := WritePrivateJSON(path, map[string]string{"https://new": "cwk_new"}); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("file mode %v, want 0600", got)
	}
	if got := mode(t, dir); got != 0o700 {
		t.Errorf("directory mode %v, want 0700", got)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "{\n  \"https://new\": \"cwk_new\"\n}\n" {
		t.Errorf("content %q", raw)
	}
	if found := leftovers(t, dir); len(found) != 0 {
		t.Errorf("temporary files left: %v", found)
	}
}

func TestAFailedWriteLeavesThePreviousFileWhole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	if err := WritePrivateJSON(path, map[string]string{"https://a": "cwk_a"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := WritePrivateJSON(path, map[string]any{"x": func() {}}); err == nil {
		t.Fatal("a value JSON cannot hold must fail")
	}
	// The rename cannot land: path's name is taken by a directory.
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivateJSON(blocked, map[string]string{}); err == nil {
		t.Fatal("renaming over a directory must fail")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("the previous file changed: %q", after)
	}
	if found := leftovers(t, dir); len(found) != 0 {
		t.Errorf("temporary files left: %v", found)
	}
}

func TestNothingIsWrittenOrReadInTheCurrentDirectory(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	os.WriteFile("config.json", []byte(`{"url":"https://attacker.example"}`), 0o600)
	os.WriteFile("credentials.json", []byte(`{"https://attacker.example":"cwk_planted"}`), 0o600)
	if err := WritePrivateJSON("credentials.json", map[string]string{}); !errors.Is(err, ErrNoConfigDir) {
		t.Errorf("a relative path must be refused, got %v", err)
	}
	if cfg := Load(""); cfg.URL != "" {
		t.Errorf("no config directory reads no config, got %+v", cfg)
	}
	store := FileStore{Path: "credentials.json"}
	if _, err := store.Get("https://attacker.example"); !errors.Is(err, ErrNoToken) {
		t.Errorf("a relative token file holds nothing, got %v", err)
	}
	if _, err := store.Set("https://x", "cwk_x"); !errors.Is(err, ErrNoConfigDir) {
		t.Errorf("nor takes anything, got %v", err)
	}
	if raw, _ := os.ReadFile("credentials.json"); string(raw) != `{"https://attacker.example":"cwk_planted"}` {
		t.Errorf("the current directory's file was touched: %q", raw)
	}
}

func TestFileStoreKeepsOneTokenPerPlatform(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	if _, err := store.Get("https://a"); !errors.Is(err, ErrNoToken) {
		t.Fatalf("empty store: %v", err)
	}
	for u, token := range map[string]string{"https://a": "cwk_a", "https://b": "cwk_b"} {
		if where, err := store.Set(u, token); err != nil || where != store.Path {
			t.Fatalf("Set: %q %v", where, err)
		}
	}
	if err := store.Delete("https://a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("https://never"); err != nil {
		t.Errorf("deleting what is not there is fine: %v", err)
	}
	if _, err := store.Get("https://a"); !errors.Is(err, ErrNoToken) {
		t.Errorf("a is forgotten: %v", err)
	}
	if token, err := store.Get("https://b"); token != "cwk_b" || err != nil {
		t.Errorf("b is kept: %q %v", token, err)
	}
	if runtime.GOOS != "windows" && mode(t, store.Path) != 0o600 {
		t.Errorf("mode %v", mode(t, store.Path))
	}
}

func TestAFileThatSaysNullIsAnEmptyStore(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	if err := os.WriteFile(store.Path, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Set("https://a", "cwk_a"); err != nil { // panicked on a nil map
		t.Fatal(err)
	}
	if token, _ := store.Get("https://a"); token != "cwk_a" {
		t.Errorf("token %q", token)
	}
	if err := os.WriteFile(store.Path, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("https://a"); err != nil {
		t.Error(err)
	}
}

func TestTheKeychainFirstTheFileWhenThereIsNone(t *testing.T) {
	t.Cleanup(func() { keyring.MockInit() })
	fallback := FileStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	store := KeychainStore{Fallback: fallback}

	keyring.MockInitWithError(keyring.ErrUnsupportedPlatform)
	if where, err := store.Set("https://a", "cwk_file"); err != nil || where != fallback.Path {
		t.Fatalf("no keychain → the file: %q %v", where, err)
	}
	if token, _ := store.Get("https://a"); token != "cwk_file" {
		t.Errorf("token %q", token)
	}

	keyring.MockInit()
	if where, err := store.Set("https://a", "cwk_keychain"); err != nil || where != "the system keychain" {
		t.Fatalf("a keychain → it: %q %v", where, err)
	}
	if _, err := fallback.Get("https://a"); !errors.Is(err, ErrNoToken) {
		t.Errorf("the file's copy is removed once the keychain has it: %v", err)
	}
	if token, _ := store.Get("https://a"); token != "cwk_keychain" {
		t.Errorf("token %q", token)
	}
	if err := store.Delete("https://a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("https://a"); err != nil {
		t.Errorf("deleting twice is fine: %v", err)
	}
	if _, err := store.Get("https://a"); !errors.Is(err, ErrNoToken) {
		t.Errorf("forgotten: %v", err)
	}
}

func TestConfigRoundTripAndPlatforms(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cw")
	if cfg := Load(dir); cfg.URL != "" || len(cfg.Platforms) != 0 {
		t.Errorf("a missing config is empty: %+v", cfg)
	}
	cfg := Config{URL: "https://a"}
	cfg.Remember("https://a")
	cfg.Remember("https://b")
	cfg.Remember("https://a")
	if err := Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	got := Load(dir)
	if got.URL != "https://a" || len(got.Platforms) != 2 {
		t.Errorf("round trip: %+v", got)
	}
	got.Forget("https://a")
	if len(got.Platforms) != 1 || got.Platforms[0] != "https://b" {
		t.Errorf("forget: %v", got.Platforms)
	}
	os.WriteFile(path(dir), []byte("not json"), 0o600)
	if cfg := Load(dir); cfg.URL != "" {
		t.Errorf("an unreadable config is empty: %+v", cfg)
	}
}

func TestResolveOrder(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Config{URL: "https://saved.example"}); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CW_URL": "env.example"}
	getenv := func(k string) string { return env[k] }
	cases := []struct {
		explicit, env, want, source string
	}{
		{"flag.example", "env.example", "https://flag.example", FromFlag},
		{"", "env.example", "https://env.example", FromEnv},
		{"", "", "https://saved.example", FromSaved},
	}
	for _, c := range cases {
		env["CW_URL"] = c.env
		base, source, err := Resolve(c.explicit, getenv, dir)
		if err != nil || base != c.want || source != c.source {
			t.Errorf("%+v: got %q %q %v", c, base, source, err)
		}
	}
	if base, source, _ := Resolve("", getenv, t.TempDir()); base != DefaultURL || source != FromDefault {
		t.Errorf("default: %q %q", base, source)
	}
	if _, _, err := Resolve("http://plain.example", getenv, dir); err == nil {
		t.Error("a token never goes over plain http off this machine")
	}
}
