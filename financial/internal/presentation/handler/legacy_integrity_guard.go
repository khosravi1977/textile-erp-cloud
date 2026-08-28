package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// GetAdviceAudited blocks the legacy advisor endpoint from producing business
// advice from a fabricated credit profile. The current financial UI has its own
// data-driven analysis path; this legacy API must fail honestly until a real
// persisted credit-profile source is wired in.
func (h *APIHandler) GetAdviceAudited(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	RespondError(w, http.StatusServiceUnavailable, "پروفایل اعتباری واقعی مشتری برای این API پیکربندی نشده است؛ تولید توصیه با مقادیر فرضی غیرفعال شده است.")
}

// GetCreditReportAudited prevents returning the historical hard-coded report
// (party 1 / fixed score / fixed credit limit) as if it were real company data.
func (h *APIHandler) GetCreditReportAudited(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	RespondError(w, http.StatusServiceUnavailable, "منبع داده واقعی اعتبارسنجی برای این API پیکربندی نشده است؛ گزارش فرضی غیرفعال شده است.")
}

// GetProfitabilityAudited preserves the legacy profitability calculation only
// when the caller explicitly supplies revenue. The previous handler silently
// substituted a fixed revenue value, which could create a false profit report.
func (h *APIHandler) GetProfitabilityAudited(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	revenueRaw := strings.TrimSpace(r.URL.Query().Get("revenue"))
	if revenueRaw == "" {
		RespondError(w, http.StatusBadRequest, "revenue is required; fixed default revenue is disabled")
		return
	}
	revenue, err := strconv.ParseFloat(revenueRaw, 64)
	if err != nil || revenue < 0 {
		RespondError(w, http.StatusBadRequest, "revenue must be a non-negative number")
		return
	}
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 3660 {
			RespondError(w, http.StatusBadRequest, "days must be between 1 and 3660")
			return
		}
		days = parsed
	}
	companyID := requestctx.CompanyID(r.Context())
	RespondJSON(w, http.StatusOK, h.costService.GetProfitabilityForCompany(companyID, valueobject.NewMoney(revenue), days))
}
