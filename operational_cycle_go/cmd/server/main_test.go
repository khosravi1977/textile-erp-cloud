package main

import (
	"bytes"
	"database/sql"
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

func TestEmptyBeamOutRejectsDuplicateUntilReturn(t *testing.T) {
	db, err := sql.Open("sqlite", "file:empty-beam-out-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	post := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/empty-beam-out", bytes.NewBufferString(`{"beam":"B-1","warper":"W-1"}`))
		response := httptest.NewRecorder()
		application.emptyBeamOut(response, request)
		return response
	}
	if response := post(); response.Code != http.StatusOK {
		t.Fatalf("first empty beam exit failed: %d %s", response.Code, response.Body.String())
	}
	if response := post(); response.Code != http.StatusConflict {
		t.Fatalf("duplicate unresolved empty beam exit was allowed: %d %s", response.Code, response.Body.String())
	}
	if _, err := application.exec(`INSERT INTO chelle(tarikh_chelle,shom_chelle,pich_chelle,codnavard_chelle) VALUES(?,?,?,?)`, jalaliToday(), "CH-1", "W-1", "B-1"); err != nil {
		t.Fatal(err)
	}
	if response := post(); response.Code != http.StatusOK {
		t.Fatalf("returned beam could not exit again: %d %s", response.Code, response.Body.String())
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

func TestAdvisorMenuIsSeededAndReadOnlyUsersCanOpenIt(t *testing.T) {
	db, err := sql.Open("sqlite", "file:advisor-menu-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	var name string
	var restricted int
	if err := application.queryRow(`SELECT menu_name, is_restricted FROM menu_items WHERE menu_key='advisor'`).Scan(&name, &restricted); err != nil {
		t.Fatal(err)
	}
	if name != "تحلیل و مشاور هوشمند" || restricted != 0 {
		t.Fatalf("unexpected advisor menu: name=%q restricted=%d", name, restricted)
	}
	if allowed, err := application.userHasMenuAccess(99, "viewer", "advisor"); err != nil || !allowed {
		t.Fatalf("advisor must be available as read-only analysis: allowed=%v err=%v", allowed, err)
	}
}

func TestMiscMovementMenuIsSeeded(t *testing.T) {
	db, err := sql.Open("sqlite", "file:misc-movement-menu-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	var name string
	var restricted int
	if err := application.queryRow(`SELECT menu_name, is_restricted FROM menu_items WHERE menu_key='v-kh-moto'`).Scan(&name, &restricted); err != nil {
		t.Fatal(err)
	}
	if name != "ورودی/خروجی متفرقه" || restricted != 0 {
		t.Fatalf("unexpected misc movement menu: name=%q restricted=%d", name, restricted)
	}
	if allowed, err := application.userHasMenuAccess(99, "manager", "v-kh-moto"); err != nil || !allowed {
		t.Fatalf("misc movement must be available to managers: allowed=%v err=%v", allowed, err)
	}
}

func TestAdvisorPayloadUsesOperationalManagementData(t *testing.T) {
	db, err := sql.Open("sqlite", "file:advisor-payload-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/advisor", nil)
	response := httptest.NewRecorder()
	application.managementReport(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("advisor payload failed: %d %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"today", "month", "machines", "stock", "notifications", "data_quality"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("advisor payload is missing %q: %#v", key, payload)
		}
	}
}

func TestPortalSessionMenusComeOnlyFromCentralAccess(t *testing.T) {
	db, err := sql.Open("sqlite", "file:portal-central-menu-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test", sessions: map[string]sessionInfo{}}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}

	allowed := portalMenuKeySet([]string{"dashboard", "reports", "users"})
	menus := application.portalMenus(allowed)
	if len(menus) != 2 {
		t.Fatalf("expected only centrally assigned menus, got %#v", menus)
	}
	for _, menu := range menus {
		if menu["menu_key"] == "users" {
			t.Fatal("portal session exposed duplicate user management")
		}
	}

	sessionRequest := httptest.NewRequest(http.MethodPost, "/api/portal/session", nil)
	sessionResponse := httptest.NewRecorder()
	if err := application.createSession(sessionResponse, sessionRequest, sessionInfo{
		UserID: 1, CompanyID: 1, Username: "portal-manager", Role: "manager", Portal: true, MenuKeys: allowed,
	}); err != nil {
		t.Fatal(err)
	}
	cookies := sessionResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("portal session cookie was not created")
	}

	okRequest := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	okRequest.AddCookie(cookies[0])
	okResponse := httptest.NewRecorder()
	application.requireMenu("dashboard", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})(okResponse, okRequest)
	if okResponse.Code != http.StatusNoContent {
		t.Fatalf("centrally assigned menu was denied: %d %s", okResponse.Code, okResponse.Body.String())
	}

	deniedRequest := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	deniedRequest.AddCookie(cookies[0])
	deniedResponse := httptest.NewRecorder()
	application.requireMenu("users", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})(deniedResponse, deniedRequest)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("portal user management must be denied, got %d: %s", deniedResponse.Code, deniedResponse.Body.String())
	}
}

func TestMachineNumberNormalizationRepairsLegacyDecimals(t *testing.T) {
	db, err := sql.Open("sqlite", "file:machine-normalization-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO chelle(shom_chelle,machin_chelle) VALUES('CH-7','7.0')`,
		`INSERT INTO gere(shom_chelle_gere,machin_gere) VALUES('CH-7','7.0')`,
		`INSERT INTO nakh_salon(shom_machin_nakh_salon,ham_nakh_salon,w_nakh_salon) VALUES('7.0','P-7',10)`,
		`INSERT INTO salon(id_salon,machin_salon,shom_chelle_salon) VALUES(1,'7.0','CH-7')`,
		`INSERT INTO production_waste(machine,shom_chelle,waste_type,weight,reason) VALUES('7.0','CH-7','pod',1,'test')`,
		`INSERT INTO machine_formul(machine,tar_percent,pod_percent) VALUES('7',60,40)`,
		`INSERT INTO machine_formul(machine,tar_percent,pod_percent) VALUES('7.0',62,38)`,
	}
	for _, statement := range statements {
		if _, err := application.exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := application.normalizeMachineNumbers(); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ table, column string }{
		{"chelle", "machin_chelle"}, {"gere", "machin_gere"},
		{"nakh_salon", "shom_machin_nakh_salon"}, {"salon", "machin_salon"},
		{"production_waste", "machine"},
	} {
		var value string
		if err := application.queryRow(`SELECT ` + check.column + ` FROM ` + check.table + ` LIMIT 1`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		if value != "7" {
			t.Fatalf("%s.%s was not normalized: %q", check.table, check.column, value)
		}
	}
	var formulaCount int
	var tarPercent, podPercent float64
	if err := application.queryRow(`SELECT COUNT(*),MAX(tar_percent),MAX(pod_percent) FROM machine_formul WHERE machine='7'`).Scan(&formulaCount, &tarPercent, &podPercent); err != nil {
		t.Fatal(err)
	}
	if formulaCount != 1 || tarPercent != 62 || podPercent != 38 {
		t.Fatalf("latest duplicate formula was not preserved: count=%d tar=%v pod=%v", formulaCount, tarPercent, podPercent)
	}
	var archived int
	if err := application.queryRow(`SELECT COUNT(*) FROM machine_formul_archive WHERE canonical_machine='7'`).Scan(&archived); err != nil || archived != 1 {
		t.Fatalf("duplicate formula was not archived: count=%d err=%v", archived, err)
	}
}

func TestNakhSalonCreateAndEditAcceptLegacyMachineDecimal(t *testing.T) {
	db, err := sql.Open("sqlite", "file:nakh-salon-machine-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO mosh_name(name_mosh) VALUES('PAREGOL')`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO nakh_name(name_nakh_name) VALUES('YARN-7')`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO chelle(id_chelle,shom_chelle,machin_chelle,hambaft_chelle,mosh_chelle,nakh_chelle) VALUES(1,'CH-7','7.0','HB-7','PAREGOL','YARN-7')`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO nakh_vor(w_vor_nakh_vor,moshname_nakh_vor,hambaft_nakh_vor,nakh_name_nakh_vor) VALUES(100,'PAREGOL','HB-7','YARN-7')`); err != nil {
		t.Fatal(err)
	}
	post := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/nakh-salon", bytes.NewBufferString(body))
		response := httptest.NewRecorder()
		application.nakhSalon(response, request)
		return response
	}
	created := post(`{"machine":"7","ham_nakh":"HB-7","weight":20,"chelle_id":1,"mosh_name":"PAREGOL","nakh_name":"YARN-7","vor_khor":"vorud"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create failed: %d %s", created.Code, created.Body.String())
	}
	updated := post(`{"id":1,"machine":"7.0","ham_nakh":"HB-7","weight":25,"chelle_id":1,"mosh_name":"PAREGOL","nakh_name":"YARN-7","vor_khor":"vorud"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("edit failed: %d %s", updated.Code, updated.Body.String())
	}
	var machine string
	var weight float64
	if err := application.queryRow(`SELECT shom_machin_nakh_salon,w_nakh_salon FROM nakh_salon WHERE id_nakh_salon=1`).Scan(&machine, &weight); err != nil {
		t.Fatal(err)
	}
	if machine != "7" || weight != 25 {
		t.Fatalf("edited yarn entry was not canonical: machine=%q weight=%v", machine, weight)
	}
}

func TestSalonDefaultsReturnsTwoLatestDistinctPodHambafts(t *testing.T) {
	db, err := sql.Open("sqlite", "file:salon-pod-options-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	for _, hambaft := range []string{"HB-OLD", "HB-CURRENT", "HB-CURRENT", "HB-NEW"} {
		if _, err := application.exec(`INSERT INTO nakh_salon(shom_machin_nakh_salon,ham_nakh_salon,w_nakh_salon,vor_khor_nakh_salon) VALUES('8.0',?,10,'vorud')`, hambaft); err != nil {
			t.Fatal(err)
		}
	}
	items := application.recentMachinePodHambafts("8", 2)
	if len(items) != 2 || items[0] != "HB-NEW" || items[1] != "HB-CURRENT" {
		t.Fatalf("unexpected pod hambaft options: %#v", items)
	}
}

func TestSalonPodOptionsLimitsResultsToTwoLatestHambafts(t *testing.T) {
	db, err := sql.Open("sqlite", "file:salon-pod-options-handler-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO chelle(id_chelle,shom_chelle,machin_chelle) VALUES(1,'CH-8','8')`); err != nil {
		t.Fatal(err)
	}
	for _, hambaft := range []string{"HB-OLD", "HB-CURRENT", "HB-NEW"} {
		if _, err := application.exec(`INSERT INTO nakh_salon(shom_machin_nakh_salon,ham_nakh_salon,w_nakh_salon,shom_chelle_nakh_salon,chelle_id_nakh_salon) VALUES('8',?,10,'CH-8',1)`, hambaft); err != nil {
			t.Fatal(err)
		}
	}
	response := httptest.NewRecorder()
	application.salonPodOptions(response, "8", 1)
	if response.Code != http.StatusOK {
		t.Fatalf("pod options failed: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []struct {
			Hambaft string `json:"hambaft"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 2 || payload.Items[0].Hambaft != "HB-NEW" || payload.Items[1].Hambaft != "HB-CURRENT" {
		t.Fatalf("unexpected pod options response: %#v", payload.Items)
	}
}

func TestSalonPodOptionsAllowsPreviousOfTwoRecentChelles(t *testing.T) {
	db, err := sql.Open("sqlite", "file:salon-previous-chelle-pod-options-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	application := &app{db: db, dialect: "sqlite", dbLabel: "test"}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := application.exec(`INSERT INTO chelle(id_chelle,shom_chelle,machin_chelle) VALUES
		(1,'CH-PREVIOUS',''),(2,'CH-CURRENT','8')`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO gere(id_gere,tarikh_gere,shom_chelle_gere,machin_gere,chelle_id_gere) VALUES
		(1,'1405/05/01','CH-PREVIOUS','8.0',1),(2,'1405/05/02','CH-CURRENT','8',2)`); err != nil {
		t.Fatal(err)
	}
	for _, hambaft := range []string{"HB-OLD", "HB-CURRENT", "HB-NEW"} {
		if _, err := application.exec(`INSERT INTO nakh_salon(shom_machin_nakh_salon,ham_nakh_salon,w_nakh_salon,shom_chelle_nakh_salon,chelle_id_nakh_salon) VALUES('8.0',?,10,'CH-PREVIOUS',1)`, hambaft); err != nil {
			t.Fatal(err)
		}
	}
	responseChannel := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		application.salonPodOptions(response, "8", 1)
		responseChannel <- response
	}()
	var response *httptest.ResponseRecorder
	select {
	case response = <-responseChannel:
	case <-time.After(2 * time.Second):
		// Let a broken implementation unwind so the test can report the deadlock
		// instead of leaving the SQLite connection occupied indefinitely.
		db.SetMaxOpenConns(2)
		<-responseChannel
		t.Fatal("pod options deadlocked while resolving a previous chelle with one database connection")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("previous chelle pod options failed: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []struct {
			Hambaft string `json:"hambaft"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 2 || payload.Items[0].Hambaft != "HB-NEW" || payload.Items[1].Hambaft != "HB-CURRENT" {
		t.Fatalf("unexpected previous chelle pod options: %#v", payload.Items)
	}
	id, _, shom, err := application.productionChelleInfo("8", 1, "CH-PREVIOUS")
	if err != nil || id != 1 || shom != "CH-PREVIOUS" {
		t.Fatalf("previous chelle was not accepted for production: id=%d shom=%q err=%v", id, shom, err)
	}
}
