package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		case 3:
			json.NewEncoder(w).Encode(map[string]any{"data": logAnswer("101", "new line")})
		default:
			cancel() // the user pressed Ctrl-C
			json.NewEncoder(w).Encode(map[string]any{"data": logAnswer("101")})
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

func TestCtrlCEndsAFollowInTheMiddleOfARequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") == "" {
			json.NewEncoder(w).Encode(map[string]any{"data": logAnswer("100", "old line")})
			return
		}
		cancel() // Ctrl-C while the platform is still answering
		select {
		case <-r.Context().Done(): // cw hung up
		case <-time.After(10 * time.Second):
		}
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	h.app.interrupt = func() (context.Context, func()) { return ctx, cancel }
	started := time.Now()
	if code := h.run("logs", "7", "--follow"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("Ctrl-C waited %v for the request to finish", took)
	}
	if strings.Contains(h.stderr.String(), "retrying") {
		t.Errorf("an interrupted request is not an error: %s", h.stderr)
	}
}

func TestCtrlCEndsAFollowThatIsCatchingUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var afters []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		after := r.URL.Query().Get("after")
		mu.Lock()
		afters = append(afters, after)
		mu.Unlock()
		page := logAnswer("100", "old line")
		if after != "" {
			// Far behind: every page is full, there is always a next one.
			page = logAnswer(after+"0", "line after "+after)
			page["truncated"] = true
		}
		json.NewEncoder(w).Encode(map[string]any{"data": page})
		if after == "1000" {
			cancel() // Ctrl-C once the second page is printed
		}
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	h.app.interrupt = func() (context.Context, func()) { return ctx, cancel }
	done := make(chan int)
	go func() { done <- h.run("logs", "7", "--follow") }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d: %s", code, h.stderr)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Ctrl-C did not stop the catch-up")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(afters) > 4 {
		t.Errorf("kept paging after Ctrl-C: %q", afters)
	}
}

func sourcedAnswer(cursor string, lines ...[2]string) map[string]any {
	items := make([]map[string]any, len(lines))
	for i, line := range lines {
		items[i] = map[string]any{"time": "2026-09-26T10:00:00.000000001Z", "source": line[0], "line": line[1]}
	}
	return map[string]any{"items": items, "cursor": cursor, "truncated": false}
}

func TestSeveralLogsNameEachLineByItsLog(t *testing.T) {
	server := fakeAPI(t, map[string]any{
		"/apps/7/logs?limit=200&since=1h&source=main%2Crestore": sourcedAnswer("9",
			[2]string{"main", "odoo started"}, [2]string{"restore", "restoring"}),
	})
	h := newHarness(t, server)
	if code := h.run("logs", "7", "--source", "main, Restore"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if h.stdout.String() != "[main] odoo started\n[restore] restoring\n" {
		t.Errorf("stdout %q", h.stdout)
	}
}

func TestOneLogIsPrintedAsItIs(t *testing.T) {
	server := fakeAPI(t, map[string]any{
		// main is never sent: a platform that predates sources refuses the parameter.
		"/apps/7/logs?limit=200&since=1h":              logAnswer("9", "odoo line"),
		"/apps/7/logs?limit=200&since=1h&source=setup": sourcedAnswer("9", [2]string{"setup", "installing"}),
	})
	for source, want := range map[string]string{"main": "odoo line\n", "setup": "installing\n"} {
		h := newHarness(t, server)
		if code := h.run("logs", "7", "--source", source); code != 0 {
			t.Fatalf("%s: exit %d: %s", source, code, h.stderr)
		}
		if h.stdout.String() != want {
			t.Errorf("%s: stdout %q", source, h.stdout)
		}
	}
}

func TestAllNamesEveryLine(t *testing.T) {
	server := fakeAPI(t, map[string]any{
		"/apps/7/logs?limit=200&since=1h&source=all": sourcedAnswer("9", [2]string{"backup", "dumped"}),
	})
	h := newHarness(t, server)
	if code := h.run("logs", "7", "--source", "all"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if h.stdout.String() != "[backup] dumped\n" {
		t.Errorf("stdout %q", h.stdout)
	}
}

func TestFollowKeepsAskingForTheSameLogs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var sources []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sources = append(sources, r.URL.Query().Get("source"))
		answer := sourcedAnswer("100", [2]string{"setup", "step one"})
		if r.URL.Query().Get("after") != "" {
			cancel() // the user pressed Ctrl-C
			answer = sourcedAnswer("100")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": answer})
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	h.app.interrupt = func() (context.Context, func()) { return ctx, cancel }
	if code := h.run("logs", "7", "--source", "setup,script", "--follow"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if strings.Join(sources, " ") != "setup,script setup,script" {
		t.Errorf("sources asked %q", sources)
	}
}

func TestAPlatformWithoutSourcesSaysSo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter(s): source"}}`))
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	if code := h.run("logs", "7", "--source", "setup"); code != 1 || !strings.Contains(h.stderr.String(), "does not offer --source") {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestAnEmptyWorkflowLogSaysWhy(t *testing.T) {
	server := fakeAPI(t, map[string]any{"/apps/7/logs?limit=200&since=1h&source=restore": sourcedAnswer("9")})
	h := newHarness(t, server)
	if code := h.run("logs", "7", "--source", "restore"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(h.stderr.String(), "No lines from restore in the last 1h") {
		t.Errorf("stderr %q", h.stderr)
	}
}
