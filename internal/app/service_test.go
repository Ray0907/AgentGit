package app

import "testing"

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
