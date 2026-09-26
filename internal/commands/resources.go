package commands

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/kingswady/cw/internal/output"
	"github.com/kingswady/cw/internal/platform"
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
	return output.Text(r[f.key])
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
	extra func(a *App, r record, out output.Table) error
	// hidesDeleted: the API leaves deleted records out unless asked (--all).
	hidesDeleted bool
	// optional columns, each shown when its own flag is given (--show-url).
	optional []optionalColumn
}

type optionalColumn struct {
	flag  string
	usage string
	field field
}

var namespaceField = field{key: "namespace", title: "NAMESPACE"}

var resources = []*resource{
	{
		name: "apps", singular: "app", byName: true, hidesDeleted: true,
		optional: []optionalColumn{{flag: "show-url", usage: "add the URL column", field: field{key: "url", title: "URL"}}},
		filters: []filter{
			{flag: "search", param: "search", usage: "only apps whose name contains this (any case)"},
			{flag: "env", param: "environment_type", usage: "production, staging or development"},
			{flag: "server", param: "server_id", usage: "only apps on this server (id or name)", lookup: "servers"},
			{flag: "version", param: "version", usage: "only this Odoo version, e.g. 19.0"},
			{flag: "edition", param: "edition", usage: "community or enterprise"},
			{flag: "project", param: "project", usage: "only apps whose project name or code contains this"},
			{flag: "state", param: "state", usage: "only apps in this state"},
		},
		columns: []field{
			{key: "id", title: "ID"}, {key: "name", title: "NAME"},
			{key: "environment_type", title: "ENV"}, {key: "version", title: "VERSION"},
			{key: "state", title: "STATE"}, {key: "server", title: "SERVER"},
			{key: "backup_health", title: "BACKUPS"}, {key: "updated_at", title: "UPDATED", format: output.Ago},
			namespaceField,
		},
		details: []field{
			{key: "id", title: "ID"}, {key: "name", title: "Name"}, {key: "namespace", title: "Namespace"},
			{key: "project", title: "Project"}, {key: "environment", title: "Environment"},
			{key: "environment_type", title: "Type"}, {key: "version", title: "Version"},
			{key: "edition", title: "Edition"}, {key: "state", title: "State"}, {key: "url", title: "URL"},
			{key: "server", title: "Server"}, {key: "deployed_at", title: "Deployed", format: output.LocalTime},
			{key: "updated_at", title: "Updated", format: output.LocalTime},
			{key: "backup_health", title: "Backup health"},
			{key: "last_backup_at", title: "Last backup", format: output.LocalTime},
		},
	},
	{
		name: "servers", singular: "server", byName: true, hidesDeleted: true,
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
		name: "installers", singular: "installer", byName: true, hidesDeleted: true,
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
			{key: "id", title: "ID"}, {key: "app", title: "APP"}, {key: "taken_at", title: "TAKEN", format: output.Ago},
			{key: "size_mb", title: "SIZE", format: output.Megabytes}, {key: "format", title: "FORMAT"},
			{key: "automated", title: "AUTO"}, {key: "state", title: "STATE"}, {key: "storage", title: "STORAGE"},
			namespaceField,
		},
		details: []field{
			{key: "id", title: "ID"}, {key: "name", title: "Name"}, {key: "namespace", title: "Namespace"},
			{key: "app", title: "App"}, {key: "environment_type", title: "Environment"},
			{key: "taken_at", title: "Taken", format: output.LocalTime}, {key: "size_mb", title: "Size", format: output.Megabytes},
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
			{key: "updated_at", title: "UPDATED", format: output.Ago}, namespaceField,
		},
		details: []field{
			{key: "id", title: "ID"}, {key: "workflow", title: "Workflow"}, {key: "action", title: "Action"},
			{key: "state", title: "State"}, {key: "namespace", title: "Namespace"}, {key: "record", title: "For"},
			{key: "record_type", title: "Type"}, {key: "created_at", title: "Started", format: output.LocalTime},
			{key: "updated_at", title: "Updated", format: output.LocalTime},
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
	color     string
	all       bool
	filters   map[string]*string
	optional  map[string]*bool
}

func (a *App) readFlagSet(res *resource) (*flag.FlagSet, *readFlags) {
	fs := a.newFlags(res.name)
	opts := &readFlags{filters: map[string]*string{}, optional: map[string]*bool{}}
	fs.BoolVar(&opts.json, "json", false, "print the API response as JSON")
	fs.StringVar(&opts.color, "color", "auto", "auto (in a terminal, unless NO_COLOR is set), always or never")
	if res.hidesDeleted {
		fs.BoolVar(&opts.all, "all", false, "include deleted "+res.name)
	}
	for _, column := range res.optional {
		opts.optional[column.flag] = fs.Bool(column.flag, false, column.usage)
	}
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

func (a *App) resource(res *resource, args []string) error {
	fs, opts := a.readFlagSet(res)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	color, err := a.useColor(opts.color, opts.json)
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	out := output.Table{Color: color}
	switch {
	case len(positional) == 0:
		return a.list(c, res, opts, out)
	case positional[0] == "show" && len(positional) == 2:
		return a.show(c, res, opts, positional[1], out)
	default:
		fs.Usage()
		return usagef("unexpected arguments: %s", strings.Join(positional, " "))
	}
}

// columnsFor is the resource's columns plus the optional ones asked for,
// placed before the trailing namespace column.
func columnsFor(res *resource, opts *readFlags) []field {
	columns := append([]field{}, res.columns...)
	for _, column := range res.optional {
		if !*opts.optional[column.flag] {
			continue
		}
		at := len(columns)
		if at > 0 && columns[at-1].key == namespaceField.key {
			at--
		}
		columns = append(columns[:at], append([]field{column.field}, columns[at:]...)...)
	}
	return columns
}

// getList asks for one list page. A platform older than include_deleted
// answers unknown_parameter and already lists deleted records, so the
// parameter is dropped there.
func getList(c *platform.Client, path string, query url.Values) (json.RawMessage, error) {
	raw, err := c.Get(path, query)
	var refused *platform.APIError
	if errors.As(err, &refused) && refused.Code == "unknown_parameter" && query.Has("include_deleted") {
		query.Del("include_deleted")
		return c.Get(path, query)
	}
	return raw, err
}

func (a *App) list(c *platform.Client, res *resource, opts *readFlags, out output.Table) error {
	query := url.Values{"limit": {strconv.Itoa(opts.limit)}, "offset": {strconv.Itoa(opts.offset)}}
	if opts.all {
		query.Set("include_deleted", "true")
	}
	if opts.namespace != "" {
		query.Set("namespace", opts.namespace)
	}
	for _, f := range res.filters {
		value := *opts.filters[f.flag]
		if value == "" {
			continue
		}
		if f.lookup != "" {
			id, err := a.resolveID(c, resourceNamed(f.lookup), value, opts.namespace, false)
			if err != nil {
				return err
			}
			value = id
		}
		query.Set(f.param, value)
	}
	raw, err := getList(c, "/"+res.name, query)
	if err != nil {
		return err
	}
	if opts.json {
		return output.WriteJSON(a.stdout, raw)
	}
	var page struct {
		Items []record    `json:"items"`
		Total json.Number `json:"total"`
	}
	if err := platform.Decode(raw, &page); err != nil {
		return err
	}
	if len(page.Items) == 0 {
		fmt.Fprintf(a.stderr, "No %s.\n", res.name)
		return nil
	}
	columns := visibleColumns(columnsFor(res, opts), page.Items)
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = col.title
	}
	rows := make([][]string, len(page.Items))
	for i, item := range page.Items {
		rows[i] = make([]string, len(columns))
		for j, col := range columns {
			rows[i][j] = out.Cell(col.key, col.render(item))
		}
	}
	if err := output.WriteTable(a.stdout, out.Headers(headers), rows); err != nil {
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
		seen[output.Text(item[namespaceField.key])] = true
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

func (a *App) show(c *platform.Client, res *resource, opts *readFlags, ref string, out output.Table) error {
	id, err := a.resolveID(c, res, ref, opts.namespace, opts.all)
	if err != nil {
		return err
	}
	raw, err := c.Get("/"+res.name+"/"+id, nil)
	if err != nil {
		return err
	}
	if opts.json {
		return output.WriteJSON(a.stdout, raw)
	}
	var item record
	if err := platform.Decode(raw, &item); err != nil {
		return err
	}
	rows := make([][]string, len(res.details))
	for i, f := range res.details {
		rows[i] = []string{out.Label(f.title + ":"), out.Cell(f.key, f.render(item))}
	}
	if err := output.WriteTable(a.stdout, nil, rows); err != nil {
		return err
	}
	if res.extra != nil {
		return res.extra(a, item, out)
	}
	return nil
}

// resolveID accepts an id, or the exact name of one record the token can see.
// Deleted records are left out unless includeDeleted (--all); their ids still work.
func (a *App) resolveID(c *platform.Client, res *resource, ref, namespace string, includeDeleted bool) (string, error) {
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
		if includeDeleted && res.hidesDeleted {
			query.Set("include_deleted", "true")
		}
		raw, err := getList(c, "/"+res.name, query)
		if err != nil {
			return "", err
		}
		var page struct {
			Items []record    `json:"items"`
			Total json.Number `json:"total"`
		}
		if err := platform.Decode(raw, &page); err != nil {
			return "", err
		}
		for _, item := range page.Items {
			if strings.EqualFold(output.Text(item["name"]), ref) {
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
		return output.Text(matches[0]["id"]), nil
	}
	candidates := make([]string, len(matches))
	for i, m := range matches {
		candidates[i] = fmt.Sprintf("%s (%s)", output.Text(m["id"]), output.Text(m["namespace"]))
	}
	return "", fmt.Errorf("%d %ss are named %q — use an id: %s", len(matches), res.singular, ref, strings.Join(candidates, ", "))
}

func printSteps(a *App, run record, out output.Table) error {
	steps, _ := run["steps"].([]any)
	if len(steps) == 0 {
		return nil
	}
	fmt.Fprintln(a.stdout, "\n"+out.Label("Steps:"))
	rows := make([][]string, 0, len(steps))
	var reasons []string
	for i, raw := range steps {
		step, _ := raw.(record)
		rows = append(rows, []string{
			strconv.Itoa(i + 1), output.Text(step["name"]), out.Cell("state", output.Text(step["state"])),
			output.LocalTime(step["started_at"]), output.LocalTime(step["updated_at"]),
		})
		if reason := output.Text(step["reason"]); reason != "-" {
			// A reason can span lines (a task message); keep them under their step.
			reason = strings.ReplaceAll(strings.TrimSpace(reason), "\n", "\n     ")
			if out.Color {
				reason = output.Paint(output.Red, reason)
			}
			reasons = append(reasons, fmt.Sprintf("  %d. %s\n     %s", i+1, output.Text(step["name"]), reason))
		}
	}
	if err := output.WriteTable(a.stdout, out.Headers([]string{"#", "STEP", "STATE", "STARTED", "UPDATED"}), rows); err != nil {
		return err
	}
	if len(reasons) > 0 {
		fmt.Fprintln(a.stdout, "\n"+out.Label("Why:"))
		fmt.Fprintln(a.stdout, strings.Join(reasons, "\n"))
	}
	return nil
}
