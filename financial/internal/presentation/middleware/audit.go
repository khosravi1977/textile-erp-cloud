package middleware

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// AuditLog records request audit information in structured logs and database when available.
func AuditLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		audit := map[string]interface{}{
			"timestamp":   start.Format(time.RFC3339),
			"method":      r.Method,
			"path":        r.URL.Path,
			"remote_ip":   r.RemoteAddr,
			"user_agent":  r.UserAgent(),
			"company_id":  CompanyIDFromContext(r.Context()),
			"user_id":     UserIDFromContext(r.Context()),
			"request_id":  RequestIDFromContext(r.Context()),
			"status_code": rec.statusCode,
			"duration_ms": duration.Milliseconds(),
		}

		auditJSON, _ := json.Marshal(audit)
		log.Printf("[AUDIT] %s", auditJSON)

		if duration > time.Second {
			log.Printf("[SLOW] %s %s took %v", r.Method, r.URL.Path, duration)
		}

		storeAuditLog(r, rec.statusCode, duration)
	})
}

func storeAuditLog(r *http.Request, statusCode int, duration time.Duration) {
	// The management report supports a scoped query-string token so scheduled
	// agents can call it. Never persist that query string to audit storage.
	// The request is still represented by the structured path/status log above.
	if r.URL.Path == "/api/management-report" {
		return
	}

	db := postgres.DB
	if db == nil {
		return
	}

	details, _ := json.Marshal(map[string]interface{}{
		"query": r.URL.RawQuery,
	})

	_, err := postgres.WithCompanySession(r.Context(), db, CompanyIDFromContext(r.Context()), func(q postgres.SessionQueryable) (struct{}, error) {
		_, err := q.ExecContext(r.Context(), `
			INSERT INTO audit_logs (
				company_id, user_id, method, path, remote_ip, user_agent,
				request_id, duration_ms, status_code, details
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`,
			nullIfZero(CompanyIDFromContext(r.Context())),
			nullIfZero(UserIDFromContext(r.Context())),
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			r.UserAgent(),
			RequestIDFromContext(r.Context()),
			duration.Milliseconds(),
			statusCode,
			string(details),
		)
		return struct{}{}, err
	})
	if err != nil {
		log.Printf("audit insert warning: %v", err)
	}
}

func nullIfZero(v int64) any {
	if v <= 0 {
		return sql.NullInt64{}
	}
	return v
}
