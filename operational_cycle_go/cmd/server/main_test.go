package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMobileLoadingSessionTransfersValidatedTaghe(t *testing.T) {
	db, err := sql.Open("sqlite", "file:mobile-loading-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test", sessions: map[string]sessionInfo{}}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO salon (id_salon,metr_salon,w_salon,machin_salon,kala_salon,ham_pod_salon,ham_chelle_salon,shom_chelle_salon) VALUES (101,25.5,7.2,'1','پارچه تست','P','T','C-1')`); err != nil {
		t.Fatal(err)
	}
	sessionRequest := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	sessionResponse := httptest.NewRecorder()
	if err := application.createSession(sessionResponse, sessionRequest, sessionInfo{UserID: 1, CompanyID: 1, Username: "admin", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	cookies := sessionResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("operational session cookie was not created")
	}
	authCookie := cookies[0]
	createRequest := httptest.NewRequest(http.MethodPost, "/api/out-invoice/mobile-sessions", bytes.NewBufferString(`{"invoice_no":"INV-1","customer":"مشتری","kala":"پارچه تست"}`))
	createRequest.AddCookie(authCookie)
	createResponse := httptest.NewRecorder()
	application.createLoadingSession(createResponse, createRequest)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("create session failed: %d %s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil || created.Token == "" {
		t.Fatalf("invalid session response: %v %s", err, createResponse.Body.String())
	}
	previewRequest := httptest.NewRequest(http.MethodPost, "/api/loading/"+created.Token+"/scan", bytes.NewBufferString(`{"code":"101"}`))
	previewRequest.AddCookie(authCookie)
	previewResponse := httptest.NewRecorder()
	application.loadingMobile(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview taghe failed: %d %s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview struct {
		Item record `json:"item"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil || preview.Item["id"] == nil {
		t.Fatalf("invalid preview response: %v %s", err, previewResponse.Body.String())
	}
	beforeConfirmRequest := httptest.NewRequest(http.MethodGet, "/api/loading/"+created.Token, nil)
	beforeConfirmRequest.AddCookie(authCookie)
	beforeConfirmResponse := httptest.NewRecorder()
	application.loadingMobile(beforeConfirmResponse, beforeConfirmRequest)
	var beforeConfirm struct {
		Totals struct {
			Count int `json:"count"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(beforeConfirmResponse.Body.Bytes(), &beforeConfirm); err != nil || beforeConfirm.Totals.Count != 0 {
		t.Fatalf("preview registered taghe before confirmation: %v %s", err, beforeConfirmResponse.Body.String())
	}
	addRequest := httptest.NewRequest(http.MethodPost, "/api/loading/"+created.Token+"/confirm", bytes.NewBufferString(`{"code":"101"}`))
	addRequest.AddCookie(authCookie)
	addResponse := httptest.NewRecorder()
	application.loadingMobile(addResponse, addRequest)
	if addResponse.Code != http.StatusOK {
		t.Fatalf("add taghe failed: %d %s", addResponse.Code, addResponse.Body.String())
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/loading/"+created.Token, nil)
	statusRequest.AddCookie(authCookie)
	statusResponse := httptest.NewRecorder()
	application.loadingMobile(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("read session failed: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	var status struct {
		Totals struct {
			Count int `json:"count"`
		} `json:"totals"`
		Items []record `json:"items"`
	}
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil || status.Totals.Count != 1 || len(status.Items) != 1 {
		t.Fatalf("unexpected mobile session payload: %v %s", err, statusResponse.Body.String())
	}
}

func TestOutInvoiceSaveValidatesAndPersists(t *testing.T) {
	db, err := sql.Open("sqlite", "file:out-invoice-save-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO salon (id_salon,metr_salon,w_salon,machin_salon,kala_salon) VALUES (101,25.5,7.2,'1','پارچه تست')`); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/out-invoice", bytes.NewBufferString(`{
		"invoice_no":" INV-100 ","sanad_no":" 000123 ","customer":" مشتری تست ","kala":" پارچه تست ","items":["101","101"]
	}`))
	response := httptest.NewRecorder()
	application.outInvoice(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save invoice failed: %d %s", response.Code, response.Body.String())
	}
	var saved struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || !saved.Success {
		t.Fatalf("unexpected save response: %v %s", err, response.Body.String())
	}
	var count int
	var customer, sanad string
	if err := application.queryRow(`SELECT COUNT(*), MIN(mosh_f_khor), MIN(shomare_sanad) FROM f_khor WHERE shom_f_khor=?`, "INV-100").Scan(&count, &customer, &sanad); err != nil {
		t.Fatal(err)
	}
	if count != 1 || customer != "مشتری تست" || sanad != "000123" {
		t.Fatalf("invoice was not normalized and saved correctly: count=%d customer=%q sanad=%q", count, customer, sanad)
	}

	conflictRequest := httptest.NewRequest(http.MethodPost, "/api/out-invoice", bytes.NewBufferString(`{
		"invoice_no":"INV-101","sanad_no":"000124","customer":"مشتری دوم","kala":"پارچه تست","items":["101"]
	}`))
	conflictResponse := httptest.NewRecorder()
	application.outInvoice(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("sold taghe must be rejected: %d %s", conflictResponse.Code, conflictResponse.Body.String())
	}
	if err := application.queryRow(`SELECT COUNT(*) FROM f_khor`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("conflicting save changed invoice rows: count=%d err=%v", count, err)
	}
}

func TestOperationalSessionUsesOpaqueServerSideToken(t *testing.T) {
	application := &app{sessions: map[string]sessionInfo{}}
	request := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	response := httptest.NewRecorder()
	if err := application.createSession(response, request, sessionInfo{UserID: 8, CompanyID: 4, Username: "tester", Role: "manager"}); err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) == 0 || len(cookies[0].Value) < 64 {
		t.Fatal("opaque session token was not created")
	}
	check := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	check.AddCookie(cookies[0])
	verified, ok := application.currentSession(check)
	if !ok || verified.CompanyID != 4 || verified.Username != "tester" {
		t.Fatalf("server-side session could not be read: %#v", verified)
	}
	tampered := *cookies[0]
	tampered.Value += "00"
	tamperedRequest := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	tamperedRequest.AddCookie(&tampered)
	if _, ok := application.currentSession(tamperedRequest); ok {
		t.Fatal("tampered session token must be rejected")
	}
}

func TestPortalManagerRespectsAssignedMenus(t *testing.T) {
	db, err := sql.Open("sqlite", "file:portal-menu-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO user_menu_access(user_id,menu_key,has_access) VALUES(5,'reports',1),(5,'database',0)`); err != nil {
		t.Fatal(err)
	}
	if allowed, err := application.userHasMenuAccess(5, "manager", "reports"); err != nil || !allowed {
		t.Fatalf("assigned report menu denied: allowed=%v err=%v", allowed, err)
	}
	if allowed, err := application.userHasMenuAccess(5, "manager", "database"); err != nil || allowed {
		t.Fatalf("unassigned restricted menu allowed: allowed=%v err=%v", allowed, err)
	}
}
