package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/erpsystem/textile-erp/internal/platform/password"
)

var DB *sql.DB
var ReadDB *sql.DB

type Config struct {
	Host       string
	Port       string
	User       string
	Password   string
	DBName     string
	SSLMode    string
	ReadHost   string
	ReadPort   string
	ReadUser   string
	ReadPass   string
	ReadDBName string
}

func LoadConfig() Config {
	return Config{
		Host:       getEnv("DB_HOST", "localhost"),
		Port:       getEnv("DB_PORT", "5432"),
		User:       getEnv("DB_USER", "erp_user"),
		Password:   getEnv("DB_PASSWORD", "change_me"),
		DBName:     getEnv("DB_NAME", "textile_erp"),
		SSLMode:    getEnv("DB_SSLMODE", "disable"),
		ReadHost:   getEnv("DB_READ_HOST", ""),
		ReadPort:   getEnv("DB_READ_PORT", getEnv("DB_PORT", "5432")),
		ReadUser:   getEnv("DB_READ_USER", getEnv("DB_USER", "erp_user")),
		ReadPass:   getEnv("DB_READ_PASSWORD", getEnv("DB_PASSWORD", "change_me")),
		ReadDBName: getEnv("DB_READ_NAME", getEnv("DB_NAME", "textile_erp")),
	}
}

func Connect(cfg Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
	)

	log.Printf("Connecting to PostgreSQL at %s:%s...", cfg.Host, cfg.Port)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("Database connected successfully")
	DB = db
	if cfg.ReadHost != "" {
		readDB, err := connectReadReplica(cfg)
		if err != nil {
			log.Printf("Read replica unavailable: %v", err)
		} else {
			ReadDB = readDB
			log.Printf("Read replica connected successfully at %s:%s", cfg.ReadHost, cfg.ReadPort)
		}
	}
	return db, nil
}

func connectReadReplica(cfg Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.ReadHost, cfg.ReadPort, cfg.ReadUser, cfg.ReadPass, cfg.ReadDBName, cfg.SSLMode,
	)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open read replica: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping read replica: %w", err)
	}
	return db, nil
}

func Close() {
	if DB != nil {
		DB.Close()
		log.Println("Database connection closed")
	}
	if ReadDB != nil {
		ReadDB.Close()
		log.Println("Read replica connection closed")
	}
}

func Reader() *sql.DB {
	if ReadDB != nil {
		return ReadDB
	}
	return DB
}

func RunMigrations(db *sql.DB, migrationsPath string) error {
	log.Println("Running database migrations...")

	files, err := filepath.Glob(filepath.Join(migrationsPath, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("failed to read migration files: %w", err)
	}

	if len(files) == 0 {
		log.Println("No migration files found")
		return nil
	}

	sort.Strings(files)

	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS schema_migrations (
            id SERIAL PRIMARY KEY,
            filename VARCHAR(255) NOT NULL UNIQUE,
            executed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    `)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	for _, file := range files {
		filename := filepath.Base(file)

		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE filename = $1", filename).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if count > 0 {
			log.Printf("  Skipping (already executed): %s", filename)
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		log.Printf("  Executing: %s", filename)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to start transaction: %w", err)
		}

		statements := splitSQLStatements(string(content))
		for _, stmt := range statements {
			stmt = normalizeSQLStatement(stmt)
			if stmt == "" {
				continue
			}

			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to execute migration %s near %q: %w", filename, statementPreview(stmt), err)
			}
		}

		if _, err := tx.Exec("INSERT INTO schema_migrations (filename) VALUES ($1)", filename); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration: %w", err)
		}

		log.Printf("  Completed: %s", filename)
	}

	log.Println("All migrations executed successfully")
	return nil
}

func HealthCheck(db *sql.DB) map[string]interface{} {
	stats := db.Stats()

	result := map[string]interface{}{
		"status":           "ok",
		"open_connections": stats.OpenConnections,
		"in_use":           stats.InUse,
		"idle":             stats.Idle,
		"max_open":         stats.MaxOpenConnections,
	}

	tables := []string{"parties", "items", "production_orders", "settlements", "commission_invoices"}
	counts := make(map[string]int)

	for _, table := range tables {
		var count int
		err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		if err == nil {
			counts[table] = count
		}
	}
	result["table_counts"] = counts

	return result
}

func EnsureFinancialUsers(db *sql.DB) error {
	ctx := context.Background()
	var companyID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO companies (code, name)
		VALUES ('PARGOL', 'پرگل')
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`).Scan(&companyID); err != nil {
		return err
	}
	initialPassword := getEnv("FINANCIAL_ADMIN_PASSWORD", "admin123")
	hash, err := password.Hash(initialPassword)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO financial_users (company_id, username, password_hash, role, is_active)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (username) DO UPDATE SET
			company_id = EXCLUDED.company_id,
			password_hash = EXCLUDED.password_hash,
			role = EXCLUDED.role,
			is_active = EXCLUDED.is_active,
			updated_at = CURRENT_TIMESTAMP
	`, companyID, "admin", hash, "admin", true)
	return err
}

func SeedSampleData(db *sql.DB) error {
	log.Println("Seeding sample data...")

	ctx := context.Background()
	var companyID int64
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM companies WHERE code = 'PARGOL'
		UNION ALL
		SELECT id FROM companies WHERE code = 'default'
		ORDER BY id
		LIMIT 1
	`).Scan(&companyID); err != nil {
		return fmt.Errorf("resolve sample company: %w", err)
	}

	err := WithCompanyTx(ctx, db, companyID, func(tx *sql.Tx) error {
		var branchID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO branches (company_id, code, name, created_by)
			VALUES ($1, 'MAIN', 'شعبه اصلی', 1)
			ON CONFLICT (code) DO UPDATE SET
				company_id = EXCLUDED.company_id,
				name = EXCLUDED.name
			RETURNING id
		`, companyID).Scan(&branchID); err != nil {
			return fmt.Errorf("seed branch: %w", err)
		}

		var customerID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO parties (company_id, code, name, type, national_id, tax_id, mobile, phone, is_active)
			VALUES ($1, 'CUST001', 'Sherkat Nasaji Nemoneh', 'Customer', '1234567890', 'TAX123456', '09120000000', '021-12345678', true)
			ON CONFLICT (code) DO UPDATE SET
				company_id = EXCLUDED.company_id,
				name = EXCLUDED.name
			RETURNING id
		`, companyID).Scan(&customerID); err != nil {
			return fmt.Errorf("seed customer: %w", err)
		}
		log.Printf("  Customer: %d", customerID)

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO customer_credit_profiles (company_id, party_id, credit_limit, credit_days, std_wastage_rate, wastage_responsibility, downtime_rate, base_score, risk_group)
			VALUES ($1, $2, 3000000000, 60, 3.00, 'Contractor', 7000000, 85, 'Low')
			ON CONFLICT (party_id) DO UPDATE SET
				company_id = EXCLUDED.company_id,
				credit_limit = EXCLUDED.credit_limit
		`, companyID, customerID); err != nil {
			return fmt.Errorf("seed credit profile: %w", err)
		}

		var contractorID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO parties (company_id, code, name, type, mobile, is_active)
			VALUES ($1, 'CONT001', 'Bafandeh Maher', 'Contractor', '09130000000', true)
			ON CONFLICT (code) DO UPDATE SET
				company_id = EXCLUDED.company_id,
				name = EXCLUDED.name
			RETURNING id
		`, companyID).Scan(&contractorID); err != nil {
			return fmt.Errorf("seed contractor: %w", err)
		}
		log.Printf("  Contractor: %d", contractorID)

		items := []struct {
			code     string
			name     string
			itemType string
		}{
			{"YARN-POLY", "Nakh Polyester", "Yarn"},
			{"YARN-COTT", "Nakh Panbeh", "Yarn"},
			{"FAB-POLY", "Parcheh Polyester", "Fabric"},
			{"FAB-COTT", "Parcheh Panbeh", "Fabric"},
			{"WASTE", "Zayeat", "Waste"},
		}

		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO items (company_id, code, name, type, is_inventory, is_active)
				VALUES ($1, $2, $3, $4, true, true)
				ON CONFLICT (code) DO UPDATE SET
					company_id = EXCLUDED.company_id,
					name = EXCLUDED.name
			`, companyID, item.code, item.name, item.itemType); err != nil {
				return fmt.Errorf("seed item %s: %w", item.code, err)
			}
		}
		log.Println("  Items: 5 sample items")

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO warehouses (company_id, branch_id, code, name, type, is_active)
			VALUES ($1, $2, 'WH-MAIN', 'Anbar Asli', 'Owned', true)
			ON CONFLICT (code) DO UPDATE SET
				company_id = EXCLUDED.company_id,
				branch_id = EXCLUDED.branch_id,
				name = EXCLUDED.name,
				type = EXCLUDED.type
		`, companyID, branchID); err != nil {
			return fmt.Errorf("seed warehouse: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO machines (company_id, branch_id, code, name, type, base_downtime_rate, is_active)
			VALUES ($1, $2, 'MCH-001', 'Dastgah Bafandegi 1', 'Weaving', 7000000, true)
			ON CONFLICT (company_id, code) DO UPDATE SET
				branch_id = EXCLUDED.branch_id,
				name = EXCLUDED.name,
				type = EXCLUDED.type,
				base_downtime_rate = EXCLUDED.base_downtime_rate,
				is_active = EXCLUDED.is_active
		`, companyID, branchID); err != nil {
			return fmt.Errorf("seed machine: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	log.Println("Sample data seeded successfully")
	return nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func splitSQLStatements(sqlText string) []string {
	var statements []string
	var current strings.Builder
	var dollarTag string
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(sqlText); i++ {
		ch := sqlText[i]
		current.WriteByte(ch)

		if dollarTag != "" {
			if strings.HasPrefix(sqlText[i:], dollarTag) {
				for j := 1; j < len(dollarTag); j++ {
					i++
					current.WriteByte(sqlText[i])
				}
				dollarTag = ""
			}
			continue
		}

		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}
		if inSingleQuote || inDoubleQuote {
			continue
		}
		if ch == '$' {
			if tag, ok := readDollarTag(sqlText[i:]); ok {
				dollarTag = tag
				for j := 1; j < len(tag); j++ {
					i++
					current.WriteByte(sqlText[i])
				}
			}
			continue
		}
		if ch == ';' {
			statements = append(statements, current.String())
			current.Reset()
		}
	}

	if strings.TrimSpace(current.String()) != "" {
		statements = append(statements, current.String())
	}
	return statements
}

func readDollarTag(text string) (string, bool) {
	if len(text) < 2 || text[0] != '$' {
		return "", false
	}
	for i := 1; i < len(text); i++ {
		if text[i] == '$' {
			return text[:i+1], true
		}
		if !(text[i] == '_' || text[i] >= 'a' && text[i] <= 'z' || text[i] >= 'A' && text[i] <= 'Z' || text[i] >= '0' && text[i] <= '9') {
			return "", false
		}
	}
	return "", false
}

func normalizeSQLStatement(stmt string) string {
	stmt = strings.TrimPrefix(stmt, "\ufeff")
	var lines []string
	for _, line := range strings.Split(stmt, "\n") {
		line = strings.TrimPrefix(line, "\ufeff")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func statementPreview(stmt string) string {
	stmt = strings.Join(strings.Fields(stmt), " ")
	if len(stmt) > 180 {
		return stmt[:180] + "..."
	}
	return stmt
}
