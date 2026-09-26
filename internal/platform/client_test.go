package platform

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestARateLimitSaysWhenToRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":"rate_limited","message":"Too many requests; retry in 12 s"}}`))
	}))
	t.Cleanup(server.Close)
	c := &Client{Base: server.URL, Token: "cwk_x", HTTP: NewHTTPClient(), UserAgent: "cw/test"}
	_, err := c.Get("/whoami", nil)
	var refused *APIError
	if !errors.As(err, &refused) || refused.Status != 429 || refused.RetryAfter != 12*time.Second {
		t.Fatalf("got %#v", err)
	}
}
