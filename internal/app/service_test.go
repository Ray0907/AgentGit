package app

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseDiffStat(t *testing.T) {
	stat := parseDiffStat(" 3 files changed, 22 insertions(+), 5 deletions(-)")
	if stat.Files != 3 {
		t.Fatalf("expected 3 files, got %d", stat.Files)
	}
	if stat.Insertions != 22 {
		t.Fatalf("expected 22 insertions, got %d", stat.Insertions)
	}
	if stat.Deletions != 5 {
		t.Fatalf("expected 5 deletions, got %d", stat.Deletions)
	}
}

func TestNormalizePathForContent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "src/auth.ts", want: "src/auth.ts"},
		{input: "old/auth.ts -> new/auth.ts", want: "new/auth.ts"},
		{input: "  old/a.go -> new/a.go  ", want: "new/a.go"},
	}

	for _, tc := range tests {
		if got := normalizePathForContent(tc.input); got != tc.want {
			t.Fatalf("normalizePathForContent(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseLogSnapshots(t *testing.T) {
	// Simulates output of: git log --first-parent --format="%x01%H%x00%P%x00%cI%x00%s" --name-status
	out := "\x01abc123\x00parent1\x00" + "2025-03-01T10:00:00Z" + "\x00snapshot 2\n" +
		"M\tapp.txt\n" +
		"A\tnew.txt\n" +
		"\n" +
		"\x01def456\x00base000\x00" + "2025-03-01T09:00:00Z" + "\x00snapshot 1\n" +
		"A\tapp.txt"

	snaps := parseLogSnapshots(out)
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
	// Newest first (snap-2), oldest last (snap-1).
	if snaps[0].Name != "snap-2" || snaps[0].Commit != "abc123" {
		t.Fatalf("unexpected first snapshot: %+v", snaps[0])
	}
	if snaps[0].Parent != "parent1" {
		t.Fatalf("unexpected parent: %q", snaps[0].Parent)
	}
	if len(snaps[0].Changes) != 2 {
		t.Fatalf("expected 2 changes in snap-2, got %d", len(snaps[0].Changes))
	}
	if snaps[1].Name != "snap-1" || snaps[1].Commit != "def456" {
		t.Fatalf("unexpected second snapshot: %+v", snaps[1])
	}
	if len(snaps[1].Changes) != 1 {
		t.Fatalf("expected 1 change in snap-1, got %d", len(snaps[1].Changes))
	}
}

func TestParseLogSnapshotsEmpty(t *testing.T) {
	snaps := parseLogSnapshots("")
	if snaps != nil {
		t.Fatalf("expected nil for empty input, got %v", snaps)
	}
	snaps = parseLogSnapshots("  \n  ")
	if snaps != nil {
		t.Fatalf("expected nil for whitespace input, got %v", snaps)
	}
}

func TestParsePorcelainChanges(t *testing.T) {
	out := " M app.txt\nA  new.txt\nR  old.txt -> renamed.txt\n?? tmp.log\n"
	changes := parsePorcelainChanges(out)
	if len(changes) != 4 {
		t.Fatalf("expected 4 changes, got %d", len(changes))
	}
	if changes[0].Status != "M" || changes[0].Path != "app.txt" {
		t.Fatalf("unexpected first change: %+v", changes[0])
	}
	if changes[2].Path != "renamed.txt" {
		t.Fatalf("unexpected rename target: %+v", changes[2])
	}
	if changes[3].Status != "??" {
		t.Fatalf("unexpected untracked status: %+v", changes[3])
	}
}

func TestParseBatchCatFile(t *testing.T) {
	// Simulates output of git cat-file --batch with two objects
	sha1 := "aaaa"
	sha2 := "bbbb"
	content1 := `{"id":"test","purpose":"fix"}`
	content2 := `{"reason":"stop"}`
	out := fmt.Sprintf("%s blob %d\n%s\n%s blob %d\n%s\n",
		sha1, len(content1), content1,
		sha2, len(content2), content2)

	result := parseBatchCatFile(out, []string{sha1, sha2})
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[sha1] != content1 {
		t.Fatalf("sha1 content mismatch: got %q", result[sha1])
	}
	if result[sha2] != content2 {
		t.Fatalf("sha2 content mismatch: got %q", result[sha2])
	}
}

func TestParseBatchCatFileSingle(t *testing.T) {
	sha := "cccc"
	content := `{"key":"value"}`
	out := fmt.Sprintf("%s blob %d\n%s\n", sha, len(content), content)

	result := parseBatchCatFile(out, []string{sha})
	if result[sha] != content {
		t.Fatalf("content mismatch: got %q", result[sha])
	}
}

func TestParseMergeTreeConflicts(t *testing.T) {
	out := "abc123\nCONFLICT (content): Merge conflict in src/auth.go\nCONFLICT (content): Merge conflict in src/util.go\n"
	conflicts := parseMergeTreeConflicts(out)
	if len(conflicts) != 2 {
		t.Fatalf("expected 2 conflicts, got %d", len(conflicts))
	}
	if !strings.Contains(conflicts[0], "auth.go") {
		t.Fatalf("expected auth.go in first conflict: %s", conflicts[0])
	}
}
