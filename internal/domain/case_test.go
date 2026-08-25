package domain

import (
	"testing"
	"time"
)

func TestReleaseWorkflowAndDeterministicManifest(t *testing.T) {
	now := time.Date(2026, 8, 25, 1, 2, 3, 0, time.UTC)
	c, err := NewReleaseCase("case-1", "collection-1", "测试案件", "research", "cataloger-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddParticipant(Participant{ID: "p1", IdentityRef: "vault://p1"}, now); err != nil {
		t.Fatal(err)
	}
	if err := c.AddRecording(RecordingItem{ID: "r1", CatalogRef: "AR-1", ParticipantIDs: []string{"p1"}, DurationSeconds: 12, LanguageTags: []string{"cmn"}, ContentDigest: "sha256:recording"}, "cataloger-1", now); err != nil {
		t.Fatal(err)
	}
	expires := now.Add(24 * time.Hour)
	if err := c.AddConsent(ConsentGrant{ID: "g1", ParticipantID: "p1", RecordingIDs: []string{"r1"}, AllowedPurposes: []string{"research"}, Audience: []string{"researchers"}, SignedAt: now.Add(-time.Hour), ExpiresAt: &expires, DocumentDigest: "sha256:consent"}, now); err != nil {
		t.Fatal(err)
	}
	if issues := c.IntegrityIssues(now); len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if err := c.SubmitReview(now); err != nil {
		t.Fatal(err)
	}
	if err := c.Decide(true, "officer-1", "范围清晰", now); err != nil {
		t.Fatal(err)
	}
	manifest, err := c.Freeze(now)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := ManifestDigest(*manifest)
	if err != nil {
		t.Fatal(err)
	}
	if digest != manifest.Digest {
		t.Fatalf("manifest digest differs: %s != %s", digest, manifest.Digest)
	}
	if err := c.ValidateSnapshot(); err != nil {
		t.Fatal(err)
	}
}

func TestSensitiveTopicRequiresExplicitGrant(t *testing.T) {
	now := time.Now().UTC()
	c, _ := NewReleaseCase("case-1", "collection-1", "测试", "research", "cataloger", now)
	_ = c.AddParticipant(Participant{ID: "p1", IdentityRef: "vault://p1"}, now)
	_ = c.AddRecording(RecordingItem{ID: "r1", CatalogRef: "AR-1", ParticipantIDs: []string{"p1"}, DurationSeconds: 5, SensitiveTopics: []string{"ritual"}, ContentDigest: "digest"}, "cataloger", now)
	_ = c.AddConsent(ConsentGrant{ID: "g1", ParticipantID: "p1", RecordingIDs: []string{"r1"}, AllowedPurposes: []string{"research"}, Audience: []string{"researchers"}, SignedAt: now.Add(-time.Hour), DocumentDigest: "consent"}, now)
	report := c.EvaluateCompliance(now)
	if report.Releasable {
		t.Fatal("sensitive recording unexpectedly releasable")
	}
	found := false
	for _, issue := range report.Issues {
		if issue.Code == ComplianceSensitive {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing sensitive issue: %+v", report.Issues)
	}
}

func TestApprovalRejectsOpenFinding(t *testing.T) {
	now := time.Now().UTC()
	c, _ := NewReleaseCase("case-1", "collection-1", "测试", "research", "cataloger", now)
	_ = c.AddParticipant(Participant{ID: "p1", IdentityRef: "vault://p1"}, now)
	_ = c.AddRecording(RecordingItem{ID: "r1", CatalogRef: "AR-1", ParticipantIDs: []string{"p1"}, DurationSeconds: 5, ContentDigest: "digest"}, "cataloger", now)
	_ = c.AddConsent(ConsentGrant{ID: "g1", ParticipantID: "p1", RecordingIDs: []string{"r1"}, AllowedPurposes: []string{"research"}, Audience: []string{"researchers"}, SignedAt: now.Add(-time.Hour), DocumentDigest: "consent"}, now)
	_ = c.SubmitReview(now)
	_ = c.AddFinding(ReviewFinding{ID: "f1", Kind: FindingSensitiveExposure, SubjectType: "recording", SubjectID: "r1", Description: "需要脱敏"}, now)
	if err := c.Decide(true, "officer", "批准", now); err == nil {
		t.Fatal("approval should reject open finding")
	}
}

func TestRejectedRemediationCanBeReviewedAgain(t *testing.T) {
	now := time.Now().UTC()
	c, _ := NewReleaseCase("case-1", "collection-1", "测试", "research", "cataloger", now)
	_ = c.AddParticipant(Participant{ID: "p1", IdentityRef: "vault://p1"}, now)
	_ = c.AddRecording(RecordingItem{ID: "r1", CatalogRef: "AR-1", ParticipantIDs: []string{"p1"}, DurationSeconds: 5, ContentDigest: "digest"}, "cataloger", now)
	_ = c.AddConsent(ConsentGrant{ID: "g1", ParticipantID: "p1", RecordingIDs: []string{"r1"}, AllowedPurposes: []string{"research"}, Audience: []string{"researchers"}, SignedAt: now.Add(-time.Hour), DocumentDigest: "consent"}, now)
	_ = c.SubmitReview(now)
	_ = c.AddFinding(ReviewFinding{ID: "f1", Kind: FindingSensitiveExposure, SubjectType: "recording", SubjectID: "r1", Description: "需要脱敏"}, now)
	if err := c.ReviewFinding("f1", "首次证据", "reviewer", false, now); err != nil {
		t.Fatal(err)
	}
	if c.Findings[0].Status != "OPEN" {
		t.Fatalf("rejected remediation should remain open: %+v", c.Findings[0])
	}
	if err := c.ReviewFinding("f1", "补充证据", "reviewer", true, now); err != nil {
		t.Fatal(err)
	}
	if c.Findings[0].Status != "CLOSED" {
		t.Fatalf("accepted remediation should close: %+v", c.Findings[0])
	}
}
