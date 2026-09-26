package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"time"
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

func (a *app) logs(args []string) error {
	fs := a.newFlags("logs")
	since := fs.String("since", "1h", "how far back: 30s, 15m, 1h … 24h")
	grep := fs.String("grep", "", "only lines containing this text (any case)")
	limit := fs.Int("limit", 200, "lines to show (1-2000)")
	follow := fs.Bool("follow", false, "keep printing new lines until interrupted (Ctrl-C)")
	asJSON := fs.Bool("json", false, "print each API answer as JSON")
	namespace := fs.String("namespace", "", "look the app up in this namespace only")
	fs.Usage = func() {
		fmt.Fprintf(a.stderr, "Usage: cw logs <app> [flags]\n\nThe app's Odoo log, newest last.\n\nFlags:\n")
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
	c, err := a.client()
	if err != nil {
		return err
	}
	id, err := a.resolveID(c, resourceNamed("apps"), positional[0], *namespace)
	if err != nil {
		return err
	}
	query := url.Values{"since": {*since}, "limit": {strconv.Itoa(*limit)}}
	if *grep != "" {
		query.Set("grep", *grep)
	}
	page, err := a.printLogs(c, id, query, *asJSON)
	if err != nil {
		return err
	}
	if page.Truncated && !*follow {
		fmt.Fprintf(a.stderr, "\nShowing the newest %d lines — narrow with --since or --grep, or raise --limit (max 2000).\n", *limit)
	}
	if !*follow {
		return nil
	}
	return a.followLogs(c, id, *grep, page.Cursor, *asJSON)
}

// followLogs asks for lines after the cursor until interrupted. A refusal
// that will not heal (token, access, app gone) ends it; anything else is
// reported and retried.
func (a *app) followLogs(c *client, id, grep, cursor string, asJSON bool) error {
	ctx, stop := a.interrupt()
	defer stop()
	for {
		if !a.wait(ctx, followInterval) {
			return nil
		}
		for {
			query := url.Values{"after": {cursor}, "limit": {"2000"}}
			if grep != "" {
				query.Set("grep", grep)
			}
			page, err := a.printLogs(c, id, query, asJSON)
			if err != nil {
				var refused *apiError
				if errors.As(err, &refused) && refused.status >= 400 && refused.status < 500 {
					return err
				}
				fmt.Fprintln(a.stderr, "cw:", err, "— retrying")
				break
			}
			cursor = page.Cursor
			if !page.Truncated {
				break
			}
		}
	}
}

func (a *app) printLogs(c *client, id string, query url.Values, asJSON bool) (logPage, error) {
	var page logPage
	raw, err := c.get("/apps/"+id+"/logs", query)
	if err != nil {
		return page, err
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return page, err
	}
	if asJSON {
		return page, writeJSON(a.stdout, raw)
	}
	for _, item := range page.Items {
		fmt.Fprintln(a.stdout, item.Line)
	}
	return page, nil
}

// signalContext ends on Ctrl-C.
func signalContext() (context.Context, func()) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

// sleepOrDone waits d; false when ctx ended first.
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
