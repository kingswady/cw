package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// exitAttention is cw attention's exit code when something needs attention,
// so a script can act on it: `cw attention || notify-team`.
const exitAttention = 3

// errNeedsAttention says the findings are printed; a.exit turns it into exitAttention.
var errNeedsAttention = errors.New("something needs attention")

type attentionSection struct {
	Rule     string   `json:"rule"`
	Severity string   `json:"severity"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"`
	Count    int      `json:"count"`
	Items    []record `json:"items"`
}

// attentionColumns are the columns of each kind of section.
var attentionColumns = map[string][]field{
	"app": {
		{key: "id", title: "ID"}, {key: "name", title: "APP"}, {key: "environment_type", title: "ENV"},
		{key: "state", title: "STATE"}, {key: "backup_health", title: "BACKUPS"},
		{key: "last_backup_at", title: "LAST BACKUP", format: ago}, {key: "server", title: "SERVER"},
		namespaceField,
	},
	"url": {
		{key: "id", title: "ID"}, {key: "url", title: "URL"}, {key: "ssl_status", title: "SSL"},
		{key: "ssl_expires_at", title: "EXPIRES", format: ago}, {key: "owner", title: "FOR"}, namespaceField,
	},
	"failure": {
		{key: "run_id", title: "RUN"}, {key: "workflow", title: "WORKFLOW"}, {key: "step", title: "STEP"},
		{key: "record", title: "FOR"}, {key: "failed_at", title: "FAILED", format: ago},
		{key: "reason", title: "WHY", format: firstLine}, namespaceField,
	},
}

var severityColor = map[string]string{"danger": ansiRed, "warning": ansiYellow}

func (a *app) attention(args []string) error {
	fs := a.newFlags("attention")
	asJSON := fs.Bool("json", false, "print the API response as JSON")
	namespace := fs.String("namespace", "", "only this namespace (code or id)")
	colorFlag := fs.String("color", "auto", "auto (in a terminal, unless NO_COLOR is set), always or never")
	fs.Usage = func() {
		fmt.Fprint(a.stderr, "Usage: cw attention [flags]\n\n"+
			"What needs attention now: the dashboard's Needs attention rules, with their records.\n"+
			"Exit code 3 when something does, 0 when nothing does.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if positional, err := parseArgs(fs, args); err != nil {
		return err
	} else if len(positional) > 0 {
		fs.Usage()
		return usagef("unexpected arguments: %s", strings.Join(positional, " "))
	}
	color, err := a.useColor(*colorFlag, *asJSON)
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	query := url.Values{}
	if *namespace != "" {
		query.Set("namespace", *namespace)
	}
	raw, err := c.get("/attention", query)
	var refused *apiError
	if errors.As(err, &refused) && refused.code == "not_found" {
		return fmt.Errorf("%s does not answer cw attention yet — it needs a newer platform version", c.base)
	}
	if err != nil {
		return err
	}
	var result struct {
		Sections []attentionSection `json:"sections"`
	}
	if err := decode(raw, &result); err != nil {
		return err
	}
	if *asJSON {
		err = writeJSON(a.stdout, raw)
	} else {
		err = a.printAttention(result.Sections, tableOutput{color: color})
	}
	if err == nil && len(result.Sections) > 0 {
		return errNeedsAttention
	}
	return err
}

func (a *app) printAttention(sections []attentionSection, out tableOutput) error {
	if len(sections) == 0 {
		msg := "Nothing needs attention."
		if out.color {
			msg = paint(ansiGreen, msg)
		}
		fmt.Fprintln(a.stdout, msg)
		return nil
	}
	failures := false
	for i, section := range sections {
		if i > 0 {
			fmt.Fprintln(a.stdout)
		}
		title := fmt.Sprintf("%-8s%d %s", strings.ToUpper(section.Severity), section.Count, section.Label)
		if out.color {
			title = paint(ansiBold+severityColor[section.Severity], title)
		}
		fmt.Fprintln(a.stdout, title)
		if err := a.printSection(section, out); err != nil {
			return err
		}
		failures = failures || section.Kind == "failure"
	}
	if failures {
		fmt.Fprintln(a.stdout, "\n"+out.label("Why a run failed, step by step: cw runs show <RUN>"))
	}
	return nil
}

func (a *app) printSection(section attentionSection, out tableOutput) error {
	columns := visibleColumns(attentionColumns[section.Kind], section.Items)
	if len(columns) == 0 || len(section.Items) == 0 {
		return nil
	}
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = col.title
	}
	rows := make([][]string, len(section.Items))
	for i, item := range section.Items {
		rows[i] = make([]string, len(columns))
		for j, col := range columns {
			rows[i][j] = out.cell(col.key, col.render(item))
		}
	}
	var table bytes.Buffer
	if err := writeTable(&table, out.headers(headers), rows); err != nil {
		return err
	}
	for _, line := range strings.SplitAfter(strings.TrimSuffix(table.String(), "\n"), "\n") {
		fmt.Fprint(a.stdout, "  "+line)
	}
	fmt.Fprintln(a.stdout)
	if more := section.Count - len(section.Items); more > 0 {
		fmt.Fprintln(a.stdout, "  "+out.label(fmt.Sprintf("… and %d more", more)))
	}
	return nil
}

// firstLine is a cell's worth of a long text: its first line, cut at 80 characters.
func firstLine(value any) string {
	s := strings.TrimSpace(strings.SplitN(text(value), "\n", 2)[0])
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:79]) + "…"
	}
	return s
}
