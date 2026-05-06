---
name: slacrawl
description: Use for local Slack archive search, sync freshness, channels/messages/mentions, desktop/API/git-share sources, TUI browsing, and Slacrawl repo/release work.
---

# Slacrawl

Use local Slack archive data first for Slack questions. Hit Slack APIs only
when the archive is stale, missing the requested scope, or the user asks for
current external context.

## Sources

- DB: `~/.slacrawl/slacrawl.db`
- Config: `~/.slacrawl/config.toml`
- Cache: `~/.slacrawl/cache`
- Logs: `~/.slacrawl/logs`
- Git share repo: `~/.slacrawl/share`
- Repo: `~/GIT/_Perso/slacrawl`
- Preferred CLI: `slacrawl`; fallback to `go run ./cmd/slacrawl` from the repo if the installed binary is stale

## Freshness

For recent/current questions, check freshness before analysis:

```bash
slacrawl status --json
```

For precise freshness from the default database:

```bash
sqlite3 ~/.slacrawl/slacrawl.db \
  "select coalesce(max(updated_at), '') from sync_state where source_name != 'doctor';"
```

Routine diagnostics:

```bash
slacrawl doctor
```

Desktop-local refresh:

```bash
slacrawl sync --source desktop
```

API latest refresh, when tokens are available:

```bash
slacrawl sync --source api --latest-only
```

Use `--full` only for deliberate historical backfills.

## Query Workflow

1. Resolve workspace, channel/DM, date range, user, and keyword.
2. Check freshness if the question is recent/current.
3. Prefer CLI search/messages for slices; use read-only SQL for exact counts.
4. Report workspace/channel names, date spans, counts, and token/source limits.

Common commands:

```bash
slacrawl search "query"
slacrawl messages --limit 50
slacrawl channels --json
slacrawl users --json
slacrawl mentions --limit 50
slacrawl sql 'select count(*) from messages;'
```

## SQL

Use `slacrawl sql` for exact counts, joins, and ranking queries when normal
CLI reads are too coarse. The command is read-only and accepts SQL as args or
stdin. Prefer `--json` for agent parsing.

Useful examples:

```bash
slacrawl --json sql 'select count(*) as messages from messages;'
slacrawl --json sql "select coalesce(nullif(c.name, ''), m.channel_id) as channel, count(*) as messages from messages m left join channels c on c.id = m.channel_id and c.workspace_id = m.workspace_id group by m.workspace_id, m.channel_id order by messages desc limit 20;"
slacrawl --json sql "select coalesce(nullif(u.display_name, ''), nullif(u.real_name, ''), nullif(u.name, ''), m.user_id) as author, count(*) as messages from messages m left join users u on u.id = m.user_id and u.workspace_id = m.workspace_id group by m.workspace_id, m.user_id order by messages desc limit 20;"
```

Keep SQL to `select`/`with` reads. Do not use SQL to mutate the archive.

When the installed CLI lacks a new feature, build or run from
`~/GIT/_Perso/slacrawl` before concluding the feature is missing.

## Slack Boundaries

API sync requires configured Slack tokens; do not invent token availability.
User tokens are needed for fuller historical threads, DMs, and MPIMs. Desktop
mode reads local Slack Desktop artifacts and must not write to Slack
application storage. Git-share snapshots must not include secrets.

## Verification

For repo edits, prefer existing Go gates:

```bash
GOWORK=off go test ./...
make test
```

Then run targeted CLI smoke for the touched surface, for example:

```bash
slacrawl doctor
slacrawl status --json
slacrawl search "test"
```
