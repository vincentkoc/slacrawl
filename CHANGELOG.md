# Changelog

## Unreleased

- Add crawlkit control metadata/status surfaces with command-local `metadata --json`, `status --json`, and `doctor --json`.
- Keep status, doctor, and TUI reads safe for fresh or missing local databases without triggering git-share auto-update.
- Add `slacrawl tui`, a terminal archive browser for stored Slack messages using the shared `crawlkit/tui` package.
- Render TUI rows with compact panes and normalized Slack message titles so raw Slack markup stays in detail, not the row list.
- Keep Slack thread roots at top level in the shared TUI while replies remain nested in chat-style thread detail.
- Resolve Slack TUI authors by Slack user ID even when cached user metadata was stored under a different workspace ID.
- Resolve Slack user mentions to cached display names in read paths so the TUI panes do not leak raw `@U...` IDs.
- Hide unresolved Slack user IDs from visible TUI author columns while preserving the raw IDs in detail metadata.
- Inherit the shared crawlkit TUI polish for newest-first startup, count-header sorting, selected-message-first detail panes, and gitcrawl-style metadata labels.
- Feed Slack reply counts and latest-reply timestamps into the TUI detail metadata for thread roots.
- Extend shell completion, help text, and validation smoke coverage for the new TUI command.
