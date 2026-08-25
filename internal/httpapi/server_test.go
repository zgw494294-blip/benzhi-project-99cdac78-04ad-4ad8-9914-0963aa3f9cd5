package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/auditlog"
	"oral-archive-release/internal/filestore"
	"oral-archive-release/internal/httpapi"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	repo, err := filestore.Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	audit, err := auditlog.Open(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	return httpapi.NewHandler(application.NewService(repo, audit))
}

func TestRevisionBatchAndTimedComplianceRoutes(t *testing.T) {
	handler := newTestHandler(t)
	createBody := []byte(`{"actorId":"cat-a","actorRole":"CATALOGER","idempotencyKey":"ext-create","collectionId":"collection","title":"原题","purpose":"research","catalogerId":"cat-a"}`)
	created := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/release-cases", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(created, req)
	var createResult struct {
		Data struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createResult); err != nil {
		t.Fatal(err)
	}
	caseURL := "/api/v1/release-cases/" + createResult.Data.ID
	title := "修订题"
	patchBody, _ := json.Marshal(map[string]any{"actorId": "cat-a", "actorRole": "CATALOGER", "expectedVersion": createResult.Data.Version, "idempotencyKey": "ext-edit", "title": title, "catalogerId": "cat-b", "transferReason": "轮岗"})
	edited := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, caseURL, bytes.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(edited, req)
	if edited.Code != http.StatusOK {
		t.Fatalf("edit status=%d body=%s", edited.Code, edited.Body.String())
	}
	var editResult struct {
		Data struct {
			Version     int64  `json:"version"`
			CatalogerID string `json:"catalogerId"`
		} `json:"data"`
	}
	_ = json.Unmarshal(edited.Body.Bytes(), &editResult)
	expires := time.Now().UTC().Add(10 * 24 * time.Hour).Format(time.RFC3339)
	signed := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	batchBody, _ := json.Marshal(map[string]any{"actorId": "cat-b", "actorRole": "CATALOGER", "expectedVersion": editResult.Data.Version, "idempotencyKey": "ext-batch", "participants": []any{map[string]any{"id": "p1", "identityRef": "vault://p1"}}, "recordings": []any{map[string]any{"id": "r1", "catalogRef": "R1", "participantIds": []string{"p1"}, "durationSeconds": 1, "contentDigest": "rd"}}, "consents": []any{map[string]any{"id": "g1", "participantId": "p1", "recordingIds": []string{"r1"}, "allowedPurposes": []string{"research"}, "audience": []string{"researchers"}, "signedAt": signed, "expiresAt": expires, "documentDigest": "gd"}}})
	batched := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, caseURL+"/catalog-batches", bytes.NewReader(batchBody))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(batched, req)
	if batched.Code != http.StatusOK {
		t.Fatalf("batch status=%d body=%s", batched.Code, batched.Body.String())
	}
	compliance := httptest.NewRecorder()
	handler.ServeHTTP(compliance, httptest.NewRequest(http.MethodGet, caseURL+"/compliance?warningDays=30", nil))
	if compliance.Code != http.StatusOK || !bytes.Contains(compliance.Body.Bytes(), []byte(`"expiringSoon":true`)) {
		t.Fatalf("compliance status=%d body=%s", compliance.Code, compliance.Body.String())
	}
	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, caseURL+"/compliance?evaluateAt=bad", nil))
	if invalid.Code != http.StatusBadRequest || !bytes.Contains(invalid.Body.Bytes(), []byte("INVALID_EVALUATION_TIME")) {
		t.Fatalf("invalid query status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestCreateAndReadCase(t *testing.T) {
	handler := newTestHandler(t)
	body := []byte(`{"actorId":"cat","actorRole":"CATALOGER","idempotencyKey":"http-create","collectionId":"collection","title":"案件","purpose":"research","catalogerId":"cat"}`)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/release-cases", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing request id header")
	}
	var result struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.ID == "" || result.Data.Status != "DRAFT" || result.RequestID == "" {
		t.Fatalf("bad response: %+v", result)
	}
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/release-cases/"+result.Data.ID, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
}

func TestStrictJSONAndStableError(t *testing.T) {
	handler := newTestHandler(t)
	body := []byte(`{"actorId":"cat","actorRole":"CATALOGER","idempotencyKey":"strict","collectionId":"collection","title":"案件","purpose":"research","catalogerId":"cat","unexpected":true}`)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/release-cases", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != "UNKNOWN_FIELD" || result.RequestID == "" {
		t.Fatalf("bad error: %+v", result)
	}
}
