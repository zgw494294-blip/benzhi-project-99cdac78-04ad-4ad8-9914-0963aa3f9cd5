package overview_shared_result_race_test

import (
	"context"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
)

type coordinatedRepository struct {
	caseSnapshot *domain.ReleaseCase
	entered      chan<- struct{}
	release      <-chan struct{}
}

func (r *coordinatedRepository) Get(context.Context, string) (*domain.ReleaseCase, error) {
	r.entered <- struct{}{}
	<-r.release
	return r.caseSnapshot, nil
}

func (*coordinatedRepository) GetByCredential(context.Context, string) (*domain.ReleaseCase, error) {
	panic("unexpected GetByCredential")
}

func (*coordinatedRepository) Save(context.Context, *domain.ReleaseCase, int64, *application.IdempotencyRecord) error {
	panic("unexpected Save")
}

func (*coordinatedRepository) LookupIdempotency(context.Context, string) (*application.IdempotencyRecord, error) {
	panic("unexpected LookupIdempotency")
}

type coordinatedAudit struct {
	entered chan<- struct{}
	release <-chan struct{}
}

func (*coordinatedAudit) Append(context.Context, application.AuditEvent) error {
	panic("unexpected Append")
}

func (a *coordinatedAudit) Timeline(context.Context, string) ([]application.AuditEvent, error) {
	a.entered <- struct{}{}
	<-a.release
	return []application.AuditEvent{{Sequence: 1, CaseID: "case-overview", Action: "CASE_CREATED"}}, nil
}

func (*coordinatedAudit) IssueCredential(context.Context, string, string, string, time.Time) (domain.ReleaseCredential, error) {
	panic("unexpected IssueCredential")
}

func (*coordinatedAudit) Credential(context.Context, string) (domain.ReleaseCredential, error) {
	panic("unexpected Credential")
}

func (*coordinatedAudit) Verify(context.Context) (bool, []string, error) {
	panic("unexpected Verify")
}

func (*coordinatedAudit) CredentialSegment(context.Context, uint64, int) ([]domain.ReleaseCredential, error) {
	panic("unexpected CredentialSegment")
}

func TestOverviewParallelLoadsSynchronizeResults(t *testing.T) {
	createdAt := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	caseSnapshot, err := domain.NewReleaseCase("case-overview", "collection-1", "并行聚合查询", "research", "cataloger-1", createdAt)
	if err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	repo := &coordinatedRepository{caseSnapshot: caseSnapshot, entered: entered, release: release}
	audit := &coordinatedAudit{entered: entered, release: release}
	service := application.NewService(repo, audit)

	result := make(chan error, 1)
	go func() {
		_, overviewErr := service.Overview(context.Background(), caseSnapshot.ID)
		result <- overviewErr
	}()

	<-entered
	<-entered
	close(release)
	if overviewErr := <-result; overviewErr != nil {
		t.Fatalf("Overview 返回意外错误: %v", overviewErr)
	}
}
