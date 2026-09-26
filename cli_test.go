package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goodToken = "cwk_test-token-0123456789"

type memoryStore map[string]string

func (m memoryStore) Get(u string) (string, error) {
	if token, ok := m[u]; ok {
		return token, nil
	}
	return "", errNoToken
}
func (m memoryStore) Set(u, token string) (string, error) { m[u] = token; return "memory", nil }
func (m memoryStore) Delete(u string) error               { delete(m, u); return nil }

// fakeAPI answers like /api/v1: routes map a path to the "data" it returns.
func fakeAPI(t *testing.T, routes map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer "+goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"code":"invalid_token","message":"Missing, revoked or expired API token"}}`))
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/api/v1")
		if r.URL.RawQuery != "" {
			if _, ok := routes[key+"?"+r.URL.RawQuery]; ok {
				key += "?" + r.URL.RawQuery
			}
		}
		data, ok := routes[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":"not_found","message":"No API function at ` + key + `"}}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	return server
}

type harness struct {
	app    *app
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	env    map[string]string
}

func newHarness(t *testing.T, server *httptest.Server) *harness {
	t.Helper()
	h := &harness{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, env: map[string]string{}}
	if server != nil {
		h.env["CW_URL"] = server.URL
		h.env["CW_TOKEN"] = goodToken
	}
	h.app = &app{
		stdin:      strings.NewReader(""),
		stdout:     h.stdout,
		stderr:     h.stderr,
		getenv:     func(key string) string { return h.env[key] },
		configDir:  t.TempDir(),
		secrets:    memoryStore{},
		httpClient: newHTTPClient(),
		interrupt:  func() (context.Context, func()) { return context.WithCancel(context.Background()) },
		wait:       func(ctx context.Context, _ time.Duration) bool { return ctx.Err() == nil },
		// Output captured into a buffer is never a terminal: plain unless asked.
		stdoutIsTerminal: func() bool { return false },
		clock:            time.Now,
	}
	return h
}

func (h *harness) run(args ...string) int { return h.app.run(args) }

var apps = map[string]any{
	"items": []map[string]any{
		{"id": 7, "name": "shop", "namespace": "acme", "environment_type": "production", "version": "19.0",
			"state": "deploy", "server": "prod-1", "backup_health": "healthy", "updated_at": nil},
		{"id": 8, "name": "shop-staging", "namespace": "acme", "environment_type": "staging", "version": "19.0",
			"state": "deploy", "server": nil, "backup_health": "none", "updated_at": "2026-09-24 09:00:00"},
	},
	"total": 2,
}

func TestParseArgsAcceptsFlagsAfterPositionals(t *testing.T) {
	h := newHarness(t, nil)
	fs := h.app.newFlags("x")
	asJSON := fs.Bool("json", false, "")
	positional, err := parseArgs(fs, []string{"show", "shop", "--json"})
	if err != nil || !*asJSON || strings.Join(positional, " ") != "show shop" {
		t.Fatalf("got %v %v %v", positional, *asJSON, err)
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"www.cloudwady.com":       "https://www.cloudwady.com",
		"https://example.com/":    "https://example.com",
		"http://localhost:8069/":  "http://localhost:8069",
		"http://127.0.0.1:8069":   "http://127.0.0.1:8069",
		"https://example.com/x?y": "https://example.com/x",
	}
	for in, want := range cases {
		if got, err := normalizeURL(in); err != nil || got != want {
			t.Errorf("normalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://example.com", "ftp://example.com", "https://"} {
		if _, err := normalizeURL(bad); err == nil {
			t.Errorf("normalizeURL(%q) accepted a URL it must refuse", bad)
		}
	}
}

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

func TestAppsTable(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/apps": apps}))
	if code := h.run("apps"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	out := h.stdout.String()
	for _, want := range []string{"ID", "NAME", "shop-staging", "production", "prod-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "NAMESPACE") {
		t.Errorf("one namespace: the column is noise\n%s", out)
	}
}

func TestShowResolvesAName(t *testing.T) {
	server := fakeAPI(t, map[string]any{
		"/apps":   apps,
		"/apps/7": map[string]any{"id": 7, "name": "shop", "state": "deploy", "deployed_at": "2026-09-01 08:00:00"},
	})
	h := newHarness(t, server)
	if code := h.run("apps", "show", "SHOP"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(h.stdout.String(), "Name:") || !strings.Contains(h.stdout.String(), "shop") {
		t.Errorf("stdout: %s", h.stdout)
	}
}

func TestAnAmbiguousNameAsksForTheID(t *testing.T) {
	twins := map[string]any{"items": []map[string]any{
		{"id": 1, "name": "shop", "namespace": "a"}, {"id": 2, "name": "shop", "namespace": "b"},
	}, "total": 2}
	h := newHarness(t, fakeAPI(t, map[string]any{"/apps": twins}))
	if code := h.run("apps", "show", "shop"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(h.stderr.String(), "1 (a), 2 (b)") {
		t.Errorf("stderr: %s", h.stderr)
	}
}

func TestBackupsFilterByAppName(t *testing.T) {
	server := fakeAPI(t, map[string]any{
		"/apps": apps,
		"/backups?app_id=7&limit=50&offset=0": map[string]any{"items": []map[string]any{
			{"id": 3, "app": "shop", "namespace": "acme", "size_mb": 2048, "automated": true, "taken_at": "2026-09-24 01:00:00"},
		}, "total": 1},
	})
	h := newHarness(t, server)
	if code := h.run("backups", "--app", "shop"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(h.stdout.String(), "2.0 GB") || !strings.Contains(h.stdout.String(), "yes") {
		t.Errorf("stdout: %s", h.stdout)
	}
}

func TestRunShowListsStepsAndWhyOneFailed(t *testing.T) {
	server := fakeAPI(t, map[string]any{"/runs/5": map[string]any{
		"id": 5, "workflow": "Deploy", "state": "error",
		"steps": []map[string]any{
			{"name": "Prepare", "state": "success"},
			{"name": "Deploy", "state": "error", "reason": "Pull image: deploy failed\ndb_password=***REDACTED***"},
		},
	}})
	h := newHarness(t, server)
	if code := h.run("runs", "show", "5"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	out := h.stdout.String()
	if !strings.Contains(out, "Steps:") || !strings.Contains(out, "2. Deploy\n     Pull image: deploy failed\n     db_password") {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestRunsAreLookedUpByIDOnly(t *testing.T) {
	h := newHarness(t, fakeAPI(t, nil))
	if code := h.run("runs", "show", "deploy"); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestJSONPrintsTheAPIData(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/apps": apps}))
	if code := h.run("apps", "--json"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	var got struct{ Total int }
	if err := json.Unmarshal(h.stdout.Bytes(), &got); err != nil || got.Total != 2 {
		t.Errorf("not the API data: %v %s", err, h.stdout)
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

func TestAHTMLPageIsNotThePlatform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>login</html>"))
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	if code := h.run("servers"); code != 1 || !strings.Contains(h.stderr.String(), "is the URL right?") {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestAgo(t *testing.T) {
	now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })
	cases := map[any]string{
		"2026-09-24 11:59:30":  "just now",
		"2026-09-24T11:15:00Z": "45m ago",
		"2026-09-24 11:15:00":  "45m ago",
		"2026-09-23 12:00:00":  "24h ago",
		"2026-09-20 12:00:00":  "4d ago",
		nil:                    "-",
	}
	for in, want := range cases {
		if got := ago(in); got != want {
			t.Errorf("ago(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestAParameterTheAPIRefusesIsAUsageError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":"invalid_parameter","message":"limit is out of range"}}`))
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	if code := h.run("apps", "--limit", "0"); code != 2 {
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
				if err := h.app.saveConfig(config{URL: c.saved}); err != nil {
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

func logAnswer(cursor string, lines ...string) map[string]any {
	items := make([]map[string]any, len(lines))
	for i, line := range lines {
		items[i] = map[string]any{"time": "2026-09-26T10:00:00.000000001Z", "line": line}
	}
	return map[string]any{"items": items, "cursor": cursor, "truncated": false}
}

func TestLogsPrintsTheLinesOfTheNamedApp(t *testing.T) {
	server := fakeAPI(t, map[string]any{
		"/apps": apps,
		"/apps/7/logs?grep=ERROR&limit=50&since=15m": logAnswer("9", "first ERROR", "second ERROR"),
	})
	h := newHarness(t, server)
	if code := h.run("logs", "shop", "--since", "15m", "--grep", "ERROR", "--limit", "50"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if h.stdout.String() != "first ERROR\nsecond ERROR\n" {
		t.Errorf("stdout %q", h.stdout)
	}
}

func TestLogsFollowAsksForLinesAfterTheCursor(t *testing.T) {
	var afters []string
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		afters = append(afters, after)
		answer := map[string]any{"": logAnswer("100", "old line"), "100": logAnswer("101", "new line")}[after]
		if after == "101" {
			cancel() // the user pressed Ctrl-C
			answer = logAnswer("101")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": answer})
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	h.app.interrupt = func() (context.Context, func()) { return ctx, cancel }
	if code := h.run("logs", "7", "--follow"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if h.stdout.String() != "old line\nnew line\n" {
		t.Errorf("stdout %q", h.stdout)
	}
	if strings.Join(afters, ",") != ",100,101" {
		t.Errorf("after cursors %q", afters)
	}
}

func TestLogsThatAreNotCollectedSaySo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"code":"unavailable","message":"This app's logs are not collected"}}`))
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	if code := h.run("logs", "7"); code != 1 || !strings.Contains(h.stderr.String(), "not collected") {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestLogsNeedsOneApp(t *testing.T) {
	h := newHarness(t, fakeAPI(t, nil))
	if code := h.run("logs"); code != 2 {
		t.Fatalf("exit %d", code)
	}
}
