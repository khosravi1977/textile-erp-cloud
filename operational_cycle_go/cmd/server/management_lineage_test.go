package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestManagementLineageReturnsRealSourceLabelsAndIsReadOnly(t *testing.T) {
	db, err := sql.Open("sqlite", "file:management-lineage-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	application := &app{db: db, dialect: "sqlite", dbLabel: "test", sessions: map[string]sessionInfo{}}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO chelle(id_chelle,tarikh_chelle,shom_chelle,nakh_chelle,w_chelle,pich_chelle,mosh_chelle,hambaft_chelle,codnavard_chelle,machin_chelle) VALUES(1,'1405/05/24','چله واقعی','نخ واقعی',120,'چله‌پیچ واقعی','مشتری واقعی','همبافت واقعی','N-1','M-1')`,
		`INSERT INTO salon(id_salon,tarikh_salon,metr_salon,w_salon,machin_salon,user_salon,kala_salon,ham_pod_salon,ham_chelle_salon,shom_chelle_salon) VALUES(101,'1405/05/24',48,17,'M-1','اپراتور واقعی','محصول واقعی','پود واقعی','تار واقعی','چله واقعی')`,
		`INSERT INTO f_khor(tarikh_f_khor,shom_f_khor,taghe_cod_f_khor,mosh_f_khor,shomare_sanad,kala_name_f_khor) VALUES('1405/05/24','OUT-1','101','مشتری واقعی','S-1','محصول واقعی')`,
	}
	for _, statement := range statements {
		if _, err := application.exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/management-lineage", nil)
	response := httptest.NewRecorder()
	application.managementLineage(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("lineage status = %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		DetailedInvoices     []record `json:"detailedInvoices"`
		Warps                []record `json:"warps"`
		ProductionUnits      []record `json:"productionUnits"`
		Orders               []record `json:"orders"`
		CommitmentsSupported bool     `json:"commitmentsSupported"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.DetailedInvoices) != 1 || len(result.Warps) != 1 || len(result.ProductionUnits) != 1 || len(result.Orders) != 0 || !result.CommitmentsSupported {
		t.Fatalf("unexpected lineage coverage: %#v", result)
	}
	for _, name := range []string{"مشتری واقعی", "محصول واقعی", "چله واقعی", "نخ واقعی", "چله‌پیچ واقعی"} {
		if !strings.Contains(response.Body.String(), name) {
			t.Fatalf("real source label %q is missing: %s", name, response.Body.String())
		}
	}

	denied := httptest.NewRecorder()
	application.managementLineage(denied, httptest.NewRequest(http.MethodPost, "/api/management-lineage", bytes.NewBufferString(`{"unsafe":true}`)))
	if denied.Code != http.StatusMethodNotAllowed || denied.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("write method was not rejected: %d", denied.Code)
	}
	var invoiceCount int
	if err := application.queryRow(`SELECT COUNT(*) FROM f_khor`).Scan(&invoiceCount); err != nil || invoiceCount != 1 {
		t.Fatalf("write attempt changed source data: count=%d err=%v", invoiceCount, err)
	}
}

func TestManagementLineageDoesNotReportCompleteWhenAReadFails(t *testing.T) {
	db, err := sql.Open("sqlite", "file:management-lineage-failure-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test", sessions: map[string]sessionInfo{}}
	response := httptest.NewRecorder()
	application.managementLineage(response, httptest.NewRequest(http.MethodGet, "/api/management-lineage", nil))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "commitmentsSupported") {
		t.Fatalf("failed source read was reported as complete: %d %s", response.Code, response.Body.String())
	}
}
