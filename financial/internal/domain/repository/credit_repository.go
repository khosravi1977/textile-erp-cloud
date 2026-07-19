package repository

import (
    "context"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
)

type CreditRepository interface {
    GetProfile(ctx context.Context, partyID int64) (*entity.CustomerCreditProfile, error)
    UpdateProfile(ctx context.Context, profile *entity.CustomerCreditProfile) error
}
