package repository

import (
    "context"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
)

type InventoryRepository interface {
    GetLot(ctx context.Context, lotID int64) (*entity.InventoryLot, error)
    UpdateLot(ctx context.Context, lot *entity.InventoryLot) error
}
