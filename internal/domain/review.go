package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type FindingImportResult struct {
	Created []ReviewFinding `json:"created"`
	Skipped []ReviewFinding `json:"skipped"`
}

func (c *ReleaseCase) ImportComplianceFindings(selected []string, at time.Time, now time.Time) (FindingImportResult, error) {
	if c.Status != StatusInReview && c.Status != StatusChangesRequired {
		return FindingImportResult{}, NewError("INVALID_STATE", "只有审查中的案件可以导入合规问题")
	}
	report := c.EvaluateComplianceAt(at, 0)
	available := map[string]ComplianceIssue{}
	for _, issue := range report.Issues {
		if _, ok := findingKindForCompliance(issue.Code); ok {
			available[issue.Key] = issue
		}
	}
	keys := uniqueSorted(selected)
	if len(keys) == 0 {
		for key := range available {
			keys = append(keys, key)
		}
		sort.Strings(keys)
	}
	for _, key := range keys {
		if _, ok := available[key]; !ok {
			return FindingImportResult{}, FieldError("COMPLIANCE_SELECTION_CHANGED", "所选合规异常已不存在", "issueKeys", key)
		}
	}
	result := FindingImportResult{Created: []ReviewFinding{}, Skipped: []ReviewFinding{}}
	for _, key := range keys {
		issue := available[key]
		kind, _ := findingKindForCompliance(issue.Code)
		dedup := FindingDedupKeyForIssue(issue)
		if prior := c.findingByDedup(dedup); prior != nil {
			result.Skipped = append(result.Skipped, *prior)
			continue
		}
		subjectType, subjectID := "recording", issue.RecordingID
		if issue.ParticipantID != "" {
			subjectType, subjectID = "participant", issue.ParticipantID
		}
		f := ReviewFinding{ID: "finding_auto_" + shortHash(dedup), CaseID: c.ID, Kind: kind, Severity: "MAJOR", SubjectType: subjectType, SubjectID: subjectID, RecordingID: issue.RecordingID, ParticipantID: issue.ParticipantID, Topic: issue.Topic, DedupKey: dedup, SourceCaseVersion: c.Version, Description: issue.Message, Status: "OPEN"}
		c.Findings = append(c.Findings, f)
		result.Created = append(result.Created, f)
	}
	if len(result.Created) > 0 {
		c.Status = StatusChangesRequired
	}
	c.touch(now)
	return result, nil
}

func FindingDedupKeyForIssue(issue ComplianceIssue) string {
	kind, _ := findingKindForCompliance(issue.Code)
	return string(kind) + "|" + issue.RecordingID + "|" + issue.ParticipantID + "|" + issue.Topic
}

func findingKindForCompliance(code ComplianceCode) (FindingKind, bool) {
	switch code {
	case ComplianceMissingConsent, ComplianceNotEffective:
		return FindingMissingConsent, true
	case CompliancePurpose, ComplianceAudience:
		return FindingPurposeExceeded, true
	case ComplianceExpired:
		return FindingExpiredGrant, true
	case ComplianceWithdrawn:
		return FindingWithdrawal, true
	case ComplianceSensitive:
		return FindingSensitiveExposure, true
	default:
		return "", false
	}
}

func (c *ReleaseCase) findingByDedup(key string) *ReviewFinding {
	for i := range c.Findings {
		candidate := c.Findings[i].DedupKey
		if candidate == "" {
			candidate = string(c.Findings[i].Kind) + "|" + c.Findings[i].RecordingID + "|" + c.Findings[i].ParticipantID + "|" + c.Findings[i].Topic
		}
		if candidate == key {
			return &c.Findings[i]
		}
	}
	return nil
}

type EvidenceSubmission struct {
	ID          string             `json:"id"`
	Description string             `json:"description"`
	FindingIDs  []string           `json:"findingIds"`
	Consents    []ConsentGrant     `json:"consents"`
	Revisions   []EvidenceRevision `json:"revisions"`
}

type EvidenceRevision struct {
	RecordingID string            `json:"recordingId"`
	Revision    RecordingRevision `json:"revision"`
}

func (c *ReleaseCase) SubmitEvidencePackage(input EvidenceSubmission, actor string, now time.Time) (*EvidencePackage, error) {
	if c.Status != StatusChangesRequired {
		return nil, NewError("INVALID_STATE", "只有 CHANGES_REQUIRED 案件可以提交证据包")
	}
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.Description) == "" || len(input.FindingIDs) == 0 || len(input.Consents)+len(input.Revisions) == 0 {
		return nil, NewError("VALIDATION_FAILED", "证据包标识、说明、问题和至少一项材料不能为空")
	}
	for _, existing := range c.EvidencePackages {
		if existing.ID == input.ID {
			return nil, NewError("DUPLICATE_EVIDENCE_PACKAGE", "证据包已存在")
		}
	}
	work, err := cloneReleaseCase(c)
	if err != nil {
		return nil, err
	}
	for i, consent := range input.Consents {
		if err := work.AddConsent(consent, now); err != nil {
			return nil, indexedError(err, "consents", i)
		}
	}
	for i, revision := range input.Revisions {
		revision.Revision.CreatedBy = actor
		if err := work.AddRedactedRevision(revision.RecordingID, revision.Revision, "evidence-package:"+input.ID, now); err != nil {
			return nil, indexedError(err, "revisions", i)
		}
	}
	findingIDs := uniqueSorted(input.FindingIDs)
	for _, id := range findingIDs {
		finding := work.finding(id)
		if finding == nil {
			return nil, FieldError("FINDING_NOT_FOUND", "审查问题不存在", "findingIds", id)
		}
		if finding.Status != "OPEN" {
			return nil, FieldError("FINDING_NOT_OPEN", "证据包只能关联未关闭问题", "findingIds", id)
		}
		if !evidenceCovers(*finding, input.Consents, input.Revisions) {
			return nil, FieldError("EVIDENCE_SUBJECT_MISMATCH", "证据与问题对象不匹配", "findingIds", id)
		}
	}
	pkg := EvidencePackage{ID: input.ID, Description: strings.TrimSpace(input.Description), FindingIDs: findingIDs, ConsentIDs: []string{}, RevisionIDs: []string{}, MaterialSummaries: []EvidenceSummary{}, RevisionSummaries: []EvidenceSummary{}, SubmittedBy: actor, SubmittedAt: now.UTC()}
	for _, consent := range input.Consents {
		pkg.ConsentIDs = append(pkg.ConsentIDs, consent.ID)
		pkg.MaterialSummaries = append(pkg.MaterialSummaries, EvidenceSummary{ID: consent.ID, SubjectID: consent.ParticipantID, Digest: consent.DocumentDigest})
	}
	for _, revision := range input.Revisions {
		pkg.RevisionIDs = append(pkg.RevisionIDs, revision.Revision.ID)
		pkg.RevisionSummaries = append(pkg.RevisionSummaries, EvidenceSummary{ID: revision.Revision.ID, SubjectID: revision.RecordingID, Digest: revision.Revision.ContentDigest, Summary: revision.Revision.Summary})
	}
	pkg.ConsentIDs, pkg.RevisionIDs = uniqueSorted(pkg.ConsentIDs), uniqueSorted(pkg.RevisionIDs)
	sort.Slice(pkg.MaterialSummaries, func(i, j int) bool { return pkg.MaterialSummaries[i].ID < pkg.MaterialSummaries[j].ID })
	sort.Slice(pkg.RevisionSummaries, func(i, j int) bool { return pkg.RevisionSummaries[i].ID < pkg.RevisionSummaries[j].ID })
	work.EvidencePackages = append(work.EvidencePackages, pkg)
	work.Version, work.UpdatedAt = c.Version, c.UpdatedAt
	work.touch(now)
	*c = *work
	return &c.EvidencePackages[len(c.EvidencePackages)-1], nil
}

func evidenceCovers(finding ReviewFinding, consents []ConsentGrant, revisions []EvidenceRevision) bool {
	if finding.SubjectType == "participant" {
		for _, consent := range consents {
			if consent.ParticipantID == finding.SubjectID && (finding.RecordingID == "" || contains(consent.RecordingIDs, finding.RecordingID)) {
				return true
			}
		}
		return false
	}
	for _, consent := range consents {
		if contains(consent.RecordingIDs, finding.SubjectID) {
			return true
		}
	}
	for _, revision := range revisions {
		if revision.RecordingID == finding.SubjectID {
			return true
		}
	}
	return false
}

func (c *ReleaseCase) ReviewFindingWithPackage(id, packageID, opinion, reviewer string, accepted bool, now time.Time) error {
	if strings.TrimSpace(packageID) == "" || strings.TrimSpace(opinion) == "" {
		return NewError("VALIDATION_FAILED", "复核必须提供 evidencePackageId 和 reviewOpinion")
	}
	finding := c.finding(id)
	if finding == nil {
		return NewError("FINDING_NOT_FOUND", "审查问题不存在")
	}
	var pkg *EvidencePackage
	for i := range c.EvidencePackages {
		if c.EvidencePackages[i].ID == packageID {
			pkg = &c.EvidencePackages[i]
		}
	}
	if pkg == nil {
		return NewError("EVIDENCE_PACKAGE_NOT_FOUND", "证据包不存在")
	}
	if !contains(pkg.FindingIDs, id) {
		return NewError("EVIDENCE_NOT_COVERING_FINDING", "证据包未覆盖该问题")
	}
	if !c.persistedPackageCovers(*finding, *pkg) {
		return NewError("EVIDENCE_NOT_COVERING_FINDING", "证据包中的材料已不能覆盖该问题")
	}
	if err := c.ReviewFinding(id, "evidence-package:"+packageID, reviewer, accepted, now); err != nil {
		return err
	}
	finding = c.finding(id)
	finding.EvidencePackageID, finding.ReviewOpinion = packageID, strings.TrimSpace(opinion)
	return nil
}

func (c *ReleaseCase) persistedPackageCovers(finding ReviewFinding, pkg EvidencePackage) bool {
	if finding.SubjectType == "participant" {
		for _, id := range pkg.ConsentIDs {
			consent := consentByID(c.Consents, id)
			if consent != nil && consent.ParticipantID == finding.SubjectID && (finding.RecordingID == "" || contains(consent.RecordingIDs, finding.RecordingID)) {
				return true
			}
		}
		return false
	}
	for _, id := range pkg.ConsentIDs {
		consent := consentByID(c.Consents, id)
		if consent != nil && contains(consent.RecordingIDs, finding.SubjectID) {
			return true
		}
	}
	for _, summary := range pkg.RevisionSummaries {
		if summary.SubjectID != finding.SubjectID {
			continue
		}
		recording := c.recording(finding.SubjectID)
		if recording != nil {
			for _, revision := range recording.Revisions {
				if revision.ID == summary.ID && revision.ContentDigest == summary.Digest {
					return true
				}
			}
		}
	}
	return false
}

type ReadinessBlocker struct {
	Code            string `json:"code"`
	RecordingID     string `json:"recordingId,omitempty"`
	ParticipantID   string `json:"participantId,omitempty"`
	Reason          string `json:"reason"`
	SuggestedRemedy string `json:"suggestedRemedy"`
}

type ApprovalReadiness struct {
	CaseID          string             `json:"caseId"`
	CaseVersion     int64              `json:"caseVersion"`
	Status          CaseStatus         `json:"status"`
	EvaluatedAt     time.Time          `json:"evaluatedAt"`
	OpenFindings    int                `json:"openFindings"`
	EarliestExpiry  *time.Time         `json:"earliestExpiry,omitempty"`
	Approvable      bool               `json:"approvable"`
	Blockers        []ReadinessBlocker `json:"blockers"`
	ReadinessDigest string             `json:"readinessDigest"`
}

func (c *ReleaseCase) ApprovalReadiness(at time.Time) (ApprovalReadiness, error) {
	at = at.UTC()
	result := ApprovalReadiness{CaseID: c.ID, CaseVersion: c.Version, Status: c.Status, EvaluatedAt: at, Approvable: true, Blockers: []ReadinessBlocker{}}
	if c.Status != StatusInReview && c.Status != StatusChangesRequired {
		result.Blockers = append(result.Blockers, ReadinessBlocker{Code: "CASE_STATE_NOT_REVIEWABLE", Reason: "案件不在可批准状态", SuggestedRemedy: "提交或恢复审查"})
	}
	for _, finding := range c.Findings {
		if finding.Status == "OPEN" {
			result.OpenFindings++
			result.Blockers = append(result.Blockers, ReadinessBlocker{Code: "OPEN_FINDING", RecordingID: finding.RecordingID, ParticipantID: finding.ParticipantID, Reason: "存在未关闭问题：" + finding.ID, SuggestedRemedy: "提交并复核整改证据"})
		}
	}
	report := c.EvaluateComplianceAt(at, 0)
	recordingsWithIssue := map[string]bool{}
	for _, issue := range report.Issues {
		recordingsWithIssue[issue.RecordingID] = true
		result.Blockers = append(result.Blockers, ReadinessBlocker{Code: string(issue.Code), RecordingID: issue.RecordingID, ParticipantID: issue.ParticipantID, Reason: issue.Message, SuggestedRemedy: remedyForCompliance(issue.Code)})
	}
	for _, scope := range report.AccessScopes {
		if scope.ExpiresAt != nil && (result.EarliestExpiry == nil || scope.ExpiresAt.Before(*result.EarliestExpiry)) {
			expires := scope.ExpiresAt.UTC()
			result.EarliestExpiry = &expires
		}
		if !scope.Valid && !recordingsWithIssue[scope.RecordingID] {
			result.Blockers = append(result.Blockers, ReadinessBlocker{Code: "INVALID_ACCESS_SCOPE", RecordingID: scope.RecordingID, Reason: strings.Join(scope.Reasons, "；"), SuggestedRemedy: "补充同意材料或脱敏修订"})
		}
	}
	sort.Slice(result.Blockers, func(i, j int) bool {
		a, b := result.Blockers[i], result.Blockers[j]
		if a.RecordingID != b.RecordingID {
			return a.RecordingID < b.RecordingID
		}
		if a.ParticipantID != b.ParticipantID {
			return a.ParticipantID < b.ParticipantID
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Reason < b.Reason
	})
	result.Approvable = len(result.Blockers) == 0
	digestInput := struct {
		CaseID   string             `json:"caseId"`
		Version  int64              `json:"version"`
		Status   CaseStatus         `json:"status"`
		Open     int                `json:"open"`
		Expiry   *time.Time         `json:"expiry,omitempty"`
		Blockers []ReadinessBlocker `json:"blockers"`
	}{c.ID, c.Version, c.Status, result.OpenFindings, result.EarliestExpiry, result.Blockers}
	b, err := json.Marshal(digestInput)
	if err != nil {
		return ApprovalReadiness{}, err
	}
	sum := sha256.Sum256(b)
	result.ReadinessDigest = hex.EncodeToString(sum[:])
	return result, nil
}

func remedyForCompliance(code ComplianceCode) string {
	switch code {
	case ComplianceMissingConsent, ComplianceExpired, ComplianceWithdrawn, ComplianceNotEffective:
		return "补充或更新同意材料"
	case CompliancePurpose, ComplianceAudience:
		return "补充明确用途和访问人群的同意材料"
	case ComplianceSensitive:
		return "补充敏感主题授权或提交脱敏修订"
	default:
		return "复核并补充整改材料"
	}
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func findingImportDetails(result FindingImportResult, sourceVersion int64) string {
	return fmt.Sprintf("sourceVersion=%d created=%d skipped=%d", sourceVersion, len(result.Created), len(result.Skipped))
}
