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

func (h *APIHandler) GetAccountingReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	companyID := requestctx.CompanyID(r.Context())
	result, err := postgres.WithCompanySession(r.Context(), postgres.DB, companyID, func(q postgres.SessionQueryable) (map[string]any, error) {
		trialBalance, err := queryAccountingRows(r, q, `
			SELECT a.code, a.name, a.type,
			       COALESCE(SUM(l.debit),0) AS debit,
			       COALESCE(SUM(l.credit),0) AS credit,
			       COALESCE(SUM(l.debit-l.credit),0) AS balance
			FROM accounts a
			JOIN journal_voucher_lines l ON l.account_id=a.id AND l.company_id=a.company_id
			JOIN journal_vouchers v ON v.id=l.journal_voucher_id AND v.company_id=l.company_id AND v.status='Posted'
			WHERE a.company_id=$1
			GROUP BY a.id, a.code, a.name, a.type
			HAVING COALESCE(SUM(l.debit),0) <> 0 OR COALESCE(SUM(l.credit),0) <> 0
			ORDER BY a.code
		`, companyID)
		if err != nil {
			return nil, err
		}
		partyBalances, err := queryAccountingRows(r, q, `
			SELECT p.name AS party,
			       COALESCE(SUM(l.debit),0) AS debit,
			       COALESCE(SUM(l.credit),0) AS credit,
			       COALESCE(SUM(l.debit-l.credit),0) AS balance
			FROM journal_voucher_lines l
			JOIN journal_vouchers v ON v.id=l.journal_voucher_id AND v.company_id=l.company_id AND v.status='Posted'
			JOIN parties p ON p.id=l.party_id AND p.company_id=l.company_id
			WHERE l.company_id=$1
			GROUP BY p.id, p.name
			HAVING ABS(COALESCE(SUM(l.debit-l.credit),0)) > 0.005
			ORDER BY p.name
		`, companyID)
		if err != nil {
			return nil, err
		}
		periods, err := queryAccountingRows(r, q, `SELECT id, title, start_date, end_date, status, closed_at FROM fiscal_periods WHERE company_id=$1 ORDER BY start_date DESC`, companyID)
		if err != nil {
			return nil, err
		}
		vouchers, err := queryAccountingRows(r, q, `
			SELECT v.id, v.voucher_no, v.voucher_date, v.description, v.source_doc_type, v.source_reference,
			       a.code AS account_code, a.name AS account_name, p.name AS party,
			       l.debit, l.credit, l.description AS line_description
			FROM journal_vouchers v
			JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id AND l.company_id=v.company_id
			JOIN accounts a ON a.id=l.account_id AND a.company_id=l.company_id
			LEFT JOIN parties p ON p.id=l.party_id AND p.company_id=l.company_id
			WHERE v.company_id=$1 AND v.status='Posted'
			ORDER BY v.voucher_date DESC, v.id DESC, l.line_no
			LIMIT 1000
		`, companyID)
		if err != nil {
			return nil, err
		}
		totals, err := queryAccountingRows(r, q, `
			SELECT
			 COALESCE(SUM(l.debit),0) AS total_debit,
			 COALESCE(SUM(l.credit),0) AS total_credit,
			 COALESCE(SUM(CASE WHEN a.type='Income' THEN l.credit-l.debit ELSE 0 END),0) AS income,
			 COALESCE(SUM(CASE WHEN a.type='Expense' THEN l.debit-l.credit ELSE 0 END),0) AS expense,
			 COALESCE(SUM(CASE WHEN a.type='Asset' THEN l.debit-l.credit ELSE 0 END),0) AS assets,
			 COALESCE(SUM(CASE WHEN a.type='Liability' THEN l.credit-l.debit ELSE 0 END),0) AS liabilities,
			 COALESCE(SUM(CASE WHEN a.type='Equity' THEN l.credit-l.debit ELSE 0 END),0) AS equity
			FROM journal_voucher_lines l
			JOIN journal_vouchers v ON v.id=l.journal_voucher_id AND v.company_id=l.company_id AND v.status='Posted'
			JOIN accounts a ON a.id=l.account_id AND a.company_id=l.company_id
			WHERE l.company_id=$1
		`, companyID)
		if err != nil {
			return nil, err
		}
		summary := map[string]any{}
		if len(totals) > 0 {
			summary = totals[0]
		}
		return map[string]any{
			"trialBalance":  trialBalance,
			"partyBalances": partyBalances,
			"vouchers":      vouchers,
			"periods":       periods,
			"summary":       summary,
			"generatedAt":   time.Now(),
		}, nil
	})
	if err != nil {
		RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	RespondJSON(w, http.StatusOK, result)
}

func (h *APIHandler) AccountingPeriods(w http.ResponseWriter, r *http.Request) {
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
		result, err := tx.ExecContext(r.Context(), `UPDATE fiscal_periods SET status=$3, closed_at=CASE WHEN $3='Closed' THEN CURRENT_TIMESTAMP ELSE NULL END, closed_by=CASE WHEN $3='Closed' THEN $4 ELSE NULL END WHERE company_id=$1 AND id=$2`, companyID, req.ID, req.Status, nullUserID(requestctx.UserID(r.Context())))
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

func queryAccountingRows(r *http.Request, q postgres.SessionQueryable, statement string, args ...any) ([]map[string]any, error) {
	rows, err := q.QueryContext(r.Context(), statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			value := values[i]
			if raw, ok := value.([]byte); ok {
				value = string(raw)
			}
			if nullable, ok := value.(sql.NullString); ok {
				if nullable.Valid {
					value = nullable.String
				} else {
					value = nil
				}
			}
			row[column] = value
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
