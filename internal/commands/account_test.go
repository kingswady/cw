package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kingswady/cw/internal/config"
)

func TestLoginValidatesAndSavesTheToken(t *testing.T) {
	server := fakeAPI(t, map[string]any{"/whoami": map[string]any{"login": "ana@acme.test", "name": "Ana", "namespaces": []any{}}})
	h := newHarness(t, nil)
	h.app.stdin = strings.NewReader(goodToken + "\n")
	if code := h.run("login", "--url", server.URL, "--with-token"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(h.stdout.String(), "as Ana (ana@acme.test)") {
		t.Errorf("stdout: %s", h.stdout)
	}
	if token, _ := h.app.secrets.Get(server.URL); token != goodToken {
		t.Errorf("token not saved for %s", server.URL)
	}
	if h.app.loadConfig().URL != server.URL {
		t.Errorf("URL not saved")
	}
	info, err := os.Stat(filepath.Join(h.app.configDir, "config.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("config must be private: %v %v", info, err)
	}
}

func TestLoginRefusesWhatIsNotAToken(t *testing.T) {
	server := fakeAPI(t, nil)
	h := newHarness(t, nil)
	h.app.stdin = strings.NewReader("hunter2")
	if code := h.run("login", "--url", server.URL, "--with-token"); code != 2 {
		t.Fatalf("exit %d", code)
	}
	h.app.stdin = strings.NewReader("cwk_revoked")
	if code := h.run("login", "--url", server.URL, "--with-token"); code != 1 {
		t.Fatalf("a refused token must not log in: exit %d", code)
	}
	if len(h.app.secrets.(memoryStore)) != 0 {
		t.Errorf("a refused token was saved")
	}
}

func TestARevokedTokenSaysHowToRecover(t *testing.T) {
	h := newHarness(t, fakeAPI(t, nil))
	h.env["CW_TOKEN"] = "cwk_revoked"
	if code := h.run("apps"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.stderr.String(), "run: cw login") {
		t.Errorf("stderr: %s", h.stderr)
	}
}

func TestNotLoggedIn(t *testing.T) {
	h := newHarness(t, nil)
	h.env["CW_URL"] = "https://platform.example"
	if code := h.run("apps"); code != 1 || !strings.Contains(h.stderr.String(), "not logged in to https://platform.example") {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestARedirectIsNeverFollowedWithTheToken(t *testing.T) {
	followed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/web/login" {
			followed = true
		}
		http.Redirect(w, r, "/web/login?redirect=/api/v1/whoami", http.StatusSeeOther)
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, nil)
	h.app.stdin = strings.NewReader(goodToken)
	if code := h.run("login", "--url", server.URL, "--with-token"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if followed {
		t.Error("the redirect was followed with the token")
	}
	if !strings.Contains(h.stderr.String(), "(it redirects to /web/login)") {
		t.Errorf("stderr: %s", h.stderr)
	}
}

func TestTheLoginPromptNamesThePlatform(t *testing.T) {
	server := fakeAPI(t, map[string]any{"/whoami": map[string]any{"login": "a", "name": "A"}})
	h := newHarness(t, nil)
	var prompted string
	h.app.readSecret = func(prompt string) (string, error) { prompted = h.stderr.String() + prompt; return goodToken, nil }
	if code := h.run("login", "--url", server.URL); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(prompted, "Logging in to "+server.URL) {
		t.Errorf("prompt: %q", prompted)
	}
}

func TestTheLoginPromptSaysWhereTheURLCameFrom(t *testing.T) {
	server := fakeAPI(t, map[string]any{"/whoami": map[string]any{"login": "a", "name": "A"}})
	cases := []struct {
		name, flag, env, saved, want string
	}{
		{"typed", server.URL, "", "", "Logging in to " + server.URL + "\n"},
		{"saved", "", "", server.URL, "Logging in to " + server.URL + " (your last platform — use --url for another)\n"},
		{"env", "", server.URL, "", "Logging in to " + server.URL + " (from CW_URL)\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.env["CW_URL"] = c.env
			if c.saved != "" {
				if err := h.app.saveConfig(config.Config{URL: c.saved}); err != nil {
					t.Fatal(err)
				}
			}
			var prompted string
			h.app.readSecret = func(prompt string) (string, error) { prompted = h.stderr.String(); return goodToken, nil }
			args := []string{"login"}
			if c.flag != "" {
				args = append(args, "--url", c.flag)
			}
			if code := h.run(args...); code != 0 {
				t.Fatalf("exit %d: %s", code, h.stderr)
			}
			if prompted != c.want {
				t.Errorf("prompt %q, want %q", prompted, c.want)
			}
		})
	}
}

func TestUseSwitchesBetweenPlatformsLoggedInTo(t *testing.T) {
	h := newHarness(t, nil)
	h.app.secrets.Set("https://one.example", goodToken)
	h.app.secrets.Set("https://two.example", goodToken)
	h.app.saveConfig(config.Config{URL: "https://one.example", Platforms: []string{"https://one.example", "https://two.example"}})
	if code := h.run("use", "two.example"); code != 0 || h.app.loadConfig().URL != "https://two.example" {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	h.stdout.Reset()
	h.run("use")
	if !strings.Contains(h.stdout.String(), "* https://two.example") || !strings.Contains(h.stdout.String(), "  https://one.example") {
		t.Errorf("list %q", h.stdout)
	}
	if code := h.run("use", "https://three.example"); code != 1 || !strings.Contains(h.stderr.String(), "cw login --url https://three.example") {
		t.Errorf("exit %d: %s", code, h.stderr)
	}
}
