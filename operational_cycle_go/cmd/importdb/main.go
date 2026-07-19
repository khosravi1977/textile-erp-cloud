package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

type tableInfo struct {
	name    string
	cols    []string
	colSet  map[string]bool
	before  int64
	after   int64
	imports int64
}

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("usage: importdb <destination.db> <source1.db> [source2.db...]")
	}
	destPath := abs(os.Args[1])
	db, err := sql.Open("sqlite", destPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	destTables, err := loadTables(db, "main")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("destination=%s\n", destPath)
	for _, srcArg := range os.Args[2:] {
		srcPath := abs(srcArg)
		if srcPath == destPath {
			continue
		}
		if info, err := os.Stat(srcPath); err != nil || info == nil || info.Size() == 0 {
			fmt.Printf("skip source=%s reason=missing-or-empty\n", srcPath)
			continue
		}
		if err := importSource(db, destTables, srcPath); err != nil {
			log.Fatal(err)
		}
	}
}

func importSource(db *sql.DB, destTables map[string]tableInfo, srcPath string) error {
	alias := "srcdb"
	if _, err := db.Exec("ATTACH DATABASE ? AS "+alias, srcPath); err != nil {
		return fmt.Errorf("attach %s: %w", srcPath, err)
	}
	defer db.Exec("DETACH DATABASE " + alias)

	srcTables, err := loadTables(db, alias)
	if err != nil {
		return err
	}

	fmt.Printf("\nsource=%s\n", srcPath)
	names := make([]string, 0, len(destTables))
	for name := range destTables {
		if _, ok := srcTables[name]; ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		dst := destTables[name]
		src := srcTables[name]
		common := commonColumns(dst, src)
		if len(common) == 0 {
			continue
		}
		before := countRows(db, "main", name)
		cols := quoteList(common)
		query := fmt.Sprintf(
			"INSERT OR IGNORE INTO main.%s (%s) SELECT %s FROM %s.%s",
			quoteIdent(name), cols, cols, alias, quoteIdent(name),
		)
		res, err := db.Exec(query)
		if err != nil {
			fmt.Printf("  %-32s error=%s\n", name, err.Error())
			continue
		}
		changed, _ := res.RowsAffected()
		after := countRows(db, "main", name)
		if changed > 0 {
			fmt.Printf("  %-32s +%-5d total=%d\n", name, changed, after)
		} else if after != before {
			fmt.Printf("  %-32s total=%d\n", name, after)
		}
	}
	return nil
}

func loadTables(db *sql.DB, schema string) (map[string]tableInfo, error) {
	rows, err := db.Query(fmt.Sprintf("SELECT name FROM %s.sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%%' ORDER BY name", schema))
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

	out := map[string]tableInfo{}
	for _, name := range names {
		cols, err := columns(db, schema, name)
		if err != nil {
			return nil, err
		}
		set := map[string]bool{}
		for _, col := range cols {
			set[col] = true
		}
		out[name] = tableInfo{name: name, cols: cols, colSet: set}
	}
	return out, nil
}

func columns(db *sql.DB, schema, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA %s.table_info(%s)", schema, quoteIdent(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := []string{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func commonColumns(dst, src tableInfo) []string {
	out := []string{}
	for _, col := range dst.cols {
		if src.colSet[col] {
			out = append(out, col)
		}
	}
	return out
}

func countRows(db *sql.DB, schema, table string) int64 {
	var n int64
	_ = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", schema, quoteIdent(table))).Scan(&n)
	return n
}

func quoteList(cols []string) string {
	items := make([]string, len(cols))
	for i, col := range cols {
		items[i] = quoteIdent(col)
	}
	return strings.Join(items, ", ")
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func abs(path string) string {
	p, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return filepath.Clean(p)
}
