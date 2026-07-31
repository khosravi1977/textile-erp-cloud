package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func (h *APIHandler) TelegramReportConfig(w http.ResponseWriter, r *http.Request) {
	if h.telegram == nil {
		RespondError(w, http.StatusServiceUnavailable, "سرویس گزارش تلگرام در دسترس نیست")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	switch r.Method {
	case http.MethodGet:
		settings, err := h.telegram.GetSettings(r.Context(), companyID)
		if err != nil {
			RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		RespondJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var input struct {
			Enabled       bool   `json:"enabled"`
			AlertsEnabled bool   `json:"alerts_enabled"`
			DailyTime     string `json:"daily_time"`
			Timezone      string `json:"timezone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			RespondError(w, http.StatusBadRequest, "اطلاعات تنظیمات نامعتبر است")
			return
		}
		settings, err := h.telegram.SaveSettings(r.Context(), companyID, input.Enabled, input.AlertsEnabled, input.DailyTime, input.Timezone)
		if err != nil {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		RespondJSON(w, http.StatusOK, settings)
	default:
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (h *APIHandler) TelegramReportPairing(w http.ResponseWriter, r *http.Request) {
	if h.telegram == nil {
		RespondError(w, http.StatusServiceUnavailable, "سرویس گزارش تلگرام در دسترس نیست")
		return
	}
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	pairing, err := h.telegram.CreatePairing(r.Context(), requestctx.CompanyID(r.Context()), requestctx.UserID(r.Context()))
	if err != nil {
		RespondError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	RespondJSON(w, http.StatusCreated, pairing)
}

func (h *APIHandler) TelegramReportTest(w http.ResponseWriter, r *http.Request) {
	if h.telegram == nil {
		RespondError(w, http.StatusServiceUnavailable, "سرویس گزارش تلگرام در دسترس نیست")
		return
	}
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if err := h.telegram.SendTest(r.Context(), requestctx.CompanyID(r.Context())); err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *APIHandler) TelegramReportHistory(w http.ResponseWriter, r *http.Request) {
	if h.telegram == nil {
		RespondError(w, http.StatusServiceUnavailable, "سرویس گزارش تلگرام در دسترس نیست")
		return
	}
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.telegram.History(r.Context(), requestctx.CompanyID(r.Context()), limit)
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"rows": rows, "total": len(rows)})
}
