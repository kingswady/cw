package platform

import "testing"

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"www.cloudwady.com":       "https://www.cloudwady.com",
		"https://example.com/":    "https://example.com",
		"http://localhost:8069/":  "http://localhost:8069",
		"http://127.0.0.1:8069":   "http://127.0.0.1:8069",
		"https://example.com/x?y": "https://example.com/x",
	}
	for in, want := range cases {
		if got, err := NormalizeURL(in); err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://example.com", "ftp://example.com", "https://"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Errorf("NormalizeURL(%q) accepted a URL it must refuse", bad)
		}
	}
}
