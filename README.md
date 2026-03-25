# AgentGit

AgentGit is a git workflow tool for coding agents.

It gives each agent its own worktree, stores agent state in git refs, supports lightweight snapshots between edit batches, and lets a human inspect, stop, roll back, or finalize the work.

The CLI binary is `agt`.

## Stability

AgentGit is experimental and is not production-ready yet.

Use it in local or test repositories first. Do not make it the primary workflow for production repositories until it has been validated in your environment.

## Features

- isolated agent worktrees via `git worktree`
- snapshot checkpoints without touching the real index
- rollback to base, `~N`, or `snap-N`
- agent status, diff, and list commands
- cooperative stop signal
- finalize to a clean branch commit with `done`
- cleanup for abandoned agent state
- terminal dashboard with list, detail, diff, and file views
- repo-level config and policy via `git config`
- agent integration helpers for preflight, stop checks, and checkpoints
- **[Agent Integration Guide](./AGENT_GUIDE.md)** for embedding AgentGit into AI agent system prompts

## Build

```bash
go build ./...
```

Build a local binary:

```bash
go build -o agt .
```

Run tests:

```bash
go test -count=1 ./...
```

## Quick Start

Create an agent workspace:

```bash
agt --repo /path/to/repo create fix-auth \
  --purpose "fix email validation" \
  --owner claude
```

Save a checkpoint:

```bash
agt --repo /path/to/repo snapshot fix-auth --msg "after validator change"
```

Inspect current state:

```bash
agt --repo /path/to/repo status fix-auth
agt --repo /path/to/repo diff fix-auth
agt --repo /path/to/repo list
```

Stop or roll back work:

```bash
agt --repo /path/to/repo stop fix-auth --reason "human intervention"
agt --repo /path/to/repo resume fix-auth
agt --repo /path/to/repo rollback fix-auth snap-1
```

Finalize or abandon work:

```bash
agt --repo /path/to/repo done fix-auth --msg "agent(fix-auth): fix email validation"
agt --repo /path/to/repo abort fix-auth
```

Open the dashboard:

```bash
agt --repo /path/to/repo dash
```

Agent integration helpers:

```bash
agt --repo /path/to/repo agent preflight fix-auth --json
agt --repo /path/to/repo agent should-stop fix-auth --exit-code
agt --repo /path/to/repo agent checkpoint fix-auth
agt --repo /path/to/repo agent finish fix-auth
```

Initialize and validate repo policy:

```bash
agt --repo /path/to/repo config init
agt --repo /path/to/repo config show
agt --repo /path/to/repo config validate
```

## Command Summary

- `create`: create a worktree, branch, metadata ref, and base ref
- `snapshot`: save the current worktree state as a snapshot commit
- `rollback`: restore a worktree to `base`, `latest`, `~N`, or `snap-N`
- `resume`: clear the stop signal and unlock the worktree
- `done`: create the final branch commit and remove agent refs/worktree
- `abort`: remove the worktree, branch, and refs
- `list`: show all known agents
- `status`: show one agent in detail, including snapshot history and unsnapshotted changes
- `diff`: diff snapshots or current worktree state
- `stop`: write a cooperative stop signal and lock the worktree
- `clean`: remove stale orphaned worktrees and refs
- `agent preflight`: return agent-facing state and policy information
- `agent should-stop`: check cooperative stop state
- `agent checkpoint`: agent-friendly alias for `snapshot`
- `agent finish`: agent-friendly alias for `done`
- `config init`: seed recommended repo-local config without overwriting existing keys
- `config show`: show effective repo policy
- `config validate`: validate known AgentGit config keys
- `dash`: open the terminal dashboard

## Dashboard

Open the terminal dashboard:

```bash
agt --repo /path/to/repo dash
```

Keyboard controls:

- `↑/↓`: move selection
- `→` or `Enter`: open agent detail
- `←/→`: switch panes in detail view
- `d`: open diff for the selected snapshot file
- `f`: open file contents for the selected snapshot file
- `s`: stop the selected active agent
- `u`: resume the selected stopped agent
- `r`: roll back to the selected snapshot
- `D`: finalize the selected agent
- `x`: abort the selected agent
- `y`: confirm the pending action
- `n`: cancel the pending action
- `Esc`: go back
- `q`: quit

All mutating dashboard actions require confirmation before they run.

## Testing

For a fresh machine:

```bash
git clone https://github.com/Ray0907/AgentGit.git
cd AgentGit
go test -count=1 ./...
go build -o ./agt .
```

Basic smoke flow in a temporary repo:

```bash
tmpdir=$(mktemp -d)
repo="$tmpdir/repo"
mkdir -p "$repo"
cd "$repo"

git init
git config user.name "Demo User"
git config user.email "demo@example.com"

printf 'hello v1\n' > app.txt
git add app.txt
git commit -m "initial"

/path/to/agt --repo "$repo" config init
/path/to/agt --repo "$repo" create fix-auth --purpose "fix auth" --owner claude
printf 'hello v2\n' > "$repo/.worktrees/fix-auth/app.txt"
/path/to/agt --repo "$repo" snapshot fix-auth
/path/to/agt --repo "$repo" stop fix-auth
/path/to/agt --repo "$repo" agent should-stop fix-auth --exit-code
/path/to/agt --repo "$repo" resume fix-auth
/path/to/agt --repo "$repo" agent finish fix-auth
```

## Data Model

AgentGit stores state inside git:

- `refs/agents/<id>/meta`
- `refs/agents/<id>/base`
- `refs/agents/<id>/latest`
- `refs/agents/<id>/stop`

Snapshot commits are regular git commit objects linked by parent history. Modified trees are built with an isolated `GIT_INDEX_FILE`, so snapshotting does not mutate the repository index.

Repo policy can be set with git config keys such as:

- `agentgit.cleanHours`
- `agentgit.dashboardRefreshSeconds`
- `agentgit.defaultOwner`
- `agentgit.doneAuthorName`
- `agentgit.doneAuthorEmail`
- `agentgit.doneMessageTemplate`
- `agentgit.snapshotMessageTemplate`
- `agentgit.stopReason`

For shell scripting, `agt agent should-stop --exit-code` returns:

- `0` when work should stop
- `1` when work may continue
- `>1` for actual errors

`agt config init` writes recommended repo-local config only for missing keys. `agt config validate` fails fast on invalid numeric values or empty known templates.

## Current Scope

- local single-user repositories
- git-backed state only
- text-based terminal workflow

Out of scope:

- merge/rebase automation back to main
- agent process management
- web UI
- multi-repo aggregation

## Status

The project builds and tests with Go. Automated tests currently cover the core agent workflow: `create`, `snapshot`, `rollback`, `diff`, `done`, `stop`, `resume`, `agent preflight`, `config init`, and `config validate`.
