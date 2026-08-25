package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type CaseRevision struct {
	Title          *string `json:"title,omitempty"`
	Purpose        *string `json:"purpose,omitempty"`
	CatalogerID    *string `json:"catalogerId,omitempty"`
	TransferReason string  `json:"transferReason,omitempty"`
}

type CaseRevisionResult struct {
	ChangedFields     []string `json:"changedFields"`
	PreviousCataloger string   `json:"previousCataloger"`
	NewCataloger      string   `json:"newCataloger"`
	TransferReason    string   `json:"transferReason,omitempty"`
}

func (c *ReleaseCase) ReviseDraft(actor string, change CaseRevision, now time.Time) (CaseRevisionResult, error) {
	if c.Status != StatusDraft {
		return CaseRevisionResult{}, NewError("INVALID_STATE", "只有 DRAFT 案件可以修订资料")
	}
	if actor != c.CatalogerID {
		return CaseRevisionResult{}, NewError("NOT_RESPONSIBLE_CATALOGER", "只有当前责任编目员可以修订或移交案件")
	}
	result := CaseRevisionResult{PreviousCataloger: c.CatalogerID, NewCataloger: c.CatalogerID}
	if change.Title != nil {
		value := strings.TrimSpace(*change.Title)
		if value == "" {
			return CaseRevisionResult{}, FieldError("VALIDATION_FAILED", "标题不能为空", "title", "不能为空")
		}
		if value != c.Title {
			c.Title = value
			result.ChangedFields = append(result.ChangedFields, "title")
		}
	}
	if change.Purpose != nil {
		value := strings.TrimSpace(*change.Purpose)
		if value == "" {
			return CaseRevisionResult{}, FieldError("VALIDATION_FAILED", "研究开放目的不能为空", "purpose", "不能为空")
		}
		if value != c.Purpose {
			c.Purpose = value
			result.ChangedFields = append(result.ChangedFields, "purpose")
		}
	}
	if change.CatalogerID != nil {
		value := strings.TrimSpace(*change.CatalogerID)
		reason := strings.TrimSpace(change.TransferReason)
		if value == "" {
			return CaseRevisionResult{}, FieldError("VALIDATION_FAILED", "新责任编目员不能为空", "catalogerId", "不能为空")
		}
		if reason == "" {
			return CaseRevisionResult{}, FieldError("TRANSFER_REASON_REQUIRED", "责任移交必须提供理由", "transferReason", "不能为空")
		}
		if value == c.CatalogerID {
			return CaseRevisionResult{}, NewError("CATALOGER_UNCHANGED", "新责任编目员必须不同于当前责任人")
		}
		c.CatalogerID = value
		result.NewCataloger, result.TransferReason = value, reason
		result.ChangedFields = append(result.ChangedFields, "catalogerId")
	} else if strings.TrimSpace(change.TransferReason) != "" {
		return CaseRevisionResult{}, FieldError("TRANSFER_TARGET_REQUIRED", "提供移交理由时必须指定新责任编目员", "catalogerId", "不能为空")
	}
	if len(result.ChangedFields) == 0 {
		return CaseRevisionResult{}, NewError("NO_CHANGES", "修订请求没有实际变化")
	}
	sort.Strings(result.ChangedFields)
	c.touch(now)
	return result, nil
}

type CatalogBatch struct {
	Participants []Participant   `json:"participants"`
	Recordings   []RecordingItem `json:"recordings"`
	Consents     []ConsentGrant  `json:"consents"`
}

type BatchCounts struct {
	Participants int `json:"participants"`
	Recordings   int `json:"recordings"`
	Consents     int `json:"consents"`
}

func (c *ReleaseCase) ApplyCatalogBatch(batch CatalogBatch, actor string, now time.Time) (BatchCounts, error) {
	if c.Status != StatusDraft {
		return BatchCounts{}, NewError("INVALID_STATE", "只有 DRAFT 案件可以批量编目")
	}
	if len(batch.Participants)+len(batch.Recordings)+len(batch.Consents) == 0 {
		return BatchCounts{}, NewError("VALIDATION_FAILED", "批量编目至少需要一个条目")
	}
	work, err := cloneReleaseCase(c)
	if err != nil {
		return BatchCounts{}, err
	}
	participantIDs := map[string]int{}
	for i, item := range batch.Participants {
		if prior, ok := participantIDs[item.ID]; ok {
			return BatchCounts{}, batchField("DUPLICATE_PARTICIPANT", "批内参与者标识重复", "participants", i, "id", fmt.Sprintf("与索引 %d 重复", prior))
		}
		participantIDs[item.ID] = i
		if err := work.AddParticipant(item, now); err != nil {
			return BatchCounts{}, indexedError(err, "participants", i)
		}
	}
	recordingIDs := map[string]int{}
	for i, item := range batch.Recordings {
		if prior, ok := recordingIDs[item.ID]; ok {
			return BatchCounts{}, batchField("DUPLICATE_RECORDING", "批内录音标识重复", "recordings", i, "id", fmt.Sprintf("与索引 %d 重复", prior))
		}
		recordingIDs[item.ID] = i
		if err := work.AddRecording(item, actor, now); err != nil {
			return BatchCounts{}, indexedError(err, "recordings", i)
		}
	}
	consentIndex := map[string]int{}
	for i, item := range batch.Consents {
		if prior, ok := consentIndex[item.ID]; ok {
			return BatchCounts{}, batchField("DUPLICATE_CONSENT", "批内同意材料标识重复", "consents", i, "id", fmt.Sprintf("与索引 %d 重复", prior))
		}
		consentIndex[item.ID] = i
	}
	if err := validateConsentGraph(work.Consents, batch.Consents, consentIndex); err != nil {
		return BatchCounts{}, err
	}
	pending := make(map[int]bool, len(batch.Consents))
	for i := range batch.Consents {
		pending[i] = true
	}
	for len(pending) > 0 {
		progress := false
		for i := 0; i < len(batch.Consents); i++ {
			if !pending[i] {
				continue
			}
			parent := batch.Consents[i].SupersedesID
			if parent != "" && consentByID(work.Consents, parent) == nil {
				continue
			}
			if err := work.AddConsent(batch.Consents[i], now); err != nil {
				return BatchCounts{}, indexedError(err, "consents", i)
			}
			delete(pending, i)
			progress = true
		}
		if !progress {
			return BatchCounts{}, NewError("CONSENT_REVISION_CYCLE", "同意材料修订关系存在环")
		}
	}
	work.Version, work.UpdatedAt = c.Version, c.UpdatedAt
	work.touch(now)
	*c = *work
	return BatchCounts{Participants: len(batch.Participants), Recordings: len(batch.Recordings), Consents: len(batch.Consents)}, nil
}

func validateConsentGraph(existing, incoming []ConsentGrant, indexes map[string]int) error {
	all := map[string]ConsentGrant{}
	for _, grant := range existing {
		all[grant.ID] = grant
	}
	for _, grant := range incoming {
		all[grant.ID] = grant
	}
	for i, grant := range incoming {
		if grant.SupersedesID == "" {
			continue
		}
		parent, ok := all[grant.SupersedesID]
		if !ok {
			return batchField("INVALID_SUPERSEDES", "被修订同意材料不存在", "consents", i, "supersedesId", grant.SupersedesID)
		}
		if parent.ParticipantID != grant.ParticipantID {
			return batchField("INVALID_SUPERSEDES", "同意修订不能跨参与者", "consents", i, "supersedesId", grant.SupersedesID)
		}
		seen := map[string]bool{grant.ID: true}
		cursor := grant.SupersedesID
		for cursor != "" {
			if seen[cursor] {
				return batchField("CONSENT_REVISION_CYCLE", "同意材料修订关系存在环", "consents", i, "supersedesId", cursor)
			}
			seen[cursor] = true
			cursor = all[cursor].SupersedesID
		}
	}
	return nil
}

func indexedError(err error, collection string, index int) error {
	detail, ok := err.(*Error)
	if !ok {
		return err
	}
	fields := map[string]string{}
	if len(detail.Fields) == 0 {
		fields[fmt.Sprintf("%s[%d]", collection, index)] = detail.Message
	} else {
		for field, value := range detail.Fields {
			fields[fmt.Sprintf("%s[%d].%s", collection, index, field)] = value
		}
	}
	return &Error{Code: detail.Code, Message: detail.Message, Fields: fields}
}

func batchField(code, message, collection string, index int, field, detail string) error {
	return FieldError(code, message, fmt.Sprintf("%s[%d].%s", collection, index, field), detail)
}

func cloneReleaseCase(c *ReleaseCase) (*ReleaseCase, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var result ReleaseCase
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func consentByID(items []ConsentGrant, id string) *ConsentGrant {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}
