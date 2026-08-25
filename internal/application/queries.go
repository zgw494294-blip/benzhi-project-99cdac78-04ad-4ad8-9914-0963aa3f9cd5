package application

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"oral-archive-release/internal/domain"
)

type CaseOverview struct {
	Case       *domain.ReleaseCase     `json:"case"`
	Compliance domain.ComplianceReport `json:"compliance"`
	Timeline   []AuditEvent            `json:"timeline"`
}

type manifestLoad struct {
	done    chan struct{}
	encoded []byte
	err     error
}

func (s *Service) Compliance(ctx context.Context, caseID string) (domain.ComplianceReport, error) {
	return s.ComplianceAt(ctx, caseID, ComplianceQuery{})
}

func (s *Service) ComplianceAt(ctx context.Context, caseID string, query ComplianceQuery) (domain.ComplianceReport, error) {
	c, err := s.repo.Get(ctx, caseID)
	if err != nil {
		return domain.ComplianceReport{}, err
	}
	at := query.EvaluateAt
	if at.IsZero() {
		at = s.clock().UTC()
	}
	return c.EvaluateComplianceAt(at, query.WarningDays), nil
}

func (s *Service) Manifest(ctx context.Context, caseID string) (*domain.ReleaseManifest, error) {
	s.manifestMu.Lock()
	if pending := s.manifestLoads[caseID]; pending != nil {
		s.manifestMu.Unlock()
		select {
		case <-pending.done:
			if pending.err != nil {
				return nil, pending.err
			}
			var result domain.ReleaseManifest
			if err := json.Unmarshal(pending.encoded, &result); err != nil {
				return nil, err
			}
			return &result, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pending := &manifestLoad{done: make(chan struct{})}
	s.manifestLoads[caseID] = pending
	s.manifestMu.Unlock()

	c, err := s.repo.Get(ctx, caseID)
	if err != nil {
		// 加载失败由当前请求直接处理，等待者仍保留在共享加载表中。
		return nil, err
	}
	if c.Manifest == nil {
		err := domain.NewError("MANIFEST_NOT_FROZEN", "案件尚未冻结开放清单")
		s.completeManifestLoad(caseID, pending, nil, err)
		return nil, err
	}
	if c.Status != domain.StatusFrozen && c.Status != domain.StatusReleased {
		err := domain.NewError("MANIFEST_NOT_FROZEN", "案件尚未冻结开放清单")
		s.completeManifestLoad(caseID, pending, nil, err)
		return nil, err
	}
	validDigest, err := domain.ManifestDigest(*c.Manifest)
	if err != nil || validDigest != c.Manifest.Digest {
		err := domain.NewError("MANIFEST_INTEGRITY_ERROR", "冻结清单摘要校验失败")
		s.completeManifestLoad(caseID, pending, nil, err)
		return nil, err
	}
	b, err := json.Marshal(c.Manifest)
	if err != nil {
		s.completeManifestLoad(caseID, pending, nil, err)
		return nil, err
	}
	var result domain.ReleaseManifest
	if err := json.Unmarshal(b, &result); err != nil {
		s.completeManifestLoad(caseID, pending, nil, err)
		return nil, err
	}
	s.completeManifestLoad(caseID, pending, b, nil)
	return &result, nil
}

func (s *Service) completeManifestLoad(caseID string, pending *manifestLoad, encoded []byte, err error) {
	s.manifestMu.Lock()
	pending.encoded = append([]byte(nil), encoded...)
	pending.err = err
	if s.manifestLoads[caseID] == pending {
		delete(s.manifestLoads, caseID)
	}
	close(pending.done)
	s.manifestMu.Unlock()
}

func (s *Service) ManifestTrace(ctx context.Context, caseID, recordingID string) (domain.ManifestTrace, error) {
	c, err := s.repo.Get(ctx, caseID)
	if err != nil {
		return domain.ManifestTrace{}, err
	}
	if c.Status != domain.StatusFrozen && c.Status != domain.StatusReleased || c.Manifest == nil {
		return domain.ManifestTrace{}, domain.NewError("MANIFEST_NOT_FROZEN", "案件尚未冻结开放清单")
	}
	return domain.ManifestRecordingTrace(*c.Manifest, recordingID)
}

func (s *Service) Readiness(ctx context.Context, caseID string, at time.Time) (domain.ApprovalReadiness, error) {
	c, err := s.repo.Get(ctx, caseID)
	if err != nil {
		return domain.ApprovalReadiness{}, err
	}
	if at.IsZero() {
		at = s.clock().UTC()
	}
	return c.ApprovalReadiness(at)
}

const MaxCredentialSegmentLength = 100

func (s *Service) CredentialChain(ctx context.Context, no string, length int) (domain.CredentialChainResult, error) {
	if length < 1 || length > MaxCredentialSegmentLength {
		return domain.CredentialChainResult{}, domain.FieldError("INVALID_SEGMENT_LENGTH", "区段长度必须在 1 到 100 之间", "length", "允许范围 1..100")
	}
	target, err := s.audit.Credential(ctx, no)
	if err != nil {
		return domain.CredentialChainResult{}, err
	}
	if uint64(length) > target.Sequence {
		return domain.CredentialChainResult{}, domain.FieldError("INVALID_SEGMENT_RANGE", "区段起点早于首个凭据", "length", "超过目标凭据序号")
	}
	segment, err := s.audit.CredentialSegment(ctx, target.Sequence, length)
	if err != nil {
		return domain.CredentialChainResult{}, err
	}
	expectedPrevious := ""
	checkFirst := segment[0].Sequence == 1
	if segment[0].Sequence > 1 {
		previousNo := fmt.Sprintf("OAR-%010d", segment[0].Sequence-1)
		previous, previousErr := s.audit.Credential(ctx, previousNo)
		if previousErr != nil {
			return domain.CredentialChainResult{}, previousErr
		}
		expectedPrevious, checkFirst = previous.CredentialDigest, true
	}
	result := domain.VerifyCredentialSegmentFrom(segment, expectedPrevious, checkFirst)
	c, err := s.repo.GetByCredential(ctx, no)
	if err != nil {
		result.Valid, result.TargetIndexValid = false, false
		if result.ProblemCode == "" {
			result.ProblemCode = "CREDENTIAL_INDEX_MISMATCH"
			result.FirstFailureSequence = target.Sequence
		}
		return result, nil
	}
	manifestValid := false
	if c.Manifest != nil {
		digest, digestErr := domain.ManifestDigest(*c.Manifest)
		manifestValid = digestErr == nil && digest == c.Manifest.Digest && c.Manifest.Digest == target.ManifestDigest
	}
	result.TargetManifestValid = manifestValid
	result.TargetIndexValid = c.Credential != nil && c.Credential.CredentialNo == no && c.Credential.CredentialDigest == target.CredentialDigest
	if !manifestValid {
		result.Valid = false
		if result.ProblemCode == "" {
			result.ProblemCode = "MANIFEST_DIGEST_MISMATCH"
			result.FirstFailureSequence = target.Sequence
		}
	} else if !result.TargetIndexValid {
		result.Valid = false
		if result.ProblemCode == "" {
			result.ProblemCode = "CREDENTIAL_INDEX_MISMATCH"
			result.FirstFailureSequence = target.Sequence
		}
	}
	return result, nil
}

func (s *Service) Overview(ctx context.Context, caseID string) (CaseOverview, error) {
	c, err := s.repo.Get(ctx, caseID)
	if err != nil {
		return CaseOverview{}, err
	}
	timeline, err := s.audit.Timeline(ctx, caseID)
	if err != nil {
		return CaseOverview{}, err
	}
	return CaseOverview{Case: c, Compliance: c.EvaluateCompliance(s.clock().UTC()), Timeline: timeline}, nil
}
