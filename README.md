<h1 align="center">💾 slacrawl</h1>

<p align="center">
  <strong>A Go-based CLI for mirroring Slack workspace data into local SQLite for search, querying, and offline inspection.</strong>
</p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/vincentkoc/slacrawl" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.25%2B-00ADD8" alt="Go 1.25+">
  <img src="https://img.shields.io/badge/storage-SQLite-003B57" alt="SQLite">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey" alt="Platform">
</p>

<p align="center">
  <img src="screenshot.png" alt="slascrawl Demo"/>
</p>

## Why slacrawl?

Slack search is convenient until you need your own workflow, your own retention, or your own queries. `slacrawl` is a Go-based CLI that pulls Slack workspace metadata and message history into SQLite so you can inspect it without depending on the Slack UI.

Data stays on your machine. You can run it in API mode, desktop mode, or a hybrid workflow that combines both. That covers one-shot syncs, live tailing over Socket Mode, and local desktop recovery or "wiretap" style inspection from Slack Desktop artifacts already on your machine.

## Included

- local SQLite storage with full-text search backed by SQLite FTS5
- workspace, channel, user, and message sync
- thread reply backfill when a user token is available
- incremental API history sync by default, with `--full` reserved for deliberate backfills
- mention extraction for structured querying
- read-only SQL access for ad hoc analysis
- `doctor` diagnostics for config, database, token, and desktop-source checks
- desktop-local ingestion of workspace metadata, channels, users, cached channel messages, drafts, read markers, recent-channel hints, and custom-status metadata
- optional Socket Mode live tailing via app token
- periodic desktop refresh with `watch`

## Current Coverage

- multi-workspace storage and filtering
- multi-workspace API sync when `[[workspaces]]` is configured
- multi-workspace live tailing when per-workspace app tokens are configured
- public channels
- private channels
- top-level messages
- channel threads
- local FTS search
- read-only SQL access
- macOS Slack Desktop discovery

## Not Yet Included

- Slack export ZIP import
- DMs and MPIMs
- attachment blob downloads
- write-back actions
- public Marketplace-style distribution hardening
- desktop-local message extraction beyond the documented bootstrap surface

If one of those gaps matters to your workflow, open an issue so it can be tracked explicitly.

## Requirements

- Go `1.25+`
- `node` if you want richer desktop-local IndexedDB blob decoding
- a Slack bot token for standard API sync
- an app token if you want to use `tail`
- an optional user token for fuller historical thread coverage
- macOS Slack Desktop only if you want desktop-local discovery

## Install

<details open>
<summary>Homebrew (macOS)</summary>

```bash
brew tap vincentkoc/homebrew-tap
brew install slacrawl
```

</details>

<details>
<summary>Linux packages from GitHub Releases</summary>

Download the package that matches your platform from [v0.1.0](https://github.com/vincentkoc/slacrawl/releases/tag/v0.1.0).

Debian/Ubuntu:

```bash
curl -LO https://github.com/vincentkoc/slacrawl/releases/download/v0.1.0/slacrawl_0.1.0_amd64.deb
sudo dpkg -i slacrawl_0.1.0_amd64.deb
```

RHEL/Fedora:

```bash
curl -LO https://github.com/vincentkoc/slacrawl/releases/download/v0.1.0/slacrawl-0.1.0-1.x86_64.rpm
sudo rpm -i slacrawl-0.1.0-1.x86_64.rpm
```

</details>

<details>
<summary>Build from source</summary>

```bash
git clone https://github.com/vincentkoc/slacrawl.git
cd slacrawl
go build -o bin/slacrawl ./cmd/slacrawl
./bin/slacrawl --help
```

</details>

<details>
<summary>Run without building a binary</summary>

```bash
git clone https://github.com/vincentkoc/slacrawl.git
cd slacrawl
go run ./cmd/slacrawl --help
```

</details>

## Quick Start

```bash
export SLACK_BOT_TOKEN="xoxb-..."
export SLACK_APP_TOKEN="xapp-..."
export SLACK_USER_TOKEN="xoxp-..."

go run ./cmd/slacrawl init
go run ./cmd/slacrawl doctor
go run ./cmd/slacrawl sync --source api
go run ./cmd/slacrawl search --workspace T01234567 "incident"
go run ./cmd/slacrawl tail --repair-every 30m
go run ./cmd/slacrawl watch --desktop-every 5m
```

If you built the binary, replace `go run ./cmd/slacrawl` with `./bin/slacrawl`.

`tail` is the live API side of the tool. `watch` is the recurring desktop-side refresh loop.

Choose the path that matches your setup:

- use `sync --source api` for normal incremental syncs
- use `sync --source api --full` only when you want a deliberate full backfill
- use `sync --source desktop` when you want local desktop recovery only
- use `watch` when you want desktop-local state to refresh into SQLite continuously

## Commands

- `init` creates a starter config file
- `doctor` checks config, DB access, token presence, FTS, and desktop source availability
- `sync` performs a one-shot crawl from API, desktop, or both
- `tail` listens for live events through Socket Mode, including one tail per configured workspace
- `watch` refreshes desktop-local state on a schedule
- `search` runs local FTS queries, optionally filtered by workspace
- `messages` lists stored messages with filters
- `mentions` lists structured mention records
- `sql` runs read-only SQL against the local database
- `users` lists synced users
- `channels` lists synced channels
- `status` prints workspace and sync status
- `completion` prints shell completion for `bash` or `zsh`

## Output Modes

The CLI supports three output modes:

- `--format text` for the styled default terminal view
- `--format json` or `--json` for machine-readable output
- `--format log` for line-oriented automation-friendly output

Color is disabled automatically when stdout is not a TTY. You can also force plain text with `--no-color` or `NO_COLOR=1`.

## Make Targets

```bash
make build
make test
make run ARGS="status"
make completion
```

Completion files are generated into `dist/completions/`.

## Shell Completion

Generate completion scripts with:

```bash
go run ./cmd/slacrawl completion bash
go run ./cmd/slacrawl completion zsh
```

Or use the Makefile:

```bash
make completion
```

Typical install locations:

```bash
# bash
go run ./cmd/slacrawl completion bash > /usr/local/etc/bash_completion.d/slacrawl

# zsh
mkdir -p "${HOME}/.zsh/completions"
go run ./cmd/slacrawl completion zsh > "${HOME}/.zsh/completions/_slacrawl"
```

## Default Paths

- config: `~/.slacrawl/config.toml`
- database: `~/.slacrawl/slacrawl.db`
- cache: `~/.slacrawl/cache`
- logs: `~/.slacrawl/logs`

## Configuration

For one workspace, keep using the top-level `[slack.bot]`, `[slack.app]`, and `[slack.user]` token config.

For multiple API workspaces or multiple live wiretap/tail sessions, add `[[workspaces]]` entries with per-workspace token env vars:

```toml
workspace_id = "T01234567"

[[workspaces]]
id = "T01234567"
default = true

[[workspaces]]
id = "T08976543"
bot_token_env = "SLACK_CLIENT_BOT_TOKEN"
app_token_env = "SLACK_CLIENT_APP_TOKEN"
user_token_env = "SLACK_CLIENT_USER_TOKEN"
```

By default, each workspace entry automatically looks for `SLACK_<WORKSPACE_ID>_BOT_TOKEN`, `SLACK_<WORKSPACE_ID>_APP_TOKEN`, and `SLACK_<WORKSPACE_ID>_USER_TOKEN`, so you only need the `id` in the common case. Top-level `enabled` flags still apply globally, which avoids repeating `enabled = true` per workspace.

Without `--workspace`, `sync --source api` and `tail` fan out across every configured workspace entry. Read commands such as `search`, `messages`, `mentions`, `users`, and `channels` accept `--workspace` to scope the shared local database when needed.

The starter config lives in [`config.example.toml`](./config.example.toml). By default it points to these environment variables:

- `SLACK_BOT_TOKEN`
- `SLACK_APP_TOKEN`
- `SLACK_USER_TOKEN`

Desktop discovery is enabled by default and uses:

```text
~/Library/Containers/com.tinyspeck.slackmacgap/Data/Library/Application Support/Slack
```

Desktop config notes:

- set `[slack.desktop].enabled = false` to disable desktop ingestion
- leave `[slack.desktop].path = ""` to auto-detect the macOS Slack path
- set a custom absolute path if Slack Desktop data lives elsewhere
- set `[slack.bot]`, `[slack.app]`, or `[slack.user]` `enabled = false` to ignore that token source entirely

## Typical Workflow

```bash
go run ./cmd/slacrawl init
go run ./cmd/slacrawl sync --source api
go run ./cmd/slacrawl status
go run ./cmd/slacrawl channels
go run ./cmd/slacrawl messages --channel C12345678 --limit 20
go run ./cmd/slacrawl mentions --limit 20
go run ./cmd/slacrawl sql 'select channel_id, count(*) as messages from messages group by channel_id order by messages desc limit 10;'
```

## Notes on Coverage

- Full historical thread reply coverage in public and private channels depends on providing a user token.
- `tail` requires an app token because it uses Socket Mode.
- SQLite FTS5 is the built-in full-text index that powers fast local text search without an external search server.
- Desktop-local support is broader than simple discovery, but still not a full write-back or export-import path.

## Development

```bash
go test ./...
go build ./cmd/slacrawl
```

See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for contribution workflow and [`SPEC.md`](./SPEC.md) for the implementation contract.

Deep-dive docs:

- [`docs/configuration.md`](./docs/configuration.md)
- [`docs/desktop-mode.md`](./docs/desktop-mode.md)

---

Built by <a href="https://github.com/vincentkoc">Vincent Koc</a> · <a href="./LICENSE">MIT</a>
