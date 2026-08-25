package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"oral-archive-release/internal/domain"
)

type Service struct {
	repo  Repository
	audit AuditPort
	clock func() time.Time
	ids   func() string
	mu    sync.Mutex
}

func NewService(repo Repository, audit AuditPort) *Service {
	return &Service{repo: repo, audit: audit, clock: time.Now, ids: randomID}
}

func (s *Service) CreateCase(ctx context.Context, cmd CreateCase) (*domain.ReleaseCase, error) {
	if err := requireRole(cmd.ActorID, cmd.ActorRole, RoleCataloger); err != nil {
		return nil, err
	}
	if cmd.IdempotencyKey == "" {
		return nil, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", "写请求必须提供 idempotencyKey")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := payloadHash(cmd)
	if replay, err := s.replay(ctx, cmd.IdempotencyKey, hash); err != nil || replay != nil {
		return replay, err
	}
	now := s.clock().UTC()
	caseID := "case_" + s.ids()
	c, err := domain.NewReleaseCase(caseID, cmd.CollectionID, cmd.Title, cmd.Purpose, cmd.CatalogerID, now)
	if err != nil {
		return nil, err
	}
	record, err := makeIdempotency(cmd.IdempotencyKey, hash, c, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, c, 0, record); err != nil {
		return nil, err
	}
	if err := s.audit.Append(ctx, s.event(c, cmd.ActorID, cmd.ActorRole, "CASE_CREATED", "开放案件已创建", now)); err != nil {
		return nil, fmt.Errorf("写入审计事件: %w", err)
	}
	return c, nil
}

func (s *Service) AddParticipant(ctx context.Context, caseID string, cmd AddParticipant) (*domain.ReleaseCase, error) {
	return s.mutate(ctx, caseID, cmd.Meta, RoleCataloger, "PARTICIPANT_ADDED", cmd, func(c *domain.ReleaseCase, now time.Time) error { return c.AddParticipant(cmd.Participant, now) })
}

func (s *Service) AddRecording(ctx context.Context, caseID string, cmd AddRecording) (*domain.ReleaseCase, error) {
	return s.mutate(ctx, caseID, cmd.Meta, RoleCataloger, "RECORDING_ADDED", cmd, func(c *domain.ReleaseCase, now time.Time) error {
		return c.AddRecording(cmd.Recording, cmd.ActorID, now)
	})
}

func (s *Service) AddConsent(ctx context.Context, caseID string, cmd AddConsent) (*domain.ReleaseCase, error) {
	return s.mutate(ctx, caseID, cmd.Meta, RoleCataloger, "CONSENT_ADDED", cmd, func(c *domain.ReleaseCase, now time.Time) error { return c.AddConsent(cmd.Consent, now) })
}

func (s *Service) ReviseCase(ctx context.Context, caseID string, cmd ReviseCase) (*domain.ReleaseCase, error) {
	var details string
	return s.mutateWithDetails(ctx, caseID, cmd.Meta, RoleCataloger, "CASE_PROFILE_REVISED", cmd, func(c *domain.ReleaseCase, now time.Time) error {
		result, err := c.ReviseDraft(cmd.ActorID, cmd.Revision(), now)
		if err == nil {
			b, _ := json.Marshal(result)
			details = string(b)
		}
		return err
	}, func() string { return details })
}

func (s *Service) CatalogBatch(ctx context.Context, caseID string, cmd CatalogBatch) (CatalogBatchResult, error) {
	var counts domain.BatchCounts
	batch := cmd.Items()
	c, err := s.mutateWithDetails(ctx, caseID, cmd.Meta, RoleCataloger, "CATALOG_BATCH_ADDED", cmd, func(c *domain.ReleaseCase, now time.Time) error {
		var applyErr error
		counts, applyErr = c.ApplyCatalogBatch(batch, cmd.ActorID, now)
		return applyErr
	}, func() string {
		ids := struct{ Participants, Recordings, Consents []string }{}
		for _, v := range batch.Participants {
			ids.Participants = append(ids.Participants, v.ID)
		}
		for _, v := range batch.Recordings {
			ids.Recordings = append(ids.Recordings, v.ID)
		}
		for _, v := range batch.Consents {
			ids.Consents = append(ids.Consents, v.ID)
		}
		return fmt.Sprintf("participants=%d recordings=%d consents=%d objectIdsDigest=%s", counts.Participants, counts.Recordings, counts.Consents, payloadHash(ids))
	})
	if err != nil {
		return CatalogBatchResult{}, err
	}
	if counts == (domain.BatchCounts{}) {
		counts = domain.BatchCounts{Participants: len(batch.Participants), Recordings: len(batch.Recordings), Consents: len(batch.Consents)}
	}
	return CatalogBatchResult{Accepted: counts, Case: c}, nil
}

func (s *Service) SubmitReview(ctx context.Context, caseID string, meta Meta) (*domain.ReleaseCase, error) {
	return s.mutate(ctx, caseID, meta, RoleCataloger, "REVIEW_SUBMITTED", meta, func(c *domain.ReleaseCase, now time.Time) error { return c.SubmitReview(now) })
}

func (s *Service) AddFinding(ctx context.Context, caseID string, cmd AddFinding) (*domain.ReleaseCase, error) {
	return s.mutate(ctx, caseID, cmd.Meta, RoleReviewer, "FINDING_RECORDED", cmd, func(c *domain.ReleaseCase, now time.Time) error { return c.AddFinding(cmd.Finding, now) })
}

func (s *Service) ImportComplianceFindings(ctx context.Context, caseID string, cmd ImportComplianceFindings) (ImportComplianceResult, error) {
	var imported domain.FindingImportResult
	var details string
	c, err := s.mutateWithDetails(ctx, caseID, cmd.Meta, RoleReviewer, "COMPLIANCE_FINDINGS_IMPORTED", cmd, func(c *domain.ReleaseCase, now time.Time) error {
		at := cmd.EvaluateAt
		if at.IsZero() {
			at = now
		}
		sourceVersion := c.Version
		var importErr error
		imported, importErr = c.ImportComplianceFindings(cmd.IssueKeys, at, now)
		if importErr == nil {
			created := map[string]bool{}
			for _, finding := range imported.Created {
				created[finding.ID] = true
			}
			for i := range c.Findings {
				if created[c.Findings[i].ID] {
					c.Findings[i].SourceImportKey = cmd.IdempotencyKey
				}
			}
			for i := range imported.Created {
				imported.Created[i].SourceImportKey = cmd.IdempotencyKey
			}
		}
		details = fmt.Sprintf("sourceVersion=%d created=%d skipped=%d", sourceVersion, len(imported.Created), len(imported.Skipped))
		return importErr
	}, func() string { return details })
	if err != nil {
		return ImportComplianceResult{}, err
	}
	if imported.Created == nil { // 幂等重放时从当前快照按去重键还原稳定分类。
		selected := map[string]bool{}
		for _, key := range cmd.IssueKeys {
			selected[key] = true
		}
		wanted := map[string]bool{}
		for key := range selected {
			parts := strings.SplitN(key, "|", 4)
			if len(parts) == 4 {
				wanted[domain.FindingDedupKeyForIssue(domain.ComplianceIssue{Code: domain.ComplianceCode(parts[0]), RecordingID: parts[1], ParticipantID: parts[2], Topic: parts[3]})] = true
			}
		}
		imported.Created = []domain.ReviewFinding{}
		imported.Skipped = []domain.ReviewFinding{}
		for _, finding := range c.Findings {
			if len(wanted) > 0 && !wanted[finding.DedupKey] {
				continue
			}
			if finding.SourceImportKey == cmd.IdempotencyKey {
				imported.Created = append(imported.Created, finding)
			} else {
				imported.Skipped = append(imported.Skipped, finding)
			}
		}
	}
	return ImportComplianceResult{Created: imported.Created, Skipped: imported.Skipped, Case: c}, nil
}

func (s *Service) AddRevision(ctx context.Context, caseID string, cmd AddRevision) (*domain.ReleaseCase, error) {
	return s.mutate(ctx, caseID, cmd.Meta, RoleCataloger, "REDACTED_REVISION_ADDED", cmd, func(c *domain.ReleaseCase, now time.Time) error {
		cmd.Revision.CreatedBy = cmd.ActorID
		return c.AddRedactedRevision(cmd.RecordingID, cmd.Revision, cmd.Evidence, now)
	})
}

func (s *Service) ReviewFinding(ctx context.Context, caseID string, cmd ReviewFinding) (*domain.ReleaseCase, error) {
	return s.mutate(ctx, caseID, cmd.Meta, RoleReviewer, "FINDING_REVIEWED", cmd, func(c *domain.ReleaseCase, now time.Time) error {
		if cmd.EvidencePackageID != "" || cmd.ReviewOpinion != "" {
			return c.ReviewFindingWithPackage(cmd.FindingID, cmd.EvidencePackageID, cmd.ReviewOpinion, cmd.ActorID, cmd.Accepted, now)
		}
		return c.ReviewFinding(cmd.FindingID, cmd.Evidence, cmd.ActorID, cmd.Accepted, now)
	})
}

func (s *Service) SubmitEvidencePackage(ctx context.Context, caseID string, cmd SubmitEvidencePackage) (*domain.ReleaseCase, error) {
	var packageID string
	return s.mutateWithDetails(ctx, caseID, cmd.Meta, RoleCataloger, "EVIDENCE_PACKAGE_SUBMITTED", cmd, func(c *domain.ReleaseCase, now time.Time) error {
		if c.CatalogerID != cmd.ActorID {
			return domain.NewError("NOT_RESPONSIBLE_CATALOGER", "只有当前责任编目员可以提交整改证据")
		}
		pkg, err := c.SubmitEvidencePackage(cmd.EvidencePackage, cmd.ActorID, now)
		if err == nil {
			packageID = pkg.ID
		}
		return err
	}, func() string { return "evidencePackageId=" + packageID })
}

func (s *Service) Decide(ctx context.Context, caseID string, cmd Decision) (*domain.ReleaseCase, error) {
	action := "CASE_RETURNED"
	if cmd.Approve {
		action = "CASE_APPROVED"
	}
	return s.mutateWithDetails(ctx, caseID, cmd.Meta, RoleOfficer, action, cmd, func(c *domain.ReleaseCase, now time.Time) error {
		if cmd.Approve && cmd.ReadinessDigest != "" {
			readiness, err := c.ApprovalReadiness(now)
			if err != nil {
				return err
			}
			if readiness.ReadinessDigest != cmd.ReadinessDigest {
				return domain.NewError("READINESS_CHANGED", "批准就绪状态已变化，请重新查询")
			}
		}
		if err := c.Decide(cmd.Approve, cmd.ActorID, cmd.Reason, now); err != nil {
			return err
		}
		if c.Decision != nil {
			c.Decision.ReadinessDigest = cmd.ReadinessDigest
		}
		return nil
	}, func() string {
		if cmd.ReadinessDigest == "" {
			return ""
		}
		return "readinessDigest=" + cmd.ReadinessDigest
	})
}

func (s *Service) Freeze(ctx context.Context, caseID string, meta Meta) (*domain.ReleaseCase, error) {
	return s.mutate(ctx, caseID, meta, RoleOfficer, "MANIFEST_FROZEN", meta, func(c *domain.ReleaseCase, now time.Time) error { _, err := c.Freeze(now); return err })
}

func (s *Service) Issue(ctx context.Context, caseID string, cmd IssueCredential) (*domain.ReleaseCase, error) {
	if err := requireRole(cmd.ActorID, cmd.ActorRole, RoleOfficer); err != nil {
		return nil, err
	}
	if err := validateMeta(cmd.Meta); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := payloadHash(cmd)
	if replay, err := s.replay(ctx, cmd.IdempotencyKey, hash); err != nil || replay != nil {
		return replay, err
	}
	c, err := s.repo.Get(ctx, caseID)
	if err != nil {
		return nil, err
	}
	if c.Version != cmd.ExpectedVersion {
		return nil, staleVersion(c.Version)
	}
	if c.Status != domain.StatusFrozen || c.Manifest == nil {
		return nil, domain.NewError("INVALID_STATE", "只有 FROZEN 案件可以签发凭据")
	}
	now := s.clock().UTC()
	credential, err := s.audit.IssueCredential(ctx, c.ID, c.Manifest.Digest, cmd.ActorID, now)
	if err != nil {
		return nil, err
	}
	if err := c.AttachCredential(credential, now); err != nil {
		return nil, err
	}
	record, err := makeIdempotency(cmd.IdempotencyKey, hash, c, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, c, cmd.ExpectedVersion, record); err != nil {
		return nil, err
	}
	if err := s.audit.Append(ctx, s.event(c, cmd.ActorID, cmd.ActorRole, "CREDENTIAL_ISSUED", credential.CredentialNo, now)); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) GetCase(ctx context.Context, id string) (*domain.ReleaseCase, error) {
	return s.repo.Get(ctx, id)
}
func (s *Service) Timeline(ctx context.Context, id string) ([]AuditEvent, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.audit.Timeline(context.WithoutCancel(ctx), id)
}
func (s *Service) Credential(ctx context.Context, no string) (domain.ReleaseCredential, error) {
	return s.audit.Credential(ctx, no)
}

func (s *Service) VerifyCredential(ctx context.Context, no string) (Verification, error) {
	credential, err := s.audit.Credential(ctx, no)
	if err != nil {
		return Verification{}, err
	}
	c, err := s.repo.GetByCredential(context.WithoutCancel(ctx), no)
	if err != nil {
		return Verification{}, err
	}
	manifestValid, problems := c.Verify()
	chainValid, chainProblems, err := s.audit.Verify(context.WithoutCancel(ctx))
	if err != nil {
		return Verification{}, err
	}
	problems = append(problems, chainProblems...)
	if c.Credential == nil || c.Credential.CredentialDigest != credential.CredentialDigest {
		problems = append(problems, "凭据索引与案件快照不一致")
	}
	return Verification{Valid: manifestValid && chainValid && len(problems) == 0, CredentialNo: no, CaseID: c.ID, ManifestDigest: credential.ManifestDigest, CredentialDigest: credential.CredentialDigest, ManifestValid: manifestValid, AuditChainValid: chainValid, Problems: problems, VerifiedAt: s.clock().UTC()}, nil
}

func (s *Service) mutate(ctx context.Context, caseID string, meta Meta, role, action string, payload any, fn func(*domain.ReleaseCase, time.Time) error) (*domain.ReleaseCase, error) {
	return s.mutateWithDetails(ctx, caseID, meta, role, action, payload, fn, func() string { return "" })
}

func (s *Service) mutateWithDetails(ctx context.Context, caseID string, meta Meta, role, action string, payload any, fn func(*domain.ReleaseCase, time.Time) error, details func() string) (*domain.ReleaseCase, error) {
	if err := requireRole(meta.ActorID, meta.ActorRole, role); err != nil {
		return nil, err
	}
	if err := validateMeta(meta); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := payloadHash(payload)
	if replay, err := s.replay(ctx, meta.IdempotencyKey, hash); err != nil || replay != nil {
		return replay, err
	}
	c, err := s.repo.Get(ctx, caseID)
	if err != nil {
		return nil, err
	}
	if c.Version != meta.ExpectedVersion {
		return nil, staleVersion(c.Version)
	}
	now := s.clock().UTC()
	if err := fn(c, now); err != nil {
		return nil, err
	}
	record, err := makeIdempotency(meta.IdempotencyKey, hash, c, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, c, meta.ExpectedVersion, record); err != nil {
		return nil, err
	}
	if err := s.audit.Append(ctx, s.event(c, meta.ActorID, meta.ActorRole, action, details(), now)); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) replay(ctx context.Context, key, hash string) (*domain.ReleaseCase, error) {
	record, err := s.repo.LookupIdempotency(ctx, key)
	if err != nil {
		var d *domain.Error
		if errors.As(err, &d) && d.Code == "IDEMPOTENCY_NOT_FOUND" {
			return nil, nil
		}
		return nil, err
	}
	if record.PayloadHash != hash {
		return nil, domain.NewError("IDEMPOTENCY_CONFLICT", "同一 idempotencyKey 已用于不同请求载荷")
	}
	if len(record.Result) == 0 {
		return nil, domain.NewError("IDEMPOTENCY_RECORD_INVALID", "幂等记录缺少原始响应快照")
	}
	var c domain.ReleaseCase
	if err := json.Unmarshal(record.Result, &c); err != nil {
		return nil, fmt.Errorf("解析幂等响应快照: %w", err)
	}
	if c.ID != record.CaseID || c.Version != record.CaseVersion {
		return nil, domain.NewError("IDEMPOTENCY_RECORD_INVALID", "幂等记录与响应快照不一致")
	}
	return &c, nil
}

func (s *Service) event(c *domain.ReleaseCase, actor, role, action, details string, at time.Time) AuditEvent {
	return AuditEvent{CaseID: c.ID, ActorID: actor, ActorRole: role, ObjectVersion: c.Version, Action: action, Details: details, At: at}
}

func validateMeta(meta Meta) error {
	if meta.ExpectedVersion < 1 {
		return domain.NewError("EXPECTED_VERSION_REQUIRED", "写请求必须提供正整数 expectedVersion")
	}
	if meta.IdempotencyKey == "" {
		return domain.NewError("IDEMPOTENCY_KEY_REQUIRED", "写请求必须提供 idempotencyKey")
	}
	return nil
}

func requireRole(actor, actual, required string) error {
	if actor == "" {
		return domain.NewError("ACTOR_REQUIRED", "写请求必须提供 actorId")
	}
	if actual != required {
		return domain.NewError("FORBIDDEN_ROLE", "当前操作者角色无权执行该操作")
	}
	return nil
}

func staleVersion(actual int64) error {
	return &domain.Error{Code: "VERSION_CONFLICT", Message: "expectedVersion 与当前案件版本不一致", Fields: map[string]string{"currentVersion": fmt.Sprint(actual)}}
}

func payloadHash(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func makeIdempotency(key, hash string, c *domain.ReleaseCase, now time.Time) (*IdempotencyRecord, error) {
	result, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("编码幂等响应快照: %w", err)
	}
	return &IdempotencyRecord{Key: key, PayloadHash: hash, CaseID: c.ID, CaseVersion: c.Version, Result: result, CreatedAt: now}, nil
}
func randomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
