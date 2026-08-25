package domain

import (
	"sort"
	"strings"
	"time"
)

func NewReleaseCase(id, collectionID, title, purpose, catalogerID string, now time.Time) (*ReleaseCase, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(collectionID) == "" || strings.TrimSpace(title) == "" || strings.TrimSpace(purpose) == "" || strings.TrimSpace(catalogerID) == "" {
		return nil, NewError("VALIDATION_FAILED", "案件标识、馆藏项目、标题、开放目的和责任编目员均不能为空")
	}
	now = now.UTC()
	return &ReleaseCase{ID: id, CollectionID: collectionID, Title: strings.TrimSpace(title), Purpose: strings.TrimSpace(purpose), CatalogerID: strings.TrimSpace(catalogerID), Status: StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now, Participants: []Participant{}, Recordings: []RecordingItem{}, Consents: []ConsentGrant{}, Findings: []ReviewFinding{}, EvidencePackages: []EvidencePackage{}}, nil
}

func (c *ReleaseCase) AddParticipant(p Participant, now time.Time) error {
	if c.Status != StatusDraft {
		return NewError("INVALID_STATE", "只有 DRAFT 案件可以新增参与者")
	}
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.IdentityRef) == "" {
		return NewError("VALIDATION_FAILED", "参与者 id 和 identityRef 不能为空")
	}
	if c.participant(p.ID) != nil {
		return NewError("DUPLICATE_PARTICIPANT", "参与者已存在")
	}
	c.Participants = append(c.Participants, p)
	c.touch(now)
	return nil
}

func (c *ReleaseCase) AddRecording(r RecordingItem, actor string, now time.Time) error {
	if c.Status != StatusDraft {
		return NewError("INVALID_STATE", "只有 DRAFT 案件可以登记录音")
	}
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.CatalogRef) == "" || strings.TrimSpace(r.ContentDigest) == "" || r.DurationSeconds <= 0 || len(r.ParticipantIDs) == 0 {
		return NewError("VALIDATION_FAILED", "录音标识、目录引用、内容摘要、时长和参与者不能为空")
	}
	if c.recording(r.ID) != nil {
		return NewError("DUPLICATE_RECORDING", "录音已存在")
	}
	for _, id := range uniqueSorted(r.ParticipantIDs) {
		if c.participant(id) == nil {
			return FieldError("UNKNOWN_PARTICIPANT", "录音引用了未知参与者", "participantIds", id)
		}
	}
	r.CaseID = c.ID
	r.ParticipantIDs = uniqueSorted(r.ParticipantIDs)
	r.LanguageTags = uniqueSorted(r.LanguageTags)
	r.SensitiveTopics = uniqueSorted(r.SensitiveTopics)
	if r.CurrentRevisionID == "" {
		r.CurrentRevisionID = r.ID + "-r1"
	}
	r.Revisions = []RecordingRevision{{ID: r.CurrentRevisionID, ContentDigest: r.ContentDigest, Summary: "原始编目录音", CreatedBy: actor, CreatedAt: now.UTC()}}
	c.Recordings = append(c.Recordings, r)
	c.touch(now)
	return nil
}

func (c *ReleaseCase) AddConsent(g ConsentGrant, now time.Time) error {
	if c.Status != StatusDraft && c.Status != StatusChangesRequired {
		return NewError("INVALID_STATE", "当前状态不能补充同意材料")
	}
	if strings.TrimSpace(g.ID) == "" || strings.TrimSpace(g.DocumentDigest) == "" || g.SignedAt.IsZero() || len(g.RecordingIDs) == 0 || len(g.AllowedPurposes) == 0 || len(g.Audience) == 0 {
		return NewError("VALIDATION_FAILED", "同意材料的标识、摘要、签署日期、录音、用途和访问人群不能为空")
	}
	if g.ExpiresAt != nil && !g.ExpiresAt.After(g.SignedAt) {
		return FieldError("INVALID_DATE", "到期时间必须晚于签署时间", "expiresAt", "必须晚于 signedAt")
	}
	if g.WithdrawnAt != nil && g.WithdrawnAt.Before(g.SignedAt) {
		return FieldError("INVALID_DATE", "撤回时间不能早于签署时间", "withdrawnAt", "不能早于 signedAt")
	}
	if c.participant(g.ParticipantID) == nil {
		return NewError("UNKNOWN_PARTICIPANT", "同意材料引用了未知参与者")
	}
	for i := range c.Consents {
		if c.Consents[i].ID == g.ID {
			return NewError("DUPLICATE_CONSENT", "同意材料已存在")
		}
	}
	for _, rid := range g.RecordingIDs {
		r := c.recording(rid)
		if r == nil {
			return FieldError("UNKNOWN_RECORDING", "同意材料引用了未知录音", "recordingIds", rid)
		}
		if !contains(r.ParticipantIDs, g.ParticipantID) {
			return NewError("CONSENT_SUBJECT_MISMATCH", "同意人不是录音参与者")
		}
	}
	if g.SupersedesID != "" {
		found := false
		for i := range c.Consents {
			if c.Consents[i].ID == g.SupersedesID && c.Consents[i].ParticipantID == g.ParticipantID {
				found = true
			}
		}
		if !found {
			return NewError("INVALID_SUPERSEDES", "被修订同意材料不存在或参与者不一致")
		}
	}
	g.CaseID = c.ID
	g.RecordingIDs = uniqueSorted(g.RecordingIDs)
	g.AllowedPurposes = uniqueSorted(g.AllowedPurposes)
	g.Audience = uniqueSorted(g.Audience)
	g.SensitiveTopics = uniqueSorted(g.SensitiveTopics)
	c.Consents = append(c.Consents, g)
	c.touch(now)
	return nil
}

func (c *ReleaseCase) SubmitReview(now time.Time) error {
	if c.Status != StatusDraft {
		return NewError("INVALID_STATE", "只有 DRAFT 案件可以提交审查")
	}
	issues := c.IntegrityIssues(now)
	if len(issues) > 0 {
		return &Error{Code: "INCOMPLETE_CASE", Message: "提交前完整性校验失败", Fields: issues}
	}
	c.Status = StatusInReview
	c.touch(now)
	return nil
}

func (c *ReleaseCase) AddFinding(f ReviewFinding, now time.Time) error {
	if c.Status != StatusInReview && c.Status != StatusChangesRequired {
		return NewError("INVALID_STATE", "只有审查中的案件可以登记问题")
	}
	if f.ID == "" || f.Description == "" || f.SubjectID == "" {
		return NewError("VALIDATION_FAILED", "问题标识、说明和关联对象不能为空")
	}
	if !validFindingKind(f.Kind) {
		return NewError("INVALID_FINDING_KIND", "不支持的问题类型")
	}
	if f.SubjectType == "recording" && c.recording(f.SubjectID) == nil {
		return NewError("UNKNOWN_RECORDING", "问题关联的录音不存在")
	}
	if f.SubjectType == "participant" && c.participant(f.SubjectID) == nil {
		return NewError("UNKNOWN_PARTICIPANT", "问题关联的参与者不存在")
	}
	if f.SubjectType != "recording" && f.SubjectType != "participant" {
		return NewError("INVALID_SUBJECT_TYPE", "问题关联对象必须是 recording 或 participant")
	}
	for i := range c.Findings {
		if c.Findings[i].ID == f.ID {
			return NewError("DUPLICATE_FINDING", "审查问题已存在")
		}
	}
	f.CaseID, f.Status = c.ID, "OPEN"
	if f.Severity == "" {
		f.Severity = "MAJOR"
	}
	c.Findings = append(c.Findings, f)
	c.Status = StatusChangesRequired
	c.touch(now)
	return nil
}

func (c *ReleaseCase) AddRedactedRevision(recordingID string, revision RecordingRevision, evidence string, now time.Time) error {
	if c.Status != StatusChangesRequired {
		return NewError("INVALID_STATE", "只有 CHANGES_REQUIRED 案件可以登记脱敏修订")
	}
	r := c.recording(recordingID)
	if r == nil {
		return NewError("UNKNOWN_RECORDING", "录音不存在")
	}
	if revision.ID == "" || revision.ContentDigest == "" || revision.RedactionDigest == "" || revision.Summary == "" || evidence == "" {
		return NewError("VALIDATION_FAILED", "修订标识、内容摘要、脱敏摘要、说明和整改证据不能为空")
	}
	for _, existing := range r.Revisions {
		if existing.ID == revision.ID {
			return NewError("DUPLICATE_REVISION", "录音修订已存在")
		}
	}
	revision.ParentID, revision.CreatedAt = r.CurrentRevisionID, now.UTC()
	r.Revisions = append(r.Revisions, revision)
	r.CurrentRevisionID, r.ContentDigest = revision.ID, revision.ContentDigest
	for i := range c.Findings {
		if c.Findings[i].SubjectType == "recording" && c.Findings[i].SubjectID == recordingID && c.Findings[i].Status == "OPEN" {
			c.Findings[i].RemediationEvidence = evidence
		}
	}
	c.touch(now)
	return nil
}

func (c *ReleaseCase) ReviewFinding(id, evidence, reviewer string, accepted bool, now time.Time) error {
	if c.Status != StatusChangesRequired {
		return NewError("INVALID_STATE", "当前案件没有待复核整改")
	}
	for i := range c.Findings {
		f := &c.Findings[i]
		if f.ID != id {
			continue
		}
		if f.Status != "OPEN" {
			return NewError("FINDING_ALREADY_REVIEWED", "问题已完成复核")
		}
		if evidence == "" {
			evidence = f.RemediationEvidence
		}
		if evidence == "" {
			return NewError("MISSING_REMEDIATION_EVIDENCE", "关闭问题前必须提供整改证据")
		}
		f.RemediationEvidence, f.ReviewedBy, f.ReviewedAt = evidence, reviewer, timePtr(now.UTC())
		if accepted {
			f.Status = "CLOSED"
		} else {
			f.Status = "OPEN"
		}
		c.touch(now)
		return nil
	}
	return NewError("FINDING_NOT_FOUND", "审查问题不存在")
}

func (c *ReleaseCase) finding(id string) *ReviewFinding {
	for i := range c.Findings {
		if c.Findings[i].ID == id {
			return &c.Findings[i]
		}
	}
	return nil
}

func (c *ReleaseCase) Decide(approve bool, actor, reason string, now time.Time) error {
	if c.Status != StatusInReview && c.Status != StatusChangesRequired {
		return NewError("INVALID_STATE", "当前案件不能作出开放决定")
	}
	if strings.TrimSpace(reason) == "" {
		return NewError("VALIDATION_FAILED", "决定理由不能为空")
	}
	if !approve {
		c.Decision = &ApprovalDecision{Decision: "RETURNED", ActorID: actor, Reason: reason, At: now.UTC()}
		c.Status = StatusChangesRequired
		c.touch(now)
		return nil
	}
	for _, f := range c.Findings {
		if f.Status != "CLOSED" {
			return NewError("OPEN_FINDINGS", "仍有未关闭的审查问题，不能批准")
		}
	}
	for _, scope := range c.ComputeAccessScopes(now) {
		if !scope.Valid {
			return NewError("AMBIGUOUS_ACCESS_SCOPE", "所有录音必须具有明确可执行的访问范围")
		}
	}
	c.Decision = &ApprovalDecision{Decision: "APPROVED", ActorID: actor, Reason: reason, At: now.UTC()}
	c.Status = StatusApproved
	c.touch(now)
	return nil
}

func (c *ReleaseCase) touch(now time.Time) { c.Version++; c.UpdatedAt = now.UTC() }

func (c *ReleaseCase) participant(id string) *Participant {
	for i := range c.Participants {
		if c.Participants[i].ID == id {
			return &c.Participants[i]
		}
	}
	return nil
}
func (c *ReleaseCase) recording(id string) *RecordingItem {
	for i := range c.Recordings {
		if c.Recordings[i].ID == id {
			return &c.Recordings[i]
		}
	}
	return nil
}
func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
func uniqueSorted(items []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}
func timePtr(t time.Time) *time.Time { return &t }
func validFindingKind(k FindingKind) bool {
	return k == FindingMissingConsent || k == FindingPurposeExceeded || k == FindingExpiredGrant || k == FindingWithdrawal || k == FindingSensitiveExposure
}
