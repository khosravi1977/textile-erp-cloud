package postgres

import (
	"context"
	"database/sql"

	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/repository"
	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type InventoryRepo struct {
	db *sql.DB
}

func NewInventoryRepository(db *sql.DB) repository.InventoryRepository {
	return &InventoryRepo{db: db}
}

func (r *InventoryRepo) GetLot(ctx context.Context, lotID int64) (*entity.InventoryLot, error) {
	return withTenantConn(ctx, r.db, func(q tenantQueryable) (*entity.InventoryLot, error) {
		query := `SELECT id, item_id, warehouse_id, owner_party_id, lot_no, qty_on_hand, 
              qty_reserved, unit_cost, agreed_price, ownership_type, status
              FROM inventory_lots WHERE id=$1 AND company_id=$2`

		lot := &entity.InventoryLot{}
		var ownerPartyID sql.NullInt64
		var agreedPriceRials sql.NullInt64
		var unitCostRials int64

		err := q.QueryRowContext(ctx, query, lotID, requestctx.CompanyID(ctx)).Scan(
			&lot.ID, &lot.ItemID, &lot.WarehouseID, &ownerPartyID,
			&lot.LotNo, &lot.QtyOnHand, &lot.QtyReserved, &unitCostRials,
			&agreedPriceRials, &lot.OwnershipType, &lot.Status,
		)

		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		lot.UnitCost = valueobject.FromRials(unitCostRials)
		if ownerPartyID.Valid {
			v := ownerPartyID.Int64
			lot.OwnerPartyID = &v
		}
		if agreedPriceRials.Valid {
			v := valueobject.FromRials(agreedPriceRials.Int64)
			lot.AgreedPrice = &v
		}

		return lot, nil
	})
}

func (r *InventoryRepo) UpdateLot(ctx context.Context, lot *entity.InventoryLot) error {
	_, err := withTenantConn(ctx, r.db, func(q tenantQueryable) (struct{}, error) {
		_, err := q.ExecContext(ctx,
			"UPDATE inventory_lots SET qty_on_hand=$1, qty_reserved=$2, status=$3, version_number=version_number+1 WHERE id=$4 AND company_id=$5",
			lot.QtyOnHand, lot.QtyReserved, lot.Status, lot.ID, requestctx.CompanyID(ctx),
		)
		return struct{}{}, err
	})
	return err
}
