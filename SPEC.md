# slacrawl Spec

This file is the build contract for contributors working in this repo.

Goal:

- build a local-first Slack crawler
- mirror Slack workspace data the configured app can access
- store it in SQLite
- support fast text search and raw SQL
- support one-shot backfill and, where credentials allow, live sync

## Product Summary

`slacrawl` is a Go CLI that mirrors Slack workspace data into local SQLite.

V1 scope:

- one workspace at a time in CLI UX
- public channels
- private channels
- top-level messages
- channel threads
- current workspace user snapshot
- FTS5 search
- raw SQL access
- desktop-local Slack discovery on macOS

Out of scope for V1:

- Slack export ZIP import
- DMs and MPIMs
- attachment blob downloads by default
- write-back actions
- Marketplace/public-distribution hardening

## Requirements Already Chosen

- config format: `TOML`
- config location: `~/.slacrawl/config.toml`
- DB location: `~/.slacrawl/slacrawl.db`
- cache dir: `~/.slacrawl/cache/`
- log dir: `~/.slacrawl/logs/`
- language: Go
- schema: single-workspace default, multi-workspace-ready
- search: FTS5 first, embeddings later
- source precedence: user-token API, then bot-token API, then desktop-local cache
- files: metadata only in DB for V1
- desktop-local source: macOS Slack Desktop container path only

## Local Environment Contract

An agent should assume:

- shell: `zsh`
- Go `1.25+` is installed
- macOS desktop-local Slack data may exist under:
  - `~/Library/Containers/com.tinyspeck.slackmacgap/Data/Library/Application Support/Slack`

## Slack Data Model Notes

Important Slack facts that drive the schema:

- messages are scoped by `(channel_id, ts)`
- threads remain message relationships via `thread_ts`
- historical thread replies for public/private channel threads require a user token
- live updates should use Socket Mode when enabled
- desktop-local data is an optional read-only source and must never become a write path

## Database Design

Use SQLite with:

- WAL mode
- foreign keys on
- FTS5 enabled

Tables:

- `workspaces`
- `channels`
- `users`
- `messages`
- `message_events`
- `sync_state`
- `message_mentions`
- `embedding_jobs`
- `message_fts`

Optional later:

- `message_embeddings`

## Search Design

V1 search mode is `fts`.

Normalize:

- Slack mrkdwn
- user mentions
- channel references
- URLs
- file titles
- thread context
- edited and deleted markers

## CLI Spec

Usage:

```text
slacrawl [global flags] <command> [args]
```

Commands:

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

### `sync`

Purpose:

- one-shot crawl

Expected flags:

- `--source api|desktop|all`
- `--workspace <id>`
- `--channels <csv>`
- `--since <timestamp>`
- `--full`
- `--concurrency <n>`

### `doctor`

Must check:

- config file readability
- token presence and shape
- DB openability
- FTS presence
- desktop-local source availability
- whether thread coverage can be full or only partial

### `tail`

Purpose:

- live sync from Socket Mode

Requirements:

- app-level token required
- reconnect automatically
- write checkpoints
- periodic repair sync later

## Config Spec

Format:

- TOML

Location:

- `~/.slacrawl/config.toml`

Credential model:

- bot token: `xoxb-`
- app token: `xapp-`
- optional user token: `xoxp-`

## Sync Algorithm

### API sync

1. load config
2. resolve tokens
3. auth test
4. fetch workspace metadata
5. fetch channels
6. fetch users
7. backfill message history
8. backfill thread replies when a user token is available
9. normalize messages
10. upsert canonical rows
11. update FTS rows and mentions
12. write checkpoints

### Desktop-local sync

1. discover the Slack Desktop path
2. snapshot/copy source artifacts before parsing
3. parse `storage/root-state.json`
4. inspect IndexedDB and Local Storage artifacts
5. upsert any supported metadata surfaced by the bootstrap parser

## Recommended Go Package Layout

```text
cmd/slacrawl/
internal/cli/
internal/config/
internal/slackapi/
internal/slackdesktop/
internal/store/
internal/search/
internal/syncer/
internal/embed/
```

## Milestones

### Milestone 0

- spec and contributor docs
- schema contract
- desktop reverse-engineering fixture plan

### Milestone 1

- config loader
- `init`
- `doctor`
- `status`
- DB open + migrations

### Milestone 2

- workspace metadata sync
- channel sync
- user sync
- message backfill
- FTS indexing

### Milestone 3

- thread coverage
- search
- sql
- users
- channels
- messages
- mentions

### Milestone 4

- desktop-local adapter hardening
- source reconciliation

### Milestone 5

- `tail`
- reconnect logic
- repair loop

## What The Repo Must Eventually Contain

- this spec
- README
- CONTRIBUTING guide
- config sample
- schema and migration files
- CLI contract in code
- tests for config, search, API sync, and desktop-local parsing
