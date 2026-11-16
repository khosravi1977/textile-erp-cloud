package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/erpsystem/textile-erp/internal/platform/monetary"
)

type rialBankAnchor struct {
	State    map[string]any
	Revision int64
}

// A later manual movement must keep its own rial unit. Never multiply a mixed
// residual, or erase it by forcing today's balance to an older SMS balance.
func findRialBankAnchors(ctx context.Context, tx *sql.Tx, company int64, raw json.RawMessage) (map[string]rialBankAnchor, error) {
	state := decodeWorkspaceMap(raw)
	anchors := map[string]rialBankAnchor{}
	for _, account := range rowsFrom(state, "accounts") {
		if account["lastReportedBalance"] == nil || stringValue(account["currency"]) == monetary.Rial {
			continue
		}
		id := stringValue(account["id"])
		opening, err := rialInteger(account["opening"])
		if err != nil {
			return nil, err
		}
		reported, err := rialInteger(account["lastReportedBalance"])
		if err != nil {
			return nil, err
		}
		if opening+mobileAccountNet(rowsFrom(state, "movements"), id) == reported {
			continue
		}
		match := map[string]any{"id": id, "opening": account["opening"], "lastReportedBalance": account["lastReportedBalance"]}
		if value, ok := account["lastReportedJalali"]; ok {
			match["lastReportedJalali"] = value
		}
		matchRaw, _ := json.Marshal([]any{match})
		rows, err := tx.QueryContext(ctx, `SELECT revision,state FROM financial_workspace_history WHERE company_id=$1 AND state->'accounts' @> $2::jsonb ORDER BY revision DESC`, company, matchRaw)
		if err != nil {
			return nil, err
		}
		found := false
		for rows.Next() {
			var revision int64
			var prior json.RawMessage
			if err := rows.Scan(&revision, &prior); err != nil {
				rows.Close()
				return nil, err
			}
			previous := decodeWorkspaceMap(prior)
			if opening+mobileAccountNet(rowsFrom(previous, "movements"), id) == reported {
				anchors[id] = rialBankAnchor{previous, revision}
				found = true
				break
			}
		}
		queryErr := rows.Err()
		rows.Close()
		if queryErr != nil {
			return nil, queryErr
		}
		if !found {
			return nil, fmt.Errorf("cannot prove the historical bank balance anchor for %s; no values changed", id)
		}
	}
	return anchors, nil
}

func rialMovementNet(movements []map[string]any, id string) (int64, error) {
	copies := make([]map[string]any, 0, len(movements))
	for _, row := range movements {
		if stringValue(row["accountId"]) != id && stringValue(row["counterAccountId"]) != id {
			continue
		}
		unit := monetary.Rial
		if legacyMobileMoney("movements", row) {
			unit = monetary.Toman
		}
		unit, err := monetary.Unit(stringValue(row["currency"]), unit)
		if err != nil {
			return 0, err
		}
		value, err := rialInteger(row["amount"])
		if err != nil {
			return 0, err
		}
		value, err = monetary.ToRials(value, unit)
		if err != nil {
			return 0, err
		}
		copyRow := map[string]any{}
		for key, value := range row {
			copyRow[key] = value
		}
		copyRow["amount"] = value
		copies = append(copies, copyRow)
	}
	net := mobileAccountNet(copies, id)
	return monetary.ToRials(net, monetary.Rial)
}
