package cli

import (
	"agt/internal/app"
	"strings"
	"testing"
)

func TestDashBeginConfirmRollbackUsesSelectedSnapshot(t *testing.T) {
	model := dashModel{
		detail: &app.AgentStatus{
			Summary: app.AgentSummary{ID: "fix-auth", Status: "active"},
			Snapshots: []app.SnapshotInfo{
				{Name: "snap-1", Commit: "abc123"},
			},
		},
	}

	if err := model.beginConfirm(actionRollback); err != nil {
		t.Fatalf("beginConfirm: %v", err)
	}
	if model.confirmAction != actionRollback {
		t.Fatalf("expected rollback confirm action, got %q", model.confirmAction)
	}
	if !strings.Contains(model.confirmPrompt, "snap-1") {
		t.Fatalf("expected prompt to mention selected snapshot, got %q", model.confirmPrompt)
	}
}

func TestDashBeginConfirmStopRejectsStoppedAgent(t *testing.T) {
	model := dashModel{
		detail: &app.AgentStatus{
			Summary: app.AgentSummary{ID: "fix-auth", Status: "stopped"},
		},
	}

	err := model.beginConfirm(actionStop)
	if err == nil {
		t.Fatalf("expected stop confirm to fail for stopped agent")
	}
	if !strings.Contains(err.Error(), "only available for active agents") {
		t.Fatalf("unexpected error: %v", err)
	}
}
