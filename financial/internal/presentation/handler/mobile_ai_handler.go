package handler

import (
	"errors"
	"net/http"

	"github.com/erpsystem/textile-erp/internal/application/intelligence"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func (h *APIHandler) MobileAIAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { RespondError(w, http.StatusMethodNotAllowed, "Method not allowed"); return }
	companyID := requestctx.CompanyID(r.Context())
	if !h.mobileDeviceActive(r, companyID) { RespondError(w, http.StatusUnauthorized, "Device is not paired"); return }
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	var summary intelligence.Summary
	if err := decodeJSONBody(r, &summary); err != nil { RespondError(w, http.StatusBadRequest, "Invalid analysis summary"); return }
	result, err := intelligence.NewFromEnv(postgres.DB).Generate(r.Context(), companyID, requestctx.UserID(r.Context()), summary)
	if err != nil {
		switch {
		case errors.Is(err, intelligence.ErrDisabled), errors.Is(err, intelligence.ErrNotConfigured): RespondError(w, http.StatusServiceUnavailable, "تحلیل هوشمند هنوز روی سرور فعال نشده است.")
		case errors.Is(err, intelligence.ErrLimitReached): RespondError(w, http.StatusTooManyRequests, "سقف تحلیل هوشمند این ماه به پایان رسیده است.")
		default: RespondError(w, http.StatusBadGateway, "ارتباط امن با سرویس هوش مصنوعی موقتاً ناموفق بود.")
		}
		return
	}
	RespondJSON(w, http.StatusOK, result)
}
