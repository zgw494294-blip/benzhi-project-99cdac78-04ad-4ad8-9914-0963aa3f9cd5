package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"oral-archive-release/internal/application"
	"oral-archive-release/internal/domain"
)

func (h *Handler) GetCompliance(w http.ResponseWriter, r *http.Request, id string) {
	query, err := parseEvaluationQuery(r, true)
	if err != nil {
		handleError(w, r, err)
		return
	}
	report, err := h.service.ComplianceAt(r.Context(), id, query)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, report)
}

func (h *Handler) GetReadiness(w http.ResponseWriter, r *http.Request, id string) {
	query, err := parseEvaluationQuery(r, false)
	if err != nil {
		handleError(w, r, err)
		return
	}
	result, err := h.service.Readiness(r.Context(), id, query.EvaluateAt)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, result)
}

func (h *Handler) GetManifest(w http.ResponseWriter, r *http.Request, id string) {
	manifest, err := h.service.Manifest(r.Context(), id)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, manifest)
}

func (h *Handler) GetManifestTrace(w http.ResponseWriter, r *http.Request, id, recordingID string) {
	if recordingID == "" {
		handleError(w, r, domain.NewError("MANIFEST_RECORDING_NOT_FOUND", "录音标识不能为空"))
		return
	}
	trace, err := h.service.ManifestTrace(r.Context(), id, recordingID)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, trace)
}

func (h *Handler) GetOverview(w http.ResponseWriter, r *http.Request, id string) {
	overview, err := h.service.Overview(r.Context(), id)
	if err != nil {
		handleError(w, r, err)
		return
	}
	writeResult(w, r, http.StatusOK, overview)
}

func parseEvaluationQuery(r *http.Request, allowWarning bool) (application.ComplianceQuery, error) {
	var result application.ComplianceQuery
	if raw := r.URL.Query().Get("evaluateAt"); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return result, domain.FieldError("INVALID_EVALUATION_TIME", "evaluateAt 必须是 RFC3339 时间", "evaluateAt", raw)
		}
		result.EvaluateAt = value.UTC()
	}
	if raw := r.URL.Query().Get("warningDays"); raw != "" {
		if !allowWarning {
			return result, domain.FieldError("INVALID_QUERY_PARAMETER", "该查询不支持 warningDays", "warningDays", raw)
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 || value > 3650 {
			return result, domain.FieldError("INVALID_WARNING_DAYS", "warningDays 必须在 0 到 3650 之间", "warningDays", raw)
		}
		result.WarningDays = value
	}
	return result, nil
}
