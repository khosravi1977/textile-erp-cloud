package main

import (
	"bytes"
	"database/sql"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newBeamFormulaTestApp(t *testing.T) *app {
	t.Helper()
	t.Setenv("OPERATIONAL_ADMIN_PASSWORD", "test-admin-password")
	name := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	application := &app{db: db, dialect: "sqlite", dbLabel: "test", sessions: map[string]sessionInfo{}}
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}
	return application
}

func requirePercent(t *testing.T, got, want float64, label string) {
	t.Helper()
	if math.Abs(got-want) > 0.001 {
		t.Fatalf("%s: got %.4f want %.4f", label, got, want)
	}
}

func TestLegacyProductionFormulaIsSnapshottedAndHistoryDoesNotChange(t *testing.T) {
	application := newBeamFormulaTestApp(t)
	if _, err := application.exec(`INSERT INTO machine_formul (machine,tar_percent,pod_percent) VALUES ('1',62,38)`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO chelle
		(id_chelle,tarikh_chelle,shom_chelle,w_chelle,hambaft_chelle,machin_chelle)
		VALUES (2950,'1405/05/01','2950',200,'همبافت تار الف','1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO salon
		(id_salon,metr_salon,w_salon,machin_salon,tarikh_salon,kala_salon,ham_pod_salon,ham_chelle_salon,shom_chelle_salon)
		VALUES (1,300,100,'1','1405/05/02','مازراتی','همبافت پود الف','همبافت تار الف','2950')`); err != nil {
		t.Fatal(err)
	}

	// Re-running a backward-compatible migration simulates upgrading a live
	// database that already contains production records.
	if err := application.migrate(); err != nil {
		t.Fatal(err)
	}

	var chelleID int64
	var tarSnapshot, podSnapshot float64
	if err := application.queryRow(`SELECT chelle_id_salon,tar_percent_salon,pod_percent_salon
		FROM salon WHERE id_salon=1`).Scan(&chelleID, &tarSnapshot, &podSnapshot); err != nil {
		t.Fatal(err)
	}
	if chelleID != 2950 {
		t.Fatalf("legacy row was not linked to its beam: got %d", chelleID)
	}
	requirePercent(t, tarSnapshot, 62, "legacy warp snapshot")
	requirePercent(t, podSnapshot, 38, "legacy weft snapshot")

	var savedFormulaCount int
	if err := application.queryRow(`SELECT COUNT(*) FROM chelle_formul
		WHERE chelle_id=2950 AND kala_name='مازراتی'
		  AND ham_chelle='همبافت تار الف' AND ham_pod='همبافت پود الف'
		  AND tar_percent=62 AND pod_percent=38`).Scan(&savedFormulaCount); err != nil {
		t.Fatal(err)
	}
	if savedFormulaCount != 1 {
		t.Fatalf("expected one migrated hambaft formula, got %d", savedFormulaCount)
	}

	if _, err := application.exec(`UPDATE machine_formul SET tar_percent=40,pod_percent=60 WHERE machine='1'`); err != nil {
		t.Fatal(err)
	}
	tx, err := application.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := application.rebuildConsumptionTx(tx, "1", "2950"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var tarUsed, podUsed float64
	if err := application.queryRow(`SELECT tar_used,pod_used FROM machine_consumption
		WHERE machine='1' AND shom_chelle='2950'`).Scan(&tarUsed, &podUsed); err != nil {
		t.Fatal(err)
	}
	requirePercent(t, tarUsed, 62, "historical warp consumption")
	requirePercent(t, podUsed, 38, "historical weft consumption")
}

func TestFormulaIsReusedOnlyForSameHambaftAndFabric(t *testing.T) {
	application := newBeamFormulaTestApp(t)
	if _, err := application.exec(`INSERT INTO machine_formul (machine,tar_percent,pod_percent)
		VALUES ('1',50,50)`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO chelle
		(id_chelle,tarikh_chelle,shom_chelle,w_chelle,hambaft_chelle,machin_chelle)
		VALUES
		(1,'1405/05/01','2950',200,'همبافت تار الف','1'),
		(2,'1405/05/02','2951',210,'همبافت تار الف','1'),
		(3,'1405/05/03','2952',220,'همبافت تار الف','1')`); err != nil {
		t.Fatal(err)
	}
	tx, err := application.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := application.saveChelleFormulaTx(tx, chelleFormula{
		ChelleID: 1, Machine: "1", ShomChelle: "2950", Kala: "مازراتی",
		HamChelle: "همبافت تار الف", HamPod: "همبافت پود الف",
		TarPercent: 62, PodPercent: 38,
	}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	reused, err := application.resolveChelleFormula(
		"1", "2951", "مازراتی", "همبافت تار الف", "همبافت پود الف",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reused.Configured || reused.Source != "same_hambaft" {
		t.Fatalf("same combination should be reused: %#v", reused)
	}
	requirePercent(t, reused.TarPercent, 62, "reused warp")
	requirePercent(t, reused.PodPercent, 38, "reused weft")

	differentWeft, err := application.resolveChelleFormula(
		"1", "2951", "مازراتی", "همبافت تار الف", "همبافت پود ب",
	)
	if err != nil {
		t.Fatal(err)
	}
	if differentWeft.Configured || differentWeft.Source != "machine_default" {
		t.Fatalf("a new weft hambaft must require confirmation: %#v", differentWeft)
	}

	differentFabric, err := application.resolveChelleFormula(
		"1", "2951", "روسری", "همبافت تار الف", "همبافت پود الف",
	)
	if err != nil {
		t.Fatal(err)
	}
	if differentFabric.Configured || differentFabric.Source != "machine_default" {
		t.Fatalf("a new fabric must require confirmation: %#v", differentFabric)
	}

	// Editing the formula on the current beam becomes the latest suggestion for
	// future beams, while already-recorded production snapshots stay untouched.
	tx, err = application.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := application.saveChelleFormulaTx(tx, chelleFormula{
		ChelleID: 2, Machine: "1", ShomChelle: "2951", Kala: "مازراتی",
		HamChelle: "همبافت تار الف", HamPod: "همبافت پود الف",
		TarPercent: 60, PodPercent: 40,
	}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	latest, err := application.resolveChelleFormula(
		"1", "2952", "مازراتی", "همبافت تار الف", "همبافت پود الف",
	)
	if err != nil {
		t.Fatal(err)
	}
	requirePercent(t, latest.TarPercent, 60, "latest reused warp")
	requirePercent(t, latest.PodPercent, 40, "latest reused weft")
}

func TestSalonRequiresConfirmationOnlyForNewHambaftCombination(t *testing.T) {
	application := newBeamFormulaTestApp(t)
	if _, err := application.exec(`INSERT INTO kala_name (id_kala_name,name_kala_name) VALUES (1,'مازراتی')`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO machine_formul (machine,tar_percent,pod_percent)
		VALUES ('1',50,50)`); err != nil {
		t.Fatal(err)
	}
	if _, err := application.exec(`INSERT INTO chelle
		(id_chelle,tarikh_chelle,shom_chelle,w_chelle,hambaft_chelle,machin_chelle)
		VALUES
		(1,'1405/05/01','2950',200,'همبافت تار الف','1'),
		(2,'1405/05/02','2951',210,'همبافت تار الف','1')`); err != nil {
		t.Fatal(err)
	}

	newCombination := `{
		"metr":300,"weight":100,"machine":"1","kala_id":1,
		"ham_pod":"همبافت پود الف","ham_chelle":"همبافت تار الف",
		"shom_chelle":"2950","user":"operator","tar_percent":62,"pod_percent":38
	}`
	unconfirmedRequest := httptest.NewRequest(http.MethodPost, "/api/salon", bytes.NewBufferString(newCombination))
	unconfirmedResponse := httptest.NewRecorder()
	application.salon(unconfirmedResponse, unconfirmedRequest)
	if unconfirmedResponse.Code != http.StatusBadRequest {
		t.Fatalf("new combination must require confirmation: %d %s", unconfirmedResponse.Code, unconfirmedResponse.Body.String())
	}

	confirmedRequest := httptest.NewRequest(http.MethodPost, "/api/salon", bytes.NewBufferString(strings.Replace(
		newCombination, `"pod_percent":38`, `"pod_percent":38,"formula_confirmed":true`, 1,
	)))
	confirmedResponse := httptest.NewRecorder()
	application.salon(confirmedResponse, confirmedRequest)
	if confirmedResponse.Code != http.StatusOK {
		t.Fatalf("confirmed formula was not saved: %d %s", confirmedResponse.Code, confirmedResponse.Body.String())
	}

	var tarSnapshot, podSnapshot float64
	if err := application.queryRow(`SELECT tar_percent_salon,pod_percent_salon FROM salon
		WHERE shom_chelle_salon='2950'`).Scan(&tarSnapshot, &podSnapshot); err != nil {
		t.Fatal(err)
	}
	requirePercent(t, tarSnapshot, 62, "confirmed warp snapshot")
	requirePercent(t, podSnapshot, 38, "confirmed weft snapshot")

	// The next beam uses the same fabric and both hambaft values. It must be
	// accepted without another explicit confirmation.
	reusedRequest := httptest.NewRequest(http.MethodPost, "/api/salon", bytes.NewBufferString(`{
		"metr":250,"weight":80,"machine":"1","kala_id":1,
		"ham_pod":"همبافت پود الف","ham_chelle":"همبافت تار الف",
		"shom_chelle":"2951","user":"operator","tar_percent":62,"pod_percent":38
	}`))
	reusedResponse := httptest.NewRecorder()
	application.salon(reusedResponse, reusedRequest)
	if reusedResponse.Code != http.StatusOK {
		t.Fatalf("same hambaft combination asked for confirmation again: %d %s", reusedResponse.Code, reusedResponse.Body.String())
	}
}
