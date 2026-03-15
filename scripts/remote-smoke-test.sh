#!/usr/bin/env bash

set -euo pipefail

AGT_BIN="${AGT_BIN:-/tmp/AgentGit}"
RUN_DASH="${RUN_DASH:-0}"
KEEP_REPO="${KEEP_REPO:-0}"

if [[ ! -x "${AGT_BIN}" ]]; then
  echo "AgentGit binary not found or not executable: ${AGT_BIN}" >&2
  echo "Copy the binary first, for example:" >&2
  echo "  scp ./agt user@host:/tmp/AgentGit" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
repo="${tmpdir}/repo"
cleanup() {
  if [[ "${KEEP_REPO}" == "1" ]]; then
    echo
    echo "keeping repo at ${repo}"
    return
  fi
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

mkdir -p "${repo}"
cd "${repo}"

echo "== Init demo repo =="
git init >/dev/null
git config user.name "Demo User"
git config user.email "demo@example.com"

printf 'hello v1\n' > app.txt
git add app.txt
git commit -m "initial" >/dev/null

echo
echo "== Configure AgentGit policy =="
git config agentgit.defaultOwner "agent-bot"
git config agentgit.doneAuthorName "Agent Bot"
git config agentgit.doneAuthorEmail "agent@example.com"
git config agentgit.doneMessageTemplate "ship {id}: {purpose}"
git config agentgit.snapshotMessageTemplate "snap {id} {timestamp}"
git config agentgit.dashboardRefreshSeconds "4"
"${AGT_BIN}" --repo "${repo}" config show

echo
echo "== Create and inspect agent =="
"${AGT_BIN}" --repo "${repo}" create fix-auth --purpose "fix auth" --owner claude
printf 'hello v2\n' > "${repo}/.worktrees/fix-auth/app.txt"
"${AGT_BIN}" --repo "${repo}" agent preflight fix-auth --json
"${AGT_BIN}" --repo "${repo}" snapshot fix-auth
"${AGT_BIN}" --repo "${repo}" status fix-auth

echo
echo "== should-stop exit code contract =="
set +e
"${AGT_BIN}" --repo "${repo}" agent should-stop fix-auth --exit-code >/dev/null
continue_exit=$?
set -e
echo "continue_exit=${continue_exit} (expected 1)"

"${AGT_BIN}" --repo "${repo}" stop fix-auth
set +e
"${AGT_BIN}" --repo "${repo}" agent should-stop fix-auth --exit-code >/dev/null
stop_exit=$?
set -e
echo "stop_exit=${stop_exit} (expected 0)"

echo
echo "== Create second agent for dashboard =="
"${AGT_BIN}" --repo "${repo}" create add-search --purpose "add search API" --owner cursor
printf 'search\n' > "${repo}/.worktrees/add-search/search.txt"
"${AGT_BIN}" --repo "${repo}" snapshot add-search --msg "snapshot one"
"${AGT_BIN}" --repo "${repo}" list

echo
echo "== Finalize first agent =="
printf 'hello final\n' > "${repo}/.worktrees/fix-auth/app.txt"
"${AGT_BIN}" --repo "${repo}" done fix-auth
git log --oneline --decorate --graph --all

echo
echo "repo=${repo}"

if [[ "${RUN_DASH}" == "1" ]]; then
  echo
  echo "== Launch dashboard =="
  "${AGT_BIN}" --repo "${repo}" dash
fi
