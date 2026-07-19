package repository

import (
    "context"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
)

type PartyRepository interface {
    Create(ctx context.Context, party *entity.Party) error
    GetByID(ctx context.Context, id int64) (*entity.Party, error)
    Update(ctx context.Context, party *entity.Party) error
    Delete(ctx context.Context, id int64) error
    List(ctx context.Context, partyType entity.PartyType) ([]*entity.Party, error)
}
