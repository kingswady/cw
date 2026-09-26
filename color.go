package main

import (
	"regexp"
	"strings"
)

const (
	ansiReset     = "\x1b[0m"
	ansiDim       = "\x1b[2m"
	ansiRed       = "\x1b[31m"
	ansiGreen     = "\x1b[32m"
	ansiYellow    = "\x1b[33m"
	ansiCyan      = "\x1b[36m"
	ansiReverse   = "\x1b[7m"
	ansiReverseOf = "\x1b[27m" // ends the highlight without ending the colour around it
	ansiBold      = "\x1b[1m"
)

// odooLine is Odoo's log format: time, pid, level, database, logger: message.
var odooLine = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3}) (\d+) (DEBUG|INFO|WARNING|ERROR|CRITICAL) (\S+) ([\w.\-]+): (.*)$`)

// httpStatus is the status in a werkzeug request line: `"GET / HTTP/1.1" 200 -`.
var httpStatus = regexp.MustCompile(`(" )([1-5]\d\d)( )`)

// Levels coloured the way Odoo's own terminal handler does.
var levelColor = map[string]string{
	"DEBUG":    "\x1b[1;34m",
	"INFO":     "\x1b[1;32m",
	"WARNING":  "\x1b[1;33m",
	"ERROR":    "\x1b[1;31m",
	"CRITICAL": "\x1b[1;37;41m",
}

// messageColor makes the message of a problem stand out; routine lines stay plain.
var messageColor = map[string]string{"WARNING": ansiYellow, "ERROR": ansiRed, "CRITICAL": ansiRed}

// colorize renders one log entry (it may span lines: a traceback) for a terminal.
func colorize(entry, grep string) string {
	lines := strings.Split(entry, "\n")
	match := odooLine.FindStringSubmatch(lines[0])
	if match == nil {
		return highlight(entry, grep)
	}
	timestamp, pid, level, db, logger, message := match[1], match[2], match[3], match[4], match[5], match[6]
	tint := messageColor[level]
	if logger == "werkzeug" {
		message = colorStatus(message)
	}
	var out strings.Builder
	out.WriteString(ansiDim + timestamp + " " + pid + ansiReset + " ")
	out.WriteString(levelColor[level] + level + ansiReset + " ")
	out.WriteString(ansiCyan + db + ansiReset + " " + ansiDim + logger + ":" + ansiReset + " ")
	out.WriteString(paint(tint, highlight(message, grep)))
	continuation := tint
	if continuation == "" {
		continuation = ansiDim
	}
	for _, line := range lines[1:] {
		out.WriteString("\n" + paint(continuation, highlight(line, grep)))
	}
	return out.String()
}

func paint(color, text string) string {
	if color == "" || text == "" {
		return text
	}
	return color + text + ansiReset
}

func colorStatus(message string) string {
	return httpStatus.ReplaceAllStringFunc(message, func(found string) string {
		parts := httpStatus.FindStringSubmatch(found)
		color := map[byte]string{'2': ansiGreen, '3': ansiCyan, '4': ansiYellow, '5': ansiRed}[parts[2][0]]
		return parts[1] + paint(color, parts[2]) + parts[3]
	})
}

// highlight marks every case-insensitive occurrence of grep in text.
func highlight(text, grep string) string {
	if grep == "" {
		return text
	}
	lower, needle := strings.ToLower(text), strings.ToLower(grep)
	if len(lower) != len(text) { // a case fold changed byte lengths: offsets would not line up
		return text
	}
	var out strings.Builder
	for {
		i := strings.Index(lower, needle)
		if i < 0 {
			out.WriteString(text)
			return out.String()
		}
		out.WriteString(text[:i] + ansiReverse + text[i:i+len(needle)] + ansiReverseOf)
		text, lower = text[i+len(needle):], lower[i+len(needle):]
	}
}

// Column colours, keyed by the API field, in the dashboard's own language:
// production red, staging yellow, development cyan; deployed green, running
// cyan, draft grey, error red.
var cellStyles = map[string]func(string) string{
	"environment_type": func(v string) string {
		return map[string]string{"production": ansiRed, "staging": ansiYellow, "development": ansiCyan}[v]
	},
	"state": stateColor,
	"backup_health": func(v string) string {
		return map[string]string{"healthy": ansiGreen, "warning": ansiYellow, "critical": ansiRed, "none": ansiDim}[v]
	},
	"ssl_status": func(v string) string {
		return map[string]string{"expired": ansiRed, "error": ansiRed, "expiring_soon": ansiYellow, "unknown": ansiYellow}[v]
	},
	"automated":      yesNoColor,
	"platform_login": yesNoColor,
	"version":        func(string) string { return ansiCyan },
}

func stateColor(state string) string {
	switch {
	case strings.HasSuffix(state, "-queue"):
		return ansiCyan
	case state == "deploy" || state == "done" || state == "successful":
		return ansiGreen
	case state == "running" || state == "partial" || state == "starting" || state == "queued":
		return ansiCyan
	case state == "error" || state == "failed" || state == "timeout" || state == "interrupted" || state == "delete":
		return ansiRed
	case state == "draft" || state == "idle" || state == "cancel" || state == "canceled" || state == "disabled":
		return ansiDim
	}
	return ""
}

func yesNoColor(v string) string {
	return map[string]string{"yes": ansiGreen, "no": ansiDim}[v]
}

// style colours one cell by its column; "-" (no value) stays plain.
func style(key, text string) string {
	if fn := cellStyles[key]; fn != nil && text != "-" {
		return paint(fn(text), text)
	}
	return text
}

func boldAll(headers []string) []string {
	bold := make([]string, len(headers))
	for i, header := range headers {
		bold[i] = paint(ansiBold, header)
	}
	return bold
}
