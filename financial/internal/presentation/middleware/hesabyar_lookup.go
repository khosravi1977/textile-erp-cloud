package middleware

import (
	"net/http"
	"os"
	"strings"
	"time"
)

const HesabyarLookupRole = "hesabyar_lookup"
const HesabyarPartiesPath = "/api/v1/hesabyar/parties"
const HesabyarStockPath = "/api/v1/hesabyar/stock"
const HesabyarLedgerPath = "/api/v1/hesabyar/ledger"

func isHesabyarPrivatePath(path string) bool {
	return path == HesabyarPartiesPath || path == HesabyarStockPath || path == HesabyarLedgerPath
}

func allowHesabyarLookup(r *http.Request, id resolvedIdentity) bool {
	// Header-based identities cannot assume this role. A configured signing key
	// is required because the legacy JWT verifier has a development fallback.
	if os.Getenv("HESABYAR_LOOKUP_ENABLED") != "true" ||
		len(strings.TrimSpace(os.Getenv("JWT_SECRET"))) < 32 || id.claims == nil ||
		id.role != HesabyarLookupRole ||
		id.claims["allow_financial"] != true || r.Method != http.MethodGet || r.URL.RawQuery != "" {
		return false
	}
	exp, ok := id.claims["exp"].(float64)
	issued, hasIssued := id.claims["iat"].(float64)
	now := float64(time.Now().Unix())
	if !ok || !hasIssued || issued <= 0 || issued > now || exp <= now || exp > issued+15*60 ||
		issued != float64(int64(issued)) || exp != float64(int64(exp)) {
		return false
	}
	// Check the claims against the resolved values to reject fractional IDs.
	company, companyOK := id.claims["company_id"].(float64)
	user, userOK := id.claims["user_id"].(float64)
	if !companyOK || !userOK || company > (1<<53)-1 || user > (1<<53)-1 ||
		company != float64(id.companyID) || user != float64(id.userID) {
		return false
	}
	path := r.URL.Path
	if id.claims["aud"] == "hesabyar-portal" {
		return id.claims["hsbyr_path"] == path && (isHesabyarPrivatePath(path) || path == "/api/operational/kala-items" || path == "/api/operational/yarn-items")
	}
	if id.claims["aud"] != "hesabyar-lookup" {
		return false
	}
	switch path {
	case HesabyarPartiesPath, "/api/operational/kala-items", "/api/operational/yarn-items":
		return true
	default:
		return false
	}
}
