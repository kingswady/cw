package platform

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// NormalizeURL accepts "host", "https://host/" and "http://localhost:8069";
// a token never travels over plain http to anything but this machine.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("not a URL: %q", raw)
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if !IsLoopback(parsed.Hostname()) {
			return "", fmt.Errorf("refusing to send a token over plain http to %s — use https", parsed.Host)
		}
	default:
		return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String(), nil
}

// IsLoopback reports whether host is this machine.
func IsLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
