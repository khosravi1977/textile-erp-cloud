package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/repository"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type ProductionRepo struct {
	db *sql.DB
}

func NewProductionRepository(db *sql.DB) repository.ProductionRepository {
	return &ProductionRepo{db: db}
}

func (r *ProductionRepo) Create(ctx context.Context, order *entity.ProductionOrder) error {
	_, err := withTenantConn(ctx, r.db, func(q tenantQueryable) (struct{}, error) {
		query := `
        INSERT INTO production_orders (company_id, branch_id, order_no, customer_party_id, product_item_id, warehouse_id, status, created_by)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING id, created_at`

		err := q.QueryRowContext(ctx, query,
			requestctx.CompanyID(ctx), 1, order.OrderNo, order.CustomerPartyID, order.ProductItemID,
			order.WarehouseID, order.Status, order.CreatedBy,
		).Scan(&order.ID, &order.CreatedAt)
		return struct{}{}, err
	})

	if err != nil {
		return fmt.Errorf("create production order: %w", err)
	}
	return nil
}

func (r *ProductionRepo) GetByID(ctx context.Context, id int64) (*entity.ProductionOrder, error) {
	return withTenantConn(ctx, r.db, func(q tenantQueryable) (*entity.ProductionOrder, error) {
		query := `
        SELECT id, order_no, customer_party_id, product_item_id, warehouse_id,
               start_date, end_date, status, total_yarn_input, total_fabric_output, created_at, created_by
        FROM production_orders WHERE id = $1 AND company_id = $2`

		order := &entity.ProductionOrder{}
		err := q.QueryRowContext(ctx, query, id, requestctx.CompanyID(ctx)).Scan(
			&order.ID, &order.OrderNo, &order.CustomerPartyID, &order.ProductItemID,
			&order.WarehouseID, &order.StartDate, &order.EndDate, &order.Status,
			&order.TotalYarnInput, &order.TotalFabricOutput, &order.CreatedAt, &order.CreatedBy,
		)

		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("get production order: %w", err)
		}
		return order, nil
	})
}

func (r *ProductionRepo) UpdateStatus(ctx context.Context, id int64, status entity.ProductionStatus) error {
	_, err := withTenantConn(ctx, r.db, func(q tenantQueryable) (struct{}, error) {
		_, err := q.ExecContext(ctx, "UPDATE production_orders SET status=$1, version_number=version_number+1 WHERE id=$2 AND company_id=$3", status, id, requestctx.CompanyID(ctx))
		return struct{}{}, err
	})
	return err
}

func (r *ProductionRepo) ListByCustomer(ctx context.Context, customerID int64) ([]*entity.ProductionOrder, error) {
	return withTenantConn(ctx, r.db, func(q tenantQueryable) ([]*entity.ProductionOrder, error) {
		query := `
        SELECT id, order_no, customer_party_id, product_item_id, warehouse_id,
               start_date, end_date, status, total_yarn_input, total_fabric_output, created_at, created_by
        FROM production_orders WHERE customer_party_id=$1 AND company_id=$2 ORDER BY created_at DESC`

		rows, err := q.QueryContext(ctx, query, customerID, requestctx.CompanyID(ctx))
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var orders []*entity.ProductionOrder
		for rows.Next() {
			o := &entity.ProductionOrder{}
			err := rows.Scan(&o.ID, &o.OrderNo, &o.CustomerPartyID, &o.ProductItemID,
				&o.WarehouseID, &o.StartDate, &o.EndDate, &o.Status,
				&o.TotalYarnInput, &o.TotalFabricOutput, &o.CreatedAt, &o.CreatedBy)
			if err != nil {
				return nil, err
			}
			orders = append(orders, o)
		}
		return orders, nil
	})
}
