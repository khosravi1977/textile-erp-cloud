package main

import (
	"testing"
	"time"
)

func TestAccessResponseDoesNotRecoverStoredPassword(t *testing.T) {
	t.Parallel()

	app := &portalApp{
		publicBase:    "https://example.test",
		sessionSecret: "accounting-audit-test-secret",
	}
	enc, err := app.encryptPassword("StoredSecret@123")
	if err != nil {
		t.Fatal(err)
	}
	record := projectAccess{
		ID:                 1,
		ProjectKey:         "textile-erp",
		CompanyName:        "Audit Co",
		Username:           "audit-user",
		FinancialCompanyID: 1,
		AccessRole:         "viewer",
		PasswordHash:       "stored-hash-present",
		PasswordEnc:        enc,
		AllowFinancial:     true,
		IsActive:           true,
		ExpiresAt:          time.Now().Add(24 * time.Hour),
		CreatedAt:          time.Now(),
		AccessToken:        "audit-token",
	}

	response := app.accessResponse(record, "")
	if got, _ := response["password"].(string); got != "" {
		t.Fatalf("stored password must never be recovered into a metadata response, got %q", got)
	}
}

func TestAccessResponseMayReturnExplicitOneTimePassword(t *testing.T) {
	t.Parallel()

	app := &portalApp{publicBase: "https://example.test"}
	record := projectAccess{
		ID:                 2,
		ProjectKey:         "textile-erp",
		CompanyName:        "Audit Co",
		Username:           "new-user",
		FinancialCompanyID: 1,
		AccessRole:         "viewer",
		PasswordHash:       "stored-hash-present",
		AllowFinancial:     true,
		IsActive:           true,
		ExpiresAt:          time.Now().Add(24 * time.Hour),
		CreatedAt:          time.Now(),
		AccessToken:        "new-user-token",
	}

	const oneTimePassword = "OneTime@123"
	response := app.accessResponse(record, oneTimePassword)
	if got, _ := response["password"].(string); got != oneTimePassword {
		t.Fatalf("explicit newly-issued password should be returned once, got %q", got)
	}
}
