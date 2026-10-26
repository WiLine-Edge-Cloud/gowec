# CLAUDE.md — cloud-cli

`gowec`, the terminal client for the WEC public API (`/api/public/v1` on
`cloudstack-integration`). Read the root `../CLAUDE.md` for behavioral
guidelines and platform context.

## Stack

Go (module `github.com/WiLine-Edge-Cloud/gowec`). The Go toolchain is installed on
the host with Homebrew, so unlike the Python backends nothing runs in Docker.

Libraries:
- `cobra` for the command tree
- `lipgloss` (plus its `table` package) for styling
- `ergochat/readline` for the shell
- `shlex`, `x/term` and `yaml.v3`

There is no Viper and no code generation.

## Commands

| Command | What it does |
|---|---|
| `make build` | Builds `bin/gowec`. The version comes from `git describe` and is set with `-X main.version`. |
| `make test` | Runs `go test ./...`. Only `pkg/wec` has tests. |
| `make vet` | Runs `go vet ./...`. |
| `make dist` | A GoReleaser snapshot build of every platform into `dist/`; publishes nothing. |

**Releases:** a pushed `v*` tag runs `.github/workflows/release.yml`: GoReleaser builds every platform and publishes a GitHub Release (archives, `checksums.txt`, `install.sh`) with the workflow's own `GITHUB_TOKEN`, so no secrets are stored. Archive names carry no version, so `install.sh` always fetches `releases/latest/download/gowec_<os>_<arch>.tar.gz`; a suffixed tag (`v0.2.0-rc1`) is a pre-release and never becomes latest. The repo is public (`github.com/WiLine-Edge-Cloud/gowec`): keep internal hosts, people and secrets out of it. README → Releasing has the steps.

## Layout

| Path | What it holds |
|---|---|
| `pkg/wec/` | The SDK. It has no CLI concerns, because the future `terraform-provider-wec` imports it. |
| `internal/config/` | `~/.config/gowec/config.yaml`: contexts (API host + key) and the `GOWEC_*` env overrides. |
| `internal/cli/` | The cobra tree. `Execute` runs one command line; `NewRoot` builds a fresh tree every time. |
| `internal/shell/` | The interactive console you get when `gowec` runs with no arguments on a TTY. |
| `internal/ui/` | Theme, banner, tables, spinner and prompts. |

### `pkg/wec/` (the SDK)

- **Retries** live in `client.go`, and are the money-relevant part:
  - GETs retry on network errors, 5xx and 429.
  - Writes retry only when they carry an `Idempotency-Key`, and always reuse the same key. A 429 is the exception: the server refused it before doing anything.
- **Errors:** `APIError` is the `{"error":{code,message,request_id,...}}` envelope. Its `Extra` field keeps keys such as `available`, `matches` and `quoted_total`.
- **Waiting:** `WaitJob` and `WaitLaunch` poll, backing off from 1 to 5 s.
- **Names:** `resolve` in `names.go` mirrors the API's own name lookup. Use it only for routes that don't take names themselves. VM (machine) paths do take names.

### `internal/cli/`

- A fresh tree per call stops flag values leaking between shell lines.
- Errors cobra raises before a command body runs become `UsageError` (exit 2).

### `internal/shell/`

- Completion goes through cobra's hidden `__complete` command.
- Confirmations and the token prompt must go through readline (`prompter`), never through `os.Stdin`, because readline owns stdin.

## Rules

- **Command grammar.** It is `gowec <resource> <action> [target] [flags]`. Resource names follow the API nouns, in the singular, with plural aliases. The one exception is `vm` (the API says "machines"); `machine` and `instance` are its aliases, and Go code keeps the API's `Machine` naming.
- **Money.** Anything that spends:
  - is quoted first;
  - asks before acting, or requires `--yes` / `--max-price` without a TTY;
  - sends one `Idempotency-Key` per command run.
  
  Never re-send automatically at a new price. Never turn a refused VM delete into a plan delete on the person's behalf.
- **Output.** `-o json` / `-o yaml` print the API's objects unchanged. Tables are for people only. Spinners and banners draw only on a TTY.
- **Passthrough shapes.** The wallet and plan shapes belong to the billing engine. `Plan` and `Wallet` keep `Raw` and marshal it back. Type only the fields a table shows.
- **Backend endpoints.** A new endpoint the CLI needs goes into `cloudstack-integration` on `pre-paid` first. Only the prepaid line serves the public API.

## Testing

- **Go tests:** keep them few, and only for money logic. `pkg/wec/client_test.go` covers idempotent retries, error parsing and the quote total; `pkg/wec/billing_test.go` covers upgrade pricing.
- **Schedules:** CloudStack stores them minutes first (`MM:HH`). The CLI takes `--at HH:MM` / `--day` and converts (`storageWhen` in `snapshot.go`).
- **Retries:** the SDK retries a 5xx only when it may pass (503, 504, or a non-API error page). Retrying the API's own 500/502 just repeats the failure in the request log.
- **Manual checks:**
  1. Log in against the local backend: `gowec auth login --context local --api-url http://localhost:5050`. Use a key from the local portal's Settings → API access; it is a `wec_test_…` key.
  2. Run read-only commands.
  3. Run the shell through a TTY.
- **Money flows:** `vm create` and `plan delete-now` move real money on the shared prepaid billing engine. Ask a maintainer before running them live.
