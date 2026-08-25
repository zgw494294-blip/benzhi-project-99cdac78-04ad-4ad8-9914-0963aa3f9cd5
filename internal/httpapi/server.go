package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"oral-archive-release/internal/application"
)

type Handler struct {
	service *application.Service
	limit   int64
	timeout time.Duration
}

func NewHandler(service *application.Service) *Handler {
	return &Handler{service: service, limit: 1 << 20, timeout: 10 * time.Second}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := r.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = newRequestID()
	}
	w.Header().Set("X-Request-Id", requestID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	r = r.WithContext(context.WithValue(ctx, requestIDKey{}, requestID))
	if r.URL.Path == "/healthz" {
		h.Health(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		h.notFound(w, r)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 1 && parts[0] == "release-cases" && r.Method == http.MethodPost {
		h.CreateReleaseCase(w, r)
		return
	}
	if len(parts) >= 2 && parts[0] == "release-cases" {
		h.routeCase(w, r, parts)
		return
	}
	if len(parts) >= 2 && parts[0] == "credentials" {
		h.routeCredential(w, r, parts)
		return
	}
	h.notFound(w, r)
}

func (h *Handler) routeCase(w http.ResponseWriter, r *http.Request, parts []string) {
	caseID := parts[1]
	if len(parts) == 2 && r.Method == http.MethodGet {
		h.GetReleaseCase(w, r, caseID)
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPatch {
		h.ReviseReleaseCase(w, r, caseID)
		return
	}
	if len(parts) == 3 {
		switch parts[2] {
		case "participants":
			if r.Method == http.MethodPost {
				h.AddParticipant(w, r, caseID)
				return
			}
		case "recordings":
			if r.Method == http.MethodPost {
				h.AddRecording(w, r, caseID)
				return
			}
		case "consents":
			if r.Method == http.MethodPost {
				h.AddConsent(w, r, caseID)
				return
			}
		case "catalog-batches":
			if r.Method == http.MethodPost {
				h.CatalogBatch(w, r, caseID)
				return
			}
		case "catalog-batch":
			if r.Method == http.MethodPost {
				h.CatalogBatch(w, r, caseID)
				return
			}
		case "profile-revisions":
			if r.Method == http.MethodPost {
				h.ReviseReleaseCase(w, r, caseID)
				return
			}
		case "evidence-packages":
			if r.Method == http.MethodPost {
				h.SubmitEvidencePackage(w, r, caseID)
				return
			}
		case "approval-readiness":
			if r.Method == http.MethodGet {
				h.GetReadiness(w, r, caseID)
				return
			}
		case "submit-review":
			if r.Method == http.MethodPost {
				h.SubmitReview(w, r, caseID)
				return
			}
		case "findings":
			if r.Method == http.MethodPost {
				h.AddFinding(w, r, caseID)
				return
			}
		case "revisions":
			if r.Method == http.MethodPost {
				h.AddRevision(w, r, caseID)
				return
			}
		case "decision":
			if r.Method == http.MethodPost {
				h.DecideCase(w, r, caseID)
				return
			}
		case "freeze":
			if r.Method == http.MethodPost {
				h.FreezeManifest(w, r, caseID)
				return
			}
		case "credentials":
			if r.Method == http.MethodPost {
				h.IssueCredential(w, r, caseID)
				return
			}
		case "timeline":
			if r.Method == http.MethodGet {
				h.GetTimeline(w, r, caseID)
				return
			}
		case "compliance":
			if r.Method == http.MethodGet {
				h.GetCompliance(w, r, caseID)
				return
			}
		case "manifest":
			if r.Method == http.MethodGet {
				h.GetManifest(w, r, caseID)
				return
			}
		case "overview":
			if r.Method == http.MethodGet {
				h.GetOverview(w, r, caseID)
				return
			}
		}
	}
	if len(parts) == 5 && parts[2] == "findings" && parts[4] == "review" && r.Method == http.MethodPost {
		h.ReviewFinding(w, r, caseID, parts[3])
		return
	}
	if len(parts) == 4 && parts[2] == "findings" && parts[3] == "compliance-import" && r.Method == http.MethodPost {
		h.ImportComplianceFindings(w, r, caseID)
		return
	}
	if len(parts) == 5 && parts[2] == "manifest" && parts[3] == "recordings" && r.Method == http.MethodGet {
		h.GetManifestTrace(w, r, caseID, parts[4])
		return
	}
	h.notFound(w, r)
}

func (h *Handler) routeCredential(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 2 && r.Method == http.MethodGet {
		h.GetCredential(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[2] == "verify" && r.Method == http.MethodGet {
		h.VerifyCredential(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[2] == "chain" && r.Method == http.MethodGet {
		h.GetCredentialChain(w, r, parts[1])
		return
	}
	h.notFound(w, r)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "requestId": requestID(r)})
}
func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotFound, "ROUTE_NOT_FOUND", "请求路由不存在", nil)
}
func requestID(r *http.Request) string {
	value, _ := r.Context().Value(requestIDKey{}).(string)
	return value
}

type requestIDKey struct{}

func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "req-unknown"
	}
	return "req-" + hex.EncodeToString(b)
}
