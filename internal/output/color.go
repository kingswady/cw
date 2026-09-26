package output

import (
	"regexp"
	"strings"
)

const (
	Reset      = "\x1b[0m"
	Dim        = "\x1b[2m"
	Red        = "\x1b[31m"
	Green      = "\x1b[32m"
	Yellow     = "\x1b[33m"
	Cyan       = "\x1b[36m"
	reverse    = "\x1b[7m"
	reverseOff = "\x1b[27m" // ends the highlight without ending the colour around it
	Bold       = "\x1b[1m"
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
var messageColor = map[string]string{"WARNING": Yellow, "ERROR": Red, "CRITICAL": Red}

// colorize renders one log entry (it may span lines: a traceback) for a terminal.
func Colorize(entry, grep string) string {
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
	out.WriteString(Dim + timestamp + " " + pid + Reset + " ")
	out.WriteString(levelColor[level] + level + Reset + " ")
	out.WriteString(Cyan + db + Reset + " " + Dim + logger + ":" + Reset + " ")
	out.WriteString(Paint(tint, highlight(message, grep)))
	continuation := tint
	if continuation == "" {
		continuation = Dim
	}
	for _, line := range lines[1:] {
		out.WriteString("\n" + Paint(continuation, highlight(line, grep)))
	}
	return out.String()
}

func Paint(color, text string) string {
	if color == "" || text == "" {
		return text
	}
	return color + text + Reset
}

func colorStatus(message string) string {
	return httpStatus.ReplaceAllStringFunc(message, func(found string) string {
		parts := httpStatus.FindStringSubmatch(found)
		color := map[byte]string{'2': Green, '3': Cyan, '4': Yellow, '5': Red}[parts[2][0]]
		return parts[1] + Paint(color, parts[2]) + parts[3]
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
		out.WriteString(text[:i] + reverse + text[i:i+len(needle)] + reverseOff)
		text, lower = text[i+len(needle):], lower[i+len(needle):]
	}
}

// Column colours, keyed by the API field, in the dashboard's own language:
// production red, staging yellow, development cyan; deployed green, running
// cyan, draft grey, error red.
var cellStyles = map[string]func(string) string{
	"environment_type": func(v string) string {
		return map[string]string{"production": Red, "staging": Yellow, "development": Cyan}[v]
	},
	"state": stateColor,
	"backup_health": func(v string) string {
		return map[string]string{"healthy": Green, "warning": Yellow, "critical": Red, "none": Dim}[v]
	},
	"ssl_status": func(v string) string {
		return map[string]string{"expired": Red, "error": Red, "expiring_soon": Yellow, "unknown": Yellow}[v]
	},
	"automated":      yesNoColor,
	"platform_login": yesNoColor,
	"version":        func(string) string { return Cyan },
}

func stateColor(state string) string {
	switch {
	case strings.HasSuffix(state, "-queue"):
		return Cyan
	case state == "deploy" || state == "done" || state == "successful":
		return Green
	case state == "running" || state == "partial" || state == "starting" || state == "queued":
		return Cyan
	case state == "error" || state == "failed" || state == "timeout" || state == "interrupted" || state == "delete":
		return Red
	case state == "draft" || state == "idle" || state == "cancel" || state == "canceled" || state == "disabled":
		return Dim
	}
	return ""
}

func yesNoColor(v string) string {
	return map[string]string{"yes": Green, "no": Dim}[v]
}

// style colours one cell by its column; "-" (no value) stays plain.
func Style(key, text string) string {
	if fn := cellStyles[key]; fn != nil && text != "-" {
		return Paint(fn(text), text)
	}
	return text
}

func BoldAll(headers []string) []string {
	bold := make([]string, len(headers))
	for i, header := range headers {
		bold[i] = Paint(Bold, header)
	}
	return bold
}
