package main

import (
	"bytes"
	"os"
	"testing"

	"textile_erp_portal/internal/auditpatch"
)

func TestProductionBuildHardeningRemovesPasswordExposure(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	hardened, err := auditpatch.Transform(raw)
	if err != nil {
		t.Fatalf("credential hardening transform cannot be applied to portal source: %v", err)
	}
	if !auditpatch.IsHardened(hardened) {
		t.Fatal("credential hardening did not produce a safe portal source")
	}
	for _, forbidden := range [][]byte{
		[]byte("rawPassword = a.portalAccessPassword(record)"),
		[]byte("form.password.value = row.password || ''"),
	} {
		if bytes.Contains(hardened, forbidden) {
			t.Fatalf("hardened portal still contains password exposure pattern %q", string(forbidden))
		}
	}
	if !bytes.Contains(hardened, []byte("رمز پس از ذخیره قابل مشاهده نیست")) {
		t.Fatal("team UI must explain that saved passwords are not recoverable")
	}
}

func TestCredentialHardeningIsIdempotent(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	once, err := auditpatch.Transform(raw)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := auditpatch.Transform(once)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatal("credential hardening must be idempotent")
	}
}
