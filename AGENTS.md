# Repository Guidelines

## Project Structure & Module Organization

This repository is a Go CLI project for AgentGit. Entry point is [`main.go`](./main.go). Core git workflow logic lives in [`internal/app/`](./internal/app), including state management, config loading, and integration tests. CLI and TUI code lives in [`internal/cli/`](./internal/cli). Keep new code inside `internal/app` for behavior and `internal/cli` for command or dashboard presentation.

## Build, Test, and Development Commands

- `go build ./...`: compile all packages.
- `go build -o ./agt .`: build a local CLI binary.
- `go test -count=1 ./...`: run the full test suite without cached results.
- `go test -count=1 ./internal/app`: run service and integration tests only.
- `go test -count=1 ./internal/cli`: run CLI/dashboard unit tests.

Use `agt --repo /path/to/repo ...` when manually testing commands against a target repository.

## Coding Style & Naming Conventions

Use standard Go formatting with `gofmt -w`. Follow existing Go naming: exported identifiers use `CamelCase`, internal helpers use `camelCase`, and tests follow `TestXxx`. Keep files ASCII unless the file already requires Unicode. Prefer small service methods with explicit error messages over hidden behavior. Put workflow logic in `internal/app/service.go`; do not bury git state transitions in CLI handlers.

## Testing Guidelines

Tests use Go’s standard `testing` package. Integration tests live in [`internal/app/service_integration_test.go`](./internal/app/service_integration_test.go); focused parser and helper tests live beside the code. Add regression tests for lifecycle changes, snapshot/finalize semantics, and config validation. For TUI work, prefer small unit tests around interaction logic over brittle full-screen snapshots.

## Commit & Pull Request Guidelines

Keep commit messages short, imperative, and scoped, matching current history, for example: `Formalize lifecycle transitions` or `Add operational dashboard actions`. Separate behavior, docs, and cleanup into distinct commits when practical. PRs should include a concise summary, test commands run, and any user-visible CLI or dashboard changes. Include terminal screenshots only when the TUI behavior changes materially.

## Security & Configuration Notes

AgentGit is experimental. Test changes in local or throwaway repositories first, not production repos. Repo policy is stored in local git config under `agentgit.*`; validate new keys and document them in [`README.md`](./README.md).
