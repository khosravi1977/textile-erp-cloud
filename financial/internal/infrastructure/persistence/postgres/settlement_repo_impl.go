package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/repository"
	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type SettlementRepo struct {
	db *sql.DB
}

func NewSettlementRepository(db *sql.DB) repository.SettlementRepository {
	return &SettlementRepo{db: db}
}

func (r *SettlementRepo) Create(ctx context.Context, s *entity.SettlementHeader) error {
	tx, err := beginTenantTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	companyID := requestctx.CompanyID(ctx)
	err = tx.QueryRowContext(ctx,
		"INSERT INTO settlements (company_id, branch_id, party_id, total_amount, status, created_by) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at",
		companyID, 1, s.PartyID, s.TotalAmount.ToRials(), s.Status, s.CreatedBy,
	).Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		return fmt.Errorf("create settlement: %w", err)
	}

	for i := range s.Lines {
		line := &s.Lines[i]
		var itemID, qty interface{}
		var checkNo, bankName interface{}
		var checkDueDate interface{}

		if line.ItemID != nil {
			itemID = *line.ItemID
		}
		if line.Qty != nil {
			qty = *line.Qty
		}
		if line.CheckNo != "" {
			checkNo = line.CheckNo
		}
		if line.BankName != "" {
			bankName = line.BankName
		}
		if line.CheckDueDate != nil {
			checkDueDate = *line.CheckDueDate
		}

		err = tx.QueryRowContext(ctx,
			`INSERT INTO settlement_lines (company_id, settlement_id, settlement_type, amount, item_id, qty, check_no, check_due_date, bank_name)
             VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at`,
			companyID, s.ID, line.SettlementType, line.Amount.ToRials(), itemID, qty, checkNo, checkDueDate, bankName,
		).Scan(&line.ID, &line.CreatedAt)
		if err != nil {
			return fmt.Errorf("create settlement line: %w", err)
		}
	}

	return tx.Commit()
}

func (r *SettlementRepo) GetByID(ctx context.Context, id int64) (*entity.SettlementHeader, error) {
	return withTenantConn(ctx, r.db, func(q tenantQueryable) (*entity.SettlementHeader, error) {
		s := &entity.SettlementHeader{}
		var totalRials int64
		err := q.QueryRowContext(ctx,
			"SELECT id, party_id, settlement_date, total_amount, status, created_by, created_at FROM settlements WHERE id=$1 AND company_id=$2", id, requestctx.CompanyID(ctx),
		).Scan(&s.ID, &s.PartyID, &s.SettlementDate, &totalRials, &s.Status, &s.CreatedBy, &s.CreatedAt)

		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		s.TotalAmount = valueobject.FromRials(totalRials)

		rows, err := q.QueryContext(ctx,
			"SELECT id, settlement_type, amount, check_no, check_due_date, bank_name, item_id, qty, created_at FROM settlement_lines WHERE settlement_id=$1 AND company_id=$2", id, requestctx.CompanyID(ctx))
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			line := entity.SettlementLine{SettlementID: id}
			var amountRials int64
			var itemID sql.NullInt64
			var qty sql.NullFloat64
			var checkNo, bankName sql.NullString
			var checkDueDate sql.NullTime

			err := rows.Scan(&line.ID, &line.SettlementType, &amountRials,
				&checkNo, &checkDueDate, &bankName, &itemID, &qty, &line.CreatedAt)
			if err != nil {
				return nil, err
			}

			line.Amount = valueobject.FromRials(amountRials)
			if checkNo.Valid {
				line.CheckNo = checkNo.String
			}
			if bankName.Valid {
				line.BankName = bankName.String
			}
			if checkDueDate.Valid {
				t := checkDueDate.Time
				line.CheckDueDate = &t
			}
			if itemID.Valid {
				v := itemID.Int64
				line.ItemID = &v
			}
			if qty.Valid {
				v := qty.Float64
				line.Qty = &v
			}

			s.Lines = append(s.Lines, line)
		}
		return s, nil
	})
}

func (r *SettlementRepo) ListByParty(ctx context.Context, partyID int64) ([]*entity.SettlementHeader, error) {
	return withTenantConn(ctx, r.db, func(q tenantQueryable) ([]*entity.SettlementHeader, error) {
		query := "SELECT id, party_id, settlement_date, total_amount, status FROM settlements WHERE party_id=$1 AND company_id=$2 ORDER BY settlement_date DESC"
		rows, err := q.QueryContext(ctx, query, partyID, requestctx.CompanyID(ctx))
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var settlements []*entity.SettlementHeader
		for rows.Next() {
			s := &entity.SettlementHeader{}
			var totalRials int64
			if err := rows.Scan(&s.ID, &s.PartyID, &s.SettlementDate, &totalRials, &s.Status); err != nil {
				return nil, err
			}
			s.TotalAmount = valueobject.FromRials(totalRials)
			settlements = append(settlements, s)
		}
		return settlements, nil
	})
}
