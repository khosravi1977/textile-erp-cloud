package repository

import (
    "context"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
)

type ProductionRepository interface {
    Create(ctx context.Context, order *entity.ProductionOrder) error
    GetByID(ctx context.Context, id int64) (*entity.ProductionOrder, error)
    UpdateStatus(ctx context.Context, id int64, status entity.ProductionStatus) error
    ListByCustomer(ctx context.Context, customerID int64) ([]*entity.ProductionOrder, error)
}
