package main

import (
	"encoding/json"
	"strings"
	"testing"
)

var attention = map[string]any{
	"sections": []map[string]any{
		{"rule": "apps_error", "severity": "danger", "label": "App(s) in error state", "kind": "app", "count": 23,
			"items": []map[string]any{
				{"id": 7, "name": "shop", "namespace": "acme", "environment_type": "production", "state": "error",
					"backup_health": "healthy", "server": "prod-1"},
			}},
		{"rule": "runners_failed_24h", "severity": "warning", "label": "Runner failure(s) in last 24h", "kind": "failure",
			"count": 1, "items": []map[string]any{
				{"run_id": 4812, "workflow": "Deploy", "step": "Pull image", "record": "shop", "namespace": "acme",
					"state": "failed", "reason": "Pull image: denied\nsecond line"},
			}},
	},
	"total": 24,
}

func TestAttentionPrintsEachRuleAndExitsThree(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/attention": attention}))
	if code := h.run("attention"); code != exitAttention {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	out := h.stdout.String()
	for _, want := range []string{
		"DANGER  23 App(s) in error state", "  ID  APP", "shop", "prod-1", "… and 22 more",
		"WARNING 1 Runner failure(s) in last 24h", "4812", "Pull image: denied", "cw runs show <RUN>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "second line") {
		t.Errorf("a reason is one line in the table:\n%s", out)
	}
	if strings.Index(out, "DANGER") > strings.Index(out, "WARNING") {
		t.Errorf("the API's order is kept, most severe first:\n%s", out)
	}
}

func TestNothingNeedsAttentionExitsZero(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/attention": map[string]any{"sections": []any{}, "total": 0}}))
	if code := h.run("attention"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	if h.stdout.String() != "Nothing needs attention.\n" {
		t.Errorf("stdout: %q", h.stdout)
	}
}

func TestAttentionJSONIsTheAPIDataAndKeepsTheExitCode(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{"/attention": attention}))
	if code := h.run("attention", "--json"); code != exitAttention {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	var got struct{ Total int }
	if err := json.Unmarshal(h.stdout.Bytes(), &got); err != nil || got.Total != 24 {
		t.Errorf("not the API data: %v %s", err, h.stdout)
	}
	if h.stderr.Len() != 0 {
		t.Errorf("findings are not an error message: %s", h.stderr)
	}
}

func TestAttentionNarrowsToOneNamespace(t *testing.T) {
	h := newHarness(t, fakeAPI(t, map[string]any{
		"/attention?namespace=acme": map[string]any{"sections": []any{}, "total": 0},
	}))
	if code := h.run("attention", "--namespace", "acme"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestAnOlderPlatformSaysItHasNoAttentionYet(t *testing.T) {
	h := newHarness(t, fakeAPI(t, nil))
	if code := h.run("attention"); code != 1 || !strings.Contains(h.stderr.String(), "needs a newer platform version") {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestSSLStatusIsColoured(t *testing.T) {
	if got := style("ssl_status", "expired"); got != ansiRed+"expired"+ansiReset {
		t.Errorf("got %q", got)
	}
}
