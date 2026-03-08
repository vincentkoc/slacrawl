<h1 align="center">slacrawl</h1>

<p align="center">
  <strong>Mirror Slack workspace data into local SQLite for fast search, structured queries, and offline inspection.</strong>
</p>

<p align="center">
  <a href="https://github.com/vincentkoc/slacrawl/blob/main/LICENSE"><img src="https://img.shields.io/github/license/vincentkoc/slacrawl" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.25%2B-00ADD8" alt="Go 1.25+">
  <img src="https://img.shields.io/badge/storage-SQLite-003B57" alt="SQLite">
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey" alt="Platform">
</p>

## Why slacrawl?

Slack search is convenient until you need your own workflow, your own retention, or your own queries. `slacrawl` is a local-first Go CLI that pulls Slack workspace metadata and message history into SQLite so you can inspect it without depending on the Slack UI.

The current implementation focuses on API-based ingestion first, with macOS Slack Desktop discovery available as a read-only bootstrap source. Data stays on your machine.

## What You Get

- local SQLite storage with FTS5-backed search
- workspace, channel, user, and message sync
- thread reply backfill when a user token is available
- mention extraction for structured querying
- read-only SQL access for ad hoc analysis
- `doctor` diagnostics for config, database, token, and desktop-source checks
- optional Socket Mode live tailing via app token

## V1 Scope

- one workspace at a time
- public channels
- private channels
- top-level messages
- channel threads
- local FTS search
- read-only SQL access
- macOS Slack Desktop discovery

Out of scope for V1:

- Slack export ZIP import
- DMs and MPIMs
- attachment blob downloads
- write-back actions
- public Marketplace-style distribution hardening
- desktop-local message extraction beyond the documented bootstrap surface

## Requirements

- Go `1.25+`
- a Slack bot token for standard API sync
- an app token if you want to use `tail`
- an optional user token for fuller historical thread coverage
- macOS Slack Desktop only if you want desktop-local discovery

## Install

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
go run ./cmd/slacrawl sync --source api --full
go run ./cmd/slacrawl search "incident"
```

If you built the binary, replace `go run ./cmd/slacrawl` with `./bin/slacrawl`.

## Commands

- `init` creates a starter config file
- `doctor` checks config, DB access, token presence, FTS, and desktop source availability
- `sync` performs a one-shot crawl from API, desktop, or both
- `tail` listens for live events through Socket Mode
- `search` runs local FTS queries
- `messages` lists stored messages with filters
- `mentions` lists structured mention records
- `sql` runs read-only SQL against the local database
- `users` lists synced users
- `channels` lists synced channels
- `status` prints workspace and sync status

## Default Paths

- config: `~/.slacrawl/config.toml`
- database: `~/.slacrawl/slacrawl.db`
- cache: `~/.slacrawl/cache`
- logs: `~/.slacrawl/logs`

## Configuration

The starter config lives in [`config.example.toml`](./config.example.toml). By default it points to these environment variables:

- `SLACK_BOT_TOKEN`
- `SLACK_APP_TOKEN`
- `SLACK_USER_TOKEN`

Desktop discovery is enabled by default and uses:

```text
~/Library/Containers/com.tinyspeck.slackmacgap/Data/Library/Application Support/Slack
```

## Typical Workflow

```bash
go run ./cmd/slacrawl init
go run ./cmd/slacrawl sync --source api --full
go run ./cmd/slacrawl status
go run ./cmd/slacrawl channels
go run ./cmd/slacrawl messages --channel C12345678 --limit 20
go run ./cmd/slacrawl mentions --limit 20
go run ./cmd/slacrawl sql 'select channel_id, count(*) as messages from messages group by channel_id order by messages desc limit 10;'
```

## Notes on Coverage

- Full historical thread reply coverage in public and private channels depends on providing a user token.
- `tail` requires an app token because it uses Socket Mode.
- Desktop-local support is currently discovery-first and bootstrap-oriented, not a full alternative ingestion path.

## Development

```bash
go test ./...
go build ./cmd/slacrawl
```

See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for contribution workflow and [`SPEC.md`](./SPEC.md) for the implementation contract.

---

Built by <a href="https://github.com/vincentkoc">Vincent Koc</a> · <a href="./LICENSE">MIT</a>
