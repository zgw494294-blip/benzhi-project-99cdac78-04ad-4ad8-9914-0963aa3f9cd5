package detached_query_cancellation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
)

type cancelingRepository struct {
	cancel context.CancelFunc
}

func (r cancelingRepository) Get(context.Context, string) (*domain.ReleaseCase, error) {
	r.cancel()
	return &domain.ReleaseCase{ID: "case-cancel", Version: 1}, nil
}

func (cancelingRepository) GetByCredential(context.Context, string) (*domain.ReleaseCase, error) {
	return nil, errors.New("unexpected GetByCredential call")
}

func (cancelingRepository) Save(context.Context, *domain.ReleaseCase, int64, *application.IdempotencyRecord) error {
	return errors.New("unexpected Save call")
}

func (cancelingRepository) LookupIdempotency(context.Context, string) (*application.IdempotencyRecord, error) {
	return nil, errors.New("unexpected LookupIdempotency call")
}

type cancellationAwareAudit struct{}

func (cancellationAwareAudit) Timeline(ctx context.Context, _ string) ([]application.AuditEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []application.AuditEvent{{Action: "SHOULD_NOT_BE_RETURNED"}}, nil
}

func (cancellationAwareAudit) Append(context.Context, application.AuditEvent) error {
	return errors.New("unexpected Append call")
}

func (cancellationAwareAudit) IssueCredential(context.Context, string, string, string, time.Time) (domain.ReleaseCredential, error) {
	return domain.ReleaseCredential{}, errors.New("unexpected IssueCredential call")
}

func (cancellationAwareAudit) Credential(context.Context, string) (domain.ReleaseCredential, error) {
	return domain.ReleaseCredential{}, errors.New("unexpected Credential call")
}

func (cancellationAwareAudit) Verify(context.Context) (bool, []string, error) {
	return false, nil, errors.New("unexpected Verify call")
}

func (cancellationAwareAudit) CredentialSegment(context.Context, uint64, int) ([]domain.ReleaseCredential, error) {
	return nil, errors.New("unexpected CredentialSegment call")
}

func TestTimelinePropagatesCancellationBetweenReadStages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := application.NewService(cancelingRepository{cancel: cancel}, cancellationAwareAudit{})

	events, err := service.Timeline(ctx, "case-cancel")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation after repository stage, got events=%v err=%v", events, err)
	}
}
