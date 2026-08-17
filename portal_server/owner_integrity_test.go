package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRepairTenantOwnerIntegrityRestoresMissingOwner(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []projectAccess{
		{
			ID: 1, ProjectKey: "textile-erp", CompanyName: "Paregol", FinancialCompanyID: 7,
			Username: "company-admin", PasswordHash: "hash", AccessRole: "viewer",
			Permissions: []string{"dashboard", "financialHealth", "reports"},
			CanManageTeam: false, AllowFinancial: true, IsActive: true, CreatedAt: created,
		},
		{
			ID: 2, ProjectKey: "textile-erp", CompanyName: "Paregol", FinancialCompanyID: 7,
			Username: "employee", PasswordHash: "hash", AccessRole: "accountant",
			AllowFinancial: true, IsActive: true, CreatedAt: created.Add(time.Hour),
		},
	}

	got, repaired := repairTenantOwnerIntegrity(items)
	if repaired != 1 {
		t.Fatalf("expected one repaired tenant, got %d", repaired)
	}
	if got[0].AccessRole != "owner" {
		t.Fatalf("oldest tenant account should be restored as owner, got %q", got[0].AccessRole)
	}
	if !got[0].CanManageTeam {
		t.Fatal("restored owner must be able to manage team")
	}
	if len(got[0].Permissions) != len(financialPermissionCatalog) {
		t.Fatalf("restored owner must receive complete financial page catalog: %#v", got[0].Permissions)
	}
	if got[1].AccessRole != "accountant" {
		t.Fatalf("employee role must remain untouched, got %q", got[1].AccessRole)
	}
}

func TestRepairTenantOwnerIntegrityDoesNotTouchExistingOwner(t *testing.T) {
	t.Parallel()
	items := []projectAccess{{
		ID: 11, ProjectKey: "textile-erp", CompanyName: "Acme", FinancialCompanyID: 3,
		Username: "owner", PasswordHash: "hash", AccessRole: "owner",
		CanManageTeam: false, AllowFinancial: true, IsActive: true,
	}}
	got, repaired := repairTenantOwnerIntegrity(items)
	if repaired != 0 {
		t.Fatalf("existing owner tenant must not be rewritten, repaired=%d", repaired)
	}
	if got[0].CanManageTeam {
		t.Fatal("repair must not rewrite existing explicit owner fields; effectiveCanManageTeam already grants owner access")
	}
}

func TestRepairTenantOwnerIntegrityKeepsPurchasedModuleFlags(t *testing.T) {
	t.Parallel()
	items := []projectAccess{{
		ID: 21, ProjectKey: "textile-erp", CompanyName: "Acme", FinancialCompanyID: 9,
		Username: "admin", PasswordHash: "hash", AccessRole: "viewer",
		AllowFinancial: true, AllowOperational: false, AllowWeaving: true,
		IsActive: true,
	}}
	got, repaired := repairTenantOwnerIntegrity(items)
	if repaired != 1 {
		t.Fatalf("expected repair, got %d", repaired)
	}
	if !got[0].AllowFinancial || got[0].AllowOperational || !got[0].AllowWeaving {
		t.Fatalf("module purchase flags must not change: %#v", got[0])
	}
}

func TestRepairTenantOwnerIntegrityFilePersistsRepair(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "portal-access.db")
	items := []projectAccess{{
		ID: 31, ProjectKey: "textile-erp", CompanyName: "Persisted", FinancialCompanyID: 12,
		Username: "admin", PasswordHash: "hash", AccessRole: "viewer", AllowFinancial: true,
		IsActive: true, CreatedAt: time.Now().Add(-time.Hour),
	}}
	payload, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	repaired, err := repairTenantOwnerIntegrityFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("expected one repair, got %d", repaired)
	}
	stored, err := readAccesses(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].AccessRole != "owner" || !stored[0].CanManageTeam {
		t.Fatalf("repair was not persisted: %#v", stored)
	}
}
