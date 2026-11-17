package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/presentation/handler"
)

func main() {
	company := flag.Int64("company", 0, "explicit company id")
	apply := flag.Bool("apply", false, "apply the audited plan atomically")
	expected := flag.String("expect-checksum", "", "workspace checksum from read-only plan")
	sourceFile := flag.String("source-file", "", "owner-verified HesabYar Excel XML file")
	sourceCurrency := flag.String("source-currency", "", "explicit owner-confirmed source unit (IRR)")
	restore := flag.String("restore-audit-ids", "", "comma-separated owner-approved external ids absent from Excel")
	flag.Parse()
	if *sourceFile == "" {
		log.Fatal("source evidence is required; automatic legacy source-unit inference is disabled")
	}
	raw, err := os.ReadFile(*sourceFile)
	if err != nil {
		log.Fatal(err)
	}
	var restoreIDs []string
	for _, id := range strings.Split(*restore, ",") {
		if id = strings.TrimSpace(id); id != "" {
			restoreIDs = append(restoreIDs, id)
		}
	}
	evidence, err := handler.ParseRialSourceExcel(raw, *sourceCurrency, restoreIDs)
	if err != nil {
		log.Fatal(err)
	}
	db, err := postgres.Connect(postgres.LoadConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := handler.ReconcileCompanyRialSource(ctx, db, *company, evidence, *apply, *expected)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		log.Fatal(err)
	}
}
