package nested_snapshot_alias_test

import (
	"context"
	"testing"
	"time"

	"oral-archive-release/internal/domain"
	"oral-archive-release/internal/filestore"
)

func TestGetCaseSnapshotOwnsNestedCollections(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := filestore.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
	c, err := domain.NewReleaseCase("case-alias", "collection-1", "口述录音", "research", "cataloger-1", now)
	if err != nil {
		t.Fatalf("new case: %v", err)
	}
	if err := store.Save(ctx, c, 0, nil); err != nil {
		t.Fatalf("save new case: %v", err)
	}
	c, err = store.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("get case for participant: %v", err)
	}
	if err := c.AddParticipant(domain.Participant{ID: "participant-1", IdentityRef: "identity-1"}, now.Add(time.Minute)); err != nil {
		t.Fatalf("add participant: %v", err)
	}
	if err := store.Save(ctx, c, 1, nil); err != nil {
		t.Fatalf("save participant: %v", err)
	}
	c, err = store.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("get case for recording: %v", err)
	}
	if err := c.AddRecording(domain.RecordingItem{
		ID:              "recording-1",
		CatalogRef:      "catalog-ref-1",
		ParticipantIDs:  []string{"participant-1"},
		DurationSeconds: 60,
		LanguageTags:    []string{"zh"},
		ContentDigest:   "sha256:recording-1",
	}, "cataloger-1", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("add recording: %v", err)
	}
	if err := store.Save(ctx, c, 2, nil); err != nil {
		t.Fatalf("save recording: %v", err)
	}

	returned, err := store.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("get caller snapshot: %v", err)
	}
	returned.Recordings[0].ParticipantIDs[0] = "participant-corrupted"
	returned.Recordings[0].Revisions[0].Summary = "caller-corrupted"

	afterMutation, err := store.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("read in-memory case: %v", err)
	}
	if afterMutation.Recordings[0].ParticipantIDs[0] != "participant-1" || afterMutation.Recordings[0].Revisions[0].Summary != "原始编目录音" {
		t.Fatalf("caller mutation leaked into stored snapshot without Save: participant=%q summary=%q version=%d",
			afterMutation.Recordings[0].ParticipantIDs[0], afterMutation.Recordings[0].Revisions[0].Summary, afterMutation.Version)
	}
}
