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

func TestSupervisorReadDoesNotGrantInvoiceWrite(t *testing.T) {
	identity := resolvedIdentity{companyID: 2, userID: 10, role: "accountant", portal: true, permissions: []string{"financialSupervisor"}, claims: map[string]any{"allow_financial": true, "portal_role": "accountant"}}
	if !authorizeRequest(httptest.NewRequest(http.MethodGet, "/api/supervisor/report", nil), identity) {
		t.Fatal("supervisor permission cannot read report")
	}
	for _, path := range []string{"/api/supervisor/preview", "/api/supervisor/commit"} {
		if authorizeRequest(httptest.NewRequest(http.MethodPost, path, nil), identity) {
			t.Fatal("read-only supervisor permission authorized write")
		}
	}
	identity.permissions = []string{"incomingInvoices"}
	if !authorizeRequest(httptest.NewRequest(http.MethodPost, "/api/supervisor/commit", nil), identity) {
		t.Fatal("invoice writer cannot approve invoice")
	}
}
