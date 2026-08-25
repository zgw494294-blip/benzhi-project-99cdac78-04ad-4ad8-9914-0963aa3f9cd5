package domain

import (
	"errors"
	"testing"
	"time"
)

func TestDraftRevisionTransferAndAtomicBatch(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	c, _ := NewReleaseCase("case-edit", "collection", "原题", "research", "cat-a", now)
	title, next := "修订题", "cat-b"
	result, err := c.ReviseDraft("cat-a", CaseRevision{Title: &title, CatalogerID: &next, TransferReason: "轮岗"}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 2 || c.Title != title || c.CatalogerID != next || len(result.ChangedFields) != 2 {
		t.Fatalf("unexpected revision: %+v %+v", c, result)
	}
	if _, err := c.ReviseDraft("cat-a", CaseRevision{Purpose: &title}, now); err == nil {
		t.Fatal("former cataloger should not edit")
	}

	bad := CatalogBatch{
		Participants: []Participant{{ID: "p1", IdentityRef: "vault://p1"}},
		Recordings: []RecordingItem{
			{ID: "r1", CatalogRef: "R1", ParticipantIDs: []string{"p1"}, DurationSeconds: 1, ContentDigest: "d1"},
			{ID: "r2", CatalogRef: "R2", ParticipantIDs: []string{"missing"}, DurationSeconds: 1, ContentDigest: "d2"},
		},
	}
	version := c.Version
	_, err = c.ApplyCatalogBatch(bad, "cat-b", now.Add(2*time.Minute))
	var de *Error
	if !errors.As(err, &de) || de.Fields["recordings[1].participantIds"] == "" {
		t.Fatalf("expected indexed error: %#v", err)
	}
	if c.Version != version || len(c.Participants) != 0 || len(c.Recordings) != 0 {
		t.Fatalf("failed batch changed aggregate: %+v", c)
	}

	expires := now.Add(10 * 24 * time.Hour)
	good := bad
	good.Recordings = good.Recordings[:1]
	good.Consents = []ConsentGrant{{ID: "g1", ParticipantID: "p1", RecordingIDs: []string{"r1"}, AllowedPurposes: []string{"research"}, Audience: []string{"researchers"}, SignedAt: now.Add(-time.Hour), ExpiresAt: &expires, DocumentDigest: "gd"}}
	counts, err := c.ApplyCatalogBatch(good, "cat-b", now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != version+1 || counts.Participants != 1 || counts.Recordings != 1 || counts.Consents != 1 {
		t.Fatalf("unexpected batch result: %+v v=%d", counts, c.Version)
	}
	report := c.EvaluateComplianceAt(now, 30)
	if !report.Releasable || !report.Assessments[0].ExpiringSoon || report.Assessments[0].Status != "VALID" {
		t.Fatalf("unexpected warning report: %+v", report)
	}
	if c.EvaluateComplianceAt(expires.Add(time.Second), 30).Releasable {
		t.Fatal("expired grant should block release")
	}
}

func TestComplianceImportEvidenceReadinessAndManifestTrace(t *testing.T) {
	now := time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)
	c, _ := NewReleaseCase("case-review", "collection", "案件", "research", "cat", now)
	_ = c.AddParticipant(Participant{ID: "p1", IdentityRef: "vault://p1"}, now)
	_ = c.AddRecording(RecordingItem{ID: "r1", CatalogRef: "R1", ParticipantIDs: []string{"p1"}, DurationSeconds: 1, ContentDigest: "d1"}, "cat", now)
	_ = c.AddConsent(ConsentGrant{ID: "g1", ParticipantID: "p1", RecordingIDs: []string{"r1"}, AllowedPurposes: []string{"education"}, Audience: []string{"researchers"}, SignedAt: now.Add(-time.Hour), DocumentDigest: "g1d"}, now)
	if err := c.SubmitReview(now); err != nil {
		t.Fatal(err)
	}
	issues := c.EvaluateCompliance(now).Issues
	if len(issues) != 1 || issues[0].Key == "" {
		t.Fatalf("unexpected issues: %+v", issues)
	}
	imported, err := c.ImportComplianceFindings([]string{issues[0].Key}, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Created) != 1 || c.Status != StatusChangesRequired {
		t.Fatalf("unexpected import: %+v", imported)
	}
	version := c.Version
	again, err := c.ImportComplianceFindings(nil, now, now)
	if err != nil || len(again.Created) != 0 || len(again.Skipped) != 1 || c.Version != version+1 {
		t.Fatalf("dedup failed: %+v %v", again, err)
	}

	pkg, err := c.SubmitEvidencePackage(EvidenceSubmission{ID: "ep1", Description: "补充授权", FindingIDs: []string{imported.Created[0].ID}, Consents: []ConsentGrant{{ID: "g2", ParticipantID: "p1", RecordingIDs: []string{"r1"}, AllowedPurposes: []string{"research"}, Audience: []string{"researchers"}, SignedAt: now, DocumentDigest: "g2d", SupersedesID: "g1"}}}, "cat", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ReviewFindingWithPackage(imported.Created[0].ID, pkg.ID, "材料有效", "reviewer", true, now); err != nil {
		t.Fatal(err)
	}
	ready, err := c.ApprovalReadiness(now)
	if err != nil || !ready.Approvable || ready.ReadinessDigest == "" {
		t.Fatalf("unexpected readiness: %+v %v", ready, err)
	}
	if err := c.Decide(true, "officer", "批准", now); err != nil {
		t.Fatal(err)
	}
	manifest, err := c.Freeze(now)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := ManifestRecordingTrace(*manifest, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if trace.Recording.RevisionID == "" || len(trace.Consents) != 2 || trace.Consents[0].Adopted == trace.Consents[1].Adopted || trace.ManifestDigest != manifest.Digest {
		t.Fatalf("unexpected trace: %+v", trace)
	}
}

func TestCredentialSegmentLocatesFailure(t *testing.T) {
	credentials := []ReleaseCredential{{CredentialNo: "OAR-0000000001", Sequence: 1, CaseID: "c1", ManifestDigest: "m1"}, {CredentialNo: "OAR-0000000002", Sequence: 2, CaseID: "c2", ManifestDigest: "m2"}}
	for i := range credentials {
		if i > 0 {
			credentials[i].PreviousDigest = credentials[i-1].CredentialDigest
		}
		digest, _ := CredentialDigest(credentials[i])
		credentials[i].CredentialDigest = digest
	}
	valid := VerifyCredentialSegment(credentials)
	if !valid.Valid {
		t.Fatalf("valid segment rejected: %+v", valid)
	}
	credentials[1].PreviousDigest = "tampered"
	broken := VerifyCredentialSegment(credentials)
	if broken.Valid || broken.FirstFailureSequence != 2 || broken.ProblemCode != "CREDENTIAL_PREVIOUS_DIGEST_MISMATCH" {
		t.Fatalf("failure not located: %+v", broken)
	}
}
