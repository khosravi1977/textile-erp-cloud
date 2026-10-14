package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/presentation/handler"
)

func TestSupervisorSweepsReleaseOperationalPools(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("application_name", "supervisor-pool-regression")
	parsed.RawQuery = query.Encode()
	t.Setenv("OPERATIONAL_DATABASE_URL", parsed.String())
	t.Setenv("SUPERVISOR_AGENT_RULE_ONLY", "true")
	t.Setenv("CACHE_DISABLED", "true")
	t.Setenv("DB_READ_HOST", "")
	previousDB, previousReadDB := postgres.DB, postgres.ReadDB
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	sslMode := query.Get("sslmode")
	if sslMode == "" {
		sslMode = "disable"
	}
	password, _ := parsed.User.Password()
	db, err := postgres.Connect(postgres.Config{
		Host: parsed.Hostname(), Port: port, User: parsed.User.Username(),
		Password: password, DBName: parsed.Path[1:], SSLMode: sslMode,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		postgres.DB, postgres.ReadDB = previousDB, previousReadDB
	})
	if err := postgres.RunMigrations(db, filepath.Join("..", "..", "internal", "infrastructure", "persistence", "postgres", "migrations")); err != nil {
		t.Fatal(err)
	}
	var companyID int64
	if err := db.QueryRow(`INSERT INTO companies(code,name) VALUES($1,'connection regression') RETURNING id`, fmt.Sprintf("POOL-%d", time.Now().UnixNano())).Scan(&companyID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for iteration := 0; iteration < 30; iteration++ {
		handler.SweepSupervisorAgentForCompany(ctx, companyID)
		if err := ctx.Err(); err != nil {
			t.Fatal(err)
		}
		var connections int
		deadline := time.Now().Add(time.Second)
		for {
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE application_name='supervisor-pool-regression'`).Scan(&connections); err != nil {
				t.Fatal(err)
			}
			if connections == 0 || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if connections != 0 {
			t.Fatalf("sweep %d leaked %d operational connections", iteration+1, connections)
		}
	}
	var runs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM financial_agent_runs WHERE company_id=$1`, companyID).Scan(&runs); err != nil || runs < 1 {
		t.Fatalf("expected successful stored sweeps: runs=%d err=%v", runs, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	(&handler.APIHandler{}).HealthCheck(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("connected database health status=%d", response.Code)
	}
}
