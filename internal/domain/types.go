package domain

import "time"

type CaseStatus string

const (
	StatusDraft           CaseStatus = "DRAFT"
	StatusInReview        CaseStatus = "IN_REVIEW"
	StatusChangesRequired CaseStatus = "CHANGES_REQUIRED"
	StatusApproved        CaseStatus = "APPROVED"
	StatusFrozen          CaseStatus = "FROZEN"
	StatusReleased        CaseStatus = "RELEASED"
)

type ReleaseCase struct {
	ID               string             `json:"id"`
	CollectionID     string             `json:"collectionId"`
	Title            string             `json:"title"`
	Purpose          string             `json:"purpose"`
	CatalogerID      string             `json:"catalogerId"`
	Status           CaseStatus         `json:"status"`
	Version          int64              `json:"version"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
	Participants     []Participant      `json:"participants"`
	Recordings       []RecordingItem    `json:"recordings"`
	Consents         []ConsentGrant     `json:"consents"`
	Findings         []ReviewFinding    `json:"findings"`
	EvidencePackages []EvidencePackage  `json:"evidencePackages"`
	Decision         *ApprovalDecision  `json:"decision,omitempty"`
	Manifest         *ReleaseManifest   `json:"manifest,omitempty"`
	Credential       *ReleaseCredential `json:"credential,omitempty"`
}

type Participant struct {
	ID          string `json:"id"`
	IdentityRef string `json:"identityRef"`
	DisplayCode string `json:"displayCode"`
}

type RecordingItem struct {
	ID                string              `json:"id"`
	CaseID            string              `json:"caseId"`
	CatalogRef        string              `json:"catalogRef"`
	ParticipantIDs    []string            `json:"participantIds"`
	DurationSeconds   int                 `json:"durationSeconds"`
	LanguageTags      []string            `json:"languageTags"`
	SensitiveTopics   []string            `json:"sensitiveTopics"`
	CurrentRevisionID string              `json:"currentRevisionId"`
	ContentDigest     string              `json:"contentDigest"`
	Revisions         []RecordingRevision `json:"revisions"`
}

type RecordingRevision struct {
	ID              string    `json:"id"`
	ParentID        string    `json:"parentId,omitempty"`
	ContentDigest   string    `json:"contentDigest"`
	RedactionDigest string    `json:"redactionDigest,omitempty"`
	Summary         string    `json:"summary"`
	CreatedBy       string    `json:"createdBy"`
	CreatedAt       time.Time `json:"createdAt"`
}

type ConsentGrant struct {
	ID              string     `json:"id"`
	CaseID          string     `json:"caseId"`
	ParticipantID   string     `json:"participantId"`
	RecordingIDs    []string   `json:"recordingIds"`
	AllowedPurposes []string   `json:"allowedPurposes"`
	Audience        []string   `json:"audience"`
	SignedAt        time.Time  `json:"signedAt"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	WithdrawnAt     *time.Time `json:"withdrawnAt,omitempty"`
	DocumentDigest  string     `json:"documentDigest"`
	SupersedesID    string     `json:"supersedesId,omitempty"`
	SensitiveTopics []string   `json:"sensitiveTopics,omitempty"`
}

type FindingKind string

const (
	FindingMissingConsent    FindingKind = "MISSING_CONSENT"
	FindingPurposeExceeded   FindingKind = "PURPOSE_EXCEEDED"
	FindingExpiredGrant      FindingKind = "EXPIRED_GRANT"
	FindingWithdrawal        FindingKind = "WITHDRAWAL_CONFLICT"
	FindingSensitiveExposure FindingKind = "SENSITIVE_EXPOSURE"
)

type ReviewFinding struct {
	ID                  string      `json:"id"`
	CaseID              string      `json:"caseId"`
	Kind                FindingKind `json:"kind"`
	Severity            string      `json:"severity"`
	SubjectType         string      `json:"subjectType"`
	SubjectID           string      `json:"subjectId"`
	RecordingID         string      `json:"recordingId,omitempty"`
	ParticipantID       string      `json:"participantId,omitempty"`
	Topic               string      `json:"topic,omitempty"`
	DedupKey            string      `json:"dedupKey,omitempty"`
	SourceCaseVersion   int64       `json:"sourceCaseVersion,omitempty"`
	SourceImportKey     string      `json:"sourceImportKey,omitempty"`
	Description         string      `json:"description"`
	Status              string      `json:"status"`
	RemediationEvidence string      `json:"remediationEvidence,omitempty"`
	ReviewedBy          string      `json:"reviewedBy,omitempty"`
	ReviewedAt          *time.Time  `json:"reviewedAt,omitempty"`
	EvidencePackageID   string      `json:"evidencePackageId,omitempty"`
	ReviewOpinion       string      `json:"reviewOpinion,omitempty"`
}

// EvidencePackage 将一次整改提交与其覆盖的审查问题固定关联。
type EvidencePackage struct {
	ID                string            `json:"id"`
	Description       string            `json:"description"`
	FindingIDs        []string          `json:"findingIds"`
	ConsentIDs        []string          `json:"consentIds"`
	RevisionIDs       []string          `json:"revisionIds"`
	MaterialSummaries []EvidenceSummary `json:"materialSummaries"`
	RevisionSummaries []EvidenceSummary `json:"revisionSummaries"`
	SubmittedBy       string            `json:"submittedBy"`
	SubmittedAt       time.Time         `json:"submittedAt"`
}

type EvidenceSummary struct {
	ID        string `json:"id"`
	SubjectID string `json:"subjectId"`
	Digest    string `json:"digest"`
	Summary   string `json:"summary,omitempty"`
}

type AccessScope struct {
	RecordingID     string     `json:"recordingId"`
	AllowedPurposes []string   `json:"allowedPurposes"`
	Audience        []string   `json:"audience"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	SensitiveTopics []string   `json:"sensitiveTopics,omitempty"`
	ExpiringSoon    bool       `json:"expiringSoon,omitempty"`
	Valid           bool       `json:"valid"`
	Reasons         []string   `json:"reasons,omitempty"`
}

type ApprovalDecision struct {
	Decision        string    `json:"decision"`
	ActorID         string    `json:"actorId"`
	Reason          string    `json:"reason"`
	At              time.Time `json:"at"`
	ReadinessDigest string    `json:"readinessDigest,omitempty"`
}

type ReleaseManifest struct {
	CaseID       string              `json:"caseId"`
	CaseVersion  int64               `json:"caseVersion"`
	CreatedAt    time.Time           `json:"createdAt"`
	Recordings   []ManifestRecording `json:"recordings"`
	Consents     []ManifestConsent   `json:"consents"`
	AccessScopes []AccessScope       `json:"accessScopes"`
	Decision     ApprovalDecision    `json:"decision"`
	Digest       string              `json:"digest"`
}

type ManifestRecording struct {
	ID             string   `json:"id"`
	RevisionID     string   `json:"revisionId"`
	ContentDigest  string   `json:"contentDigest"`
	ParticipantIDs []string `json:"participantIds"`
}

type ManifestConsent struct {
	ID             string   `json:"id"`
	ParticipantID  string   `json:"participantId"`
	DocumentDigest string   `json:"documentDigest"`
	RecordingIDs   []string `json:"recordingIds,omitempty"`
	SupersedesID   string   `json:"supersedesId,omitempty"`
	Adopted        bool     `json:"adopted,omitempty"`
	AdoptionStatus string   `json:"adoptionStatus,omitempty"`
}

type ReleaseCredential struct {
	CredentialNo     string    `json:"credentialNo"`
	CaseID           string    `json:"caseId"`
	ManifestDigest   string    `json:"manifestDigest"`
	PreviousDigest   string    `json:"previousDigest"`
	CredentialDigest string    `json:"credentialDigest"`
	Sequence         uint64    `json:"sequence"`
	IssuedBy         string    `json:"issuedBy"`
	IssuedAt         time.Time `json:"issuedAt"`
}
