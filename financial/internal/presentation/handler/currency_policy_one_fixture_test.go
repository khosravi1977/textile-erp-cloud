package handler

// Historical policy-1 replay exists only in tests. Production binaries cannot
// execute the retired source-guessing migration. These fixtures reproduce its
// failure so the evidence-based correction is tested against actual behavior.
import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
	"github.com/erpsystem/textile-erp/internal/platform/monetary"
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

func legacyTypedToman(row map[string]any) bool {
	return stringValue(row["currency"]) == "" && (stringValue(row["source"]) == "HESABYAR" || (stringValue(row["source"]) == "IMPORT" && strings.HasPrefix(stringValue(row["external_transaction_id"]), "HY-")))
}

func migrateTypedRialRow(ctx context.Context, tx *sql.Tx, companyID, revision int64, row map[string]any) error {
	id, err := rialInteger(row["id"])
	if err != nil {
		return err
	}
	if stringValue(row["currency"]) == monetary.Rial {
		return nil
	}
	if !legacyTypedToman(row) {
		_, err = tx.ExecContext(ctx, "UPDATE bank_transactions SET currency='IRR',source_currency='IRR',source_amount=amount WHERE company_id=$1 AND id=$2 AND currency IS NULL", companyID, id)
		return err
	}
	oldAmount, err := rialInteger(row["amount"])
	if err != nil {
		return err
	}
	amount, err := monetary.ToRials(oldAmount, monetary.Toman)
	if err != nil {
		return err
	}
	interest, err := rialInteger(row["interest_amount"])
	if err != nil {
		return err
	}
	newInterest, err := monetary.ToRials(interest, monetary.Toman)
	if err != nil {
		return err
	}
	voucherID, err := rialInteger(row["journal_voucher_id"])
	if err != nil {
		return err
	}
	newVoucherID := voucherID
	active := stringValue(row["status"]) == "ACTIVE" && stringValue(row["posting_status"]) == "POSTED"
	if voucherID > 0 && active {
		entry, err := typedCurrencyEntry(ctx, tx, companyID, id, voucherID)
		if err != nil {
			return err
		}
		branchID, err := ensureLedgerBranch(ctx, tx, companyID)
		if err != nil {
			return err
		}
		if err := financecore.ReverseCurrencyVoucher(ctx, tx, companyID, voucherID); err != nil {
			return err
		}
		for i := range entry.Lines {
			if math.Abs(entry.Lines[i].Debit) > float64(monetary.MaxAmount/10) || math.Abs(entry.Lines[i].Credit) > float64(monetary.MaxAmount/10) {
				return fmt.Errorf("typed voucher exceeds safe rial range")
			}
			entry.Lines[i].Debit *= 10
			entry.Lines[i].Credit *= 10
		}
		if err := insertLedgerEntry(ctx, tx, companyID, 0, branchID, revision, entry, "N", ledgerHash(entry)); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT id FROM journal_vouchers WHERE company_id=$1 AND source_reference=$2 AND workspace_revision=$3 ORDER BY id DESC LIMIT 1", companyID, entry.Key, revision).Scan(&newVoucherID); err != nil {
			return err
		}
	}
	partyID, err := rialInteger(row["party_id"])
	if err != nil {
		return err
	}
	if active && partyID > 0 {
		req := financecore.TransactionRequest{TransactionType: stringValue(row["transaction_type"]), Direction: stringValue(row["direction"]), Amount: amount - oldAmount, InterestAmount: newInterest - interest, PartyID: partyID, Currency: monetary.Rial}
		if err := financecore.ApplyCurrencyPartyDelta(ctx, tx, companyID, req); err != nil {
			return err
		}
	}
	var invalidAllocation bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM transaction_allocations WHERE company_id=$1 AND bank_transaction_id=$2 AND ABS(allocated_amount)>$3)", companyID, id, monetary.MaxAmount/10).Scan(&invalidAllocation); err != nil {
		return err
	}
	if invalidAllocation {
		return fmt.Errorf("allocation exceeds safe rial range")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE transaction_allocations SET allocated_amount=allocated_amount*10 WHERE company_id=$1 AND bank_transaction_id=$2", companyID, id); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE bank_transactions SET amount=$3,interest_amount=NULLIF($4,0),currency='IRR',source_currency='IRT',source_amount=$5,journal_voucher_id=NULLIF($6,0),updated_at=CURRENT_TIMESTAMP WHERE company_id=$1 AND id=$2 AND currency IS NULL`, companyID, id, amount, newInterest, oldAmount, newVoucherID)
	return err
}

// prepareRialWorkspace is a pure, replay-safe migration. Only the proven
// Android sources (or an explicit IRT unit) are scaled; manual rial invoices
// and cheques retain their numbers. The original state is separately audited.
func prepareRialWorkspace(raw json.RawMessage) (map[string]any, int, error) {
	return prepareRialWorkspaceWithAnchors(raw, nil)
}

func prepareRialWorkspaceWithAnchors(raw json.RawMessage, anchors map[string]rialBankAnchor) (map[string]any, int, error) {
	var state map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&state); err != nil {
		return nil, 0, err
	}
	anchorNets := map[string]int64{}
	for _, account := range rowsFrom(state, "accounts") {
		if account["lastReportedBalance"] == nil || stringValue(account["currency"]) == monetary.Rial {
			continue
		}
		id := stringValue(account["id"])
		opening, err := rialInteger(account["opening"])
		if err != nil {
			return nil, 0, err
		}
		reported, err := rialInteger(account["lastReportedBalance"])
		if err != nil {
			return nil, 0, err
		}
		movements := rowsFrom(state, "movements")
		if opening+mobileAccountNet(movements, id) != reported {
			anchor, ok := anchors[id]
			if !ok {
				return nil, 0, fmt.Errorf("historical bank balance anchor required for %s", id)
			}
			movements = rowsFrom(anchor.State, "movements")
			if opening+mobileAccountNet(movements, id) != reported {
				return nil, 0, fmt.Errorf("invalid historical bank anchor for %s", id)
			}
			account["sourceBalanceAnchorRevision"] = anchor.Revision
		}
		anchorNets[id], err = rialMovementNet(movements, id)
		if err != nil {
			return nil, 0, err
		}
	}
	count := 0
	for _, key := range currencyCollections {
		for _, row := range rowsFrom(state, key) {
			fallback := monetary.Rial
			if legacyMobileMoney(key, row) {
				fallback = monetary.Toman
			}
			unit, err := monetary.Unit(stringValue(row["currency"]), fallback)
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %w", key, err)
			}
			if unit == monetary.Toman {
				if row["amount"] != nil {
					row["sourceAmount"] = row["amount"]
				}
				row["sourceCurrency"] = monetary.Toman
				if err := scaleLegacyMoney(row); err != nil {
					return nil, 0, fmt.Errorf("%s: %w", key, err)
				}
				count++
			}
			row["currency"] = monetary.Rial
		}
	}
	for _, row := range rowsFrom(state, "accounts") {
		if stringValue(row["currency"]) == monetary.Rial {
			continue
		}
		unit, err := monetary.Unit(stringValue(row["currency"]), monetary.Rial)
		if err != nil {
			return nil, 0, err
		}
		if row["lastReportedBalance"] != nil || unit == monetary.Toman {
			// A reported SMS bank balance is proven IRT. Recompute opening
			// against the converted movements, including transfer counterlegs.
			original := row["lastReportedBalance"]
			if original == nil {
				original = row["opening"]
			}
			value, err := rialInteger(original)
			if err != nil {
				return nil, 0, err
			}
			balance, err := monetary.ToRials(value, monetary.Toman)
			if err != nil {
				return nil, 0, err
			}
			row["sourceOpening"] = row["opening"]
			if row["lastReportedBalance"] != nil {
				row["sourceReportedBalance"] = original
				row["lastReportedBalance"] = balance
				row["opening"] = balance - anchorNets[stringValue(row["id"])]
			} else {
				row["opening"] = balance
			}
			row["sourceCurrency"] = monetary.Toman
			count++
		}
		row["currency"] = monetary.Rial
	}
	state["currency"] = monetary.Rial
	state["currencyPolicyVersion"] = 1
	if err := monetary.ValidateDocument(state); err != nil {
		return nil, 0, err
	}
	return state, count, nil
}

func scaleLegacyMoney(row map[string]any) error {
	for _, key := range []string{"amount", "total", "subtotal", "taxAmount", "unitPrice", "costAmount", "costUnitPrice", "paid", "reportedBalance", "debit", "credit", "unitCost", "value", "price"} {
		if row[key] == nil || row[key] == "" {
			continue
		}
		value, err := rialInteger(row[key])
		if err != nil {
			return err
		}
		converted, err := monetary.ToRials(value, monetary.Toman)
		if err != nil {
			return err
		}
		row[key] = converted
	}
	for _, key := range []string{"payments", "lines"} {
		for _, nested := range rowsFrom(row, key) {
			if err := scaleLegacyMoney(nested); err != nil {
				return err
			}
		}
	}
	if _, exists := row["currency"]; exists {
		row["currency"] = monetary.Rial
	}
	return nil
}
