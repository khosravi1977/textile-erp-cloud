package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type RialMigrationResult struct {
	CompanyID          int64  `json:"company_id"`
	Revision           int64  `json:"revision"`
	Checksum           string `json:"checksum"`
	WorkspaceConverted int    `json:"workspace_converted"`
	TypedConverted     int    `json:"typed_converted"`
	AlreadyApplied     bool   `json:"already_applied"`
	Applied            bool   `json:"applied"`
}

// MigrateCompanyToRials defaults to a read-only plan. Applying requires the
// exact workspace checksum from that plan. The original records are retained
// in a tenant-scoped audit row; vouchers are reversed/reposted, never edited.
func MigrateCompanyToRials(ctx context.Context, db *sql.DB, companyID int64, apply bool, expected string) (RialMigrationResult, error) {
	result := RialMigrationResult{CompanyID: companyID}
	if companyID <= 0 {
		return result, errors.New("an explicit company id is required")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: !apply, Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.company_id',$1,true)", fmt.Sprint(companyID)); err != nil {
		return result, err
	}
	if apply {
		if err := lockRialCompany(ctx, tx, companyID); err != nil {
			return result, err
		}
	}
	var raw json.RawMessage
	query := "SELECT state,revision,checksum FROM financial_workspace_states WHERE company_id=$1"
	if apply {
		query += " FOR UPDATE"
	}
	if err := tx.QueryRowContext(ctx, query, companyID).Scan(&raw, &result.Revision, &result.Checksum); err != nil {
		return result, err
	}
	anchors, err := findRialBankAnchors(ctx, tx, companyID, raw)
	if err != nil {
		return result, err
	}
	state, converted, err := prepareRialWorkspaceWithAnchors(raw, anchors)
	if err != nil {
		return result, err
	}
	result.WorkspaceConverted = converted
	oldState := decodeWorkspaceMap(raw)
	if err := prepareRialOpeningDates(ctx, tx, companyID, oldState, state); err != nil {
		return result, err
	}
	var auditExists bool
	if err := tx.QueryRowContext(ctx, "SELECT to_regclass('financial_currency_audits') IS NOT NULL").Scan(&auditExists); err != nil {
		return result, err
	}
	if auditExists {
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM financial_currency_audits WHERE company_id=$1 AND policy_version=1)", companyID).Scan(&result.AlreadyApplied); err != nil {
			return result, err
		}
		if result.AlreadyApplied {
			return result, nil
		}
	}
	var typedRaw, allocationsRaw, partiesRaw json.RawMessage
	for _, item := range []struct {
		table  string
		target *json.RawMessage
	}{{"bank_transactions", &typedRaw}, {"transaction_allocations", &allocationsRaw}, {"ar_ap_balances", &partiesRaw}} {
		q := fmt.Sprintf("SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM %s t WHERE company_id=$1", item.table)
		if err := tx.QueryRowContext(ctx, q, companyID).Scan(item.target); err != nil {
			return result, err
		}
	}
	var typed []map[string]any
	d := json.NewDecoder(strings.NewReader(string(typedRaw)))
	d.UseNumber()
	if err := d.Decode(&typed); err != nil {
		return result, err
	}
	for _, row := range typed {
		if legacyTypedToman(row) {
			result.TypedConverted++
		}
	}
	if _, err := deriveWorkspaceLedger(state); err != nil {
		return result, fmt.Errorf("rial ledger plan: %w", err)
	}
	if !apply {
		return result, nil
	}
	if !auditExists {
		return result, errors.New("rial currency schema migration is required before applying")
	}
	if expected == "" || expected != result.Checksum {
		return result, errors.New("workspace checksum changed; produce a fresh read-only plan")
	}
	next := result.Revision + 1
	if _, err := tx.ExecContext(ctx, `INSERT INTO financial_currency_audits(company_id,policy_version,original_workspace,original_typed_transactions,original_allocations,original_party_balances,resulting_revision) VALUES($1,1,$2,$3,$4,$5,$6)`, companyID, raw, typedRaw, allocationsRaw, partiesRaw, next); err != nil {
		return result, err
	}
	for _, row := range typed {
		if err := migrateTypedRialRow(ctx, tx, companyID, next, row); err != nil {
			return result, err
		}
	}
	newRaw, _ := json.Marshal(state)
	canonical, checksum, err := validateWorkspaceState(newRaw)
	if err != nil {
		return result, err
	}
	if err := syncWorkspaceLedger(ctx, tx, companyID, 0, next, oldState, state); err != nil {
		return result, err
	}
	if err := verifyRialLedger(ctx, tx, companyID, state); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE financial_workspace_states SET state=$2,revision=$3,checksum=$4,updated_at=CURRENT_TIMESTAMP WHERE company_id=$1`, companyID, canonical, next, checksum); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO financial_workspace_history(company_id,revision,checksum,state) VALUES($1,$2,$3,$4)`, companyID, next, checksum, canonical); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	result.Applied, result.Revision, result.Checksum = true, next, checksum
	return result, nil
}

func lockRialCompany(ctx context.Context, tx *sql.Tx, companyID int64) error {
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('rial-policy:' || $1::text,0))", companyID)
	return err
}

func legacyTypedToman(row map[string]any) bool {
	return stringValue(row["currency"]) == "" && (stringValue(row["source"]) == "HESABYAR" || (stringValue(row["source"]) == "IMPORT" && strings.HasPrefix(stringValue(row["external_transaction_id"]), "HY-")))
}
