# slacrawl

`slacrawl` mirrors Slack workspace data into local SQLite so you can search, inspect, and query channel history without depending on Slack search.

It is a local-first Go CLI. V1 supports Slack Web API ingestion and macOS Slack Desktop local-state discovery. Data stays local.

## What It Does

- discovers the configured workspace
- syncs channels, users, and message history into SQLite
- backfills thread replies when a user token is available
- maintains FTS5 search indexes for fast local text search
- records structured mentions for direct querying
- exposes read-only SQL for ad hoc analysis
- reports desktop-local Slack cache availability on macOS

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
- desktop-local message extraction beyond the documented bootstrap surface

## Requirements

- Go `1.25+`
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
bin/slacrawl sync --source api --full
bin/slacrawl search "incident"
```

## Commands

- `init`
- `doctor`
- `sync`
- `tail`
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
