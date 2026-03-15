# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AgentGit (`agt`) is a Git workflow CLI for coding agents. It gives each agent an isolated worktree, stores agent state in git refs (`refs/agents/<id>/{meta,base,latest,stop}`), supports lightweight snapshot checkpoints between edit batches, and lets humans inspect, stop, roll back, or finalize work. The project is experimental.

## Build and Test Commands

```bash
go build ./...                          # compile all packages
go build -o agt .                       # build local CLI binary
go test -count=1 ./...                  # full test suite (no cache)
go test -count=1 ./internal/app         # service + integration tests only
go test -count=1 ./internal/cli         # CLI/dashboard unit tests only
go test -race -count=1 ./...            # tests with race detector
```

Manual testing against a target repo: `./agt --repo /path/to/repo <command>`

## Architecture

```
main.go                              # entry point, delegates to cli.Execute()
internal/
  app/
    service.go                       # core domain: all git workflow logic (Service struct)
    config.go                        # config loading from git config + env vars
    service_test.go                  # unit tests (parsers, helpers)
    service_integration_test.go      # integration tests (full create/snapshot/done lifecycle)
  cli/
    root.go                          # cobra command tree, CLI output formatting
    dashboard.go                     # bubbletea TUI dashboard (list/detail/diff/file views)
    dashboard_test.go                # dashboard interaction unit tests
```

**Key boundary:** All git state transitions and workflow logic live in `internal/app/service.go`. CLI handlers in `root.go` are thin wrappers that call `Service` methods and format output. Do not put git logic in CLI handlers.

**Service** is the central type. It wraps a repo path and a Config, and exposes methods for the full agent lifecycle: `Create`, `Snapshot`, `Rollback`, `Stop`, `Resume`, `Done`, `Abort`, `Diff`, `Status`, `ListAgents`, `CleanCandidates`, `ApplyClean`, `AgentPreflightInfo`.

**Dashboard** uses Charm's bubbletea with four view modes (`modeList`, `modeDetail`, `modeDiff`, `modeContent`). All mutating dashboard actions (stop, resume, rollback, done, abort) require confirmation.

## Data Model

Agent state is stored entirely in git:
- Worktrees under `<repo>/.worktrees/<id>/`
- Branches named `agent/<id>`
- Refs: `refs/agents/<id>/{meta,base,latest,stop}`
- Snapshots are regular git commit objects with parent history, built using an isolated `GIT_INDEX_FILE` (never touches the real index)

## Config Resolution

Config values resolve in order: environment variable > git config (`agentgit.*`) > default. Environment variables use `AGENTGIT_` prefix (e.g., `AGENTGIT_CLEAN_HOURS`, `AGENTGIT_DEFAULT_OWNER`).

## Testing Patterns

- Integration tests in `service_integration_test.go` create temporary repos via `initTestRepo()` and exercise full lifecycle flows
- `runGit()` / `runGitRaw()` helpers execute git commands in test repos
- Dashboard tests use isolated `dashModel` structs without a live `Service`
- For TUI work, prefer small unit tests around interaction logic over brittle full-screen snapshots

## CLI Conventions

- All commands accept `--repo` (defaults to `.`) and `--json` for machine-readable output
- Agent subcommands (`agent preflight`, `agent should-stop`, `agent checkpoint`, `agent finish`) are integration helpers for automated agents
- `should-stop --exit-code` returns exit 0 = stop, exit 1 = continue (inverted from typical convention)

## Dependencies

- `github.com/spf13/cobra` for CLI command tree
- `github.com/charmbracelet/bubbletea` + `lipgloss` for TUI dashboard
- Go 1.22+, standard `os/exec` for all git operations (no git library)
