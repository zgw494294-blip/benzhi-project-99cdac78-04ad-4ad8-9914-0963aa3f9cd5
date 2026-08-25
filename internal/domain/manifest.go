package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

func (c *ReleaseCase) Freeze(now time.Time) (*ReleaseManifest, error) {
	if c.Status != StatusApproved || c.Decision == nil || c.Decision.Decision != "APPROVED" {
		return nil, NewError("INVALID_STATE", "只有已批准案件可以冻结")
	}
	scopes := c.ComputeAccessScopes(now)
	for _, s := range scopes {
		if !s.Valid {
			return nil, NewError("AMBIGUOUS_ACCESS_SCOPE", "清单包含不可执行访问范围")
		}
	}
	m := ReleaseManifest{CaseID: c.ID, CaseVersion: c.Version + 1, CreatedAt: now.UTC(), AccessScopes: scopes, Decision: *c.Decision}
	for _, r := range c.Recordings {
		m.Recordings = append(m.Recordings, ManifestRecording{ID: r.ID, RevisionID: r.CurrentRevisionID, ContentDigest: r.ContentDigest, ParticipantIDs: uniqueSorted(r.ParticipantIDs)})
	}
	for _, g := range c.Consents {
		adopted := !c.supersededConsentIDs()[g.ID]
		status := "SUPERSEDED"
		if adopted {
			status = "ADOPTED"
		}
		m.Consents = append(m.Consents, ManifestConsent{ID: g.ID, ParticipantID: g.ParticipantID, DocumentDigest: g.DocumentDigest, RecordingIDs: uniqueSorted(g.RecordingIDs), SupersedesID: g.SupersedesID, Adopted: adopted, AdoptionStatus: status})
	}
	sort.Slice(m.Recordings, func(i, j int) bool { return m.Recordings[i].ID < m.Recordings[j].ID })
	sort.Slice(m.Consents, func(i, j int) bool { return m.Consents[i].ID < m.Consents[j].ID })
	digest, err := ManifestDigest(m)
	if err != nil {
		return nil, err
	}
	m.Digest = digest
	c.Manifest, c.Status = &m, StatusFrozen
	c.touch(now)
	return &m, nil
}

type ManifestTrace struct {
	CaseID         string            `json:"caseId"`
	Recording      ManifestRecording `json:"recording"`
	Consents       []ManifestConsent `json:"consents"`
	AccessScope    AccessScope       `json:"accessScope"`
	Decision       ApprovalDecision  `json:"decision"`
	ManifestDigest string            `json:"manifestDigest"`
}

func ManifestRecordingTrace(m ReleaseManifest, recordingID string) (ManifestTrace, error) {
	digest, err := ManifestDigest(m)
	if err != nil || digest != m.Digest {
		return ManifestTrace{}, NewError("MANIFEST_INTEGRITY_ERROR", "冻结清单摘要校验失败")
	}
	var recording *ManifestRecording
	for i := range m.Recordings {
		if m.Recordings[i].ID == recordingID {
			recording = &m.Recordings[i]
			break
		}
	}
	if recording == nil {
		return ManifestTrace{}, NewError("MANIFEST_RECORDING_NOT_FOUND", "冻结清单中不存在该录音")
	}
	trace := ManifestTrace{CaseID: m.CaseID, Recording: *recording, Consents: []ManifestConsent{}, Decision: m.Decision, ManifestDigest: m.Digest}
	participants := map[string]bool{}
	for _, id := range recording.ParticipantIDs {
		participants[id] = true
	}
	for _, consent := range m.Consents {
		if participants[consent.ParticipantID] && contains(consent.RecordingIDs, recordingID) {
			trace.Consents = append(trace.Consents, consent)
		}
	}
	for _, scope := range m.AccessScopes {
		if scope.RecordingID == recordingID {
			trace.AccessScope = scope
			break
		}
	}
	sort.Slice(trace.Consents, func(i, j int) bool {
		if trace.Consents[i].ParticipantID != trace.Consents[j].ParticipantID {
			return trace.Consents[i].ParticipantID < trace.Consents[j].ParticipantID
		}
		return trace.Consents[i].ID < trace.Consents[j].ID
	})
	return trace, nil
}

func ManifestDigest(m ReleaseManifest) (string, error) {
	m.Digest = ""
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("编码开放清单: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func CredentialDigest(credential ReleaseCredential) (string, error) {
	credential.CredentialDigest = ""
	b, err := json.Marshal(credential)
	if err != nil {
		return "", fmt.Errorf("编码访问凭据: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (c *ReleaseCase) AttachCredential(credential ReleaseCredential, now time.Time) error {
	if c.Status != StatusFrozen || c.Manifest == nil {
		return NewError("INVALID_STATE", "只有已冻结案件可以签发凭据")
	}
	if credential.CaseID != c.ID || credential.ManifestDigest != c.Manifest.Digest {
		return NewError("CREDENTIAL_MANIFEST_MISMATCH", "凭据与冻结清单不匹配")
	}
	computed, err := CredentialDigest(credential)
	if err != nil {
		return err
	}
	if credential.CredentialDigest != computed {
		return NewError("INVALID_CREDENTIAL_DIGEST", "凭据摘要不正确")
	}
	c.Credential, c.Status = &credential, StatusReleased
	c.touch(now)
	return nil
}

func (c *ReleaseCase) Verify() (bool, []string) {
	var problems []string
	if c.Manifest == nil {
		return false, []string{"缺少冻结清单"}
	}
	d, err := ManifestDigest(*c.Manifest)
	if err != nil || d != c.Manifest.Digest {
		problems = append(problems, "开放清单摘要不匹配")
	}
	if c.Credential == nil {
		problems = append(problems, "缺少访问凭据")
	} else {
		cd, err := CredentialDigest(*c.Credential)
		if err != nil || cd != c.Credential.CredentialDigest {
			problems = append(problems, "访问凭据摘要不匹配")
		}
		if c.Credential.ManifestDigest != c.Manifest.Digest {
			problems = append(problems, "凭据未引用当前冻结清单")
		}
	}
	return len(problems) == 0, problems
}
