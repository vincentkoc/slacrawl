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
