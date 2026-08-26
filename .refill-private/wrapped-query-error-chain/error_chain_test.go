package wrappedqueryerrorchain_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
	"oral-archive-release/internal/httpapi"
)

type wrappedMissingRepository struct{}

func (wrappedMissingRepository) Get(context.Context, string) (*domain.ReleaseCase, error) {
	return nil, fmt.Errorf("读取案件封套: %w", domain.NewError("CASE_NOT_FOUND", "开放案件不存在"))
}

func (wrappedMissingRepository) GetByCredential(context.Context, string) (*domain.ReleaseCase, error) {
	return nil, fmt.Errorf("读取凭据索引: %w", domain.NewError("CREDENTIAL_NOT_FOUND", "访问凭据不存在"))
}

func (wrappedMissingRepository) Save(context.Context, *domain.ReleaseCase, int64, *application.IdempotencyRecord) error {
	return fmt.Errorf("unexpected Save call")
}

func (wrappedMissingRepository) LookupIdempotency(context.Context, string) (*application.IdempotencyRecord, error) {
	return nil, fmt.Errorf("unexpected LookupIdempotency call")
}

type unusedAudit struct{}

func (unusedAudit) Append(context.Context, application.AuditEvent) error {
	return fmt.Errorf("unexpected Append call")
}

func (unusedAudit) Timeline(context.Context, string) ([]application.AuditEvent, error) {
	return nil, fmt.Errorf("unexpected Timeline call")
}

func (unusedAudit) IssueCredential(context.Context, string, string, string, time.Time) (domain.ReleaseCredential, error) {
	return domain.ReleaseCredential{}, fmt.Errorf("unexpected IssueCredential call")
}

func (unusedAudit) Credential(context.Context, string) (domain.ReleaseCredential, error) {
	return domain.ReleaseCredential{}, fmt.Errorf("unexpected Credential call")
}

func (unusedAudit) Verify(context.Context) (bool, []string, error) {
	return false, nil, fmt.Errorf("unexpected Verify call")
}

func (unusedAudit) CredentialSegment(context.Context, uint64, int) ([]domain.ReleaseCredential, error) {
	return nil, fmt.Errorf("unexpected CredentialSegment call")
}

func TestWrappedRepositoryErrorsRetainDomainIdentity(t *testing.T) {
	handler := httpapi.NewHandler(application.NewService(wrappedMissingRepository{}, unusedAudit{}))
	paths := []string{
		"/api/v1/release-cases/missing",
		"/api/v1/release-cases/missing/timeline",
		"/api/v1/release-cases/missing/compliance",
		"/api/v1/release-cases/missing/manifest",
		"/api/v1/release-cases/missing/manifest/recordings/recording-1",
		"/api/v1/release-cases/missing/approval-readiness",
		"/api/v1/release-cases/missing/overview",
	}

	for _, path := range paths {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("GET %s status=%d, want %d; body=%s", path, recorder.Code, http.StatusNotFound, recorder.Body.String())
		}
	}
}
