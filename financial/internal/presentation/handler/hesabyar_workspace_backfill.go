package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func (h *APIHandler) syncHesabyarTransactionsIntoWorkspace(r *http.Request) error {
	service := h.financeCore()
	if service == nil {
		return nil
	}
	companyID := requestctx.CompanyID(r.Context())
	if companyID <= 0 {
		return nil
	}
	transactions, err := service.ListTransactions(r.Context(), companyID, 500)
	if err != nil || len(transactions) == 0 {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		doc, err := loadWorkspace(r, companyID)
		if err != nil {
			return err
		}
		state := decodeWorkspaceMap(doc.State)
		if !mergeHesabyarTransactionsIntoWorkspaceState(state, transactions, time.Now().UTC()) {
			return nil
		}
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		payload, checksum, err := validateWorkspaceState(raw)
		if err != nil {
			return err
		}
		revision := doc.Revision
		_, err = saveWorkspace(
			r,
			companyID,
			0,
			&revision,
			payload,
			checksum,
			map[string]bool{"accounts": true, "mobileTransactions": true, "movements": true, "expenses": true},
		)
		if err == nil {
			return nil
		}
		var conflict workspaceConflict
		if !errors.As(err, &conflict) {
			return err
		}
	}
	return errors.New("workspace changed repeatedly while HesabYar transactions were being backfilled")
}

func mergeHesabyarTransactionsIntoWorkspaceState(state map[string]any, transactions []financecore.BankTransaction, syncedAt time.Time) bool {
	if state == nil {
		return false
	}
	accounts := rowsFrom(state, "accounts")
	changed := false
	for _, tx := range transactions {
		if !shouldBackfillHesabyarTransaction(tx) {
			continue
		}
		externalID := workspaceHesabyarExternalID(tx.ExternalID)
		if externalID == "" || workspaceHasMobileTransaction(state, externalID) {
			continue
		}
		accountID := ensureWorkspaceBankAccount(&accounts, tx)
		typedType := strings.ToUpper(strings.TrimSpace(tx.TransactionType))
		legacyType := legacyStateTypeForTyped(typedType)
		direction := strings.ToLower(strings.TrimSpace(tx.Direction))
		if direction != "in" && direction != "out" {
			if strings.EqualFold(tx.Direction, "IN") {
				direction = "in"
			} else {
				direction = "out"
			}
		}
		date := mobileAccountingDate(tx.TransactionDate)
		occurredJalali := ""
		if strings.Contains(tx.TransactionDate, "/") {
			occurredJalali = strings.TrimSpace(tx.TransactionDate)
		}
		partyName := strings.TrimSpace(tx.PartyName)
		typed := &typedStateMeta{TypedType: typedType, PartyName: partyName, CandidateName: partyName, ExpenseLike: typedExpenseLike(typedType)}
		group, subgroup := normalizeMobileCategory("", "", legacyType, typed)
		counterparty := strings.Trim(strings.TrimSpace(group+" / "+subgroup), " /")
		if partyName != "" {
			counterparty = partyName
		}
		trackingNo := strings.TrimSpace(tx.ExternalID)
		if trackingNo == "" {
			trackingNo = externalID
		}
		now := syncedAt.Format(time.RFC3339)
		mobileRow := map[string]any{
			"id": "sms-" + externalID, "externalId": externalID, "title": tx.Description,
			"amount": tx.Amount, "direction": direction, "transactionType": legacyType,
			"transactionTypeExplicit": true, "accountId": accountID, "group": group,
			"subgroup": subgroup, "counterparty": counterparty, "bank": tx.BankAccountName,
			"trackingNo": trackingNo, "occurredAt": date, "occurredJalali": occurredJalali,
			"syncedAt": now, "typedType": typedType, "source_type": "mobile_sms",
			"sourceId": externalID, "typedCoreBackfill": true,
		}
		if partyName != "" {
			applyConfirmedCounterparty(mobileRow, partyName)
		}
		movement := map[string]any{
			"id": "mov-sms-" + externalID, "accountId": accountID, "date": date,
			"occurredJalali": occurredJalali, "direction": direction, "transactionType": legacyType,
			"amount": tx.Amount, "counterparty": counterparty, "trackingNo": trackingNo,
			"description": tx.Description, "sourceMobileTransaction": externalID,
			"source_type": "mobile_sms", "sourceId": externalID, "bank": tx.BankAccountName,
			"group": group, "subgroup": subgroup, "typedType": typedType, "typedCoreBackfill": true,
		}
		if partyName != "" {
			applyConfirmedCounterparty(movement, partyName)
		}
		if legacyType == "transfer" && tx.BankAccountName != "" {
			movement["counterAccount"] = tx.BankAccountName
		}
		if typed.ExpenseLike && legacyType == "expense" {
			expenseID := "exp-sms-" + externalID
			expense := map[string]any{
				"id": expenseID, "date": date, "occurredJalali": occurredJalali,
				"group": group, "subgroup": subgroup, "amount": tx.Amount,
				"description": tx.Description, "accountId": accountID, "counterparty": counterparty,
				"source_type": "mobile_sms", "sourceId": externalID, "bank": tx.BankAccountName,
				"typedType": typedType, "typedCoreBackfill": true,
			}
			if partyName != "" {
				applyConfirmedCounterparty(expense, partyName)
			}
			state["expenses"] = append([]any{expense}, anyRows(state, "expenses")...)
			movement["sourceExpense"] = expenseID
		}
		state["mobileTransactions"] = append([]any{mobileRow}, anyRows(state, "mobileTransactions")...)
		state["movements"] = append([]any{movement}, anyRows(state, "movements")...)
		changed = true
	}
	if changed {
		state["accounts"] = mapsToAny(accounts)
	}
	return changed
}

func shouldBackfillHesabyarTransaction(tx financecore.BankTransaction) bool {
	if !strings.EqualFold(strings.TrimSpace(tx.Source), financecore.SourceHesabyar) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(tx.Status), "VOIDED") {
		return false
	}
	return strings.TrimSpace(tx.ExternalID) != "" && tx.Amount > 0
}

func workspaceHesabyarExternalID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "HY-")
	return strings.TrimSpace(value)
}

func workspaceHasMobileTransaction(state map[string]any, externalID string) bool {
	values := map[string]bool{externalID: true, "HY-" + externalID: true}
	for _, key := range []string{"mobileTransactions", "movements", "expenses"} {
		for _, row := range rowsFrom(state, key) {
			for _, field := range []string{"externalId", "sourceId", "sourceMobileTransaction"} {
				if values[strings.TrimSpace(stringValue(row[field]))] {
					return true
				}
			}
		}
	}
	return false
}

func ensureWorkspaceBankAccount(accounts *[]map[string]any, tx financecore.BankTransaction) string {
	name := strings.TrimSpace(mobileCanonicalBankName(tx.BankAccountName))
	if name == "" {
		name = strings.TrimSpace(tx.BankAccountName)
	}
	if name == "" {
		name = "حساب حسابیار"
	}
	typedID := ""
	if tx.BankAccountID > 0 {
		typedID = strconv.FormatInt(tx.BankAccountID, 10)
	}
	for _, account := range *accounts {
		if typedID != "" && (stringValue(account["typedBankAccountId"]) == typedID || stringValue(account["id"]) == typedID) {
			return strings.TrimSpace(stringValue(account["id"]))
		}
		if strings.EqualFold(strings.TrimSpace(stringValue(account["name"])), name) || strings.EqualFold(strings.TrimSpace(stringValue(account["name"])), tx.BankAccountName) {
			return strings.TrimSpace(stringValue(account["id"]))
		}
	}
	id := "bank-typed-" + pairingHash(name)[:12]
	*accounts = append(*accounts, map[string]any{
		"id": id, "name": name, "type": "بانک", "opening": int64(0),
		"source": "typed_hesabyar", "mobileManaged": true, "typedBankAccountId": typedID,
	})
	return id
}
