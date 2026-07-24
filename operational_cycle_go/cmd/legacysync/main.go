package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type tableConfig struct {
	TargetTable      string
	SourceTable      string
	KeyColumns       []string
	ColumnMap        map[string]string
	DerivedColumns   map[string]func(sourceRow, *syncContext) any
	IgnoreSourceCols map[string]bool
	SkipTargetCols   map[string]bool
	PreserveTarget   bool
}

type sqlRunner interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type tableMeta struct {
	Name      string
	Columns   []string
	ColumnSet map[string]bool
	PKColumns []string
}

type sourceRow map[string]any

type foreignKeyViolation struct {
	Table  string `json:"table"`
	RowID  any    `json:"row_id"`
	Parent string `json:"parent_table"`
	FKID   int64  `json:"foreign_key_id"`
}

type tableReport struct {
	Table            string           `json:"table"`
	SourceTable      string           `json:"source_table"`
	TargetTable      string           `json:"target_table"`
	SourceRows       int64            `json:"source_rows"`
	TargetRowsBefore int64            `json:"target_rows_before"`
	TargetRowsAfter  int64            `json:"target_rows_after"`
	ProcessedRows    int64            `json:"processed_rows"`
	UpsertedRows     int64            `json:"upserted_rows"`
	SkippedRows      int64            `json:"skipped_rows"`
	KeyColumns       []string         `json:"key_columns"`
	ColumnMappings   []string         `json:"column_mappings"`
	SourceOnlyCols   []string         `json:"source_only_cols"`
	TargetOnlyCols   []string         `json:"target_only_cols"`
	Warnings         []string         `json:"warnings,omitempty"`
	SkipReasons      map[string]int64 `json:"skip_reasons,omitempty"`
}

type runReport struct {
	StartedAt          string        `json:"started_at"`
	FinishedAt         string        `json:"finished_at"`
	SourcePath         string        `json:"source_path"`
	SourceSHA256       string        `json:"source_sha256"`
	SnapshotPath       string        `json:"snapshot_path"`
	SnapshotSHA256     string        `json:"snapshot_sha256"`
	SourceIntegrity    string        `json:"source_integrity"`
	SourceFKViolations int           `json:"source_foreign_key_violations"`
	TargetDriver       string        `json:"target_driver"`
	TargetLabel        string        `json:"target_label"`
	TargetSchema       string        `json:"target_schema"`
	TargetCompanyID    int64         `json:"target_company_id"`
	DryRun             bool          `json:"dry_run"`
	Committed          bool          `json:"committed"`
	SchemaBefore       string        `json:"schema_before_sha256"`
	SchemaAfter        string        `json:"schema_after_sha256"`
	AdminPreserved     bool          `json:"admin_preserved"`
	MenusPreserved     bool          `json:"menus_preserved"`
	Tables             []tableReport `json:"tables"`
	SkippedTables      []string      `json:"skipped_tables"`
	ArchivedTables     []string      `json:"archived_tables"`
	Error              string        `json:"error,omitempty"`
}

type syncContext struct {
	sourceDB       *sql.DB
	targetDB       sqlRunner
	targetDialect  string
	companyID      int64
	salonKalaByTag map[string]string
	adminUserID    int64
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	log.SetOutput(os.Stdout)
	if err := run(); err != nil {
		log.Printf("migration failed: %v", err)
		os.Exit(1)
	}
}

func run() (runErr error) {
	var sourcePath string
	var reportDir string
	var targetSchema string
	var dryRun bool
	flag.StringVar(&sourcePath, "source", "", "path to the legacy sqlite database")
	flag.StringVar(&reportDir, "report-dir", "", "directory for sync reports and archived legacy-only data")
	flag.StringVar(&targetSchema, "target-schema", env("OPERATIONAL_SCHEMA", "public"), "PostgreSQL tenant schema to import into")
	flag.BoolVar(&dryRun, "dry-run", false, "inspect and validate without writing to the target database")
	flag.Parse()

	if strings.TrimSpace(sourcePath) == "" {
		return errors.New("missing required --source")
	}

	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absSource); err != nil {
		return fmt.Errorf("source db not found: %w", err)
	}

	reportDir = strings.TrimSpace(reportDir)
	if reportDir == "" {
		reportDir = filepath.Join("legacy_sync_reports", time.Now().Format("20060102_150405"))
	}
	reportDir, err = filepath.Abs(reportDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return err
	}

	report := &runReport{
		StartedAt:  time.Now().Format(time.RFC3339),
		SourcePath: absSource,
		DryRun:     dryRun,
	}
	reportPath := filepath.Join(reportDir, "summary.json")
	defer func() {
		report.FinishedAt = time.Now().Format(time.RFC3339)
		if runErr != nil {
			report.Error = runErr.Error()
		}
		if err := writeJSONFile(reportPath, report); err != nil {
			log.Printf("could not write report: %v", err)
		}
	}()

	report.SourceSHA256, err = fileSHA256(absSource)
	if err != nil {
		return fmt.Errorf("hash source database: %w", err)
	}

	snapshotPath := filepath.Join(reportDir, "source_snapshot.db")
	if err := createSQLiteSnapshot(absSource, snapshotPath); err != nil {
		return fmt.Errorf("create consistent source snapshot: %w", err)
	}
	report.SnapshotPath = snapshotPath
	report.SnapshotSHA256, err = fileSHA256(snapshotPath)
	if err != nil {
		return fmt.Errorf("hash source snapshot: %w", err)
	}

	sourceDB, err := sql.Open("sqlite", snapshotPath)
	if err != nil {
		return err
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)
	if err := sourceDB.Ping(); err != nil {
		return err
	}
	report.SourceIntegrity, err = sqliteIntegrity(sourceDB)
	if err != nil {
		return fmt.Errorf("source integrity check: %w", err)
	}
	if !strings.EqualFold(report.SourceIntegrity, "ok") {
		return fmt.Errorf("source snapshot integrity check failed: %s", report.SourceIntegrity)
	}
	fkViolations, err := sqliteForeignKeyViolations(sourceDB)
	if err != nil {
		return fmt.Errorf("source foreign-key check: %w", err)
	}
	report.SourceFKViolations = len(fkViolations)
	if len(fkViolations) > 0 {
		if err := writeJSONFile(filepath.Join(reportDir, "source_foreign_key_violations.json"), fkViolations); err != nil {
			return err
		}
	}
	log.Printf("legacy snapshot ready: %s", snapshotPath)

	targetDB, targetDialect, targetLabel, err := openOperationalDB()
	if err != nil {
		return err
	}
	defer targetDB.Close()
	targetDB.SetMaxOpenConns(1)
	companyID, err := envPositiveInt64("OPERATIONAL_COMPANY_ID", 1)
	if err != nil {
		return err
	}
	report.TargetDriver = targetDialect
	report.TargetLabel = targetLabel
	report.TargetSchema = targetSchema
	report.TargetCompanyID = companyID
	log.Printf("target connected: %s (%s)", targetLabel, targetDialect)

	sourceTables, err := loadTableMeta(sourceDB, "sqlite")
	if err != nil {
		return err
	}
	log.Printf("loaded source schema: %d tables", len(sourceTables))

	tx, err := targetDB.Begin()
	if err != nil {
		return fmt.Errorf("begin target transaction: %w", err)
	}
	txFinished := false
	defer func() {
		if !txFinished {
			_ = tx.Rollback()
		}
	}()
	if targetDialect == "postgres" {
		targetSchema = strings.TrimSpace(targetSchema)
		if !validPostgresIdentifier(targetSchema) {
			return fmt.Errorf("invalid PostgreSQL target schema: %q", targetSchema)
		}
		report.TargetSchema = targetSchema
		searchPath := quoteIdent(targetSchema) + ", public"
		if _, err := tx.Exec(`SELECT set_config('search_path', $1, true)`, searchPath); err != nil {
			return fmt.Errorf("select tenant schema %s: %w", targetSchema, err)
		}
		if _, err := tx.Exec(`SELECT set_config('app.company_id', $1, true)`, strconv.FormatInt(companyID, 10)); err != nil {
			return fmt.Errorf("select online company %d: %w", companyID, err)
		}
		var activeSchema string
		if err := tx.QueryRow(`SELECT current_schema()`).Scan(&activeSchema); err != nil {
			return fmt.Errorf("verify tenant schema %s: %w", targetSchema, err)
		}
		if activeSchema != targetSchema {
			return fmt.Errorf("tenant schema isolation failed: selected=%s active=%s", targetSchema, activeSchema)
		}
	}

	targetTables, err := loadTableMeta(tx, targetDialect)
	if err != nil {
		return err
	}
	log.Printf("loaded target schema: %d tables", len(targetTables))
	report.SchemaBefore = schemaFingerprint(targetTables)
	adminBefore, adminID, err := adminFingerprint(tx, targetDialect, targetTables["users"], companyID)
	if err != nil {
		return fmt.Errorf("protect target admin: %w", err)
	}
	menusBefore, err := menuFingerprint(tx, targetDialect, targetTables["menu_items"])
	if err != nil {
		return fmt.Errorf("protect target menus: %w", err)
	}

	ctx := &syncContext{
		sourceDB:      sourceDB,
		targetDB:      tx,
		targetDialect: targetDialect,
		companyID:     companyID,
		adminUserID:   adminID,
	}
	ctx.salonKalaByTag, _ = buildSalonKalaMap(sourceDB, sourceTables["salon"])
	log.Printf("prepared source lookups")

	configs := syncConfigs()
	for _, cfg := range configs {
		log.Printf("inspecting table sync: %s", cfg.TargetTable)
		srcMeta, srcOK := sourceTables[cfg.SourceTable]
		dstMeta, dstOK := targetTables[cfg.TargetTable]
		if !srcOK || !dstOK {
			if !srcOK {
				report.SkippedTables = append(report.SkippedTables, cfg.SourceTable)
			}
			continue
		}
		tableReport, err := syncTable(ctx, cfg, srcMeta, dstMeta, dryRun)
		if err != nil {
			return fmt.Errorf("sync %s: %w", cfg.TargetTable, err)
		}
		report.Tables = append(report.Tables, tableReport)
	}

	archived, skipped, err := archiveLegacyOnlyTables(sourceDB, sourceTables, targetTables, configs, reportDir)
	if err != nil {
		return err
	}
	log.Printf("archived legacy-only tables: %d", len(archived))
	report.ArchivedTables = archived
	report.SkippedTables = appendUnique(report.SkippedTables, skipped...)

	targetTablesAfter, err := loadTableMeta(tx, targetDialect)
	if err != nil {
		return err
	}
	report.SchemaAfter = schemaFingerprint(targetTablesAfter)
	adminAfter, _, err := adminFingerprint(tx, targetDialect, targetTablesAfter["users"], companyID)
	if err != nil {
		return fmt.Errorf("verify target admin: %w", err)
	}
	menusAfter, err := menuFingerprint(tx, targetDialect, targetTablesAfter["menu_items"])
	if err != nil {
		return fmt.Errorf("verify target menus: %w", err)
	}
	report.AdminPreserved = adminBefore == adminAfter
	report.MenusPreserved = menusBefore == menusAfter
	if report.SchemaBefore != report.SchemaAfter {
		return errors.New("target schema changed during migration; transaction was rolled back")
	}
	if !report.AdminPreserved {
		return errors.New("target admin account changed during migration; transaction was rolled back")
	}
	if !report.MenusPreserved {
		return errors.New("target menu definitions changed during migration; transaction was rolled back")
	}

	if dryRun {
		if err := tx.Rollback(); err != nil {
			return fmt.Errorf("rollback dry run: %w", err)
		}
		txFinished = true
	} else {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration: %w", err)
		}
		txFinished = true
		report.Committed = true
	}

	for _, t := range report.Tables {
		fmt.Printf("%-24s source=%-4d processed=%-4d upserted=%-4d\n", t.TargetTable, t.SourceRows, t.ProcessedRows, t.UpsertedRows)
		if len(t.Warnings) > 0 {
			for _, warning := range t.Warnings {
				fmt.Printf("  warning: %s\n", warning)
			}
		}
	}
	fmt.Printf("report=%s\n", reportPath)
	if len(report.ArchivedTables) > 0 {
		fmt.Printf("archived_legacy_tables=%s\n", strings.Join(report.ArchivedTables, ", "))
	}
	return nil
}

func syncConfigs() []tableConfig {
	return []tableConfig{
		newConfig("mosh_name", "id_mosh_name"),
		newConfig("nakh_name", "id_nakh_name"),
		newConfig("kala_name", "id_kala_name"),
		newConfig("chellepich", "id_chellepich"),
		newConfig("kod_navard", "id_kod_navard"),
		newConfig("gerezan", "id_gerezan"),
		newConfig("nakh_vor", "id_nakh_vor"),
		newConfig("nakh_khor", "id_nakh_khor"),
		newConfig("chelle", "id_chelle"),
		newConfig("gere", "id_gere"),
		newConfig("nakh_salon", "id_nakh_salon"),
		newConfig("salon", "id_salon"),
		newConfig("machine_consumption", "id_consumption"),
		newConfig("machine_formul", "id_formul"),
		{
			TargetTable: "f_khor",
			SourceTable: "f_khor",
			KeyColumns:  []string{"id_f_khor"},
			ColumnMap: map[string]string{
				"id_f_khor":        "id_f_khor",
				"tarikh_f_khor":    "tarikh_f_khor",
				"shom_f_khor":      "shom_f_khor",
				"taghe_cod_f_khor": "taghe_cod_f_khor",
				"mosh_f_khor":      "mosh_f_khor",
				"shomare_sanad":    "shomare_sanad",
			},
			DerivedColumns: map[string]func(sourceRow, *syncContext) any{
				"kala_name_f_khor": func(row sourceRow, ctx *syncContext) any {
					if v := rowText(row, "kala_name_f_khor"); v != "" {
						return v
					}
					tag := rowText(row, "taghe_cod_f_khor")
					if tag == "" {
						return nil
					}
					if kala := strings.TrimSpace(ctx.salonKalaByTag[tag]); kala != "" {
						return kala
					}
					return nil
				},
			},
			IgnoreSourceCols: map[string]bool{
				"barcode_code": true,
			},
		},
		newConfig("hazine", "id_hazine"),
		newConfig("operator_name", "id_operator"),
		newConfig("driver_name", "id_driver"),
		newConfig("weaver_name", "id_weaver"),
		newConfig("h_rozmare", "id_h_rozmare"),
		newConfig("service_type", "id_service_type"),
		newConfig("spare_part", "id_spare_part"),
		newConfig("spare_parts_inventory", "id_spare_inventory"),
		newConfig("machinery_service", "id_machinery_service"),
		newConfig("v_kh_moto", "id"),
		{
			TargetTable:    "users",
			SourceTable:    "users",
			KeyColumns:     []string{"id_user"},
			PreserveTarget: true,
		},
		{
			TargetTable: "menu_items",
			SourceTable: "menu_items",
			KeyColumns:  []string{"menu_key"},
			ColumnMap: map[string]string{
				"menu_key":      "menu_key",
				"menu_name":     "menu_name",
				"path":          "menu_url",
				"icon":          "menu_icon",
				"is_restricted": "is_restricted",
				"sort_order":    "sort_order",
			},
			IgnoreSourceCols: map[string]bool{
				"id_menu": true,
			},
			SkipTargetCols: map[string]bool{
				"id_menu": true,
			},
			PreserveTarget: true,
		},
		{
			TargetTable: "user_menu_access",
			SourceTable: "user_menu_access",
			KeyColumns:  []string{"user_id", "menu_key"},
			ColumnMap: map[string]string{
				"id_access":  "id_access",
				"user_id":    "user_id",
				"menu_key":   "menu_key",
				"has_access": "has_access",
				"granted_by": "granted_by",
				"granted_at": "granted_at",
			},
			SkipTargetCols: map[string]bool{
				"id_access": true,
			},
			PreserveTarget: true,
		},
	}
}

func newConfig(table string, key string) tableConfig {
	return tableConfig{
		TargetTable: table,
		SourceTable: table,
		KeyColumns:  []string{key},
		ColumnMap:   map[string]string{},
	}
}

func syncTable(ctx *syncContext, cfg tableConfig, srcMeta, dstMeta tableMeta, dryRun bool) (tableReport, error) {
	rep := tableReport{
		Table:            cfg.TargetTable,
		SourceTable:      cfg.SourceTable,
		TargetTable:      cfg.TargetTable,
		KeyColumns:       append([]string{}, cfg.KeyColumns...),
		SourceRows:       countRows(ctx.sourceDB, srcMeta.Name),
		TargetRowsBefore: countTargetRows(ctx, dstMeta),
		SkipReasons:      map[string]int64{},
	}

	if len(cfg.KeyColumns) == 0 {
		cfg.KeyColumns = append([]string{}, dstMeta.PKColumns...)
		rep.KeyColumns = append([]string{}, cfg.KeyColumns...)
	}
	if len(cfg.KeyColumns) == 0 {
		return rep, fmt.Errorf("table %s has no merge key", cfg.TargetTable)
	}

	targetCols := []string{}
	sourceColsUsed := map[string]bool{}
	for _, col := range dstMeta.Columns {
		if cfg.SkipTargetCols != nil && cfg.SkipTargetCols[col] {
			continue
		}
		if col == "company_id" {
			targetCols = append(targetCols, col)
			rep.ColumnMappings = append(rep.ColumnMappings, "company_id<=<selected-company>")
			continue
		}
		sourceCol := col
		if mapped, ok := cfg.ColumnMap[col]; ok {
			sourceCol = mapped
		}
		if sourceCol != "" && srcMeta.ColumnSet[sourceCol] {
			targetCols = append(targetCols, col)
			sourceColsUsed[sourceCol] = true
			rep.ColumnMappings = append(rep.ColumnMappings, fmt.Sprintf("%s<=%s", col, sourceCol))
			continue
		}
		if cfg.DerivedColumns != nil && cfg.DerivedColumns[col] != nil {
			targetCols = append(targetCols, col)
			rep.ColumnMappings = append(rep.ColumnMappings, fmt.Sprintf("%s<=<derived>", col))
		}
	}

	rep.SourceOnlyCols = difference(srcMeta.Columns, sourceColsUsed, cfg.IgnoreSourceCols)
	targetColSet := map[string]bool{}
	for _, col := range targetCols {
		targetColSet[col] = true
	}
	rep.TargetOnlyCols = differenceTarget(dstMeta.Columns, targetColSet)

	rows, err := readSourceRows(ctx.sourceDB, srcMeta)
	if err != nil {
		return rep, err
	}
	rep.ProcessedRows = int64(len(rows))

	if cfg.PreserveTarget {
		rep.SkippedRows = int64(len(rows))
		rep.SkipReasons["protected target configuration"] = int64(len(rows))
		rep.TargetRowsAfter = rep.TargetRowsBefore
		return rep, nil
	}
	if len(rows) == 0 || len(targetCols) == 0 {
		rep.TargetRowsAfter = rep.TargetRowsBefore
		return rep, nil
	}

	stmt, err := buildUpsertSQL(ctx.targetDialect, cfg.TargetTable, targetCols, cfg.KeyColumns)
	if err != nil {
		return rep, err
	}

	upserted := int64(0)
	validUserIDs := map[int64]bool{}
	validMenuKeys := map[string]bool{}
	if cfg.TargetTable == "user_menu_access" {
		validUserIDs, err = loadTargetUserIDs(ctx)
		if err != nil {
			return rep, err
		}
		validMenuKeys, err = loadTargetMenuKeys(ctx)
		if err != nil {
			return rep, err
		}
	}
	for _, row := range rows {
		if reason := skipRowReason(ctx, cfg, row, validUserIDs, validMenuKeys); reason != "" {
			rep.SkippedRows++
			rep.SkipReasons[reason]++
			continue
		}
		if dstMeta.ColumnSet["company_id"] {
			if err := ensureCompanySafeConflict(ctx, cfg, row); err != nil {
				return rep, err
			}
		}
		values := make([]any, 0, len(targetCols))
		for _, col := range targetCols {
			if col == "company_id" {
				values = append(values, ctx.companyID)
				continue
			}
			if derive := cfg.DerivedColumns[col]; derive != nil {
				values = append(values, normalizeValue(derive(row, ctx)))
				continue
			}
			sourceCol := col
			if mapped, ok := cfg.ColumnMap[col]; ok {
				sourceCol = mapped
			}
			values = append(values, normalizeValue(row[sourceCol]))
		}
		if _, err := ctx.targetDB.Exec(rebind(ctx.targetDialect, stmt), values...); err != nil {
			return rep, fmt.Errorf("%s: %w", cfg.TargetTable, err)
		}
		upserted++
	}
	rep.UpsertedRows = upserted

	if !dryRun {
		if err := refreshSequence(ctx.targetDB, ctx.targetDialect, cfg.TargetTable, dstMeta); err != nil {
			return rep, fmt.Errorf("sequence refresh: %w", err)
		}
	}
	rep.TargetRowsAfter = countTargetRows(ctx, dstMeta)
	return rep, nil
}

func buildUpsertSQL(dialect, table string, cols, keys []string) (string, error) {
	if len(cols) == 0 {
		return "", errors.New("no columns to insert")
	}
	if len(keys) == 0 {
		return "", errors.New("no conflict keys")
	}
	insertCols := quoteColumns(cols)
	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = "?"
	}
	updateCols := []string{}
	keySet := map[string]bool{}
	for _, key := range keys {
		keySet[key] = true
	}
	for _, col := range cols {
		if !keySet[col] && col != "company_id" {
			updateCols = append(updateCols, quoteIdent(col)+" = EXCLUDED."+quoteIdent(col))
		}
	}
	sqlText := "INSERT INTO " + quoteIdent(table) + " (" + insertCols + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	if len(updateCols) == 0 {
		sqlText += " ON CONFLICT (" + quoteColumns(keys) + ") DO NOTHING"
	} else {
		sqlText += " ON CONFLICT (" + quoteColumns(keys) + ") DO UPDATE SET " + strings.Join(updateCols, ", ")
	}
	if dialect == "postgres" {
		return rebind(dialect, sqlText), nil
	}
	return sqlText, nil
}

func archiveLegacyOnlyTables(sourceDB *sql.DB, sourceTables, targetTables map[string]tableMeta, configs []tableConfig, reportDir string) ([]string, []string, error) {
	mappedTargets := map[string]bool{}
	mappedSources := map[string]bool{}
	for _, cfg := range configs {
		mappedTargets[cfg.TargetTable] = true
		mappedSources[cfg.SourceTable] = true
	}

	archived := []string{}
	skipped := []string{}
	archiveDir := filepath.Join(reportDir, "archived_legacy_tables")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return nil, nil, err
	}
	for name, meta := range sourceTables {
		if mappedSources[name] {
			continue
		}
		if name == "" {
			skipped = append(skipped, name)
			continue
		}
		if targetTables[name].Name != "" && !mappedTargets[name] {
			skipped = append(skipped, name)
			continue
		}
		rows, err := readSourceRows(sourceDB, meta)
		if err != nil {
			return nil, nil, err
		}
		payload := map[string]any{
			"table":   name,
			"columns": meta.Columns,
			"rows":    rows,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return nil, nil, err
		}
		file := filepath.Join(archiveDir, sanitizeFilename(name)+".json")
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return nil, nil, err
		}
		archived = append(archived, name)
	}
	sort.Strings(archived)
	sort.Strings(skipped)
	return archived, skipped, nil
}

func loadTableMeta(db sqlRunner, dialect string) (map[string]tableMeta, error) {
	var query string
	if dialect == "postgres" {
		query = `SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename`
	} else {
		query = `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
	}
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := map[string]tableMeta{}
	for _, name := range names {
		meta, err := describeTable(db, dialect, name)
		if err != nil {
			return nil, err
		}
		out[name] = meta
	}
	return out, nil
}

func describeTable(db sqlRunner, dialect, table string) (tableMeta, error) {
	meta := tableMeta{Name: table, ColumnSet: map[string]bool{}}
	if dialect == "postgres" {
		rows, err := db.Query(`
			SELECT column_name
			FROM information_schema.columns
			WHERE table_schema=current_schema() AND table_name=$1
			ORDER BY ordinal_position`, table)
		if err != nil {
			return meta, err
		}
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				rows.Close()
				return meta, err
			}
			meta.Columns = append(meta.Columns, col)
			meta.ColumnSet[col] = true
		}
		rows.Close()
		pkRows, err := db.Query(`
			SELECT a.attname
			FROM pg_index i
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE i.indrelid = to_regclass(current_schema() || '.' || quote_ident($1)) AND i.indisprimary
			ORDER BY array_position(i.indkey, a.attnum)`, table)
		if err != nil {
			return meta, err
		}
		for pkRows.Next() {
			var col string
			if err := pkRows.Scan(&col); err != nil {
				pkRows.Close()
				return meta, err
			}
			meta.PKColumns = append(meta.PKColumns, col)
		}
		pkRows.Close()
		return meta, nil
	}

	rows, err := db.Query("PRAGMA table_info(" + quoteIdent(table) + ")")
	if err != nil {
		return meta, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return meta, err
		}
		meta.Columns = append(meta.Columns, name)
		meta.ColumnSet[name] = true
		if pk > 0 {
			meta.PKColumns = append(meta.PKColumns, name)
		}
	}
	return meta, rows.Err()
}

func readSourceRows(db *sql.DB, meta tableMeta) ([]sourceRow, error) {
	if len(meta.Columns) == 0 {
		return nil, nil
	}
	rows, err := db.Query("SELECT " + quoteColumns(meta.Columns) + " FROM " + quoteIdent(meta.Name))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]sourceRow, 0, 256)
	for rows.Next() {
		raw := make([]any, len(meta.Columns))
		ptrs := make([]any, len(meta.Columns))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		item := sourceRow{}
		for i, col := range meta.Columns {
			switch value := raw[i].(type) {
			case []byte:
				item[col] = string(value)
			default:
				item[col] = value
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func buildSalonKalaMap(db *sql.DB, meta tableMeta) (map[string]string, error) {
	if meta.Name == "" || !meta.ColumnSet["id_salon"] || !meta.ColumnSet["kala_salon"] {
		return map[string]string{}, nil
	}
	rows, err := db.Query(`SELECT CAST(id_salon AS TEXT), COALESCE(kala_salon,'') FROM salon`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, kala string
		if err := rows.Scan(&id, &kala); err != nil {
			return nil, err
		}
		out[strings.TrimSpace(id)] = strings.TrimSpace(kala)
	}
	return out, rows.Err()
}

func refreshSequence(db sqlRunner, dialect, table string, meta tableMeta) error {
	if dialect != "postgres" || len(meta.PKColumns) != 1 {
		return nil
	}
	pk := meta.PKColumns[0]
	var seq sql.NullString
	if err := db.QueryRow(`SELECT pg_get_serial_sequence($1, $2)`, table, pk).Scan(&seq); err != nil {
		return err
	}
	if !seq.Valid || strings.TrimSpace(seq.String) == "" {
		return nil
	}
	var maxID sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(` + quoteIdent(pk) + `) FROM ` + quoteIdent(table)).Scan(&maxID); err != nil {
		return err
	}
	var lastValue int64
	if err := db.QueryRow(`SELECT last_value FROM ` + quoteQualifiedIdent(seq.String)).Scan(&lastValue); err != nil {
		return err
	}
	nextBase := lastValue
	if maxID.Valid && maxID.Int64 > nextBase {
		nextBase = maxID.Int64
	}
	if nextBase < 1 {
		nextBase = 1
	}
	_, err := db.Exec(`SELECT setval($1, $2, true)`, seq.String, nextBase)
	return err
}

func countRows(db *sql.DB, table string) int64 {
	var count int64
	_ = db.QueryRow("SELECT COUNT(*) FROM " + quoteIdent(table)).Scan(&count)
	return count
}

func difference(cols []string, used map[string]bool, ignored map[string]bool) []string {
	out := []string{}
	for _, col := range cols {
		if ignored != nil && ignored[col] {
			continue
		}
		if !used[col] {
			out = append(out, col)
		}
	}
	sort.Strings(out)
	return out
}

func differenceTarget(cols []string, used map[string]bool) []string {
	out := []string{}
	for _, col := range cols {
		if !used[col] {
			out = append(out, col)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return v
	}
}

func openOperationalDB() (*sql.DB, string, string, error) {
	if dsn := strings.TrimSpace(os.Getenv("OPERATIONAL_DATABASE_URL")); dsn != "" {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, "", "", err
		}
		if err := db.Ping(); err != nil {
			_ = db.Close()
			return nil, "", "", err
		}
		db.SetMaxOpenConns(1)
		return db, "postgres", "PostgreSQL target from OPERATIONAL_DATABASE_URL", nil
	}

	driver := strings.ToLower(strings.TrimSpace(env("OPERATIONAL_DB_DRIVER", "postgres")))
	if driver == "postgres" || driver == "pg" || driver == "postgresql" {
		host := env("DB_HOST", "localhost")
		port := env("DB_PORT", "5432")
		user := env("DB_USER", "erp_user")
		password := env("DB_PASSWORD", "change_me")
		name := env("DB_NAME", "textile_erp")
		sslMode := env("DB_SSLMODE", "disable")
		dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host, port, user, password, name, sslMode)
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, "", "", err
		}
		if err := db.Ping(); err != nil {
			_ = db.Close()
			return nil, "", "", err
		}
		db.SetMaxOpenConns(1)
		return db, "postgres", fmt.Sprintf("postgres://%s@%s:%s/%s", user, host, port, name), nil
	}

	dbPath := env("OPERATIONAL_DB", filepath.Join("..", "operational", "database.db"))
	absDB, _ := filepath.Abs(dbPath)
	db, err := sql.Open("sqlite", absDB)
	if err != nil {
		return nil, "", "", err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, "", "", err
	}
	db.SetMaxOpenConns(1)
	return db, "sqlite", absDB, nil
}

func createSQLiteSnapshot(sourcePath, snapshotPath string) error {
	if same, _ := filepath.Abs(sourcePath); strings.EqualFold(same, snapshotPath) {
		return errors.New("source and snapshot paths must be different")
	}
	if err := os.Remove(snapshotPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	db, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 30000`); err != nil {
		return err
	}
	var quickCheck string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&quickCheck); err != nil {
		return err
	}
	if !strings.EqualFold(quickCheck, "ok") {
		return fmt.Errorf("legacy database quick_check failed: %s", quickCheck)
	}
	if _, err := db.Exec(`VACUUM INTO ?`, snapshotPath); err != nil {
		return err
	}
	return nil
}

func sqliteIntegrity(db *sql.DB) (string, error) {
	rows, err := db.Query(`PRAGMA integrity_check`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	results := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return "", err
		}
		results = append(results, value)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(results, "; "), nil
}

func sqliteForeignKeyViolations(db *sql.DB) ([]foreignKeyViolation, error) {
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []foreignKeyViolation{}
	for rows.Next() {
		var item foreignKeyViolation
		if err := rows.Scan(&item.Table, &item.RowID, &item.Parent, &item.FKID); err != nil {
			return nil, err
		}
		if raw, ok := item.RowID.([]byte); ok {
			item.RowID = string(raw)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(hash.Sum(nil))), nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func schemaFingerprint(tables map[string]tableMeta) string {
	type schemaTable struct {
		Name    string   `json:"name"`
		Columns []string `json:"columns"`
		PK      []string `json:"primary_key"`
	}
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	payload := make([]schemaTable, 0, len(names))
	for _, name := range names {
		meta := tables[name]
		payload = append(payload, schemaTable{
			Name:    name,
			Columns: append([]string{}, meta.Columns...),
			PK:      append([]string{}, meta.PKColumns...),
		})
	}
	return jsonFingerprint(payload)
}

func adminFingerprint(db sqlRunner, dialect string, meta tableMeta, companyID int64) (string, int64, error) {
	if meta.Name == "" || !meta.ColumnSet["id_user"] || !meta.ColumnSet["username"] || !meta.ColumnSet["password_hash"] {
		return "", 0, errors.New("target users table is missing required admin columns")
	}
	columns := append([]string{}, meta.Columns...)
	query := `SELECT ` + quoteColumns(columns) + ` FROM ` + quoteIdent(meta.Name)
	args := []any{}
	if meta.ColumnSet["company_id"] {
		query += ` WHERE company_id=?`
		args = append(args, companyID)
	}
	query += ` ORDER BY id_user`
	rows, err := db.Query(rebind(dialect, query), args...)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	payload := [][]any{}
	var adminID int64
	for rows.Next() {
		values, err := scanAnyRow(rows, len(columns))
		if err != nil {
			return "", 0, err
		}
		payload = append(payload, values)
		if adminID == 0 {
			for i, col := range columns {
				if col == "id_user" {
					adminID, err = anyInt64(values[i])
					if err != nil {
						return "", 0, err
					}
					break
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}
	if len(payload) == 0 {
		return "", 0, errors.New("target user accounts were not found")
	}
	return jsonFingerprint(payload), adminID, nil
}

func menuFingerprint(db sqlRunner, dialect string, meta tableMeta) (string, error) {
	if meta.Name == "" || len(meta.Columns) == 0 {
		return "", errors.New("target menu_items table was not found")
	}
	orderColumn := meta.Columns[0]
	if meta.ColumnSet["id_menu"] {
		orderColumn = "id_menu"
	} else if meta.ColumnSet["menu_key"] {
		orderColumn = "menu_key"
	}
	query := `SELECT ` + quoteColumns(meta.Columns) + ` FROM ` + quoteIdent(meta.Name) + ` ORDER BY ` + quoteIdent(orderColumn)
	rows, err := db.Query(rebind(dialect, query))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	payload := [][]any{}
	for rows.Next() {
		values, err := scanAnyRow(rows, len(meta.Columns))
		if err != nil {
			return "", err
		}
		payload = append(payload, values)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return jsonFingerprint(payload), nil
}

func scanAnyRow(rows *sql.Rows, count int) ([]any, error) {
	values := make([]any, count)
	pointers := make([]any, count)
	for i := range values {
		pointers[i] = &values[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, err
	}
	for i, value := range values {
		if raw, ok := value.([]byte); ok {
			values[i] = string(raw)
		}
	}
	return values, nil
}

func jsonFingerprint(value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

func countTargetRows(ctx *syncContext, meta tableMeta) int64 {
	query := `SELECT COUNT(*) FROM ` + quoteIdent(meta.Name)
	args := []any{}
	if meta.ColumnSet["company_id"] {
		query += ` WHERE company_id=?`
		args = append(args, ctx.companyID)
	}
	var count int64
	if err := ctx.targetDB.QueryRow(rebind(ctx.targetDialect, query), args...).Scan(&count); err != nil {
		return -1
	}
	return count
}

func loadTargetUserIDs(ctx *syncContext) (map[int64]bool, error) {
	meta, err := describeTable(ctx.targetDB, ctx.targetDialect, "users")
	if err != nil {
		return nil, err
	}
	query := `SELECT id_user FROM users`
	args := []any{}
	if meta.ColumnSet["company_id"] {
		query += ` WHERE company_id=?`
		args = append(args, ctx.companyID)
	}
	rows, err := ctx.targetDB.Query(rebind(ctx.targetDialect, query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func loadTargetMenuKeys(ctx *syncContext) (map[string]bool, error) {
	rows, err := ctx.targetDB.Query(`SELECT menu_key FROM menu_items`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key sql.NullString
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if key.Valid {
			out[strings.TrimSpace(key.String)] = true
		}
	}
	return out, rows.Err()
}

func skipRowReason(ctx *syncContext, cfg tableConfig, row sourceRow, validUserIDs map[int64]bool, validMenuKeys map[string]bool) string {
	if cfg.TargetTable == "users" && strings.EqualFold(rowText(row, "username"), "admin") {
		return "protected target admin"
	}
	if cfg.TargetTable != "user_menu_access" {
		return ""
	}
	userID, err := rowInt64(row, "user_id")
	if err != nil {
		return "invalid user_id"
	}
	if userID == ctx.adminUserID {
		return "protected target admin access"
	}
	if !validUserIDs[userID] {
		return "missing target user"
	}
	menuKey := rowText(row, "menu_key")
	if menuKey == "" {
		return "empty menu_key"
	}
	if !validMenuKeys[menuKey] {
		return "menu not present in new version"
	}
	return ""
}

func ensureCompanySafeConflict(ctx *syncContext, cfg tableConfig, row sourceRow) error {
	where := make([]string, 0, len(cfg.KeyColumns))
	values := make([]any, 0, len(cfg.KeyColumns))
	for _, key := range cfg.KeyColumns {
		var value any
		if key == "company_id" {
			value = ctx.companyID
		} else if derive := cfg.DerivedColumns[key]; derive != nil {
			value = derive(row, ctx)
		} else {
			sourceColumn := key
			if mapped, ok := cfg.ColumnMap[key]; ok {
				sourceColumn = mapped
			}
			value = row[sourceColumn]
		}
		if value == nil {
			return fmt.Errorf("merge key %s is NULL", key)
		}
		where = append(where, quoteIdent(key)+`=?`)
		values = append(values, normalizeValue(value))
	}
	query := `SELECT company_id FROM ` + quoteIdent(cfg.TargetTable) + ` WHERE ` + strings.Join(where, ` AND `) + ` LIMIT 1`
	var existingCompanyID int64
	err := ctx.targetDB.QueryRow(rebind(ctx.targetDialect, query), values...).Scan(&existingCompanyID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if existingCompanyID != ctx.companyID {
		return fmt.Errorf("merge key belongs to company %d, not selected company %d", existingCompanyID, ctx.companyID)
	}
	return nil
}

func rowText(row sourceRow, column string) string {
	value, ok := row[column]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []byte:
		return strings.TrimSpace(string(typed))
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func rowInt64(row sourceRow, column string) (int64, error) {
	return anyInt64(row[column])
}

func anyInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case int:
		return int64(typed), nil
	case float64:
		return int64(typed), nil
	case []byte:
		return strconv.ParseInt(strings.TrimSpace(string(typed)), 10, 64)
	case string:
		return strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %v to integer", value)
	}
}

func envPositiveInt64(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func rebind(dialect, q string) string {
	if dialect != "postgres" {
		return q
	}
	q = strings.NewReplacer(
		"datetime('now','localtime')", "CURRENT_TIMESTAMP",
		"INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY",
		"REAL", "DOUBLE PRECISION",
	).Replace(q)
	var b strings.Builder
	inSingle := false
	idx := 1
	for i := 0; i < len(q); i++ {
		ch := q[i]
		if ch == '\'' {
			inSingle = !inSingle
			b.WriteByte(ch)
			continue
		}
		if ch == '?' && !inSingle {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(idx))
			idx++
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func ddl(dialect, stmt string) string {
	if dialect != "postgres" {
		return stmt
	}
	replacer := strings.NewReplacer(
		"INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY",
		"REAL", "DOUBLE PRECISION",
		"DEFAULT (datetime('now','localtime'))", "DEFAULT CURRENT_TIMESTAMP",
		"datetime('now','localtime')", "CURRENT_TIMESTAMP",
	)
	return replacer.Replace(stmt)
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func validPostgresIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '_' || (index > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func quoteQualifiedIdent(name string) string {
	parts := strings.Split(name, ".")
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		quoted = append(quoted, quoteIdent(part))
	}
	return strings.Join(quoted, ".")
}

func quoteColumns(cols []string) string {
	items := make([]string, len(cols))
	for i, col := range cols {
		items[i] = quoteIdent(col)
	}
	return strings.Join(items, ", ")
}

func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer("\\", "_", "/", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(name)
}

func appendUnique(base []string, extra ...string) []string {
	seen := map[string]bool{}
	for _, item := range base {
		seen[item] = true
	}
	for _, item := range extra {
		if item == "" || seen[item] {
			continue
		}
		base = append(base, item)
		seen[item] = true
	}
	sort.Strings(base)
	return base
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
