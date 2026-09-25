package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// legacyTime is how API servers before RFC 3339 datetimes wrote them (naive UTC).
const legacyTime = "2006-01-02 15:04:05"

// now is swapped in tests.
var now = time.Now

func writeJSON(w io.Writer, raw json.RawMessage) error {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	_, err := out.WriteTo(w)
	return err
}

// writeTable aligns rows under headers; nil headers print rows only.
func writeTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if headers != nil {
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

// text renders one JSON value for a cell: null and "" read as "-".
func text(value any) string {
	switch v := value.(type) {
	case nil:
		return "-"
	case string:
		if v == "" {
			return "-"
		}
		return v
	case bool:
		if v {
			return "yes"
		}
		return "no"
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func parseServerTime(value any) (time.Time, bool) {
	s, ok := value.(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	t, err := time.ParseInLocation(legacyTime, s, time.UTC)
	return t, err == nil
}

// localTime is the absolute time in this machine's zone.
func localTime(value any) string {
	t, ok := parseServerTime(value)
	if !ok {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// ago is a short relative time for table cells.
func ago(value any) string {
	t, ok := parseServerTime(value)
	if !ok {
		return "-"
	}
	d := now().Sub(t)
	switch {
	case d < 0:
		return t.Local().Format("2006-01-02")
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Local().Format("2006-01-02")
	}
}

func megabytes(value any) string {
	n, ok := value.(json.Number)
	if !ok {
		return "-"
	}
	mb, err := n.Float64()
	if err != nil {
		return "-"
	}
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", mb/1024)
	}
	return fmt.Sprintf("%.1f MB", mb)
}
