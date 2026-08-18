from pathlib import Path

main = Path("portal_server/main.go")
source = main.read_text(encoding="utf-8-sig")

replacements = [
    (
        "out = append(out, a.accessResponse(item, a.mustDecryptPassword(item)))",
        "out = append(out, a.accessResponseWithoutPassword(item))",
    ),
    (
        "row := a.accessResponse(item, a.mustDecryptPassword(item))",
        "row := a.accessResponseWithoutPassword(item)",
    ),
    (
        '\toperationalRole := "viewer"\n\tif normalizeAccessRole(role) == "owner" {\n\t\toperationalRole = "admin"\n\t}',
        "\toperationalRole := operationalProvisionRole(role)",
    ),
    (
        '''\trawPassword := ""\n\tif requiresSetup {\n\t\trecord.PasswordHash = ""\n\t\trecord.PasswordEnc = ""\n\t\trecord.MustChangePassword = false\n\t} else {\n\t\trecord.MustChangePassword = false\n\t\trawPassword = a.mustDecryptPassword(record)\n\t\tif strings.TrimSpace(password) != "" {\n\t\t\tpasswordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)\n\t\t\tif err != nil {\n\t\t\t\treturn projectAccess{}, "", err\n\t\t\t}\n\t\t\tpasswordEnc, err := a.encryptPassword(password)\n\t\t\tif err != nil {\n\t\t\t\treturn projectAccess{}, "", err\n\t\t\t}\n\t\t\trecord.PasswordHash = string(passwordHash)\n\t\t\trecord.PasswordEnc = passwordEnc\n\t\t\trawPassword = password\n\t\t\trecord.MustChangePassword = false\n\t\t}\n\t}\n\tdownstreamPassword := strings.TrimSpace(rawPassword)''',
        '''\trawPassword := ""\n\tdownstreamPassword := ""\n\tif requiresSetup {\n\t\trecord.PasswordHash = ""\n\t\trecord.PasswordEnc = ""\n\t\trecord.MustChangePassword = false\n\t} else {\n\t\trecord.MustChangePassword = false\n\t\t// Existing passwords may still be needed internally to synchronize downstream\n\t\t// modules, but must never be returned again by update APIs.\n\t\tdownstreamPassword = strings.TrimSpace(a.mustDecryptPassword(record))\n\t\tif strings.TrimSpace(password) != "" {\n\t\t\tpasswordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)\n\t\t\tif err != nil {\n\t\t\t\treturn projectAccess{}, "", err\n\t\t\t}\n\t\t\tpasswordEnc, err := a.encryptPassword(password)\n\t\t\tif err != nil {\n\t\t\t\treturn projectAccess{}, "", err\n\t\t\t}\n\t\t\trecord.PasswordHash = string(passwordHash)\n\t\t\trecord.PasswordEnc = passwordEnc\n\t\t\trawPassword = password // one-time reveal only when a new password is explicitly set\n\t\t\tdownstreamPassword = password\n\t\t\trecord.MustChangePassword = false\n\t\t}\n\t}''',
    ),
    (
        '''func (a *portalApp) accessResponse(record projectAccess, rawPassword string) map[string]any {\n\tif accessRequiresSetup(record) {\n\t\trawPassword = ""\n\t} else if rawPassword == "" {\n\t\trawPassword = a.portalAccessPassword(record)\n\t}''',
        '''func (a *portalApp) accessResponse(record projectAccess, rawPassword string) map[string]any {\n\t// Passwords are write-only from the API perspective. They may be revealed once\n\t// immediately after creation/reset when the caller explicitly supplies rawPassword,\n\t// but list/read/update responses must never decrypt a stored password.\n\tif accessRequiresSetup(record) {\n\t\trawPassword = ""\n\t}''',
    ),
]

for old, new in replacements:
    if old in source:
        source = source.replace(old, new, 1)
    elif new not in source:
        raise SystemExit("Expected patch anchor was not found:\n" + old[:240])

main.write_text(source, encoding="utf-8")

Path("portal_server/rbac_security.go").write_text(r'''package main

// operationalProvisionRole keeps the portal role model intact when a Textile
// ERP account is provisioned into the operational service. Owner is the only
// role that maps to the operational superuser role.
func operationalProvisionRole(role string) string {
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

// accessResponseWithoutPassword is the safe form for list/read endpoints.
func (a *portalApp) accessResponseWithoutPassword(record projectAccess) map[string]any {
	row := a.accessResponse(record, "")
	delete(row, "password")
	return row
}
''', encoding="utf-8")

Path("portal_server/rbac_security_test.go").write_text(r'''package main

import (
	"testing"
	"time"
)

func TestOperationalProvisionRolePreservesPortalRoles(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"owner": "admin", "manager": "manager", "accountant": "accountant", "viewer": "viewer",
	}
	for input, want := range cases {
		if got := operationalProvisionRole(input); got != want {
			t.Fatalf("operationalProvisionRole(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestAccessResponseDoesNotRecoverStoredPassword(t *testing.T) {
	t.Parallel()
	app := &portalApp{sessionSecret: "test-session-secret-that-is-long-enough", publicBase: "https://example.test"}
	enc, err := app.encryptPassword("NeverReturnThisPassword123!")
	if err != nil { t.Fatal(err) }
	record := projectAccess{
		ID: 1, ProjectKey: "textile-erp", CompanyName: "Test", Username: "owner",
		FinancialCompanyID: 1, AccessRole: "owner", CanManageTeam: true,
		AllowFinancial: true, IsActive: true, AccessToken: "token", PasswordEnc: enc,
		PasswordHash: "hash-present", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(),
	}
	row := app.accessResponse(record, "")
	if got, _ := row["password"].(string); got != "" {
		t.Fatalf("stored password leaked from accessResponse: %q", got)
	}
	safe := app.accessResponseWithoutPassword(record)
	if _, exists := safe["password"]; exists { t.Fatal("password field must be omitted from list/read responses") }
}

func TestAccessResponseAllowsOneTimeExplicitPassword(t *testing.T) {
	t.Parallel()
	app := &portalApp{publicBase: "https://example.test"}
	record := projectAccess{
		ID: 1, ProjectKey: "textile-erp", CompanyName: "Test", Username: "staff",
		FinancialCompanyID: 1, AccessRole: "viewer", AllowFinancial: true,
		IsActive: true, AccessToken: "token", PasswordHash: "hash-present",
		ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(),
	}
	row := app.accessResponse(record, "OneTimePassword123!")
	if got, _ := row["password"].(string); got != "OneTimePassword123!" {
		t.Fatalf("explicit one-time password was not returned, got %q", got)
	}
}
''', encoding="utf-8")
