# Contributing to slacrawl

Thanks for contributing. This project is still early, so the main goal is to keep changes small, testable, and aligned with the current product contract in [`SPEC.md`](./SPEC.md).

## Before You Start

- Read [`SPEC.md`](./SPEC.md) before changing behavior or CLI surfaces.
- Open an issue first for larger changes, new commands, or scope changes.
- Keep secrets and real Slack workspace data out of git, screenshots, fixtures, and examples.

## Local Setup

Requirements:

- Go `1.25+`
- SQLite with FTS5 support

Build and test:

```bash
go test ./...
go build ./cmd/slacrawl
```

Run the CLI locally:

```bash
go run ./cmd/slacrawl --help
```

## Development Workflow

1. Create a dedicated worktree and branch with `gwt new <branch>`.
2. Keep the branch focused on one change or one small related set of changes.
3. Update docs when behavior, flags, config, or scope changes.
4. Run tests before opening a pull request.
5. Re-read your diff for accidental secret exposure, noisy refactors, or unrelated edits.

## Pull Requests

- Use `gh` for pull request operations.
- Prefer opening draft PRs first.
- Link related issues with `Fixes: <issue>` when applicable.
- Explain the user-visible behavior change and any important tradeoffs.
- Add or update tests for behavior changes and regressions.
- Keep PRs reviewable. Smaller is better.

## Coding Guidelines

- Prefer Go stdlib and small, stable dependencies.
- Use explicit structs and straightforward control flow.
- Preserve the local-first model. Do not introduce remote storage requirements for core usage.
- Surface partial-coverage states explicitly in `doctor`, `status`, or command output instead of hiding them.
- Add comments only when the reason for Slack-specific behavior is not obvious from the code.

## Documentation

- Keep [`README.md`](./README.md), [`SPEC.md`](./SPEC.md), and examples in sync with the implementation.
- Do not document features as supported until they are actually implemented.
- When changing config keys or defaults, update [`config.example.toml`](./config.example.toml).

## Testing Expectations

- Run `go test ./...` before opening a PR.
- Add targeted tests when changing parsing, normalization, config loading, store behavior, or CLI output.
- If a known failing test blocks your branch, call that out clearly in the PR description.

## Reporting Bugs

When filing an issue, include:

- what you ran
- what you expected
- what happened instead
- relevant config snippets with secrets removed
- platform details such as OS, Go version, and whether Slack Desktop discovery was involved
