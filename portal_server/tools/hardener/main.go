package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	path := flag.String("file", "main.go", "portal source file to harden")
	check := flag.Bool("check", false, "validate that hardening can be applied without writing")
	flag.Parse()

	payload, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	hardened, err := hardenSource(string(payload))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		if hardened == string(payload) {
			fmt.Println("portal source is already hardened")
		} else {
			fmt.Println("portal source hardening validated")
		}
		return
	}
	if hardened == string(payload) {
		return
	}
	if err := os.WriteFile(*path, []byte(hardened), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func hardenSource(source string) (string, error) {
	var err error
	replace := func(old, next, label string) {
		if err != nil {
			return
		}
		if strings.Contains(source, next) {
			return
		}
		if !strings.Contains(source, old) {
			err = fmt.Errorf("portal hardening anchor not found: %s", label)
			return
		}
		source = strings.Replace(source, old, next, 1)
	}

	replace(`func normalizeAccessRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "owner", "customer":
		return "owner"
	case "manager":
		return "manager"
	case "accountant":
		return "accountant"
	case "viewer":
		return "viewer"
	default:
		return "owner"
	}
}`,
		`func normalizeAccessRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "owner", "customer":
		return "owner"
	case "manager":
		return "manager"
	case "accountant":
		return "accountant"
	case "viewer":
		return "viewer"
	default:
		// Unknown non-empty roles must fail closed instead of escalating to owner.
		return "viewer"
	}
}`,
		"unknown role fail-closed")

	replace(`out = append(out, a.accessResponse(item, a.mustDecryptPassword(item)))`,
		`out = append(out, a.accessResponseWithoutPassword(item))`,
		"admin access list password redaction")

	replace(`row := a.accessResponse(item, a.mustDecryptPassword(item))`,
		`row := a.accessResponseWithoutPassword(item)`,
		"team access list password redaction")

	replace(`	operationalRole := "viewer"
	if normalizeAccessRole(role) == "owner" {
		operationalRole = "admin"
	}`,
		`	operationalRole := operationalProvisionRole(role)`,
		"operational role propagation")

	replace(`	rawPassword := a.mustDecryptPassword(record)
	if strings.TrimSpace(password) != "" {`,
		`	rawPassword := ""
	if strings.TrimSpace(password) != "" {`,
		"legacy update one-time password")

	replace(`	rawPassword := ""
	if requiresSetup {
		record.PasswordHash = ""
		record.PasswordEnc = ""
		record.MustChangePassword = false
	} else {
		record.MustChangePassword = false
		rawPassword = a.mustDecryptPassword(record)
		if strings.TrimSpace(password) != "" {
			passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return projectAccess{}, "", err
			}
			passwordEnc, err := a.encryptPassword(password)
			if err != nil {
				return projectAccess{}, "", err
			}
			record.PasswordHash = string(passwordHash)
			record.PasswordEnc = passwordEnc
			rawPassword = password
			record.MustChangePassword = false
		}
	}
	downstreamPassword := strings.TrimSpace(rawPassword)`,
		`	rawPassword := ""
	downstreamPassword := ""
	if requiresSetup {
		record.PasswordHash = ""
		record.PasswordEnc = ""
		record.MustChangePassword = false
	} else {
		record.MustChangePassword = false
		// Existing credentials can still be used internally to synchronize
		// downstream modules, but must never be returned by read/update APIs.
		downstreamPassword = strings.TrimSpace(a.mustDecryptPassword(record))
		if strings.TrimSpace(password) != "" {
			passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return projectAccess{}, "", err
			}
			passwordEnc, err := a.encryptPassword(password)
			if err != nil {
				return projectAccess{}, "", err
			}
			record.PasswordHash = string(passwordHash)
			record.PasswordEnc = passwordEnc
			rawPassword = password
			downstreamPassword = password
			record.MustChangePassword = false
		}
	}`,
		"managed update password separation")

	replace(`func (a *portalApp) accessResponse(record projectAccess, rawPassword string) map[string]any {
	if accessRequiresSetup(record) {
		rawPassword = ""
	} else if rawPassword == "" {
		rawPassword = a.portalAccessPassword(record)
	}`,
		`func (a *portalApp) accessResponse(record projectAccess, rawPassword string) map[string]any {
	// Passwords are write-only from the API perspective. A newly created or
	// explicitly reset password may be returned once via rawPassword; an
	// existing stored credential is never decrypted for a response.
	if accessRequiresSetup(record) {
		rawPassword = ""
	}`,
		"access response password fallback")

	replace(`		"user": map[string]any{
			"id":       record.ID,
			"username": record.Username,
			"role":     "customer",
			"company":  record.CompanyName,
		},`,
		`		"user": map[string]any{
			"id":       record.ID,
			"username": record.Username,
			"role":     effectiveAccessRole(record),
			"company":  record.CompanyName,
		},`,
		"operational session role response")

	if err != nil {
		return "", err
	}

	if !strings.Contains(source, "func operationalProvisionRole(role string) string") {
		anchor := `func (a *portalApp) accessResponse(record projectAccess, rawPassword string) map[string]any {`
		if !strings.Contains(source, anchor) {
			return "", fmt.Errorf("portal hardening anchor not found: helper insertion")
		}
		helpers := `func operationalProvisionRole(role string) string {
	switch normalizeAccessRole(role) {
	case "owner":
		return "admin"
	case "manager":
		return "manager"
	case "accountant":
		return "accountant"
	default:
		return "viewer"
	}
}

func (a *portalApp) accessResponseWithoutPassword(record projectAccess) map[string]any {
	row := a.accessResponse(record, "")
	delete(row, "password")
	return row
}

`
		source = strings.Replace(source, anchor, helpers+anchor, 1)
	}

	// A list page should never encourage copying an already-stored password.
	source = strings.Replace(source, `password=r.password||'ثبت شده و مخفی'`, `password=r.password||'ثبت شده و غیرقابل نمایش'`, 1)

	return source, nil
}
