package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type operationalFixture struct {
	app      *app
	ownerID  int64
	yarnAID  int64
	yarnBID  int64
	warperID int64
	beamID   int64
	tierID   int64
	kalaID   int64
}

func newOperationalFixture(t *testing.T) operationalFixture {
	t.Helper()
	dsn := fmt.Sprintf("file:operations-acceptance-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	a := &app{db: db, dialect: "sqlite", dbLabel: "test", sessions: map[string]sessionInfo{}}
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO mosh_name(name_mosh) VALUES('مالک الف')`,
		`INSERT INTO nakh_name(name_nakh_name) VALUES('نخ A'),('نخ B')`,
		`INSERT INTO chellepich(name_chellepich) VALUES('چله‌پیچ الف')`,
		`INSERT INTO kod_navard(kod_kod_navard) VALUES('نورد-۱'),('نورد-۲')`,
		`INSERT INTO gerezan(name_gerezan) VALUES('گره‌زن الف')`,
		`INSERT INTO kala_name(name_kala_name) VALUES('پارچه تست')`,
	} {
		if _, err := a.exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var f operationalFixture
	f.app = a
	queries := []struct {
		target *int64
		query  string
	}{
		{&f.ownerID, `SELECT id_mosh_name FROM mosh_name WHERE name_mosh='مالک الف'`},
		{&f.yarnAID, `SELECT id_nakh_name FROM nakh_name WHERE name_nakh_name='نخ A'`},
		{&f.yarnBID, `SELECT id_nakh_name FROM nakh_name WHERE name_nakh_name='نخ B'`},
		{&f.warperID, `SELECT id_chellepich FROM chellepich WHERE name_chellepich='چله‌پیچ الف'`},
		{&f.beamID, `SELECT id_kod_navard FROM kod_navard WHERE kod_kod_navard='نورد-۱'`},
		{&f.tierID, `SELECT id_gerezan FROM gerezan WHERE name_gerezan='گره‌زن الف'`},
		{&f.kalaID, `SELECT id_kala_name FROM kala_name WHERE name_kala_name='پارچه تست'`},
	}
	for _, item := range queries {
		if err := a.queryRow(item.query).Scan(item.target); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func callOperationalJSON(t *testing.T, handler http.HandlerFunc, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body := bytes.NewBuffer(nil)
	if payload != nil {
		if err := json.NewEncoder(body).Encode(payload); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func requireStatus(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("unexpected status: got %d want %d: %s", response.Code, want, response.Body.String())
	}
}

func seedYarnInbound(t *testing.T, f operationalFixture, yarnID int64, weight float64) {
	t.Helper()
	response := callOperationalJSON(t, f.app.nakhVor, http.MethodPost, "/api/nakh-vor", map[string]any{
		"hambaft": "HB-1", "weight": weight, "mosh_id": f.ownerID, "nakh_id": yarnID,
	})
	requireStatus(t, response, http.StatusOK)
}

func seedWarperExit(t *testing.T, f operationalFixture, yarn string, weight float64) {
	t.Helper()
	response := callOperationalJSON(t, f.app.nakhKhor, http.MethodPost, "/api/nakh-khor", map[string]any{
		"hambaft": "HB-1", "weight": weight, "mosh_name": "چله‌پیچ الف",
		"owner_mosh": "مالک الف", "nakh_name": yarn, "destination_type": "warper",
	})
	requireStatus(t, response, http.StatusOK)
}

func seedChelle(t *testing.T, f operationalFixture, number string, weight float64, beamID int64) int64 {
	t.Helper()
	response := callOperationalJSON(t, f.app.chelle, http.MethodPost, "/api/chelle", map[string]any{
		"shom_chelle": number, "nakh_id": f.yarnAID, "weight": weight,
		"pich_id": f.warperID, "mosh_id": f.ownerID, "hambaft": "HB-1", "kod_navard_id": beamID,
	})
	requireStatus(t, response, http.StatusOK)
	var id int64
	if err := f.app.queryRow(`SELECT id_chelle FROM chelle WHERE shom_chelle=? ORDER BY id_chelle DESC LIMIT 1`, number).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assignChelle(t *testing.T, f operationalFixture, chelleID int64, machine string) {
	t.Helper()
	response := callOperationalJSON(t, f.app.gere, http.MethodPost, "/api/gere", map[string]any{
		"gerezan_id": f.tierID, "chelle_id": chelleID, "machine": machine,
	})
	requireStatus(t, response, http.StatusOK)
}

func TestAcceptanceYarnInventoryUsesOwnerHambaftAndYarnType(t *testing.T) {
	f := newOperationalFixture(t)
	seedYarnInbound(t, f, f.yarnAID, 40)
	seedYarnInbound(t, f, f.yarnBID, 60)
	inventory := f.app.yarnInventory()
	balances := map[string]float64{}
	for _, item := range inventory {
		if owner, _ := item["mosh"].(string); owner == "مالک الف" {
			balances[fmt.Sprint(item["yarn"])] = item["inventory"].(float64)
		}
	}
	if balances["نخ A"] != 40 || balances["نخ B"] != 60 {
		t.Fatalf("yarn types were mixed: %#v", balances)
	}
}

func TestAcceptanceYarnExitCannotExceedExactInventory(t *testing.T) {
	f := newOperationalFixture(t)
	seedYarnInbound(t, f, f.yarnAID, 100)
	payload := map[string]any{"hambaft": "HB-1", "mosh_name": "مصرف دیگر", "owner_mosh": "مالک الف", "nakh_name": "نخ A", "destination_type": "other"}
	payload["weight"] = 101
	requireStatus(t, callOperationalJSON(t, f.app.nakhKhor, http.MethodPost, "/api/nakh-khor", payload), http.StatusBadRequest)
	payload["weight"] = 30
	requireStatus(t, callOperationalJSON(t, f.app.nakhKhor, http.MethodPost, "/api/nakh-khor", payload), http.StatusOK)
	if balance := f.app.warehouseYarnBalance("مالک الف", "HB-1", "نخ A", 0, 0); balance != 70 {
		t.Fatalf("unexpected balance: %.3f", balance)
	}
	payload["destination_type"] = "invalid"
	requireStatus(t, callOperationalJSON(t, f.app.nakhKhor, http.MethodPost, "/api/nakh-khor", payload), http.StatusBadRequest)
}

func TestAcceptanceOtherDestinationDoesNotIncreaseWarperBalance(t *testing.T) {
	f := newOperationalFixture(t)
	seedYarnInbound(t, f, f.yarnAID, 100)
	payload := map[string]any{"hambaft": "HB-1", "weight": 20, "mosh_name": "چله‌پیچ الف", "owner_mosh": "مالک الف", "nakh_name": "نخ A", "destination_type": "other"}
	requireStatus(t, callOperationalJSON(t, f.app.nakhKhor, http.MethodPost, "/api/nakh-khor", payload), http.StatusOK)
	for _, row := range f.app.warperYarnBalances() {
		if row["warper"] == "چله‌پیچ الف" && row["sent_weight"].(float64) != 0 {
			t.Fatalf("non-warper yarn exit leaked into warper balance: %#v", row)
		}
	}
}

func TestAcceptanceSalonYarnRequiresActiveChelleOnSameMachineAndLimitsReturns(t *testing.T) {
	f := newOperationalFixture(t)
	seedYarnInbound(t, f, f.yarnAID, 100)
	seedWarperExit(t, f, "نخ A", 60)
	chelleID := seedChelle(t, f, "CH-1", 50, f.beamID)
	assignChelle(t, f, chelleID, "M-1")
	payload := map[string]any{"machine": "M-2", "ham_nakh": "HB-1", "weight": 40, "chelle_id": chelleID, "mosh_name": "مالک الف", "nakh_name": "نخ A", "vor_khor": "vorud"}
	requireStatus(t, callOperationalJSON(t, f.app.nakhSalon, http.MethodPost, "/api/nakh-salon", payload), http.StatusBadRequest)
	payload["machine"] = "M-1"
	payload["ham_nakh"] = "HB-نامعتبر"
	requireStatus(t, callOperationalJSON(t, f.app.nakhSalon, http.MethodPost, "/api/nakh-salon", payload), http.StatusBadRequest)
	payload["ham_nakh"] = "HB-1"
	payload["mosh_name"] = "مالک نامعتبر"
	requireStatus(t, callOperationalJSON(t, f.app.nakhSalon, http.MethodPost, "/api/nakh-salon", payload), http.StatusBadRequest)
	payload["mosh_name"] = "مالک الف"
	payload["nakh_name"] = "نخ نامعتبر"
	requireStatus(t, callOperationalJSON(t, f.app.nakhSalon, http.MethodPost, "/api/nakh-salon", payload), http.StatusBadRequest)
	payload["nakh_name"] = "نخ A"
	requireStatus(t, callOperationalJSON(t, f.app.nakhSalon, http.MethodPost, "/api/nakh-salon", payload), http.StatusOK)
	payload["weight"] = 41
	payload["vor_khor"] = "khoroj"
	requireStatus(t, callOperationalJSON(t, f.app.nakhSalon, http.MethodPost, "/api/nakh-salon", payload), http.StatusBadRequest)
}

func TestAcceptanceFormulaMustEqualOneHundred(t *testing.T) {
	f := newOperationalFixture(t)
	requireStatus(t, callOperationalJSON(t, f.app.formulas, http.MethodPost, "/api/formulas", map[string]any{"machine": "M-1", "tar_percent": 70, "pod_percent": 40}), http.StatusBadRequest)
	requireStatus(t, callOperationalJSON(t, f.app.formulas, http.MethodPost, "/api/formulas", map[string]any{"machine": "M-1", "tar_percent": 60, "pod_percent": 40}), http.StatusOK)
}

func TestAcceptanceOnlyLatestChelleRemainsActiveOnMachine(t *testing.T) {
	f := newOperationalFixture(t)
	seedYarnInbound(t, f, f.yarnAID, 200)
	seedWarperExit(t, f, "نخ A", 160)
	first := seedChelle(t, f, "CH-1", 70, f.beamID)
	second := seedChelle(t, f, "CH-2", 70, f.beamID)
	assignChelle(t, f, first, "M-1")
	assignChelle(t, f, second, "M-1")
	var latestLinkID int64
	if err := f.app.queryRow(`SELECT id_gere FROM gere WHERE chelle_id_gere=? ORDER BY id_gere DESC LIMIT 1`, second).Scan(&latestLinkID); err != nil {
		t.Fatal(err)
	}
	var firstMachine, secondMachine string
	_ = f.app.queryRow(`SELECT COALESCE(machin_chelle,'') FROM chelle WHERE id_chelle=?`, first).Scan(&firstMachine)
	_ = f.app.queryRow(`SELECT COALESCE(machin_chelle,'') FROM chelle WHERE id_chelle=?`, second).Scan(&secondMachine)
	if firstMachine != "" || secondMachine != "M-1" {
		t.Fatalf("active chelle invariant failed: first=%q second=%q", firstMachine, secondMachine)
	}
	requireStatus(t, callOperationalJSON(t, f.app.gereByID, http.MethodDelete, fmt.Sprintf("/api/gere/%d", latestLinkID), nil), http.StatusOK)
	_ = f.app.queryRow(`SELECT COALESCE(machin_chelle,'') FROM chelle WHERE id_chelle=?`, first).Scan(&firstMachine)
	_ = f.app.queryRow(`SELECT COALESCE(machin_chelle,'') FROM chelle WHERE id_chelle=?`, second).Scan(&secondMachine)
	if firstMachine != "M-1" || secondMachine != "" {
		t.Fatalf("previous chelle was not restored after deleting latest link: first=%q second=%q", firstMachine, secondMachine)
	}
}

func TestAcceptanceProductionEditDeleteRebuildsConsumptionAndUsesSession(t *testing.T) {
	f := newOperationalFixture(t)
	seedYarnInbound(t, f, f.yarnAID, 200)
	seedWarperExit(t, f, "نخ A", 100)
	chelleID := seedChelle(t, f, "CH-1", 80, f.beamID)
	assignChelle(t, f, chelleID, "M-1")
	requireStatus(t, callOperationalJSON(t, f.app.formulas, http.MethodPost, "/api/formulas", map[string]any{"machine": "M-1", "tar_percent": 60, "pod_percent": 40}), http.StatusOK)

	sessionResponse := httptest.NewRecorder()
	if err := f.app.createSession(sessionResponse, httptest.NewRequest(http.MethodPost, "/api/login", nil), sessionInfo{UserID: 9, Username: "operator-real", Role: "manager"}); err != nil {
		t.Fatal(err)
	}
	cookie := sessionResponse.Result().Cookies()[0]
	postProduction := func(id int64, weight float64) *httptest.ResponseRecorder {
		payload := map[string]any{
			"id": id, "metr": 50, "weight": weight, "machine": "M-1", "kala_id": f.kalaID,
			"ham_pod": "HB-1", "ham_chelle": "HB-1", "shom_chelle": "CH-1", "chelle_id": chelleID,
			"tar_percent": 60, "pod_percent": 40, "formula_confirmed": true, "user": "spoofed",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/salon", bytes.NewReader(body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		f.app.salon(rec, req)
		return rec
	}
	created := postProduction(0, 20)
	requireStatus(t, created, http.StatusOK)
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil || payload.ID == 0 {
		t.Fatalf("production id missing: %v %s", err, created.Body.String())
	}
	requireStatus(t, postProduction(payload.ID, 10), http.StatusOK)
	var total, tar, pod float64
	if err := f.app.queryRow(`SELECT total_weight,tar_used,pod_used FROM machine_consumption WHERE machine='M-1' AND shom_chelle='CH-1'`).Scan(&total, &tar, &pod); err != nil {
		t.Fatal(err)
	}
	if total != 10 || tar != 6 || pod != 4 {
		t.Fatalf("consumption was incremented instead of rebuilt: total=%v tar=%v pod=%v", total, tar, pod)
	}
	var savedUser string
	_ = f.app.queryRow(`SELECT user_salon FROM salon WHERE id_salon=?`, payload.ID).Scan(&savedUser)
	if savedUser != "operator-real" {
		t.Fatalf("production user was not taken from session: %q", savedUser)
	}
	var savedChelleID int64
	if err := f.app.queryRow(`SELECT COALESCE(chelle_id_salon,0) FROM salon WHERE id_salon=?`, payload.ID).Scan(&savedChelleID); err != nil || savedChelleID != chelleID {
		t.Fatalf("production did not retain the internal chelle id: id=%d err=%v", savedChelleID, err)
	}
	requireStatus(t, callOperationalJSON(t, f.app.salonByPath, http.MethodDelete, fmt.Sprintf("/api/salon/%d", payload.ID), nil), http.StatusOK)
	_ = f.app.queryRow(`SELECT total_weight FROM machine_consumption WHERE machine='M-1' AND shom_chelle='CH-1'`).Scan(&total)
	if total != 0 {
		t.Fatalf("consumption was not rebuilt after delete: %v", total)
	}
}

func TestAcceptanceWasteIsSeparateFromMaterialRemaining(t *testing.T) {
	f := newOperationalFixture(t)
	seedYarnInbound(t, f, f.yarnAID, 200)
	seedWarperExit(t, f, "نخ A", 100)
	chelleID := seedChelle(t, f, "CH-1", 80, f.beamID)
	assignChelle(t, f, chelleID, "M-1")
	response := callOperationalJSON(t, f.app.productionWaste, http.MethodPost, "/api/production-waste", map[string]any{
		"machine": "M-1", "chelle_id": chelleID, "shom_chelle": "CH-1", "waste_type": "tar", "weight": 2, "reason": "پارگی تار", "corrective_action": "تنظیم کشش",
	})
	requireStatus(t, response, http.StatusOK)
	var savedChelleID int64
	var correctiveAction string
	if err := f.app.queryRow(`SELECT COALESCE(chelle_id_waste,0),COALESCE(corrective_action,'') FROM production_waste ORDER BY id_waste DESC LIMIT 1`).Scan(&savedChelleID, &correctiveAction); err != nil || savedChelleID != chelleID || correctiveAction != "تنظیم کشش" {
		t.Fatalf("waste ledger lost chelle identity or corrective action: id=%d action=%q err=%v", savedChelleID, correctiveAction, err)
	}
	status, err := f.app.activeMachineStatus()
	if err != nil || len(status) != 1 {
		t.Fatalf("machine status failed: %v %#v", err, status)
	}
	if status[0]["actual_waste"].(float64) != 2 || status[0]["remaining"].(float64) == 2 {
		t.Fatalf("waste and remaining were mixed: %#v", status[0])
	}
}

func TestAcceptanceChelleCannotExceedSentYarnOrDuplicateNumber(t *testing.T) {
	f := newOperationalFixture(t)
	seedYarnInbound(t, f, f.yarnAID, 100)
	seedWarperExit(t, f, "نخ A", 30)
	tooHeavy := callOperationalJSON(t, f.app.chelle, http.MethodPost, "/api/chelle", map[string]any{
		"shom_chelle": "CH-X", "nakh_id": f.yarnAID, "weight": 31,
		"pich_id": f.warperID, "mosh_id": f.ownerID, "hambaft": "HB-1", "kod_navard_id": f.beamID,
	})
	requireStatus(t, tooHeavy, http.StatusBadRequest)
	seedChelle(t, f, "CH-1", 20, f.beamID)
	duplicate := callOperationalJSON(t, f.app.chelle, http.MethodPost, "/api/chelle", map[string]any{
		"shom_chelle": "CH-1", "nakh_id": f.yarnAID, "weight": 5,
		"pich_id": f.warperID, "mosh_id": f.ownerID, "hambaft": "HB-1", "kod_navard_id": f.beamID,
	})
	requireStatus(t, duplicate, http.StatusBadRequest)
}

func TestAcceptanceReportsRespondAndUnauthorizedRequestsAreRejected(t *testing.T) {
	f := newOperationalFixture(t)
	response := callOperationalJSON(t, f.app.managementReport, http.MethodGet, "/api/management-report", nil)
	requireStatus(t, response, http.StatusOK)
	var report map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"today", "month", "yarn_inventory", "warper_balances", "machines", "waste", "notifications", "data_quality", "out_invoices"} {
		if _, ok := report[key]; !ok {
			t.Fatalf("management report is missing %q", key)
		}
	}
	mux := http.NewServeMux()
	f.app.routes(mux)
	unauthorized := httptest.NewRecorder()
	mux.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/management-report", nil))
	requireStatus(t, unauthorized, http.StatusUnauthorized)
}
