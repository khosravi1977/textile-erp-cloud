package operationalbridge

import (
	"database/sql"
	"testing"
)

func TestYarnIncomingAllReturnsBeyondListLimit(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE nakh_vor (id_nakh_vor INTEGER, tarikh_nakh_vor TEXT, hambaft_nakh_vor TEXT, moshname_nakh_vor TEXT, nakh_name_nakh_vor TEXT, w_vor_nakh_vor REAL)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 230; i++ {
		if _, err = db.Exec(`INSERT INTO nakh_vor VALUES (?, '', '', '', '', 1)`, i); err != nil {
			t.Fatal(err)
		}
	}
	b := &Bridge{db: db, dialect: "sqlite"}
	limited, err := b.YarnIncoming(200)
	if err != nil {
		t.Fatal(err)
	}
	all, err := b.YarnIncoming(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 200 || len(all) != 230 || all[len(all)-1].ID != 1 {
		t.Fatalf("limited=%d all=%d oldest=%d", len(limited), len(all), all[len(all)-1].ID)
	}
}
