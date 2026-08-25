package httpapi

import (
	"net/http"

	"oral-archive-release/internal/application"
)

func (h *Handler) CreateReleaseCase(w http.ResponseWriter, r *http.Request) {
	var cmd application.CreateCase
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.CreateCase(r.Context(), cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusCreated, c)
}

func (h *Handler) GetReleaseCase(w http.ResponseWriter, r *http.Request, id string) {
	c, err := h.service.GetCase(r.Context(), id)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) ReviseReleaseCase(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.ReviseCase
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.ReviseCase(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) CatalogBatch(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.CatalogBatch
	if !h.decode(w, r, &cmd) {
		return
	}
	const maxItems = 200
	batch := cmd.Items()
	if len(batch.Participants) > maxItems || len(batch.Recordings) > maxItems || len(batch.Consents) > maxItems {
		writeError(w, r, http.StatusBadRequest, "BATCH_TOO_LARGE", "每类批量条目不能超过 200 项", nil)
		return
	}
	result, err := h.service.CatalogBatch(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, result)
}

func (h *Handler) AddParticipant(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.AddParticipant
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.AddParticipant(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) AddRecording(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.AddRecording
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.AddRecording(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) AddConsent(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.AddConsent
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.AddConsent(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) SubmitReview(w http.ResponseWriter, r *http.Request, id string) {
	var meta application.Meta
	if !h.decode(w, r, &meta) {
		return
	}
	c, err := h.service.SubmitReview(r.Context(), id, meta)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) AddFinding(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.AddFinding
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.AddFinding(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) ImportComplianceFindings(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.ImportComplianceFindings
	if !h.decode(w, r, &cmd) {
		return
	}
	if len(cmd.IssueKeys) > 500 {
		writeError(w, r, http.StatusBadRequest, "BATCH_TOO_LARGE", "异常选择不能超过 500 项", nil)
		return
	}
	result, err := h.service.ImportComplianceFindings(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, result)
}

func (h *Handler) SubmitEvidencePackage(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.SubmitEvidencePackage
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.SubmitEvidencePackage(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) AddRevision(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.AddRevision
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.AddRevision(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) ReviewFinding(w http.ResponseWriter, r *http.Request, id, findingID string) {
	var cmd application.ReviewFinding
	if !h.decode(w, r, &cmd) {
		return
	}
	if cmd.FindingID != "" && cmd.FindingID != findingID {
		writeError(w, r, http.StatusBadRequest, "PATH_BODY_MISMATCH", "路径与请求体 findingId 不一致", nil)
		return
	}
	cmd.FindingID = findingID
	c, err := h.service.ReviewFinding(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) DecideCase(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.Decision
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.Decide(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) FreezeManifest(w http.ResponseWriter, r *http.Request, id string) {
	var meta application.Meta
	if !h.decode(w, r, &meta) {
		return
	}
	c, err := h.service.Freeze(r.Context(), id, meta)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, c)
}

func (h *Handler) IssueCredential(w http.ResponseWriter, r *http.Request, id string) {
	var cmd application.IssueCredential
	if !h.decode(w, r, &cmd) {
		return
	}
	c, err := h.service.Issue(r.Context(), id, cmd)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusCreated, c.Credential)
}

func (h *Handler) GetTimeline(w http.ResponseWriter, r *http.Request, id string) {
	events, err := h.service.Timeline(r.Context(), id)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, events)
}

func (h *Handler) GetCredential(w http.ResponseWriter, r *http.Request, no string) {
	credential, err := h.service.Credential(r.Context(), no)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, credential)
}

func (h *Handler) VerifyCredential(w http.ResponseWriter, r *http.Request, no string) {
	verification, err := h.service.VerifyCredential(r.Context(), no)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, verification)
}

func (h *Handler) GetCredentialChain(w http.ResponseWriter, r *http.Request, no string) {
	length, err := parsePositiveInt(r.URL.Query().Get("length"), "length")
	if err != nil {
		handleError(w, r, err)
		return
	}
	result, err := h.service.CredentialChain(r.Context(), no, length)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, result)
}
