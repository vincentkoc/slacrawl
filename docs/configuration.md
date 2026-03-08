# Configuration

`slacrawl` is configured with TOML at `~/.slacrawl/config.toml` by default.

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

## Token Sources

Each Slack token source is controlled independently.

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
