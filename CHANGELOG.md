# Changelog

## Unreleased

- Add a repo-local `slacrawl` agent skill for local Slack archive, freshness,
  query, and verification workflows.
- Document `slacrawl sql` read-only query examples in the repo-local agent
  skill so agents can do exact archive counts and rankings safely.
- Replace the single validation workflow with CI jobs for dependencies,
  formatting/vet, tests, CLI control-surface smoke checks, and GoReleaser
  snapshot builds.
- Add CodeQL analysis on pull requests, `main`, the crawlkit integration branch,
  weekly schedule, and manual dispatch.
- Depend on `github.com/vincentkoc/crawlkit v0.4.0` for shared config,
  status/control, snapshot, mirror, state, output, and terminal explorer
  mechanics.
- Keep Slack API/Desktop parsing, token scopes, Slack schema, Slack text
  normalization, channel/thread semantics, and analytics app-owned while the
  shared mechanics move to crawlkit.
- Document the gitcrawl-style TUI shape: workspace/channel/person groups,
  message rows, message/thread detail, sorting, mouse selection, right-click
  actions, and local/remote status chrome.
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
