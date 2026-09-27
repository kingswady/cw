package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fiveTwins are five apps named v19-0, as the platform lists them for
// search=v19-0 (a name containing it, v19-0-old, comes along).
var fiveTwins = map[string]any{"items": []map[string]any{
	{"id": 1101, "name": "v19-0", "project": "internal", "environment_type": "production", "server": "prod-1", "namespace": "acme"},
	{"id": 1102, "name": "v19-0", "project": "internal", "environment_type": "staging", "server": "stage-1", "namespace": "acme"},
	{"id": 1103, "name": "v19-0", "project": "webshop", "environment_type": "production", "server": "prod-1", "namespace": "acme"},
	{"id": 1104, "name": "v19-0", "project": "webshop", "environment_type": "staging", "server": "stage-1", "namespace": "acme"},
	{"id": 1105, "name": "V19-0", "project": "crm", "environment_type": "development", "server": "dev-1", "namespace": "acme"},
	{"id": 1106, "name": "v19-0-old", "project": "internal", "environment_type": "production", "server": "prod-1", "namespace": "acme"},
}, "total": 6}

// oneOf is a list page of the one item.
func oneOf(item map[string]any) map[string]any {
	return map[string]any{"items": []map[string]any{item}, "total": 1}
}

var internalProduction = map[string]any{
	"id": 1101, "name": "v19-0", "project": "internal", "environment_type": "production", "server": "prod-1", "namespace": "acme",
}

func TestShowNarrowsANameWithTheListsFilters(t *testing.T) {
	server, seen := recordedAPI(t, map[string]any{
		"/apps?environment_type=production&limit=200&offset=0&project=internal&search=v19-0": oneOf(internalProduction),
		"/apps/1101": internalProduction,
	})
	h := newHarness(t, server)
	if code := h.run("apps", "show", "v19-0", "--project", "internal", "--env", "production"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(h.stdout.String(), "1101") {
		t.Errorf("stdout:\n%s", h.stdout)
	}
	// One question to the platform finds the name: no paging through every app.
	if lookups := seen.to("/apps"); len(lookups) != 1 {
		t.Errorf("lookups %q", lookups)
	}
}

func TestALookupAsksForTheNameAndMatchesItExactly(t *testing.T) {
	server, seen := recordedAPI(t, map[string]any{
		"/apps":      fiveTwins,
		"/apps/1106": map[string]any{"id": 1106, "name": "v19-0-old"},
	})
	h := newHarness(t, server)
	// /apps answers every query with fiveTwins here: of them, only 1106 is named v19-0-old.
	if code := h.run("apps", "show", "v19-0-old"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if got := seen.to("/apps"); len(got) != 1 || got[0] != "/apps?limit=200&offset=0&search=v19-0-old" {
		t.Errorf("lookup %q", got)
	}
}

func TestBackupsNarrowTheAppWithoutFilteringTheBackups(t *testing.T) {
	server, seen := recordedAPI(t, map[string]any{
		"/apps?limit=200&offset=0&project=internal&search=v19-0": map[string]any{"items": []map[string]any{
			internalProduction,
			{"id": 1106, "name": "v19-0-old", "project": "internal", "namespace": "acme"},
		}, "total": 2},
		"/backups?app_id=1101&limit=50&offset=0": oneOf(map[string]any{"id": 3, "app": "v19-0", "namespace": "acme"}),
	})
	h := newHarness(t, server)
	if code := h.run("backups", "--app", "v19-0", "--project", "internal"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if got := seen.to("/backups"); len(got) != 1 || strings.Contains(got[0], "project") {
		t.Errorf("--project narrows the app, never the backups: %q", got)
	}
}

func TestRunsStateIsTheRunsOwnNotTheApps(t *testing.T) {
	server, seen := recordedAPI(t, map[string]any{
		"/apps?environment_type=production&limit=200&offset=0&search=v19-0": oneOf(internalProduction),
		"/runs?app_id=1101&limit=50&offset=0&state=error":                   oneOf(map[string]any{"id": 5, "workflow": "Deploy", "state": "error"}),
	})
	h := newHarness(t, server)
	if code := h.run("runs", "--app", "v19-0", "--env", "production", "--state", "error"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if lookups := seen.to("/apps"); len(lookups) != 1 || strings.Contains(lookups[0], "state") {
		t.Errorf("--state must not narrow the app: %q", lookups)
	}
}

func TestLogsNarrowTheirApp(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{
		"/apps?limit=200&offset=0&project=internal&search=v19-0&version=19.0": oneOf(internalProduction),
		"/apps/1101/logs?limit=200&since=1h":                                  logAnswer("9", "a line"),
	}))
	if code := h.run("logs", "v19-0", "--project", "internal", "--version", "19.0"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if h.stdout.String() != "a line\n" {
		t.Errorf("stdout %q", h.stdout)
	}
}

func TestAServerNameNarrowsAnAppLookup(t *testing.T) {
	server, seen := recordedAPI(t, map[string]any{
		"/servers": map[string]any{"items": []map[string]any{
			{"id": 3, "name": "prod-1", "namespace": "acme"}, {"id": 4, "name": "stage-1", "namespace": "acme"},
		}, "total": 2},
		"/apps?limit=200&namespace=acme&offset=0&search=v19-0&server_id=3": oneOf(internalProduction),
		"/runs?app_id=1101&limit=50&namespace=acme&offset=0":               map[string]any{"items": []any{}, "total": 0},
	})
	h := newHarness(t, server)
	if code := h.run("runs", "--app", "v19-0", "--server", "prod-1", "--namespace", "acme"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	// Servers have no name search: they are paged through, in the namespace asked for.
	if got := seen.to("/servers"); len(got) != 1 || got[0] != "/servers?limit=200&namespace=acme&offset=0" {
		t.Errorf("server lookup %q", got)
	}
}

func TestNoMatchSaysWhichFiltersWereApplied(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/apps": map[string]any{"items": []any{}, "total": 0}}))
	if code := h.run("backups", "--app", "v19-0", "--project", "big shop", "--env", "staging", "--namespace", "acme"); code != 1 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	want := `cw: no app named "v19-0" with --namespace acme --project "big shop" --env staging in this token's namespaces`
	if !strings.Contains(h.stderr.String(), want) {
		t.Errorf("stderr:\n%s", h.stderr)
	}
}

func TestAStillAmbiguousNameOffersTheFlagsNotUsedYet(t *testing.T) {
	stillTwo := map[string]any{"items": []map[string]any{
		internalProduction,
		{"id": 1102, "name": "v19-0", "project": "internal", "environment_type": "production", "server": "prod-2", "namespace": "acme"},
	}, "total": 2}
	h := newHarness(t, fakeAPI(t, map[string]any{"/apps": stillTwo}))
	if code := h.run("logs", "v19-0", "--project", "internal", "--env", "production"); code != 1 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	stderr := h.stderr.String()
	for _, want := range []string{
		`2 apps are named "v19-0" with --project internal --env production:`,
		"1101", "1102", "prod-2",
		`cw: use one of these ids instead of "v19-0", or narrow it with --server, --version, --edition or --namespace`,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("missing %q in:\n%s", want, stderr)
		}
	}
}

func TestShowOffersItsOwnFiltersWhenAmbiguous(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/apps": fiveTwins}))
	if code := h.run("apps", "show", "v19-0"); code != 1 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	stderr := h.stderr.String()
	if !strings.HasPrefix(stderr, `5 apps are named "v19-0":`) {
		t.Errorf("any case, exact name only (not v19-0-old):\n%s", stderr)
	}
	if !strings.Contains(stderr, "or narrow it with --project, --env, --server, --version, --edition, --state or --namespace") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestNarrowingWithoutAppIsAMistake(t *testing.T) {
	h := newHarness(t, fakeAPI(t, nil))
	if code := h.run("backups", "--project", "internal", "--env", "production"); code != 2 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if !strings.Contains(h.stderr.String(), "--project and --env only narrow which app --app names; add --app <name>") {
		t.Errorf("stderr:\n%s", h.stderr)
	}
	one := newHarness(t, fakeAPI(t, nil))
	if code := one.run("runs", "--server", "prod-1"); code != 2 || !strings.Contains(one.stderr.String(), "--server only narrows which app --app names") {
		t.Errorf("exit %d: %s", code, one.stderr)
	}
}

func TestShowRefusesSearch(t *testing.T) {
	h := newHarness(t, fakeAPI(t, nil))
	if code := h.run("apps", "show", "v19-0", "--search", "v19"); code != 2 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestAnIDIsTakenAsGivenWhateverTheFilters(t *testing.T) {
	server, seen := recordedAPI(t, map[string]any{"/apps/1101": internalProduction})
	h := newHarness(t, server)
	if code := h.run("apps", "show", "1101", "--project", "elsewhere", "--server", "nowhere"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if got := seen.to("/apps"); len(got) != 0 {
		t.Errorf("an id needs no lookup: %q", got)
	}
	if got := seen.to("/servers"); len(got) != 0 {
		t.Errorf("nor a server lookup: %q", got)
	}
}

func TestInstallersKeepPagingWithTheirFilters(t *testing.T) {
	page := func(offset int) map[string]any {
		items := make([]map[string]any, 0, 200)
		for i := 0; i < 200 && offset+i < 250; i++ {
			name := "other"
			if offset+i == 230 {
				name = "grafana"
			}
			items = append(items, map[string]any{"id": 5000 + offset + i, "name": name, "namespace": "acme"})
		}
		return map[string]any{"items": items, "total": 250}
	}
	server, seen := recordedAPI(t, map[string]any{
		"/servers": oneOf(map[string]any{"id": 3, "name": "prod-1", "namespace": "acme"}),
		"/installers?limit=200&offset=0&server_id=3&state=running":   page(0),
		"/installers?limit=200&offset=200&server_id=3&state=running": page(200),
		"/installers/5230": map[string]any{"id": 5230, "name": "grafana"},
	})
	h := newHarness(t, server)
	if code := h.run("installers", "show", "grafana", "--server", "prod-1", "--state", "running"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if got := seen.to("/installers"); len(got) != 2 {
		t.Errorf("two pages of the filtered list: %q", got)
	}
}

func TestAnOlderPlatformWithoutSearchIsPagedThrough(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Has("search") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter(s): search"}}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": apps})
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	if code := h.run("backups", "--app", "shop"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if len(queries) != 3 || strings.Contains(queries[1], "search") || !strings.Contains(queries[2], "app_id=7") {
		t.Errorf("queries %q", queries)
	}
}

func TestEveryCommandsFlagsAreDistinct(t *testing.T) {
	// A narrowing flag with the name of a command's own flag would panic here
	// (flag redefined), before any user saw it.
	for _, res := range resources {
		h := newHarness(t, nil)
		if code := h.run(res.name, "--help"); code != 0 {
			t.Errorf("%s --help: exit %d", res.name, code)
		}
	}
	h := newHarness(t, nil)
	if code := h.run("logs", "--help"); code != 0 {
		t.Errorf("logs --help: exit %d", code)
	}
	for _, want := range []string{"narrows <app> to an app whose project name or code contains this", "cw logs v19-0 --project internal"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("logs --help: missing %q in\n%s", want, h.stderr)
		}
	}
	runs := newHarness(t, nil)
	runs.run("runs", "--help")
	for _, want := range []string{
		"--app takes an app's id or name", "--project, --env, --server, --version and --edition",
		"narrows --app to an app on this server (id or name)", "only runs in this state",
	} {
		if !strings.Contains(runs.stderr.String(), want) {
			t.Errorf("runs --help: missing %q in\n%s", want, runs.stderr)
		}
	}
}
