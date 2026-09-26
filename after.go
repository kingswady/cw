package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	expiryWarning     = 7 * 24 * time.Hour
	updateCheckPeriod = 24 * time.Hour
)

// afterCommand warns about a token that is about to expire and, at most once
// a day, says when a newer cw exists. Both go to stderr and never change the
// exit code.
func (a *app) afterCommand(name string, code int) {
	a.warnExpiry()
	if (code == 0 || code == exitAttention) && name != "update" && name != "version" {
		a.hintUpdate()
	}
}

func (a *app) warnExpiry() {
	if a.current == nil {
		return
	}
	expires, ok := parseServerTime(a.current.tokenExpires)
	if !ok || expires.Sub(a.clock()) > expiryWarning {
		return
	}
	fmt.Fprintf(a.stderr, "\nThis API token expires %s — create a new one in My Settings → API Tokens.\n", untilText(a.clock(), expires))
}

// untilText is "in 3 days", "in 5 hours" or "soon".
func untilText(now, when time.Time) string {
	left := when.Sub(now)
	switch {
	case left >= 48*time.Hour:
		return fmt.Sprintf("in %d days", int(left.Hours()/24))
	case left >= 2*time.Hour:
		return fmt.Sprintf("in %d hours", int(left.Hours()))
	default:
		return "soon"
	}
}

type updateState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

func (a *app) updateStatePath() string { return filepath.Join(a.configDir, "update-check.json") }

// hintUpdate prints one line when a newer release exists: in a terminal only,
// never in CI, and without asking GitHub more than once a day.
func (a *app) hintUpdate() {
	if version == "dev" || a.getenv("CI") != "" || a.getenv("CW_NO_UPDATE_CHECK") != "" || !a.stdoutIsTerminal() {
		return
	}
	var state updateState
	if raw, err := os.ReadFile(a.updateStatePath()); err == nil {
		_ = json.Unmarshal(raw, &state)
	}
	if a.clock().Sub(state.CheckedAt) >= updateCheckPeriod {
		state.CheckedAt = a.clock()
		if latest, err := latestTagWith(quickClient, a.releases()); err == nil {
			state.Latest = latest
		}
		_ = writePrivateJSON(a.updateStatePath(), state)
	}
	current := "v" + version
	if state.Latest != "" && newer(state.Latest, current) {
		fmt.Fprintf(a.stderr, "\ncw %s is available (you have %s) — run: cw update\n", state.Latest, current)
	}
}

// quickClient bounds the daily check: a slow network must not slow a command down.
var quickClient = &http.Client{
	Timeout: 3 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func (a *app) releases() string {
	if base := a.getenv("CW_DOWNLOAD_BASE"); base != "" {
		return base
	}
	return defaultReleases
}

// use switches the saved platform to one logged in to before.
func (a *app) use(args []string) error {
	fs := a.newFlags("use")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	cfg := a.loadConfig()
	if len(positional) == 0 {
		current, _ := a.baseURL("")
		if len(cfg.Platforms) == 0 {
			fmt.Fprintf(a.stdout, "Using %s. Log in to another with: cw login --url <address>\n", current)
			return nil
		}
		for _, known := range cfg.Platforms {
			marker := "  "
			if known == current {
				marker = "* "
			}
			fmt.Fprintln(a.stdout, marker+known)
		}
		return nil
	}
	if len(positional) != 1 {
		return usagef("usage: cw use [<platform address>]")
	}
	base, err := normalizeURL(positional[0])
	if err != nil {
		return err
	}
	if _, err := a.secrets.Get(base); err != nil {
		return fmt.Errorf("not logged in to %s — run: cw login --url %s", base, base)
	}
	cfg.URL = base
	cfg.remember(base)
	if err := a.saveConfig(cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Now using %s.\n", base)
	return nil
}
