package main

import (
    "database/sql"
    "fmt"
    "log"
    "os"

    _ "github.com/lib/pq"
)

func main() {
    host := getEnv("DB_HOST", "localhost")
    port := getEnv("DB_PORT", "5433")
    user := getEnv("DB_USER", "erp_user")
    password := getEnv("DB_PASSWORD", "change_me")
    dbname := getEnv("DB_NAME", "textile_erp")

    dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", host, port, user, password, dbname)
    
    db, err := sql.Open("postgres", dsn)
    if err != nil {
        log.Printf("Connection failed: %v", err)
        log.Println("Make sure PostgreSQL is running: docker-compose up -d")
        os.Exit(0)
    }
    defer db.Close()

    if err := db.Ping(); err != nil {
        log.Printf("Ping failed: %v", err)
        os.Exit(0)
    }

    log.Println("Database connected successfully!")

    var count int
    db.QueryRow("SELECT COUNT(*) FROM parties").Scan(&count)
    log.Printf("Parties in database: %d", count)
    
    db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count)
    log.Printf("Items in database: %d", count)
    
    db.QueryRow("SELECT COUNT(*) FROM production_orders").Scan(&count)
    log.Printf("Production orders: %d", count)

    log.Println("All checks passed!")
}

func getEnv(key, defaultValue string) string {
    if value, exists := os.LookupEnv(key); exists {
        return value
    }
    return defaultValue
}
