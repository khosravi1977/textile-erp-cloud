package handler

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type hesabyarStock struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
	Unit string `json:"unit"`
	Qty  string `json:"qty"`
}

type hesabyarLedger struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Debit  string `json:"debit"`
	Credit string `json:"credit"`
	Count  int64  `json:"count"`
}

func (h *APIHandler) GetHesabyarStock(w http.ResponseWriter, r *http.Request) {
	readHesabyarRows(w, r, `
		SELECT i.id, i.name, i.code, COALESCE(u.name,''), SUM(l.qty_on_hand)::text
		FROM public.items i
		JOIN public.inventory_lots l ON l.item_id=i.id AND l.company_id=i.company_id
		LEFT JOIN public.uoms u ON u.id=i.uom_id
		WHERE i.company_id=$1 AND i.is_active IS TRUE AND i.is_inventory IS TRUE
		  AND l.status='Active' AND l.ownership_type='Owned'
		GROUP BY i.id, i.name, i.code, u.name ORDER BY i.id`,
		func(rows *sql.Rows) (hesabyarStock, error) {
			var out hesabyarStock
			err := rows.Scan(&out.ID, &out.Name, &out.Code, &out.Unit, &out.Qty)
			return out, err
		})
}

func (h *APIHandler) GetHesabyarLedger(w http.ResponseWriter, r *http.Request) {
	readHesabyarRows(w, r, `
		SELECT p.id, p.name, SUM(COALESCE(l.debit,0))::text,
		       SUM(COALESCE(l.credit,0))::text, COUNT(*)
		FROM public.journal_voucher_lines l
		JOIN public.journal_vouchers v ON v.id=l.journal_voucher_id AND v.company_id=l.company_id
		JOIN public.parties p ON p.id=l.party_id AND p.company_id=l.company_id
		WHERE l.company_id=$1 AND v.status='Posted' AND p.is_active IS TRUE
		  AND EXISTS (SELECT 1 FROM public.party_roles pr WHERE pr.party_id=p.id AND pr.role='CUSTOMER')
		GROUP BY p.id, p.name ORDER BY p.id`,
		func(rows *sql.Rows) (hesabyarLedger, error) {
			var out hesabyarLedger
			err := rows.Scan(&out.ID, &out.Name, &out.Debit, &out.Credit, &out.Count)
			return out, err
		})
}

func readHesabyarRows[T any](w http.ResponseWriter, r *http.Request, query string, scan func(*sql.Rows) (T, error)) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	companyID := requestctx.CompanyID(ctx)
	fail := func() { RespondError(w, http.StatusServiceUnavailable, "Hesabyar read unavailable") }
	if postgres.DB == nil || companyID <= 0 {
		fail()
		return
	}
	tx, err := postgres.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		fail()
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_catalog.set_config('app.company_id', $1, true)", strconv.FormatInt(companyID, 10)); err != nil {
		fail()
		return
	}
	rows, err := tx.QueryContext(ctx, query, companyID)
	if err != nil {
		fail()
		return
	}
	defer rows.Close()
	result := []T{}
	for rows.Next() {
		row, err := scan(rows)
		if err != nil || len(result) >= 10000 {
			fail()
			return
		}
		result = append(result, row)
	}
	if rows.Err() != nil {
		fail()
		return
	}
	writeHesabyarLookup(w, result, len(result))
}
