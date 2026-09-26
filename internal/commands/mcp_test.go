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
