package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestColouredCellsStayAligned(t *testing.T) {
	var out bytes.Buffer
	rows := [][]string{{Paint(Green, "deploy"), "a"}, {"error", "b"}}
	if err := WriteTable(&out, BoldAll([]string{"STATE", "X"}), rows); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var columns []int
	for _, line := range lines {
		plain := ansiEscape.ReplaceAllString(line, "")
		columns = append(columns, len(plain)-1) // the second column's single letter ends each line
	}
	if columns[0] != columns[1] || columns[1] != columns[2] {
		t.Errorf("second column misaligned: %v\n%s", columns, out.String())
	}
}

func TestAgo(t *testing.T) {
	now = func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = time.Now })
	cases := map[any]string{
		"2026-09-24 11:59:30":  "just now",
		"2026-09-24T11:15:00Z": "45m ago",
		"2026-09-24 11:15:00":  "45m ago",
		"2026-09-23 12:00:00":  "24h ago",
		"2026-09-20 12:00:00":  "4d ago",
		nil:                    "-",
	}
	for in, want := range cases {
		if got := Ago(in); got != want {
			t.Errorf("Ago(%v) = %q, want %q", in, got, want)
		}
	}
}
