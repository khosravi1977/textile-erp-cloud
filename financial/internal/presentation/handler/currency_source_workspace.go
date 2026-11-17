package handler

import (
	"encoding/json"
	"fmt"

	"github.com/erpsystem/textile-erp/internal/platform/monetary"
)

type RialSourceCorrectionResult struct {
	CompanyID          int64  `json:"company_id"`
	Revision           int64  `json:"revision"`
	Checksum           string `json:"checksum"`
	SourceSHA256       string `json:"source_sha256"`
	ExcelRows          int    `json:"excel_rows"`
	ExcelMatched       int    `json:"excel_matched"`
	AuditRestored      int    `json:"audit_restored"`
	WorkspaceCorrected int    `json:"workspace_corrected"`
	TypedCorrected     int    `json:"typed_corrected"`
	BankAccounts       int    `json:"bank_accounts"`
	AlreadyApplied     bool   `json:"already_applied"`
	Applied            bool   `json:"applied"`
}

func workspaceIndex(state map[string]any, key string) map[string]map[string]any {
	result := map[string]map[string]any{}
	for _, row := range rowsFrom(state, key) {
		result[stringValue(row["id"])] = row
	}
	return result
}

func prepareSourceCorrection(raw, prior json.RawMessage, typed []map[string]any, e RialSourceEvidence, result *RialSourceCorrectionResult) (map[string]any, map[string]sourceVerifiedAmount, error) {
	state := decodeWorkspaceMap(raw)
	original := decodeWorkspaceMap(prior)
	verified := map[string]sourceVerifiedAmount{}
	typeIndex := sourceTypedIndex(typed)
	oldMobile := workspaceIndex(original, "mobileTransactions")
	for _, row := range rowsFrom(state, "mobileTransactions") {
		// New records received through the corrected IRR adapter are already
		// canonical. They are not part of the erroneous policy-1 conversion.
		if stringValue(row["sourceCurrency"]) == "IRR" && stringValue(row["currency"]) == "IRR" {
			continue
		}
		id := stringValue(row["externalId"])
		if id == "" {
			return nil, nil, fmt.Errorf("mobile row has no external identity")
		}
		match, err := matchRialSource(row, typeIndex[id], e, oldMobile[stringValue(row["id"])])
		if err != nil {
			return nil, nil, err
		}
		if _, exists := verified[id]; exists {
			return nil, nil, fmt.Errorf("duplicate mobile external id %s", id)
		}
		verified[id] = match
		if match.Basis == "excel" {
			result.ExcelMatched++
		} else {
			result.AuditRestored++
		}
	}
	// Update source rows and their existing projections only. No Excel-only row
	// is inserted, no source duplicate is merged, and manual documents stay intact.
	for _, key := range currencyCollections {
		oldRows := workspaceIndex(original, key)
		for _, row := range rowsFrom(state, key) {
			if stringValue(row["sourceCurrency"]) != "IRT" {
				continue
			}
			if !legacyMobileMoney(key, row) {
				return nil, nil, fmt.Errorf("unrecognized converted source in %s", key)
			}
			match, ok := verified[sourceID(row)]
			if !ok {
				// Mobile payable documents are not transactions in this Excel. Undo only
				// the exact policy-1 change retained in the immutable audit, not a guess.
				old := oldRows[stringValue(row["id"])]
				if key != "payableDocs" || old == nil || stringValue(row["source_type"]) != "hesabyar_mobile" {
					return nil, nil, fmt.Errorf("unmatched converted projection %s/%s", key, stringValue(row["id"]))
				}
				amount, err := rialInteger(old["amount"])
				if err != nil {
					return nil, nil, err
				}
				match = sourceVerifiedAmount{Amount: amount, Basis: "owner_verified_policy_1_mobile_document"}
			}
			if err := applyVerifiedSourceRow(row, match, e.SHA256); err != nil {
				return nil, nil, fmt.Errorf("%s/%s: %w", key, stringValue(row["id"]), err)
			}
			result.WorkspaceCorrected++
		}
	}
	before := decodeWorkspaceMap(raw)
	for _, account := range rowsFrom(state, "accounts") {
		if stringValue(account["sourceCurrency"]) != "IRT" {
			continue
		}
		id := stringValue(account["id"])
		previous := workspaceIndex(before, "accounts")[id]
		reported, err := rialInteger(account["lastReportedBalance"])
		if err != nil {
			return nil, nil, err
		}
		target, err := rialInteger(account["sourceReportedBalance"])
		if err != nil {
			return nil, nil, err
		}
		if reported != target*10 {
			return nil, nil, fmt.Errorf("bank %s has an unproven balance conversion", id)
		}
		proven := false
		for _, r := range e.Rows {
			if r.Balance != nil && *r.Balance == target && r.Bank == sourceBank(stringValue(account["name"])) {
				proven = true
				break
			}
		}
		if !proven {
			old := workspaceIndex(original, "accounts")[id]
			value, err := rialInteger(old["lastReportedBalance"])
			if old == nil || err != nil || value != target {
				return nil, nil, fmt.Errorf("bank %s balance lacks Excel or policy-1 evidence", id)
			}
		}
		// Preserve the exact residual after the last report, including later manual
		// rial payments. Only the verified reported balance and affected source net
		// change; forcing today's balance to a stale SMS would erase those payments.
		residual := number(previous["opening"]) + float64(mobileAccountNet(rowsFrom(before, "movements"), id)) - float64(reported)
		residualRials, err := rialInteger(residual)
		if err != nil {
			return nil, nil, err
		}
		opening := target + residualRials - mobileAccountNet(rowsFrom(state, "movements"), id)
		if _, err := monetary.ToRials(opening, monetary.Rial); err != nil {
			return nil, nil, err
		}
		account["opening"], account["lastReportedBalance"] = opening, target
		account["sourceCurrency"], account["currency"], account["currencyEvidenceSHA256"] = "IRR", "IRR", e.SHA256
		result.BankAccounts++
	}
	state["currencyPolicyVersion"], state["currency"], state["currencySourceEvidenceSHA256"] = 2, "IRR", e.SHA256
	if err := monetary.ValidateDocument(state); err != nil {
		return nil, nil, err
	}
	return state, verified, nil
}

func applyVerifiedSourceRow(row map[string]any, match sourceVerifiedAmount, hash string) error {
	current, err := rialInteger(row["amount"])
	if err != nil {
		return err
	}
	source, err := rialInteger(row["sourceAmount"])
	if err != nil {
		return err
	}
	if source != match.Amount || current != match.Amount*10 {
		return fmt.Errorf("amount differs from the recorded prior conversion")
	}
	if row["reportedBalance"] != nil {
		amount, err := rialInteger(row["reportedBalance"])
		if err != nil {
			return err
		}
		if match.Balance == nil || amount != *match.Balance*10 {
			return fmt.Errorf("reported balance differs from source evidence")
		}
		row["reportedBalance"] = *match.Balance
	}
	row["amount"], row["sourceAmount"], row["sourceCurrency"], row["currency"] = match.Amount, match.Amount, "IRR", "IRR"
	row["currencyEvidenceSHA256"] = hash
	return nil
}
