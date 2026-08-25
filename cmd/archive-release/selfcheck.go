package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"oral-archive-release/internal/domain"
)

type apiEnvelope struct {
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"requestId"`
	Error     *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func runSelfcheck(parent context.Context, baseURL string) error {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second}
	unique := fmt.Sprintf("%d", time.Now().UnixNano())
	var c domain.ReleaseCase
	if err := postJSON(ctx, client, baseURL+"/api/v1/release-cases", map[string]any{"actorId": "cataloger-selfcheck", "actorRole": "CATALOGER", "idempotencyKey": "self-create-" + unique, "collectionId": "collection-selfcheck", "title": "自检口述录音开放案件", "purpose": "linguistic-research", "catalogerId": "cataloger-selfcheck"}, &c); err != nil {
		return err
	}
	caseURL := baseURL + "/api/v1/release-cases/" + c.ID
	if err := postJSON(ctx, client, caseURL+"/participants", map[string]any{"actorId": "cataloger-selfcheck", "actorRole": "CATALOGER", "expectedVersion": c.Version, "idempotencyKey": "self-participant-" + unique, "participant": map[string]any{"id": "participant-1", "identityRef": "vault://participants/selfcheck-1", "displayCode": "P-001"}}, &c); err != nil {
		return err
	}
	if err := postJSON(ctx, client, caseURL+"/recordings", map[string]any{"actorId": "cataloger-selfcheck", "actorRole": "CATALOGER", "expectedVersion": c.Version, "idempotencyKey": "self-recording-" + unique, "recording": map[string]any{"id": "recording-1", "catalogRef": "AR-SC-001", "participantIds": []string{"participant-1"}, "durationSeconds": 90, "languageTags": []string{"cmn"}, "sensitiveTopics": []string{}, "contentDigest": "sha256:selfcheck-original"}}, &c); err != nil {
		return err
	}
	signed := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	expires := time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339)
	if err := postJSON(ctx, client, caseURL+"/consents", map[string]any{"actorId": "cataloger-selfcheck", "actorRole": "CATALOGER", "expectedVersion": c.Version, "idempotencyKey": "self-consent-" + unique, "consent": map[string]any{"id": "consent-1", "participantId": "participant-1", "recordingIds": []string{"recording-1"}, "allowedPurposes": []string{"linguistic-research"}, "audience": []string{"accredited-researchers"}, "signedAt": signed, "expiresAt": expires, "documentDigest": "sha256:selfcheck-consent"}}, &c); err != nil {
		return err
	}
	if err := postJSON(ctx, client, caseURL+"/submit-review", writeMeta("cataloger-selfcheck", "CATALOGER", c.Version, "self-submit-"+unique), &c); err != nil {
		return err
	}
	if err := postJSON(ctx, client, caseURL+"/findings", map[string]any{"actorId": "reviewer-selfcheck", "actorRole": "REVIEWER", "expectedVersion": c.Version, "idempotencyKey": "self-finding-" + unique, "finding": map[string]any{"id": "finding-1", "kind": "SENSITIVE_EXPOSURE", "severity": "MAJOR", "subjectType": "recording", "subjectId": "recording-1", "description": "需要对可识别地名进行脱敏"}}, &c); err != nil {
		return err
	}
	if err := postJSON(ctx, client, caseURL+"/revisions", map[string]any{"actorId": "cataloger-selfcheck", "actorRole": "CATALOGER", "expectedVersion": c.Version, "idempotencyKey": "self-revision-" + unique, "recordingId": "recording-1", "evidence": "revision:recording-1-r2", "revision": map[string]any{"id": "recording-1-r2", "contentDigest": "sha256:selfcheck-redacted", "redactionDigest": "sha256:selfcheck-redaction-map", "summary": "移除可识别地名"}}, &c); err != nil {
		return err
	}
	if err := postJSON(ctx, client, caseURL+"/findings/finding-1/review", map[string]any{"actorId": "reviewer-selfcheck", "actorRole": "REVIEWER", "expectedVersion": c.Version, "idempotencyKey": "self-review-" + unique, "findingId": "finding-1", "evidence": "revision:recording-1-r2", "accepted": true}, &c); err != nil {
		return err
	}
	if err := postJSON(ctx, client, caseURL+"/decision", map[string]any{"actorId": "officer-selfcheck", "actorRole": "RELEASE_OFFICER", "expectedVersion": c.Version, "idempotencyKey": "self-approve-" + unique, "approve": true, "reason": "同意范围明确且整改已复核"}, &c); err != nil {
		return err
	}
	if err := postJSON(ctx, client, caseURL+"/freeze", writeMeta("officer-selfcheck", "RELEASE_OFFICER", c.Version, "self-freeze-"+unique), &c); err != nil {
		return err
	}
	var credential domain.ReleaseCredential
	if err := postJSON(ctx, client, caseURL+"/credentials", writeMeta("officer-selfcheck", "RELEASE_OFFICER", c.Version, "self-issue-"+unique), &credential); err != nil {
		return err
	}
	var verification struct {
		Valid           bool `json:"valid"`
		AuditChainValid bool `json:"auditChainValid"`
	}
	if err := getJSON(ctx, client, baseURL+"/api/v1/credentials/"+credential.CredentialNo+"/verify", &verification); err != nil {
		return err
	}
	if !verification.Valid || !verification.AuditChainValid {
		return fmt.Errorf("凭据核验未通过")
	}
	var timeline []any
	if err := getJSON(ctx, client, caseURL+"/timeline", &timeline); err != nil {
		return err
	}
	if len(timeline) < 10 {
		return fmt.Errorf("审计时间线事件不足：%d", len(timeline))
	}
	return nil
}

func writeMeta(actor, role string, version int64, key string) map[string]any {
	return map[string]any{"actorId": actor, "actorRole": role, "expectedVersion": version, "idempotencyKey": key}
}

func postJSON(ctx context.Context, client *http.Client, url string, input, output any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return execute(client, req, output)
}

func getJSON(ctx context.Context, client *http.Client, url string, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return execute(client, req, output)
}

func execute(client *http.Client, req *http.Request, output any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	var envelope apiEnvelope
	if err := json.Unmarshal(b, &envelope); err != nil {
		return fmt.Errorf("解析 HTTP %d 响应: %w", resp.StatusCode, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if envelope.Error != nil {
			return fmt.Errorf("HTTP %d %s: %s", resp.StatusCode, envelope.Error.Code, envelope.Error.Message)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if output != nil {
		if err := json.Unmarshal(envelope.Data, output); err != nil {
			return fmt.Errorf("解析响应 data: %w", err)
		}
	}
	return nil
}
