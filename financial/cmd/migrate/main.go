package main

import (
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"log"
	"os"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg := postgres.LoadConfig()

	db, err := postgres.Connect(cfg)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()

	migrationsPath := "internal/infrastructure/persistence/postgres/migrations"
	if err := postgres.RunMigrations(db, migrationsPath); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	if err := postgres.EnsureFinancialUsers(db); err != nil {
		log.Printf("⚠️  User seed warning: %v", err)
	}

	if os.Getenv("SEED_DEMO_DATA") == "true" {
		if err := postgres.SeedSampleData(db); err != nil {
			log.Fatalf("Demo seed failed: %v", err)
		}
	}

	health := postgres.HealthCheck(db)
	log.Printf("Database health: %+v", health)
}
