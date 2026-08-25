package domain

import (
	"fmt"
	"sort"
	"time"
)

func (c *ReleaseCase) IntegrityIssues(at time.Time) map[string]string {
	issues := map[string]string{}
	if len(c.Participants) == 0 {
		issues["participants"] = "至少需要一个参与者"
	}
	if len(c.Recordings) == 0 {
		issues["recordings"] = "至少需要一条录音"
	}
	for _, r := range c.Recordings {
		for _, pid := range r.ParticipantIDs {
			if len(c.activeGrants(r.ID, pid, at)) == 0 {
				issues["recording."+r.ID+".participant."+pid] = "缺少当前有效且未撤回的同意授权"
			}
		}
	}
	return issues
}

func (c *ReleaseCase) ComputeAccessScopes(at time.Time) []AccessScope {
	result := make([]AccessScope, 0, len(c.Recordings))
	for _, r := range c.Recordings {
		scope := AccessScope{RecordingID: r.ID, Valid: true}
		var purposeIntersection, audienceIntersection []string
		var earliest *time.Time
		for participantIndex, pid := range r.ParticipantIDs {
			grants := c.activeGrants(r.ID, pid, at)
			if len(grants) == 0 {
				scope.Valid = false
				scope.Reasons = append(scope.Reasons, "参与者 "+pid+" 缺少有效授权")
				continue
			}
			participantPurposes, participantAudience := unionGrants(grants)
			if participantIndex == 0 {
				purposeIntersection, audienceIntersection = participantPurposes, participantAudience
			} else {
				purposeIntersection = intersect(purposeIntersection, participantPurposes)
				audienceIntersection = intersect(audienceIntersection, participantAudience)
			}
			for _, g := range grants {
				if g.ExpiresAt != nil && (earliest == nil || g.ExpiresAt.Before(*earliest)) {
					t := g.ExpiresAt.UTC()
					earliest = &t
				}
			}
		}
		scope.AllowedPurposes, scope.Audience, scope.ExpiresAt = purposeIntersection, audienceIntersection, earliest
		if !contains(scope.AllowedPurposes, c.Purpose) {
			scope.Valid = false
			scope.Reasons = append(scope.Reasons, "案件开放目的不在共同授权范围内")
		}
		if len(scope.Audience) == 0 {
			scope.Valid = false
			scope.Reasons = append(scope.Reasons, "参与者授权没有共同访问人群")
		}
		for _, topic := range r.SensitiveTopics {
			allowed := true
			for _, pid := range r.ParticipantIDs {
				covered := false
				for _, g := range c.activeGrants(r.ID, pid, at) {
					if contains(g.SensitiveTopics, topic) {
						covered = true
					}
				}
				if !covered {
					allowed = false
				}
			}
			if !allowed {
				scope.SensitiveTopics = append(scope.SensitiveTopics, topic)
				scope.Valid = false
				scope.Reasons = append(scope.Reasons, "敏感主题未获全部参与者明确授权："+topic)
			}
		}
		scope.Reasons = uniqueSorted(scope.Reasons)
		result = append(result, scope)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RecordingID < result[j].RecordingID })
	return result
}

func (c *ReleaseCase) activeGrants(recordingID, participantID string, at time.Time) []ConsentGrant {
	superseded := map[string]bool{}
	for _, g := range c.Consents {
		if g.SupersedesID != "" {
			superseded[g.SupersedesID] = true
		}
	}
	var result []ConsentGrant
	for _, g := range c.Consents {
		if g.ParticipantID != participantID || !contains(g.RecordingIDs, recordingID) || superseded[g.ID] {
			continue
		}
		if g.SignedAt.After(at) || (g.ExpiresAt != nil && !g.ExpiresAt.After(at)) || (g.WithdrawnAt != nil && !g.WithdrawnAt.After(at)) {
			continue
		}
		result = append(result, g)
	}
	return result
}

func unionGrants(grants []ConsentGrant) ([]string, []string) {
	var purposes, audience []string
	for _, g := range grants {
		purposes = append(purposes, g.AllowedPurposes...)
		audience = append(audience, g.Audience...)
	}
	return uniqueSorted(purposes), uniqueSorted(audience)
}

func intersect(a, b []string) []string {
	set := map[string]bool{}
	for _, value := range b {
		set[value] = true
	}
	var out []string
	for _, value := range a {
		if set[value] {
			out = append(out, value)
		}
	}
	return uniqueSorted(out)
}

func ExplainScope(scope AccessScope) string {
	if scope.Valid {
		return fmt.Sprintf("录音 %s 可供 %v 用于 %v", scope.RecordingID, scope.Audience, scope.AllowedPurposes)
	}
	return fmt.Sprintf("录音 %s 不可开放：%v", scope.RecordingID, scope.Reasons)
}
