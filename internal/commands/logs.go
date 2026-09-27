package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kingswady/cw/internal/output"
	"github.com/kingswady/cw/internal/platform"
)

// followInterval is how often --follow asks for newer lines.
const followInterval = 2 * time.Second

type logPage struct {
	Items []struct {
		Time string `json:"time"`
		Line string `json:"line"`
	} `json:"items"`
	Cursor    string `json:"cursor"`
	Truncated bool   `json:"truncated"`
}

func (a *App) logs(args []string) error {
	fs := a.newFlags("logs")
	since := fs.String("since", "1h", "how far back: 30s, 15m, 1h … 24h")
	grep := fs.String("grep", "", "only lines containing this text (any case)")
	limit := fs.Int("limit", 200, "lines to show (1-2000)")
	follow := fs.Bool("follow", false, "keep printing new lines until interrupted (Ctrl-C)")
	asJSON := fs.Bool("json", false, "print each API answer as JSON")
	namespace := fs.String("namespace", "", "look the app up in this namespace only")
	colorMode := fs.String("color", "auto", "auto (in a terminal, unless NO_COLOR is set), always or never")
	apps := resourceNamed("apps")
	narrow := narrowFlags(fs, apps, "<app>")
	fs.Usage = func() {
		fmt.Fprintf(a.stderr, "Usage: cw logs <app> [flags]\n\nThe app's Odoo log, newest last.\n\n"+
			"<app> is an id or a name. Where several apps share the name,\n"+
			"%s narrow which one it means:\n  cw logs v19-0 --project internal --env production\n\nFlags:\n",
			joinFlags(narrowing(apps), "and"))
		fs.PrintDefaults()
	}
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return usagef("name one app: cw logs <id|name>")
	}
	color, err := a.useColor(*colorMode, *asJSON)
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	id, err := a.resolveID(c, apps, positional[0], scope{
		namespace: *namespace, filters: given(narrow), offered: narrowing(apps),
	}, output.Table{Color: color})
	if err != nil {
		return err
	}
	query := url.Values{"since": {*since}, "limit": {strconv.Itoa(*limit)}}
	if *grep != "" {
		query.Set("grep", *grep)
	}
	out := logOutput{json: *asJSON, color: color, grep: *grep}
	page, err := a.printLogs(context.Background(), c, id, query, out)
	if err != nil {
		return err
	}
	if page.Truncated && !*follow {
		fmt.Fprintf(a.stderr, "\nShowing the newest %d lines — narrow with --since or --grep, or raise --limit (max 2000).\n", *limit)
	}
	if !*follow {
		return nil
	}
	return a.followLogs(c, id, page.Cursor, out)
}

// followLogs asks for lines after the cursor until interrupted — also in
// the middle of a request, or of catching up. A refusal that will not heal
// (token, access, app gone) ends it; the rate limit is waited out; anything
// else is reported and retried.
func (a *App) followLogs(c *platform.Client, id, cursor string, out logOutput) error {
	ctx, stop := a.interrupt()
	defer stop()
	pause := followInterval
	for {
		if !a.wait(ctx, pause) {
			return nil
		}
		pause = followInterval
		var err error
		cursor, err = a.catchUp(ctx, c, id, cursor, out)
		var refused *platform.APIError
		switch {
		case ctx.Err() != nil:
			return nil // Ctrl-C
		case err == nil:
		case errors.As(err, &refused) && refused.Status == http.StatusTooManyRequests:
			// The platform's rate limit: wait as long as it says, then follow on.
			pause = max(refused.RetryAfter, followInterval)
			fmt.Fprintln(a.stderr, "cw:", output.Clean(err.Error()), "— waiting, then following on")
		case errors.As(err, &refused) && refused.Status >= 400 && refused.Status < 500:
			return err
		default:
			fmt.Fprintln(a.stderr, "cw:", output.Clean(err.Error()), "— retrying")
		}
	}
}

// catchUp prints every line after cursor, page by page, until it is caught
// up or ctx ends, and returns the cursor it reached.
func (a *App) catchUp(ctx context.Context, c *platform.Client, id, cursor string, out logOutput) (string, error) {
	for ctx.Err() == nil {
		query := url.Values{"after": {cursor}, "limit": {"2000"}}
		if out.grep != "" {
			query.Set("grep", out.grep)
		}
		page, err := a.printLogs(ctx, c, id, query, out)
		if err != nil {
			return cursor, err
		}
		cursor = page.Cursor
		if !page.Truncated {
			return cursor, nil
		}
	}
	return cursor, ctx.Err()
}

// logOutput is how answers are printed: raw JSON, or lines — coloured in a terminal.
type logOutput struct {
	json  bool
	color bool
	grep  string
}

func (a *App) printLogs(ctx context.Context, c *platform.Client, id string, query url.Values, out logOutput) (logPage, error) {
	var page logPage
	raw, err := c.GetContext(ctx, "/apps/"+id+"/logs", query)
	if err != nil {
		return page, err
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return page, err
	}
	if out.json {
		return page, output.WriteJSON(a.stdout, raw)
	}
	for _, item := range page.Items {
		fmt.Fprintln(a.stdout, output.LogLine(strings.TrimRight(item.Line, "\n "), out.color, out.grep))
	}
	return page, nil
}
