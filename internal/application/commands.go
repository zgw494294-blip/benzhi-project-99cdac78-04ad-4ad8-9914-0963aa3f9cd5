package application

import (
	"time"

	"oral-archive-release/internal/domain"
)

const (
	RoleCataloger = "CATALOGER"
	RoleReviewer  = "REVIEWER"
	RoleOfficer   = "RELEASE_OFFICER"
)

type Meta struct {
	ActorID         string `json:"actorId"`
	ActorRole       string `json:"actorRole"`
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type CreateCase struct {
	ActorID        string `json:"actorId"`
	ActorRole      string `json:"actorRole"`
	IdempotencyKey string `json:"idempotencyKey"`
	CollectionID   string `json:"collectionId"`
	Title          string `json:"title"`
	Purpose        string `json:"purpose"`
	CatalogerID    string `json:"catalogerId"`
}

type AddParticipant struct {
	Meta
	Participant domain.Participant `json:"participant"`
}

type AddRecording struct {
	Meta
	Recording domain.RecordingItem `json:"recording"`
}

type AddConsent struct {
	Meta
	Consent domain.ConsentGrant `json:"consent"`
}

type ReviseCase struct {
	Meta
	Changes        *domain.CaseRevision `json:"changes,omitempty"`
	Title          *string              `json:"title,omitempty"`
	Purpose        *string              `json:"purpose,omitempty"`
	CatalogerID    *string              `json:"catalogerId,omitempty"`
	TransferReason string               `json:"transferReason,omitempty"`
}

func (c ReviseCase) Revision() domain.CaseRevision {
	if c.Changes != nil {
		return *c.Changes
	}
	return domain.CaseRevision{Title: c.Title, Purpose: c.Purpose, CatalogerID: c.CatalogerID, TransferReason: c.TransferReason}
}

type CatalogBatch struct {
	Meta
	Batch        *domain.CatalogBatch   `json:"batch,omitempty"`
	Participants []domain.Participant   `json:"participants,omitempty"`
	Recordings   []domain.RecordingItem `json:"recordings,omitempty"`
	Consents     []domain.ConsentGrant  `json:"consents,omitempty"`
}

func (c CatalogBatch) Items() domain.CatalogBatch {
	if c.Batch != nil {
		return *c.Batch
	}
	return domain.CatalogBatch{Participants: c.Participants, Recordings: c.Recordings, Consents: c.Consents}
}

type CatalogBatchResult struct {
	Accepted domain.BatchCounts  `json:"accepted"`
	Case     *domain.ReleaseCase `json:"case"`
}

type AddFinding struct {
	Meta
	Finding domain.ReviewFinding `json:"finding"`
}

type AddRevision struct {
	Meta
	RecordingID string                   `json:"recordingId"`
	Revision    domain.RecordingRevision `json:"revision"`
	Evidence    string                   `json:"evidence"`
}

type ReviewFinding struct {
	Meta
	FindingID         string `json:"findingId"`
	Evidence          string `json:"evidence"`
	Accepted          bool   `json:"accepted"`
	EvidencePackageID string `json:"evidencePackageId,omitempty"`
	ReviewOpinion     string `json:"reviewOpinion,omitempty"`
}

type ImportComplianceFindings struct {
	Meta
	IssueKeys  []string  `json:"issueKeys,omitempty"`
	EvaluateAt time.Time `json:"evaluateAt,omitempty"`
}

type ImportComplianceResult struct {
	Created []domain.ReviewFinding `json:"created"`
	Skipped []domain.ReviewFinding `json:"skipped"`
	Case    *domain.ReleaseCase    `json:"case"`
}

type SubmitEvidencePackage struct {
	Meta
	EvidencePackage domain.EvidenceSubmission `json:"evidencePackage"`
}

type Decision struct {
	Meta
	Approve         bool   `json:"approve"`
	Reason          string `json:"reason"`
	ReadinessDigest string `json:"readinessDigest,omitempty"`
}

type IssueCredential struct{ Meta }

type Verification struct {
	Valid            bool      `json:"valid"`
	CredentialNo     string    `json:"credentialNo"`
	CaseID           string    `json:"caseId"`
	ManifestDigest   string    `json:"manifestDigest"`
	CredentialDigest string    `json:"credentialDigest"`
	ManifestValid    bool      `json:"manifestValid"`
	AuditChainValid  bool      `json:"auditChainValid"`
	Problems         []string  `json:"problems"`
	VerifiedAt       time.Time `json:"verifiedAt"`
}

type ComplianceQuery struct {
	EvaluateAt  time.Time
	WarningDays int
}
