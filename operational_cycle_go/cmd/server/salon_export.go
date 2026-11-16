package main

import (
	"net/http"
	"strings"
)

func exportListQuery(r *http.Request, query string) string {
	if r.URL.Query().Get("all") == "1" {
		for _, limit := range []string{" LIMIT 200", " LIMIT 300", " LIMIT 500"} {
			query = strings.TrimSuffix(query, limit)
		}
	}
	return query
}

func salonListQuery(all bool) string {
	query := `SELECT s.id_salon, s.tarikh_salon, s.metr_salon, s.w_salon, s.machin_salon, s.user_salon, s.kala_salon, s.ham_pod_salon, s.ham_chelle_salon, s.shom_chelle_salon, COALESCE(k.id_kala_name,0), COALESCE(s.chelle_id_salon,0), COALESCE(s.tar_percent_salon,50), COALESCE(s.pod_percent_salon,50) FROM salon s LEFT JOIN kala_name k ON k.name_kala_name=s.kala_salon`
	if all {
		query += ` ORDER BY s.id_salon ASC`
	} else {
		query += ` ORDER BY s.id_salon DESC LIMIT 200`
	}
	return query
}

func reportInvoiceLimit(r *http.Request) int {
	if r.URL.Query().Get("all") == "1" {
		return 0
	}
	return 200
}
