# slacrawl

`slacrawl` mirrors Slack workspace data into local SQLite so you can search, inspect, and query channel history without depending on Slack search.

It is a local-first Go CLI. V1 supports Slack Web API ingestion and macOS Slack Desktop local-state discovery. Data stays local.

Use it in one of three ways:

- API mode for Slack app-driven sync
- desktop mode for local Slack Desktop recovery
- hybrid mode when you want both

## What It Does

- discovers the configured workspace
- syncs channels, users, and message history into SQLite
- backfills thread replies when a user token is available
- uses incremental API history sync by default and reserves `--full` for explicit backfills
- maintains FTS5 search indexes for fast local text search
- records structured mentions for direct querying
- exposes read-only SQL for ad hoc analysis
- reports desktop-local Slack cache availability on macOS
- ingests desktop-local workspace metadata, channels, users, cached channel messages, drafts, read markers, recent-channel hints, and custom-status metadata
- tails Socket Mode events when an app-level token is configured
- can periodically refresh desktop-local state with `watch`

## V1 Scope

- one workspace at a time in the CLI
- public channels
- private channels
- top-level messages
- channel threads
- FTS5 search
- read-only SQL access

Out of scope for V1:

- Slack export ZIP import
- DMs and MPIMs
- attachment binary downloads
- write-back actions
- Marketplace/public-distribution guarantees
- full desktop-local sent-message extraction from IndexedDB caches

## Requirements

- Go `1.25+`
- `node` if you want richer desktop-local IndexedDB blob decoding
- a Slack app with a bot token
- an app-level token for Socket Mode if you want `tail`
- an optional user token for complete historical thread replies in public/private channels
- macOS Slack Desktop only if you want desktop-local discovery in V1

## Install

```bash
git clone https://github.com/vincentkoc/slacrawl.git
cd slacrawl
go build -o bin/slacrawl ./cmd/slacrawl
./bin/slacrawl --help
```

## Quick Start

```bash
export SLACK_BOT_TOKEN="xoxb-..."
export SLACK_APP_TOKEN="xapp-..."
export SLACK_USER_TOKEN="xoxp-..."

bin/slacrawl init
bin/slacrawl doctor
bin/slacrawl sync --source api
bin/slacrawl search "incident"
bin/slacrawl tail --repair-every 30m
bin/slacrawl watch --desktop-every 5m
```

Choose the path that matches your setup:

- use `sync --source api` for normal incremental syncs
- use `sync --source api --full` only when you want a deliberate full backfill
- use `sync --source desktop` when you want local desktop recovery only
- use `watch` when you want desktop-local state to refresh into SQLite continuously

## Commands

- `init`
- `doctor`
- `sync`
- `tail`
- `watch`
- `search`
- `messages`
- `mentions`
- `sql`
- `users`
- `channels`
- `status`

## Config

Default runtime paths:

- config: `~/.slacrawl/config.toml`
- database: `~/.slacrawl/slacrawl.db`
- cache: `~/.slacrawl/cache/`
- logs: `~/.slacrawl/logs/`

See [`SPEC.md`](./SPEC.md) for the full product contract and [`config.example.toml`](./config.example.toml) for a starting config.

Desktop config notes:

- set `[slack.desktop].enabled = false` to disable desktop ingestion
- leave `[slack.desktop].path = ""` to auto-detect the macOS Slack path
- set a custom absolute path if Slack Desktop data lives elsewhere
- set `[slack.bot|app|user].enabled = false` to ignore that token source entirely

Deep-dive docs:

- [`docs/configuration.md`](./docs/configuration.md)
- [`docs/desktop-mode.md`](./docs/desktop-mode.md)
