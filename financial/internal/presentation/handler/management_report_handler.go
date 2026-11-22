package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"
)

// defaultManagementReportTokenSHA256 is a hash only. The corresponding random
// token is intentionally not stored in the repository. Set
// TEXTILE_MANAGEMENT_REPORT_TOKEN_SHA256 to rotate it without a code change.
const defaultManagementReportTokenSHA256 = "2208ebdb86c62a30418ee65ae9ae976c42bbdc469b2e5cbffcc99c08ea9b57ed"

func (h *APIHandler) ManagementReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if !validManagementReportToken(r) {
		RespondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if h.telegram == nil {
		RespondError(w, http.StatusServiceUnavailable, "سرویس گزارش مدیریتی در دسترس نیست")
		return
	}

	company := strings.TrimSpace(os.Getenv("TEXTILE_MANAGEMENT_REPORT_COMPANY"))
	if company == "" {
		company = "paregol"
	}
	companyID, err := h.telegram.ResolveCompanyID(r.Context(), company)
	if err != nil {
		RespondError(w, http.StatusNotFound, "شرکت گزارش‌گیری پیدا نشد")
		return
	}

	period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
	if period != "weekly" && period != "monthly" {
		period = "daily"
	}
	report, err := h.telegram.BuildManagementReport(r.Context(), companyID, period, time.Now())
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "txt") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+report.Filename+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(report.Text))
		return
	}
	RespondJSON(w, http.StatusOK, report)
}

func validManagementReportToken(r *http.Request) bool {
	provided := strings.TrimSpace(r.Header.Get("X-Management-Report-Key"))
	if provided == "" {
		provided = strings.TrimSpace(r.URL.Query().Get("access_token"))
	}
	if provided == "" {
		return false
	}
	expected := strings.ToLower(strings.TrimSpace(os.Getenv("TEXTILE_MANAGEMENT_REPORT_TOKEN_SHA256")))
	if expected == "" {
		expected = defaultManagementReportTokenSHA256
	}
	expectedBytes, err := hex.DecodeString(expected)
	if err != nil || len(expectedBytes) != sha256.Size {
		return false
	}
	actual := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(actual[:], expectedBytes) == 1
}
