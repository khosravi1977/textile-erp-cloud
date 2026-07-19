package postgres

import (
	"context"
	"database/sql"

	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/repository"
	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type CreditRepo struct {
	db *sql.DB
}

func NewCreditRepository(db *sql.DB) repository.CreditRepository {
	return &CreditRepo{db: db}
}

func (r *CreditRepo) GetProfile(ctx context.Context, partyID int64) (*entity.CustomerCreditProfile, error) {
	return withTenantConn(ctx, r.db, func(q tenantQueryable) (*entity.CustomerCreditProfile, error) {
		query := `SELECT id, party_id, credit_limit, credit_days, std_wastage_rate, 
              wastage_responsibility, downtime_rate, base_score, risk_group, is_blocked, block_reason
              FROM customer_credit_profiles WHERE party_id=$1 AND company_id=$2`

		p := &entity.CustomerCreditProfile{}
		var limitRials, downtimeRials int64
		var blockReason sql.NullString

		err := q.QueryRowContext(ctx, query, partyID, requestctx.CompanyID(ctx)).Scan(
			&p.ID, &p.PartyID, &limitRials, &p.CreditDays, &p.StdWastageRate,
			&p.WastageResponsibility, &downtimeRials, &p.BaseScore, &p.RiskGroup,
			&p.IsBlocked, &blockReason,
		)

		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		p.CreditLimit = valueobject.FromRials(limitRials)
		p.DowntimeRate = valueobject.FromRials(downtimeRials)
		if blockReason.Valid {
			p.BlockReason = blockReason.String
		}

		return p, nil
	})
}

func (r *CreditRepo) UpdateProfile(ctx context.Context, profile *entity.CustomerCreditProfile) error {
	_, err := withTenantConn(ctx, r.db, func(q tenantQueryable) (struct{}, error) {
		_, err := q.ExecContext(ctx,
			`UPDATE customer_credit_profiles SET credit_limit=$1, credit_days=$2, 
         risk_group=$3, is_blocked=$4, block_reason=$5, last_score_update=NOW()
         WHERE id=$6 AND company_id=$7`,
			profile.CreditLimit.ToRials(), profile.CreditDays,
			profile.RiskGroup, profile.IsBlocked, profile.BlockReason, profile.ID, requestctx.CompanyID(ctx),
		)
		return struct{}{}, err
	})
	return err
}
