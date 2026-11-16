package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/presentation/handler"
)

func main() {
	company := flag.Int64("company", 0, "explicit company id")
	apply := flag.Bool("apply", false, "apply the audited plan atomically")
	expected := flag.String("expect-checksum", "", "workspace checksum from read-only plan")
	flag.Parse()
	db, err := postgres.Connect(postgres.LoadConfig())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := handler.MigrateCompanyToRials(ctx, db, *company, *apply, *expected)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		log.Fatal(err)
	}
}
