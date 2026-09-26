package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestColouredCellsStayAligned(t *testing.T) {
	var out bytes.Buffer
	rows := [][]string{{paint(ansiGreen, "deploy"), "a"}, {"error", "b"}}
	if err := writeTable(&out, boldAll([]string{"STATE", "X"}), rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var columns []int
	for _, line := range lines {
		plain := ansiEscape.ReplaceAllString(line, "")
		columns = append(columns, len(plain)-1) // the second column's single letter ends each line
	}
	if columns[0] != columns[1] || columns[1] != columns[2] {
		t.Errorf("second column misaligned: %v\n%s", columns, out.String())
	}
}

var mixedApps = map[string]any{
	"items": []map[string]any{
		{"id": 7, "name": "shop", "namespace": "acme", "environment_type": "production", "state": "deploy",
			"url": "https://shop.example", "backup_health": "healthy"},
		{"id": 9, "name": "lab", "namespace": "beta", "environment_type": "development", "state": "error",
			"url": nil, "backup_health": "critical"},
	},
	"total": 2,
}

func TestAppsAreColouredLikeTheDashboard(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/apps": mixedApps}))
	if code := h.run("apps", "--color", "always"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	out := h.stdout.String()
	for _, want := range []string{
		ansiBold + "ENV" + ansiReset,
		ansiRed + "production" + ansiReset,
		ansiCyan + "development" + ansiReset,
		ansiGreen + "deploy" + ansiReset,
		ansiRed + "error" + ansiReset,
		ansiGreen + "healthy" + ansiReset,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestShowURLAddsTheColumnBeforeTheNamespace(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/apps": mixedApps}))
	if code := h.run("apps", "--show-url"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	header := strings.Fields(strings.SplitN(h.stdout.String(), "\n", 2)[0])
	if header[len(header)-2] != "URL" || header[len(header)-1] != "NAMESPACE" {
		t.Errorf("header %v", header)
	}
	if !strings.Contains(h.stdout.String(), "https://shop.example") {
		t.Errorf("stdout %s", h.stdout)
	}
	plain := newHarness(t, fakeAPI(t, map[string]any{"/apps": mixedApps}))
	plain.run("apps")
	if strings.Contains(plain.stdout.String(), "URL") {
		t.Errorf("URL is opt-in:\n%s", plain.stdout)
	}
}

func TestDeletedAppsAreHiddenUnlessAll(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		json.NewEncoder(w).Encode(map[string]any{"data": mixedApps})
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	h.run("apps")
	h.run("apps", "--all")
	if strings.Contains(queries[0], "include_deleted") || !strings.Contains(queries[1], "include_deleted=true") {
		t.Errorf("queries %q", queries)
	}
}

func TestAllOnAnOlderPlatformFallsBackToItsFullList(t *testing.T) {
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Has("include_deleted") {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"code":"unknown_parameter","message":"Unknown parameter(s): include_deleted"}}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": mixedApps})
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	if code := h.run("apps", "--all"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if len(queries) != 2 || strings.Contains(queries[1], "include_deleted") {
		t.Errorf("queries %q", queries)
	}
}

func TestShowAllFindsADeletedAppByName(t *testing.T) {
	deleted := map[string]any{"items": []map[string]any{{"id": 5, "name": "old", "namespace": "acme", "state": "delete"}}, "total": 1}
	server := fakeAPI(t, map[string]any{
		"/apps?include_deleted=true&limit=200&offset=0": deleted,
		"/apps":   map[string]any{"items": []map[string]any{}, "total": 0},
		"/apps/5": map[string]any{"id": 5, "name": "old", "state": "delete"},
	})
	h := newHarness(t, server)
	if code := h.run("apps", "show", "old"); code != 1 {
		t.Fatalf("a deleted app is not found by name without --all: exit %d", code)
	}
	h = newHarness(t, server)
	if code := h.run("apps", "show", "old", "--all"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestAppFiltersBecomeQueryParameters(t *testing.T) {
	servers := map[string]any{"items": []map[string]any{{"id": 3, "name": "prod-1", "namespace": "acme"}}, "total": 1}
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/servers" {
			json.NewEncoder(w).Encode(map[string]any{"data": servers})
			return
		}
		got = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{"data": mixedApps})
	}))
	t.Cleanup(server.Close)
	h := newHarness(t, server)
	code := h.run("apps", "--search", "shop", "--server", "prod-1", "--version", "19.0", "--edition", "enterprise", "--project", "acme")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	for _, want := range []string{"search=shop", "server_id=3", "version=19.0", "edition=enterprise", "project=acme"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %q", want, got)
		}
	}
}
