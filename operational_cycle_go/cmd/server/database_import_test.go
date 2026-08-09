package main

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"
)

func TestImportXLSXRestoresTruncatedLongTableSheetNames(t *testing.T) {
	a, _, _ := newLoadingTestApp(t)
	if _, err := a.exec(`INSERT INTO machine_number_normalization_audit(table_name,row_id,column_name,old_value,new_value) VALUES(?,?,?,?,?)`, "salon", 7, "machin_salon", "7.0", "7"); err != nil {
		t.Fatal(err)
	}

	exported := httptest.NewRecorder()
	if err := a.exportXLSX(exported); err != nil {
		t.Fatal(err)
	}
	if _, err := a.exec(`DELETE FROM machine_number_normalization_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.exec(`INSERT INTO machine_number_normalization_audit(table_name,row_id,column_name,old_value,new_value) VALUES(?,?,?,?,?)`, "salon", 99, "machin_salon", "99.0", "99"); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "operational.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(exported.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/database/import-xlsx", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	tableCount, rowCount, err := a.importXLSX(req)
	if err != nil {
		t.Fatal(err)
	}
	if tableCount == 0 || rowCount == 0 {
		t.Fatalf("expected imported tables and rows, got tables=%d rows=%d", tableCount, rowCount)
	}

	var oldValue, newValue string
	if err := a.queryRow(`SELECT old_value,new_value FROM machine_number_normalization_audit WHERE row_id=7`).Scan(&oldValue, &newValue); err != nil {
		t.Fatal(err)
	}
	if oldValue != "7.0" || newValue != "7" {
		t.Fatalf("unexpected restored audit row: %q -> %q", oldValue, newValue)
	}
}
