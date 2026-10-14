package operationalbridge

import (
	"net/url"
	"os"
	"testing"
)

func TestPostgresPoolIsBoundedForBothConfigurationPaths(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is required")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	t.Setenv("DB_HOST", parsed.Hostname())
	t.Setenv("DB_PORT", parsed.Port())
	t.Setenv("DB_USER", parsed.User.Username())
	t.Setenv("DB_PASSWORD", password)
	t.Setenv("DB_NAME", parsed.Path[1:])
	t.Setenv("DB_SSLMODE", "disable")
	t.Setenv("OPERATIONAL_DB_DRIVER", "postgres")
	t.Setenv("OPERATIONAL_DB_MAX_OPEN_CONNS", "3")
	t.Setenv("OPERATIONAL_DB_MAX_IDLE_CONNS", "1")
	for _, configuration := range []string{"url", "environment"} {
		t.Run(configuration, func(t *testing.T) {
			if configuration == "url" {
				t.Setenv("OPERATIONAL_DATABASE_URL", dsn)
			} else {
				t.Setenv("OPERATIONAL_DATABASE_URL", "")
			}
			bridge, err := NewFromEnv()
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.Close()
			if stats := bridge.db.Stats(); stats.MaxOpenConnections != 3 {
				t.Fatalf("unbounded %s pool: %+v", configuration, stats)
			}
			if err := bridge.Close(); err != nil {
				t.Fatal(err)
			}
			if stats := bridge.db.Stats(); stats.OpenConnections != 0 {
				t.Fatalf("closing %s left connections open: %+v", configuration, stats)
			}
		})
	}
}
