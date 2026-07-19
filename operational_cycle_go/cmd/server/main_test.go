package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMobileLoadingSessionTransfersValidatedTaghe(t *testing.T) {
	db, err := sql.Open("sqlite", "file:mobile-loading-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test", companyID: 1, sessionSecret: "test-session-secret-at-least-32-characters"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO salon (id_salon,metr_salon,w_salon,machin_salon,kala_salon,ham_pod_salon,ham_chelle_salon,shom_chelle_salon,company_id) VALUES (101,25.5,7.2,'1','پارچه تست','P','T','C-1',1)`); err != nil {
		t.Fatal(err)
	}
	signed, err := application.signSession(sessionInfo{UserID: 1, CompanyID: 1, Username: "admin", Role: "admin", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	createRequest := httptest.NewRequest(http.MethodPost, "/api/out-invoice/mobile-sessions", bytes.NewBufferString(`{"invoice_no":"INV-1","customer":"مشتری"}`))
	createRequest.AddCookie(&http.Cookie{Name: "operational_session", Value: signed})
	createResponse := httptest.NewRecorder()
	application.createMobileLoadingSession(createResponse, createRequest)
	if createResponse.Code != http.StatusOK {
		t.Fatalf("create session failed: %d %s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil || created.Token == "" {
		t.Fatalf("invalid session response: %v %s", err, createResponse.Body.String())
	}
	previewRequest := httptest.NewRequest(http.MethodPost, "/api/mobile-loading/"+created.Token+"/preview", bytes.NewBufferString(`{"code":"101"}`))
	previewResponse := httptest.NewRecorder()
	application.mobileLoadingPublic(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview taghe failed: %d %s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview struct {
		Item record `json:"item"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil || preview.Item["id"] == nil {
		t.Fatalf("invalid preview response: %v %s", err, previewResponse.Body.String())
	}
	beforeConfirmRequest := httptest.NewRequest(http.MethodGet, "/api/mobile-loading/"+created.Token, nil)
	beforeConfirmResponse := httptest.NewRecorder()
	application.mobileLoadingPublic(beforeConfirmResponse, beforeConfirmRequest)
	var beforeConfirm struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(beforeConfirmResponse.Body.Bytes(), &beforeConfirm); err != nil || beforeConfirm.Count != 0 {
		t.Fatalf("preview registered taghe before confirmation: %v %s", err, beforeConfirmResponse.Body.String())
	}
	addRequest := httptest.NewRequest(http.MethodPost, "/api/mobile-loading/"+created.Token+"/items", bytes.NewBufferString(`{"code":"101"}`))
	addResponse := httptest.NewRecorder()
	application.mobileLoadingPublic(addResponse, addRequest)
	if addResponse.Code != http.StatusOK {
		t.Fatalf("add taghe failed: %d %s", addResponse.Code, addResponse.Body.String())
	}
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/mobile-loading/"+created.Token, nil)
	statusResponse := httptest.NewRecorder()
	application.mobileLoadingPublic(statusResponse, statusRequest)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("read session failed: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	var status struct {
		Count int      `json:"count"`
		Items []record `json:"items"`
	}
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil || status.Count != 1 || len(status.Items) != 1 {
		t.Fatalf("unexpected mobile session payload: %v %s", err, statusResponse.Body.String())
	}
}

func TestOutInvoiceSaveValidatesAndPersists(t *testing.T) {
	db, err := sql.Open("sqlite", "file:out-invoice-save-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test", companyID: 1}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO salon (id_salon,metr_salon,w_salon,machin_salon,kala_salon,company_id) VALUES (101,25.5,7.2,'1','پارچه تست',1)`); err != nil {
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
		Success   bool   `json:"success"`
		InvoiceNo string `json:"invoice_no"`
		ItemCount int    `json:"item_count"`
		Date      string `json:"tarikh"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil || !saved.Success || saved.InvoiceNo != "INV-100" || saved.ItemCount != 1 || saved.Date == "" {
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

func TestOperationalSessionIsSignedAndRestartSafe(t *testing.T) {
	appOne := &app{sessionSecret: "test-session-secret-at-least-32-characters"}
	session := sessionInfo{UserID: 8, CompanyID: 4, Username: "tester", Role: "manager", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	token, err := appOne.signSession(session)
	if err != nil {
		t.Fatal(err)
	}
	appAfterRestart := &app{sessionSecret: appOne.sessionSecret}
	verified, err := appAfterRestart.verifySession(token)
	if err != nil || verified.CompanyID != 4 || verified.Username != "tester" {
		t.Fatalf("session did not survive restart: %#v %v", verified, err)
	}
	if _, err := appAfterRestart.verifySession(token + "tampered"); err == nil {
		t.Fatal("tampered session must be rejected")
	}
}

func TestPortalManagerRespectsAssignedMenus(t *testing.T) {
	db, err := sql.Open("sqlite", "file:portal-menu-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test", companyID: 1}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	session := sessionInfo{UserID: 5, CompanyID: 1, Role: "manager", Portal: true, MenuKeys: []string{"dashboard", "reports"}}
	if allowed, err := application.userHasMenuAccess(session, "reports"); err != nil || !allowed {
		t.Fatalf("assigned report menu denied: allowed=%v err=%v", allowed, err)
	}
	if allowed, err := application.userHasMenuAccess(session, "database"); err != nil || allowed {
		t.Fatalf("unassigned restricted menu allowed: allowed=%v err=%v", allowed, err)
	}
}

func TestPortalSessionClaimsAreVerified(t *testing.T) {
	secret := "test-portal-secret-at-least-32-characters"
	claims := portalSessionClaims{UserID: 9, CompanyID: 6, Username: "portal-user", Role: "viewer", AllowOperational: true, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	payload, _ := json.Marshal(claims)
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	token := body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	application := &app{portalSessionSecret: secret}
	verified, err := application.verifyPortalSession(token)
	if err != nil || verified.CompanyID != 6 || !verified.AllowOperational {
		t.Fatalf("portal token verification failed: %#v %v", verified, err)
	}
}
