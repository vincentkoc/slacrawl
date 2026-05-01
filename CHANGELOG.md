# Changelog

## Unreleased

- Add crawlkit control metadata/status surfaces with command-local `metadata --json`, `status --json`, and `doctor --json`.
- Keep status, doctor, and TUI reads safe for fresh or missing local databases without triggering git-share auto-update.
- Add `slacrawl tui`, a terminal archive browser for stored Slack messages using the shared `crawlkit/tui` package.
- Extend shell completion, help text, and validation smoke coverage for the new TUI command.
