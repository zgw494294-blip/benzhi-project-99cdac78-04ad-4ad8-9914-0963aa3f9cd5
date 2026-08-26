package orphanedcredentialallocation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
)

type frozenCredentialRepository struct {
	caseSnapshot *domain.ReleaseCase
	saveCalls    int
}

func (r *frozenCredentialRepository) Get(context.Context, string) (*domain.ReleaseCase, error) {
	copy := *r.caseSnapshot
	return &copy, nil
}

func (r *frozenCredentialRepository) GetByCredential(context.Context, string) (*domain.ReleaseCase, error) {
	return nil, domain.NewError("CREDENTIAL_NOT_FOUND", "案件凭据索引中不存在该凭据")
}

func (r *frozenCredentialRepository) Save(context.Context, *domain.ReleaseCase, int64, *application.IdempotencyRecord) error {
	r.saveCalls++
	return nil
}

func (r *frozenCredentialRepository) LookupIdempotency(context.Context, string) (*application.IdempotencyRecord, error) {
	return nil, domain.NewError("IDEMPOTENCY_NOT_FOUND", "幂等记录不存在")
}

type publishingCredentialAudit struct {
	credential domain.ReleaseCredential
}

func (a *publishingCredentialAudit) Append(context.Context, application.AuditEvent) error {
	return nil
}

func (a *publishingCredentialAudit) Timeline(context.Context, string) ([]application.AuditEvent, error) {
	return nil, nil
}

func (a *publishingCredentialAudit) IssueCredential(_ context.Context, caseID, manifestDigest, issuedBy string, at time.Time) (domain.ReleaseCredential, error) {
	credential := domain.ReleaseCredential{
		CredentialNo:   "OAR-0000000001",
		CaseID:         caseID,
		ManifestDigest: manifestDigest,
		Sequence:       1,
		IssuedBy:       issuedBy,
		IssuedAt:       at.UTC(),
	}
	digest, err := domain.CredentialDigest(credential)
	if err != nil {
		return domain.ReleaseCredential{}, err
	}
	credential.CredentialDigest = digest
	a.credential = credential
	return credential, nil
}

func (a *publishingCredentialAudit) Credential(_ context.Context, no string) (domain.ReleaseCredential, error) {
	if a.credential.CredentialNo != no {
		return domain.ReleaseCredential{}, domain.NewError("CREDENTIAL_NOT_FOUND", "访问凭据不存在")
	}
	return a.credential, nil
}

func (a *publishingCredentialAudit) Verify(context.Context) (bool, []string, error) {
	return true, nil, nil
}

func (a *publishingCredentialAudit) CredentialSegment(context.Context, uint64, int) ([]domain.ReleaseCredential, error) {
	return nil, nil
}

func TestStaleIssuanceDoesNotPublishCredential(t *testing.T) {
	issuedAt := time.Date(2026, 8, 25, 9, 30, 0, 0, time.UTC)
	manifest := &domain.ReleaseManifest{
		CaseID:      "case-atomic-credential",
		CaseVersion: 7,
		CreatedAt:   issuedAt.Add(-time.Hour),
		Digest:      "frozen-manifest-digest",
	}
	repo := &frozenCredentialRepository{caseSnapshot: &domain.ReleaseCase{
		ID:         manifest.CaseID,
		Status:     domain.StatusFrozen,
		Version:    7,
		Manifest:   manifest,
		Credential: nil,
	}}
	audit := &publishingCredentialAudit{}
	service := application.NewService(repo, audit)

	_, err := service.Issue(context.Background(), manifest.CaseID, application.IssueCredential{Meta: application.Meta{
		ActorID:         "officer-1",
		ActorRole:       application.RoleOfficer,
		ExpectedVersion: 6,
		IdempotencyKey:  "issue-atomicity-1",
	}})
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) || domainErr.Code != "VERSION_CONFLICT" {
		t.Fatalf("陈旧签发应返回 VERSION_CONFLICT，实际为 %v", err)
	}
	if repo.saveCalls != 0 {
		t.Fatalf("陈旧签发不应进入案件保存，实际调用 %d 次", repo.saveCalls)
	}
	if _, err := repo.GetByCredential(context.Background(), "OAR-0000000001"); err == nil {
		t.Fatal("失败提交不应建立案件凭据索引")
	}
	if credential, err := service.Credential(context.Background(), "OAR-0000000001"); err == nil {
		t.Fatalf("陈旧签发失败后不应发布凭据，实际仍可查询 credentialNo=%s caseId=%s", credential.CredentialNo, credential.CaseID)
	}
}
