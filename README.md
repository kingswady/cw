# cw — the platform from your terminal

`cw` reads your apps, servers, installed services, backups and runs through the platform's customer API. It is
read-only: nothing you run with it can change, deploy or delete anything.

## Install

macOS and Linux:

```bash
curl -fsSL https://www.cloudwady.com/install.sh | sh
curl -fsSL https://www.cloudwady.com/install.sh | sh -s -- --version v1.0.0 --dir "$HOME/bin"
```

The script downloads the release for your machine, checks its SHA-256 against the release's `checksums.txt`,
and installs `cw` into `/usr/local/bin` if you can write there, `~/.local/bin` otherwise. It never uses sudo.
Your platform serves the same script at its own address, and then tells you the matching `cw login --url`.

Windows: download `cw_windows_amd64.zip` from the [releases](https://github.com/kingswady/cw/releases).

From source (Go 1.26+, or Docker): `make build` → `bin/cw`; `make docker-dist` builds every platform.

## Log in

Create a token in the dashboard under **My Settings → API Tokens**: choose its namespaces and what it may
read. The token is shown once.

```bash
cw login                                   # paste the token; it is not echoed
cw login --url https://your.platform.host  # a platform other than www.cloudwady.com
echo "$TOKEN" | cw login --with-token      # from a script
cw whoami
```

A later `cw login` goes back to the platform you last logged in to, and says so; `--url` picks another.

The token is kept in the system keychain (macOS Keychain, Windows Credential Manager, the Secret Service on
Linux). Where there is none, it goes to a file only you can read, next to `cw`'s config. `cw logout` forgets
it; revoke it in the dashboard if it may have leaked.

## Read

```bash
cw apps                          # all apps the token can see
cw apps --env production
cw apps show shop                # by name or id
cw servers
cw installers --server prod-1
cw backups --app shop
cw runs --app shop --state error
cw runs show 4812                # steps, and why a failed step failed
```

Every read command takes `--json` (the API's own response, for `jq`), `--namespace <code>`, `--limit` and
`--offset`. A token never sees more than its namespaces and your own access, whatever you ask for.

## Logs

```bash
cw logs shop                         # the newest 200 lines of the last hour
cw logs shop --since 15m --grep ERROR
cw logs shop --limit 2000            # up to 2 000 lines, up to a day back
cw logs shop --follow                # keep printing new lines (Ctrl-C to stop)
```

The app's Odoo log, from the platform's log store — only that app's lines, secrets masked. `--grep` matches
text, any case. The token needs **Logs** in what it can read.

## In CI

```bash
export CW_URL=https://www.cloudwady.com
export CW_TOKEN=cwk_…        # from your CI secret store
cw runs --app shop --state error --json
```

`CW_TOKEN` overrides the saved token; `CW_URL` the saved platform. Exit codes: `0` success, `1` the platform
refused or failed, `2` a mistake on the command line.

## License

Apache License 2.0 — see [LICENSE](LICENSE). The CloudWady name and logo are not covered by it.
