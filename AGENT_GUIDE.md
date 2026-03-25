# Agent Integration Guide

This document is a self-contained protocol for AI coding agents using the `agt` CLI for git-based checkpointing. It can be embedded directly into an agent's system prompt.

The agent does **not** need to understand git internals. `agt` handles worktree isolation, snapshot commits, ref management, and cleanup.

## Quick Reference

All commands use `--json` for machine-readable output. If the repo is not the current directory, add `--repo /path/to/repo` to every command.

| Phase | Command | When |
|-------|---------|------|
| Startup | `agt agent preflight <ID> --json` | Once at start of work |
| Poll stop | `agt agent should-stop <ID> --json` | Before each edit batch |
| Checkpoint | `agt agent checkpoint <ID> --json --msg "..."` | After each logical change |
| Finish | `agt agent finish <ID> --json --msg "..."` | When task is complete |

## Work Loop Protocol

```
1. PREFLIGHT
   Run: agt agent preflight <ID> --json
   Parse the JSON response.
   Extract "path" — this is your worktree directory. ALL file edits happen here.
   If "should_stop" is true or "locked" is true: STOP immediately. Do not edit.

2. WORK LOOP (repeat until task is complete)

   a. CHECK STOP SIGNAL
      Run: agt agent should-stop <ID> --json
      If "should_stop" is true:
        → Checkpoint any pending work (step 2c)
        → STOP. Do NOT call finish. A human will decide next steps.

   b. EDIT FILES
      Make changes inside the worktree path from step 1.
      Work in focused batches — one logical unit per checkpoint.
      A logical unit is: one function, one test, one config change, one bug fix.

   c. CHECKPOINT
      Run: agt agent checkpoint <ID> --json --msg "<describe what changed>"
      If "created" is false: no files changed since last checkpoint. Continue working.
      If "created" is true: snapshot saved. Continue to next edit batch.

3. FINISH
   When the task is fully complete:
   Run: agt agent finish <ID> --json --msg "<summary of all work done>"
   This creates a final commit on the agent branch and cleans up the worktree.
```

### Autonomy Rules

- **Checkpoint frequently.** After every logical unit of work. Do not batch many changes into one checkpoint.
- **Check stop signal before each edit batch**, not after.
- **Never ask for human confirmation.** Never pause for input. The stop signal is the human communication channel.
- **If stop signal fires:** checkpoint pending work first, then halt. Do NOT call finish — the human may want to roll back or inspect.
- **If checkpoint returns `"created": false`:** nothing changed on disk. This is normal. Continue working.
- **If a command fails (non-zero exit):** retry once. If it fails again, stop gracefully.

## JSON Output Reference

### `agent preflight --json`

```json
{
  "id": "fix-auth",
  "path": "/repo/.worktrees/fix-auth",
  "branch": "agent/fix-auth",
  "base": "abc1234",
  "latest": "def5678",
  "locked": false,
  "should_stop": false,
  "stop_reason": "",
  "current_changes": 2,
  "current_paths": ["src/auth.go", "src/auth_test.go"],
  "snapshot_count": 3,
  "done_author_name": "claude",
  "done_author_email": "",
  "done_message_preview": "agent(fix-auth): fix email validation",
  "snapshot_message_preview": "snapshot(fix-auth): 2026-03-25T10:00:00Z",
  "default_owner": "",
  "refresh_seconds": 2,
  "clean_threshold_hours": 24
}
```

Key fields for agents:
- `path` — the worktree directory; all edits go here
- `should_stop` — if true, do not start new work
- `locked` — if true, worktree is locked; do not edit
- `current_changes` — number of files changed since last checkpoint
- `snapshot_count` — total checkpoints taken so far

### `agent should-stop --json`

No stop signal:
```json
{
  "id": "fix-auth",
  "should_stop": false
}
```

Stop signal present:
```json
{
  "id": "fix-auth",
  "should_stop": true,
  "reason": "budget exceeded"
}
```

### `agent checkpoint --json`

Checkpoint created:
```json
{
  "id": "fix-auth",
  "created": true,
  "commit": "a1b2c3d4e5f6...",
  "snapshot": {
    "name": "snap-4",
    "commit": "a1b2c3d4e5f6...",
    "parent": "f6e5d4c3b2a1...",
    "timestamp": "2026-03-25T10:05:00Z",
    "message": "add email format validator",
    "changes": [
      {"path": "src/auth.go", "status": "M"},
      {"path": "src/auth_test.go", "status": "A"}
    ]
  }
}
```

No changes to checkpoint:
```json
{
  "id": "fix-auth",
  "created": false
}
```

### `agent finish --json`

```json
{
  "id": "fix-auth",
  "branch": "agent/fix-auth",
  "commit": "b2c3d4e5f6a1...",
  "message": "fix email validation with RFC 5322 compliance"
}
```

## Edge Cases

| Situation | What to do |
|-----------|------------|
| `preflight` shows `locked: true` | Do not edit files. Stop and report. |
| `checkpoint` returns `created: false` | Not an error — no files changed. Continue working. |
| Command exits non-zero | Retry once. If still failing, stop gracefully. |
| `should-stop` returns `true` | Checkpoint pending work, then stop. Do NOT call `finish`. |
| Agent's changes broke something | Use `agt rollback <ID> snap-N --reason "..."` to revert to a previous checkpoint, then try a different approach. |
| `finish` returns empty `commit` | No changes were made since the base. Worktree was cleaned up. |

## System Prompt Snippet

Copy this block into your agent's system prompt. Replace `{AGENT_ID}` and `{REPO_PATH}` with actual values.

````markdown
## Git Checkpointing Protocol (AgentGit)

You have access to `agt` for git-based checkpointing. Your agent ID is `{AGENT_ID}`. Your repo is at `{REPO_PATH}`.

### Commands
- Preflight: `agt --repo {REPO_PATH} agent preflight {AGENT_ID} --json`
- Stop check: `agt --repo {REPO_PATH} agent should-stop {AGENT_ID} --json`
- Checkpoint: `agt --repo {REPO_PATH} agent checkpoint {AGENT_ID} --json --msg "..."`
- Finish: `agt --repo {REPO_PATH} agent finish {AGENT_ID} --json --msg "..."`

### Protocol
1. Run **preflight** at start. Extract `path` from JSON — this is your worktree. All edits go here.
   If `should_stop` or `locked` is true, stop immediately.
2. Before each edit batch, run **should-stop**. If `should_stop` is true, checkpoint pending work and halt.
   Do NOT call finish when stopped — a human will decide next steps.
3. After each logical change (one function, one test, one fix), run **checkpoint** with a descriptive `--msg`.
   If `created` is false, nothing changed — continue working.
4. When all work is complete, run **finish** with a summary `--msg`.

### Rules
- All file edits happen inside the worktree path from preflight.
- Checkpoint frequently — after every function, test, or config change.
- Never ask for human confirmation. The stop signal is the communication channel.
- If a command fails, retry once, then stop gracefully.
- Parse all output as JSON. Always check `should_stop` and `created` fields.
````

## Why Git is Perfect for Agent Checkpointing

AgentGit leverages git plumbing commands that humans rarely use but are ideal for AI agents:

| Git Feature | Why It Matters for Agents |
|---|---|
| `git worktree` | Each agent gets an isolated working directory. Multiple agents can work on the same repo concurrently without conflicts. |
| `git commit-tree` | Creates snapshot commits without checkout or index pollution. Pure data operation. |
| `GIT_INDEX_FILE` (env var) | Builds file trees using a temporary index, never touching the real repo index. Zero side effects. |
| `git update-ref` | Atomic ref updates for state transitions. No branch switching, no race conditions. |
| `git hash-object` | Stores arbitrary data (JSON metadata, stop signals) as git objects. The repo IS the database. |
| `git for-each-ref` | Reads all agent refs in a single call. Structured, parseable output. |
| `git merge-tree` | Previews merge conflicts without touching the worktree. Agents can check before finishing. |
| `git diff-tree` | Compares trees at plumbing level without generating full patches. Fast change detection. |
| `git sparse-checkout` | Limits the worktree to only relevant paths. Reduces noise for focused agents. |
| `git bisect` | Binary search for bugs. An AI agent can automate the entire bisect loop programmatically. |

The key insight: git's plumbing layer was designed for scripts and tools, not humans. AI agents are the ultimate scriptable consumer. Features like isolated indices, atomic ref updates, and tree-level operations that feel awkward for humans are natural for agents that think in structured data.

## Shell Wrapper Example

A minimal bash loop demonstrating the protocol mechanically:

```bash
#!/usr/bin/env bash
set -euo pipefail

REPO="$1"
ID="$2"

# 1. Preflight
preflight=$(agt --repo "$REPO" agent preflight "$ID" --json)
worktree=$(echo "$preflight" | jq -r '.path')
if [ "$(echo "$preflight" | jq -r '.should_stop')" = "true" ]; then
  echo "Stop signal active at startup" >&2
  exit 0
fi

echo "Working in: $worktree"

# 2. Work loop
while true; do
  # Check stop signal
  stop=$(agt --repo "$REPO" agent should-stop "$ID" --json | jq -r '.should_stop')
  if [ "$stop" = "true" ]; then
    agt --repo "$REPO" agent checkpoint "$ID" --json --msg "checkpoint before stop" || true
    echo "Stopped by signal" >&2
    exit 0
  fi

  # === Agent edits files in $worktree here ===

  # Checkpoint
  result=$(agt --repo "$REPO" agent checkpoint "$ID" --json --msg "batch complete")
  created=$(echo "$result" | jq -r '.created')
  if [ "$created" = "true" ]; then
    snap=$(echo "$result" | jq -r '.snapshot.name')
    echo "Saved: $snap"
  fi

  # Break when task is done (agent decides)
  # break
done

# 3. Finish
agt --repo "$REPO" agent finish "$ID" --json --msg "task complete"
```
