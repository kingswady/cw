package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

const tokenPrefix = "cwk_"

type whoamiData struct {
	Login      string `json:"login"`
	Name       string `json:"name"`
	Namespaces []struct {
		Name  string `json:"name"`
		Code  string `json:"code"`
		Level string `json:"level"`
	} `json:"namespaces"`
}

func (a *app) login(args []string) error {
	fs := a.newFlags("login")
	urlFlag := fs.String("url", "", "platform URL (default "+defaultURL+")")
	withToken := fs.Bool("with-token", false, "read the token from standard input")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	base, err := a.baseURL(*urlFlag)
	if err != nil {
		return err
	}
	token, err := a.readToken(base, *withToken)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(token, tokenPrefix) {
		return usagef("that is not an API token — they start with %s (create one in My Settings → API Tokens)", tokenPrefix)
	}
	me, err := checkToken(&client{base: base, token: token, http: a.httpClient})
	if err != nil {
		return err
	}
	where, err := a.secrets.Set(base, token)
	if err != nil {
		return fmt.Errorf("saving the token: %w", err)
	}
	if err := a.saveConfig(config{URL: base}); err != nil {
		return fmt.Errorf("saving the config: %w", err)
	}
	if me == nil {
		fmt.Fprintf(a.stdout, "Logged in to %s (this token may not read your identity).\n", base)
	} else {
		fmt.Fprintf(a.stdout, "Logged in to %s as %s (%s).\n", base, me.Name, me.Login)
	}
	fmt.Fprintf(a.stdout, "Token saved in %s.\n", where)
	return nil
}

// checkToken asks /whoami; a token without that function is still a valid token.
func checkToken(c *client) (*whoamiData, error) {
	raw, err := c.get("/whoami", nil)
	var refused *apiError
	if errors.As(err, &refused) && refused.code == "function_not_allowed" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var me whoamiData
	if err := decode(raw, &me); err != nil {
		return nil, err
	}
	return &me, nil
}

func (a *app) readToken(base string, fromStdin bool) (string, error) {
	if fromStdin || a.readSecret == nil {
		raw, err := io.ReadAll(io.LimitReader(a.stdin, 4096))
		return strings.TrimSpace(string(raw)), err
	}
	// Say where the token will go before asking for it (--url picks another platform).
	fmt.Fprintf(a.stderr, "Logging in to %s\n", base)
	token, err := a.readSecret("Paste your API token (My Settings → API Tokens): ")
	return strings.TrimSpace(token), err
}

func (a *app) logout(args []string) error {
	fs := a.newFlags("logout")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	base, err := a.baseURL("")
	if err != nil {
		return err
	}
	if err := a.secrets.Delete(base); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Logged out of %s. Revoke the token in My Settings → API Tokens if it may have leaked.\n", base)
	return nil
}

func (a *app) whoami(args []string) error {
	fs := a.newFlags("whoami")
	asJSON := fs.Bool("json", false, "print the API response as JSON")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	raw, err := c.get("/whoami", nil)
	if err != nil {
		return err
	}
	if *asJSON {
		return writeJSON(a.stdout, raw)
	}
	var me whoamiData
	if err := decode(raw, &me); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "%s (%s) on %s\n\n", me.Name, me.Login, c.base)
	rows := make([][]string, 0, len(me.Namespaces))
	for _, ns := range me.Namespaces {
		rows = append(rows, []string{ns.Name, ns.Code, ns.Level})
	}
	return writeTable(a.stdout, []string{"NAMESPACE", "CODE", "LEVEL"}, rows)
}
