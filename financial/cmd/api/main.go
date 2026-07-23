package main

import (
	"database/sql"
	"fmt"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/presentation/router"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Database connection
	cfg := postgres.LoadConfig()
	db, err := connectDatabaseWithRetry(cfg, 5*time.Minute)
	if err != nil {
		log.Fatalf("Database did not become ready: %v", err)
	}
	defer db.Close()

	// Run migrations
	migrationsPath := "internal/infrastructure/persistence/postgres/migrations"
	if err := postgres.RunMigrations(db, migrationsPath); err != nil {
		log.Printf("⚠️  Migration warning: %v", err)
	}
	if err := postgres.EnsureFinancialUsers(db); err != nil {
		log.Printf("⚠️  User seed warning: %v", err)
	}

	// Seed sample data (only in development)
	if os.Getenv("APP_ENV") == "development" {
		if err := postgres.SeedSampleData(db); err != nil {
			log.Printf("⚠️  Seed warning: %v", err)
		}
	}

	// Setup router
	r := router.SetupRouter()

	port := getEnv("APP_PORT", "8081")

	fmt.Println("")
	log.Printf("🚀 ERP Textile Server v2.1.0")
	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Printf("📊 Health:    http://localhost:%s/health", port)
	log.Printf("🤖 Advisor:   http://localhost:%s/api/advisor/advice", port)
	log.Printf("🧮 Commission: http://localhost:%s/api/commission/calculate", port)
	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("")

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}

func connectDatabaseWithRetry(cfg postgres.Config, timeout time.Duration) (*sql.DB, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		db, err := postgres.Connect(cfg)
		if err == nil {
			return db, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, lastErr
		}
		log.Printf("Database is starting; retrying in 3 seconds: %v", err)
		time.Sleep(3 * time.Second)
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
