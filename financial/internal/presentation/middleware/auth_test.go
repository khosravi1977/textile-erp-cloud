package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMobilePairingRequiresExplicitPermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/pairing", nil)
	identity := resolvedIdentity{companyID: 2, userID: 10, role: "accountant", portal: true, permissions: []string{"reports"}, claims: map[string]any{"allow_financial": true, "portal_role": "accountant"}}
	if authorizeRequest(req, identity) {
		t.Fatal("reports-only user must not create a mobile pairing")
	}
	identity.permissions = append(identity.permissions, "mobileApp")
	if !authorizeRequest(req, identity) {
		t.Fatal("user with mobileApp permission should create a mobile pairing")
	}
}
