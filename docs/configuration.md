# Configuration

`slacrawl` is configured with TOML at `~/.slacrawl/config.toml` by default.

Config path resolution, runtime directories, status payloads, and token
diagnostic formatting are normalized through `crawlkit`. Slack token scopes,
workspace selection, API/Desktop source behavior, and schema compatibility stay
in `slacrawl`.

The config is designed to work with safe defaults:

- SQLite lives under `~/.slacrawl/`
- Slack Desktop is enabled by default
- the desktop path is auto-detected when left blank
- Slack tokens are resolved from environment variables

## Example

```toml
version = 1
workspace_id = ""
db_path = "~/.slacrawl/slacrawl.db"
cache_dir = "~/.slacrawl/cache"
log_dir = "~/.slacrawl/logs"

[[workspaces]]
id = "T01234567"
default = true
# uses:
# SLACK_T01234567_BOT_TOKEN
# SLACK_T01234567_APP_TOKEN
# SLACK_T01234567_USER_TOKEN

[slack.bot]
enabled = true
token_env = "SLACK_BOT_TOKEN"

[slack.app]
enabled = true
token_env = "SLACK_APP_TOKEN"

[slack.user]
enabled = true
token_env = "SLACK_USER_TOKEN"

[slack.desktop]
enabled = true
path = ""

[sync]
concurrency = 4
repair_every = "30m"
desktop_refresh_every = "5m"
full_history = true

[search]
default_mode = "fts"

[share]
remote = ""
repo_path = "~/.slacrawl/share"
branch = "main"
auto_update = true
stale_after = "15m"
```

## Workspace Selection

`workspace_id` remains the default CLI workspace.

Use `[[workspaces]]` when you want separate bot/app/user tokens per Slack workspace, especially for multi-workspace API sync and live tailing:

```toml
[[workspaces]]
id = "T01234567"
default = true

[[workspaces]]
id = "T08976543"
bot_token_env = "SLACK_CLIENT_BOT_TOKEN"
app_token_env = "SLACK_CLIENT_APP_TOKEN"
user_token_env = "SLACK_CLIENT_USER_TOKEN"
```

Behavior:

- each workspace automatically tries `SLACK_<WORKSPACE_ID>_BOT_TOKEN`, `SLACK_<WORKSPACE_ID>_APP_TOKEN`, and `SLACK_<WORKSPACE_ID>_USER_TOKEN`
- top-level `enabled` flags are inherited, so you do not need to repeat `enabled = true` for every workspace
- `bot_token_env`, `app_token_env`, and `user_token_env` are optional overrides when you do not want the default env naming convention
- `sync --source api` without `--workspace` runs against every configured `[[workspaces]]` entry
- `tail` without `--workspace` starts one live tail per configured `[[workspaces]]` entry
- `search`, `messages`, `mentions`, `users`, and `channels` accept `--workspace` to filter the shared SQLite database
- if `[[workspaces]]` is empty, the legacy top-level `[slack.*]` token config is used

## Git Archive Sharing

Use `[share]` when you want one machine to publish a private Slack archive snapshot and other machines to query it locally without Slack API credentials.

```toml
[share]
remote = "git@github.com:your-org/private-slacrawl-archive.git"
repo_path = "~/.slacrawl/share"
branch = "main"
auto_update = true
stale_after = "15m"
```

Behavior:

- `publish` exports gzipped JSONL table shards plus `manifest.json` into `repo_path`
- `subscribe` writes a git-reader config, disables Slack API and desktop sources for that config, clones the repo, and imports the snapshot
- pass `--db` to `subscribe` when you want the reader archive to use a non-default SQLite file
- `update` pulls and imports only when the manifest changed
- `status`, `search`, `messages`, `mentions`, `sql`, `users`, `channels`, and `report` auto-refresh stale git-backed snapshots before querying when `auto_update = true`
- `stale_after` controls how old the last successful import can be before the next read pulls/imports again
- `status` and `doctor` show the configured share repo plus last import / manifest freshness details

## Token Sources

Each Slack token source is controlled independently.

Text normalization notes:

- malformed UTF-8 is repaired before indexing
- compatibility forms are normalized with NFKC
- zero-width and non-printable control noise is stripped from indexed text
- weird spacing is collapsed so FTS and mentions stay queryable even when Slack/Desktop payloads are messy

### Bot token

Use the bot token for normal API sync:

- channel discovery
- users snapshot
- channel history

Disable it entirely if you want desktop-only operation:

```toml
[slack.bot]
enabled = false
token_env = "SLACK_BOT_TOKEN"
```

### App token

Use the app token only when you want live Socket Mode tailing:

```toml
[slack.app]
enabled = true
token_env = "SLACK_APP_TOKEN"
```

If app tailing is not needed, disable it:

```toml
[slack.app]
enabled = false
token_env = "SLACK_APP_TOKEN"
```

### User token

The user token is optional, but it upgrades historical thread coverage for public and private channels.

```toml
[slack.user]
enabled = true
token_env = "SLACK_USER_TOKEN"
```

If you do not want user-token access at all:

```toml
[slack.user]
enabled = false
token_env = "SLACK_USER_TOKEN"
```

## Desktop Source

Desktop ingestion is optional and read-only.

```toml
[slack.desktop]
enabled = true
path = ""
```

Behavior:

- `enabled = true` turns on desktop sync support
- `path = ""` auto-detects the supported macOS Slack container path
- `path = "/custom/path"` overrides detection

To disable desktop ingestion completely:

```toml
[slack.desktop]
enabled = false
path = ""
```

## Sync Settings

### `repair_every`

Used by `tail` to run periodic API reconciliation during live sync.

```toml
[sync]
repair_every = "30m"
```

### `desktop_refresh_every`

Used by `watch` to periodically refresh local Slack Desktop state into SQLite.

```toml
[sync]
desktop_refresh_every = "5m"
```

### `concurrency`

Used by API sync to fan out channel history fetches across workers. Keep the default unless you have a reason to tune it for a specific workspace.

Notes:

- higher values increase API fan-out, not write parallelism inside SQLite
- useful mainly for multi-channel API sync, not single-channel runs
- `--concurrency` on the CLI overrides the config value for that run

### `latest-only`

Use `sync --latest-only` when you want to refresh only channels that already have a stored cursor.

Notes:

- useful for fast publisher jobs that already seeded history once
- channels with no local history are skipped instead of triggering a first-time backfill
- `--full` overrides this behavior and still does the full crawl

## Recommended Profiles

### Desktop only

```toml
[slack.bot]
enabled = false
token_env = "SLACK_BOT_TOKEN"

[slack.app]
enabled = false
token_env = "SLACK_APP_TOKEN"

[slack.user]
enabled = false
token_env = "SLACK_USER_TOKEN"

[slack.desktop]
enabled = true
path = ""
```

### API sync without live tail

```toml
[slack.bot]
enabled = true
token_env = "SLACK_BOT_TOKEN"

[slack.app]
enabled = false
token_env = "SLACK_APP_TOKEN"

[slack.user]
enabled = true
token_env = "SLACK_USER_TOKEN"
```

### API sync with live tail and desktop refresh

```toml
[slack.bot]
enabled = true
token_env = "SLACK_BOT_TOKEN"

[slack.app]
enabled = true
token_env = "SLACK_APP_TOKEN"

[slack.user]
enabled = true
token_env = "SLACK_USER_TOKEN"

[slack.desktop]
enabled = true
path = ""

[sync]
repair_every = "30m"
desktop_refresh_every = "5m"
```
