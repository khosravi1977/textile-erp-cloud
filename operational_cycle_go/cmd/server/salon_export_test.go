package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestSalonExportIncludesRecordsBeyondListLimit(t *testing.T) {
	a, _, _ := newLoadingTestApp(t)
	if _, err := a.exec(`DELETE FROM salon`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 205; i++ {
		if _, err := a.exec(`INSERT INTO salon (id_salon, machin_salon) VALUES (?, ?)`, i, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path string
		want int
	}{{"/api/salon", 200}, {"/api/salon?all=1", 205}} {
		w := httptest.NewRecorder()
		a.salon(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: status %d: %s", tc.path, w.Code, w.Body.String())
		}
		var rows []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		if len(rows) != tc.want {
			t.Fatalf("%s: got %d rows, want %d", tc.path, len(rows), tc.want)
		}
	}
}
