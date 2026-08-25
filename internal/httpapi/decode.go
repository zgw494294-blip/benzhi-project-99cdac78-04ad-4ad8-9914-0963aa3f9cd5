package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"oral-archive-release/internal/domain"
)

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, "application/json") {
		writeError(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "请求体必须使用 application/json", nil)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		h.decodeError(w, r, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "请求体只能包含一个 JSON 对象", nil)
		return false
	}
	return true
}

func (h *Handler) decodeError(w http.ResponseWriter, r *http.Request, err error) {
	var syntax *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "JSON 语法错误", map[string]string{"offset": number(syntax.Offset)})
	case errors.As(err, &typeError):
		writeError(w, r, http.StatusBadRequest, "INVALID_FIELD_TYPE", "JSON 字段类型错误", map[string]string{typeError.Field: typeError.Type.String()})
	case errors.Is(err, io.EOF):
		writeError(w, r, http.StatusBadRequest, "EMPTY_BODY", "请求体不能为空", nil)
	case strings.Contains(err.Error(), "unknown field"):
		writeError(w, r, http.StatusBadRequest, "UNKNOWN_FIELD", "请求包含未知字段", map[string]string{"json": err.Error()})
	case strings.Contains(err.Error(), "request body too large"):
		writeError(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "请求体超过 1 MiB 限制", nil)
	default:
		writeError(w, r, http.StatusBadRequest, "INVALID_JSON", "无法解析 JSON 请求体", nil)
	}
}

func writeResult(w http.ResponseWriter, r *http.Request, status int, data any) {
	writeJSON(w, status, map[string]any{"data": data, "requestId": requestID(r)})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "fields": fields}, "requestId": requestID(r)})
}

func handleError(w http.ResponseWriter, r *http.Request, err error) {
	var d *domain.Error
	if errors.As(err, &d) {
		status := http.StatusUnprocessableEntity
		switch d.Code {
		case "CASE_NOT_FOUND", "CREDENTIAL_NOT_FOUND", "FINDING_NOT_FOUND", "EVIDENCE_PACKAGE_NOT_FOUND", "MANIFEST_RECORDING_NOT_FOUND":
			status = http.StatusNotFound
		case "FORBIDDEN_ROLE":
			status = http.StatusForbidden
		case "VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT", "IDEMPOTENCY_RESULT_EXPIRED", "INVALID_STATE", "CASE_ALREADY_EXISTS", "READINESS_CHANGED", "COMPLIANCE_SELECTION_CHANGED", "MANIFEST_INTEGRITY_ERROR":
			status = http.StatusConflict
		case "ACTOR_REQUIRED", "EXPECTED_VERSION_REQUIRED", "IDEMPOTENCY_KEY_REQUIRED", "VALIDATION_FAILED", "INVALID_EVALUATION_TIME", "INVALID_WARNING_DAYS", "INVALID_QUERY_PARAMETER", "INVALID_SEGMENT_LENGTH", "INVALID_SEGMENT_RANGE", "BATCH_TOO_LARGE":
			status = http.StatusBadRequest
		}
		writeError(w, r, status, d.Code, d.Message, d.Fields)
		return
	}
	if errors.Is(err, r.Context().Err()) {
		writeError(w, r, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "请求处理超时", nil)
		return
	}
	writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "服务处理请求失败", nil)
}

func number(value int64) string { return strconv.FormatInt(value, 10) }

func parsePositiveInt(raw, field string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, domain.FieldError("INVALID_QUERY_PARAMETER", field+" 必须为正整数", field, raw)
	}
	return value, nil
}
