# Contributing

## Setup

```bash
go test ./...
go build ./cmd/slacrawl
```

## Workflow

1. Create a dedicated worktree/branch with `gwt new <branch>`.
2. Keep changes scoped and reviewable.
3. Run `go test ./...` before opening a PR.
4. Keep docs and code in sync. `SPEC.md` is the implementation contract.

## Pull Requests

- Use `gh` for PR operations.
- Prefer draft PRs first.
- Link issues with `Fixes: <issue>` when applicable.
- Add tests for behavior changes and regressions.
- Keep secrets out of git and out of command examples.

## Code Style

- Use Go stdlib and small stable dependencies.
- Prefer explicit structs and straightforward control flow.
- Keep Slack-specific behavior documented in code comments only when the reason is non-obvious.
- Do not silently swallow partial-coverage states. Surface them in status or doctor output.
