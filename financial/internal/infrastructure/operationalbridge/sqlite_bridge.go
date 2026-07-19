package operationalbridge

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type Bridge struct {
	db      *sql.DB
	conn    *sql.Conn
	dialect string
}

type LookupRow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type OperationalOutInvoice struct {
	ID           int64   `json:"id_f_khor"`
	Date         string  `json:"tarikh_f_khor"`
	Number       string  `json:"shom_f_khor"`
	CustomerName string  `json:"mosh_f_khor"`
	KalaName     string  `json:"kala_name"`
	PieceCount   int64   `json:"piece_count"`
	Meter        float64 `json:"metr_salon"`
	Weight       float64 `json:"w_salon"`
}

type YarnTxn struct {
	ID           int64   `json:"id"`
	Date         string  `json:"date"`
	DocNo        string  `json:"doc_no"`
	CustomerName string  `json:"customer_name"`
	YarnName     string  `json:"yarn_name"`
	Weight       float64 `json:"weight"`
	Type         string  `json:"type"`
}

type ExpenseRow struct {
	ID          int64   `json:"id"`
	Date        string  `json:"date"`
	Title       string  `json:"title"`
	Operator    string  `json:"operator_name"`
	Weaver      string  `json:"weaver_name"`
	Amount      float64 `json:"amount"`
	Description string  `json:"description"`
	DocNo       string  `json:"doc_no"`
}

type MiscIncomingRow struct {
	ID           int64  `json:"id"`
	Date         string `json:"date"`
	Operation    string `json:"operation_type"`
	ItemName     string `json:"item_name"`
	ItemNo       string `json:"item_no"`
	FromLocation string `json:"from_location"`
	ToLocation   string `json:"to_location"`
	Person       string `json:"person"`
	Status       string `json:"status"`
	Description  string `json:"description"`
	ReturnDate   string `json:"return_date"`
}

type SparePartStockRow struct {
	ID          int64   `json:"id"`
	Date        string  `json:"date"`
	PartName    string  `json:"part_name"`
	PartNumber  string  `json:"part_number"`
	Quantity    float64 `json:"quantity"`
	Condition   string  `json:"condition_status"`
	VendorName  string  `json:"vendor_name"`
	Description string  `json:"description"`
}

type ChelleIncomingRow struct {
	ID           int64   `json:"id"`
	Date         string  `json:"date"`
	DocNo        string  `json:"doc_no"`
	YarnName     string  `json:"yarn_name"`
	Weight       float64 `json:"weight"`
	Warper       string  `json:"warper"`
	CustomerName string  `json:"customer_name"`
	Hambaft      string  `json:"hambaft"`
	Navard       string  `json:"navard"`
	Machine      string  `json:"machine"`
}

func NewFromEnv() (*Bridge, error) {
	if dsn := strings.TrimSpace(os.Getenv("OPERATIONAL_DATABASE_URL")); dsn != "" {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, err
		}
		if err = db.Ping(); err != nil {
			_ = db.Close()
			return nil, err
		}
		return &Bridge{db: db, dialect: "postgres"}, nil
	}
	driver := strings.ToLower(strings.TrimSpace(getEnv("OPERATIONAL_DB_DRIVER", "postgres")))
	if driver == "postgres" || driver == "pg" || driver == "postgresql" {
		host := getEnv("DB_HOST", "localhost")
		port := getEnv("DB_PORT", "5432")
		user := getEnv("DB_USER", "erp_user")
		password := getEnv("DB_PASSWORD", "change_me")
		dbName := getEnv("DB_NAME", "textile_erp")
		sslMode := getEnv("DB_SSLMODE", "disable")
		dsn := "host=" + host + " port=" + port + " user=" + user + " password=" + password + " dbname=" + dbName + " sslmode=" + sslMode
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, err
		}
		if err = db.Ping(); err != nil {
			_ = db.Close()
			return nil, err
		}
		return &Bridge{db: db, dialect: "postgres"}, nil
	}
	path := strings.TrimSpace(os.Getenv("OPERATIONAL_SQLITE_PATH"))
	if path == "" {
		path = strings.TrimSpace(os.Getenv("OPERATIONAL_DB_PATH"))
	}
	if path == "" {
		path = "F:/project/12/deploy_clean/database.db"
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return &Bridge{db: db, dialect: "sqlite"}, nil
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (b *Bridge) query(q string, args ...any) (*sql.Rows, error) {
	if b.conn != nil {
		return b.conn.QueryContext(context.Background(), rebind(b.dialect, q), args...)
	}
	return b.db.Query(rebind(b.dialect, q), args...)
}

func (b *Bridge) ForCompany(ctx context.Context, companyID int64) (*Bridge, func(), error) {
	if b == nil || b.db == nil || companyID <= 0 {
		return nil, func() {}, sql.ErrConnDone
	}
	clone := *b
	if b.dialect != "postgres" {
		return &clone, func() {}, nil
	}
	conn, err := b.db.Conn(ctx)
	if err != nil {
		return nil, func() {}, err
	}
	if _, err := conn.ExecContext(ctx, `SELECT set_config('app.company_id', $1, false)`, strconv.FormatInt(companyID, 10)); err != nil {
		_ = conn.Close()
		return nil, func() {}, err
	}
	clone.conn = conn
	return &clone, func() {
		_, _ = conn.ExecContext(context.Background(), `RESET app.company_id`)
		_ = conn.Close()
	}, nil
}

func rebind(dialect, q string) string {
	if dialect != "postgres" {
		return q
	}
	var out strings.Builder
	out.Grow(len(q) + 8)
	inSingle := false
	idx := 1
	for i := 0; i < len(q); i++ {
		ch := q[i]
		if ch == '\'' {
			inSingle = !inSingle
			out.WriteByte(ch)
			continue
		}
		if ch == '?' && !inSingle {
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(idx))
			idx++
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func (b *Bridge) Close() error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Close()
}

func (b *Bridge) Customers() ([]LookupRow, error) {
	return b.lookup(`SELECT id_mosh_name, COALESCE(name_mosh,'') FROM mosh_name ORDER BY name_mosh`)
}

func (b *Bridge) KalaItems() ([]LookupRow, error) {
	return b.lookup(`SELECT id_kala_name, COALESCE(name_kala_name,'') FROM kala_name ORDER BY name_kala_name`)
}

func (b *Bridge) YarnItems() ([]LookupRow, error) {
	return b.lookup(`SELECT id_nakh_name, COALESCE(name_nakh_name,'') FROM nakh_name ORDER BY name_nakh_name`)
}

func (b *Bridge) lookup(q string) ([]LookupRow, error) {
	rows, err := b.query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]LookupRow, 0, 200)
	for rows.Next() {
		var r LookupRow
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *Bridge) OutInvoices(limit int) ([]OperationalOutInvoice, error) {
	if limit <= 0 {
		limit = 300
	}
	rows, err := b.query(`
		SELECT
			MIN(f.id_f_khor) AS id_f_khor,
			MIN(COALESCE(f.tarikh_f_khor,'')) AS tarikh_f_khor,
			TRIM(COALESCE(f.shom_f_khor,'')) AS shom_f_khor,
			MIN(COALESCE(f.mosh_f_khor,'')) AS mosh_f_khor,
			MIN(COALESCE(NULLIF(f.kala_name_f_khor,''), s.kala_salon, '')) AS kala_name,
			COUNT(*) AS piece_count,
			COALESCE(SUM(s.metr_salon),0) AS metr_salon,
			COALESCE(SUM(s.w_salon),0) AS w_salon
		FROM f_khor f
		LEFT JOIN salon s ON CAST(s.id_salon AS TEXT)=CAST(f.taghe_cod_f_khor AS TEXT)
		WHERE TRIM(COALESCE(f.shom_f_khor,'')) <> ''
		GROUP BY TRIM(COALESCE(f.shom_f_khor,''))
		ORDER BY MIN(f.id_f_khor) DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OperationalOutInvoice, 0, limit)
	for rows.Next() {
		var r OperationalOutInvoice
		if err := rows.Scan(&r.ID, &r.Date, &r.Number, &r.CustomerName, &r.KalaName, &r.PieceCount, &r.Meter, &r.Weight); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *Bridge) YarnIncoming(limit int) ([]YarnTxn, error) {
	return b.yarnTxns("nakh_vor", "id_nakh_vor", "tarikh_nakh_vor", "hambaft_nakh_vor", "moshname_nakh_vor", "nakh_name_nakh_vor", "w_vor_nakh_vor", "incoming", limit)
}

func (b *Bridge) YarnOutgoing(limit int) ([]YarnTxn, error) {
	return b.yarnTxns("nakh_khor", "id_nakh_khor", "tarikh_nakh_khor", "hambaft_nakh_khor", "moshname_nakh_khor", "nakh_name_nakh_khor", "w_vor_nakh_khor", "outgoing", limit)
}

func (b *Bridge) yarnTxns(table, idCol, dateCol, docCol, customerCol, yarnCol, weightCol, typ string, limit int) ([]YarnTxn, error) {
	if limit <= 0 {
		limit = 300
	}
	q := "SELECT " + idCol + ", COALESCE(" + dateCol + ",''), COALESCE(" + docCol + ",''), COALESCE(" + customerCol + ",''), COALESCE(" + yarnCol + ",''), COALESCE(" + weightCol + ",0) FROM " + table + " ORDER BY " + idCol + " DESC LIMIT ?"
	rows, err := b.query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]YarnTxn, 0, limit)
	for rows.Next() {
		var r YarnTxn
		if err := rows.Scan(&r.ID, &r.Date, &r.DocNo, &r.CustomerName, &r.YarnName, &r.Weight); err != nil {
			return nil, err
		}
		r.Type = typ
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *Bridge) Expenses(limit int) ([]ExpenseRow, error) {
	if limit <= 0 {
		limit = 300
	}
	rows, err := b.query(`
		SELECT id_h_rozmare, COALESCE(tarikh_h_rozmare,''), COALESCE(onvan_hazine,''), COALESCE(operator_name,''),
		       COALESCE(weaver_name,''), COALESCE(mablagh_h_rozmare,0), COALESCE(tozih_h_rozmare,''), COALESCE(shomare_sanad,'')
		FROM h_rozmare
		ORDER BY id_h_rozmare DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ExpenseRow, 0, limit)
	for rows.Next() {
		var r ExpenseRow
		if err := rows.Scan(&r.ID, &r.Date, &r.Title, &r.Operator, &r.Weaver, &r.Amount, &r.Description, &r.DocNo); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *Bridge) MiscIncoming(limit int) ([]MiscIncomingRow, error) {
	if limit <= 0 {
		limit = 300
	}
	if !b.tableExists("v_kh_moto") {
		return []MiscIncomingRow{}, nil
	}
	rows, err := b.query(`
		SELECT id, COALESCE(tarikh_v_kh_moto,''), COALESCE(operation_type,''), COALESCE(name_kala,''),
		       COALESCE(shomare_kala,''), COALESCE(from_location,''), COALESCE(to_location,''),
		       COALESCE(person,''), COALESCE(status,''), COALESCE(tozih_v_kh_moto,''), COALESCE(tarikh_bazgasht,'')
		FROM v_kh_moto
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MiscIncomingRow, 0, limit)
	for rows.Next() {
		var r MiscIncomingRow
		if err := rows.Scan(&r.ID, &r.Date, &r.Operation, &r.ItemName, &r.ItemNo, &r.FromLocation, &r.ToLocation, &r.Person, &r.Status, &r.Description, &r.ReturnDate); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *Bridge) tableExists(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	var n int
	var err error
	if b.dialect == "postgres" {
		err = b.db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1`, name).Scan(&n)
	} else {
		err = b.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	}
	return err == nil && n > 0
}

func (b *Bridge) SparePartsInventory(limit int) ([]SparePartStockRow, error) {
	if limit <= 0 {
		limit = 300
	}
	rows, err := b.query(`
		SELECT spi.id_spare_inventory,
		       COALESCE(spi.updated_at, spi.created_at, '') AS date,
		       COALESCE(sp.name_spare_part, spi.part_name, '') AS part_name,
		       COALESCE(spi.part_number, sp.part_number_spare_part, '') AS part_number,
		       COALESCE(spi.quantity, 0) AS quantity,
		       COALESCE(spi.condition_status, '') AS condition_status,
		       COALESCE(spi.vendor_name, '') AS vendor_name,
		       COALESCE(spi.description, '') AS description
		FROM spare_parts_inventory spi
		LEFT JOIN spare_part sp ON sp.id_spare_part = spi.spare_part_id
		ORDER BY spi.id_spare_inventory DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SparePartStockRow, 0, limit)
	for rows.Next() {
		var r SparePartStockRow
		if err := rows.Scan(&r.ID, &r.Date, &r.PartName, &r.PartNumber, &r.Quantity, &r.Condition, &r.VendorName, &r.Description); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (b *Bridge) ChelleIncoming(limit int) ([]ChelleIncomingRow, error) {
	if limit <= 0 {
		limit = 300
	}
	rows, err := b.query(`
		SELECT id_chelle,
		       COALESCE(tarikh_chelle, '') AS date,
		       COALESCE(shom_chelle, '') AS doc_no,
		       COALESCE(nakh_chelle, '') AS yarn_name,
		       COALESCE(w_chelle, 0) AS weight,
		       COALESCE(pich_chelle, '') AS warper,
		       COALESCE(mosh_chelle, '') AS customer_name,
		       COALESCE(hambaft_chelle, '') AS hambaft,
		       COALESCE(codnavard_chelle, '') AS navard,
		       COALESCE(machin_chelle, '') AS machine
		FROM chelle
		ORDER BY id_chelle DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChelleIncomingRow, 0, limit)
	for rows.Next() {
		var r ChelleIncomingRow
		if err := rows.Scan(&r.ID, &r.Date, &r.DocNo, &r.YarnName, &r.Weight, &r.Warper, &r.CustomerName, &r.Hambaft, &r.Navard, &r.Machine); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
