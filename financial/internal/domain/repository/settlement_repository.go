package repository

import (
    "context"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
)

type SettlementRepository interface {
    Create(ctx context.Context, settlement *entity.SettlementHeader) error
    GetByID(ctx context.Context, id int64) (*entity.SettlementHeader, error)
    ListByParty(ctx context.Context, partyID int64) ([]*entity.SettlementHeader, error)
}
