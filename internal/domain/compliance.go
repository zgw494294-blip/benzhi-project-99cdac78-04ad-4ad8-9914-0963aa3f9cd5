package domain

import (
	"sort"
	"time"
)

type ComplianceCode string

const (
	ComplianceMissingConsent ComplianceCode = "MISSING_CONSENT"
	CompliancePurpose        ComplianceCode = "PURPOSE_EXCEEDED"
	ComplianceAudience       ComplianceCode = "AUDIENCE_UNDEFINED"
	ComplianceExpired        ComplianceCode = "EXPIRED_GRANT"
	ComplianceWithdrawn      ComplianceCode = "WITHDRAWAL_CONFLICT"
	ComplianceSensitive      ComplianceCode = "SENSITIVE_EXPOSURE"
	ComplianceNotEffective   ComplianceCode = "NOT_YET_EFFECTIVE"
)

type ComplianceIssue struct {
	Key           string         `json:"key"`
	Code          ComplianceCode `json:"code"`
	RecordingID   string         `json:"recordingId"`
	ParticipantID string         `json:"participantId,omitempty"`
	Topic         string         `json:"topic,omitempty"`
	Message       string         `json:"message"`
}

type ConsentAssessment struct {
	RecordingID     string            `json:"recordingId"`
	ParticipantID   string            `json:"participantId"`
	GrantIDs        []string          `json:"grantIds"`
	AllowedPurposes []string          `json:"allowedPurposes"`
	Audience        []string          `json:"audience"`
	SensitiveTopics []string          `json:"sensitiveTopics"`
	ExpiresAt       *time.Time        `json:"expiresAt,omitempty"`
	Effective       bool              `json:"effective"`
	Status          string            `json:"status"`
	ExpiringSoon    bool              `json:"expiringSoon"`
	Issues          []ComplianceIssue `json:"issues"`
}

type ComplianceReport struct {
	CaseID       string              `json:"caseId"`
	CaseVersion  int64               `json:"caseVersion"`
	EvaluatedAt  time.Time           `json:"evaluatedAt"`
	WarningDays  int                 `json:"warningDays"`
	Complete     bool                `json:"complete"`
	Releasable   bool                `json:"releasable"`
	Assessments  []ConsentAssessment `json:"assessments"`
	AccessScopes []AccessScope       `json:"accessScopes"`
	Issues       []ComplianceIssue   `json:"issues"`
}

func (c *ReleaseCase) EvaluateCompliance(at time.Time) ComplianceReport {
	return c.EvaluateComplianceAt(at, 0)
}

func (c *ReleaseCase) EvaluateComplianceAt(at time.Time, warningDays int) ComplianceReport {
	at = at.UTC()
	report := ComplianceReport{CaseID: c.ID, CaseVersion: c.Version, EvaluatedAt: at, WarningDays: warningDays, Complete: len(c.Recordings) > 0 && len(c.Participants) > 0, Releasable: true, Assessments: []ConsentAssessment{}, AccessScopes: []AccessScope{}, Issues: []ComplianceIssue{}}
	for _, recording := range c.Recordings {
		for _, participantID := range recording.ParticipantIDs {
			assessment := c.assessConsent(recording, participantID, at, warningDays)
			report.Assessments = append(report.Assessments, assessment)
			report.Issues = append(report.Issues, assessment.Issues...)
			if !assessment.Effective {
				report.Complete = false
			}
		}
	}
	report.AccessScopes = c.ComputeAccessScopes(at)
	for i := range report.AccessScopes {
		scope := &report.AccessScopes[i]
		if scope.ExpiresAt != nil && warningDays > 0 && !scope.ExpiresAt.After(at.Add(time.Duration(warningDays)*24*time.Hour)) && scope.ExpiresAt.After(at) {
			scope.ExpiringSoon = true
		}
		if !scope.Valid {
			report.Releasable = false
		}
	}
	if !report.Complete {
		report.Releasable = false
	}
	sort.Slice(report.Assessments, func(i, j int) bool {
		if report.Assessments[i].RecordingID == report.Assessments[j].RecordingID {
			return report.Assessments[i].ParticipantID < report.Assessments[j].ParticipantID
		}
		return report.Assessments[i].RecordingID < report.Assessments[j].RecordingID
	})
	sort.Slice(report.Issues, func(i, j int) bool {
		a, b := report.Issues[i], report.Issues[j]
		if a.RecordingID != b.RecordingID {
			return a.RecordingID < b.RecordingID
		}
		if a.ParticipantID != b.ParticipantID {
			return a.ParticipantID < b.ParticipantID
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Topic < b.Topic
	})
	for i := range report.Issues {
		report.Issues[i].Key = ComplianceIssueKey(report.Issues[i])
	}
	for i := range report.Assessments {
		for j := range report.Assessments[i].Issues {
			report.Assessments[i].Issues[j].Key = ComplianceIssueKey(report.Assessments[i].Issues[j])
		}
	}
	return report
}

func (c *ReleaseCase) assessConsent(recording RecordingItem, participantID string, at time.Time, warningDays int) ConsentAssessment {
	assessment := ConsentAssessment{RecordingID: recording.ID, ParticipantID: participantID, GrantIDs: []string{}, AllowedPurposes: []string{}, Audience: []string{}, SensitiveTopics: []string{}, Issues: []ComplianceIssue{}, Status: "MISSING"}
	var candidates []ConsentGrant
	superseded := c.supersededConsentIDs()
	for _, grant := range c.Consents {
		if grant.ParticipantID == participantID && contains(grant.RecordingIDs, recording.ID) && !superseded[grant.ID] {
			candidates = append(candidates, grant)
		}
	}
	if len(candidates) == 0 {
		assessment.Issues = append(assessment.Issues, ComplianceIssue{Code: ComplianceMissingConsent, RecordingID: recording.ID, ParticipantID: participantID, Message: "参与者缺少适用于该录音的同意材料"})
		return assessment
	}
	var active []ConsentGrant
	for _, grant := range candidates {
		if grant.SignedAt.After(at) {
			assessment.Issues = append(assessment.Issues, ComplianceIssue{Code: ComplianceNotEffective, RecordingID: recording.ID, ParticipantID: participantID, Message: "适用授权尚未生效"})
			continue
		}
		if grant.WithdrawnAt != nil && !grant.WithdrawnAt.After(at) {
			assessment.Issues = append(assessment.Issues, ComplianceIssue{Code: ComplianceWithdrawn, RecordingID: recording.ID, ParticipantID: participantID, Message: "适用授权已撤回"})
			continue
		}
		if grant.ExpiresAt != nil && !grant.ExpiresAt.After(at) {
			assessment.Issues = append(assessment.Issues, ComplianceIssue{Code: ComplianceExpired, RecordingID: recording.ID, ParticipantID: participantID, Message: "适用授权已过期"})
			continue
		}
		active = append(active, grant)
	}
	if len(active) == 0 {
		if len(assessment.Issues) > 0 {
			assessment.Status = complianceStatus(assessment.Issues)
		}
		return assessment
	}
	assessment.AllowedPurposes, assessment.Audience = unionGrants(active)
	for _, grant := range active {
		assessment.GrantIDs = append(assessment.GrantIDs, grant.ID)
		assessment.SensitiveTopics = append(assessment.SensitiveTopics, grant.SensitiveTopics...)
		if grant.ExpiresAt != nil && (assessment.ExpiresAt == nil || grant.ExpiresAt.Before(*assessment.ExpiresAt)) {
			expires := grant.ExpiresAt.UTC()
			assessment.ExpiresAt = &expires
		}
	}
	assessment.GrantIDs = uniqueSorted(assessment.GrantIDs)
	assessment.SensitiveTopics = uniqueSorted(assessment.SensitiveTopics)
	if !contains(assessment.AllowedPurposes, c.Purpose) {
		assessment.Issues = append(assessment.Issues, ComplianceIssue{Code: CompliancePurpose, RecordingID: recording.ID, ParticipantID: participantID, Message: "研究开放目的超出参与者授权"})
	}
	if len(assessment.Audience) == 0 {
		assessment.Issues = append(assessment.Issues, ComplianceIssue{Code: ComplianceAudience, RecordingID: recording.ID, ParticipantID: participantID, Message: "同意材料没有定义访问人群"})
	}
	for _, topic := range recording.SensitiveTopics {
		if !contains(assessment.SensitiveTopics, topic) {
			assessment.Issues = append(assessment.Issues, ComplianceIssue{Code: ComplianceSensitive, RecordingID: recording.ID, ParticipantID: participantID, Topic: topic, Message: "敏感主题缺少明确授权"})
		}
	}
	assessment.Effective = len(assessment.Issues) == 0
	assessment.Status = "VALID"
	if !assessment.Effective {
		assessment.Status = "INVALID"
	}
	if assessment.Effective && assessment.ExpiresAt != nil && warningDays > 0 && !assessment.ExpiresAt.After(at.Add(time.Duration(warningDays)*24*time.Hour)) {
		assessment.ExpiringSoon = true
	}
	return assessment
}

func complianceStatus(issues []ComplianceIssue) string {
	for _, issue := range issues {
		if issue.Code == ComplianceWithdrawn {
			return "WITHDRAWN"
		}
	}
	for _, issue := range issues {
		if issue.Code == ComplianceExpired {
			return "EXPIRED"
		}
	}
	for _, issue := range issues {
		if issue.Code == ComplianceNotEffective {
			return "NOT_YET_EFFECTIVE"
		}
	}
	return "INVALID"
}

func ComplianceIssueKey(issue ComplianceIssue) string {
	return string(issue.Code) + "|" + issue.RecordingID + "|" + issue.ParticipantID + "|" + issue.Topic
}

func (c *ReleaseCase) supersededConsentIDs() map[string]bool {
	result := map[string]bool{}
	for _, grant := range c.Consents {
		if grant.SupersedesID != "" {
			result[grant.SupersedesID] = true
		}
	}
	return result
}
