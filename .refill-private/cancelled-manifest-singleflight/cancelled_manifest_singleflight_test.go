package cancelled_manifest_singleflight_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
)

func TestCancelledManifestLoadDoesNotPoisonNextRequest(t *testing.T) {
	repo := &cancelThenMissingRepository{firstStarted: make(chan struct{})}
	service := application.NewService(repo, unusedAudit{})

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		_, err := service.Manifest(firstCtx, "case-cancelled-manifest")
		firstResult <- err
	}()

	<-repo.firstStarted
	waiterBase, cancelWaiter := context.WithTimeout(context.Background(), time.Second)
	defer cancelWaiter()
	waiterCtx := &doneObservedContext{Context: waiterBase, observed: make(chan struct{})}
	waiterResult := make(chan error, 1)
	go func() {
		_, err := service.Manifest(waiterCtx, "case-cancelled-manifest")
		waiterResult <- err
	}()
	<-waiterCtx.observed

	cancelFirst()
	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("首次加载应返回 context.Canceled，实际为 %v", err)
	}

	if err := <-waiterResult; errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("取消的首次加载污染了后续请求：第二次调用等待遗留条目并返回 %v", err)
	} else {
		assertCaseNotFound(t, err)
	}

	if calls := repo.callCount(); calls != 2 {
		t.Fatalf("后续请求应重新进入 repository，Get 调用次数=%d", calls)
	}
}

func assertCaseNotFound(t *testing.T, err error) {
	t.Helper()
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) || domainErr.Code != "CASE_NOT_FOUND" {
		t.Fatalf("第二次加载应重新访问 repository 并返回 CASE_NOT_FOUND，实际为 %v", err)
	}
}

type doneObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

type cancelThenMissingRepository struct {
	mu           sync.Mutex
	calls        int
	firstStarted chan struct{}
}

func (r *cancelThenMissingRepository) Get(ctx context.Context, _ string) (*domain.ReleaseCase, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	r.mu.Unlock()
	if call == 1 {
		close(r.firstStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, domain.NewError("CASE_NOT_FOUND", "开放案件不存在")
}

func (r *cancelThenMissingRepository) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (*cancelThenMissingRepository) GetByCredential(context.Context, string) (*domain.ReleaseCase, error) {
	panic("unexpected GetByCredential call")
}

func (*cancelThenMissingRepository) Save(context.Context, *domain.ReleaseCase, int64, *application.IdempotencyRecord) error {
	panic("unexpected Save call")
}

func (*cancelThenMissingRepository) LookupIdempotency(context.Context, string) (*application.IdempotencyRecord, error) {
	panic("unexpected LookupIdempotency call")
}

type unusedAudit struct{}

func (unusedAudit) Append(context.Context, application.AuditEvent) error {
	panic("unexpected Append call")
}

func (unusedAudit) Timeline(context.Context, string) ([]application.AuditEvent, error) {
	panic("unexpected Timeline call")
}

func (unusedAudit) IssueCredential(context.Context, string, string, string, time.Time) (domain.ReleaseCredential, error) {
	panic("unexpected IssueCredential call")
}

func (unusedAudit) Credential(context.Context, string) (domain.ReleaseCredential, error) {
	panic("unexpected Credential call")
}

func (unusedAudit) Verify(context.Context) (bool, []string, error) {
	panic("unexpected Verify call")
}

func (unusedAudit) CredentialSegment(context.Context, uint64, int) ([]domain.ReleaseCredential, error) {
	panic("unexpected CredentialSegment call")
}
