package cancelled_save_recovery_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
	"oral-archive-release/internal/filestore"
)

type stagedCancelContext struct {
	context.Context
	target  int
	checks  int
	reached chan<- struct{}
	release <-chan struct{}
}

func (c *stagedCancelContext) Err() error {
	c.checks++
	if c.checks == c.target {
		c.reached <- struct{}{}
		<-c.release
	}
	return c.Context.Err()
}

func TestCancelledSaveDoesNotReappearAfterRestart(t *testing.T) {
	for cancelCheck := 2; cancelCheck <= 4; cancelCheck++ {
		t.Run(fmt.Sprintf("commit-stage-%d", cancelCheck-1), func(t *testing.T) {
			dir := t.TempDir()
			storeDir := filepath.Join(dir, "store")
			store, err := filestore.Open(storeDir)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)
			original, err := domain.NewReleaseCase("case-cancel", "collection", "取消边界", "research", "cat", now)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Save(context.Background(), original, 0, idem(t, "create", original, now)); err != nil {
				t.Fatal(err)
			}
			candidate, err := store.Get(context.Background(), original.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := candidate.AddParticipant(domain.Participant{ID: "p1", IdentityRef: "vault://p1"}, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}

			parent, cancel := context.WithCancel(context.Background())
			reached := make(chan struct{}, 1)
			release := make(chan struct{})
			ctx := &stagedCancelContext{Context: parent, target: cancelCheck, reached: reached, release: release}
			result := make(chan error, 1)
			go func() {
				result <- store.Save(ctx, candidate, original.Version, idem(t, "participant", candidate, now.Add(time.Minute)))
			}()

			var saveErr error
			select {
			case saveErr = <-result:
				if saveErr != nil {
					t.Fatalf("未到达取消屏障时 Save 意外失败: %v", saveErr)
				}
				return
			case <-reached:
				cancel()
				close(release)
				saveErr = <-result
			}
			if !errors.Is(saveErr, context.Canceled) {
				t.Fatalf("Save 应返回 context.Canceled，实际为 %v", saveErr)
			}
			current, err := store.Get(context.Background(), original.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Version != original.Version {
				t.Fatalf("取消返回后当前进程不应发布版本 %d", current.Version)
			}

			reopened, err := filestore.Open(storeDir)
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := reopened.Get(context.Background(), original.ID)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.Version != original.Version || len(recovered.Participants) != 0 {
				t.Errorf("TestCancelledSaveDoesNotReappearAfterRestart: 取消的版本在重启后被恢复：version=%d participants=%d", recovered.Version, len(recovered.Participants))
			}
		})
	}
}

func idem(t *testing.T, key string, c *domain.ReleaseCase, at time.Time) *application.IdempotencyRecord {
	t.Helper()
	result, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return &application.IdempotencyRecord{
		Key:         key,
		PayloadHash: key + "-hash",
		CaseID:      c.ID,
		CaseVersion: c.Version,
		Result:      result,
		CreatedAt:   at,
	}
}
