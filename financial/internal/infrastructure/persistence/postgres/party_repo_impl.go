package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/repository"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type PartyRepo struct {
	db *sql.DB
}

func NewPartyRepository(db *sql.DB) repository.PartyRepository {
	return &PartyRepo{db: db}
}

func (r *PartyRepo) Create(ctx context.Context, party *entity.Party) error {
	_, err := withTenantConn(ctx, r.db, func(q tenantQueryable) (struct{}, error) {
		query := `
        INSERT INTO parties (company_id, code, name, type, national_id, tax_id, mobile, phone, address, is_active)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
        RETURNING id, created_at`

		err := q.QueryRowContext(ctx, query,
			requestctx.CompanyID(ctx), party.Code, party.Name, party.Type, party.NationalID,
			party.TaxID, party.Mobile, party.Phone, party.Address, party.IsActive,
		).Scan(&party.ID, &party.CreatedAt)
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("create party: %w", err)
	}
	return nil
}

func (r *PartyRepo) GetByID(ctx context.Context, id int64) (*entity.Party, error) {
	return withTenantConn(ctx, r.db, func(q tenantQueryable) (*entity.Party, error) {
		query := `
        SELECT id, code, name, type, national_id, tax_id, mobile, phone, address, is_active, created_at
        FROM parties WHERE id = $1 AND company_id = $2`

		party := &entity.Party{}
		err := q.QueryRowContext(ctx, query, id, requestctx.CompanyID(ctx)).Scan(
			&party.ID, &party.Code, &party.Name, &party.Type,
			&party.NationalID, &party.TaxID, &party.Mobile, &party.Phone,
			&party.Address, &party.IsActive, &party.CreatedAt,
		)

		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get party: %w", err)
		}
		return party, nil
	})
}

func (r *PartyRepo) Update(ctx context.Context, party *entity.Party) error {
	_, err := withTenantConn(ctx, r.db, func(q tenantQueryable) (struct{}, error) {
		query := `
        UPDATE parties SET name=$1, type=$2, national_id=$3, tax_id=$4, 
        mobile=$5, phone=$6, address=$7, is_active=$8
        WHERE id=$9 AND company_id=$10`

		_, err := q.ExecContext(ctx, query,
			party.Name, party.Type, party.NationalID, party.TaxID,
			party.Mobile, party.Phone, party.Address, party.IsActive, party.ID,
			requestctx.CompanyID(ctx),
		)
		return struct{}{}, err
	})
	return err
}

func (r *PartyRepo) Delete(ctx context.Context, id int64) error {
	_, err := withTenantConn(ctx, r.db, func(q tenantQueryable) (struct{}, error) {
		_, err := q.ExecContext(ctx, "DELETE FROM parties WHERE id=$1 AND company_id=$2", id, requestctx.CompanyID(ctx))
		return struct{}{}, err
	})
	return err
}

func (r *PartyRepo) List(ctx context.Context, partyType entity.PartyType) ([]*entity.Party, error) {
	return withTenantConn(ctx, r.db, func(q tenantQueryable) ([]*entity.Party, error) {
		query := `
        SELECT id, code, name, type, national_id, tax_id, mobile, phone, address, is_active, created_at
        FROM parties WHERE type=$1 AND company_id=$2 ORDER BY name`

		rows, err := q.QueryContext(ctx, query, partyType, requestctx.CompanyID(ctx))
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var parties []*entity.Party
		for rows.Next() {
			p := &entity.Party{}
			err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Type,
				&p.NationalID, &p.TaxID, &p.Mobile, &p.Phone,
				&p.Address, &p.IsActive, &p.CreatedAt)
			if err != nil {
				return nil, err
			}
			parties = append(parties, p)
		}
		return parties, nil
	})
}
