package parallelmutationauditleak

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
)

var errPersistenceUnavailable = errors.New("controlled persistence failure")

type failingRepository struct {
	caseSnapshot *domain.ReleaseCase
	saveStarted  chan struct{}
	releaseSave  chan struct{}
	once         sync.Once
}

func (r *failingRepository) Get(context.Context, string) (*domain.ReleaseCase, error) {
	data, err := json.Marshal(r.caseSnapshot)
	if err != nil {
		return nil, err
	}
	var snapshot domain.ReleaseCase
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *failingRepository) GetByCredential(context.Context, string) (*domain.ReleaseCase, error) {
	return nil, domain.NewError("CREDENTIAL_NOT_FOUND", "访问凭据不存在")
}

func (r *failingRepository) Save(context.Context, *domain.ReleaseCase, int64, *application.IdempotencyRecord) error {
	r.once.Do(func() { close(r.saveStarted) })
	<-r.releaseSave
	return errPersistenceUnavailable
}

func (r *failingRepository) LookupIdempotency(context.Context, string) (*application.IdempotencyRecord, error) {
	return nil, domain.NewError("IDEMPOTENCY_NOT_FOUND", "幂等记录不存在")
}

type recordingAudit struct {
	saveStarted <-chan struct{}
	events      chan application.AuditEvent
}

func (a *recordingAudit) Append(_ context.Context, event application.AuditEvent) error {
	<-a.saveStarted
	a.events <- event
	return nil
}

func (a *recordingAudit) Timeline(context.Context, string) ([]application.AuditEvent, error) {
	var result []application.AuditEvent
	for {
		select {
		case event := <-a.events:
			result = append(result, event)
		default:
			return result, nil
		}
	}
}

func (a *recordingAudit) IssueCredential(context.Context, string, string, string, time.Time) (domain.ReleaseCredential, error) {
	return domain.ReleaseCredential{}, errors.New("not used")
}

func (a *recordingAudit) Credential(context.Context, string) (domain.ReleaseCredential, error) {
	return domain.ReleaseCredential{}, domain.NewError("CREDENTIAL_NOT_FOUND", "访问凭据不存在")
}

func (a *recordingAudit) Verify(context.Context) (bool, []string, error) {
	return true, nil, nil
}

func (a *recordingAudit) CredentialSegment(context.Context, uint64, int) ([]domain.ReleaseCredential, error) {
	return nil, errors.New("not used")
}

func TestFailedMutationDoesNotPublishAuditEvent(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	caseSnapshot, err := domain.NewReleaseCase("case-controlled", "collection", "案件", "research", "cataloger-a", now)
	if err != nil {
		t.Fatal(err)
	}
	repo := &failingRepository{
		caseSnapshot: caseSnapshot,
		saveStarted:  make(chan struct{}),
		releaseSave:  make(chan struct{}),
	}
	audit := &recordingAudit{saveStarted: repo.saveStarted, events: make(chan application.AuditEvent, 1)}
	service := application.NewService(repo, audit)

	finished := make(chan error, 1)
	go func() {
		_, callErr := service.AddParticipant(context.Background(), caseSnapshot.ID, application.AddParticipant{
			Meta: application.Meta{
				ActorID: "cataloger-a", ActorRole: application.RoleCataloger,
				ExpectedVersion: 1, IdempotencyKey: "participant-controlled",
			},
			Participant: domain.Participant{ID: "participant-1", IdentityRef: "vault://participant-1"},
		})
		finished <- callErr
	}()

	<-repo.saveStarted
	close(repo.releaseSave)
	if callErr := <-finished; !errors.Is(callErr, errPersistenceUnavailable) {
		t.Fatalf("expected controlled persistence error, got %v", callErr)
	}
	events, err := audit.Timeline(context.Background(), caseSnapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("failed mutation published audit event: action=%s objectVersion=%d persistedVersion=%d",
			events[0].Action, events[0].ObjectVersion, caseSnapshot.Version)
	}
}
