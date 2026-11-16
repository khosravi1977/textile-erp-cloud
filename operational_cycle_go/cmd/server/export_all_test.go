package main

import (
	"net/http/httptest"
	"testing"
)

func TestExportListQueryPreservesDefaultLimit(t *testing.T) {
	q := "SELECT id FROM records ORDER BY id DESC LIMIT 200"
	if got := exportListQuery(httptest.NewRequest("GET", "/", nil), q); got != q {
		t.Fatal(got)
	}
	if got := exportListQuery(httptest.NewRequest("GET", "/?all=1", nil), q); got != "SELECT id FROM records ORDER BY id DESC" {
		t.Fatal(got)
	}
}
