# Architecture

`cw` is one binary (`cmd/cw`) over layered packages in `internal/`. Code only calls **downward**:
`cmd/cw/layers_test.go` fails the build's tests when an import breaks that.

```
cmd/cw                    entry point: builds commands.App with the version, runs it, exits
└── internal/commands     the command line: dispatch, flags, exit codes, hints — and each command
    ├── mcpbridge         cw mcp: stdio ↔ <platform>/mcp, one JSON-RPC message per line
    ├── selfupdate        cw update: the latest release, checked against checksums.txt, swapped in atomically
    ├── config            the platforms logged in to, the one in use, one token each (keychain or 0600 file)
    ├── output            tables aligned by visible width, colours, JSON, times
    └── platform          the /api/v1 client, and the rule for where a token may be sent
```

| Package      | Owns                                                                                     | May import                          |
| ------------ | ---------------------------------------------------------------------------------------- | ----------------------------------- |
| `commands`   | every command (`account`, `resources`, `logs`, `attention`, `update`, `mcp`) and `cli.go` | all of the below                    |
| `mcpbridge`  | `Bridge`: forwards an AI client's messages with the MCP headers the platform checks       | —                                   |
| `selfupdate` | `LatestTag`, `Fetch`, `ReplaceExecutable`, `Newer`                                         | `platform`                          |
| `config`     | `Config`, `Resolve` (flag → `CW_URL` → saved → default), `SecretStore`                     | `platform`                          |
| `output`     | `Table`, `WriteTable`, `Colorize`, `Style`, `Ago`, `LocalTime`                             | —                                   |
| `platform`   | `Client.Get`, `APIError`, `NormalizeURL` (never plain http off this machine), no redirects | —                                   |

## Where things go

- **A read command over an API function** — a `resource` entry in `commands/resources.go` (list, `show`,
  filters, columns). Anything else gets its own file in `commands/`. Commands reach the platform only through
  `platform.Client`.
- **A column** — the resource's column list; its colour, keyed by the API field, in `output/color.go`.
- **Anything printed** goes through `output`; **anything kept on the machine** through `config`.

## Conventions

- **Exit codes:** `0` success, `1` the platform refused or failed, `2` a mistake on the command line (`usagef`,
  or an HTTP 400), `3` `cw attention` found something.
- **A token goes only where the user sent it:** https (or this machine), no redirects, never on the command line.
- **Colour only in a terminal**, unless `--color always`; `NO_COLOR` is honoured; JSON is never coloured.
- **Tests sit beside the code.** Command tests run the real command line against a fake API (`newHarness`,
  `fakeAPI` in `commands/cli_test.go`); lower layers are tested directly.
- **Release:** push a tag `vX.Y.Z`. GoReleaser builds `./cmd/cw` and stamps `-X main.version`; `cw update`
  and the install script rely on the version-less archive names in `.goreleaser.yaml`.
