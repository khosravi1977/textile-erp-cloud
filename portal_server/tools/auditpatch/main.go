package main

import (
	"fmt"
	"os"

	"textile_erp_portal/internal/auditpatch"
)

func main() {
	const path = "main.go"
	raw, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	hardened, err := auditpatch.Transform(raw)
	if err != nil {
		panic(err)
	}
	if !auditpatch.IsHardened(hardened) {
		panic("portal credential hardening verification failed")
	}
	if err := os.WriteFile(path, hardened, 0o600); err != nil {
		panic(err)
	}
	fmt.Println("portal credential hardening applied")
}
