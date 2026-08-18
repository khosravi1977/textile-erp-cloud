package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// AccountingPeriodsAudited keeps fiscal closing irreversible through the
// normal ERP API. Corrections after close must be posted as controlled
// adjustment entries in an open period rather than reopening history.
func (h *APIHandler) AccountingPeriodsAudited(w http.ResponseWriter, r *http.Request) {
	companyID := requestctx.CompanyID(r.Context())
	if r.Method == http.MethodGet {
		h.GetAccountingReports(w, r)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, POST, PUT")
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if requestctx.IsPortalAccess(r.Context()) && !requestctx.HasPermission(r.Context(), "accounting") {
		RespondError(w, http.StatusForbidden, "مجوز مدیریت دوره مالی وجود ندارد")
		return
	}
	var req struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid fiscal period")
		return
	}

	err := postgres.WithCompanyTx(r.Context(), postgres.DB, companyID, func(tx *sql.Tx) error {
		if r.Method == http.MethodPost {
			if _, err := tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock($1)`, companyID); err != nil {
				return err
			}
			start, startErr := time.Parse("2006-01-02", req.StartDate)
			end, endErr := time.Parse("2006-01-02", req.EndDate)
			if startErr != nil || endErr != nil || end.Before(start) || strings.TrimSpace(req.Title) == "" {
				return errors.New("عنوان و بازه معتبر دوره مالی الزامی است")
			}
			var overlap bool
			if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM fiscal_periods WHERE company_id=$1 AND daterange(start_date,end_date,'[]') && daterange($2::date,$3::date,'[]'))`, companyID, start, end).Scan(&overlap); err != nil {
				return err
			}
			if overlap {
				return errors.New("بازه دوره مالی با دوره موجود هم‌پوشانی دارد")
			}
			_, err := tx.ExecContext(r.Context(), `INSERT INTO fiscal_periods(company_id,title,start_date,end_date,status) VALUES($1,$2,$3,$4,'Open')`, companyID, truncateText(req.Title, 100), start, end)
			return err
		}

		if req.ID <= 0 || (req.Status != "Open" && req.Status != "Closed") {
			return errors.New("شناسه یا وضعیت دوره مالی معتبر نیست")
		}
		var currentStatus string
		if err := tx.QueryRowContext(r.Context(), `SELECT status FROM fiscal_periods WHERE company_id=$1 AND id=$2 FOR UPDATE`, companyID, req.ID).Scan(&currentStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("دوره مالی پیدا نشد")
			}
			return err
		}
		if err := validateFiscalPeriodTransition(currentStatus, req.Status); err != nil {
			return err
		}
		result, err := tx.ExecContext(r.Context(), `UPDATE fiscal_periods SET status=$3, closed_at=CASE WHEN $3='Closed' THEN COALESCE(closed_at,CURRENT_TIMESTAMP) ELSE NULL END, closed_by=CASE WHEN $3='Closed' THEN COALESCE(closed_by,$4) ELSE NULL END WHERE company_id=$1 AND id=$2`, companyID, req.ID, req.Status, nullUserID(requestctx.UserID(r.Context())))
		if err != nil {
			return err
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			return errors.New("دوره مالی پیدا نشد")
		}
		return nil
	})
	if err != nil {
		RespondError(w, http.StatusBadRequest, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, map[string]any{"status": "saved"})
}

func validateFiscalPeriodTransition(currentStatus, targetStatus string) error {
	current := strings.TrimSpace(currentStatus)
	target := strings.TrimSpace(targetStatus)
	if current == "Closed" && target == "Open" {
		return errors.New("دوره مالی بسته قابل بازگشایی نیست؛ اصلاحات باید با سند تعدیلی در دوره باز انجام شود")
	}
	if target != "Open" && target != "Closed" {
		return errors.New("وضعیت دوره مالی معتبر نیست")
	}
	return nil
}
