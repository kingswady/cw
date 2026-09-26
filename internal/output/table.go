package output

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Table is how a table prints: plain, or coloured for a terminal.
type Table struct{ Color bool }

// Cell is one value, coloured by its column (the API field) when colour is on.
func (t Table) Cell(key, text string) string {
	if t.Color {
		return Style(key, text)
	}
	return text
}

func (t Table) Headers(titles []string) []string {
	if t.Color {
		return BoldAll(titles)
	}
	return titles
}

// Label is a dimmed caption ("Name:", "Steps:").
func (t Table) Label(text string) string {
	if t.Color {
		return Paint(Dim, text)
	}
	return text
}

// FirstLine is a cell's worth of a long text: its first line, cut at 80 characters.
func FirstLine(value any) string {
	s := strings.TrimSpace(strings.SplitN(Text(value), "\n", 2)[0])
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:79]) + "…"
	}
	return s
}

// Until is "in 3 days", "in 5 hours" or "soon".
func Until(now, when time.Time) string {
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
