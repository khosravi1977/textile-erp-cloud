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
		log.Printf("⚠️  Database connection failed: %v", err)
		log.Println("Make sure PostgreSQL is running (docker-compose up -d)")
		os.Exit(0)
	}
	defer db.Close()

	migrationsPath := "internal/infrastructure/persistence/postgres/migrations"
	if err := postgres.RunMigrations(db, migrationsPath); err != nil {
		log.Printf("⚠️  Migration warning: %v", err)
	}
	if err := postgres.EnsureFinancialUsers(db); err != nil {
		log.Printf("⚠️  User seed warning: %v", err)
	}

	if err := postgres.SeedSampleData(db); err != nil {
		log.Printf("⚠️  Seed warning: %v", err)
	}

	health := postgres.HealthCheck(db)
	log.Printf("Database health: %+v", health)
}
