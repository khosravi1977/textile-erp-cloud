package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestPostgresOperationalTenantIsolation(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "postgres", dbLabel: "integration", defaultSchema: "public", sessions: map[string]sessionInfo{}}
	if err := application.initializeTenancy(); err != nil {
		t.Fatalf("initialize operational tenancy: %v", err)
	}

	companyOne := int64(71001)
	companyTwo := int64(71002)
	nameOne := fmt.Sprintf("tenant-one-%d", time.Now().UnixNano())
	nameTwo := fmt.Sprintf("tenant-two-%d", time.Now().UnixNano())
	schemaOne := fmt.Sprintf("tenant_test_%d", companyOne)
	schemaTwo := fmt.Sprintf("tenant_test_%d", companyTwo)
	for _, tenant := range []struct {
		company int64
		name    string
		schema  string
	}{{companyOne, nameOne, schemaOne}, {companyTwo, nameTwo, schemaTwo}} {
		if _, err := db.Exec(`CREATE SCHEMA IF NOT EXISTS ` + quoteIdent(tenant.schema)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO public.operational_tenants(external_company_id,company_name,schema_name,active) VALUES($1,$2,$3,1) ON CONFLICT(external_company_id) DO UPDATE SET company_name=EXCLUDED.company_name,schema_name=EXCLUDED.schema_name,active=1`, tenant.company, tenant.name, tenant.schema); err != nil {
			t.Fatal(err)
		}
		if err := application.setSearchPath(tenant.schema); err != nil {
			t.Fatal(err)
		}
		if err := application.migrate(); err != nil {
			t.Fatalf("migrate %s: %v", tenant.schema, err)
		}
	}
	defer func() {
		_ = application.setSearchPath("public")
		_, _ = db.Exec(`DELETE FROM public.operational_tenants WHERE external_company_id IN ($1,$2)`, companyOne, companyTwo)
		_, _ = db.Exec(`DROP SCHEMA IF EXISTS ` + quoteIdent(schemaOne) + ` CASCADE`)
		_, _ = db.Exec(`DROP SCHEMA IF EXISTS ` + quoteIdent(schemaTwo) + ` CASCADE`)
	}()

	if err := application.setSearchPath(schemaOne); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO mosh_name (name_mosh) VALUES (?)`, nameOne); err != nil {
		t.Fatal(err)
	}
	if err := application.setSearchPath(schemaTwo); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO mosh_name (name_mosh) VALUES (?)`, nameTwo); err != nil {
		t.Fatal(err)
	}
	var visibleOne, visibleTwo int
	if err := application.setSearchPath(schemaOne); err != nil {
		t.Fatal(err)
	}
	if err := application.queryRow(`SELECT COUNT(*) FROM mosh_name WHERE name_mosh IN (?,?)`, nameOne, nameTwo).Scan(&visibleOne); err != nil {
		t.Fatal(err)
	}
	if err := application.setSearchPath(schemaTwo); err != nil {
		t.Fatal(err)
	}
	if err := application.queryRow(`SELECT COUNT(*) FROM mosh_name WHERE name_mosh IN (?,?)`, nameOne, nameTwo).Scan(&visibleTwo); err != nil {
		t.Fatal(err)
	}
	if visibleOne != 1 || visibleTwo != 1 {
		t.Fatalf("tenant rows leaked: company1=%d company2=%d", visibleOne, visibleTwo)
	}
	if err := application.setSearchPath(schemaOne); err != nil {
		t.Fatal(err)
	}

	warper := fmt.Sprintf("warper-%d", time.Now().UnixNano())
	beam := fmt.Sprintf("beam-%d", time.Now().UnixNano())
	yarn := fmt.Sprintf("yarn-%d", time.Now().UnixNano())
	customer := fmt.Sprintf("customer-%d", time.Now().UnixNano())
	for _, statement := range []struct {
		query string
		value string
	}{
		{`INSERT INTO chellepich (name_chellepich) VALUES (?)`, warper},
		{`INSERT INTO kod_navard (kod_kod_navard) VALUES (?)`, beam},
		{`INSERT INTO nakh_name (name_nakh_name) VALUES (?)`, yarn},
		{`INSERT INTO mosh_name (name_mosh) VALUES (?)`, customer},
	} {
		if _, err := application.exec(statement.query, statement.value); err != nil {
			t.Fatal(err)
		}
	}
	postJSON := func(handler func(http.ResponseWriter, *http.Request), payload any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler(recorder, req)
		return recorder
	}
	firstExit := postJSON(application.emptyBeamOut, map[string]any{"beam": beam, "warper": warper})
	if firstExit.Code != http.StatusOK {
		t.Fatalf("create empty beam exit: %d %s", firstExit.Code, firstExit.Body.String())
	}
	duplicateExit := postJSON(application.emptyBeamOut, map[string]any{"beam": beam, "warper": warper})
	if duplicateExit.Code != http.StatusConflict {
		t.Fatalf("expected duplicate unresolved beam conflict, got %d: %s", duplicateExit.Code, duplicateExit.Body.String())
	}
	var yarnID, warperID, customerID, beamID int64
	if err := application.queryRow(`SELECT id_nakh_name FROM nakh_name WHERE name_nakh_name=?`, yarn).Scan(&yarnID); err != nil {
		t.Fatal(err)
	}
	if err := application.queryRow(`SELECT id_chellepich FROM chellepich WHERE name_chellepich=?`, warper).Scan(&warperID); err != nil {
		t.Fatal(err)
	}
	if err := application.queryRow(`SELECT id_mosh_name FROM mosh_name WHERE name_mosh=?`, customer).Scan(&customerID); err != nil {
		t.Fatal(err)
	}
	if err := application.queryRow(`SELECT id_kod_navard FROM kod_navard WHERE kod_kod_navard=?`, beam).Scan(&beamID); err != nil {
		t.Fatal(err)
	}
	chelleResponse := postJSON(application.chelle, map[string]any{
		"shom_chelle": "CH-TEST", "nakh_id": yarnID, "weight": 100, "pich_id": warperID,
		"mosh_id": customerID, "hambaft": "HB-TEST", "kod_navard_id": beamID,
	})
	if chelleResponse.Code != http.StatusOK {
		t.Fatalf("create chelle: %d %s", chelleResponse.Code, chelleResponse.Body.String())
	}
	var returnedAt, returnedChelle string
	if err := application.queryRow(`SELECT COALESCE(returned_at,''), COALESCE(returned_chelle_no,'') FROM empty_beam_out WHERE kod_navard=? ORDER BY id_empty_beam_out DESC LIMIT 1`, beam).Scan(&returnedAt, &returnedChelle); err != nil {
		t.Fatal(err)
	}
	if returnedAt == "" || returnedChelle != "CH-TEST" {
		t.Fatalf("empty beam return was not linked: returned_at=%q chelle=%q", returnedAt, returnedChelle)
	}
}
