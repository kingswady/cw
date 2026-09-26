package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const infoLine = "2026-09-26 13:16:25,466 56 INFO v19-0 odoo.addons.base.models.ir_cron: Job done"

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

func TestWhenLogsAreColoured(t *testing.T) {
	cases := []struct {
		mode     string
		terminal bool
		noColor  string
		json     bool
		want     bool
	}{
		{"auto", true, "", false, true},
		{"auto", false, "", false, false},
		{"auto", true, "1", false, false},
		{"auto", true, "", true, false},
		{"always", false, "1", false, true},
		{"always", true, "", true, false},
		{"never", true, "", false, false},
	}
	for _, c := range cases {
		h := newHarness(t, nil)
		h.app.stdoutIsTerminal = func() bool { return c.terminal }
		h.env["NO_COLOR"] = c.noColor
		got, err := h.app.useColor(c.mode, c.json)
		if err != nil || got != c.want {
			t.Errorf("%+v: got %v, %v", c, got, err)
		}
	}
	if _, err := newHarness(t, nil).app.useColor("rainbow", false); err == nil {
		t.Error("an unknown --color value must be refused")
	}
}

func TestColorAlwaysColoursEvenWhenPiped(t *testing.T) {
	server := fakeAPI(t, map[string]any{"/apps/7/logs?limit=200&since=1h": logAnswer("9", infoLine)})
	h := newHarness(t, server)
	if code := h.run("logs", "7", "--color", "always"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(h.stdout.String(), "\x1b[1;32mINFO") {
		t.Errorf("stdout %q", h.stdout)
	}
}

func TestLogsFollowWaitsOutTheRateLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			json.NewEncoder(w).Encode(map[string]any{"data": logAnswer("100", "old line")})
		case 2:
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"code":"rate_limited","message":"Too many requests; retry in 7 s"}}`))
		default:
			cancel() // the user pressed Ctrl-C
			json.NewEncoder(w).Encode(map[string]any{"data": logAnswer("101", "new line")})
		}
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	var pauses []time.Duration
	h.app.interrupt = func() (context.Context, func()) { return ctx, cancel }
	h.app.wait = func(ctx context.Context, d time.Duration) bool {
		pauses = append(pauses, d)
		return ctx.Err() == nil
	}
	if code := h.run("logs", "7", "--follow"); code != 0 {
		t.Fatalf("a rate limit must not end --follow: exit %d: %s", code, h.stderr)
	}
	if h.stdout.String() != "old line\nnew line\n" {
		t.Errorf("stdout %q", h.stdout)
	}
	if len(pauses) < 2 || pauses[1] != 7*time.Second {
		t.Errorf("pauses %v: the second wait must be the platform's Retry-After", pauses)
	}
}
