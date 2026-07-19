package main

import (
	"fmt"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/presentation/router"
	"log"
	"net/http"
	"os"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	validateProductionConfiguration()

	// Database connection
	cfg := postgres.LoadConfig()
	db, err := postgres.Connect(cfg)
	if err != nil {
		if os.Getenv("APP_ENV") == "production" {
			log.Fatalf("Database is required in production: %v", err)
		}
		log.Printf("⚠️  Database not available: %v", err)
		log.Println("⚠️  Running without database - some features disabled")
	} else {
		defer db.Close()

		// Run migrations
		migrationsPath := "internal/infrastructure/persistence/postgres/migrations"
		if err := postgres.RunMigrations(db, migrationsPath); err != nil {
			if os.Getenv("APP_ENV") == "production" {
				log.Fatalf("Database migration failed: %v", err)
			}
			log.Printf("⚠️  Migration warning: %v", err)
		}
		if err := postgres.EnsureFinancialUsers(db); err != nil {
			if os.Getenv("APP_ENV") == "production" {
				log.Fatalf("Financial user initialization failed: %v", err)
			}
			log.Printf("⚠️  User seed warning: %v", err)
		}

		// Seed sample data (only in development)
		if os.Getenv("APP_ENV") == "development" {
			if err := postgres.SeedSampleData(db); err != nil {
				log.Printf("⚠️  Seed warning: %v", err)
			}
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

func validateProductionConfiguration() {
	if os.Getenv("APP_ENV") != "production" {
		return
	}
	for _, key := range []string{"JWT_SECRET", "DB_PASSWORD", "FINANCIAL_ADMIN_PASSWORD"} {
		value := os.Getenv(key)
		if len(value) < 12 || value == "change_me" || value == "admin123" {
			log.Fatalf("%s must be configured securely for production", key)
		}
	}
	if len(os.Getenv("JWT_SECRET")) < 32 {
		log.Fatal("JWT_SECRET must contain at least 32 characters in production")
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
