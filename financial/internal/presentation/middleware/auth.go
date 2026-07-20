package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

type rateWindow struct {
	started time.Time
	count   int
}

var financialRateLimiter = struct {
	sync.Mutex
	clients map[string]rateWindow
}{clients: map[string]rateWindow{}}

type resolvedIdentity struct {
	companyID   int64
	userID      int64
	role        string
	permissions []string
	portal      bool
	claims      map[string]any
}

func AdminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/health") ||
			r.URL.Path == "/" ||
			strings.HasPrefix(r.URL.Path, "/metrics") ||
			strings.HasPrefix(r.URL.Path, "/api/auth/login") ||
			r.URL.Path == "/api/mobile/pair" {
			next.ServeHTTP(w, r)
			return
		}

		identity, ok := resolveIdentity(r)
		if !ok {
			writeAuthError(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		if !authorizeRequest(r, identity) {
			writeAuthError(w, http.StatusForbidden, "Access to this module is not allowed")
			return
		}

		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = strconv.FormatInt(time.Now().UnixNano(), 36)
		}

		ctx := WithIdentity(r.Context(), identity.companyID, identity.userID, identity.role, requestID)
		ctx = requestctx.WithAccess(ctx, identity.permissions, identity.portal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/health") || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		limit := 600
		if configured, err := strconv.Atoi(strings.TrimSpace(os.Getenv("RATE_LIMIT_PER_MINUTE"))); err == nil && configured >= 60 {
			limit = configured
		}
		now := time.Now()
		client := requestClientIP(r)
		financialRateLimiter.Lock()
		window := financialRateLimiter.clients[client]
		if window.started.IsZero() || now.Sub(window.started) >= time.Minute {
			window = rateWindow{started: now}
		}
		window.count++
		financialRateLimiter.clients[client] = window
		if len(financialRateLimiter.clients) > 4096 {
			for key, value := range financialRateLimiter.clients {
				if now.Sub(value.started) >= 2*time.Minute {
					delete(financialRateLimiter.clients, key)
				}
			}
		}
		financialRateLimiter.Unlock()
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
		remaining := limit - window.count
		if remaining < 0 {
			remaining = 0
		}
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		if window.count > limit {
			w.Header().Set("Retry-After", "60")
			writeAuthError(w, http.StatusTooManyRequests, "Too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestClientIP(r *http.Request) string {
	if strings.EqualFold(os.Getenv("TRUST_PROXY"), "true") {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
			return forwarded
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func resolveIdentity(r *http.Request) (resolvedIdentity, bool) {
	if r.Header.Get("X-Dev-Mode") == "true" && strings.EqualFold(os.Getenv("ALLOW_DEV_AUTH"), "true") {
		return resolvedIdentity{companyID: parseInt64Header(r, "X-Company-ID", 1), userID: parseInt64Header(r, "X-User-ID", 1), role: headerOr(r, "X-User-Role", "admin")}, true
	}
	if apiKey := strings.TrimSpace(r.Header.Get("X-API-Key")); apiKey != "" && os.Getenv("FINANCIAL_API_KEY") != "" && apiKey == os.Getenv("FINANCIAL_API_KEY") {
		return resolvedIdentity{companyID: parseInt64Header(r, "X-Company-ID", 1), userID: parseInt64Header(r, "X-User-ID", 1), role: headerOr(r, "X-User-Role", "admin")}, true
	}

	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return resolvedIdentity{}, false
	}
	token := strings.TrimSpace(authHeader[len("Bearer "):])
	claims, err := VerifyJWT(token)
	if err != nil {
		return resolvedIdentity{}, false
	}
	companyID := int64FromClaims(claims, "company_id", 0)
	userID := int64FromClaims(claims, "user_id", 0)
	role := stringFromClaims(claims, "role", "")
	if companyID <= 0 || userID <= 0 || strings.TrimSpace(role) == "" {
		return resolvedIdentity{}, false
	}
	portal := strings.EqualFold(stringFromClaims(claims, "project_key", ""), "textile-erp")
	return resolvedIdentity{
		companyID: companyID, userID: userID, role: role, claims: claims,
		portal: portal, permissions: stringSliceFromClaims(claims, "permissions"),
	}, true
}

func authorizeRequest(r *http.Request, identity resolvedIdentity) bool {
	if strings.EqualFold(identity.role, "mobile") {
		return strings.HasPrefix(r.URL.Path, "/api/mobile/") && r.URL.Path != "/api/mobile/pairing"
	}
	if !identity.portal {
		return true
	}
	if allowed, exists := identity.claims["allow_financial"].(bool); exists && !allowed {
		return false
	}
	portalRole := strings.ToLower(stringFromClaims(identity.claims, "portal_role", identity.role))
	if r.Method != http.MethodGet && r.Method != http.MethodHead && portalRole == "viewer" {
		return false
	}
	required := requiredPermissions(r.URL.Path)
	if len(required) == 0 {
		return len(identity.permissions) > 0
	}
	for _, permission := range required {
		if containsPermission(identity.permissions, permission) {
			return true
		}
	}
	return false
}

func requiredPermissions(path string) []string {
	switch {
	case path == "/api/workspace":
		return []string{"dashboard", "financialHealth", "initialData", "incomingInvoices", "yarnOutInvoices", "invoices", "inventory", "costs", "receivableDocs", "payableDocs", "bankCash", "reports", "taxReports", "credit", "advisor"}
	case strings.HasPrefix(path, "/api/workspace/"):
		return []string{"dashboard", "financialHealth", "reports", "taxReports", "credit", "advisor"}
	case strings.Contains(path, "/operational/out-invoices") || strings.HasSuffix(path, "/operational/f_khor"):
		return []string{"operational", "invoices"}
	case strings.Contains(path, "/operational/yarn-incoming") || strings.Contains(path, "/operational/chelle-incoming"):
		return []string{"operational", "incomingInvoices"}
	case strings.Contains(path, "/operational/yarn-outgoing"):
		return []string{"operational", "yarnOutInvoices"}
	case strings.Contains(path, "/operational/expenses"):
		return []string{"operational", "costs"}
	case strings.Contains(path, "/operational/spare-parts-inventory"):
		return []string{"operational", "inventory"}
	case strings.HasPrefix(path, "/api/operational/") || strings.HasPrefix(path, "/api/financial/lookups/") || strings.HasPrefix(path, "/api/financial/operational/"):
		return []string{"operational", "initialData", "incomingInvoices", "invoices", "inventory"}
	case strings.HasPrefix(path, "/api/invoices"):
		return []string{"invoices"}
	case strings.HasPrefix(path, "/api/inventory"):
		return []string{"inventory"}
	case strings.HasPrefix(path, "/api/costs"):
		return []string{"costs"}
	case strings.HasPrefix(path, "/api/advisor"):
		return []string{"advisor", "credit"}
	case strings.HasPrefix(path, "/api/commission"):
		return []string{"invoices"}
	case strings.HasPrefix(path, "/api/settlements"):
		return []string{"invoices", "receivableDocs", "payableDocs", "bankCash"}
	case strings.HasPrefix(path, "/api/production"):
		return []string{"operational"}
	default:
		return nil
	}
}

func stringSliceFromClaims(claims map[string]any, key string) []string {
	values, ok := claims[key].([]any)
	if !ok {
		if typed, ok := claims[key].([]string); ok {
			return append([]string(nil), typed...)
		}
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			out = append(out, strings.TrimSpace(text))
		}
	}
	return out
}

func containsPermission(permissions []string, expected string) bool {
	for _, permission := range permissions {
		if permission == expected {
			return true
		}
	}
	return false
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func int64FromClaims(claims map[string]any, key string, fallback int64) int64 {
	v, ok := claims[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return int64(n)
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func stringFromClaims(claims map[string]any, key, fallback string) string {
	if v, ok := claims[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func parseInt64Header(r *http.Request, key string, fallback int64) int64 {
	if v := strings.TrimSpace(r.Header.Get(key)); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func headerOr(r *http.Request, key, fallback string) string {
	if v := strings.TrimSpace(r.Header.Get(key)); v != "" {
		return v
	}
	return fallback
}
