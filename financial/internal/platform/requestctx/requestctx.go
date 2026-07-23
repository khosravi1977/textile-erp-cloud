package requestctx

import "context"

type contextKey string

const (
	companyIDKey    contextKey = "company_id"
	userIDKey       contextKey = "user_id"
	userRoleKey     contextKey = "user_role"
	requestIDKey    contextKey = "request_id"
	permissionsKey  contextKey = "permissions"
	portalAccessKey contextKey = "portal_access"
)

func WithIdentity(ctx context.Context, companyID, userID int64, role, requestID string) context.Context {
	ctx = context.WithValue(ctx, companyIDKey, companyID)
	ctx = context.WithValue(ctx, userIDKey, userID)
	ctx = context.WithValue(ctx, userRoleKey, role)
	ctx = context.WithValue(ctx, requestIDKey, requestID)
	return ctx
}

func WithAccess(ctx context.Context, permissions []string, portalAccess bool) context.Context {
	copyPermissions := append([]string(nil), permissions...)
	ctx = context.WithValue(ctx, permissionsKey, copyPermissions)
	return context.WithValue(ctx, portalAccessKey, portalAccess)
}

func CompanyID(ctx context.Context) int64 {
	if v, ok := ctx.Value(companyIDKey).(int64); ok && v > 0 {
		return v
	}
	return 1
}

func UserID(ctx context.Context) int64 {
	if v, ok := ctx.Value(userIDKey).(int64); ok && v > 0 {
		return v
	}
	return 1
}

func UserRole(ctx context.Context) string {
	if v, ok := ctx.Value(userRoleKey).(string); ok && v != "" {
		return v
	}
	return "admin"
}

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func Permissions(ctx context.Context) []string {
	if values, ok := ctx.Value(permissionsKey).([]string); ok {
		return append([]string(nil), values...)
	}
	return nil
}

func HasPermission(ctx context.Context, expected string) bool {
	for _, permission := range Permissions(ctx) {
		if permission == expected {
			return true
		}
	}
	return false
}

func IsPortalAccess(ctx context.Context) bool {
	value, _ := ctx.Value(portalAccessKey).(bool)
	return value
}
