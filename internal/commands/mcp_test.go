package commands

import (
	"strings"
	"testing"
)

func TestMCPNeedsALogin(t *testing.T) {
	h := newHarness(t, nil)
	h.env["CW_URL"] = "https://platform.example"
	if code := h.run("mcp"); code != 1 || !strings.Contains(h.stderr.String(), "not logged in") {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
}

func TestMCPRunByAPersonSaysWhatItIs(t *testing.T) {
	h := newHarness(t, fakeAPI(t, nil))
	h.app.stdinIsTerminal = func() bool { return true }
	if code := h.run("mcp"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.stderr)
	}
	for _, want := range []string{"waiting for one on stdin", "claude mcp add -s user cloudwady -- cw mcp", "Ctrl-D"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Errorf("missing %q in %q", want, h.stderr)
		}
	}
	if h.stdout.Len() != 0 {
		t.Errorf("the hint must never reach stdout, where the protocol runs: %q", h.stdout)
	}
	client := newHarness(t, fakeAPI(t, nil))
	client.run("mcp")
	if client.stderr.Len() != 0 {
		t.Errorf("an AI client gets no hint: %q", client.stderr)
	}
}
