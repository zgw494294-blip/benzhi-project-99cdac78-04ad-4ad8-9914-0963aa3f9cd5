package stale_compliance_cache_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/auditlog"
	"oral-archive-release/internal/filestore"
	"oral-archive-release/internal/httpapi"
)

type response struct {
	Data struct {
		ID          string `json:"id"`
		Version     int64  `json:"version"`
		CaseVersion int64  `json:"caseVersion"`
		Complete    bool   `json:"complete"`
		Issues      []any  `json:"issues"`
	} `json:"data"`
}

func TestExplicitComplianceCacheTracksCaseVersion(t *testing.T) {
	dir := t.TempDir()
	repo, err := filestore.Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	audit, err := auditlog.Open(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandler(application.NewService(repo, audit))

	created := request(t, handler, http.MethodPost, "/api/v1/release-cases", map[string]any{
		"actorId": "cat", "actorRole": "CATALOGER", "idempotencyKey": "cache-create",
		"collectionId": "collection", "title": "案件", "purpose": "research", "catalogerId": "cat",
	})
	caseID := created.Data.ID
	queryURL := "/api/v1/release-cases/" + caseID + "/compliance?evaluateAt=2026-08-25T08%3A00%3A00Z"
	initial := request(t, handler, http.MethodGet, queryURL, nil)
	if initial.Data.CaseVersion != 1 || initial.Data.Complete || len(initial.Data.Issues) != 0 {
		t.Fatalf("unexpected initial report: %+v", initial.Data)
	}

	participant := request(t, handler, http.MethodPost, "/api/v1/release-cases/"+caseID+"/participants", map[string]any{
		"actorId": "cat", "actorRole": "CATALOGER", "expectedVersion": int64(1), "idempotencyKey": "cache-participant",
		"participant": map[string]any{"id": "p1", "identityRef": "vault://p1"},
	})
	request(t, handler, http.MethodPost, "/api/v1/release-cases/"+caseID+"/recordings", map[string]any{
		"actorId": "cat", "actorRole": "CATALOGER", "expectedVersion": participant.Data.Version, "idempotencyKey": "cache-recording",
		"recording": map[string]any{"id": "r1", "catalogRef": "R1", "participantIds": []string{"p1"}, "durationSeconds": 60, "contentDigest": "recording-digest"},
	})

	updated := request(t, handler, http.MethodGet, queryURL, nil)
	if updated.Data.CaseVersion != 3 || updated.Data.Complete || len(updated.Data.Issues) != 1 {
		t.Fatalf("explicit compliance query reused stale case version: got version=%d complete=%v issues=%d, want version=3 complete=false issues=1", updated.Data.CaseVersion, updated.Data.Complete, len(updated.Data.Issues))
	}
}

func request(t *testing.T, handler http.Handler, method, target string, body any) response {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	handler.ServeHTTP(recorder, req)
	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf("%s %s returned %d: %s", method, target, recorder.Code, recorder.Body.String())
	}
	var result response
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
