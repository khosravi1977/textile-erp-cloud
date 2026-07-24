package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/erpsystem/textile-erp/internal/application/intelligence"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func (h *APIHandler) GenerateAIAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var summary intelligence.Summary
	if err := decodeJSONBody(r, &summary); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid analysis summary")
		return
	}
	service := intelligence.NewFromEnv(postgres.DB)
	result, err := service.Generate(r.Context(), requestctx.CompanyID(r.Context()), requestctx.UserID(r.Context()), summary)
	if err != nil {
		switch {
		case errors.Is(err, intelligence.ErrDisabled), errors.Is(err, intelligence.ErrNotConfigured):
			RespondError(w, http.StatusServiceUnavailable, "تحلیل AI هنوز توسط مدیر سرور فعال نشده است؛ گزارش عددی بدون AI همچنان قابل استفاده است.")
		case errors.Is(err, intelligence.ErrLimitReached):
			RespondError(w, http.StatusTooManyRequests, "سقف تحلیل AI این ماه به پایان رسیده است.")
		default:
			RespondError(w, http.StatusBadGateway, "ارتباط امن با سرویس AI موقتاً ناموفق بود.")
		}
		return
	}
	RespondJSON(w, http.StatusOK, result)
}

func decodeJSONBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
