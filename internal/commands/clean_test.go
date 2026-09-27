package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// hostile is what a platform could put in any text: an escape that clears
// the screen, one that retitles the terminal, a bell, a carriage return and a CSI.
const hostile = "\x1b[2J\x1b]0;owned\x07\r\u009b31m"

// assertClean fails when out holds a control character but tab and newline.
func assertClean(t *testing.T, what, out string) {
	t.Helper()
	for _, r := range out {
		if (r < 0x20 && r != '\t' && r != '\n') || (r >= 0x7f && r <= 0x9f) {
			t.Errorf("%s holds control character %U:\n%q", what, r, out)
			return
		}
	}
}

func TestPlatformTextReachesTheTerminalClean(t *testing.T) {
	server := fakeAPI(t, map[string]any{
		"/apps": map[string]any{"items": []map[string]any{
			{"id": 7, "name": "shop" + hostile, "namespace": "acme" + hostile, "state": "deploy" + hostile},
			{"id": 8, "name": "shop" + hostile, "namespace": "beta", "state": "deploy"},
		}, "total": 2},
		"/apps/7":      map[string]any{"id": 7, "name": "shop" + hostile, "url": "https://x" + hostile},
		"/apps/7/logs": logAnswer("9", "line"+hostile+"\nsecond"),
		"/runs/5": map[string]any{"id": 5, "workflow": "Deploy" + hostile, "state": "error", "steps": []map[string]any{
			{"name": "Pull" + hostile, "state": "error", "reason": "denied" + hostile + "\nsecond line"},
		}},
		"/attention": map[string]any{"sections": []map[string]any{
			{"severity": "danger" + hostile, "label": "Apps in error" + hostile, "kind": "app", "count": 1,
				"items": []map[string]any{{"id": 7, "name": "shop" + hostile, "namespace": "acme"}}},
		}},
		"/whoami": map[string]any{"login": "a" + hostile, "name": "A" + hostile,
			"namespaces": []map[string]any{{"name": "Acme" + hostile, "code": "acme", "level": "admin"}}},
	})
	for _, args := range [][]string{
		{"apps"}, {"apps", "show", "7"}, {"apps", "show", "shop" + hostile}, {"logs", "7"}, {"runs", "show", "5"},
		{"attention"}, {"whoami"}, {"apps", "--json"},
	} {
		for _, color := range []string{"never", "always"} {
			h := newHarness(t, server)
			h.run(append(args, "--color", color)...)
			stdout, stderr := h.stdout.String(), h.stderr.String()
			if color == "always" {
				// cw's own colours are the only escapes left.
				stdout, stderr = stripOwnColours(stdout), stripOwnColours(stderr)
			}
			assertClean(t, strings.Join(args, " ")+" --color "+color+" stdout", stdout)
			assertClean(t, strings.Join(args, " ")+" --color "+color+" stderr", stderr)
		}
	}
}

func TestAPlatformErrorMessageIsClean(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":"forbidden","message":"no\u001b[2J\u0007 access"}}`))
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	if code := h.run("apps"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	assertClean(t, "stderr", h.stderr.String())
	if !strings.Contains(h.stderr.String(), "cw: no[2J access") {
		t.Errorf("stderr %q", h.stderr)
	}
}

// stripOwnColours removes the SGR colour codes cw writes itself.
func stripOwnColours(s string) string {
	var out strings.Builder
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return out.String() + s
		}
		j := i + 2
		for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			out.WriteString(s[:i])
			s = s[j+1:]
			continue
		}
		out.WriteString(s[:i+1]) // not a colour: keep it for assertClean to find
		s = s[i+1:]
	}
}
