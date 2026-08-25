package domain

import (
	"fmt"
	"strings"
)

func (c *ReleaseCase) ValidateSnapshot() error {
	if c == nil {
		return fmt.Errorf("案件快照为空")
	}
	if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.CollectionID) == "" || strings.TrimSpace(c.CatalogerID) == "" {
		return fmt.Errorf("案件核心标识不完整")
	}
	if c.Version < 1 || c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return fmt.Errorf("案件版本或时间戳无效")
	}
	if !validStatus(c.Status) {
		return fmt.Errorf("未知案件状态 %q", c.Status)
	}
	participants := map[string]bool{}
	for _, participant := range c.Participants {
		if participant.ID == "" || participant.IdentityRef == "" {
			return fmt.Errorf("参与者记录不完整")
		}
		if participants[participant.ID] {
			return fmt.Errorf("参与者 %s 重复", participant.ID)
		}
		participants[participant.ID] = true
	}
	recordings := map[string]bool{}
	for _, recording := range c.Recordings {
		if recording.ID == "" || recording.CaseID != c.ID || recording.CurrentRevisionID == "" || recording.ContentDigest == "" {
			return fmt.Errorf("录音 %s 核心字段不完整", recording.ID)
		}
		if recordings[recording.ID] {
			return fmt.Errorf("录音 %s 重复", recording.ID)
		}
		recordings[recording.ID] = true
		for _, participantID := range recording.ParticipantIDs {
			if !participants[participantID] {
				return fmt.Errorf("录音 %s 引用未知参与者 %s", recording.ID, participantID)
			}
		}
		revisions := map[string]bool{}
		currentFound := false
		for _, revision := range recording.Revisions {
			if revision.ID == "" || revision.ContentDigest == "" {
				return fmt.Errorf("录音 %s 包含无效修订", recording.ID)
			}
			if revisions[revision.ID] {
				return fmt.Errorf("录音 %s 的修订 %s 重复", recording.ID, revision.ID)
			}
			revisions[revision.ID] = true
			if revision.ID == recording.CurrentRevisionID {
				currentFound = true
				if revision.ContentDigest != recording.ContentDigest {
					return fmt.Errorf("录音 %s 当前修订摘要不一致", recording.ID)
				}
			}
		}
		if !currentFound {
			return fmt.Errorf("录音 %s 的当前修订不存在", recording.ID)
		}
	}
	consents := map[string]ConsentGrant{}
	for _, consent := range c.Consents {
		if consent.ID == "" || consent.CaseID != c.ID || !participants[consent.ParticipantID] || consent.DocumentDigest == "" {
			return fmt.Errorf("同意材料 %s 核心字段或关联无效", consent.ID)
		}
		if _, duplicate := consents[consent.ID]; duplicate {
			return fmt.Errorf("同意材料 %s 重复", consent.ID)
		}
		for _, recordingID := range consent.RecordingIDs {
			if !recordings[recordingID] {
				return fmt.Errorf("同意材料 %s 引用未知录音 %s", consent.ID, recordingID)
			}
			for _, recording := range c.Recordings {
				if recording.ID == recordingID && !contains(recording.ParticipantIDs, consent.ParticipantID) {
					return fmt.Errorf("同意材料 %s 的参与者不属于录音 %s", consent.ID, recordingID)
				}
			}
		}
		consents[consent.ID] = consent
	}
	for _, consent := range c.Consents {
		if consent.SupersedesID != "" {
			prior, ok := consents[consent.SupersedesID]
			if !ok || prior.ParticipantID != consent.ParticipantID {
				return fmt.Errorf("同意材料 %s 的修订关系无效", consent.ID)
			}
		}
	}
	for _, consent := range c.Consents {
		seen := map[string]bool{consent.ID: true}
		cursor := consent.SupersedesID
		for cursor != "" {
			if seen[cursor] {
				return fmt.Errorf("同意材料 %s 的修订关系存在环", consent.ID)
			}
			seen[cursor] = true
			cursor = consents[cursor].SupersedesID
		}
	}
	findings := map[string]bool{}
	for _, finding := range c.Findings {
		if finding.ID == "" || finding.CaseID != c.ID || !validFindingKind(finding.Kind) {
			return fmt.Errorf("审查问题 %s 无效", finding.ID)
		}
		if findings[finding.ID] {
			return fmt.Errorf("审查问题 %s 重复", finding.ID)
		}
		findings[finding.ID] = true
		if finding.SubjectType == "recording" && !recordings[finding.SubjectID] {
			return fmt.Errorf("审查问题 %s 引用未知录音", finding.ID)
		}
		if finding.SubjectType == "participant" && !participants[finding.SubjectID] {
			return fmt.Errorf("审查问题 %s 引用未知参与者", finding.ID)
		}
	}
	packages := map[string]bool{}
	for _, pkg := range c.EvidencePackages {
		if pkg.ID == "" || pkg.Description == "" || pkg.SubmittedBy == "" || pkg.SubmittedAt.IsZero() {
			return fmt.Errorf("证据包核心字段不完整")
		}
		if packages[pkg.ID] {
			return fmt.Errorf("证据包 %s 重复", pkg.ID)
		}
		packages[pkg.ID] = true
		for _, id := range pkg.FindingIDs {
			if !findings[id] {
				return fmt.Errorf("证据包 %s 引用未知问题", pkg.ID)
			}
		}
		for _, id := range pkg.ConsentIDs {
			if _, ok := consents[id]; !ok {
				return fmt.Errorf("证据包 %s 引用未知同意材料", pkg.ID)
			}
		}
	}
	if c.Status == StatusApproved && (c.Decision == nil || c.Decision.Decision != "APPROVED") {
		return fmt.Errorf("APPROVED 案件缺少批准决定")
	}
	if c.Status == StatusFrozen || c.Status == StatusReleased {
		if c.Manifest == nil {
			return fmt.Errorf("%s 案件缺少冻结清单", c.Status)
		}
		digest, err := ManifestDigest(*c.Manifest)
		if err != nil || digest != c.Manifest.Digest {
			return fmt.Errorf("冻结清单摘要无效")
		}
	}
	if c.Status == StatusReleased {
		if c.Credential == nil {
			return fmt.Errorf("RELEASED 案件缺少凭据")
		}
		digest, err := CredentialDigest(*c.Credential)
		if err != nil || digest != c.Credential.CredentialDigest {
			return fmt.Errorf("访问凭据摘要无效")
		}
	}
	return nil
}

func validStatus(status CaseStatus) bool {
	return status == StatusDraft || status == StatusInReview || status == StatusChangesRequired || status == StatusApproved || status == StatusFrozen || status == StatusReleased
}
