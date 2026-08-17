package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// tenantOwnerKey returns the same practical tenant boundary used by the portal:
// prefer the financial company id when it exists, otherwise fall back to the
// normalized company name. This lets legacy records participate in the repair.
func tenantOwnerKey(record projectAccess) string {
	if record.ProjectKey != "textile-erp" {
		return ""
	}
	if record.FinancialCompanyID > 0 {
		return fmt.Sprintf("company-id:%d", record.FinancialCompanyID)
	}
	name := strings.ToLower(strings.TrimSpace(record.CompanyName))
	if name == "" {
		return ""
	}
	return "company-name:" + name
}

// repairTenantOwnerIntegrity restores the portal invariant that every Textile
// ERP tenant has an owner account. Team-created users cannot be owners, so when
// a tenant has lost its owner role (for example through a legacy migration or
// an accidental admin update), the oldest account in that tenant is the safest
// canonical owner to restore.
//
// The function is deliberately conservative:
//   - it never changes a tenant that already has an owner;
//   - it never activates/deactivates an account;
//   - it never changes purchased module flags;
//   - it only repairs role, full financial page permissions, and team management.
func repairTenantOwnerIntegrity(items []projectAccess) ([]projectAccess, int) {
	groups := make(map[string][]int)
	for i := range items {
		key := tenantOwnerKey(items[i])
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], i)
	}

	repaired := 0
	for _, indexes := range groups {
		hasOwner := false
		for _, idx := range indexes {
			if normalizeAccessRole(items[idx].AccessRole) == "owner" {
				hasOwner = true
				break
			}
		}
		if hasOwner {
			continue
		}

		// Prefer usable records. If all records are inactive/setup-only we do not
		// silently grant authority to an unusable account.
		candidates := make([]int, 0, len(indexes))
		for _, idx := range indexes {
			if items[idx].IsActive && !accessRequiresSetup(items[idx]) {
				candidates = append(candidates, idx)
			}
		}
		if len(candidates) == 0 {
			continue
		}

		sort.SliceStable(candidates, func(i, j int) bool {
			a := items[candidates[i]]
			b := items[candidates[j]]
			if a.CreatedAt.Equal(b.CreatedAt) {
				return a.ID < b.ID
			}
		if a.CreatedAt.IsZero() {
			return true
		}
		if b.CreatedAt.IsZero() {
			return false
		}
		return a.CreatedAt.Before(b.CreatedAt)
		})

		idx := candidates[0]
		items[idx].AccessRole = "owner"
		items[idx].CanManageTeam = true
		items[idx].Permissions = append([]string(nil), financialPermissionCatalog...)
		repaired++
	}
	return items, repaired
}

func repairTenantOwnerIntegrityFile(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var items []projectAccess
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, err
	}

	items, repaired := repairTenantOwnerIntegrity(items)
	if repaired == 0 {
		return 0, nil
	}

	payload, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return 0, err
	}
	payload = append(payload, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".portal-access-owner-repair-*")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return 0, err
	}
	return repaired, nil
}

// Existing installations can contain records written before owner/team roles
// were enforced. Run the integrity repair before main starts. New installations
// are unaffected because their access store does not exist yet.
func init() {
	path := strings.TrimSpace(os.Getenv("ACCESS_DB_PATH"))
	if path == "" {
		path = "/data/portal-access.db"
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	repaired, err := repairTenantOwnerIntegrityFile(path)
	if err != nil {
		log.Printf("tenant owner integrity repair skipped: %v", err)
		return
	}
	if repaired > 0 {
		log.Printf("tenant owner integrity restored for %d tenant(s)", repaired)
	}
}
