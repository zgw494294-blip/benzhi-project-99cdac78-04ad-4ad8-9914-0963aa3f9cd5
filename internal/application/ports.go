package application

import (
	"context"
	"encoding/json"
	"time"

	"oral-archive-release/internal/domain"
)

type IdempotencyRecord struct {
	Key         string          `json:"key"`
	PayloadHash string          `json:"payloadHash"`
	CaseID      string          `json:"caseId"`
	CaseVersion int64           `json:"caseVersion"`
	Result      json.RawMessage `json:"result"`
	CreatedAt   time.Time       `json:"createdAt"`
}

type Repository interface {
	Get(context.Context, string) (*domain.ReleaseCase, error)
	GetByCredential(context.Context, string) (*domain.ReleaseCase, error)
	Save(context.Context, *domain.ReleaseCase, int64, *IdempotencyRecord) error
	LookupIdempotency(context.Context, string) (*IdempotencyRecord, error)
}

type AuditEvent struct {
	Sequence       uint64    `json:"sequence"`
	CaseID         string    `json:"caseId"`
	ActorID        string    `json:"actorId"`
	ActorRole      string    `json:"actorRole"`
	ObjectVersion  int64     `json:"objectVersion"`
	Action         string    `json:"action"`
	At             time.Time `json:"at"`
	Details        string    `json:"details,omitempty"`
	PreviousDigest string    `json:"previousDigest"`
	Digest         string    `json:"digest"`
}

type AuditPort interface {
	Append(context.Context, AuditEvent) error
	Timeline(context.Context, string) ([]AuditEvent, error)
	IssueCredential(context.Context, string, string, string, time.Time) (domain.ReleaseCredential, error)
	Credential(context.Context, string) (domain.ReleaseCredential, error)
	Verify(context.Context) (bool, []string, error)
	CredentialSegment(context.Context, uint64, int) ([]domain.ReleaseCredential, error)
}
