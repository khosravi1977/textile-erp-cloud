package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newLoadingTestApp(t *testing.T) (*app, *http.ServeMux, string) {
	t.Helper()
	t.Setenv("OPERATIONAL_ADMIN_PASSWORD", "test-admin-password")
	t.Setenv("LOADING_PUBLIC_BASE", "")
	db, err := sql.Open("sqlite", "file:loading-test-"+strings.ReplaceAll(t.Name(), "/", "-")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	a := &app{db: db, dialect: "sqlite", dbLabel: "test", sessions: map[string]sessionInfo{}}
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := a.queryRow(`SELECT id_user FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	sessionToken := "employee-session"
	a.sessions[sessionToken] = sessionInfo{UserID: userID, Username: "admin", Role: "admin"}
	if _, err := a.exec(`INSERT INTO salon (id_salon,metr_salon,w_salon,machin_salon,user_salon,tarikh_salon,kala_salon,ham_pod_salon,ham_chelle_salon,shom_chelle_salon) VALUES (101,52.5,18.2,'M-1','operator','1405/04/23','پارچه تست','پود 1','تار 1','CH-7')`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.exec(`INSERT INTO salon (id_salon,metr_salon,w_salon,machin_salon,user_salon,tarikh_salon,kala_salon,ham_pod_salon,ham_chelle_salon,shom_chelle_salon) VALUES (102,48,17.1,'M-2','operator','1405/04/23','کالای دیگر','پود 2','تار 2','CH-8')`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.exec(`INSERT INTO salon (id_salon,metr_salon,w_salon,machin_salon,user_salon,tarikh_salon,kala_salon,ham_pod_salon,ham_chelle_salon,shom_chelle_salon) VALUES (103,44,16.4,'M-3','operator','1405/04/23','پارچه تست','پود 3','تار 3','CH-9')`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.exec(`INSERT INTO f_khor (tarikh_f_khor,shom_f_khor,taghe_cod_f_khor,mosh_f_khor,shomare_sanad,kala_name_f_khor) VALUES ('1405/04/23','OUT-OLD','103','مشتری قبلی','000000','پارچه تست')`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.routes(mux)
	return a, mux, sessionToken
}

func loadingRequest(t *testing.T, mux http.Handler, method, path, sessionToken string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &body)
	req.Host = "192.168.10.20:8091"
	req.Header.Set("Content-Type", "application/json")
	if sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "operational_session", Value: sessionToken})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeLoadingResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	data := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("decode response %d %q: %v", rec.Code, rec.Body.String(), err)
	}
	return data
}

func TestLoadingSessionScanConfirmAndFinalize(t *testing.T) {
	a, mux, employeeSession := newLoadingTestApp(t)
	createdRec := loadingRequest(t, mux, http.MethodPost, "/api/out-invoice/loading", employeeSession, map[string]any{
		"invoice_no": "OUT-100", "sanad_no": "000001", "customer": "مشتری تست", "kala": "پارچه تست",
	})
	if createdRec.Code != http.StatusOK {
		t.Fatalf("create session: %d %s", createdRec.Code, createdRec.Body.String())
	}
	created := decodeLoadingResponse(t, createdRec)
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatal("expected loading token")
	}
	if got := created["url"]; got != "http://192.168.10.20:8091/login?next="+url.QueryEscape("/operational/loading/"+token) {
		t.Fatalf("unexpected local loading URL: %v", got)
	}

	scanRec := loadingRequest(t, mux, http.MethodPost, "/api/loading/"+token+"/scan", employeeSession, map[string]string{"code": "101"})
	if scanRec.Code != http.StatusOK {
		t.Fatalf("scan: %d %s", scanRec.Code, scanRec.Body.String())
	}
	scan := decodeLoadingResponse(t, scanRec)
	item := scan["item"].(map[string]any)
	if item["matches"] != true || item["kala"] != "پارچه تست" {
		t.Fatalf("unexpected scan item: %#v", item)
	}

	mismatchRec := loadingRequest(t, mux, http.MethodPost, "/api/loading/"+token+"/scan", employeeSession, map[string]string{"code": "102"})
	mismatch := decodeLoadingResponse(t, mismatchRec)["item"].(map[string]any)
	if mismatch["matches"] != false {
		t.Fatalf("expected mismatch: %#v", mismatch)
	}
	mismatchConfirm := loadingRequest(t, mux, http.MethodPost, "/api/loading/"+token+"/confirm", employeeSession, map[string]string{"code": "102"})
	if mismatchConfirm.Code != http.StatusConflict {
		t.Fatalf("mismatched taghe must not confirm: %d %s", mismatchConfirm.Code, mismatchConfirm.Body.String())
	}

	confirmRec := loadingRequest(t, mux, http.MethodPost, "/api/loading/"+token+"/confirm", employeeSession, map[string]string{"code": "101"})
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("confirm: %d %s", confirmRec.Code, confirmRec.Body.String())
	}
	repeatScan := loadingRequest(t, mux, http.MethodPost, "/api/loading/"+token+"/scan", employeeSession, map[string]string{"code": "101"})
	if repeatScan.Code != http.StatusConflict {
		t.Fatalf("duplicate scan in same invoice must be rejected: %d %s", repeatScan.Code, repeatScan.Body.String())
	}
	repeatConfirm := loadingRequest(t, mux, http.MethodPost, "/api/loading/"+token+"/confirm", employeeSession, map[string]string{"code": "101"})
	if repeatConfirm.Code != http.StatusConflict {
		t.Fatalf("duplicate confirm in same invoice must be rejected: %d %s", repeatConfirm.Code, repeatConfirm.Body.String())
	}
	oldInvoiceScan := loadingRequest(t, mux, http.MethodPost, "/api/loading/"+token+"/scan", employeeSession, map[string]string{"code": "103"})
	if oldInvoiceScan.Code != http.StatusConflict || !strings.Contains(oldInvoiceScan.Body.String(), "OUT-OLD") {
		t.Fatalf("taghe from another invoice must be rejected with invoice number: %d %s", oldInvoiceScan.Code, oldInvoiceScan.Body.String())
	}
	stateRec := loadingRequest(t, mux, http.MethodGet, "/api/loading/"+token, employeeSession, nil)
	state := decodeLoadingResponse(t, stateRec)
	if state["totals"].(map[string]any)["count"] != float64(1) {
		t.Fatalf("unexpected state: %#v", state)
	}

	finalRec := loadingRequest(t, mux, http.MethodPost, "/api/out-invoice", employeeSession, map[string]any{
		"invoice_no": "OUT-100", "sanad_no": "000001", "customer": "مشتری تست", "kala": "پارچه تست",
		"items": []string{"101"}, "loading_session_token": token,
	})
	if finalRec.Code != http.StatusOK {
		t.Fatalf("finalize invoice: %d %s", finalRec.Code, finalRec.Body.String())
	}
	var invoiceNo string
	if err := a.queryRow(`SELECT shom_f_khor FROM f_khor WHERE taghe_cod_f_khor='101'`).Scan(&invoiceNo); err != nil || invoiceNo != "OUT-100" {
		t.Fatalf("invoice not persisted: %q %v", invoiceNo, err)
	}
	closedRec := loadingRequest(t, mux, http.MethodGet, "/api/loading/"+token, employeeSession, nil)
	if closedRec.Code != http.StatusGone {
		t.Fatalf("completed session must be closed: %d %s", closedRec.Code, closedRec.Body.String())
	}
}

func TestLoadingSessionRequiresEmployeeLogin(t *testing.T) {
	_, mux, employeeSession := newLoadingTestApp(t)
	createdRec := loadingRequest(t, mux, http.MethodPost, "/api/out-invoice/loading", employeeSession, map[string]any{
		"invoice_no": "OUT-101", "customer": "مشتری تست", "kala": "پارچه تست",
	})
	token := decodeLoadingResponse(t, createdRec)["token"].(string)
	unauthorized := loadingRequest(t, mux, http.MethodGet, "/api/loading/"+token, "", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected employee authentication, got %d", unauthorized.Code)
	}
}

func TestLoadingSessionRejectsItemsAlreadyInDesktopInvoice(t *testing.T) {
	_, mux, employeeSession := newLoadingTestApp(t)
	createdRec := loadingRequest(t, mux, http.MethodPost, "/api/out-invoice/loading", employeeSession, map[string]any{
		"invoice_no": "OUT-MANUAL", "sanad_no": "000002", "customer": "مشتری تست", "kala": "پارچه تست", "items": []string{"101"},
	})
	if createdRec.Code != http.StatusOK {
		t.Fatalf("create session with desktop item: %d %s", createdRec.Code, createdRec.Body.String())
	}
	token := decodeLoadingResponse(t, createdRec)["token"].(string)

	duplicateScan := loadingRequest(t, mux, http.MethodPost, "/api/loading/"+token+"/scan", employeeSession, map[string]string{"code": "101"})
	if duplicateScan.Code != http.StatusConflict {
		t.Fatalf("mobile must reject item already added on desktop: %d %s", duplicateScan.Code, duplicateScan.Body.String())
	}

	finalRec := loadingRequest(t, mux, http.MethodPost, "/api/out-invoice", employeeSession, map[string]any{
		"invoice_no": "OUT-MANUAL", "sanad_no": "000002", "customer": "مشتری تست", "kala": "پارچه تست",
		"items": []string{"101"}, "loading_session_token": token,
	})
	if finalRec.Code != http.StatusOK {
		t.Fatalf("desktop item reserved by loading session must finalize: %d %s", finalRec.Code, finalRec.Body.String())
	}
}
