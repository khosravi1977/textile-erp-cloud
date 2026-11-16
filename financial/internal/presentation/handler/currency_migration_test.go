package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

func TestRialMigrationAtomicAuditAndReversal(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("disposable PostgreSQL is not configured")
	}
	cfg := postgres.LoadConfig()
	if !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("rial migration test requires a dedicated _test database")
	}
	db, err := postgres.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := postgres.RunMigrations(db, "../../infrastructure/persistence/postgres/migrations"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var company, party int64
	err = db.QueryRow(`INSERT INTO companies(code,name) VALUES($1,'Rial migration fixture') RETURNING id`, fmt.Sprintf("RIAL-%d", time.Now().UnixNano())).Scan(&company)
	if err != nil {
		t.Fatal(err)
	}
	err = postgres.WithCompanyTx(ctx, db, company, func(tx *sql.Tx) error {
		return tx.QueryRow(`INSERT INTO parties(company_id,code,name,type) VALUES($1,'RIAL-C','Rial customer','Customer') RETURNING id`, company).Scan(&party)
	})
	if err != nil {
		t.Fatal(err)
	}
	service := financecore.New(db)
	posted, err := service.PostTransaction(ctx, company, 0, financecore.TransactionRequest{ExternalTransactionID: "HY-rial-fixture", TransactionType: financecore.TypeCustomerReceipt, Direction: "IN", Amount: 100, PartyID: party, BankAccountName: "Rial test bank", TransactionDate: "2026-11-01", Source: financecore.SourceHesabyar}, true)
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"accounts":[{"id":"undated","name":"Undated bank","opening":99}],"expenses":[{"id":"e","amount":100,"date":"2026-11-01","group":"Test","source_type":"mobile_sms"}],"receivableDocs":[{"id":"c","amount":900,"customer":"Rial customer","date":"2026-11-01","dueDate":"2026-12-01","checkNo":"123","status":"open"}]}`)
	_, checksum, err := validateWorkspaceState(raw)
	if err != nil {
		t.Fatal(err)
	}
	err = postgres.WithCompanyTx(ctx, db, company, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE bank_transactions SET currency=NULL,source_currency=NULL,source_amount=NULL WHERE company_id=$1`, company); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO financial_workspace_states(company_id,state,revision,checksum) VALUES($1,$2,1,$3)`, company, raw, checksum)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := MigrateCompanyToRials(ctx, db, company, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Applied || plan.WorkspaceConverted != 1 || plan.TypedConverted != 1 {
		t.Fatalf("bad plan %+v", plan)
	}
	if _, err := MigrateCompanyToRials(ctx, db, company, true, "stale"); err == nil {
		t.Fatal("stale checksum accepted")
	}
	applied, err := MigrateCompanyToRials(ctx, db, company, true, checksum)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Revision != 2 {
		t.Fatalf("%+v", applied)
	}
	var newVoucher int64
	err = postgres.WithCompanyTx(ctx, db, company, func(tx *sql.Tx) error {
		var amount, audited int64
		var balance float64
		if err := tx.QueryRow(`SELECT amount,journal_voucher_id FROM bank_transactions WHERE company_id=$1 AND id=$2`, company, posted.BankTransactionID).Scan(&amount, &newVoucher); err != nil {
			return err
		}
		if amount != 1000 || newVoucher == posted.VoucherID {
			t.Fatalf("transaction/voucher not converted: %d %d", amount, newVoucher)
		}
		if err := tx.QueryRow(`SELECT debit_balance FROM ar_ap_balances WHERE company_id=$1 AND party_id=$2`, company, party).Scan(&balance); err != nil {
			return err
		}
		if balance != -1000 {
			t.Fatalf("party balance %f", balance)
		}
		if err := tx.QueryRow(`SELECT (original_typed_transactions->0->>'amount')::bigint FROM financial_currency_audits WHERE company_id=$1`, company).Scan(&audited); err != nil {
			return err
		}
		if audited != 100 {
			t.Fatal("audit did not retain original")
		}
		var got json.RawMessage
		if err := tx.QueryRow(`SELECT state FROM financial_workspace_states WHERE company_id=$1`, company).Scan(&got); err != nil {
			return err
		}
		if mobileInt64(rowsFrom(decodeWorkspaceMap(got), "receivableDocs")[0]["amount"]) != 900 {
			t.Fatal("manual rial cheque changed")
		}
		var openingNet float64
		if err := tx.QueryRow(`SELECT COALESCE(SUM(l.debit-l.credit),0) FROM journal_vouchers v JOIN journal_voucher_lines l ON l.journal_voucher_id=v.id JOIN accounts a ON a.id=l.account_id WHERE v.company_id=$1 AND v.source_reference='account-opening:undated' AND a.type='Asset'`, company).Scan(&openingNet); err != nil {
			return err
		}
		if openingNet != 99 {
			t.Fatalf("undated opening was lost or phantom-reversed: %f", openingNet)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := MigrateCompanyToRials(ctx, db, company, true, applied.Checksum)
	if err != nil || !repeat.AlreadyApplied || repeat.Applied {
		t.Fatalf("repeat %+v %v", repeat, err)
	}
	if err := service.ReverseTransaction(ctx, company, 0, posted.BankTransactionID, "test converted reversal"); err != nil {
		t.Fatal(err)
	}
	err = postgres.WithCompanyTx(ctx, db, company, func(tx *sql.Tx) error {
		var balance float64
		if err := tx.QueryRow(`SELECT debit_balance FROM ar_ap_balances WHERE company_id=$1 AND party_id=$2`, company, party).Scan(&balance); err != nil {
			return err
		}
		if balance != 0 {
			t.Fatalf("reverse left balance %f", balance)
		}
		var debit, credit float64
		err := tx.QueryRow(`SELECT SUM(l.debit),SUM(l.credit) FROM journal_voucher_lines l JOIN journal_vouchers v ON v.id=l.journal_voucher_id WHERE l.company_id=$1 AND (v.id=$2 OR v.source_reference=$3 OR v.reversal_of=$4)`, company, posted.VoucherID, fmt.Sprintf("rial-currency:typed:%d", posted.BankTransactionID), newVoucher).Scan(&debit, &credit)
		if err != nil {
			return err
		}
		if debit != credit {
			t.Fatal("unbalanced vouchers")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
