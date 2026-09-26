package main

import (
	"strings"
	"testing"
)

const infoLine = "2026-09-26 13:16:25,466 56 INFO v19-0 odoo.addons.base.models.ir_cron: Job done"

func TestAnInfoLineGetsOdoosColours(t *testing.T) {
	got := colorize(infoLine, "")
	for _, want := range []string{
		ansiDim + "2026-09-26 13:16:25,466 56" + ansiReset,
		"\x1b[1;32mINFO" + ansiReset,
		ansiCyan + "v19-0" + ansiReset,
		ansiDim + "odoo.addons.base.models.ir_cron:" + ansiReset,
		" Job done",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, ansiRed) {
		t.Errorf("a routine line must not look like an error: %q", got)
	}
}

func TestAnErrorAndItsTracebackAreRed(t *testing.T) {
	entry := "2026-09-26 13:16:25,466 56 ERROR v19-0 odoo.http: Exception during request\nTraceback (most recent call last):\n  File \"x.py\", line 1"
	lines := strings.Split(colorize(entry, ""), "\n")
	if !strings.Contains(lines[0], "\x1b[1;31mERROR") || !strings.Contains(lines[0], ansiRed+"Exception during request") {
		t.Errorf("first line: %q", lines[0])
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, ansiRed) {
			t.Errorf("traceback line not red: %q", line)
		}
	}
}

func TestHTTPStatusIsColouredByClass(t *testing.T) {
	for status, color := range map[string]string{"200": ansiGreen, "302": ansiCyan, "404": ansiYellow, "500": ansiRed} {
		line := `2026-09-26 13:16:25,466 56 INFO v19-0 werkzeug: 1.2.3.4 - - [26/Sep/2026] "GET / HTTP/1.1" ` + status + " - 3"
		if got := colorize(line, ""); !strings.Contains(got, color+status+ansiReset) {
			t.Errorf("%s: %q", status, got)
		}
	}
}

func TestGrepMatchesAreHighlightedInTheirOwnCase(t *testing.T) {
	got := colorize(infoLine, "job")
	if !strings.Contains(got, ansiReverse+"Job"+ansiReverseOf) {
		t.Errorf("%q", got)
	}
}

func TestLinesThatAreNotOdoosAreLeftAlone(t *testing.T) {
	for _, line := range []string{"plain text", "Traceback (most recent call last):"} {
		if got := colorize(line, ""); got != line {
			t.Errorf("%q became %q", line, got)
		}
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
