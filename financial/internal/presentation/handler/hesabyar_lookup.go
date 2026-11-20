package handler

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
	"github.com/erpsystem/textile-erp/internal/presentation/middleware"
)

type hesabyarParty struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Roles  []string `json:"roles"`
}

func (h *APIHandler) GetHesabyarParties(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if postgres.DB == nil {
		RespondError(w, http.StatusServiceUnavailable, "Financial lookup unavailable")
		return
	}
	companyID := requestctx.CompanyID(ctx)
	rows, err := readHesabyarParties(ctx, postgres.DB, companyID)
	if err != nil {
		RespondError(w, http.StatusServiceUnavailable, "Financial lookup unavailable")
		return
	}
	writeHesabyarLookup(w, rows, len(rows))
}

func readHesabyarParties(ctx context.Context, db *sql.DB, companyID int64) ([]hesabyarParty, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_catalog.set_config('app.company_id', $1, true)", strconv.FormatInt(companyID, 10)); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, p.name
		FROM public.parties p
		WHERE p.company_id = $1 AND p.is_active IS TRUE
		  AND EXISTS (
			SELECT 1 FROM public.party_roles pr
			WHERE pr.party_id = p.id AND pr.role = 'CUSTOMER'
		  )
		ORDER BY p.name, p.id
	`, companyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []hesabyarParty{}
	for rows.Next() {
		row := hesabyarParty{Status: "ACTIVE", Roles: []string{"CUSTOMER"}}
		if err := rows.Scan(&row.ID, &row.Name); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (h *APIHandler) getHesabyarCatalog(w http.ResponseWriter, r *http.Request, catalog string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := h.operational.HesabyarCatalog(ctx, requestctx.CompanyID(ctx), catalog)
	if err != nil {
		RespondError(w, http.StatusServiceUnavailable, "Operational lookup unavailable")
		return
	}
	writeHesabyarLookup(w, rows, len(rows))
}

func writeHesabyarLookup(w http.ResponseWriter, rows any, count int) {
	w.Header().Set("Cache-Control", "no-store")
	RespondJSON(w, http.StatusOK, map[string]any{"success": true, "rows": rows, "total": count})
}

func (h *APIHandler) GetOperationalKalaItems(w http.ResponseWriter, r *http.Request) {
	if requestctx.UserRole(r.Context()) == middleware.HesabyarLookupRole {
		h.getHesabyarCatalog(w, r, "kala-items")
		return
	}
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	h.handleOperationalLookup(w, r, "kala-items", bridge.KalaItems, bridge.CreateKalaItem)
}

func (h *APIHandler) GetOperationalYarnItems(w http.ResponseWriter, r *http.Request) {
	if requestctx.UserRole(r.Context()) == middleware.HesabyarLookupRole {
		h.getHesabyarCatalog(w, r, "yarn-items")
		return
	}
	bridge, cleanup := h.requireOperational(w, r)
	if bridge == nil {
		return
	}
	defer cleanup()
	h.handleOperationalLookup(w, r, "yarn-items", bridge.YarnItems, bridge.CreateYarnItem)
}
