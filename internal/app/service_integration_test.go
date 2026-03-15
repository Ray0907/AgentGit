package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceCreateSnapshotDone(t *testing.T) {
	repo := initTestRepo(t)
	svc, err := NewService(repo)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	summary, err := svc.Create(CreateOptions{
		ID:      "fix-auth",
		Purpose: "fix email validation",
		Owner:   "claude",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if summary.Branch != "agent/fix-auth" {
		t.Fatalf("unexpected branch: %s", summary.Branch)
	}

	writeFile(t, filepath.Join(summary.Path, "app.txt"), "hello v2\n")
	writeFile(t, filepath.Join(summary.Path, "new.txt"), "new file\n")

	snap, err := svc.Snapshot("fix-auth", "snapshot one")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !snap.Created {
		t.Fatalf("expected snapshot to be created")
	}

	status, err := svc.Status("fix-auth")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(status.Snapshots))
	}
	if len(status.CurrentChanges) != 0 {
		t.Fatalf("expected no unsnapshotted changes right after snapshot, got %+v", status.CurrentChanges)
	}

	result, err := svc.Done("fix-auth", DoneOptions{Message: "agent(fix-auth): finalize"})
	if err != nil {
		t.Fatalf("Done: %v", err)
	}
	if strings.TrimSpace(result.Commit) == "" {
		t.Fatalf("expected final commit")
	}

	branchHead := runGit(t, repo, "rev-parse", "agent/fix-auth")
	if branchHead != result.Commit {
		t.Fatalf("branch head %s does not match final commit %s", branchHead, result.Commit)
	}

	content := runGitRaw(t, repo, "show", "agent/fix-auth:app.txt")
	if content != "hello v2\n" {
		t.Fatalf("unexpected committed content: %q", content)
	}

	if _, err := os.Stat(summary.Path); !os.IsNotExist(err) {
		t.Fatalf("expected worktree path to be removed, stat err=%v", err)
	}

	refOut := runGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/agents")
	if strings.TrimSpace(refOut) != "" {
		t.Fatalf("expected agent refs to be cleaned, got %q", refOut)
	}
}

func TestServiceRollbackToPreviousSnapshot(t *testing.T) {
	repo := initTestRepo(t)
	svc, err := NewService(repo)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	summary, err := svc.Create(CreateOptions{ID: "fix-auth"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	writeFile(t, filepath.Join(summary.Path, "app.txt"), "hello v2\n")
	if _, err := svc.Snapshot("fix-auth", "snapshot one"); err != nil {
		t.Fatalf("Snapshot one: %v", err)
	}

	writeFile(t, filepath.Join(summary.Path, "app.txt"), "hello v3\n")
	if _, err := svc.Snapshot("fix-auth", "snapshot two"); err != nil {
		t.Fatalf("Snapshot two: %v", err)
	}

	rollback, err := svc.Rollback("fix-auth", "snap-1", "test rollback")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if strings.TrimSpace(rollback.Commit) == "" {
		t.Fatalf("expected rollback commit")
	}

	data, err := os.ReadFile(filepath.Join(summary.Path, "app.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello v2\n" {
		t.Fatalf("unexpected file content after rollback: %q", string(data))
	}

	status, err := svc.Status("fix-auth")
	if err != nil {
		t.Fatalf("Status after rollback: %v", err)
	}
	if len(status.Snapshots) != 1 {
		t.Fatalf("expected snapshot history to end at first snapshot, got %d", len(status.Snapshots))
	}
}

func TestServiceDiffFallsBackToBaseBeforeFirstSnapshot(t *testing.T) {
	repo := initTestRepo(t)
	svc, err := NewService(repo)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	summary, err := svc.Create(CreateOptions{ID: "fix-auth"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	writeFile(t, filepath.Join(summary.Path, "app.txt"), "hello v2\n")
	diff, err := svc.Diff("fix-auth", "", "")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "-hello v1") || !strings.Contains(diff, "+hello v2") {
		t.Fatalf("unexpected diff output: %s", diff)
	}
}

func TestServiceDiffCurrentAgainstSnapshot(t *testing.T) {
	repo := initTestRepo(t)
	svc, err := NewService(repo)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	summary, err := svc.Create(CreateOptions{ID: "fix-auth"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	writeFile(t, filepath.Join(summary.Path, "app.txt"), "hello v2\n")
	writeFile(t, filepath.Join(summary.Path, "new.txt"), "new file\n")
	if _, err := svc.Snapshot("fix-auth", "snapshot one"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	diff, err := svc.Diff("fix-auth", "snap-1", "current")
	if err != nil {
		t.Fatalf("Diff snap-1 current: %v", err)
	}
	if strings.TrimSpace(diff) != "" {
		t.Fatalf("expected empty diff right after snapshot, got: %s", diff)
	}

	writeFile(t, filepath.Join(summary.Path, "app.txt"), "hello v3\n")
	diff, err = svc.Diff("fix-auth", "snap-1", "current")
	if err != nil {
		t.Fatalf("Diff after edit: %v", err)
	}
	if !strings.Contains(diff, "-hello v2") || !strings.Contains(diff, "+hello v3") {
		t.Fatalf("unexpected diff after edit: %s", diff)
	}
}

func TestServiceRollbackBaseClearsLatestRef(t *testing.T) {
	repo := initTestRepo(t)
	svc, err := NewService(repo)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	summary, err := svc.Create(CreateOptions{ID: "fix-auth"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	writeFile(t, filepath.Join(summary.Path, "app.txt"), "hello v2\n")
	if _, err := svc.Snapshot("fix-auth", "snapshot one"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if _, err := svc.Rollback("fix-auth", "base", "reset to base"); err != nil {
		t.Fatalf("Rollback base: %v", err)
	}

	status, err := svc.Status("fix-auth")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Latest != "" {
		t.Fatalf("expected latest ref to be cleared after rollback to base, got %s", status.Latest)
	}
	if len(status.Snapshots) != 0 {
		t.Fatalf("expected 0 snapshots after rollback to base, got %d", len(status.Snapshots))
	}
}

func TestServiceCreateFailsWhenPreservedBranchExists(t *testing.T) {
	repo := initTestRepo(t)
	svc, err := NewService(repo)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	summary, err := svc.Create(CreateOptions{ID: "fix-auth"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	writeFile(t, filepath.Join(summary.Path, "app.txt"), "hello v2\n")
	if _, err := svc.Done("fix-auth", DoneOptions{Message: "done"}); err != nil {
		t.Fatalf("Done: %v", err)
	}

	_, err = svc.Create(CreateOptions{ID: "fix-auth"})
	if err == nil {
		t.Fatalf("expected create to fail when preserved branch exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "config", "user.email", "test@example.com")
	writeFile(t, filepath.Join(repo, "app.txt"), "hello v1\n")
	runGit(t, repo, "add", "app.txt")
	runGit(t, repo, "commit", "-m", "initial commit")
	return repo
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func runGitRaw(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
