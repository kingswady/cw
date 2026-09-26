package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/term"
)

// app holds everything a command touches, so tests can swap each piece.
type app struct {
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	getenv     func(string) string
	configDir  string
	secrets    secretStore
	httpClient *http.Client
	// readSecret prompts for the token without echo; nil when stdin is not a terminal.
	readSecret func(prompt string) (string, error)
	// interrupt and wait pace --follow; tests replace them.
	interrupt func() (context.Context, func())
	wait      func(ctx context.Context, d time.Duration) bool
	// stdoutIsTerminal decides whether logs are coloured by default.
	stdoutIsTerminal func() bool
	// executable is the file cw update replaces.
	executable func() (string, error)
	// current is the client the command used, for its token's expiry.
	current *client
	// now is swapped in tests.
	clock func() time.Time
}

func newApp() *app {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	dir = filepath.Join(dir, "cw")
	a := &app{
		stdin:      os.Stdin,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		getenv:     os.Getenv,
		configDir:  dir,
		secrets:    keychainStore{fallback: fileStore{path: filepath.Join(dir, "credentials.json")}},
		httpClient: newHTTPClient(),
		interrupt:  signalContext,
		wait:       sleepOrDone,
		stdoutIsTerminal: func() bool {
			return term.IsTerminal(int(os.Stdout.Fd()))
		},
		executable: executablePath,
		clock:      time.Now,
	}
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		a.readSecret = func(prompt string) (string, error) {
			fmt.Fprint(a.stderr, prompt)
			raw, err := term.ReadPassword(fd)
			fmt.Fprintln(a.stderr)
			return string(raw), err
		}
	}
	return a
}

// newHTTPClient never follows a redirect: the token goes to the URL given and
// nowhere else, and a redirect (to a login page, say) is never the API.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// usageError is a mistake on the command line: exit code 2, not 1.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		a.usage(a.stderr)
		return 2
	}
	name, rest := args[0], args[1:]
	code := a.dispatch(name, rest)
	a.afterCommand(name, code)
	return code
}

func (a *app) dispatch(name string, rest []string) int {
	switch name {
	case "help", "-h", "--help":
		a.usage(a.stdout)
		return 0
	case "version", "--version":
		fmt.Fprintln(a.stdout, "cw", version)
		return 0
	case "login":
		return a.exit(a.login(rest))
	case "logout":
		return a.exit(a.logout(rest))
	case "whoami":
		return a.exit(a.whoami(rest))
	case "logs":
		return a.exit(a.logs(rest))
	case "update":
		return a.exit(a.update(rest))
	case "use":
		return a.exit(a.use(rest))
	}
	if res := resourceNamed(name); res != nil {
		return a.exit(a.resource(res, rest))
	}
	fmt.Fprintf(a.stderr, "cw: unknown command %q\n\n", name)
	a.usage(a.stderr)
	return 2
}

func (a *app) exit(err error) int {
	var usage *usageError
	var refused *apiError
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &usage), errors.As(err, &refused) && refused.status == http.StatusBadRequest:
		// A 400 is the platform saying the command line was wrong (--limit 0).
		fmt.Fprintln(a.stderr, "cw:", err)
		return 2
	default:
		fmt.Fprintln(a.stderr, "cw:", err)
		return 1
	}
}

func (a *app) usage(w io.Writer) {
	fmt.Fprint(w, `cw reads your apps, servers, backups and runs from the terminal.

Usage: cw <command> [flags]

Account
  login        Save an API token (create one in My Settings → API Tokens)
  logout       Forget the saved token
  whoami       Show who the token acts as, and its namespaces
  use          Switch between platforms you logged in to
  update       Update cw to the latest release (--check only looks)

Read
  apps         List apps                  cw apps show <id|name>
  servers      List servers               cw servers show <id|name>
  installers   List installed services    cw installers show <id|name>
  backups      List backups               cw backups show <id>
  runs         List recent runs           cw runs show <id>
  logs         An app's Odoo log          cw logs <app> [--since 1h] [--grep …] [--follow]

Flags on every read command
  --json              Print the API response as JSON
  --namespace <code>  Only this namespace of the token
  --limit <n>         Rows per page (1-200, default 50)
  --offset <n>        Rows to skip

Environment
  CW_URL     Platform URL (default `+defaultURL+`)
  CW_TOKEN   API token; overrides the saved one (for CI)

Run "cw <command> --help" for a command's own flags.
`)
}

// newFlags is a flag set that reports to stderr and returns errors instead of exiting.
func (a *app) newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("cw "+name, flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	return fs
}

// parseArgs lets flags follow positional arguments ("cw apps show shop --json"),
// which the flag package alone stops at.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}
