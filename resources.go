package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type record = map[string]any

// field is one value of a record: its JSON key, its label and how it prints.
type field struct {
	key    string
	title  string
	format func(any) string
}

func (f field) render(r record) string {
	if f.format != nil {
		return f.format(r[f.key])
	}
	return text(r[f.key])
}

// filter is a flag that narrows a list; lookup names the resource whose name
// the flag may be given instead of an id ("--app shop").
type filter struct {
	flag   string
	param  string
	usage  string
	lookup string
}

type resource struct {
	name     string // command and path: "apps" → /apps
	singular string
	// byName: "show" and filters accept this resource's name, not only its id.
	byName  bool
	filters []filter
	columns []field
	details []field
	// extra prints what a detail view has beyond its fields (a run's steps).
	extra func(a *app, r record) error
}

var namespaceField = field{key: "namespace", title: "NAMESPACE"}

var resources = []*resource{
	{
		name: "apps", singular: "app", byName: true,
		filters: []filter{
			{flag: "env", param: "environment_type", usage: "production, staging or development"},
			{flag: "state", param: "state", usage: "only apps in this state"},
		},
		columns: []field{
			{key: "id", title: "ID"}, {key: "name", title: "NAME"},
			{key: "environment_type", title: "ENV"}, {key: "version", title: "VERSION"},
			{key: "state", title: "STATE"}, {key: "server", title: "SERVER"},
			{key: "backup_health", title: "BACKUPS"}, {key: "updated_at", title: "UPDATED", format: ago},
			namespaceField,
		},
		details: []field{
			{key: "id", title: "ID"}, {key: "name", title: "Name"}, {key: "namespace", title: "Namespace"},
			{key: "project", title: "Project"}, {key: "environment", title: "Environment"},
			{key: "environment_type", title: "Type"}, {key: "version", title: "Version"},
			{key: "edition", title: "Edition"}, {key: "state", title: "State"}, {key: "url", title: "URL"},
			{key: "server", title: "Server"}, {key: "deployed_at", title: "Deployed", format: localTime},
			{key: "updated_at", title: "Updated", format: localTime},
			{key: "backup_health", title: "Backup health"},
			{key: "last_backup_at", title: "Last backup", format: localTime},
		},
	},
	{
		name: "servers", singular: "server", byName: true,
		filters: []filter{{flag: "state", param: "state", usage: "only servers in this state"}},
		columns: []field{
			{key: "id", title: "ID"}, {key: "name", title: "NAME"}, {key: "state", title: "STATE"},
			{key: "provider", title: "PROVIDER"}, {key: "region", title: "REGION"}, {key: "size", title: "SIZE"},
			{key: "public_ip", title: "IP"}, {key: "app_count", title: "APPS"}, namespaceField,
		},
		details: []field{
			{key: "id", title: "ID"}, {key: "name", title: "Name"}, {key: "namespace", title: "Namespace"},
			{key: "state", title: "State"}, {key: "provider", title: "Provider"}, {key: "size", title: "Size"},
			{key: "region", title: "Region"}, {key: "image", title: "Image"},
			{key: "lb_engine", title: "Load balancer"}, {key: "public_ip", title: "Public IP"},
			{key: "app_count", title: "Apps"},
		},
	},
	{
		name: "installers", singular: "installer", byName: true,
		filters: []filter{
			{flag: "server", param: "server_id", usage: "only on this server (id or name)", lookup: "servers"},
			{flag: "state", param: "state", usage: "only installers in this state"},
		},
		columns: []field{
			{key: "id", title: "ID"}, {key: "name", title: "NAME"}, {key: "type", title: "TYPE"},
			{key: "state", title: "STATE"}, {key: "server", title: "SERVER"}, {key: "url", title: "URL"},
			namespaceField,
		},
		details: []field{
			{key: "id", title: "ID"}, {key: "name", title: "Name"}, {key: "namespace", title: "Namespace"},
			{key: "type", title: "Type"}, {key: "app", title: "Marketplace app"}, {key: "state", title: "State"},
			{key: "server", title: "Server"}, {key: "url", title: "URL"},
			{key: "platform_login", title: "Platform login"},
		},
	},
	{
		name: "backups", singular: "backup",
		filters: []filter{{flag: "app", param: "app_id", usage: "only this app's backups (id or name)", lookup: "apps"}},
		columns: []field{
			{key: "id", title: "ID"}, {key: "app", title: "APP"}, {key: "taken_at", title: "TAKEN", format: ago},
			{key: "size_mb", title: "SIZE", format: megabytes}, {key: "format", title: "FORMAT"},
			{key: "automated", title: "AUTO"}, {key: "state", title: "STATE"}, {key: "storage", title: "STORAGE"},
			namespaceField,
		},
		details: []field{
			{key: "id", title: "ID"}, {key: "name", title: "Name"}, {key: "namespace", title: "Namespace"},
			{key: "app", title: "App"}, {key: "environment_type", title: "Environment"},
			{key: "taken_at", title: "Taken", format: localTime}, {key: "size_mb", title: "Size", format: megabytes},
			{key: "format", title: "Format"}, {key: "automated", title: "Automated"}, {key: "state", title: "State"},
			{key: "storage", title: "Storage"},
		},
	},
	{
		name: "runs", singular: "run",
		filters: []filter{
			{flag: "app", param: "app_id", usage: "only this app's runs (id or name)", lookup: "apps"},
			{flag: "state", param: "state", usage: "only runs in this state (e.g. error, running, done)"},
		},
		columns: []field{
			{key: "id", title: "ID"}, {key: "workflow", title: "WORKFLOW"}, {key: "action", title: "ACTION"},
			{key: "state", title: "STATE"}, {key: "record", title: "FOR"},
			{key: "updated_at", title: "UPDATED", format: ago}, namespaceField,
		},
		details: []field{
			{key: "id", title: "ID"}, {key: "workflow", title: "Workflow"}, {key: "action", title: "Action"},
			{key: "state", title: "State"}, {key: "namespace", title: "Namespace"}, {key: "record", title: "For"},
			{key: "record_type", title: "Type"}, {key: "created_at", title: "Started", format: localTime},
			{key: "updated_at", title: "Updated", format: localTime},
		},
		extra: printSteps,
	},
}

func resourceNamed(name string) *resource {
	for _, r := range resources {
		if r.name == name {
			return r
		}
	}
	return nil
}

// readFlags are the flags every read command takes.
type readFlags struct {
	json      bool
	namespace string
	limit     int
	offset    int
	filters   map[string]*string
}

func (a *app) readFlagSet(res *resource) (*flag.FlagSet, *readFlags) {
	fs := a.newFlags(res.name)
	opts := &readFlags{filters: map[string]*string{}}
	fs.BoolVar(&opts.json, "json", false, "print the API response as JSON")
	fs.StringVar(&opts.namespace, "namespace", "", "only this namespace (code or id)")
	fs.IntVar(&opts.limit, "limit", 50, "rows per page (1-200)")
	fs.IntVar(&opts.offset, "offset", 0, "rows to skip")
	for _, f := range res.filters {
		opts.filters[f.flag] = fs.String(f.flag, "", f.usage)
	}
	fs.Usage = func() {
		show := "<id>"
		if res.byName {
			show = "<id|name>"
		}
		fmt.Fprintf(a.stderr, "Usage: cw %s [flags]\n       cw %s show %s [--json]\n\nFlags:\n", res.name, res.name, show)
		fs.PrintDefaults()
	}
	return fs, opts
}

func (a *app) resource(res *resource, args []string) error {
	fs, opts := a.readFlagSet(res)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	switch {
	case len(positional) == 0:
		return a.list(c, res, opts)
	case positional[0] == "show" && len(positional) == 2:
		return a.show(c, res, opts, positional[1])
	default:
		fs.Usage()
		return usagef("unexpected arguments: %s", strings.Join(positional, " "))
	}
}

func (a *app) list(c *client, res *resource, opts *readFlags) error {
	query := url.Values{"limit": {strconv.Itoa(opts.limit)}, "offset": {strconv.Itoa(opts.offset)}}
	if opts.namespace != "" {
		query.Set("namespace", opts.namespace)
	}
	for _, f := range res.filters {
		value := *opts.filters[f.flag]
		if value == "" {
			continue
		}
		if f.lookup != "" {
			id, err := a.resolveID(c, resourceNamed(f.lookup), value, opts.namespace)
			if err != nil {
				return err
			}
			value = id
		}
		query.Set(f.param, value)
	}
	raw, err := c.get("/"+res.name, query)
	if err != nil {
		return err
	}
	if opts.json {
		return writeJSON(a.stdout, raw)
	}
	var page struct {
		Items []record    `json:"items"`
		Total json.Number `json:"total"`
	}
	if err := decode(raw, &page); err != nil {
		return err
	}
	if len(page.Items) == 0 {
		fmt.Fprintf(a.stderr, "No %s.\n", res.name)
		return nil
	}
	columns := visibleColumns(res.columns, page.Items)
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = col.title
	}
	rows := make([][]string, len(page.Items))
	for i, item := range page.Items {
		rows[i] = make([]string, len(columns))
		for j, col := range columns {
			rows[i][j] = col.render(item)
		}
	}
	if err := writeTable(a.stdout, headers, rows); err != nil {
		return err
	}
	if total, _ := page.Total.Int64(); int(total) > opts.offset+len(page.Items) {
		fmt.Fprintf(a.stderr, "\nShowing %d-%d of %d — next page: --offset %d\n",
			opts.offset+1, opts.offset+len(page.Items), total, opts.offset+len(page.Items))
	}
	return nil
}

// visibleColumns drops the namespace column when every row shares one.
func visibleColumns(columns []field, items []record) []field {
	seen := map[string]bool{}
	for _, item := range items {
		seen[text(item[namespaceField.key])] = true
	}
	if len(seen) > 1 {
		return columns
	}
	visible := make([]field, 0, len(columns))
	for _, col := range columns {
		if col.key != namespaceField.key {
			visible = append(visible, col)
		}
	}
	return visible
}

func (a *app) show(c *client, res *resource, opts *readFlags, ref string) error {
	id, err := a.resolveID(c, res, ref, opts.namespace)
	if err != nil {
		return err
	}
	raw, err := c.get("/"+res.name+"/"+id, nil)
	if err != nil {
		return err
	}
	if opts.json {
		return writeJSON(a.stdout, raw)
	}
	var item record
	if err := decode(raw, &item); err != nil {
		return err
	}
	rows := make([][]string, len(res.details))
	for i, f := range res.details {
		rows[i] = []string{f.title + ":", f.render(item)}
	}
	if err := writeTable(a.stdout, nil, rows); err != nil {
		return err
	}
	if res.extra != nil {
		return res.extra(a, item)
	}
	return nil
}

// resolveID accepts an id, or the exact name of one record the token can see.
func (a *app) resolveID(c *client, res *resource, ref, namespace string) (string, error) {
	if _, err := strconv.Atoi(ref); err == nil {
		return ref, nil
	}
	if !res.byName {
		return "", usagef("%s are looked up by id, not by name: %q", res.name, ref)
	}
	var matches []record
	for offset := 0; ; offset += 200 {
		query := url.Values{"limit": {"200"}, "offset": {strconv.Itoa(offset)}}
		if namespace != "" {
			query.Set("namespace", namespace)
		}
		raw, err := c.get("/"+res.name, query)
		if err != nil {
			return "", err
		}
		var page struct {
			Items []record    `json:"items"`
			Total json.Number `json:"total"`
		}
		if err := decode(raw, &page); err != nil {
			return "", err
		}
		for _, item := range page.Items {
			if strings.EqualFold(text(item["name"]), ref) {
				matches = append(matches, item)
			}
		}
		if total, _ := page.Total.Int64(); int64(offset+200) >= total {
			break
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no %s named %q in this token's namespaces", res.singular, ref)
	case 1:
		return text(matches[0]["id"]), nil
	}
	candidates := make([]string, len(matches))
	for i, m := range matches {
		candidates[i] = fmt.Sprintf("%s (%s)", text(m["id"]), text(m["namespace"]))
	}
	return "", fmt.Errorf("%d %ss are named %q — use an id: %s", len(matches), res.singular, ref, strings.Join(candidates, ", "))
}

func printSteps(a *app, run record) error {
	steps, _ := run["steps"].([]any)
	if len(steps) == 0 {
		return nil
	}
	fmt.Fprintln(a.stdout, "\nSteps:")
	rows := make([][]string, 0, len(steps))
	var reasons []string
	for i, raw := range steps {
		step, _ := raw.(record)
		rows = append(rows, []string{
			strconv.Itoa(i + 1), text(step["name"]), text(step["state"]),
			localTime(step["started_at"]), localTime(step["updated_at"]),
		})
		if reason := text(step["reason"]); reason != "-" {
			// A reason can span lines (a task message); keep them under their step.
			reason = strings.ReplaceAll(strings.TrimSpace(reason), "\n", "\n     ")
			reasons = append(reasons, fmt.Sprintf("  %d. %s\n     %s", i+1, text(step["name"]), reason))
		}
	}
	if err := writeTable(a.stdout, []string{"#", "STEP", "STATE", "STARTED", "UPDATED"}, rows); err != nil {
		return err
	}
	if len(reasons) > 0 {
		fmt.Fprintln(a.stdout, "\nWhy:")
		fmt.Fprintln(a.stdout, strings.Join(reasons, "\n"))
	}
	return nil
}
