package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type client struct {
	base  string
	token string
	http  *http.Client
}

// apiError is a refusal from the platform, with its machine-readable code.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string {
	switch e.code {
	case "invalid_token":
		return e.message + " — run: cw login"
	case "function_not_allowed":
		return e.message + " (give the token more access in My Settings → API Tokens)"
	}
	return e.message
}

// client builds an authenticated client from CW_TOKEN or the saved token.
func (a *app) client() (*client, error) {
	base, err := a.baseURL("")
	if err != nil {
		return nil, err
	}
	token := a.getenv("CW_TOKEN")
	if token == "" {
		if token, err = a.secrets.Get(base); err != nil {
			return nil, fmt.Errorf("not logged in to %s — run: cw login", base)
		}
	}
	return &client{base: base, token: token, http: a.httpClient}, nil
}

// get returns the response's "data" member.
func (c *client) get(path string, query url.Values) (json.RawMessage, error) {
	target := c.base + "/api/v1" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cw/"+version)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the response from %s: %w", c.base, err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, &apiError{
			status: resp.StatusCode,
			code:   "redirect",
			message: fmt.Sprintf("%s does not serve the platform API here (it redirects to %s) — check the URL, "+
				"or whether the platform has the API enabled", c.base, redirectPath(resp)),
		}
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, &apiError{
			status:  resp.StatusCode,
			code:    "bad_response",
			message: fmt.Sprintf("%s did not answer as the platform API (HTTP %d) — is the URL right?", c.base, resp.StatusCode),
		}
	}
	if envelope.Error != nil {
		return nil, &apiError{status: resp.StatusCode, code: envelope.Error.Code, message: envelope.Error.Message}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &apiError{status: resp.StatusCode, code: "http_error", message: fmt.Sprintf("HTTP %d from %s", resp.StatusCode, c.base)}
	}
	return envelope.Data, nil
}

// decode reads JSON numbers as json.Number so ids print as integers.
func decode(raw json.RawMessage, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

// redirectPath is where a redirect points, without its query ("/web/login").
func redirectPath(resp *http.Response) string {
	target, err := resp.Location()
	if err != nil || target.Path == "" {
		return "another page"
	}
	return target.Path
}
