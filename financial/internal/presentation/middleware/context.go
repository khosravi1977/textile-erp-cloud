package middleware

import (
	"context"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

func WithIdentity(ctx context.Context, companyID, userID int64, role, requestID string) context.Context {
	return requestctx.WithIdentity(ctx, companyID, userID, role, requestID)
}

func CompanyIDFromContext(ctx context.Context) int64 {
	return requestctx.CompanyID(ctx)
}

func UserIDFromContext(ctx context.Context) int64 {
	return requestctx.UserID(ctx)
}

func UserRoleFromContext(ctx context.Context) string {
	return requestctx.UserRole(ctx)
}

func RequestIDFromContext(ctx context.Context) string {
	return requestctx.RequestID(ctx)
}
