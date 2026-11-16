package handler

import "net/http"

func exportLimit(r *http.Request, fallback int) int {
	if r.URL.Query().Get("all") == "1" {
		return -1
	}
	return parseLimit(r, fallback)
}
