package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type supervisorReport struct {
	Revision            int64               `json:"revision"`
	CheckedAt           time.Time           `json:"checkedAt"`
	Complete            bool                `json:"complete"`
	Coverage            []string            `json:"coverage"`
	Findings            []supervisorFinding `json:"findings"`
	Checked             int                 `json:"checked"`
	BackgroundCheckedAt *time.Time          `json:"backgroundCheckedAt,omitempty"`
}

func (h *APIHandler) buildSupervisorReport(r *http.Request) (supervisorReport, error) {
	doc, err := loadWorkspace(r, requestctx.CompanyID(r.Context()))
	if err != nil {
		return supervisorReport{}, err
	}
	state := decodeWorkspaceMap(doc.State)
	report := supervisorReport{Revision: doc.Revision, CheckedAt: time.Now().UTC(), Complete: true, Coverage: []string{"ارتباط اسناد ثبت‌شده و اثر بانک، چک و موجودی"}, Findings: supervisorStateFindings(state)}
	for _, field := range []string{"accounts", "expenses", "movements", "incomingInvoices", "invoices", "yarnOutInvoices", "ownedInventory", "payableDocs", "receivableDocs", "mobileTransactions"} {
		report.Checked += len(rowsFrom(state, field))
	}
	add := func(code, severity, page, ref, detail string) {
		report.Findings = append(report.Findings, supervisorFinding{ID: code + ":" + ref, Severity: severity, Page: page, Reference: ref, Category: "تطبیق با مبدأ", Title: detail, Detail: detail})
	}
	incomplete := func(source string) {
		report.Complete = false
		report.Coverage = append(report.Coverage, source+": بررسی کامل نشد؛ نتیجه سلامت نامشخص است")
	}
	if h.operational == nil {
		incomplete("عملیاتی")
	} else {
		bridge, closeBridge, bridgeErr := h.operational.ForCompany(r.Context(), requestctx.CompanyID(r.Context()))
		if bridgeErr != nil {
			incomplete("عملیاتی")
		} else {
			defer closeBridge()
			sources, readErr := bridge.Expenses(10001)
			if readErr != nil {
				incomplete("هزینه‌های عملیاتی")
			} else {
				if len(sources) >= 10001 {
					incomplete("هزینه‌های عملیاتی؛ سقف ۱۰۰۰۰ ردیف")
				}
				bySource := map[string]map[string]any{}
				for _, e := range rowsFrom(state, "expenses") {
					if firstText(e, "source_type") == "operational_expense" {
						bySource[firstText(e, "sourceId")] = e
					}
				}
				for _, source := range sources {
					report.Checked++
					id := strconv.FormatInt(source.ID, 10)
					e := bySource[id]
					if e == nil {
						add("op-missing", "critical", "costs", id, "هزینه عملیاتی در مالی ثبت نشده است")
						continue
					}
					date, dateErr := financecore.AccountingDate(source.Date)
					if dateErr != nil {
						add("op-date", "warning", "costs", id, "تاریخ مبدأ هزینه معتبر نیست")
						continue
					}
					group, subgroup := strings.TrimSpace(source.Title), strings.TrimSpace(source.Weaver)
					if group == "" {
						group = "سایر"
					}
					if subgroup == "" {
						subgroup = "سایر"
					}
					if !amountsEqual(source.Amount, number(e["amount"])) || date.Format("2006-01-02") != firstText(e, "date") || group != firstText(e, "group") || subgroup != firstText(e, "subgroup") || source.DocNo != firstText(e, "documentNo", "doc_no") || source.Description != firstText(e, "description") || source.Operator != firstText(e, "enteredBy") {
						add("op-difference", "critical", "costs", id, "تاریخ، مبلغ یا مشخصات هزینه با مبدأ عملیاتی متفاوت است")
					}
				}
				report.Coverage = append(report.Coverage, fmt.Sprintf("تطبیق %d هزینه عملیاتی با مالی", len(sources)))
			}
		}
	}
	// Stream every typed source transaction: no silent 500-row UI pagination cap.
	_, err = postgres.WithCompanySession(r.Context(), postgres.DB, requestctx.CompanyID(r.Context()), func(q postgres.SessionQueryable) (bool, error) {
		rows, err := q.QueryContext(r.Context(), `SELECT external_transaction_id,amount,direction,transaction_date::text,status FROM bank_transactions WHERE company_id=$1 AND source='HESABYAR'`, requestctx.CompanyID(r.Context()))
		if err != nil {
			return false, err
		}
		defer rows.Close()
		bySource := map[string]map[string]any{}
		for _, m := range rowsFrom(state, "movements") {
			if id := firstText(m, "sourceMobileTransaction"); id != "" {
				bySource[id] = m
			}
		}
		count := 0
		for rows.Next() {
			var id, dir, date, status string
			var amount float64
			if err := rows.Scan(&id, &amount, &dir, &date, &status); err != nil {
				return false, err
			}
			count++
			report.Checked++
			m := bySource[id]
			if status == "VOIDED" {
				if m != nil {
					add("mobile-voided", "critical", "bankCash", id, "تراکنش باطل‌شده حسابیار هنوز گردش فعال دارد")
				}
				continue
			}
			if m == nil {
				add("mobile-missing", "critical", "mobileApp", id, "رویداد حسابیار در هسته دریافت شده اما گردش مالی ندارد")
				continue
			}
			if !amountsEqual(amount, number(m["amount"])) || strings.ToLower(dir) != firstText(m, "direction") || date != firstText(m, "date") {
				add("mobile-core", "critical", "bankCash", id, "مبلغ، جهت یا تاریخ گردش با هسته حسابیار یکسان نیست")
			}
		}
		report.Coverage = append(report.Coverage, fmt.Sprintf("تطبیق %d تراکنش هسته حسابیار", count))
		return true, rows.Err()
	})
	if err != nil {
		incomplete("هسته حسابیار")
	}
	if _, err := deriveWorkspaceLedger(state); err != nil {
		add("ledger-invalid", "critical", "accounting", "ledger", "موتور حسابداری امکان استخراج سند تراز از این داده‌ها را ندارد")
	}
	ledgerMatches, ledgerErr := supervisorPersistedLedger(r.Context(), requestctx.CompanyID(r.Context()), state)
	if ledgerErr != nil {
		incomplete("تطبیق دفتر حسابداری ذخیره‌شده")
	} else if !ledgerMatches {
		add("ledger-difference", "critical", "accounting", "ledger", "مانده دفتر ثبت‌شده با آثار اسناد مالی یکسان نیست؛ رسیدگی حسابداری لازم است")
	} else {
		report.Coverage = append(report.Coverage, "تطبیق خالص دفتر ذخیره‌شده با اسناد مالی")
	}
	// A scan can overlap an ordinary write. Never label a mixed revision healthy.
	latest, err := loadWorkspace(r, requestctx.CompanyID(r.Context()))
	if err != nil || latest.Revision != doc.Revision {
		incomplete("تغییر هم‌زمان داده؛ بازبینی دوباره لازم است")
	}
	return report, nil
}

func (h *APIHandler) SupervisorReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, 405, "Method not allowed")
		return
	}
	report, err := h.buildSupervisorReport(r)
	if err != nil {
		RespondError(w, 503, "بررسی مالی در دسترس نیست؛ سلامت قابل تأیید نیست")
		return
	}
	_, _ = postgres.WithCompanySession(r.Context(), postgres.DB, requestctx.CompanyID(r.Context()), func(q postgres.SessionQueryable) (bool, error) {
		var checked time.Time
		err := q.QueryRowContext(r.Context(), `SELECT checked_at FROM financial_supervisor_snapshots WHERE company_id=$1`, requestctx.CompanyID(r.Context())).Scan(&checked)
		if err == nil {
			report.BackgroundCheckedAt = &checked
		}
		return err == nil, err
	})
	w.Header().Set("Cache-Control", "no-store")
	RespondJSON(w, 200, report)
}

// A read-only financial scan runs even with no browser open. Only its diagnostic
// snapshot is stored; this worker never classifies, repairs or posts money.
func StartSupervisorWorker(ctx context.Context) {
	if postgres.DB == nil {
		return
	}
	h := NewAPIHandler(nil)
	go func() {
		run := func() {
			rows, err := postgres.DB.QueryContext(ctx, `SELECT id FROM companies ORDER BY id`)
			if err != nil {
				return
			}
			ids := []int64{}
			for rows.Next() {
				var id int64
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
			for _, id := range ids {
				if ctx.Err() != nil {
					return
				}
				scanCtx, cancel := context.WithTimeout(requestctx.WithIdentity(ctx, id, 0, "system", "supervisor"), 45*time.Second)
				r, _ := http.NewRequestWithContext(scanCtx, http.MethodGet, "http://internal/api/supervisor/report", nil)
				report, err := h.buildSupervisorReport(r)
				if err == nil {
					raw, _ := json.Marshal(report)
					err = postgres.WithCompanyTx(scanCtx, postgres.DB, id, func(tx *sql.Tx) error {
						_, e := tx.ExecContext(scanCtx, `INSERT INTO financial_supervisor_snapshots(company_id,revision,checked_at,report) VALUES($1,$2,$3,$4) ON CONFLICT(company_id) DO UPDATE SET revision=EXCLUDED.revision,checked_at=EXCLUDED.checked_at,report=EXCLUDED.report WHERE financial_supervisor_snapshots.checked_at<EXCLUDED.checked_at`, id, report.Revision, report.CheckedAt, raw)
						return e
					})
				}
				if err != nil {
					log.Printf("financial supervisor snapshot unavailable company=%d", id)
				}
				cancel()
			}
		}
		run()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
