package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardenSourceAgainstPortalMain(t *testing.T) {
	path := filepath.Join("..", "..", "main.go")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read portal source: %v", err)
	}
	hardened, err := hardenSource(string(payload))
	if err != nil {
		t.Fatalf("harden portal source: %v", err)
	}

	for _, forbidden := range []string{
		"out = append(out, a.accessResponse(item, a.mustDecryptPassword(item)))",
		"row := a.accessResponse(item, a.mustDecryptPassword(item))",
		"rawPassword = a.portalAccessPassword(record)",
		`"role":     "customer"`,
	} {
		if strings.Contains(hardened, forbidden) {
			t.Fatalf("forbidden insecure pattern remains after hardening: %s", forbidden)
		}
	}

	for _, required := range []string{
		"func operationalProvisionRole(role string) string",
		"func (a *portalApp) accessResponseWithoutPassword(record projectAccess)",
		`return "viewer"`,
		`"role":     effectiveAccessRole(record)`,
		"downstreamPassword = strings.TrimSpace(a.mustDecryptPassword(record))",
	} {
		if !strings.Contains(hardened, required) {
			t.Fatalf("required hardened pattern missing: %s", required)
		}
	}
}

func TestHardenSourceIsIdempotent(t *testing.T) {
	path := filepath.Join("..", "..", "main.go")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := hardenSource(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	second, err := hardenSource(first)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("hardening transformation is not idempotent")
	}
}

func TestOperationalRoleMappingInGeneratedSource(t *testing.T) {
	path := filepath.Join("..", "..", "main.go")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hardened, err := hardenSource(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	for _, mapping := range []string{
		`case "owner":
		return "admin"`,
		`case "manager":
		return "manager"`,
		`case "accountant":
		return "accountant"`,
	} {
		if !strings.Contains(hardened, mapping) {
			t.Fatalf("generated source is missing role mapping: %s", mapping)
		}
	}
}
