package auditappendatomicity_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/auditlog"
)

func TestFailedAuditAppendDoesNotPublishUnpersistedEvent(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, "audit")
	manager, err := auditlog.Open(auditDir)
	if err != nil {
		t.Fatalf("open audit manager: %v", err)
	}

	if err := os.Remove(auditDir); err != nil {
		t.Fatalf("remove empty audit directory: %v", err)
	}
	if err := os.WriteFile(auditDir, []byte("resource invalidated"), 0o600); err != nil {
		t.Fatalf("replace audit directory with regular file: %v", err)
	}

	event := application.AuditEvent{
		CaseID:        "case-resource-loss",
		ActorID:       "cataloger-1",
		ActorRole:     application.RoleCataloger,
		ObjectVersion: 1,
		Action:        "CASE_CREATED",
		At:            time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC),
	}
	if err := manager.Append(context.Background(), event); err == nil {
		t.Fatal("expected persistence failure after audit directory invalidation")
	}

	events, err := manager.Timeline(context.Background(), event.CaseID)
	if err != nil {
		t.Fatalf("read in-memory timeline: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("failed append published %d unpersisted event(s): %+v", len(events), events)
	}
}
