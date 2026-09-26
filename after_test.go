package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// expiringAPI answers /whoami with the given token expiry header.
func expiringAPI(t *testing.T, expires time.Time) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Token-Expires-At", expires.UTC().Format(time.RFC3339))
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"login": "a", "name": "A", "namespaces": []any{}}})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestATokenInItsLastWeekIsWarnedAbout(t *testing.T) {
	h := newHarness(t, expiringAPI(t, time.Now().Add(3*24*time.Hour+time.Hour)))
	if code := h.run("whoami"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(h.stderr.String(), "expires in 3 days") {
		t.Errorf("stderr %q", h.stderr)
	}
	if !strings.Contains(h.stdout.String(), "Token expires ") {
		t.Errorf("whoami should show the expiry: %q", h.stdout)
	}
	far := newHarness(t, expiringAPI(t, time.Now().Add(60*24*time.Hour)))
	far.run("whoami")
	if strings.Contains(far.stderr.String(), "expires") {
		t.Errorf("no warning two months out: %q", far.stderr)
	}
}

func TestUseSwitchesBetweenPlatformsLoggedInTo(t *testing.T) {
	h := newHarness(t, nil)
	h.app.secrets.Set("https://one.example", goodToken)
	h.app.secrets.Set("https://two.example", goodToken)
	h.app.saveConfig(config{URL: "https://one.example", Platforms: []string{"https://one.example", "https://two.example"}})
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

func TestAnUpdateHintAtMostOnceADayInATerminal(t *testing.T) {
	checks := 0
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks++
		http.Redirect(w, r, "/tag/v9.9.9", http.StatusFound)
	}))
	t.Cleanup(releases.Close)
	previous := version
	version = "0.3.0"
	t.Cleanup(func() { version = previous })
	api := fakeAPI(t, map[string]any{"/whoami": map[string]any{"login": "a", "name": "A"}})

	h := newHarness(t, api)
	h.env["CW_DOWNLOAD_BASE"] = releases.URL
	h.app.stdoutIsTerminal = func() bool { return true }
	h.run("whoami")
	if !strings.Contains(h.stderr.String(), "cw v9.9.9 is available (you have v0.3.0)") {
		t.Fatalf("stderr %q", h.stderr)
	}
	h.stderr.Reset()
	h.run("whoami") // same config dir: the cached answer, no second request
	if checks != 1 || !strings.Contains(h.stderr.String(), "v9.9.9 is available") {
		t.Errorf("checks %d, stderr %q", checks, h.stderr)
	}

	for name, tweak := range map[string]func(*harness){
		"in CI":   func(h *harness) { h.env["CI"] = "true" },
		"opt-out": func(h *harness) { h.env["CW_NO_UPDATE_CHECK"] = "1" },
		"piped":   func(h *harness) { h.app.stdoutIsTerminal = func() bool { return false } },
	} {
		quiet := newHarness(t, api)
		quiet.env["CW_DOWNLOAD_BASE"] = releases.URL
		quiet.app.stdoutIsTerminal = func() bool { return true }
		tweak(quiet)
		quiet.run("whoami")
		if strings.Contains(quiet.stderr.String(), "available") {
			t.Errorf("%s: %q", name, quiet.stderr)
		}
	}
}
