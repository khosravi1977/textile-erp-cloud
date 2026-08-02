package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionImagePackagesLegacySync(t *testing.T) {
	dockerfilePath := filepath.Join("..", "..", "Dockerfile")
	payload, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("read production Dockerfile: %v", err)
	}
	dockerfile := string(payload)
	for _, required := range []string{
		"go build -o /out/legacysync ./cmd/legacysync",
		"COPY --from=builder /out/legacysync /app/legacysync",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Fatalf("production Dockerfile does not package legacy sync tool: missing %q", required)
		}
	}
}

func TestValidPostgresIdentifier(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"public", "tenant_textile_100003", "_tenant"} {
		if !validPostgresIdentifier(value) {
			t.Fatalf("expected valid schema identifier %q", value)
		}
	}
	for _, value := range []string{"", "100003", "tenant-name", "tenant;DROP SCHEMA public", `tenant"name`} {
		if validPostgresIdentifier(value) {
			t.Fatalf("expected invalid schema identifier %q", value)
		}
	}
}

func TestSyncConfigurationPreservesCentralIdentity(t *testing.T) {
	t.Parallel()

	foundUsers := false
	foundAccess := false
	foundMotorLog := false
	for _, config := range syncConfigs() {
		switch config.TargetTable {
		case "users":
			foundUsers = true
			if !config.PreserveTarget {
				t.Fatal("central users must never be overwritten by a legacy database")
			}
		case "user_menu_access":
			foundAccess = true
			if !config.PreserveTarget {
				t.Fatal("central menu access must never be overwritten by a legacy database")
			}
		case "v_kh_moto":
			foundMotorLog = true
		}
	}
	if !foundUsers || !foundAccess || !foundMotorLog {
		t.Fatalf("incomplete safe sync configuration: users=%v access=%v motor_log=%v", foundUsers, foundAccess, foundMotorLog)
	}
}
