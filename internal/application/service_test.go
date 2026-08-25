package application_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/auditlog"
	"oral-archive-release/internal/domain"
	"oral-archive-release/internal/filestore"
)

func TestOptimisticConcurrencyAndIdempotencyReplay(t *testing.T) {
	dir := t.TempDir()
	repo, err := filestore.Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	audit, err := auditlog.Open(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewService(repo, audit)
	ctx := context.Background()
	create := application.CreateCase{ActorID: "cat", ActorRole: application.RoleCataloger, IdempotencyKey: "create-1", CollectionID: "collection", Title: "案件", Purpose: "research", CatalogerID: "cat"}
	c, err := service.CreateCase(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	firstVersion := c.Version
	replayed, err := service.CreateCase(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != c.ID || replayed.Version != firstVersion {
		t.Fatalf("bad replay: %+v", replayed)
	}
	cmd := application.AddParticipant{Meta: application.Meta{ActorID: "cat", ActorRole: application.RoleCataloger, ExpectedVersion: c.Version, IdempotencyKey: "participant-1"}, Participant: domain.Participant{ID: "p1", IdentityRef: "vault://p1"}}
	c, err = service.AddParticipant(ctx, c.ID, cmd)
	if err != nil {
		t.Fatal(err)
	}
	stale := application.AddParticipant{Meta: application.Meta{ActorID: "cat", ActorRole: application.RoleCataloger, ExpectedVersion: firstVersion, IdempotencyKey: "participant-2"}, Participant: domain.Participant{ID: "p2", IdentityRef: "vault://p2"}}
	_, err = service.AddParticipant(ctx, c.ID, stale)
	var d *domain.Error
	if !errors.As(err, &d) || d.Code != "VERSION_CONFLICT" {
		t.Fatalf("expected version conflict, got %v", err)
	}
	replayed, err = service.CreateCase(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Version != firstVersion {
		t.Fatalf("expected original result version %d, got %d", firstVersion, replayed.Version)
	}
	conflict := create
	conflict.Title = "不同载荷"
	_, err = service.CreateCase(ctx, conflict)
	if !errors.As(err, &d) || d.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}
}

func TestStoreRecoversStateAndAudit(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	auditDir := filepath.Join(dir, "audit")
	repo, _ := filestore.Open(storeDir)
	audit, _ := auditlog.Open(auditDir)
	service := application.NewService(repo, audit)
	c, err := service.CreateCase(context.Background(), application.CreateCase{ActorID: "cat", ActorRole: application.RoleCataloger, IdempotencyKey: "create", CollectionID: "collection", Title: "案件", Purpose: "research", CatalogerID: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := filestore.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Version != c.Version || recovered.Title != c.Title {
		t.Fatalf("recovered case differs: %+v", recovered)
	}
	reopenedAudit, err := auditlog.Open(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	events, err := reopenedAudit.Timeline(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "CASE_CREATED" {
		t.Fatalf("unexpected events: %+v", events)
	}
}
