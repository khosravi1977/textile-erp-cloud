package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func decodeSourceRows(raw json.RawMessage) ([]map[string]any, error) {
	var rows []map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	err := d.Decode(&rows)
	return rows, err
}
func sourceNumericIndex(rows []map[string]any) map[int64]map[string]any {
	result := map[int64]map[string]any{}
	for _, r := range rows {
		id, _ := rialInteger(r["id"])
		result[id] = r
	}
	return result
}

// ReconcileCompanyRialSource applies only owner-confirmed source evidence or
// exact, explicitly approved restoration of the retained policy-1 originals.
// Plan is read-only. Apply preserves a second immutable audit and reconciles
// workspace, typed postings, allocation and party changes in one transaction.
func ReconcileCompanyRialSource(ctx context.Context, db *sql.DB, company int64, e RialSourceEvidence, apply bool, expected string) (RialSourceCorrectionResult, error) {
	result := RialSourceCorrectionResult{CompanyID: company, SourceSHA256: e.SHA256, ExcelRows: len(e.Rows)}
	if company <= 0 || e.Currency != "IRR" || len(e.SHA256) != 64 || len(e.Rows) == 0 {
		return result, fmt.Errorf("explicit company and verified IRR source evidence are required")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: !apply, Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.company_id',$1,true)", fmt.Sprint(company)); err != nil {
		return result, err
	}
	if apply {
		if err := lockRialCompany(ctx, tx, company); err != nil {
			return result, err
		}
	}
	var raw, prior, priorTyped, priorAlloc json.RawMessage
	query := "SELECT state,revision,checksum FROM financial_workspace_states WHERE company_id=$1"
	if apply {
		query += " FOR UPDATE"
	}
	if err := tx.QueryRowContext(ctx, query, company).Scan(&raw, &result.Revision, &result.Checksum); err != nil {
		return result, err
	}
	var alreadyHash string
	err = tx.QueryRowContext(ctx, "SELECT source_evidence->>'sha256' FROM financial_currency_audits WHERE company_id=$1 AND policy_version=2", company).Scan(&alreadyHash)
	if err == nil {
		if alreadyHash != e.SHA256 {
			return result, fmt.Errorf("a different source correction is already audited")
		}
		result.AlreadyApplied = true
		return result, nil
	}
	if err != sql.ErrNoRows {
		return result, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT original_workspace,original_typed_transactions,original_allocations FROM financial_currency_audits WHERE company_id=$1 AND policy_version=1", company).Scan(&prior, &priorTyped, &priorAlloc); err != nil {
		return result, err
	}
	var typedRaw, allocRaw, partiesRaw json.RawMessage
	for _, item := range []struct {
		table  string
		target *json.RawMessage
	}{{"bank_transactions", &typedRaw}, {"transaction_allocations", &allocRaw}, {"ar_ap_balances", &partiesRaw}} {
		if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM %s t WHERE company_id=$1", item.table), company).Scan(item.target); err != nil {
			return result, err
		}
	}
	typed, err := decodeSourceRows(typedRaw)
	if err != nil {
		return result, err
	}
	state, verified, err := prepareSourceCorrection(raw, prior, typed, e, &result)
	if err != nil {
		return result, err
	}
	originalTyped, err := decodeSourceRows(priorTyped)
	if err != nil {
		return result, err
	}
	oldTyped := sourceNumericIndex(originalTyped)
	originalAlloc, err := decodeSourceRows(priorAlloc)
	if err != nil {
		return result, err
	}
	oldAlloc := sourceNumericIndex(originalAlloc)
	for _, row := range typed {
		if stringValue(row["source_currency"]) == "IRT" {
			result.TypedCorrected++
		}
	}
	oldState := decodeWorkspaceMap(raw)
	if err := prepareRialOpeningDates(ctx, tx, company, oldState, state); err != nil {
		return result, err
	}
	if _, err := deriveWorkspaceLedger(state); err != nil {
		return result, err
	}
	if !apply {
		return result, nil
	}
	if expected == "" || expected != result.Checksum {
		return result, fmt.Errorf("workspace changed; generate a fresh read-only plan")
	}
	next := result.Revision + 1
	evidenceRaw, err := json.Marshal(e)
	if err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO financial_currency_audits(company_id,policy_version,original_workspace,original_typed_transactions,original_allocations,original_party_balances,resulting_revision,source_evidence) VALUES($1,2,$2,$3,$4,$5,$6,$7)`, company, raw, typedRaw, allocRaw, partiesRaw, next, evidenceRaw); err != nil {
		return result, err
	}
	for _, row := range typed {
		if stringValue(row["source_currency"]) != "IRT" {
			continue
		}
		external := strings.TrimPrefix(stringValue(row["external_transaction_id"]), "HY-")
		match, ok := verified[external]
		if !ok || stringValue(row["source"]) == "OPERATIONAL" {
			return result, fmt.Errorf("typed source %s lacks verified evidence", external)
		}
		id, err := rialInteger(row["id"])
		if err != nil {
			return result, err
		}
		if err := correctTypedRialSource(ctx, tx, company, next, row, oldTyped[id], match, oldAlloc); err != nil {
			return result, err
		}
	}
	canonicalRaw, err := json.Marshal(state)
	if err != nil {
		return result, err
	}
	canonical, checksum, err := validateWorkspaceState(canonicalRaw)
	if err != nil {
		return result, err
	}
	if err := syncWorkspaceLedger(ctx, tx, company, 0, next, oldState, state); err != nil {
		return result, err
	}
	if err := verifyRialLedger(ctx, tx, company, state); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE financial_workspace_states SET state=$2,revision=$3,checksum=$4,updated_at=CURRENT_TIMESTAMP WHERE company_id=$1", company, canonical, next, checksum); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO financial_workspace_history(company_id,revision,checksum,state) VALUES($1,$2,$3,$4)", company, next, checksum, canonical); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	result.Applied, result.Revision, result.Checksum = true, next, checksum
	return result, nil
}
