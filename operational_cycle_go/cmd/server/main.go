package main

import (
	"archive/zip"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type app struct {
	db            *sql.DB
	dialect       string
	dbLabel       string
	defaultSchema string
	portalSecret  string
	sessions      map[string]sessionInfo
	mu            sync.Mutex
	dbMu          sync.Mutex
}

type sessionInfo struct {
	UserID    int64
	CompanyID int64
	Schema    string
	Username  string
	Role      string
	Portal    bool
	MenuKeys  map[string]bool
}

type loadingSession struct {
	ID                string
	TokenHash         string
	InvoiceNo         string
	SanadNo           string
	Customer          string
	Kala              string
	Status            string
	CreatedBy         int64
	CreatedByUsername string
	CreatedAt         string
	ExpiresAt         string
}

type tagheData struct {
	ID         int64
	Metr       float64
	Weight     float64
	Machine    string
	Kala       string
	HamPod     string
	HamChelle  string
	ShomChelle string
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
		return db, "postgres", dsn, nil
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
	return db, "sqlite", absDB, nil
}

func (a *app) exec(q string, args ...any) (sql.Result, error) {
	return a.db.Exec(rebind(a.dialect, q), args...)
}

func (a *app) query(q string, args ...any) (*sql.Rows, error) {
	return a.db.Query(rebind(a.dialect, q), args...)
}

func (a *app) queryRow(q string, args ...any) *sql.Row {
	return a.db.QueryRow(rebind(a.dialect, q), args...)
}

func txExec(dialect string, tx *sql.Tx, q string, args ...any) (sql.Result, error) {
	return tx.Exec(rebind(dialect, q), args...)
}

func txQueryRow(dialect string, tx *sql.Tx, q string, args ...any) *sql.Row {
	return tx.QueryRow(rebind(dialect, q), args...)
}

func rebind(dialect, q string) string {
	if dialect != "postgres" {
		return q
	}
	q = strings.NewReplacer(
		"datetime('now','localtime')", "CURRENT_TIMESTAMP",
		"printf('%012d', g.id_gere)", "LPAD(CAST(g.id_gere AS TEXT), 12, '0')",
	).Replace(q)
	var b strings.Builder
	b.Grow(len(q) + 8)
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

func dbType(dialect, typ string) string {
	if dialect != "postgres" {
		return typ
	}
	r := strings.NewReplacer(
		"INTEGER PRIMARY KEY AUTOINCREMENT", "BIGSERIAL PRIMARY KEY",
		"REAL", "DOUBLE PRECISION",
		"DEFAULT (datetime('now','localtime'))", "DEFAULT CURRENT_TIMESTAMP",
		"datetime('now','localtime')", "CURRENT_TIMESTAMP",
	)
	return r.Replace(typ)
}

func ddl(dialect, stmt string) string {
	if dialect != "postgres" {
		return stmt
	}
	return dbType(dialect, stmt)
}

type apiError struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}

type lookupItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type record map[string]any

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	db, dialect, label, err := openOperationalDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	a := &app{
		db:            db,
		dialect:       dialect,
		dbLabel:       label,
		defaultSchema: "public",
		portalSecret:  strings.TrimSpace(os.Getenv("OPERATIONAL_PORTAL_SECRET")),
		sessions:      map[string]sessionInfo{},
	}
	if dialect == "postgres" {
		if err := a.initializeTenancy(); err != nil {
			log.Fatal(err)
		}
		if err := a.migrateAllTenants(); err != nil {
			log.Fatal(err)
		}
	} else if err := a.migrate(); err != nil {
		log.Fatal(err)
	}
	if a.dialect == "postgres" {
		if err := a.importSQLiteOnEmpty(); err != nil {
			log.Printf("sqlite seed import warning: %v", err)
		}
	}

	mux := http.NewServeMux()
	a.routes(mux)

	port := env("PORT", "8091")
	log.Printf("Operational cycle Go server started on :%s", port)
	log.Printf("operational_db=%s", label)
	log.Printf("operational_db_driver=%s", dialect)
	log.Fatal(http.ListenAndServe(":"+port, withCORS(a.serializeDatabaseAccess(mux))))
}

var operationalTenantTables = []string{
	"mosh_name", "nakh_name", "kala_name", "chellepich", "kod_navard", "gerezan",
	"nakh_vor", "nakh_khor", "empty_beam_out", "chelle", "gere", "nakh_salon", "salon",
	"machine_consumption", "machine_formul", "chelle_formul", "machine_formul_archive", "machine_number_normalization_audit", "f_khor", "hazine", "operator_name", "driver_name",
	"weaver_name", "h_rozmare", "service_type", "spare_part", "spare_parts_inventory",
	"machinery_service", "users", "menu_items", "user_menu_access", "loading_sessions",
	"loading_session_items", "loading_reservations", "v_kh_moto", "production_waste",
}

func (a *app) setSearchPath(schema string) error {
	if a.dialect != "postgres" {
		return nil
	}
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = a.defaultSchema
	}
	_, err := a.db.Exec(`SET search_path TO ` + quoteIdent(schema) + `, public`)
	return err
}

func (a *app) initializeTenancy() error {
	if a.dialect != "postgres" {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS public.operational_tenants (
			id BIGSERIAL PRIMARY KEY, external_company_id BIGINT UNIQUE,
			company_name TEXT NOT NULL, schema_name TEXT NOT NULL UNIQUE,
			active INTEGER NOT NULL DEFAULT 1, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS public.operational_platform_users (
			id BIGSERIAL PRIMARY KEY,
			tenant_id BIGINT NOT NULL REFERENCES public.operational_tenants(id) ON DELETE CASCADE,
			local_user_id BIGINT NOT NULL, portal_access_id BIGINT,
			username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tenant_id, local_user_id)
		)`,
		`ALTER TABLE public.operational_platform_users ADD COLUMN IF NOT EXISTS portal_access_id BIGINT`,
		`CREATE UNIQUE INDEX IF NOT EXISTS operational_platform_users_portal_access_udx ON public.operational_platform_users(tenant_id,portal_access_id) WHERE portal_access_id IS NOT NULL`,
	}
	for _, stmt := range stmts {
		if _, err := a.db.Exec(stmt); err != nil {
			return err
		}
	}
	var tenantCount int64
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM public.operational_tenants`).Scan(&tenantCount); err != nil {
		return err
	}
	if tenantCount == 0 {
		const defaultSchema = "tenant_textile_default"
		tx, err := a.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`CREATE SCHEMA IF NOT EXISTS ` + quoteIdent(defaultSchema)); err != nil {
			return err
		}
		for _, table := range operationalTenantTables {
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1`, table).Scan(&exists); err != nil {
				return err
			}
			if exists == 1 {
				if _, err := tx.Exec(`ALTER TABLE public.` + quoteIdent(table) + ` SET SCHEMA ` + quoteIdent(defaultSchema)); err != nil {
					return fmt.Errorf("move operational table %s: %w", table, err)
				}
			}
		}
		if _, err := tx.Exec(`INSERT INTO public.operational_tenants(external_company_id,company_name,schema_name,active) VALUES(NULL,'Internal / legacy',$1,1)`, defaultSchema); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if err := a.db.QueryRow(`SELECT schema_name FROM public.operational_tenants WHERE external_company_id IS NULL AND active=1 ORDER BY id LIMIT 1`).Scan(&a.defaultSchema); err != nil {
		return err
	}
	return a.setSearchPath(a.defaultSchema)
}

func (a *app) migrateAllTenants() error {
	rows, err := a.db.Query(`SELECT id,schema_name FROM public.operational_tenants WHERE active=1 ORDER BY id`)
	if err != nil {
		return err
	}
	type tenant struct {
		id     int64
		schema string
	}
	tenants := []tenant{}
	for rows.Next() {
		var item tenant
		if err := rows.Scan(&item.id, &item.schema); err != nil {
			rows.Close()
			return err
		}
		tenants = append(tenants, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range tenants {
		if err := a.setSearchPath(item.schema); err != nil {
			return err
		}
		if err := a.migrate(); err != nil {
			return fmt.Errorf("migrate tenant %s: %w", item.schema, err)
		}
		if err := a.syncTenantUsers(item.id); err != nil {
			return fmt.Errorf("sync tenant users %s: %w", item.schema, err)
		}
	}
	return a.setSearchPath(a.defaultSchema)
}

func (a *app) syncTenantUsers(tenantID int64) error {
	rows, err := a.query(`SELECT id_user,username,password_hash,COALESCE(is_active,1) FROM users WHERE COALESCE(username,'')<>''`)
	if err != nil {
		return err
	}
	type user struct {
		id             int64
		username, hash string
		active         int64
	}
	users := []user{}
	for rows.Next() {
		var item user
		if err := rows.Scan(&item.id, &item.username, &item.hash, &item.active); err != nil {
			rows.Close()
			return err
		}
		users = append(users, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range users {
		platformUsername := fmt.Sprintf("tenant_%d_user_%d", tenantID, item.id)
		if _, err := a.exec(`INSERT INTO public.operational_platform_users(tenant_id,local_user_id,username,password_hash,active)
			VALUES(?,?,?,?,?) ON CONFLICT(tenant_id,local_user_id) DO UPDATE SET
			username=CASE WHEN operational_platform_users.portal_access_id IS NULL THEN excluded.username ELSE operational_platform_users.username END,
			password_hash=excluded.password_hash,active=excluded.active`, tenantID, item.id, platformUsername, item.hash, item.active); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) serializeDatabaseAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.dialect != "postgres" || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		a.dbMu.Lock()
		defer a.dbMu.Unlock()
		_ = a.setSearchPath(a.defaultSchema)
		if session, ok := a.currentSession(r); ok && strings.TrimSpace(session.Schema) != "" {
			if err := a.setSearchPath(session.Schema); err != nil {
				fail(w, http.StatusInternalServerError, "tenant database is unavailable")
				return
			}
		}
		defer func() { _ = a.setSearchPath(a.defaultSchema) }()
		next.ServeHTTP(w, r)
	})
}

func (a *app) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", a.health)
	mux.HandleFunc("/api/portal/provision", a.portalProvision)
	mux.HandleFunc("/api/portal/deprovision", a.portalDeprovision)
	mux.HandleFunc("/api/portal/session", a.portalSession)
	mux.HandleFunc("/api/login", a.login)
	mux.HandleFunc("/api/logout", a.logout)
	mux.HandleFunc("/api/session", a.requireAuth(a.sessionStatus))
	mux.HandleFunc("/api/dashboard", a.requireMenu("dashboard", a.dashboard))
	mux.HandleFunc("/api/lookups", a.requireAuth(a.lookups))
	mux.HandleFunc("/api/basic/", a.requireMenu("initial", a.basic))
	mux.HandleFunc("/api/nakh-vor", a.requireMenu("nakh-vor", a.nakhVor))
	mux.HandleFunc("/api/nakh-vor/", a.requireMenu("nakh-vor", a.nakhVorByID))
	mux.HandleFunc("/api/chelle", a.requireMenu("chelle", a.chelle))
	mux.HandleFunc("/api/chelle/", a.requireMenu("chelle", a.chelleByID))
	mux.HandleFunc("/api/gere", a.requireMenu("gere", a.gere))
	mux.HandleFunc("/api/gere/", a.requireMenu("gere", a.gereByID))
	mux.HandleFunc("/api/nakh-salon", a.requireMenu("nakh-salon", a.nakhSalon))
	mux.HandleFunc("/api/nakh-salon/", a.requireMenu("nakh-salon", a.nakhSalonByID))
	mux.HandleFunc("/api/nakh-khor", a.requireMenu("yarn-out", a.nakhKhor))
	mux.HandleFunc("/api/nakh-khor/", a.requireMenu("yarn-out", a.nakhKhorByID))
	mux.HandleFunc("/api/warper-yarn-balance", a.requireMenu("yarn-out", a.warperYarnBalance))
	mux.HandleFunc("/api/empty-beam-out", a.requireMenu("empty-beam-out", a.emptyBeamOut))
	mux.HandleFunc("/api/empty-beam-out/", a.requireMenu("empty-beam-out", a.emptyBeamOutByID))
	mux.HandleFunc("/api/salon", a.requireMenu("salon", a.salon))
	mux.HandleFunc("/api/salon/", a.requireMenu("salon", a.salonByPath))
	mux.HandleFunc("/api/out-invoice", a.requireMenu("out-invoice", a.outInvoice))
	mux.HandleFunc("/api/out-invoice/", a.requireMenu("out-invoice", a.outInvoiceByPath))
	mux.HandleFunc("/api/loading/", a.loadingMobile)
	mux.HandleFunc("/api/expenses", a.requireMenu("reports", a.expenses))
	mux.HandleFunc("/api/expenses/", a.requireMenu("reports", a.expenseByID))
	mux.HandleFunc("/api/formulas", a.requireMenu("formulas", a.formulas))
	mux.HandleFunc("/api/formulas/", a.requireMenu("formulas", a.formulaByID))
	mux.HandleFunc("/api/database/", a.requireMenu("database", a.databaseTools))
	mux.HandleFunc("/api/spare-parts", a.requireMenu("spare-parts", a.spareParts))
	mux.HandleFunc("/api/spare-parts/", a.requireMenu("spare-parts", a.sparePartByID))
	mux.HandleFunc("/api/machinery-services", a.requireMenu("machinery-services", a.machineryServices))
	mux.HandleFunc("/api/machinery-services/", a.requireMenu("machinery-services", a.machineryServiceByID))
	mux.HandleFunc("/api/menus", a.requireMenu("users", a.menus))
	mux.HandleFunc("/api/users", a.requireMenu("users", a.users))
	mux.HandleFunc("/api/users/", a.requireMenu("users", a.userByID))
	mux.HandleFunc("/api/next-salon-id", a.requireMenu("salon", a.nextSalonID))
	mux.HandleFunc("/api/consumption/machines", a.requireMenu("consumption", a.consumptionMachines))
	mux.HandleFunc("/api/production-waste", a.requireMenu("consumption", a.productionWaste))
	mux.HandleFunc("/api/production-waste/", a.requireMenu("consumption", a.productionWasteByID))
	mux.HandleFunc("/api/advisor", a.requireMenu("advisor", a.managementReport))
	mux.HandleFunc("/api/management-report", a.requireMenu("reports", a.managementReport))
	mux.HandleFunc("/api/reset-cycle", a.requireMenu("initial", a.resetCycle))
	mux.HandleFunc("/", staticHandler())
}

func (a *app) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := a.currentSession(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "نشست شما معتبر نیست. دوباره وارد شوید.")
			return
		}
		var active int64
		if err := a.queryRow(`SELECT COALESCE(is_active,1) FROM users WHERE id_user=?`, session.UserID).Scan(&active); err != nil || active != 1 {
			a.clearSession(w, r)
			fail(w, http.StatusUnauthorized, "حساب کاربری غیرفعال است")
			return
		}
		next(w, r)
	}
}

func (a *app) requireMenu(menuKey string, next http.HandlerFunc) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		session, ok := a.currentSession(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "نشست شما معتبر نیست. دوباره وارد شوید.")
			return
		}
		if session.Portal {
			if menuKey == "users" || (!session.MenuKeys["*"] && !session.MenuKeys[menuKey]) {
				fail(w, http.StatusForbidden, "این دسترسی فقط از بخش مدیریت کاربران مرکزی تعیین می‌شود.")
				return
			}
			next(w, r)
			return
		}
		allowed, err := a.userHasMenuAccess(session.UserID, session.Role, menuKey)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			fail(w, http.StatusForbidden, "شما به این بخش دسترسی ندارید.")
			return
		}
		next(w, r)
	})
}

func (a *app) createSession(w http.ResponseWriter, r *http.Request, session sessionInfo) error {
	token, err := randomSessionToken()
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.sessions[token] = session
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "operational_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	})
	return nil
}

func (a *app) clearSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("operational_session")
	if err == nil && cookie.Value != "" {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "operational_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (a *app) currentSession(r *http.Request) (sessionInfo, bool) {
	cookie, err := r.Cookie("operational_session")
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return sessionInfo{}, false
	}
	a.mu.Lock()
	session, ok := a.sessions[cookie.Value]
	a.mu.Unlock()
	if !ok {
		return sessionInfo{}, false
	}
	return session, true
}

func (a *app) userHasMenuAccess(userID int64, role, menuKey string) (bool, error) {
	if strings.EqualFold(strings.TrimSpace(role), "admin") {
		return true, nil
	}
	var hasAccess int64
	err := a.queryRow(`SELECT COALESCE(uma.has_access, CASE WHEN COALESCE(m.is_restricted,0)=1 THEN 0 ELSE 1 END)
		FROM menu_items m
		LEFT JOIN user_menu_access uma ON uma.menu_key=m.menu_key AND uma.user_id=?
		WHERE m.menu_key=?`, userID, menuKey).Scan(&hasAccess)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return hasAccess == 1, nil
}

func randomSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isSecureRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (a *app) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS mosh_name (id_mosh_name INTEGER PRIMARY KEY AUTOINCREMENT, name_mosh TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS nakh_name (id_nakh_name INTEGER PRIMARY KEY AUTOINCREMENT, name_nakh_name TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS kala_name (id_kala_name INTEGER PRIMARY KEY AUTOINCREMENT, name_kala_name TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS chellepich (id_chellepich INTEGER PRIMARY KEY AUTOINCREMENT, name_chellepich TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS kod_navard (id_kod_navard INTEGER PRIMARY KEY AUTOINCREMENT, kod_kod_navard TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS gerezan (id_gerezan INTEGER PRIMARY KEY AUTOINCREMENT, name_gerezan TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS nakh_vor (id_nakh_vor INTEGER PRIMARY KEY AUTOINCREMENT, tarikh_nakh_vor TEXT, hambaft_nakh_vor TEXT, w_vor_nakh_vor REAL, moshname_nakh_vor TEXT, nakh_name_nakh_vor TEXT)`,
		`CREATE TABLE IF NOT EXISTS nakh_khor (id_nakh_khor INTEGER PRIMARY KEY AUTOINCREMENT, tarikh_nakh_khor TEXT, hambaft_nakh_khor TEXT, w_vor_nakh_khor REAL, moshname_nakh_khor TEXT, nakh_name_nakh_khor TEXT)`,
		`CREATE TABLE IF NOT EXISTS empty_beam_out (id_empty_beam_out INTEGER PRIMARY KEY AUTOINCREMENT, tarikh_empty_beam_out TEXT, kod_navard TEXT, chellepich_name TEXT, description TEXT, created_at TEXT DEFAULT (datetime('now','localtime')))`,
		`CREATE TABLE IF NOT EXISTS chelle (id_chelle INTEGER PRIMARY KEY AUTOINCREMENT, tarikh_chelle TEXT, shom_chelle TEXT, nakh_chelle TEXT, w_chelle REAL, pich_chelle TEXT, mosh_chelle TEXT, hambaft_chelle TEXT, codnavard_chelle TEXT, machin_chelle TEXT)`,
		`CREATE TABLE IF NOT EXISTS gere (id_gere INTEGER PRIMARY KEY AUTOINCREMENT, name_gere TEXT, shom_chelle_gere TEXT, machin_gere TEXT, tarikh_gere TEXT)`,
		`CREATE TABLE IF NOT EXISTS nakh_salon (id_nakh_salon INTEGER PRIMARY KEY AUTOINCREMENT, tarikh_nakh_salon TEXT, shom_machin_nakh_salon TEXT, ham_nakh_salon TEXT, w_nakh_salon REAL, shom_chelle_nakh_salon TEXT, mosh_name_nakh_salon TEXT, vor_khor_nakh_salon TEXT)`,
		`CREATE TABLE IF NOT EXISTS salon (id_salon INTEGER PRIMARY KEY, metr_salon REAL, w_salon REAL, machin_salon TEXT, user_salon TEXT, tarikh_salon TEXT, kala_salon TEXT, ham_pod_salon TEXT, ham_chelle_salon TEXT, shom_chelle_salon TEXT)`,
		`CREATE TABLE IF NOT EXISTS machine_consumption (id_consumption INTEGER PRIMARY KEY AUTOINCREMENT, machine TEXT, shom_chelle TEXT, tar_used REAL, pod_used REAL, total_weight REAL, remaining_weight REAL, tarikh_consumption TEXT)`,
		`CREATE TABLE IF NOT EXISTS machine_formul (id_formul INTEGER PRIMARY KEY AUTOINCREMENT, machine TEXT UNIQUE, tar_percent REAL DEFAULT 50, pod_percent REAL DEFAULT 50, tozih_formul TEXT)`,
		`CREATE TABLE IF NOT EXISTS chelle_formul (id_formul INTEGER PRIMARY KEY AUTOINCREMENT, chelle_id INTEGER NOT NULL, machine TEXT NOT NULL, shom_chelle TEXT NOT NULL, kala_name TEXT NOT NULL DEFAULT '', ham_chelle TEXT NOT NULL DEFAULT '', ham_pod TEXT NOT NULL DEFAULT '', tar_percent REAL NOT NULL DEFAULT 50, pod_percent REAL NOT NULL DEFAULT 50, tozih_formul TEXT, created_at TEXT DEFAULT (datetime('now','localtime')), updated_at TEXT DEFAULT (datetime('now','localtime')), UNIQUE(chelle_id,kala_name,ham_chelle,ham_pod))`,
		`CREATE INDEX IF NOT EXISTS idx_chelle_formul_machine_chelle ON chelle_formul(machine,shom_chelle)`,
		`CREATE TABLE IF NOT EXISTS machine_formul_archive (id_archive INTEGER PRIMARY KEY AUTOINCREMENT, source_id INTEGER, machine TEXT, canonical_machine TEXT, tar_percent REAL, pod_percent REAL, tozih_formul TEXT, reason TEXT, archived_at TEXT DEFAULT (datetime('now','localtime')))`,
		`CREATE TABLE IF NOT EXISTS machine_number_normalization_audit (id_audit INTEGER PRIMARY KEY AUTOINCREMENT, table_name TEXT, row_id INTEGER, column_name TEXT, old_value TEXT, new_value TEXT, normalized_at TEXT DEFAULT (datetime('now','localtime')))`,
		`CREATE TABLE IF NOT EXISTS production_waste (id_waste INTEGER PRIMARY KEY AUTOINCREMENT, waste_date TEXT, machine TEXT NOT NULL, shom_chelle TEXT NOT NULL, waste_type TEXT NOT NULL, weight REAL NOT NULL, reason TEXT, operator_name TEXT, description TEXT, created_at TEXT DEFAULT (datetime('now','localtime')))`,
		`CREATE TABLE IF NOT EXISTS f_khor (id_f_khor INTEGER PRIMARY KEY AUTOINCREMENT, tarikh_f_khor TEXT, shom_f_khor TEXT, taghe_cod_f_khor TEXT, mosh_f_khor TEXT, shomare_sanad TEXT, kala_name_f_khor TEXT)`,
		`CREATE TABLE IF NOT EXISTS hazine (id_hazine INTEGER PRIMARY KEY AUTOINCREMENT, onvan_hazine TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS operator_name (id_operator INTEGER PRIMARY KEY AUTOINCREMENT, name_operator TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS driver_name (id_driver INTEGER PRIMARY KEY AUTOINCREMENT, name_driver TEXT UNIQUE, tozih_driver TEXT)`,
		`CREATE TABLE IF NOT EXISTS weaver_name (id_weaver INTEGER PRIMARY KEY AUTOINCREMENT, name_weaver TEXT UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS h_rozmare (id_h_rozmare INTEGER PRIMARY KEY AUTOINCREMENT, tarikh_h_rozmare TEXT, onvan_hazine TEXT, operator_name TEXT, weaver_name TEXT, mablagh_h_rozmare REAL, tozih_h_rozmare TEXT, shomare_sanad TEXT)`,
		`CREATE TABLE IF NOT EXISTS service_type (id_service_type INTEGER PRIMARY KEY AUTOINCREMENT, name_service_type TEXT, tozih_service_type TEXT)`,
		`CREATE TABLE IF NOT EXISTS spare_part (id_spare_part INTEGER PRIMARY KEY AUTOINCREMENT, name_spare_part TEXT NOT NULL, part_number_spare_part TEXT, tozih_spare_part TEXT)`,
		`CREATE TABLE IF NOT EXISTS spare_parts_inventory (id_spare_inventory INTEGER PRIMARY KEY AUTOINCREMENT, spare_part_id INTEGER UNIQUE, part_name TEXT NOT NULL, part_number TEXT, quantity INTEGER DEFAULT 0, condition_status TEXT, vendor_name TEXT, used_machine TEXT, receiver_name TEXT, description TEXT, created_at TEXT DEFAULT (datetime('now','localtime')), updated_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS machinery_service (id_machinery_service INTEGER PRIMARY KEY AUTOINCREMENT, machinery_name TEXT, service_date TEXT, service_type_id INTEGER, spare_part_id INTEGER, quantity_spare INTEGER DEFAULT 1, description_service TEXT, operator_name TEXT)`,
		`CREATE TABLE IF NOT EXISTS v_kh_moto (id INTEGER PRIMARY KEY AUTOINCREMENT, tarikh_v_kh_moto TEXT, operation_type TEXT, name_kala TEXT, shomare_kala TEXT, from_location TEXT, to_location TEXT, person TEXT, status TEXT, tozih_v_kh_moto TEXT, tarikh_bazgasht TEXT)`,
		`CREATE TABLE IF NOT EXISTS users (id_user INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT DEFAULT 'viewer', is_active INTEGER DEFAULT 1, created_at TEXT DEFAULT (datetime('now','localtime')))`,
		`CREATE TABLE IF NOT EXISTS menu_items (id_menu INTEGER PRIMARY KEY AUTOINCREMENT, menu_key TEXT UNIQUE, menu_name TEXT, path TEXT, icon TEXT, is_restricted INTEGER DEFAULT 0, sort_order INTEGER DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS user_menu_access (id_access INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, menu_key TEXT, has_access INTEGER DEFAULT 1, granted_by INTEGER, granted_at TEXT DEFAULT (datetime('now','localtime')), UNIQUE(user_id, menu_key))`,
		`CREATE TABLE IF NOT EXISTS loading_sessions (id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, invoice_no TEXT NOT NULL, sanad_no TEXT, customer TEXT NOT NULL, kala TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active', created_by INTEGER NOT NULL, created_by_username TEXT, created_at TEXT NOT NULL, expires_at TEXT NOT NULL, completed_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS loading_session_items (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, taghe_code TEXT NOT NULL, confirmed_by INTEGER NOT NULL, confirmed_by_username TEXT, confirmed_at TEXT NOT NULL, UNIQUE(session_id, taghe_code))`,
		`CREATE TABLE IF NOT EXISTS loading_reservations (taghe_code TEXT PRIMARY KEY, session_id TEXT NOT NULL, reserved_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_loading_sessions_status_expires ON loading_sessions(status, expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_loading_items_session ON loading_session_items(session_id)`,
	}
	for _, stmt := range stmts {
		if _, err := a.exec(ddl(a.dialect, stmt)); err != nil {
			return err
		}
	}
	for _, col := range []struct{ table, name, typ string }{
		{"gere", "tarikh_gere", "TEXT"},
		{"f_khor", "kala_name_f_khor", "TEXT"},
		{"f_khor", "barcode_code", "TEXT"},
		{"gerezan", "tozih_gerezan", "TEXT"},
		{"hazine", "tozih_hazine", "TEXT"},
		{"kala_name", "tozih_kala_name", "TEXT"},
		{"kod_navard", "tozih_kod_navard", "TEXT"},
		{"machine_formul", "tarikh_formul", "TEXT"},
		{"mosh_name", "add_mosh", "TEXT"},
		{"mosh_name", "phon_mosh", "TEXT"},
		{"nakh_name", "tozih_nakh_name", "TEXT"},
		{"operator_name", "tozih_operator", "TEXT"},
		{"salon", "barcode_code", "TEXT"},
		{"salon", "chelle_id_salon", "INTEGER"},
		{"salon", "tar_percent_salon", "REAL"},
		{"salon", "pod_percent_salon", "REAL"},
		{"weaver_name", "tozih_weaver", "TEXT"},
		{"h_rozmare", "weaver_name", "TEXT"},
		{"h_rozmare", "shomare_sanad", "TEXT"},
		{"spare_parts_inventory", "spare_part_id", "INTEGER"},
		{"spare_parts_inventory", "used_machine", "TEXT"},
		{"spare_parts_inventory", "receiver_name", "TEXT"},
		{"menu_items", "path", "TEXT"},
		{"menu_items", "icon", "TEXT"},
		{"menu_items", "is_restricted", "INTEGER DEFAULT 0"},
		{"menu_items", "sort_order", "INTEGER DEFAULT 0"},
		{"user_menu_access", "granted_by", "INTEGER"},
		{"user_menu_access", "granted_at", "TEXT"},
		{"nakh_khor", "owner_mosh_nakh_khor", "TEXT"},
		{"nakh_khor", "destination_type_nakh_khor", "TEXT DEFAULT 'warper'"},
		{"nakh_salon", "nakh_name_nakh_salon", "TEXT"},
		{"nakh_salon", "chelle_id_nakh_salon", "INTEGER"},
		{"gere", "chelle_id_gere", "INTEGER"},
		{"salon", "chelle_id_salon", "INTEGER"},
		{"production_waste", "chelle_id_waste", "INTEGER"},
		{"production_waste", "corrective_action", "TEXT"},
	} {
		if err := a.ensureColumn(col.table, col.name, col.typ); err != nil {
			return err
		}
	}
	var userCount int64
	_ = a.queryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount)
	if userCount == 0 {
		initialPassword := env("OPERATIONAL_ADMIN_PASSWORD", "admin123")
		hash, err := hashPassword(initialPassword)
		if err != nil {
			return err
		}
		if _, err := a.exec(`INSERT INTO users (username,password_hash,role,is_active) VALUES ('admin',?,'admin',1)`, hash); err != nil {
			return err
		}
	}
	if err := a.seedMenus(); err != nil {
		return err
	}
	// Keep legacy rows available, but make new material movements fully traceable.
	_, _ = a.exec(`UPDATE nakh_khor SET owner_mosh_nakh_khor=moshname_nakh_khor WHERE COALESCE(owner_mosh_nakh_khor,'')=''`)
	_, _ = a.exec(`UPDATE nakh_khor SET destination_type_nakh_khor='warper' WHERE COALESCE(destination_type_nakh_khor,'')=''`)
	_, _ = a.exec(`UPDATE gere SET chelle_id_gere=(SELECT MAX(c.id_chelle) FROM chelle c WHERE c.shom_chelle=gere.shom_chelle_gere) WHERE chelle_id_gere IS NULL`)
	_, _ = a.exec(`UPDATE nakh_salon SET chelle_id_nakh_salon=(SELECT MAX(c.id_chelle) FROM chelle c WHERE c.shom_chelle=nakh_salon.shom_chelle_nakh_salon AND c.machin_chelle=nakh_salon.shom_machin_nakh_salon) WHERE chelle_id_nakh_salon IS NULL`)
	_, _ = a.exec(`UPDATE salon SET chelle_id_salon=(SELECT MAX(c.id_chelle) FROM chelle c WHERE c.shom_chelle=salon.shom_chelle_salon AND (c.machin_chelle=salon.machin_salon OR EXISTS(SELECT 1 FROM gere g WHERE g.chelle_id_gere=c.id_chelle AND g.machin_gere=salon.machin_salon))) WHERE chelle_id_salon IS NULL`)
	_, _ = a.exec(`UPDATE production_waste SET chelle_id_waste=(SELECT MAX(c.id_chelle) FROM chelle c WHERE c.shom_chelle=production_waste.shom_chelle AND (c.machin_chelle=production_waste.machine OR EXISTS(SELECT 1 FROM gere g WHERE g.chelle_id_gere=c.id_chelle AND g.machin_gere=production_waste.machine))) WHERE chelle_id_waste IS NULL`)
	_, _ = a.exec(`UPDATE machine_formul SET tar_percent=50,pod_percent=50 WHERE COALESCE(tar_percent,0)+COALESCE(pod_percent,0)<=0`)
	_, _ = a.exec(`UPDATE machine_formul SET tar_percent=tar_percent*100.0/(tar_percent+pod_percent), pod_percent=pod_percent*100.0/(tar_percent+pod_percent) WHERE ABS((tar_percent+pod_percent)-100)>0.001 AND tar_percent+pod_percent>0`)
	if err := a.normalizeMachineNumbers(); err != nil {
		return err
	}
	// Retry all legacy relations after machine labels such as 7.0 have become 7.
	_, _ = a.exec(`UPDATE nakh_salon SET chelle_id_nakh_salon=(SELECT MAX(c.id_chelle) FROM chelle c WHERE c.shom_chelle=nakh_salon.shom_chelle_nakh_salon AND c.machin_chelle=nakh_salon.shom_machin_nakh_salon) WHERE COALESCE(chelle_id_nakh_salon,0)=0`)
	_, _ = a.exec(`UPDATE salon SET chelle_id_salon=(SELECT MAX(c.id_chelle) FROM chelle c WHERE c.shom_chelle=salon.shom_chelle_salon AND (c.machin_chelle=salon.machin_salon OR EXISTS(SELECT 1 FROM gere g WHERE g.chelle_id_gere=c.id_chelle AND g.machin_gere=salon.machin_salon))) WHERE COALESCE(chelle_id_salon,0)=0`)
	_, _ = a.exec(`UPDATE production_waste SET chelle_id_waste=(SELECT MAX(c.id_chelle) FROM chelle c WHERE c.shom_chelle=production_waste.shom_chelle AND (c.machin_chelle=production_waste.machine OR EXISTS(SELECT 1 FROM gere g WHERE g.chelle_id_gere=c.id_chelle AND g.machin_gere=production_waste.machine))) WHERE COALESCE(chelle_id_waste,0)=0`)
	// Freeze the formula used by every legacy production row. Once populated,
	// changes to a machine default or a later beam can no longer rewrite history.
	_, _ = a.exec(`UPDATE salon SET chelle_id_salon=(SELECT MAX(c.id_chelle) FROM chelle c WHERE c.shom_chelle=salon.shom_chelle_salon AND c.machin_chelle=salon.machin_salon) WHERE COALESCE(chelle_id_salon,0)=0`)
	_, _ = a.exec(`UPDATE salon SET tar_percent_salon=COALESCE((SELECT mf.tar_percent FROM machine_formul mf WHERE mf.machine=salon.machin_salon),50), pod_percent_salon=COALESCE((SELECT mf.pod_percent FROM machine_formul mf WHERE mf.machine=salon.machin_salon),50) WHERE tar_percent_salon IS NULL OR pod_percent_salon IS NULL OR COALESCE(tar_percent_salon,0)+COALESCE(pod_percent_salon,0)<=0`)
	_, _ = a.exec(`UPDATE salon SET tar_percent_salon=tar_percent_salon*100.0/(tar_percent_salon+pod_percent_salon), pod_percent_salon=pod_percent_salon*100.0/(tar_percent_salon+pod_percent_salon) WHERE ABS((tar_percent_salon+pod_percent_salon)-100)>0.001 AND tar_percent_salon+pod_percent_salon>0`)
	_, _ = a.exec(`INSERT INTO chelle_formul (chelle_id,machine,shom_chelle,kala_name,ham_chelle,ham_pod,tar_percent,pod_percent,tozih_formul)
		SELECT s.chelle_id_salon,TRIM(s.machin_salon),TRIM(s.shom_chelle_salon),TRIM(COALESCE(s.kala_salon,'')),TRIM(COALESCE(s.ham_chelle_salon,'')),TRIM(COALESCE(s.ham_pod_salon,'')),s.tar_percent_salon,s.pod_percent_salon,'ثبت خودکار از سابقه تولید'
		FROM salon s
		WHERE COALESCE(s.chelle_id_salon,0)>0
		  AND s.id_salon=(SELECT MAX(s2.id_salon) FROM salon s2 WHERE s2.chelle_id_salon=s.chelle_id_salon AND TRIM(COALESCE(s2.kala_salon,''))=TRIM(COALESCE(s.kala_salon,'')) AND TRIM(COALESCE(s2.ham_chelle_salon,''))=TRIM(COALESCE(s.ham_chelle_salon,'')) AND TRIM(COALESCE(s2.ham_pod_salon,''))=TRIM(COALESCE(s.ham_pod_salon,'')))
		ON CONFLICT(chelle_id,kala_name,ham_chelle,ham_pod) DO NOTHING`)
	if err := a.reconcileActiveChelles(); err != nil {
		return err
	}
	return nil
}

// reconcileActiveChelles guarantees one current beam per machine without deleting
// the assignment history kept in gere. Only machines that have a gere record are
// reconciled, so old direct assignments are not lost.
func (a *app) reconcileActiveChelles() error {
	rows, err := a.query(`SELECT id_gere, COALESCE(machin_gere,''), COALESCE(chelle_id_gere,0)
		FROM gere WHERE COALESCE(machin_gere,'')<>''
		ORDER BY machin_gere, COALESCE(tarikh_gere,'') DESC, id_gere DESC`)
	if err != nil {
		return err
	}
	type assignment struct {
		machine string
		chelle  int64
	}
	latest := []assignment{}
	seen := map[string]bool{}
	for rows.Next() {
		var id, chelleID int64
		var machine string
		if err := rows.Scan(&id, &machine, &chelleID); err != nil {
			rows.Close()
			return err
		}
		machine = strings.TrimSpace(machine)
		if machine == "" || chelleID == 0 || seen[machine] {
			continue
		}
		seen[machine] = true
		latest = append(latest, assignment{machine: machine, chelle: chelleID})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(latest) == 0 {
		return nil
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range latest {
		if _, err = txExec(a.dialect, tx, `UPDATE chelle SET machin_chelle='' WHERE machin_chelle=?`, item.machine); err != nil {
			return err
		}
		if _, err = txExec(a.dialect, tx, `UPDATE chelle SET machin_chelle=? WHERE id_chelle=?`, item.machine, item.chelle); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// activateLatestChelleForMachineTx keeps the assignment history in gere while
// deriving exactly one active chelle for a machine. A preferred chelle is used
// for a new/edit assignment; after delete, the latest still-valid historical
// assignment is restored.
func (a *app) activateLatestChelleForMachineTx(tx *sql.Tx, machine string, preferredChelleID int64) error {
	machine = strings.TrimSpace(machine)
	if machine == "" {
		return nil
	}
	if _, err := txExec(a.dialect, tx, `UPDATE chelle SET machin_chelle='' WHERE machin_chelle=?`, machine); err != nil {
		return err
	}
	chelleID := preferredChelleID
	if chelleID == 0 {
		err := txQueryRow(a.dialect, tx, `SELECT g.chelle_id_gere
			FROM gere g JOIN chelle c ON c.id_chelle=g.chelle_id_gere
			WHERE g.machin_gere=? AND COALESCE(g.chelle_id_gere,0)>0
			AND COALESCE(c.machin_chelle,'') IN ('',?)
			ORDER BY COALESCE(g.tarikh_gere,'') DESC, g.id_gere DESC LIMIT 1`, machine, machine).Scan(&chelleID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	result, err := txExec(a.dialect, tx, `UPDATE chelle SET machin_chelle=?
		WHERE id_chelle=? AND COALESCE(machin_chelle,'') IN ('',?)`, machine, chelleID, machine)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("چله انتخاب‌شده هم‌اکنون روی ماشین دیگری فعال است")
	}
	return nil
}

func (a *app) importSQLiteOnEmpty() error {
	path := strings.TrimSpace(os.Getenv("OPERATIONAL_SQLITE_PATH"))
	if path == "" {
		path = strings.TrimSpace(os.Getenv("OPERATIONAL_DB_PATH"))
	}
	if path == "" {
		path = filepath.Join("..", "operational", "database.db")
	}
	abs, _ := filepath.Abs(path)
	if _, err := os.Stat(abs); err != nil {
		return nil
	}
	src, err := sql.Open("sqlite", abs)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := src.Ping(); err != nil {
		return err
	}
	tables, err := a.tableNames()
	if err != nil {
		return err
	}
	for _, table := range tables {
		if a.count(table) > 0 || !sqliteTableExists(src, table) {
			continue
		}
		srcCols, rows, err := readTableFromDB(src, table)
		if err != nil || len(srcCols) == 0 || len(rows) == 0 {
			if err != nil {
				log.Printf("sqlite import skipped %s: %v", table, err)
			}
			continue
		}
		dstCols, _, err := a.readTable(table)
		if err != nil {
			log.Printf("sqlite import skipped %s: %v", table, err)
			continue
		}
		cols, indexes := commonImportColumns(dstCols, srcCols)
		if len(cols) == 0 {
			continue
		}
		placeholders := make([]string, len(cols))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		stmt := "INSERT INTO " + quoteIdent(table) + " (" + quoteColumns(cols) + ") VALUES (" + strings.Join(placeholders, ",") + ")"
		imported := 0
		for _, row := range rows {
			vals := make([]any, len(indexes))
			for i, idx := range indexes {
				if idx >= 0 && idx < len(row) {
					value := row[idx]
					if strings.TrimSpace(value) == "" {
						vals[i] = nil
					} else {
						vals[i] = value
					}
				}
			}
			if _, err := a.exec(stmt, vals...); err != nil {
				log.Printf("sqlite import row skipped table=%s: %v", table, err)
				continue
			}
			imported++
		}
		if err := a.refreshPostgresSequence(table, cols); err != nil {
			log.Printf("sequence refresh warning %s: %v", table, err)
		}
		log.Printf("imported sqlite seed table=%s rows=%d from=%s", table, imported, abs)
	}
	return nil
}

func sqliteTableExists(db *sql.DB, table string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n)
	return n > 0
}

func commonImportColumns(dstCols, srcCols []string) ([]string, []int) {
	srcIndex := map[string]int{}
	for i, col := range srcCols {
		srcIndex[col] = i
	}
	cols := make([]string, 0, len(dstCols))
	indexes := make([]int, 0, len(dstCols))
	for _, col := range dstCols {
		if idx, ok := srcIndex[col]; ok {
			cols = append(cols, col)
			indexes = append(indexes, idx)
		}
	}
	return cols, indexes
}

func readTableFromDB(db *sql.DB, table string) ([]string, [][]string, error) {
	colRows, err := db.Query("PRAGMA table_info(" + quoteIdent(table) + ")")
	if err != nil {
		return nil, nil, err
	}
	defer colRows.Close()
	cols := []string{}
	for colRows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt any
		var pk int
		if err := colRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, nil, err
		}
		cols = append(cols, name)
	}
	if len(cols) == 0 {
		return cols, [][]string{}, nil
	}
	rows, err := db.Query("SELECT " + quoteColumns(cols) + " FROM " + quoteIdent(table))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	data := [][]string{}
	for rows.Next() {
		raw := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		line := make([]string, len(cols))
		for i, v := range raw {
			if v.Valid {
				line[i] = v.String
			}
		}
		data = append(data, line)
	}
	return cols, data, rows.Err()
}

func (a *app) refreshPostgresSequence(table string, cols []string) error {
	if a.dialect != "postgres" {
		return nil
	}
	for _, col := range cols {
		if strings.HasPrefix(col, "id_") || col == "id" {
			_, err := a.exec(`SELECT setval(pg_get_serial_sequence(?, ?), COALESCE((SELECT MAX(`+quoteIdent(col)+`) FROM `+quoteIdent(table)+`),0)+1, false)`, table, col)
			return err
		}
	}
	return nil
}

func (a *app) ensureColumn(table, column, typ string) error {
	if a.dialect == "postgres" {
		var n int
		if err := a.queryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=? AND column_name=?`, table, column).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		_, err := a.exec("ALTER TABLE " + quoteIdent(table) + " ADD COLUMN " + quoteIdent(column) + " " + dbType(a.dialect, typ))
		return err
	} else {
		rows, err := a.query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull int
			var dflt any
			var pk int
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return err
			}
			if name == column {
				return nil
			}
		}
	}
	_, err := a.exec("ALTER TABLE " + quoteIdent(table) + " ADD COLUMN " + quoteIdent(column) + " " + typ)
	return err
}

func (a *app) seedMenus() error {
	menus := []struct {
		key, name, path, icon string
		restricted, order     int
	}{
		{"dashboard", "داشبورد", "/", "📈", 0, 1},
		{"initial", "اطلاعات اولیه", "/initial", "📝", 0, 2},
		{"nakh-vor", "ورود نخ", "/nakh-vor", "🧵", 0, 3},
		{"chelle", "ورود چله", "/chelle", "📦", 0, 4},
		{"gere", "گره", "/gere", "🪢", 0, 5},
		{"nakh-salon", "ورود نخ سالن", "/nakh-salon", "🧶", 0, 6},
		{"formulas", "فرمول پیش‌فرض ماشین‌ها", "/formulas", "📐", 0, 7},
		{"salon", "سالن تولید", "/salon", "🏭", 0, 8},
		{"consumption", "مصرف تار و پود", "/consumption", "📊", 0, 9},
		{"yarn-out", "خروج نخ", "/yarn-out", "🚪", 0, 10},
		{"empty-beam-out", "خروج نورد خالی", "/empty-beam-out", "📍", 0, 11},
		{"out-invoice", "فاکتور خروج", "/out-invoice", "🧾", 0, 11},
		{"expenses", "هزینه‌ها", "/expenses", "💰", 1, 12},
		{"advisor", "تحلیل و مشاور هوشمند", "/advisor", "🧠", 0, 13},
		{"reports", "گزارشات", "/reports", "📊", 0, 14},
		{"v-kh-moto", "ورودی/خروجی متفرقه", "/v-kh-moto", "🔁", 0, 15},
		{"database", "مدیریت دیتابیس", "/database", "🗄️", 1, 16},
		{"machinery-services", "خدمات ماشین‌آلات", "/machinery-services", "🔧", 1, 17},
		{"spare-parts", "موجودی انبار قطعات", "/spare-parts", "⚙️", 1, 18},
		{"users", "مدیریت کاربران", "/users", "👥", 1, 19},
	}
	for _, m := range menus {
		if _, err := a.exec(`INSERT INTO menu_items (menu_key, menu_name, path, icon, is_restricted, sort_order) VALUES (?,?,?,?,?,?)
			ON CONFLICT(menu_key) DO UPDATE SET menu_name=excluded.menu_name, path=excluded.path, icon=excluded.icon, is_restricted=excluded.is_restricted, sort_order=excluded.sort_order`,
			m.key, m.name, m.path, m.icon, m.restricted, m.order); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) portalSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	provided := strings.TrimSpace(r.Header.Get("X-Operational-Portal-Secret"))
	expected := strings.TrimSpace(a.portalSecret)
	if expected == "" || !hmac.Equal([]byte(provided), []byte(expected)) {
		fail(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var payload struct {
		CompanyID int64    `json:"company_id"`
		AccessID  int64    `json:"access_id"`
		Username  string   `json:"username"`
		Role      string   `json:"role"`
		MenuKeys  []string `json:"menu_keys"`
	}
	if !decode(w, r, &payload) {
		return
	}
	payload.Username = strings.TrimSpace(payload.Username)
	if payload.CompanyID <= 0 || payload.AccessID <= 0 || payload.Username == "" {
		fail(w, http.StatusBadRequest, "company, access, and username are required")
		return
	}

	var localUserID int64
	var companyID int64
	var schemaName string
	err := a.queryRow(`SELECT u.local_user_id,t.external_company_id,t.schema_name
		FROM public.operational_platform_users u
		JOIN public.operational_tenants t ON t.id=u.tenant_id
		WHERE u.portal_access_id=? AND t.external_company_id=? AND u.active=1 AND t.active=1`, payload.AccessID, payload.CompanyID).Scan(&localUserID, &companyID, &schemaName)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusUnauthorized, "operational access is not provisioned")
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "operational session could not be created")
		return
	}
	role := strings.ToLower(strings.TrimSpace(payload.Role))
	switch role {
	case "admin", "manager", "accountant", "viewer":
	default:
		role = "viewer"
	}
	menuKeys := portalMenuKeySet(payload.MenuKeys)
	menus := a.portalMenus(menuKeys)
	if err := a.createSession(w, r, sessionInfo{
		UserID:    localUserID,
		CompanyID: companyID,
		Schema:    schemaName,
		Username:  payload.Username,
		Role:      role,
		Portal:    true,
		MenuKeys:  menuKeys,
	}); err != nil {
		fail(w, http.StatusInternalServerError, "operational session could not be created")
		return
	}
	if err := a.setSearchPath(schemaName); err != nil {
		fail(w, http.StatusInternalServerError, "tenant database is unavailable")
		return
	}
	writeJSON(w, record{
		"success": true,
		"user": record{
			"id":       localUserID,
			"username": payload.Username,
			"role":     role,
		},
		"menus": menus,
	})
}

func (a *app) portalProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	provided := strings.TrimSpace(r.Header.Get("X-Operational-Portal-Secret"))
	expected := strings.TrimSpace(a.portalSecret)
	if expected == "" || !hmac.Equal([]byte(provided), []byte(expected)) {
		fail(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var payload struct {
		CompanyID int64  `json:"company_id"`
		AccessID  int64  `json:"access_id"`
		Company   string `json:"company_name"`
		Contact   string `json:"contact_name"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Role      string `json:"role"`
	}
	if !decode(w, r, &payload) {
		return
	}
	payload.Company = strings.TrimSpace(payload.Company)
	payload.Username = strings.TrimSpace(payload.Username)
	if payload.AccessID <= 0 || payload.Company == "" || payload.Username == "" || payload.Password == "" {
		fail(w, http.StatusBadRequest, "access, company, username, and password are required")
		return
	}
	allocatedCompany := false
	if payload.CompanyID <= 0 {
		companyID, err := a.allocateFinancialCompany(payload.Company)
		if err != nil {
			log.Printf("financial company allocation failed for access=%d: %v", payload.AccessID, err)
			fail(w, http.StatusBadRequest, "financial company could not be allocated")
			return
		}
		payload.CompanyID = companyID
		allocatedCompany = true
	}
	if err := a.provisionTenantAccess(payload.CompanyID, payload.AccessID, payload.Company, payload.Contact, payload.Username, payload.Password, payload.Role); err != nil {
		if allocatedCompany {
			_, _ = a.db.Exec(`DELETE FROM public.companies c WHERE c.id=$1 AND NOT EXISTS (SELECT 1 FROM public.operational_tenants t WHERE t.external_company_id=c.id)`, payload.CompanyID)
		}
		log.Printf("operational tenant provisioning failed for company=%d access=%d: %v", payload.CompanyID, payload.AccessID, err)
		fail(w, http.StatusBadRequest, "tenant access could not be provisioned")
		return
	}
	writeJSON(w, record{"success": true, "company_id": payload.CompanyID})
}

func (a *app) portalDeprovision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	provided := strings.TrimSpace(r.Header.Get("X-Operational-Portal-Secret"))
	expected := strings.TrimSpace(a.portalSecret)
	if expected == "" || !hmac.Equal([]byte(provided), []byte(expected)) {
		fail(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var payload struct {
		Username string `json:"username"`
	}
	if !decode(w, r, &payload) {
		return
	}
	username := strings.TrimSpace(payload.Username)
	if username == "" {
		fail(w, http.StatusBadRequest, "username is required")
		return
	}
	var tenantID, companyID int64
	var schemaName string
	err := a.db.QueryRow(`SELECT t.id,t.external_company_id,t.schema_name FROM public.operational_platform_users u JOIN public.operational_tenants t ON t.id=u.tenant_id WHERE u.username=$1`, username).Scan(&tenantID, &companyID, &schemaName)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, record{"success": true, "already_deleted": true})
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, "tenant lookup failed")
		return
	}
	if tenantID <= 0 || companyID <= 0 || !strings.HasPrefix(schemaName, "tenant_textile_") || schemaName == a.defaultSchema {
		fail(w, http.StatusConflict, "refusing to delete an invalid tenant boundary")
		return
	}

	tx, err := a.db.Begin()
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not start tenant deletion")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DROP SCHEMA IF EXISTS ` + quoteIdent(schemaName) + ` CASCADE`); err != nil {
		fail(w, http.StatusConflict, "operational schema deletion failed")
		return
	}
	if _, err = tx.Exec(`DELETE FROM public.operational_tenants WHERE id=$1 AND external_company_id=$2`, tenantID, companyID); err != nil {
		fail(w, http.StatusConflict, "operational tenant deletion failed")
		return
	}

	rows, err := tx.Query(`SELECT DISTINCT table_name FROM information_schema.columns WHERE table_schema='public' AND column_name='company_id' AND table_name<>'companies' ORDER BY table_name`)
	if err != nil {
		fail(w, http.StatusInternalServerError, "could not inspect financial tenant tables")
		return
	}
	var pending []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			fail(w, http.StatusInternalServerError, "could not inspect financial tenant tables")
			return
		}
		pending = append(pending, table)
	}
	rows.Close()
	for len(pending) > 0 {
		progress := false
		next := make([]string, 0, len(pending))
		for i, table := range pending {
			sp := fmt.Sprintf("tenant_purge_%d", i)
			if _, err = tx.Exec("SAVEPOINT " + sp); err != nil {
				fail(w, http.StatusInternalServerError, "could not create deletion savepoint")
				return
			}
			_, deleteErr := tx.Exec(`DELETE FROM public.`+quoteIdent(table)+` WHERE company_id=$1`, companyID)
			if deleteErr == nil {
				_, _ = tx.Exec("RELEASE SAVEPOINT " + sp)
				progress = true
				continue
			}
			_, _ = tx.Exec("ROLLBACK TO SAVEPOINT " + sp)
			_, _ = tx.Exec("RELEASE SAVEPOINT " + sp)
			var pqErr *pq.Error
			if errors.As(deleteErr, &pqErr) && string(pqErr.Code) == "23503" {
				next = append(next, table)
				continue
			}
			fail(w, http.StatusConflict, "financial tenant deletion failed")
			return
		}
		if !progress {
			fail(w, http.StatusConflict, "financial tenant deletion blocked by an unscoped foreign key")
			return
		}
		pending = next
	}
	if result, err := tx.Exec(`DELETE FROM public.companies WHERE id=$1`, companyID); err != nil {
		fail(w, http.StatusConflict, "financial tenant root deletion failed")
		return
	} else if affected, _ := result.RowsAffected(); affected != 1 {
		fail(w, http.StatusConflict, "financial tenant root changed during deletion")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, "tenant deletion could not be committed")
		return
	}
	a.mu.Lock()
	for token, session := range a.sessions {
		if session.CompanyID == companyID {
			delete(a.sessions, token)
		}
	}
	a.mu.Unlock()
	writeJSON(w, record{"success": true, "company_id": companyID})
}

func (a *app) allocateFinancialCompany(companyName string) (int64, error) {
	companyName = strings.TrimSpace(companyName)
	if a.dialect != "postgres" || companyName == "" {
		return 0, errors.New("postgres and company name are required")
	}
	code := fmt.Sprintf("PANEL-AUTO-%d", time.Now().UnixNano())
	var companyID int64
	if err := a.db.QueryRow(`INSERT INTO public.companies(code,name,is_active) VALUES($1,$2,TRUE) RETURNING id`, code, companyName).Scan(&companyID); err != nil {
		return 0, err
	}
	return companyID, nil
}

func (a *app) ensureFinancialCompany(companyID int64, companyName string) error {
	companyName = strings.TrimSpace(companyName)
	var existingName string
	err := a.queryRow(`SELECT name FROM public.companies WHERE id=?`, companyID).Scan(&existingName)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := a.exec(`INSERT INTO public.companies(id,code,name,is_active) VALUES(?,?,?,TRUE)`, companyID, "PANEL-"+strconv.FormatInt(companyID, 10), companyName); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !strings.EqualFold(strings.TrimSpace(existingName), companyName) {
		return fmt.Errorf("company id %d belongs to a different tenant", companyID)
	}
	_, err = a.db.Exec(`SELECT setval(pg_get_serial_sequence('public.companies','id'), GREATEST((SELECT COALESCE(MAX(id),1) FROM public.companies),1), true)`)
	return err
}

func (a *app) provisionTenantAccess(companyID, accessID int64, companyName, contactName, username, password, role string) error {
	if a.dialect != "postgres" {
		return errors.New("tenant provisioning requires postgres")
	}
	companyName = strings.TrimSpace(companyName)
	contactName = strings.TrimSpace(contactName)
	username = strings.TrimSpace(username)
	if companyID <= 0 || accessID <= 0 || companyName == "" || username == "" || password == "" {
		return errors.New("company, access, username, and password are required")
	}
	if !strings.EqualFold(strings.TrimSpace(role), "admin") {
		role = "viewer"
	} else {
		role = "admin"
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := a.ensureFinancialCompany(companyID, companyName); err != nil {
		return err
	}
	var tenantID int64
	var schemaName string
	err = a.queryRow(`SELECT id,schema_name FROM public.operational_tenants WHERE external_company_id=? AND active=1`, companyID).Scan(&tenantID, &schemaName)
	newTenant := errors.Is(err, sql.ErrNoRows)
	if err != nil && !newTenant {
		return err
	}
	if newTenant {
		schemaName = "tenant_textile_" + strconv.FormatInt(companyID, 10)
		tx, err := a.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`CREATE SCHEMA IF NOT EXISTS ` + quoteIdent(schemaName)); err != nil {
			return err
		}
		if err := tx.QueryRow(`INSERT INTO public.operational_tenants(external_company_id,company_name,schema_name,active) VALUES($1,$2,$3,1) RETURNING id`, companyID, companyName, schemaName).Scan(&tenantID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	platformUsername := fmt.Sprintf("portal_%d_%d", tenantID, accessID)
	provisioned := false
	if newTenant {
		defer func() {
			if provisioned {
				return
			}
			_ = a.setSearchPath(a.defaultSchema)
			_, _ = a.exec(`DELETE FROM public.operational_platform_users WHERE tenant_id=?`, tenantID)
			_, _ = a.db.Exec(`DROP SCHEMA IF EXISTS ` + quoteIdent(schemaName) + ` CASCADE`)
			_, _ = a.exec(`DELETE FROM public.operational_tenants WHERE id=?`, tenantID)
		}()
	}
	if err := a.setSearchPath(schemaName); err != nil {
		return err
	}
	if newTenant {
		if err := a.migrate(); err != nil {
			return err
		}
	}
	_, _ = a.exec(`UPDATE public.operational_tenants SET company_name=? WHERE id=?`, companyName, tenantID)

	var localUserID int64
	createdUser := false
	err = a.queryRow(`SELECT id_user FROM users WHERE LOWER(username)=LOWER(?) ORDER BY id_user LIMIT 1`, username).Scan(&localUserID)
	if errors.Is(err, sql.ErrNoRows) {
		err = a.queryRow(`SELECT local_user_id FROM public.operational_platform_users WHERE tenant_id=? AND portal_access_id=?`, tenantID, accessID).Scan(&localUserID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		var mappedUsers int64
		if err := a.queryRow(`SELECT COUNT(*) FROM public.operational_platform_users WHERE tenant_id=?`, tenantID).Scan(&mappedUsers); err != nil {
			return err
		}
		if mappedUsers == 0 {
			if err := a.queryRow(`SELECT id_user FROM users WHERE role='admin' ORDER BY id_user LIMIT 1`).Scan(&localUserID); err != nil {
				return err
			}
			if _, err := a.exec(`UPDATE users SET username=?,password_hash=?,role=?,is_active=1 WHERE id_user=?`, username, passwordHash, role, localUserID); err != nil {
				return err
			}
		} else {
			if err := a.queryRow(`INSERT INTO users(username,password_hash,role,is_active) VALUES(?,?,?,1) RETURNING id_user`, username, passwordHash, role).Scan(&localUserID); err != nil {
				return err
			}
			createdUser = true
		}
	} else if err != nil {
		return err
	}
	if !createdUser {
		if _, err := a.exec(`UPDATE users SET username=?,password_hash=?,role=?,is_active=1 WHERE id_user=?`, username, passwordHash, role, localUserID); err != nil {
			return err
		}
	}
	if _, err := a.exec(`DELETE FROM public.operational_platform_users WHERE tenant_id=? AND (local_user_id=? OR portal_access_id=?)`, tenantID, localUserID, accessID); err != nil {
		return err
	}
	if _, err := a.exec(`INSERT INTO public.operational_platform_users(tenant_id,local_user_id,portal_access_id,username,password_hash,active) VALUES(?,?,?,?,?,1)`, tenantID, localUserID, accessID, platformUsername, passwordHash); err != nil {
		return err
	}
	provisioned = true
	return nil
}

func (a *app) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, record{"ok": true, "service": "operational-cycle-go", "date": jalaliToday()})
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &p) {
		return
	}
	var id, active, companyID int64
	var username, hash, role, schemaName string
	var err error
	if a.dialect == "postgres" {
		err = a.queryRow(`SELECT pu.local_user_id,pu.username,pu.password_hash,pu.tenant_id,t.schema_name
			FROM public.operational_platform_users pu JOIN public.operational_tenants t ON t.id=pu.tenant_id
			WHERE pu.username=? AND pu.active=1 AND t.active=1`, strings.TrimSpace(p.Username)).Scan(&id, &username, &hash, &companyID, &schemaName)
		if err == nil {
			err = a.setSearchPath(schemaName)
		}
		if err == nil {
			err = a.queryRow(`SELECT role,COALESCE(is_active,1) FROM users WHERE id_user=?`, id).Scan(&role, &active)
		}
	} else {
		err = a.queryRow(`SELECT id_user, username, password_hash, role, COALESCE(is_active,1) FROM users WHERE username=?`, strings.TrimSpace(p.Username)).Scan(&id, &username, &hash, &role, &active)
	}
	if err != nil || active != 1 || !verifyPassword(p.Password, hash) {
		fail(w, http.StatusUnauthorized, "نام کاربری یا رمز عبور معتبر نیست")
		return
	}
	if err := a.createSession(w, r, sessionInfo{UserID: id, CompanyID: companyID, Schema: schemaName, Username: username, Role: role}); err != nil {
		fail(w, http.StatusInternalServerError, "ایجاد نشست امکان‌پذیر نیست")
		return
	}
	menus := a.userMenus(id, role)
	writeJSON(w, record{"success": true, "user": record{"id": id, "username": username, "role": role}, "menus": menus})
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	a.clearSession(w, r)
	writeJSON(w, record{"success": true})
}

func (a *app) sessionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	session, ok := a.currentSession(r)
	if !ok {
		fail(w, http.StatusUnauthorized, "نشست شما معتبر نیست. دوباره وارد شوید.")
		return
	}
	menus := a.userMenus(session.UserID, session.Role)
	if session.Portal {
		menus = a.portalMenus(session.MenuKeys)
	}
	writeJSON(w, record{
		"success": true,
		"user":    record{"id": session.UserID, "username": session.Username, "role": session.Role},
		"menus":   menus,
	})
}

func (a *app) dashboard(w http.ResponseWriter, r *http.Request) {
	today := jalaliToday()
	month := today
	if len(today) >= 7 {
		month = today[:7]
	}
	out := record{
		"nakh_vor_count":    a.count("nakh_vor"),
		"nakh_khor_count":   a.count("nakh_khor"),
		"chelle_count":      a.count("chelle"),
		"gere_count":        a.count("gere"),
		"nakh_salon_net":    scalarFloat(a.db, "SELECT COALESCE(SUM(w_nakh_salon),0) FROM nakh_salon"),
		"salon_count":       a.count("salon"),
		"salon_metr":        scalarFloat(a.db, "SELECT COALESCE(SUM(metr_salon),0) FROM salon"),
		"salon_weight":      scalarFloat(a.db, "SELECT COALESCE(SUM(w_salon),0) FROM salon"),
		"out_invoice_count": a.count("f_khor"),
		"expense_total":     scalarFloat(a.db, "SELECT COALESCE(SUM(mablagh_h_rozmare),0) FROM h_rozmare"),
		"today":             a.productionSummary("tarikh_salon = ?", today),
		"today_by_machine":  a.productionByMachine(today),
		"month":             a.productionSummary("SUBSTR(tarikh_salon,1,7) = ?", month),
		"stock":             a.stockSummary(),
		"yarn_inventory":    a.yarnInventory(),
		"machines":          a.machineStatus(),
		"month_production":  a.monthProduction(month),
		"latest_production": a.latestSalon(8),
		"latest_yarn_exit":  a.latestNakhKhor(8),
		"latest_invoices":   a.latestOutInvoices(8),
		"notifications":     a.notifications(),
		"date":              today,
	}
	writeJSON(w, out)
}

func (a *app) nextSalonID(w http.ResponseWriter, r *http.Request) {
	next := int64(1)
	_ = a.queryRow(`SELECT COALESCE(MAX(id_salon),0)+1 FROM salon`).Scan(&next)
	writeJSON(w, record{"success": true, "next_id": next})
}

func (a *app) consumptionMachines(w http.ResponseWriter, r *http.Request) {
	items, err := a.activeMachineStatus()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, items)
}

func (a *app) lookups(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, record{
		"customers":    a.lookup("mosh_name", "id_mosh_name", "name_mosh"),
		"yarns":        a.lookup("nakh_name", "id_nakh_name", "name_nakh_name"),
		"fabrics":      a.lookup("kala_name", "id_kala_name", "name_kala_name"),
		"warpers":      a.lookup("chellepich", "id_chellepich", "name_chellepich"),
		"beams":        a.lookup("kod_navard", "id_kod_navard", "kod_kod_navard"),
		"tiers":        a.lookup("gerezan", "id_gerezan", "name_gerezan"),
		"costs":        a.lookup("hazine", "id_hazine", "onvan_hazine"),
		"operators":    a.lookup("operator_name", "id_operator", "name_operator"),
		"drivers":      a.lookup("driver_name", "id_driver", "name_driver"),
		"weavers":      a.lookup("weaver_name", "id_weaver", "name_weaver"),
		"serviceTypes": a.lookup("service_type", "id_service_type", "name_service_type"),
		"spareParts":   a.lookup("spare_part", "id_spare_part", "name_spare_part"),
		"hambaftYarn":  distinct(a.db, "SELECT DISTINCT hambaft_nakh_vor FROM nakh_vor WHERE COALESCE(hambaft_nakh_vor,'')<>'' ORDER BY hambaft_nakh_vor"),
		"hamPod":       distinct(a.db, "SELECT DISTINCT ham_nakh_salon FROM nakh_salon WHERE COALESCE(ham_nakh_salon,'')<>'' ORDER BY ham_nakh_salon"),
	})
}

func (a *app) basic(w http.ResponseWriter, r *http.Request) {
	kind := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/basic/"), "/")
	table, idCol, nameCol, ok := basicMap(kind)
	if !ok {
		fail(w, http.StatusNotFound, "نوع اطلاعات اولیه معتبر نیست")
		return
	}
	if r.Method == http.MethodPost {
		var p struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &p) {
			return
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			fail(w, http.StatusBadRequest, "نام را وارد کنید")
			return
		}
		var exists int
		checkStmt := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s=?", table, nameCol)
		if a.dialect == "postgres" {
			checkStmt = fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s=?", quoteIdent(table), quoteIdent(nameCol))
		}
		_ = a.queryRow(checkStmt, p.Name).Scan(&exists)
		if exists == 0 {
			stmt := fmt.Sprintf("INSERT INTO %s (%s) VALUES (?)", table, nameCol)
			if a.dialect == "postgres" {
				stmt = fmt.Sprintf("INSERT INTO %s (%s) VALUES (?)", quoteIdent(table), quoteIdent(nameCol))
			}
			_, err := a.exec(stmt, p.Name)
			if err != nil {
				fail(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	if r.Method == http.MethodDelete {
		id := strings.TrimPrefix(kind, "")
		_ = id
	}
	writeJSON(w, a.lookup(table, idCol, nameCol))
}

func (a *app) nakhVor(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT nv.id_nakh_vor, nv.tarikh_nakh_vor, nv.hambaft_nakh_vor, nv.w_vor_nakh_vor, nv.moshname_nakh_vor, nv.nakh_name_nakh_vor, COALESCE(m.id_mosh_name,0), COALESCE(n.id_nakh_name,0) FROM nakh_vor nv LEFT JOIN mosh_name m ON m.name_mosh=nv.moshname_nakh_vor LEFT JOIN nakh_name n ON n.name_nakh_name=nv.nakh_name_nakh_vor ORDER BY nv.id_nakh_vor DESC LIMIT 200`)
		writeRows(w, rows, err, []string{"id", "tarikh", "hambaft", "weight", "mosh", "nakh", "mosh_id", "nakh_id"})
	case http.MethodPost:
		var p struct {
			ID      int64   `json:"id"`
			Hambaft string  `json:"hambaft"`
			Weight  float64 `json:"weight"`
			MoshID  int64   `json:"mosh_id"`
			NakhID  int64   `json:"nakh_id"`
		}
		if !decode(w, r, &p) {
			return
		}
		mosh, err := a.nameByID("mosh_name", "id_mosh_name", "name_mosh", p.MoshID)
		if err != nil {
			fail(w, 400, "مشتری معتبر نیست")
			return
		}
		nakh, err := a.nameByID("nakh_name", "id_nakh_name", "name_nakh_name", p.NakhID)
		if err != nil {
			fail(w, 400, "نوع نخ معتبر نیست")
			return
		}
		if p.Hambaft == "" || p.Weight <= 0 {
			fail(w, 400, "همبافت و وزن را کامل وارد کنید")
			return
		}
		if p.ID > 0 {
			_, err = a.exec(`UPDATE nakh_vor SET hambaft_nakh_vor=?, w_vor_nakh_vor=?, moshname_nakh_vor=?, nakh_name_nakh_vor=? WHERE id_nakh_vor=?`, p.Hambaft, p.Weight, mosh, nakh, p.ID)
		} else {
			_, err = a.exec(`INSERT INTO nakh_vor (tarikh_nakh_vor,hambaft_nakh_vor,w_vor_nakh_vor,moshname_nakh_vor,nakh_name_nakh_vor) VALUES (?,?,?,?,?)`, jalaliToday(), p.Hambaft, p.Weight, mosh, nakh)
		}
		writeSave(w, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) nakhVorByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	writeSave(w, execErr(a.exec(`DELETE FROM nakh_vor WHERE id_nakh_vor=?`, id)))
}

func (a *app) warehouseYarnBalance(owner, hambaft, yarn string, excludeOutID, excludeSalonID int64) float64 {
	var inbound, outbound, salonNet float64
	_ = a.queryRow(`SELECT COALESCE(SUM(w_vor_nakh_vor),0) FROM nakh_vor
		WHERE moshname_nakh_vor=? AND hambaft_nakh_vor=? AND nakh_name_nakh_vor=?`, owner, hambaft, yarn).Scan(&inbound)
	_ = a.queryRow(`SELECT COALESCE(SUM(ABS(w_vor_nakh_khor)),0) FROM nakh_khor
		WHERE COALESCE(NULLIF(owner_mosh_nakh_khor,''),moshname_nakh_khor)=? AND hambaft_nakh_khor=? AND nakh_name_nakh_khor=? AND id_nakh_khor<>?`, owner, hambaft, yarn, excludeOutID).Scan(&outbound)
	_ = a.queryRow(`SELECT COALESCE(SUM(w_nakh_salon),0) FROM nakh_salon
		WHERE mosh_name_nakh_salon=? AND ham_nakh_salon=? AND COALESCE(nakh_name_nakh_salon,'')=? AND id_nakh_salon<>?`, owner, hambaft, yarn, excludeSalonID).Scan(&salonNet)
	return inbound - outbound - salonNet
}

func (a *app) machineYarnBalance(machine string, chelleID int64, owner, hambaft, yarn string, excludeID int64) float64 {
	var balance float64
	_ = a.queryRow(`SELECT COALESCE(SUM(w_nakh_salon),0) FROM nakh_salon
		WHERE shom_machin_nakh_salon=? AND chelle_id_nakh_salon=? AND mosh_name_nakh_salon=?
		AND ham_nakh_salon=? AND COALESCE(nakh_name_nakh_salon,'')=? AND id_nakh_salon<>?`,
		machine, chelleID, owner, hambaft, yarn, excludeID).Scan(&balance)
	return balance
}

func (a *app) warperYarnAmounts(warper, owner, hambaft, yarn string, excludeChelleID int64) (float64, float64) {
	var sent, returned float64
	_ = a.queryRow(`SELECT COALESCE(SUM(ABS(w_vor_nakh_khor)),0) FROM nakh_khor
		WHERE moshname_nakh_khor=? AND hambaft_nakh_khor=? AND nakh_name_nakh_khor=?
		AND COALESCE(destination_type_nakh_khor,'warper')='warper'
		AND (COALESCE(NULLIF(owner_mosh_nakh_khor,''),moshname_nakh_khor)=? OR COALESCE(NULLIF(owner_mosh_nakh_khor,''),moshname_nakh_khor)=moshname_nakh_khor)`,
		warper, hambaft, yarn, owner).Scan(&sent)
	_ = a.queryRow(`SELECT COALESCE(SUM(w_chelle),0) FROM chelle
		WHERE pich_chelle=? AND mosh_chelle=? AND hambaft_chelle=? AND nakh_chelle=? AND id_chelle<>?`,
		warper, owner, hambaft, yarn, excludeChelleID).Scan(&returned)
	return sent, returned
}

func (a *app) chelle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT c.id_chelle, c.tarikh_chelle, c.shom_chelle, c.nakh_chelle, c.w_chelle, c.pich_chelle, c.mosh_chelle, c.hambaft_chelle, c.codnavard_chelle, COALESCE(c.machin_chelle,''), COALESCE(n.id_nakh_name,0), COALESCE(cp.id_chellepich,0), COALESCE(m.id_mosh_name,0), COALESCE(k.id_kod_navard,0) FROM chelle c LEFT JOIN nakh_name n ON n.name_nakh_name=c.nakh_chelle LEFT JOIN chellepich cp ON cp.name_chellepich=c.pich_chelle LEFT JOIN mosh_name m ON m.name_mosh=c.mosh_chelle LEFT JOIN kod_navard k ON k.kod_kod_navard=c.codnavard_chelle ORDER BY c.id_chelle DESC LIMIT 200`)
		writeRows(w, rows, err, []string{"id", "tarikh", "shom_chelle", "nakh", "weight", "pich", "mosh", "hambaft", "kod_navard", "machine", "nakh_id", "pich_id", "mosh_id", "kod_navard_id"})
	case http.MethodPost:
		var p struct {
			ID          int64   `json:"id"`
			ShomChelle  string  `json:"shom_chelle"`
			NakhID      int64   `json:"nakh_id"`
			Weight      float64 `json:"weight"`
			PichID      int64   `json:"pich_id"`
			MoshID      int64   `json:"mosh_id"`
			Hambaft     string  `json:"hambaft"`
			KodNavardID int64   `json:"kod_navard_id"`
		}
		if !decode(w, r, &p) {
			return
		}
		nakh, err1 := a.nameByID("nakh_name", "id_nakh_name", "name_nakh_name", p.NakhID)
		pich, err2 := a.nameByID("chellepich", "id_chellepich", "name_chellepich", p.PichID)
		mosh, err3 := a.nameByID("mosh_name", "id_mosh_name", "name_mosh", p.MoshID)
		kod, err4 := a.nameByID("kod_navard", "id_kod_navard", "kod_kod_navard", p.KodNavardID)
		if errors.Join(err1, err2, err3, err4) != nil || p.ShomChelle == "" || p.Hambaft == "" || p.Weight <= 0 {
			fail(w, 400, "اطلاعات چله کامل نیست")
			return
		}
		p.ShomChelle = strings.TrimSpace(p.ShomChelle)
		p.Hambaft = strings.TrimSpace(p.Hambaft)
		var duplicate int64
		_ = a.queryRow(`SELECT COUNT(*) FROM chelle WHERE shom_chelle=? AND id_chelle<>?`, p.ShomChelle, p.ID).Scan(&duplicate)
		if duplicate > 0 {
			fail(w, 400, "شماره چله تکراری است؛ برای ردیابی صحیح یک شماره یکتا وارد کنید")
			return
		}
		sent, returned := a.warperYarnAmounts(pich, mosh, p.Hambaft, nakh, p.ID)
		if sent <= 0 {
			fail(w, 400, "برای این مالک، هم‌بافت و نوع نخ هیچ خروجی به چله‌پیچ انتخاب‌شده ثبت نشده است")
			return
		}
		if returned+p.Weight > sent+0.001 {
			fail(w, 400, fmt.Sprintf("وزن چله از مانده نخ نزد چله‌پیچ بیشتر است؛ مانده قابل ثبت %.3f کیلوگرم", sent-returned))
			return
		}
		var err error
		if p.ID > 0 {
			_, err = a.exec(`UPDATE chelle SET shom_chelle=?, nakh_chelle=?, w_chelle=?, pich_chelle=?, mosh_chelle=?, hambaft_chelle=?, codnavard_chelle=? WHERE id_chelle=?`, p.ShomChelle, nakh, p.Weight, pich, mosh, p.Hambaft, kod, p.ID)
		} else {
			_, err = a.exec(`INSERT INTO chelle (tarikh_chelle,shom_chelle,nakh_chelle,w_chelle,pich_chelle,mosh_chelle,hambaft_chelle,codnavard_chelle,machin_chelle) VALUES (?,?,?,?,?,?,?,?,?)`, jalaliToday(), p.ShomChelle, nakh, p.Weight, pich, mosh, p.Hambaft, kod, "")
		}
		writeSave(w, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) chelleByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	tx, err := a.db.Begin()
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM chelle_formul WHERE chelle_id=?`, id)
	}
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM chelle WHERE id_chelle=?`, id)
	}
	if err != nil {
		if tx != nil {
			_ = tx.Rollback()
		}
		writeSave(w, err)
		return
	}
	writeSave(w, tx.Commit())
}

func (a *app) gere(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("available") == "1" {
			rows, err := a.query(`SELECT id_chelle, shom_chelle, w_chelle, hambaft_chelle FROM chelle WHERE COALESCE(machin_chelle,'')='' ORDER BY id_chelle DESC`)
			writeRows(w, rows, err, []string{"id", "shom_chelle", "weight", "hambaft"})
			return
		}
		rows, err := a.query(`SELECT g.id_gere, g.tarikh_gere, g.name_gere, g.shom_chelle_gere, g.machin_gere, COALESCE(gr.id_gerezan,0), COALESCE(g.chelle_id_gere,0) FROM gere g LEFT JOIN gerezan gr ON gr.name_gerezan=g.name_gere ORDER BY COALESCE(g.tarikh_gere,'') DESC, g.id_gere DESC LIMIT 200`)
		writeRows(w, rows, err, []string{"id", "tarikh", "name_gere", "shom_chelle", "machine", "gerezan_id", "chelle_id"})
	case http.MethodPost:
		var p struct {
			ID        int64  `json:"id"`
			GerezanID int64  `json:"gerezan_id"`
			ChelleID  int64  `json:"chelle_id"`
			Machine   string `json:"machine"`
		}
		if !decode(w, r, &p) {
			return
		}
		machine, machineErr := canonicalMachineNumber(p.Machine)
		if machineErr != nil {
			fail(w, 400, machineErr.Error())
			return
		}
		p.Machine = machine
		gerezan, err := a.nameByID("gerezan", "id_gerezan", "name_gerezan", p.GerezanID)
		if err != nil || p.ChelleID == 0 || strings.TrimSpace(p.Machine) == "" {
			fail(w, 400, "اطلاعات گره کامل نیست")
			return
		}
		var shom string
		var oldChelleID int64
		var oldMachine string
		if p.ID > 0 {
			_ = a.queryRow(`SELECT COALESCE(chelle_id_gere,0),COALESCE(machin_gere,'') FROM gere WHERE id_gere=?`, p.ID).Scan(&oldChelleID, &oldMachine)
		}
		var assignedMachine string
		if err := a.queryRow(`SELECT shom_chelle,COALESCE(machin_chelle,'') FROM chelle WHERE id_chelle=?`, p.ChelleID).Scan(&shom, &assignedMachine); err != nil {
			fail(w, 400, "چله معتبر نیست")
			return
		}
		if assignedMachine != "" && assignedMachine != p.Machine && oldChelleID != p.ChelleID {
			fail(w, 400, "این چله هم‌اکنون روی ماشین دیگری فعال است")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if p.ID > 0 {
			if oldChelleID > 0 && oldChelleID != p.ChelleID {
				_, _ = txExec(a.dialect, tx, `UPDATE chelle SET machin_chelle='' WHERE id_chelle=?`, oldChelleID)
			}
			_, err = txExec(a.dialect, tx, `UPDATE gere SET name_gere=?, shom_chelle_gere=?, machin_gere=?, tarikh_gere=?, chelle_id_gere=? WHERE id_gere=?`, gerezan, shom, p.Machine, jalaliToday(), p.ChelleID, p.ID)
		} else {
			_, err = txExec(a.dialect, tx, `INSERT INTO gere (name_gere, shom_chelle_gere, machin_gere, tarikh_gere, chelle_id_gere) VALUES (?,?,?,?,?)`, gerezan, shom, p.Machine, jalaliToday(), p.ChelleID)
		}
		if err == nil && oldMachine != "" && oldMachine != p.Machine {
			err = a.activateLatestChelleForMachineTx(tx, oldMachine, 0)
		}
		if err == nil {
			err = a.activateLatestChelleForMachineTx(tx, p.Machine, p.ChelleID)
		}
		if err == nil {
			_, err = txExec(a.dialect, tx, `UPDATE chelle_formul SET machine=?,shom_chelle=?,updated_at=? WHERE chelle_id=?`, p.Machine, shom, time.Now().UTC().Format(time.RFC3339Nano), p.ChelleID)
		}
		if err != nil {
			_ = tx.Rollback()
			fail(w, 500, err.Error())
			return
		}
		writeSave(w, tx.Commit())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) gereByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	var machine string
	_ = a.queryRow(`SELECT COALESCE(machin_gere,'') FROM gere WHERE id_gere=?`, id).Scan(&machine)
	tx, err := a.db.Begin()
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM gere WHERE id_gere=?`, id)
	}
	if err == nil {
		err = a.activateLatestChelleForMachineTx(tx, machine, 0)
	}
	if err != nil {
		_ = tx.Rollback()
		fail(w, 500, err.Error())
		return
	}
	writeSave(w, tx.Commit())
}

func (a *app) nakhSalon(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("chelles") == "1" {
			rows, err := a.query(`SELECT id_chelle, shom_chelle, machin_chelle, w_chelle, hambaft_chelle, COALESCE(mosh_chelle,''), COALESCE(nakh_chelle,'') FROM chelle WHERE COALESCE(machin_chelle,'')<>'' ORDER BY id_chelle DESC`)
			writeRows(w, rows, err, []string{"id", "shom_chelle", "machine", "weight", "hambaft", "mosh_name", "nakh_name"})
			return
		}
		rows, err := a.query(`SELECT ns.id_nakh_salon, ns.tarikh_nakh_salon, ns.shom_machin_nakh_salon, ns.ham_nakh_salon, ns.w_nakh_salon, ns.shom_chelle_nakh_salon, ns.mosh_name_nakh_salon, ns.vor_khor_nakh_salon, COALESCE(ns.chelle_id_nakh_salon,0), COALESCE(ns.nakh_name_nakh_salon,'') FROM nakh_salon ns ORDER BY ns.id_nakh_salon DESC LIMIT 200`)
		writeRows(w, rows, err, []string{"id", "tarikh", "machine", "ham_nakh", "weight", "shom_chelle", "mosh_name", "vor_khor", "chelle_id", "nakh_name"})
	case http.MethodPost:
		var p struct {
			ID       int64   `json:"id"`
			Machine  string  `json:"machine"`
			HamNakh  string  `json:"ham_nakh"`
			Weight   float64 `json:"weight"`
			ChelleID int64   `json:"chelle_id"`
			MoshName string  `json:"mosh_name"`
			NakhName string  `json:"nakh_name"`
			VorKhor  string  `json:"vor_khor"`
		}
		if !decode(w, r, &p) {
			return
		}
		machine, machineErr := canonicalMachineNumber(p.Machine)
		if machineErr != nil {
			fail(w, 400, machineErr.Error())
			return
		}
		p.Machine = machine
		p.HamNakh = strings.TrimSpace(p.HamNakh)
		p.MoshName = strings.TrimSpace(p.MoshName)
		p.NakhName = strings.TrimSpace(p.NakhName)
		if p.Machine == "" || p.HamNakh == "" || p.Weight <= 0 || p.ChelleID == 0 || p.MoshName == "" || p.NakhName == "" || (p.VorKhor != "vorud" && p.VorKhor != "khoroj") {
			fail(w, 400, "اطلاعات نخ سالن کامل نیست")
			return
		}
		var shom, activeMachine, chelleOwner string
		if err := a.queryRow(`SELECT shom_chelle,COALESCE(machin_chelle,''),COALESCE(mosh_chelle,'') FROM chelle WHERE id_chelle=?`, p.ChelleID).Scan(&shom, &activeMachine, &chelleOwner); err != nil {
			fail(w, 400, "چله معتبر نیست")
			return
		}
		activeMachine = normalizeStoredMachine(activeMachine)
		if activeMachine == "" || activeMachine != p.Machine {
			fail(w, 400, "چله انتخاب‌شده روی این ماشین فعال نیست")
			return
		}
		if !sameText(chelleOwner, p.MoshName) {
			fail(w, 400, "مالک نخ با مالک چله فعال تطابق ندارد")
			return
		}
		// Hall yarn is weft yarn. Its hambaft is an independent inventory
		// dimension and must not be forced to match the warp/beam hambaft.
		var ownerExists, yarnExists int64
		_ = a.queryRow(`SELECT COUNT(*) FROM mosh_name WHERE name_mosh=?`, p.MoshName).Scan(&ownerExists)
		_ = a.queryRow(`SELECT COUNT(*) FROM nakh_name WHERE name_nakh_name=?`, p.NakhName).Scan(&yarnExists)
		if ownerExists == 0 || yarnExists == 0 {
			fail(w, 400, "مالک نخ یا نوع نخ در اطلاعات اولیه معتبر نیست")
			return
		}
		if p.VorKhor == "vorud" {
			available := a.warehouseYarnBalance(p.MoshName, p.HamNakh, p.NakhName, 0, p.ID)
			if p.Weight > available+0.001 {
				fail(w, 400, fmt.Sprintf("موجودی انبار این نخ کافی نیست؛ موجودی قابل تخصیص %.3f کیلوگرم", available))
				return
			}
		} else {
			assigned := a.machineYarnBalance(p.Machine, p.ChelleID, p.MoshName, p.HamNakh, p.NakhName, p.ID)
			if p.Weight > assigned+0.001 {
				fail(w, 400, fmt.Sprintf("وزن مرجوعی از مانده پود روی ماشین بیشتر است؛ مانده قابل مرجوع %.3f کیلوگرم", assigned))
				return
			}
		}
		finalWeight := p.Weight
		if p.VorKhor == "khoroj" {
			finalWeight = -p.Weight
		}
		var err error
		if p.ID > 0 {
			_, err = a.exec(`UPDATE nakh_salon SET shom_machin_nakh_salon=?, ham_nakh_salon=?, w_nakh_salon=?, shom_chelle_nakh_salon=?, mosh_name_nakh_salon=?, vor_khor_nakh_salon=?, nakh_name_nakh_salon=?, chelle_id_nakh_salon=? WHERE id_nakh_salon=?`, p.Machine, p.HamNakh, finalWeight, shom, p.MoshName, p.VorKhor, p.NakhName, p.ChelleID, p.ID)
		} else {
			_, err = a.exec(`INSERT INTO nakh_salon (tarikh_nakh_salon, shom_machin_nakh_salon, ham_nakh_salon, w_nakh_salon, shom_chelle_nakh_salon, mosh_name_nakh_salon, vor_khor_nakh_salon, nakh_name_nakh_salon, chelle_id_nakh_salon) VALUES (?,?,?,?,?,?,?,?,?)`, jalaliToday(), p.Machine, p.HamNakh, finalWeight, shom, p.MoshName, p.VorKhor, p.NakhName, p.ChelleID)
		}
		writeSave(w, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) nakhSalonByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	writeSave(w, execErr(a.exec(`DELETE FROM nakh_salon WHERE id_nakh_salon=?`, id)))
}

func (a *app) nakhKhor(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("inventory") == "1" {
			writeJSON(w, a.yarnInventory())
			return
		}
		rows, err := a.query(`SELECT id_nakh_khor, tarikh_nakh_khor, hambaft_nakh_khor, ABS(COALESCE(w_vor_nakh_khor,0)), moshname_nakh_khor, nakh_name_nakh_khor, COALESCE(NULLIF(owner_mosh_nakh_khor,''),moshname_nakh_khor), COALESCE(destination_type_nakh_khor,'warper') FROM nakh_khor ORDER BY id_nakh_khor DESC LIMIT 300`)
		writeRows(w, rows, err, []string{"id", "tarikh", "hambaft", "weight", "mosh", "nakh", "owner_mosh", "destination_type"})
	case http.MethodPost:
		var p struct {
			ID              int64   `json:"id"`
			Hambaft         string  `json:"hambaft"`
			Weight          float64 `json:"weight"`
			MoshName        string  `json:"mosh_name"`
			OwnerMosh       string  `json:"owner_mosh"`
			NakhName        string  `json:"nakh_name"`
			DestinationType string  `json:"destination_type"`
		}
		if !decode(w, r, &p) {
			return
		}
		p.Hambaft = strings.TrimSpace(p.Hambaft)
		p.MoshName = strings.TrimSpace(p.MoshName)
		p.OwnerMosh = strings.TrimSpace(p.OwnerMosh)
		p.NakhName = strings.TrimSpace(p.NakhName)
		p.DestinationType = strings.TrimSpace(p.DestinationType)
		if p.DestinationType == "" {
			p.DestinationType = "warper"
		}
		if p.DestinationType != "warper" && p.DestinationType != "other" {
			fail(w, 400, "نوع مقصد خروج نخ باید چله‌پیچ یا مشتری/مصرف دیگر باشد")
			return
		}
		if p.Hambaft == "" || p.Weight <= 0 || p.MoshName == "" || p.OwnerMosh == "" || p.NakhName == "" {
			fail(w, 400, "اطلاعات خروج نخ کامل نیست")
			return
		}
		available := a.warehouseYarnBalance(p.OwnerMosh, p.Hambaft, p.NakhName, p.ID, 0)
		if p.Weight > available+0.001 {
			fail(w, 400, fmt.Sprintf("موجودی انبار این نخ کافی نیست؛ موجودی قابل خروج %.3f کیلوگرم", available))
			return
		}
		var err error
		if p.ID > 0 {
			_, err = a.exec(`UPDATE nakh_khor SET hambaft_nakh_khor=?, w_vor_nakh_khor=?, moshname_nakh_khor=?, nakh_name_nakh_khor=?, owner_mosh_nakh_khor=?, destination_type_nakh_khor=? WHERE id_nakh_khor=?`, p.Hambaft, -p.Weight, p.MoshName, p.NakhName, p.OwnerMosh, p.DestinationType, p.ID)
		} else {
			_, err = a.exec(`INSERT INTO nakh_khor (tarikh_nakh_khor,hambaft_nakh_khor,w_vor_nakh_khor,moshname_nakh_khor,nakh_name_nakh_khor,owner_mosh_nakh_khor,destination_type_nakh_khor) VALUES (?,?,?,?,?,?,?)`, jalaliToday(), p.Hambaft, -p.Weight, p.MoshName, p.NakhName, p.OwnerMosh, p.DestinationType)
		}
		writeSave(w, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) nakhKhorByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	writeSave(w, execErr(a.exec(`DELETE FROM nakh_khor WHERE id_nakh_khor=?`, id)))
}

func (a *app) emptyBeamOut(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`
			SELECT e.id_empty_beam_out, COALESCE(e.tarikh_empty_beam_out,''), COALESCE(e.kod_navard,''),
			       COALESCE(e.chellepich_name,''), COALESCE(e.description,''),
			       COALESCE(k.id_kod_navard,0), COALESCE(cp.id_chellepich,0),
			       COALESCE((SELECT c.tarikh_chelle FROM chelle c
			         WHERE c.codnavard_chelle=e.kod_navard AND c.pich_chelle=e.chellepich_name
			           AND COALESCE(c.tarikh_chelle,'')>=COALESCE(e.tarikh_empty_beam_out,'')
			         ORDER BY c.tarikh_chelle DESC, c.id_chelle DESC LIMIT 1),'') AS return_date,
			       COALESCE((SELECT c.shom_chelle FROM chelle c
			         WHERE c.codnavard_chelle=e.kod_navard AND c.pich_chelle=e.chellepich_name
			           AND COALESCE(c.tarikh_chelle,'')>=COALESCE(e.tarikh_empty_beam_out,'')
			         ORDER BY c.tarikh_chelle DESC, c.id_chelle DESC LIMIT 1),'') AS return_chelle
			FROM empty_beam_out e
			LEFT JOIN kod_navard k ON k.kod_kod_navard=e.kod_navard
			LEFT JOIN chellepich cp ON cp.name_chellepich=e.chellepich_name
			ORDER BY e.id_empty_beam_out DESC LIMIT 300`)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		defer rows.Close()
		items := []record{}
		for rows.Next() {
			var id, beamID, warperID int64
			var date, beam, warper, desc, returnDate, returnChelle string
			if err := rows.Scan(&id, &date, &beam, &warper, &desc, &beamID, &warperID, &returnDate, &returnChelle); err != nil {
				fail(w, 500, err.Error())
				return
			}
			status := "نزد چله‌پیچ"
			if returnDate != "" || returnChelle != "" {
				status = "برگشته"
			}
			items = append(items, record{
				"id": id, "tarikh": date, "beam": beam, "warper": warper, "description": desc,
				"beam_id": beamID, "warper_id": warperID, "return_date": returnDate,
				"return_chelle": returnChelle, "status": status,
			})
		}
		writeJSON(w, items)
	case http.MethodPost:
		var p struct {
			ID          int64  `json:"id"`
			BeamID      int64  `json:"beam_id"`
			WarperID    int64  `json:"warper_id"`
			BeamCode    string `json:"beam"`
			WarperName  string `json:"warper"`
			Description string `json:"description"`
		}
		if !decode(w, r, &p) {
			return
		}
		beam := strings.TrimSpace(p.BeamCode)
		warper := strings.TrimSpace(p.WarperName)
		var err error
		if p.BeamID > 0 {
			beam, err = a.nameByID("kod_navard", "id_kod_navard", "kod_kod_navard", p.BeamID)
		}
		if err == nil && p.WarperID > 0 {
			warper, err = a.nameByID("chellepich", "id_chellepich", "name_chellepich", p.WarperID)
		}
		if err != nil || beam == "" || warper == "" {
			fail(w, 400, "اطلاعات خروج نورد خالی کامل نیست")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer tx.Rollback()
		if a.dialect == "postgres" {
			if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, beam); err != nil {
				fail(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		var unresolved int
		err = txQueryRow(a.dialect, tx, `
			SELECT COUNT(*)
			FROM empty_beam_out e
			WHERE e.kod_navard=? AND e.id_empty_beam_out<>?
			  AND NOT EXISTS (
				SELECT 1 FROM chelle c
				WHERE c.codnavard_chelle=e.kod_navard
				  AND c.pich_chelle=e.chellepich_name
				  AND COALESCE(c.tarikh_chelle,'')>=COALESCE(e.tarikh_empty_beam_out,'')
			  )
		`, beam, p.ID).Scan(&unresolved)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if unresolved > 0 {
			fail(w, http.StatusConflict, "این نورد هنوز نزد چله‌پیچ است و خروج تکراری مجاز نیست")
			return
		}
		if p.ID > 0 {
			_, err = txExec(a.dialect, tx, `UPDATE empty_beam_out SET kod_navard=?, chellepich_name=?, description=? WHERE id_empty_beam_out=?`, beam, warper, strings.TrimSpace(p.Description), p.ID)
		} else {
			_, err = txExec(a.dialect, tx, `INSERT INTO empty_beam_out (tarikh_empty_beam_out,kod_navard,chellepich_name,description) VALUES (?,?,?,?)`, jalaliToday(), beam, warper, strings.TrimSpace(p.Description))
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err = tx.Commit(); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, record{"success": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) emptyBeamOutByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	writeSave(w, execErr(a.exec(`DELETE FROM empty_beam_out WHERE id_empty_beam_out=?`, id)))
}

type recentMachineChelle struct {
	ID   int64
	Shom string
}

// recentMachineChelles returns the same two-beam window that is offered by the
// production-hall UI. A previous beam can still have a few final rolls to be
// registered after the next beam is tied to the machine, so checking only the
// current value of chelle.machin_chelle incorrectly rejects that valid beam.
func (a *app) recentMachineChelles(machine string, limit int) []recentMachineChelle {
	where, args := machineWhere("g.machin_gere", machine)
	rows, err := a.query(`SELECT COALESCE(g.chelle_id_gere,0),COALESCE(g.shom_chelle_gere,'')
		FROM gere g WHERE `+where+` AND COALESCE(g.shom_chelle_gere,'')<>''
		ORDER BY COALESCE(g.tarikh_gere,'') DESC,g.id_gere DESC`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	items := []recentMachineChelle{}
	seen := map[string]bool{}
	for rows.Next() {
		var item recentMachineChelle
		if rows.Scan(&item.ID, &item.Shom) != nil {
			continue
		}
		item.Shom = strings.TrimSpace(item.Shom)
		key := strings.ToLower(item.Shom)
		if item.Shom == "" || seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, item)
		if len(items) == limit {
			break
		}
	}
	return items
}

func (a *app) isRecentMachineChelle(machine string, chelleID int64, shom string) bool {
	for _, item := range a.recentMachineChelles(machine, 2) {
		if chelleID > 0 && item.ID == chelleID {
			return true
		}
		if sameText(item.Shom, shom) {
			return true
		}
	}
	return false
}

func (a *app) productionChelleInfo(machine string, chelleID int64, shom string) (int64, string, string, error) {
	machine = normalizeStoredMachine(machine)
	shom = strings.TrimSpace(shom)
	query := `SELECT id_chelle,COALESCE(hambaft_chelle,''),shom_chelle,COALESCE(machin_chelle,'') FROM chelle`
	args := []any{}
	if chelleID > 0 {
		query += ` WHERE id_chelle=?`
		args = append(args, chelleID)
	} else {
		query += ` WHERE shom_chelle=? ORDER BY id_chelle DESC`
		args = append(args, shom)
	}
	rows, err := a.query(query, args...)
	if err != nil {
		return 0, "", "", err
	}
	type chelleCandidate struct {
		id              int64
		hambaft         string
		shom            string
		assignedMachine string
	}
	candidates := []chelleCandidate{}
	for rows.Next() {
		var candidate chelleCandidate
		if err := rows.Scan(&candidate.id, &candidate.hambaft, &candidate.shom, &candidate.assignedMachine); err != nil {
			_ = rows.Close()
			return 0, "", "", err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, "", "", err
	}
	if err := rows.Close(); err != nil {
		return 0, "", "", err
	}

	// PostgreSQL production keeps a single connection so the tenant search_path
	// cannot leak between requests. The candidate rows must be closed before the
	// recent-beam query starts, otherwise the second query waits forever for the
	// connection held by the first result set and blocks every operational API.
	recent := a.recentMachineChelles(machine, 2)
	for _, candidate := range candidates {
		if normalizeStoredMachine(candidate.assignedMachine) == machine {
			return candidate.id, candidate.hambaft, candidate.shom, nil
		}
		for _, item := range recent {
			if (candidate.id > 0 && item.ID == candidate.id) || sameText(item.Shom, candidate.shom) {
				return candidate.id, candidate.hambaft, candidate.shom, nil
			}
		}
	}
	return 0, "", "", sql.ErrNoRows
}

func (a *app) activeChelleInfo(machine, shom string) (int64, string, error) {
	id, hambaft, _, err := a.productionChelleInfo(machine, 0, shom)
	return id, hambaft, err
}

type chelleFormula struct {
	ChelleID   int64
	Machine    string
	ShomChelle string
	Kala       string
	HamChelle  string
	HamPod     string
	TarPercent float64
	PodPercent float64
	Tozih      string
	Configured bool
	Source     string
}

func normalizedFormula(tar, pod float64) (float64, float64, bool) {
	if tar < 0 || pod < 0 {
		return 0, 0, false
	}
	total := tar + pod
	if total <= 0 {
		return 0, 0, false
	}
	return tar * 100 / total, pod * 100 / total, true
}

func formulaIsValid(tar, pod float64) bool {
	return tar >= 0 && pod >= 0 && absFloat(tar+pod-100) <= 0.001
}

func (a *app) resolveChelleFormula(machine, shom, kala, hamChelle, hamPod string) (chelleFormula, error) {
	machine = strings.TrimSpace(machine)
	shom = strings.TrimSpace(shom)
	kala = strings.TrimSpace(kala)
	hamChelle = strings.TrimSpace(hamChelle)
	hamPod = strings.TrimSpace(hamPod)
	chelleID, activeHambaft, err := a.activeChelleInfo(machine, shom)
	if err != nil {
		return chelleFormula{}, err
	}
	if hamChelle == "" {
		hamChelle = strings.TrimSpace(activeHambaft)
	}
	result := chelleFormula{
		ChelleID: chelleID, Machine: machine, ShomChelle: shom, Kala: kala,
		HamChelle: hamChelle, HamPod: hamPod,
		TarPercent: 50, PodPercent: 50,
	}
	err = a.queryRow(`SELECT COALESCE(tar_percent,50),COALESCE(pod_percent,50),COALESCE(tozih_formul,'')
		FROM chelle_formul
		WHERE chelle_id=? AND kala_name=? AND ham_chelle=? AND ham_pod=?
		ORDER BY id_formul DESC LIMIT 1`, chelleID, kala, hamChelle, hamPod).
		Scan(&result.TarPercent, &result.PodPercent, &result.Tozih)
	if err == nil {
		result.TarPercent, result.PodPercent, _ = normalizedFormula(result.TarPercent, result.PodPercent)
		result.Configured = true
		result.Source = "beam"
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return chelleFormula{}, err
	}
	if kala != "" && hamChelle != "" && hamPod != "" {
		err = a.queryRow(`SELECT COALESCE(tar_percent,50),COALESCE(pod_percent,50),COALESCE(tozih_formul,'')
			FROM chelle_formul
			WHERE kala_name=? AND ham_chelle=? AND ham_pod=?
			ORDER BY updated_at DESC,id_formul DESC LIMIT 1`, kala, hamChelle, hamPod).
			Scan(&result.TarPercent, &result.PodPercent, &result.Tozih)
		if err == nil {
			result.TarPercent, result.PodPercent, _ = normalizedFormula(result.TarPercent, result.PodPercent)
			result.Configured = true
			result.Source = "same_hambaft"
			return result, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return chelleFormula{}, err
		}
	}
	_ = a.queryRow(`SELECT COALESCE(tar_percent,50),COALESCE(pod_percent,50)
		FROM machine_formul WHERE machine=?`, machine).Scan(&result.TarPercent, &result.PodPercent)
	result.TarPercent, result.PodPercent, _ = normalizedFormula(result.TarPercent, result.PodPercent)
	result.Source = "machine_default"
	return result, nil
}

func (a *app) saveChelleFormulaTx(tx *sql.Tx, formula chelleFormula) error {
	formula.Machine = strings.TrimSpace(formula.Machine)
	formula.ShomChelle = strings.TrimSpace(formula.ShomChelle)
	formula.Kala = strings.TrimSpace(formula.Kala)
	formula.HamChelle = strings.TrimSpace(formula.HamChelle)
	formula.HamPod = strings.TrimSpace(formula.HamPod)
	if formula.ChelleID <= 0 || formula.Machine == "" || formula.ShomChelle == "" || formula.Kala == "" ||
		formula.HamChelle == "" || formula.HamPod == "" ||
		!formulaIsValid(formula.TarPercent, formula.PodPercent) {
		return errors.New("beam production formula is incomplete or invalid")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := txExec(a.dialect, tx, `INSERT INTO chelle_formul
		(chelle_id,machine,shom_chelle,kala_name,ham_chelle,ham_pod,tar_percent,pod_percent,tozih_formul,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(chelle_id,kala_name,ham_chelle,ham_pod) DO UPDATE SET
		machine=excluded.machine,shom_chelle=excluded.shom_chelle,
		tar_percent=excluded.tar_percent,pod_percent=excluded.pod_percent,
		tozih_formul=excluded.tozih_formul,updated_at=excluded.updated_at`,
		formula.ChelleID, formula.Machine, formula.ShomChelle, formula.Kala, formula.HamChelle, formula.HamPod,
		formula.TarPercent, formula.PodPercent, formula.Tozih, now, now)
	return err
}

func (a *app) salonFormula(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		machine := strings.TrimSpace(r.URL.Query().Get("machine"))
		shom := strings.TrimSpace(r.URL.Query().Get("shom_chelle"))
		hamChelle := strings.TrimSpace(r.URL.Query().Get("ham_chelle"))
		hamPod := strings.TrimSpace(r.URL.Query().Get("ham_pod"))
		kalaID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("kala_id")), 10, 64)
		kala, err := a.nameByID("kala_name", "id_kala_name", "name_kala_name", kalaID)
		if err != nil || machine == "" || shom == "" {
			fail(w, http.StatusBadRequest, "ماشین، چله و نام پارچه را مشخص کنید")
			return
		}
		formula, err := a.resolveChelleFormula(machine, shom, kala, hamChelle, hamPod)
		if err != nil {
			fail(w, http.StatusBadRequest, "چله انتخاب‌شده روی این ماشین فعال نیست")
			return
		}
		writeJSON(w, record{
			"success": true, "configured": formula.Configured,
			"chelle_id": formula.ChelleID, "machine": formula.Machine,
			"shom_chelle": formula.ShomChelle, "kala": formula.Kala,
			"ham_chelle": formula.HamChelle, "ham_pod": formula.HamPod,
			"tar_percent": formula.TarPercent, "pod_percent": formula.PodPercent,
			"tozih": formula.Tozih, "source": formula.Source,
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) salon(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT s.id_salon, s.tarikh_salon, s.metr_salon, s.w_salon, s.machin_salon, s.user_salon, s.kala_salon, s.ham_pod_salon, s.ham_chelle_salon, s.shom_chelle_salon, COALESCE(k.id_kala_name,0), COALESCE(s.chelle_id_salon,0), COALESCE(s.tar_percent_salon,50), COALESCE(s.pod_percent_salon,50) FROM salon s LEFT JOIN kala_name k ON k.name_kala_name=s.kala_salon ORDER BY s.id_salon DESC LIMIT 200`)
		writeRows(w, rows, err, []string{"id", "tarikh", "metr", "weight", "machine", "user", "kala", "ham_pod", "ham_chelle", "shom_chelle", "kala_id", "chelle_id", "tar_percent", "pod_percent"})
	case http.MethodPost:
		var p struct {
			ID               int64   `json:"id"`
			Metr             float64 `json:"metr"`
			Weight           float64 `json:"weight"`
			Machine          string  `json:"machine"`
			KalaID           int64   `json:"kala_id"`
			HamPod           string  `json:"ham_pod"`
			HamChelle        string  `json:"ham_chelle"`
			ShomChelle       string  `json:"shom_chelle"`
			ChelleID         int64   `json:"chelle_id"`
			User             string  `json:"user"`
			TarPercent       float64 `json:"tar_percent"`
			PodPercent       float64 `json:"pod_percent"`
			FormulaConfirmed bool    `json:"formula_confirmed"`
		}
		if !decode(w, r, &p) {
			return
		}
		machine, machineErr := canonicalMachineNumber(p.Machine)
		if machineErr != nil {
			fail(w, 400, machineErr.Error())
			return
		}
		p.Machine = machine
		kala, err := a.nameByID("kala_name", "id_kala_name", "name_kala_name", p.KalaID)
		if err != nil || p.Metr <= 0 || p.Weight <= 0 || p.Machine == "" || p.HamPod == "" || p.HamChelle == "" || p.ShomChelle == "" {
			fail(w, 400, "اطلاعات تولید کامل نیست")
			return
		}
		p.ShomChelle = strings.TrimSpace(p.ShomChelle)
		activeChelleID, activeHambaft, activeShom := int64(0), "", ""
		var activeErr error
		activeChelleID, activeHambaft, activeShom, activeErr = a.productionChelleInfo(p.Machine, p.ChelleID, p.ShomChelle)
		if activeErr != nil {
			fail(w, 400, "چله انتخاب‌شده روی این ماشین فعال نیست؛ ابتدا ثبت گره را کنترل کنید")
			return
		}
		if p.ShomChelle != "" && !sameText(p.ShomChelle, activeShom) {
			fail(w, 400, "شماره چله با شناسه چله فعال تطابق ندارد")
			return
		}
		p.ChelleID = activeChelleID
		p.ShomChelle = activeShom
		if !sameText(activeHambaft, p.HamChelle) {
			fail(w, 400, "هم‌بافت چله با چله فعال ماشین تطابق ندارد")
			return
		}
		session, ok := a.currentSession(r)
		if !ok || strings.TrimSpace(session.Username) == "" {
			fail(w, http.StatusUnauthorized, "نشست کاربر معتبر نیست؛ دوباره وارد سامانه شوید")
			return
		}
		// Attribution always comes from the authenticated server-side session.
		p.User = session.Username
		knownFormula, formulaErr := a.resolveChelleFormula(p.Machine, p.ShomChelle, kala, p.HamChelle, p.HamPod)
		if formulaErr != nil {
			fail(w, http.StatusBadRequest, "فرمول تار و پود این چله قابل بررسی نیست")
			return
		}
		formulaProvided := p.TarPercent != 0 || p.PodPercent != 0
		if p.ID > 0 && !formulaProvided {
			_ = a.queryRow(`SELECT COALESCE(tar_percent_salon,0),COALESCE(pod_percent_salon,0) FROM salon WHERE id_salon=?`, p.ID).
				Scan(&p.TarPercent, &p.PodPercent)
		}
		if !formulaIsValid(p.TarPercent, p.PodPercent) {
			p.TarPercent, p.PodPercent = knownFormula.TarPercent, knownFormula.PodPercent
		}
		if !knownFormula.Configured && !p.FormulaConfirmed {
			fail(w, http.StatusBadRequest, "هم‌بافت یا پارچه جدید است؛ درصد مصرف تار و پود را تأیید کنید")
			return
		}
		if !formulaIsValid(p.TarPercent, p.PodPercent) {
			fail(w, http.StatusBadRequest, "جمع درصد تار و پود باید دقیقاً ۱۰۰ باشد")
			return
		}
		tx, txErr := a.db.Begin()
		if txErr != nil {
			fail(w, 500, txErr.Error())
			return
		}
		defer tx.Rollback()
		if p.ID > 0 {
			var oldMachine, oldChelle string
			_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(machin_salon,''),COALESCE(shom_chelle_salon,'') FROM salon WHERE id_salon=?`, p.ID).Scan(&oldMachine, &oldChelle)
			_, err = txExec(a.dialect, tx, `UPDATE salon SET metr_salon=?, w_salon=?, machin_salon=?, user_salon=?, kala_salon=?, ham_pod_salon=?, ham_chelle_salon=?, shom_chelle_salon=?, chelle_id_salon=?, tar_percent_salon=?, pod_percent_salon=? WHERE id_salon=?`, p.Metr, p.Weight, p.Machine, p.User, kala, p.HamPod, p.HamChelle, p.ShomChelle, p.ChelleID, p.TarPercent, p.PodPercent, p.ID)
			if err == nil && formulaProvided {
				err = a.saveChelleFormulaTx(tx, chelleFormula{
					ChelleID: p.ChelleID, Machine: p.Machine, ShomChelle: p.ShomChelle, Kala: kala,
					HamChelle: p.HamChelle, HamPod: p.HamPod,
					TarPercent: p.TarPercent, PodPercent: p.PodPercent,
				})
			}
			if err == nil {
				err = a.rebuildConsumptionTx(tx, oldMachine, oldChelle)
			}
			if err == nil && (oldMachine != p.Machine || oldChelle != p.ShomChelle) {
				err = a.rebuildConsumptionTx(tx, p.Machine, p.ShomChelle)
			}
			if err != nil {
				fail(w, 500, err.Error())
				return
			}
			writeSave(w, tx.Commit())
			return
		}
		next := int64(1)
		_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(MAX(id_salon),0)+1 FROM salon`).Scan(&next)
		err = a.saveChelleFormulaTx(tx, chelleFormula{
			ChelleID: p.ChelleID, Machine: p.Machine, ShomChelle: p.ShomChelle, Kala: kala,
			HamChelle: p.HamChelle, HamPod: p.HamPod,
			TarPercent: p.TarPercent, PodPercent: p.PodPercent,
		})
		if err == nil {
			_, err = txExec(a.dialect, tx, `INSERT INTO salon (id_salon, metr_salon, w_salon, machin_salon, user_salon, tarikh_salon, kala_salon, ham_pod_salon, ham_chelle_salon, shom_chelle_salon, chelle_id_salon, tar_percent_salon, pod_percent_salon) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, next, p.Metr, p.Weight, p.Machine, p.User, jalaliToday(), kala, p.HamPod, p.HamChelle, p.ShomChelle, p.ChelleID, p.TarPercent, p.PodPercent)
		}
		if err == nil {
			err = a.rebuildConsumptionTx(tx, p.Machine, p.ShomChelle)
		}
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if err = tx.Commit(); err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeJSON(w, record{"success": true, "id": next})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) outInvoice(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.writeOutInvoices(w, 200)
	case http.MethodPost:
		var p struct {
			InvoiceNo           string   `json:"invoice_no"`
			SanadNo             string   `json:"sanad_no"`
			Customer            string   `json:"customer"`
			Kala                string   `json:"kala"`
			Items               []string `json:"items"`
			OldNo               string   `json:"old_invoice_no"`
			LoadingSessionToken string   `json:"loading_session_token"`
		}
		if !decode(w, r, &p) {
			return
		}
		p.InvoiceNo = strings.TrimSpace(p.InvoiceNo)
		p.SanadNo = strings.TrimSpace(p.SanadNo)
		p.Customer = strings.TrimSpace(p.Customer)
		p.Kala = strings.TrimSpace(p.Kala)
		p.OldNo = strings.TrimSpace(p.OldNo)
		p.LoadingSessionToken = strings.TrimSpace(p.LoadingSessionToken)
		p.Items = uniqueCodes(p.Items)
		if p.InvoiceNo == "" || p.Customer == "" || p.Kala == "" || len(p.Items) == 0 {
			fail(w, 400, "اطلاعات فاکتور خروج کامل نیست")
			return
		}
		var loading *loadingSession
		if p.LoadingSessionToken != "" {
			s, err := a.loadingSessionByToken(p.LoadingSessionToken)
			if err != nil || !a.loadingSessionIsActive(s) {
				fail(w, http.StatusBadRequest, "جلسه بارگیری معتبر یا فعال نیست")
				return
			}
			if s.InvoiceNo != p.InvoiceNo || !sameText(s.Customer, p.Customer) || !sameText(s.Kala, p.Kala) {
				fail(w, http.StatusBadRequest, "مشخصات جلسه بارگیری با فاکتور جاری یکسان نیست")
				return
			}
			loading = &s
		}
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		conflict := false
		sort.Strings(p.Items)
		for _, code := range p.Items {
			taghe, lockErr := a.tagheForUpdate(tx, code)
			if lockErr != nil {
				err = fmt.Errorf("طاقه %s در دیتابیس موجود نیست", code)
				break
			}
			if !sameText(taghe.Kala, p.Kala) {
				err = fmt.Errorf("کالای طاقه %s با کالای فاکتور مغایرت دارد", code)
				break
			}
			var existingInvoice string
			existingErr := txQueryRow(a.dialect, tx, `SELECT shom_f_khor FROM f_khor WHERE taghe_cod_f_khor=? AND (?='' OR shom_f_khor<>?) LIMIT 1`, code, p.OldNo, p.OldNo).Scan(&existingInvoice)
			if existingErr == nil && existingInvoice != "" {
				conflict = true
				err = fmt.Errorf("طاقه %s قبلاً در فاکتور %s ثبت شده است", code, existingInvoice)
				break
			}
			if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
				err = existingErr
				break
			}
			var reservationSession string
			reservationErr := txQueryRow(a.dialect, tx, `SELECT session_id FROM loading_reservations WHERE taghe_code=?`, code).Scan(&reservationSession)
			if reservationErr == nil && (loading == nil || reservationSession != loading.ID) {
				conflict = true
				err = fmt.Errorf("طاقه %s در یک بارگیری دیگر رزرو شده است", code)
				break
			}
			if reservationErr != nil && !errors.Is(reservationErr, sql.ErrNoRows) {
				err = reservationErr
				break
			}
		}
		if err == nil && p.OldNo != "" {
			_, err = txExec(a.dialect, tx, `DELETE FROM f_khor WHERE shom_f_khor=?`, p.OldNo)
		}
		for _, code := range p.Items {
			if err == nil {
				_, err = txExec(a.dialect, tx, `INSERT INTO f_khor (tarikh_f_khor, shom_f_khor, taghe_cod_f_khor, mosh_f_khor, shomare_sanad, kala_name_f_khor) VALUES (?,?,?,?,?,?)`, jalaliToday(), p.InvoiceNo, code, p.Customer, p.SanadNo, p.Kala)
			}
		}
		if err == nil && loading != nil {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			_, err = txExec(a.dialect, tx, `UPDATE loading_sessions SET status='completed', completed_at=? WHERE id=? AND status='active'`, now, loading.ID)
			if err == nil {
				_, err = txExec(a.dialect, tx, `DELETE FROM loading_reservations WHERE session_id=?`, loading.ID)
			}
		}
		if err != nil {
			_ = tx.Rollback()
			status := http.StatusBadRequest
			if conflict {
				status = http.StatusConflict
			}
			fail(w, status, err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, record{"success": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) outInvoiceByPath(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/out-invoice/"), "/")
	if path == "loading" {
		a.createLoadingSession(w, r)
		return
	}
	if strings.HasPrefix(path, "loading/") {
		a.cancelLoadingSession(w, r, strings.TrimPrefix(path, "loading/"))
		return
	}
	if strings.HasPrefix(path, "taghe/") {
		code := strings.TrimPrefix(path, "taghe/")
		a.tagheInfo(w, code)
		return
	}
	if strings.HasPrefix(path, "details/") {
		no := strings.TrimPrefix(path, "details/")
		writeJSON(w, a.outInvoiceDetails(no))
		return
	}
	if strings.HasPrefix(path, "stock") {
		a.stockTaghes(w)
		return
	}
	if strings.HasPrefix(path, "next-sanad") {
		writeJSON(w, record{"success": true, "sanad_number": a.nextSanadNumber(), "tarikh": jalaliToday()})
		return
	}
	if r.Method == http.MethodDelete {
		writeSave(w, execErr(a.exec(`DELETE FROM f_khor WHERE shom_f_khor=?`, pathLast(r.URL.Path))))
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func uniqueCodes(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		code := strings.TrimSpace(item)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	return out
}

func sameText(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func loadingTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func loadingSessionTTL() time.Duration {
	minutes, err := strconv.Atoi(strings.TrimSpace(env("LOADING_SESSION_TTL_MINUTES", "480")))
	if err != nil || minutes < 15 || minutes > 1440 {
		minutes = 480
	}
	return time.Duration(minutes) * time.Minute
}

func loadingPublicBase(r *http.Request) string {
	if configured := strings.TrimSpace(os.Getenv("LOADING_PUBLIC_BASE")); configured != "" {
		// This setting used to contain the internal operational API prefix.
		// A mobile user must instead enter through the public portal so their
		// customer credentials can be verified and scoped to the right tenant.
		if parsed, err := url.Parse(configured); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return parsed.Scheme + "://" + parsed.Host
		}
		return strings.TrimRight(configured, "/")
	}
	scheme := "http"
	if isSecureRequest(r) {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	return scheme + "://" + host
}

func loadingPublicURL(r *http.Request, token string) string {
	next := "/operational/loading/" + url.PathEscape(token)
	return loadingPublicBase(r) + "/module-login?module=operational&next=" + url.QueryEscape(next)
}

func (a *app) cleanupExpiredLoadingSessions() {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = a.exec(`UPDATE loading_sessions SET status='expired' WHERE status='active' AND expires_at<=?`, now)
	_, _ = a.exec(`DELETE FROM loading_reservations WHERE session_id IN (SELECT id FROM loading_sessions WHERE status<>'active' OR expires_at<=?)`, now)
}

func (a *app) createLoadingSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		InvoiceNo string   `json:"invoice_no"`
		SanadNo   string   `json:"sanad_no"`
		Customer  string   `json:"customer"`
		Kala      string   `json:"kala"`
		Items     []string `json:"items"`
		OldNo     string   `json:"old_invoice_no"`
	}
	if !decode(w, r, &payload) {
		return
	}
	payload.InvoiceNo = strings.TrimSpace(payload.InvoiceNo)
	payload.SanadNo = strings.TrimSpace(payload.SanadNo)
	payload.Customer = strings.TrimSpace(payload.Customer)
	payload.Kala = strings.TrimSpace(payload.Kala)
	payload.OldNo = strings.TrimSpace(payload.OldNo)
	payload.Items = uniqueCodes(payload.Items)
	if payload.InvoiceNo == "" || payload.Customer == "" || payload.Kala == "" {
		fail(w, http.StatusBadRequest, "شماره فاکتور، مشتری و نام کالا برای شروع بارگیری الزامی است")
		return
	}
	employee, ok := a.currentSession(r)
	if !ok {
		fail(w, http.StatusUnauthorized, "نشست کارمند معتبر نیست")
		return
	}
	token, err := randomSessionToken()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := randomSessionToken()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(loadingSessionTTL())
	a.cleanupExpiredLoadingSessions()
	tx, err := a.db.Begin()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = txExec(a.dialect, tx, `UPDATE loading_sessions SET status='cancelled', completed_at=? WHERE created_by=? AND status='active'`, now.Format(time.RFC3339Nano), employee.UserID)
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM loading_reservations WHERE session_id IN (SELECT id FROM loading_sessions WHERE created_by=? AND status='cancelled')`, employee.UserID)
	}
	if err == nil {
		_, err = txExec(a.dialect, tx, `INSERT INTO loading_sessions (id,token_hash,invoice_no,sanad_no,customer,kala,status,created_by,created_by_username,created_at,expires_at) VALUES (?,?,?,?,?,?,'active',?,?,?,?)`, id, loadingTokenHash(token), payload.InvoiceNo, payload.SanadNo, payload.Customer, payload.Kala, employee.UserID, employee.Username, now.Format(time.RFC3339Nano), expiresAt.Format(time.RFC3339Nano))
	}
	if err == nil {
		sort.Strings(payload.Items)
		for _, code := range payload.Items {
			taghe, lockErr := a.tagheForUpdate(tx, code)
			if lockErr != nil {
				err = fmt.Errorf("طاقه %s در دیتابیس موجود نیست", code)
				break
			}
			if !sameText(taghe.Kala, payload.Kala) {
				err = fmt.Errorf("کالای طاقه %s با کالای فاکتور مغایرت دارد", code)
				break
			}
			var existingInvoice string
			existingErr := txQueryRow(a.dialect, tx, `SELECT shom_f_khor FROM f_khor WHERE taghe_cod_f_khor=? AND (?='' OR shom_f_khor<>?) LIMIT 1`, code, payload.OldNo, payload.OldNo).Scan(&existingInvoice)
			if existingErr == nil && existingInvoice != "" {
				err = fmt.Errorf("طاقه %s قبلاً در فاکتور %s ثبت شده است", code, existingInvoice)
				break
			}
			if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
				err = existingErr
				break
			}
			_, err = txExec(a.dialect, tx, `INSERT INTO loading_reservations (taghe_code,session_id,reserved_at) VALUES (?,?,?)`, code, id, now.Format(time.RFC3339Nano))
			if err != nil {
				err = fmt.Errorf("طاقه %s در یک بارگیری دیگر رزرو شده است", code)
				break
			}
		}
	}
	if err != nil {
		_ = tx.Rollback()
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, record{
		"success":    true,
		"token":      token,
		"url":        loadingPublicURL(r, token),
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

func (a *app) cancelLoadingSession(w http.ResponseWriter, r *http.Request, token string) {
	if r.Method != http.MethodDelete || strings.Contains(token, "/") {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s, err := a.loadingSessionByToken(token)
	if err != nil {
		fail(w, http.StatusNotFound, "جلسه بارگیری پیدا نشد")
		return
	}
	employee, ok := a.currentSession(r)
	if !ok {
		fail(w, http.StatusUnauthorized, "نشست کارمند معتبر نیست")
		return
	}
	if employee.UserID != s.CreatedBy && !strings.EqualFold(employee.Role, "admin") {
		fail(w, http.StatusForbidden, "فقط ایجادکننده یا مدیر می‌تواند جلسه را لغو کند")
		return
	}
	tx, err := a.db.Begin()
	if err == nil {
		_, err = txExec(a.dialect, tx, `UPDATE loading_sessions SET status='cancelled', completed_at=? WHERE id=? AND status='active'`, time.Now().UTC().Format(time.RFC3339Nano), s.ID)
	}
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM loading_reservations WHERE session_id=?`, s.ID)
	}
	if err != nil {
		if tx != nil {
			_ = tx.Rollback()
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, record{"success": true})
}

func (a *app) loadingSessionByToken(token string) (loadingSession, error) {
	var session loadingSession
	err := a.queryRow(`SELECT id,token_hash,invoice_no,COALESCE(sanad_no,''),customer,kala,status,created_by,COALESCE(created_by_username,''),created_at,expires_at FROM loading_sessions WHERE token_hash=?`, loadingTokenHash(token)).Scan(
		&session.ID, &session.TokenHash, &session.InvoiceNo, &session.SanadNo, &session.Customer, &session.Kala, &session.Status, &session.CreatedBy, &session.CreatedByUsername, &session.CreatedAt, &session.ExpiresAt,
	)
	return session, err
}

func (a *app) loadingSessionIsActive(session loadingSession) bool {
	if session.Status != "active" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, session.ExpiresAt)
	return err == nil && time.Now().UTC().Before(expiresAt)
}

func (a *app) loadingEmployee(w http.ResponseWriter, r *http.Request) (sessionInfo, bool) {
	employee, ok := a.currentSession(r)
	if !ok {
		fail(w, http.StatusUnauthorized, "برای استفاده از بارکدخوان با حساب کارمند وارد شوید")
		return sessionInfo{}, false
	}
	allowed, err := a.userHasMenuAccess(employee.UserID, employee.Role, "out-invoice")
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return sessionInfo{}, false
	}
	if !allowed {
		fail(w, http.StatusForbidden, "این کارمند به فاکتور خروج دسترسی ندارد")
		return sessionInfo{}, false
	}
	return employee, true
}

func (a *app) loadingMobile(w http.ResponseWriter, r *http.Request) {
	employee, ok := a.loadingEmployee(w, r)
	if !ok {
		return
	}
	a.cleanupExpiredLoadingSessions()
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/loading/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		http.NotFound(w, r)
		return
	}
	token := parts[0]
	session, err := a.loadingSessionByToken(token)
	if err != nil {
		fail(w, http.StatusNotFound, "جلسه بارگیری پیدا نشد")
		return
	}
	if !a.loadingSessionIsActive(session) {
		fail(w, http.StatusGone, "زمان جلسه بارگیری پایان یافته یا جلسه بسته شده است")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		a.writeLoadingState(w, session)
		return
	}
	if len(parts) == 2 && parts[1] == "scan" && r.Method == http.MethodPost {
		a.scanLoadingTaghe(w, r, session)
		return
	}
	if len(parts) == 2 && parts[1] == "confirm" && r.Method == http.MethodPost {
		a.confirmLoadingTaghe(w, r, session, employee)
		return
	}
	if len(parts) == 3 && parts[1] == "items" && r.Method == http.MethodDelete {
		a.removeLoadingTaghe(w, session, parts[2])
		return
	}
	http.NotFound(w, r)
}

func (a *app) writeLoadingState(w http.ResponseWriter, session loadingSession) {
	items, err := a.loadingSessionItems(session.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	var totalMetr, totalWeight float64
	for _, item := range items {
		totalMetr += item["metr"].(float64)
		totalWeight += item["weight"].(float64)
	}
	writeJSON(w, record{
		"success": true,
		"session": record{
			"invoice_no": session.InvoiceNo, "sanad_no": session.SanadNo, "customer": session.Customer,
			"kala": session.Kala, "status": session.Status, "created_by": session.CreatedByUsername, "expires_at": session.ExpiresAt,
		},
		"items":  items,
		"totals": record{"count": len(items), "metr": totalMetr, "weight": totalWeight},
	})
}

func (a *app) loadingSessionItems(sessionID string) ([]record, error) {
	rows, err := a.query(`SELECT i.taghe_code,COALESCE(s.metr_salon,0),COALESCE(s.w_salon,0),COALESCE(s.machin_salon,''),COALESCE(s.kala_salon,''),COALESCE(s.ham_pod_salon,''),COALESCE(s.ham_chelle_salon,''),COALESCE(s.shom_chelle_salon,''),COALESCE(i.confirmed_by_username,''),i.confirmed_at
		FROM loading_session_items i LEFT JOIN salon s ON CAST(s.id_salon AS TEXT)=i.taghe_code WHERE i.session_id=? ORDER BY i.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var code, machine, kala, hamPod, hamChelle, shomChelle, confirmedBy, confirmedAt string
		var metr, weight float64
		if err := rows.Scan(&code, &metr, &weight, &machine, &kala, &hamPod, &hamChelle, &shomChelle, &confirmedBy, &confirmedAt); err != nil {
			return nil, err
		}
		items = append(items, record{"id": code, "metr": metr, "weight": weight, "machine": machine, "kala": kala, "ham_pod": hamPod, "ham_chelle": hamChelle, "shom_chelle": shomChelle, "confirmed_by": confirmedBy, "confirmed_at": confirmedAt})
	}
	return items, rows.Err()
}

func (a *app) scanLoadingTaghe(w http.ResponseWriter, r *http.Request, session loadingSession) {
	var payload struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &payload) {
		return
	}
	code := strings.TrimSpace(payload.Code)
	taghe, err := a.findTaghe(code)
	if err != nil {
		fail(w, http.StatusNotFound, "کد طاقه در دیتابیس پیدا نشد")
		return
	}
	if invoice, err := a.invoiceForTaghe(code); err == nil && invoice != "" {
		fail(w, http.StatusConflict, "این طاقه قبلاً در فاکتور "+invoice+" ثبت شده است")
		return
	}
	var reservedBy string
	reservationErr := a.queryRow(`SELECT session_id FROM loading_reservations WHERE taghe_code=?`, code).Scan(&reservedBy)
	if reservationErr == nil && reservedBy != session.ID {
		fail(w, http.StatusConflict, "این طاقه در یک بارگیری دیگر رزرو شده است")
		return
	}
	if reservationErr == nil && reservedBy == session.ID {
		fail(w, http.StatusConflict, "این طاقه قبلاً در همین فاکتور اضافه شده است")
		return
	}
	if reservationErr != nil && !errors.Is(reservationErr, sql.ErrNoRows) {
		fail(w, http.StatusInternalServerError, reservationErr.Error())
		return
	}
	matches := sameText(taghe.Kala, session.Kala)
	reason := ""
	if !matches {
		reason = "نام کالای طاقه با کالای فاکتور یکسان نیست"
	}
	item := taghe.record()
	item["matches"] = matches
	item["mismatch_reason"] = reason
	item["already_confirmed"] = false
	writeJSON(w, record{"success": true, "item": item})
}

func (a *app) confirmLoadingTaghe(w http.ResponseWriter, r *http.Request, session loadingSession, employee sessionInfo) {
	var payload struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &payload) {
		return
	}
	code := strings.TrimSpace(payload.Code)
	if code == "" {
		fail(w, http.StatusBadRequest, "کد طاقه وارد نشده است")
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	taghe, err := a.tagheForUpdate(tx, code)
	if err != nil {
		_ = tx.Rollback()
		fail(w, http.StatusNotFound, "کد طاقه در دیتابیس پیدا نشد")
		return
	}
	if !sameText(taghe.Kala, session.Kala) {
		_ = tx.Rollback()
		fail(w, http.StatusConflict, "مشخصات کالای طاقه با فاکتور مغایرت دارد")
		return
	}
	var existingInvoice string
	existingErr := txQueryRow(a.dialect, tx, `SELECT shom_f_khor FROM f_khor WHERE taghe_cod_f_khor=? LIMIT 1`, code).Scan(&existingInvoice)
	if existingErr == nil && existingInvoice != "" {
		_ = tx.Rollback()
		fail(w, http.StatusConflict, "این طاقه قبلاً در فاکتور "+existingInvoice+" ثبت شده است")
		return
	}
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		_ = tx.Rollback()
		fail(w, http.StatusInternalServerError, existingErr.Error())
		return
	}
	var reservedBy string
	reservationErr := txQueryRow(a.dialect, tx, `SELECT session_id FROM loading_reservations WHERE taghe_code=?`, code).Scan(&reservedBy)
	if reservationErr == nil && reservedBy != session.ID {
		_ = tx.Rollback()
		fail(w, http.StatusConflict, "این طاقه در یک بارگیری دیگر رزرو شده است")
		return
	}
	if reservationErr == nil && reservedBy == session.ID {
		_ = tx.Rollback()
		fail(w, http.StatusConflict, "این طاقه قبلاً در همین فاکتور اضافه شده است")
		return
	}
	if reservationErr != nil && !errors.Is(reservationErr, sql.ErrNoRows) {
		_ = tx.Rollback()
		fail(w, http.StatusInternalServerError, reservationErr.Error())
		return
	}
	if errors.Is(reservationErr, sql.ErrNoRows) {
		_, err = txExec(a.dialect, tx, `INSERT INTO loading_reservations (taghe_code,session_id,reserved_at) VALUES (?,?,?)`, code, session.ID, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if err == nil && errors.Is(reservationErr, sql.ErrNoRows) {
		_, err = txExec(a.dialect, tx, `INSERT INTO loading_session_items (session_id,taghe_code,confirmed_by,confirmed_by_username,confirmed_at) VALUES (?,?,?,?,?)`, session.ID, code, employee.UserID, employee.Username, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if err != nil {
		_ = tx.Rollback()
		fail(w, http.StatusConflict, "این طاقه قبلاً تأیید شده یا هم‌زمان رزرو شده است")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	item := taghe.record()
	item["confirmed_by"] = employee.Username
	writeJSON(w, record{"success": true, "item": item})
}

func (a *app) removeLoadingTaghe(w http.ResponseWriter, session loadingSession, code string) {
	code = strings.TrimSpace(code)
	tx, err := a.db.Begin()
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM loading_session_items WHERE session_id=? AND taghe_code=?`, session.ID, code)
	}
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM loading_reservations WHERE session_id=? AND taghe_code=?`, session.ID, code)
	}
	if err != nil {
		if tx != nil {
			_ = tx.Rollback()
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeLoadingState(w, session)
}

func (a *app) findTaghe(code string) (tagheData, error) {
	var taghe tagheData
	err := a.queryRow(`SELECT id_salon,metr_salon,w_salon,COALESCE(machin_salon,''),COALESCE(kala_salon,''),COALESCE(ham_pod_salon,''),COALESCE(ham_chelle_salon,''),COALESCE(shom_chelle_salon,'') FROM salon WHERE id_salon=?`, strings.TrimSpace(code)).Scan(
		&taghe.ID, &taghe.Metr, &taghe.Weight, &taghe.Machine, &taghe.Kala, &taghe.HamPod, &taghe.HamChelle, &taghe.ShomChelle,
	)
	return taghe, err
}

func (a *app) tagheForUpdate(tx *sql.Tx, code string) (tagheData, error) {
	query := `SELECT id_salon,metr_salon,w_salon,COALESCE(machin_salon,''),COALESCE(kala_salon,''),COALESCE(ham_pod_salon,''),COALESCE(ham_chelle_salon,''),COALESCE(shom_chelle_salon,'') FROM salon WHERE id_salon=?`
	if a.dialect == "postgres" {
		query += " FOR UPDATE"
	}
	var taghe tagheData
	err := txQueryRow(a.dialect, tx, query, strings.TrimSpace(code)).Scan(
		&taghe.ID, &taghe.Metr, &taghe.Weight, &taghe.Machine, &taghe.Kala, &taghe.HamPod, &taghe.HamChelle, &taghe.ShomChelle,
	)
	return taghe, err
}

func (a *app) invoiceForTaghe(code string) (string, error) {
	var invoice string
	err := a.queryRow(`SELECT shom_f_khor FROM f_khor WHERE taghe_cod_f_khor=? LIMIT 1`, strings.TrimSpace(code)).Scan(&invoice)
	return invoice, err
}

func (taghe tagheData) record() record {
	return record{"id": taghe.ID, "metr": taghe.Metr, "weight": taghe.Weight, "machine": taghe.Machine, "kala": taghe.Kala, "ham_pod": taghe.HamPod, "ham_chelle": taghe.HamChelle, "shom_chelle": taghe.ShomChelle}
}

func (a *app) expenses(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT h.id_h_rozmare, h.tarikh_h_rozmare, h.onvan_hazine, h.operator_name, COALESCE(h.weaver_name,''), h.mablagh_h_rozmare, COALESCE(h.tozih_h_rozmare,''), COALESCE(h.shomare_sanad,''), COALESCE(c.id_hazine,0), COALESCE(o.id_operator,0), COALESCE(wv.id_weaver,0) FROM h_rozmare h LEFT JOIN hazine c ON c.onvan_hazine=h.onvan_hazine LEFT JOIN operator_name o ON o.name_operator=h.operator_name LEFT JOIN weaver_name wv ON wv.name_weaver=h.weaver_name ORDER BY h.id_h_rozmare DESC LIMIT 300`)
		writeRows(w, rows, err, []string{"id", "tarikh", "onvan_hazine", "operator_name", "weaver_name", "mablagh", "tozih", "shomare_sanad", "hazine_id", "operator_id", "weaver_id"})
	case http.MethodPost:
		var p struct {
			ID          int64   `json:"id"`
			HazineID    int64   `json:"hazine_id"`
			OperatorID  int64   `json:"operator_id"`
			WeaverID    int64   `json:"weaver_id"`
			Mablagh     float64 `json:"mablagh"`
			Description string  `json:"description"`
			SanadNo     string  `json:"sanad_no"`
		}
		if !decode(w, r, &p) {
			return
		}
		hazine, err1 := a.nameByID("hazine", "id_hazine", "onvan_hazine", p.HazineID)
		operator, err2 := a.nameByID("operator_name", "id_operator", "name_operator", p.OperatorID)
		weaver := ""
		if p.WeaverID > 0 {
			weaver, _ = a.nameByID("weaver_name", "id_weaver", "name_weaver", p.WeaverID)
		}
		if errors.Join(err1, err2) != nil || p.Mablagh <= 0 {
			fail(w, 400, "اطلاعات هزینه کامل نیست")
			return
		}
		var err error
		if p.ID > 0 {
			_, err = a.exec(`UPDATE h_rozmare SET onvan_hazine=?, operator_name=?, weaver_name=?, mablagh_h_rozmare=?, tozih_h_rozmare=?, shomare_sanad=? WHERE id_h_rozmare=?`, hazine, operator, weaver, p.Mablagh, p.Description, p.SanadNo, p.ID)
		} else {
			_, err = a.exec(`INSERT INTO h_rozmare (tarikh_h_rozmare,onvan_hazine,operator_name,weaver_name,mablagh_h_rozmare,tozih_h_rozmare,shomare_sanad) VALUES (?,?,?,?,?,?,?)`, jalaliToday(), hazine, operator, weaver, p.Mablagh, p.Description, p.SanadNo)
		}
		writeSave(w, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) expenseByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	writeSave(w, execErr(a.exec(`DELETE FROM h_rozmare WHERE id_h_rozmare=?`, id)))
}

func (a *app) productionWaste(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT id_waste,COALESCE(waste_date,''),machine,shom_chelle,waste_type,weight,COALESCE(reason,''),COALESCE(operator_name,''),COALESCE(description,''),COALESCE(chelle_id_waste,0),COALESCE(corrective_action,'') FROM production_waste ORDER BY id_waste DESC LIMIT 500`)
		writeRows(w, rows, err, []string{"id", "tarikh", "machine", "shom_chelle", "waste_type", "weight", "reason", "operator_name", "description", "chelle_id", "corrective_action"})
	case http.MethodPost:
		var p struct {
			ID          int64   `json:"id"`
			Machine     string  `json:"machine"`
			ShomChelle  string  `json:"shom_chelle"`
			ChelleID    int64   `json:"chelle_id"`
			WasteType   string  `json:"waste_type"`
			Weight      float64 `json:"weight"`
			Reason      string  `json:"reason"`
			Description string  `json:"description"`
			Corrective  string  `json:"corrective_action"`
		}
		if !decode(w, r, &p) {
			return
		}
		machine, machineErr := canonicalMachineNumber(p.Machine)
		if machineErr != nil {
			fail(w, 400, machineErr.Error())
			return
		}
		p.Machine = machine
		p.ShomChelle = strings.TrimSpace(p.ShomChelle)
		p.WasteType = strings.TrimSpace(p.WasteType)
		allowed := map[string]bool{"tar": true, "pod": true, "fabric": true, "selvage": true, "other": true}
		if p.Machine == "" || p.ShomChelle == "" || !allowed[p.WasteType] || p.Weight <= 0 || strings.TrimSpace(p.Reason) == "" {
			fail(w, 400, "ماشین، چله، نوع، وزن و علت ضایعات الزامی است")
			return
		}
		resolvedID, resolvedShom := int64(0), ""
		if p.ChelleID > 0 {
			_ = a.queryRow(`SELECT id_chelle,shom_chelle FROM chelle c WHERE id_chelle=? AND (machin_chelle=? OR EXISTS(SELECT 1 FROM gere g WHERE g.chelle_id_gere=c.id_chelle AND g.machin_gere=?) OR EXISTS(SELECT 1 FROM salon s WHERE s.chelle_id_salon=c.id_chelle AND s.machin_salon=?))`, p.ChelleID, p.Machine, p.Machine, p.Machine).Scan(&resolvedID, &resolvedShom)
		} else {
			_ = a.queryRow(`SELECT id_chelle,shom_chelle FROM chelle c WHERE shom_chelle=? AND (machin_chelle=? OR EXISTS(SELECT 1 FROM gere g WHERE g.chelle_id_gere=c.id_chelle AND g.machin_gere=?) OR EXISTS(SELECT 1 FROM salon s WHERE s.chelle_id_salon=c.id_chelle AND s.machin_salon=?)) ORDER BY id_chelle DESC LIMIT 1`, p.ShomChelle, p.Machine, p.Machine, p.Machine).Scan(&resolvedID, &resolvedShom)
		}
		if resolvedID == 0 {
			fail(w, 400, "چله برای ماشین انتخاب‌شده سابقه معتبر ندارد")
			return
		}
		if p.ShomChelle != "" && !sameText(p.ShomChelle, resolvedShom) {
			fail(w, 400, "شماره چله با شناسه چله انتخاب‌شده تطابق ندارد")
			return
		}
		p.ChelleID, p.ShomChelle = resolvedID, resolvedShom
		operator := "admin"
		if session, ok := a.currentSession(r); ok {
			operator = session.Username
		}
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		oldMachine, oldChelle := "", ""
		if p.ID > 0 {
			_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(machine,''),COALESCE(shom_chelle,'') FROM production_waste WHERE id_waste=?`, p.ID).Scan(&oldMachine, &oldChelle)
			_, err = txExec(a.dialect, tx, `UPDATE production_waste SET waste_date=?,machine=?,shom_chelle=?,waste_type=?,weight=?,reason=?,operator_name=?,description=?,chelle_id_waste=?,corrective_action=? WHERE id_waste=?`, jalaliToday(), p.Machine, p.ShomChelle, p.WasteType, p.Weight, p.Reason, operator, p.Description, p.ChelleID, p.Corrective, p.ID)
		} else {
			_, err = txExec(a.dialect, tx, `INSERT INTO production_waste(waste_date,machine,shom_chelle,waste_type,weight,reason,operator_name,description,chelle_id_waste,corrective_action) VALUES(?,?,?,?,?,?,?,?,?,?)`, jalaliToday(), p.Machine, p.ShomChelle, p.WasteType, p.Weight, p.Reason, operator, p.Description, p.ChelleID, p.Corrective)
		}
		if err == nil && oldMachine != "" {
			err = a.rebuildConsumptionTx(tx, oldMachine, oldChelle)
		}
		if err == nil && (oldMachine != p.Machine || oldChelle != p.ShomChelle) {
			err = a.rebuildConsumptionTx(tx, p.Machine, p.ShomChelle)
		}
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeSave(w, tx.Commit())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) productionWasteByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	var machine, shom string
	_ = a.queryRow(`SELECT COALESCE(machine,''),COALESCE(shom_chelle,'') FROM production_waste WHERE id_waste=?`, id).Scan(&machine, &shom)
	tx, err := a.db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	_, err = txExec(a.dialect, tx, `DELETE FROM production_waste WHERE id_waste=?`, id)
	if err == nil {
		err = a.rebuildConsumptionTx(tx, machine, shom)
	}
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeSave(w, tx.Commit())
}

func (a *app) formulas(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT id_formul, machine, COALESCE(tar_percent,50), COALESCE(pod_percent,50), COALESCE(tozih_formul,'') FROM machine_formul ORDER BY CAST(machine AS REAL), machine`)
		writeRows(w, rows, err, []string{"id", "machine", "tar_percent", "pod_percent", "tozih"})
	case http.MethodPost:
		var p struct {
			ID         int64   `json:"id"`
			Machine    string  `json:"machine"`
			TarPercent float64 `json:"tar_percent"`
			PodPercent float64 `json:"pod_percent"`
			Tozih      string  `json:"tozih"`
		}
		if !decode(w, r, &p) {
			return
		}
		machine, machineErr := canonicalMachineNumber(p.Machine)
		if machineErr != nil {
			fail(w, 400, machineErr.Error())
			return
		}
		p.Machine = machine
		if p.Machine == "" || p.TarPercent < 0 || p.PodPercent < 0 || absFloat((p.TarPercent+p.PodPercent)-100) > 0.001 {
			fail(w, 400, "جمع درصد تار و پود باید دقیقاً ۱۰۰ باشد")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		oldMachine := ""
		if p.ID > 0 {
			_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(machine,'') FROM machine_formul WHERE id_formul=?`, p.ID).Scan(&oldMachine)
			_, err = txExec(a.dialect, tx, `UPDATE machine_formul SET machine=?, tar_percent=?, pod_percent=?, tozih_formul=? WHERE id_formul=?`, p.Machine, p.TarPercent, p.PodPercent, p.Tozih, p.ID)
		} else {
			_, err = txExec(a.dialect, tx, `INSERT INTO machine_formul (machine, tar_percent, pod_percent, tozih_formul) VALUES (?,?,?,?)
				ON CONFLICT(machine) DO UPDATE SET tar_percent=excluded.tar_percent, pod_percent=excluded.pod_percent, tozih_formul=excluded.tozih_formul`, p.Machine, p.TarPercent, p.PodPercent, p.Tozih)
		}
		if err == nil {
			err = a.rebuildMachineConsumptionTx(tx, p.Machine)
		}
		if err == nil && oldMachine != "" && oldMachine != p.Machine {
			err = a.rebuildMachineConsumptionTx(tx, oldMachine)
		}
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeSave(w, tx.Commit())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) formulaByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	var machine string
	_ = a.queryRow(`SELECT COALESCE(machine,'') FROM machine_formul WHERE id_formul=?`, id).Scan(&machine)
	tx, err := a.db.Begin()
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM machine_formul WHERE id_formul=?`, id)
	}
	if err == nil {
		err = a.rebuildMachineConsumptionTx(tx, machine)
	}
	if err != nil {
		_ = tx.Rollback()
		fail(w, 500, err.Error())
		return
	}
	writeSave(w, tx.Commit())
}

func (a *app) databaseTools(w http.ResponseWriter, r *http.Request) {
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/database/"), "/")
	switch action {
	case "summary":
		a.writeDatabaseSummary(w)
	case "backup":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		dir := a.backupDir()
		_ = os.MkdirAll(dir, 0755)
		name := "backup_" + time.Now().Format("20060102_150405") + ".json"
		target := filepath.Join(dir, name)
		err := a.writeJSONBackup(target)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeJSON(w, record{"success": true, "file": name, "path": target})
	case "backups":
		dir := a.backupDir()
		entries, _ := os.ReadDir(dir)
		items := []record{}
		for _, e := range entries {
			if e.IsDir() || (!strings.HasSuffix(e.Name(), ".json") && !strings.HasSuffix(e.Name(), ".db")) {
				continue
			}
			info, _ := e.Info()
			items = append(items, record{"file": e.Name(), "size": info.Size(), "date": info.ModTime().Format("2006-01-02 15:04")})
		}
		writeJSON(w, record{"success": true, "backups": items, "path": dir})
	case "restore":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var p struct {
			File string `json:"file"`
		}
		if !decode(w, r, &p) {
			return
		}
		name := filepath.Base(strings.TrimSpace(p.File))
		if name == "" || !strings.HasSuffix(name, ".json") {
			fail(w, 400, "فایل بکاپ معتبر نیست")
			return
		}
		if err := a.restoreJSONBackup(filepath.Join(a.backupDir(), name)); err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeJSON(w, record{"success": true, "file": name})
	case "export-xlsx":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := a.exportXLSX(w); err != nil {
			fail(w, 500, err.Error())
			return
		}
	case "import-xlsx":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		dir := a.backupDir()
		if err := os.MkdirAll(dir, 0755); err != nil {
			fail(w, 500, err.Error())
			return
		}
		backupName := "backup_before_excel_import_" + time.Now().Format("20060102_150405") + ".json"
		if err := a.writeJSONBackup(filepath.Join(dir, backupName)); err != nil {
			fail(w, 500, "ساخت پشتیبان پیش از ورود اکسل انجام نشد: "+err.Error())
			return
		}
		importedTables, importedRows, err := a.importXLSX(r)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		tables, err := a.tableNames()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		items := []record{}
		for _, name := range tables {
			items = append(items, record{"table": name, "count": a.count(name)})
		}
		writeJSON(w, record{
			"success": true, "tables": items, "database": a.dbLabel, "driver": a.dialect,
			"imported_tables": importedTables, "imported_rows": importedRows, "backup": backupName,
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (a *app) writeDatabaseSummary(w http.ResponseWriter) {
	tables, err := a.tableNames()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	items := []record{}
	for _, name := range tables {
		items = append(items, record{"table": name, "count": a.count(name)})
	}
	writeJSON(w, record{"success": true, "tables": items, "database": a.dbLabel, "driver": a.dialect})
}

type backupTable struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

func (a *app) backupDir() string {
	if a.dialect == "postgres" {
		schema := a.defaultSchema
		_ = a.queryRow(`SELECT current_schema()`).Scan(&schema)
		schema = regexp.MustCompile(`[^a-zA-Z0-9_]+`).ReplaceAllString(schema, "_")
		return filepath.Join(env("OPERATIONAL_BACKUP_DIR", "/app/backups"), schema)
	}
	return filepath.Join(filepath.Dir(dbPath()), "backups")
}

func (a *app) writeJSONBackup(target string) error {
	tables, err := a.tableNames()
	if err != nil {
		return err
	}
	payload := map[string]backupTable{}
	for _, table := range tables {
		cols, rows, err := a.readTable(table)
		if err != nil {
			return err
		}
		payload[table] = backupTable{Columns: cols, Rows: rows}
	}
	data, err := json.MarshalIndent(record{
		"created_at": time.Now().Format(time.RFC3339),
		"driver":     a.dialect,
		"database":   a.dbLabel,
		"tables":     payload,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0644)
}

func (a *app) restoreJSONBackup(source string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	var payload struct {
		Tables map[string]backupTable `json:"tables"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	tableList, err := a.tableNames()
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, table := range tableList {
		existing[table] = true
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	for table, backup := range payload.Tables {
		if !existing[table] || len(backup.Columns) == 0 {
			continue
		}
		if _, err := txExec(a.dialect, tx, "DELETE FROM "+quoteIdent(table)); err != nil {
			_ = tx.Rollback()
			return err
		}
		placeholders := strings.TrimRight(strings.Repeat("?,", len(backup.Columns)), ",")
		stmt := "INSERT INTO " + quoteIdent(table) + " (" + quoteColumns(backup.Columns) + ") VALUES (" + placeholders + ")"
		for _, row := range backup.Rows {
			vals := make([]any, len(backup.Columns))
			for i := range backup.Columns {
				if i < len(row) {
					if row[i] == "" {
						vals[i] = nil
					} else {
						vals[i] = row[i]
					}
				}
			}
			if _, err := txExec(a.dialect, tx, stmt, vals...); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("%s: %w", table, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return a.migrate()
}

func (a *app) tableNames() ([]string, error) {
	q := `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
	if a.dialect == "postgres" {
		q = `SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename`
	}
	rows, err := a.query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (a *app) exportXLSX(w http.ResponseWriter) error {
	tables, err := a.tableNames()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := xlsxWriteStaticFiles(zw); err != nil {
		return err
	}
	workbookSheets := []string{}
	rels := []string{}
	contentTypes := []string{}
	for i, table := range tables {
		cols, data, err := a.readTable(table)
		if err != nil {
			return err
		}
		sheetID := i + 1
		workbookSheets = append(workbookSheets, fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEsc(sheetName(table)), sheetID, sheetID))
		rels = append(rels, fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, sheetID, sheetID))
		contentTypes = append(contentTypes, fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, sheetID))
		if err := zipAdd(zw, fmt.Sprintf("xl/worksheets/sheet%d.xml", sheetID), buildSheetXML(cols, data)); err != nil {
			return err
		}
	}
	if err := zipAdd(zw, "xl/workbook.xml", buildWorkbookXML(workbookSheets)); err != nil {
		return err
	}
	if err := zipAdd(zw, "xl/_rels/workbook.xml.rels", buildWorkbookRelsXML(rels)); err != nil {
		return err
	}
	if err := zipAdd(zw, "[Content_Types].xml", buildContentTypesXML(contentTypes)); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	filename := "operational_database_" + time.Now().Format("20060102_150405") + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, err = w.Write(buf.Bytes())
	return err
}

func (a *app) readTable(table string) ([]string, [][]string, error) {
	cols := []string{}
	if a.dialect == "postgres" {
		colRows, err := a.query(`SELECT column_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=? ORDER BY ordinal_position`, table)
		if err != nil {
			return nil, nil, err
		}
		defer colRows.Close()
		for colRows.Next() {
			var name string
			if err := colRows.Scan(&name); err != nil {
				return nil, nil, err
			}
			cols = append(cols, name)
		}
	} else {
		colRows, err := a.query("PRAGMA table_info(" + quoteIdent(table) + ")")
		if err != nil {
			return nil, nil, err
		}
		defer colRows.Close()
		for colRows.Next() {
			var cid int
			var name, ctype string
			var notnull int
			var dflt any
			var pk int
			if err := colRows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return nil, nil, err
			}
			cols = append(cols, name)
		}
	}
	if len(cols) == 0 {
		return cols, [][]string{}, nil
	}
	rows, err := a.query("SELECT " + quoteColumns(cols) + " FROM " + quoteIdent(table))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	data := [][]string{}
	for rows.Next() {
		raw := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		line := make([]string, len(cols))
		for i, v := range raw {
			if v.Valid {
				line[i] = v.String
			}
		}
		data = append(data, line)
	}
	return cols, data, rows.Err()
}

func (a *app) importXLSX(r *http.Request) (int, int, error) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return 0, 0, err
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		return 0, 0, err
	}
	sheets, err := parseXLSX(content)
	if err != nil {
		return 0, 0, err
	}
	tableList, err := a.tableNames()
	if err != nil {
		return 0, 0, err
	}
	tableBySheetName := map[string]string{}
	for _, table := range tableList {
		tableBySheetName[table] = table
		short := sheetName(table)
		if current, exists := tableBySheetName[short]; !exists || current == table {
			tableBySheetName[short] = table
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	importedTables := 0
	importedRows := 0
	importedTableNames := []string{}
	for worksheetName, rows := range sheets {
		table, exists := tableBySheetName[worksheetName]
		if len(rows) < 1 || !exists {
			continue
		}
		cols := rows[0]
		if len(cols) == 0 {
			continue
		}
		if _, err := txExec(a.dialect, tx, "DELETE FROM "+quoteIdent(table)); err != nil {
			_ = tx.Rollback()
			return 0, 0, err
		}
		placeholders := strings.TrimRight(strings.Repeat("?,", len(cols)), ",")
		stmt := "INSERT INTO " + quoteIdent(table) + " (" + quoteColumns(cols) + ") VALUES (" + placeholders + ")"
		for _, row := range rows[1:] {
			vals := make([]any, len(cols))
			for i := range cols {
				if i < len(row) {
					vals[i] = row[i]
				} else {
					vals[i] = nil
				}
			}
			if _, err := txExec(a.dialect, tx, stmt, vals...); err != nil {
				_ = tx.Rollback()
				return 0, 0, fmt.Errorf("%s: %w", table, err)
			}
			importedRows++
		}
		importedTables++
		importedTableNames = append(importedTableNames, table)
	}
	if importedTables == 0 {
		_ = tx.Rollback()
		return 0, 0, errors.New("هیچ شیت معتبری مطابق جدول‌های دیتابیس پیدا نشد؛ فایل خروجی همین سامانه را انتخاب کنید")
	}
	if err := a.resetImportedSequences(tx, importedTableNames); err != nil {
		_ = tx.Rollback()
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	if err := a.migrate(); err != nil {
		return 0, 0, err
	}
	return importedTables, importedRows, nil
}

func (a *app) resetImportedSequences(tx *sql.Tx, tables []string) error {
	if a.dialect != "postgres" {
		return nil
	}
	for _, table := range tables {
		rows, err := tx.Query(`
			SELECT column_name
			FROM information_schema.columns
			WHERE table_schema=current_schema() AND table_name=$1
			  AND (is_identity='YES' OR column_default LIKE 'nextval(%')
		`, table)
		if err != nil {
			return err
		}
		columns := []string{}
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				_ = rows.Close()
				return err
			}
			columns = append(columns, column)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, column := range columns {
			var sequence sql.NullString
			if err := tx.QueryRow(`SELECT pg_get_serial_sequence(current_schema()||'.'||$1,$2)`, table, column).Scan(&sequence); err != nil {
				return err
			}
			if !sequence.Valid || strings.TrimSpace(sequence.String) == "" {
				continue
			}
			var maximum sql.NullInt64
			if err := tx.QueryRow("SELECT MAX(" + quoteIdent(column) + ") FROM " + quoteIdent(table)).Scan(&maximum); err != nil {
				return err
			}
			if maximum.Valid && maximum.Int64 > 0 {
				if _, err := tx.Exec(`SELECT setval($1,$2,true)`, sequence.String, maximum.Int64); err != nil {
					return err
				}
			} else if _, err := tx.Exec(`SELECT setval($1,1,false)`, sequence.String); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *app) tableExists(table string) bool {
	var n int
	if a.dialect == "postgres" {
		_ = a.queryRow(`SELECT COUNT(*) FROM pg_tables WHERE schemaname=current_schema() AND tablename=?`, table).Scan(&n)
	} else {
		_ = a.queryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n)
	}
	return n > 0
}

func (a *app) spareParts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT spi.id_spare_inventory, COALESCE(spi.spare_part_id,0), COALESCE(sp.name_spare_part, spi.part_name), COALESCE(spi.part_number, sp.part_number_spare_part, ''), COALESCE(spi.quantity,0), COALESCE(spi.condition_status,''), COALESCE(spi.vendor_name,''), COALESCE(spi.description,''), COALESCE(spi.created_at,''), COALESCE(spi.updated_at,''),
			COALESCE((SELECT SUM(ms.quantity_spare) FROM machinery_service ms WHERE ms.spare_part_id=spi.spare_part_id),0),
			COALESCE((SELECT ms.machinery_name FROM machinery_service ms WHERE ms.spare_part_id=spi.spare_part_id ORDER BY ms.id_machinery_service DESC LIMIT 1),''),
			COALESCE((SELECT ms.operator_name FROM machinery_service ms WHERE ms.spare_part_id=spi.spare_part_id ORDER BY ms.id_machinery_service DESC LIMIT 1),''),
			COALESCE((SELECT ms.service_date FROM machinery_service ms WHERE ms.spare_part_id=spi.spare_part_id ORDER BY ms.id_machinery_service DESC LIMIT 1),'')
			FROM spare_parts_inventory spi
			LEFT JOIN spare_part sp ON sp.id_spare_part=spi.spare_part_id
			ORDER BY COALESCE(sp.name_spare_part, spi.part_name), spi.id_spare_inventory DESC`)
		writeRows(w, rows, err, []string{"id", "spare_part_id", "part_name", "part_number", "quantity", "condition_status", "vendor_name", "description", "created_at", "updated_at", "total_used", "last_machine", "last_operator", "last_use_date"})
	case http.MethodPost:
		var p struct {
			ID              int64  `json:"id"`
			SparePartID     int64  `json:"spare_part_id"`
			PartName        string `json:"part_name"`
			PartNumber      string `json:"part_number"`
			Quantity        int64  `json:"quantity"`
			ConditionStatus string `json:"condition_status"`
			VendorName      string `json:"vendor_name"`
			Description     string `json:"description"`
		}
		if !decode(w, r, &p) {
			return
		}
		if strings.TrimSpace(p.PartName) == "" && p.SparePartID == 0 {
			fail(w, 400, "نام قطعه الزامی است")
			return
		}
		if p.Quantity < 0 {
			fail(w, 400, "موجودی نمی‌تواند منفی باشد")
			return
		}
		if strings.TrimSpace(p.ConditionStatus) == "" {
			p.ConditionStatus = "سالم"
		}
		if p.SparePartID == 0 {
			_ = a.queryRow(`SELECT id_spare_part FROM spare_part WHERE name_spare_part=?`, p.PartName).Scan(&p.SparePartID)
			if p.SparePartID == 0 {
				if _, err := a.exec(`INSERT INTO spare_part (name_spare_part, part_number_spare_part) VALUES (?,?)`, p.PartName, p.PartNumber); err != nil {
					fail(w, 500, err.Error())
					return
				}
				_ = a.queryRow(`SELECT id_spare_part FROM spare_part WHERE name_spare_part=?`, p.PartName).Scan(&p.SparePartID)
			}
		}
		if p.ID > 0 {
			writeSave(w, execErr(a.exec(`UPDATE spare_parts_inventory SET spare_part_id=?, part_name=?, part_number=?, quantity=?, condition_status=?, vendor_name=?, description=?, updated_at=datetime('now','localtime') WHERE id_spare_inventory=?`, p.SparePartID, p.PartName, p.PartNumber, p.Quantity, p.ConditionStatus, p.VendorName, p.Description, p.ID)))
			return
		}
		var existingID int64
		_ = a.queryRow(`SELECT id_spare_inventory FROM spare_parts_inventory WHERE spare_part_id=? LIMIT 1`, p.SparePartID).Scan(&existingID)
		var err error
		if existingID > 0 {
			_, err = a.exec(`UPDATE spare_parts_inventory SET part_name=?, part_number=?, quantity=?, condition_status=?, vendor_name=?, description=?, updated_at=datetime('now','localtime') WHERE id_spare_inventory=?`,
				p.PartName, p.PartNumber, p.Quantity, p.ConditionStatus, p.VendorName, p.Description, existingID)
		} else {
			_, err = a.exec(`INSERT INTO spare_parts_inventory (spare_part_id, part_name, part_number, quantity, condition_status, vendor_name, description) VALUES (?,?,?,?,?,?,?)`,
				p.SparePartID, p.PartName, p.PartNumber, p.Quantity, p.ConditionStatus, p.VendorName, p.Description)
		}
		writeSave(w, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) sparePartByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	writeSave(w, execErr(a.exec(`DELETE FROM spare_parts_inventory WHERE id_spare_inventory=?`, id)))
}

func (a *app) machineryServices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT ms.id_machinery_service, ms.machinery_name, ms.service_date, COALESCE(st.name_service_type,''), COALESCE(sp.name_spare_part, spi.part_name, ''), COALESCE(ms.quantity_spare,1), COALESCE(ms.description_service,''), COALESCE(ms.operator_name,''), COALESCE(ms.service_type_id,0), COALESCE(ms.spare_part_id,0)
			FROM machinery_service ms
			LEFT JOIN service_type st ON st.id_service_type=ms.service_type_id
			LEFT JOIN spare_part sp ON sp.id_spare_part=ms.spare_part_id
			LEFT JOIN spare_parts_inventory spi ON spi.spare_part_id=ms.spare_part_id
			ORDER BY ms.service_date DESC, ms.id_machinery_service DESC`)
		writeRows(w, rows, err, []string{"id", "machinery_name", "service_date", "service_type", "spare_part", "quantity", "description", "operator_name", "service_type_id", "spare_part_id"})
	case http.MethodPost:
		var p struct {
			ID            int64  `json:"id"`
			MachineryName string `json:"machinery_name"`
			ServiceDate   string `json:"service_date"`
			ServiceTypeID int64  `json:"service_type_id"`
			SparePartID   int64  `json:"spare_part_id"`
			Quantity      int64  `json:"quantity"`
			Description   string `json:"description"`
			OperatorName  string `json:"operator_name"`
		}
		if !decode(w, r, &p) {
			return
		}
		if p.MachineryName == "" || p.ServiceDate == "" {
			fail(w, 400, "نام ماشین و تاریخ سرویس الزامی است")
			return
		}
		if p.Quantity <= 0 {
			p.Quantity = 1
		}
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if p.ID > 0 {
			var oldPart, oldQty int64
			_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(spare_part_id,0), COALESCE(quantity_spare,1) FROM machinery_service WHERE id_machinery_service=?`, p.ID).Scan(&oldPart, &oldQty)
			if oldPart > 0 {
				_, _ = txExec(a.dialect, tx, `UPDATE spare_parts_inventory SET quantity=quantity+?, updated_at=datetime('now','localtime') WHERE spare_part_id=?`, oldQty, oldPart)
			}
		}
		if p.SparePartID > 0 {
			var stock int64
			if err := txQueryRow(a.dialect, tx, `SELECT COALESCE(quantity,0) FROM spare_parts_inventory WHERE spare_part_id=?`, p.SparePartID).Scan(&stock); err != nil {
				_ = tx.Rollback()
				fail(w, 400, "برای قطعه انتخابی موجودی انبار ثبت نشده است")
				return
			}
			if stock < p.Quantity {
				_ = tx.Rollback()
				fail(w, 400, fmt.Sprintf("موجودی قطعه کافی نیست؛ موجودی فعلی: %d", stock))
				return
			}
			_, err = txExec(a.dialect, tx, `UPDATE spare_parts_inventory SET quantity=quantity-?, updated_at=datetime('now','localtime') WHERE spare_part_id=?`, p.Quantity, p.SparePartID)
			if err != nil {
				_ = tx.Rollback()
				fail(w, 500, err.Error())
				return
			}
		}
		if p.ID > 0 {
			_, err = txExec(a.dialect, tx, `UPDATE machinery_service SET machinery_name=?, service_date=?, service_type_id=?, spare_part_id=?, quantity_spare=?, description_service=?, operator_name=? WHERE id_machinery_service=?`, p.MachineryName, p.ServiceDate, nullID(p.ServiceTypeID), nullID(p.SparePartID), p.Quantity, p.Description, p.OperatorName, p.ID)
		} else {
			_, err = txExec(a.dialect, tx, `INSERT INTO machinery_service (machinery_name, service_date, service_type_id, spare_part_id, quantity_spare, description_service, operator_name) VALUES (?,?,?,?,?,?,?)`, p.MachineryName, p.ServiceDate, nullID(p.ServiceTypeID), nullID(p.SparePartID), p.Quantity, p.Description, p.OperatorName)
		}
		if err != nil {
			_ = tx.Rollback()
			fail(w, 500, err.Error())
			return
		}
		writeSave(w, tx.Commit())
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) machineryServiceByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	tx, err := a.db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	var partID, qty int64
	_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(spare_part_id,0), COALESCE(quantity_spare,1) FROM machinery_service WHERE id_machinery_service=?`, id).Scan(&partID, &qty)
	if partID > 0 {
		_, _ = txExec(a.dialect, tx, `UPDATE spare_parts_inventory SET quantity=quantity+?, updated_at=datetime('now','localtime') WHERE spare_part_id=?`, qty, partID)
	}
	if _, err = txExec(a.dialect, tx, `DELETE FROM machinery_service WHERE id_machinery_service=?`, id); err != nil {
		_ = tx.Rollback()
		fail(w, 500, err.Error())
		return
	}
	writeSave(w, tx.Commit())
}

func (a *app) menus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rows, err := a.query(`SELECT id_menu, menu_key, menu_name, path, COALESCE(icon,''), COALESCE(is_restricted,0), COALESCE(sort_order,0) FROM menu_items WHERE COALESCE(path,'')<>'' ORDER BY sort_order, id_menu`)
	writeRows(w, rows, err, []string{"id", "menu_key", "menu_name", "path", "icon", "is_restricted", "sort_order"})
}

func (a *app) users(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT id_user, username, role, COALESCE(is_active,1), COALESCE(created_at,'') FROM users ORDER BY id_user`)
		writeRows(w, rows, err, []string{"id", "username", "role", "is_active", "created_at"})
	case http.MethodPost:
		var p struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if !decode(w, r, &p) {
			return
		}
		if p.Username == "" || p.Password == "" {
			fail(w, 400, "نام کاربری و رمز عبور الزامی است")
			return
		}
		if p.Role == "" {
			p.Role = "viewer"
		}
		hash, err := hashPassword(p.Password)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if a.dialect == "postgres" {
			session, ok := a.currentSession(r)
			if !ok || session.CompanyID <= 0 {
				fail(w, http.StatusUnauthorized, "نشست معتبر نیست")
				return
			}
			tx, err := a.db.Begin()
			if err != nil {
				fail(w, 500, err.Error())
				return
			}
			defer tx.Rollback()
			var userID int64
			if err := txQueryRow(a.dialect, tx, `INSERT INTO users (username,password_hash,role,is_active) VALUES (?,?,?,1) RETURNING id_user`, p.Username, hash, p.Role).Scan(&userID); err != nil {
				fail(w, 500, err.Error())
				return
			}
			platformUsername := fmt.Sprintf("tenant_%d_user_%d", session.CompanyID, userID)
			if _, err := txExec(a.dialect, tx, `INSERT INTO public.operational_platform_users(tenant_id,local_user_id,username,password_hash,active) VALUES(?,?,?,?,1)`, session.CompanyID, userID, platformUsername, hash); err != nil {
				fail(w, 500, err.Error())
				return
			}
			writeSave(w, tx.Commit())
			return
		}
		writeSave(w, execErr(a.exec(`INSERT INTO users (username,password_hash,role,is_active) VALUES (?,?,?,1)`, p.Username, hash, p.Role)))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *app) userByID(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/users/"), "/")
	if strings.HasSuffix(path, "/menu-access") {
		idPart := strings.TrimSuffix(path, "/menu-access")
		userID, _ := strconv.Atoi(strings.Trim(idPart, "/"))
		if r.Method == http.MethodGet {
			a.userMenuAccess(w, int64(userID))
			return
		}
		if r.Method == http.MethodPost {
			a.saveUserMenuAccess(w, r, int64(userID))
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if strings.HasSuffix(path, "/toggle") {
		id, _ := strconv.Atoi(strings.TrimSuffix(path, "/toggle"))
		if a.dialect == "postgres" {
			session, ok := a.currentSession(r)
			if !ok || session.CompanyID <= 0 {
				fail(w, http.StatusUnauthorized, "نشست معتبر نیست")
				return
			}
			tx, err := a.db.Begin()
			if err != nil {
				fail(w, 500, err.Error())
				return
			}
			defer tx.Rollback()
			var active int64
			if err := txQueryRow(a.dialect, tx, `UPDATE users SET is_active=CASE WHEN COALESCE(is_active,1)=1 THEN 0 ELSE 1 END WHERE id_user=? RETURNING is_active`, id).Scan(&active); err != nil {
				fail(w, 500, err.Error())
				return
			}
			if _, err := txExec(a.dialect, tx, `UPDATE public.operational_platform_users SET active=? WHERE tenant_id=? AND local_user_id=?`, active, session.CompanyID, id); err != nil {
				fail(w, 500, err.Error())
				return
			}
			writeSave(w, tx.Commit())
			return
		}
		_, err := a.exec(`UPDATE users SET is_active=CASE WHEN COALESCE(is_active,1)=1 THEN 0 ELSE 1 END WHERE id_user=?`, id)
		writeSave(w, err)
		return
	}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(pathLast(r.URL.Path))
	if id == 1 {
		fail(w, 400, "ادمین اصلی قابل حذف نیست")
		return
	}
	if a.dialect == "postgres" {
		session, ok := a.currentSession(r)
		if !ok || session.CompanyID <= 0 {
			fail(w, http.StatusUnauthorized, "نشست معتبر نیست")
			return
		}
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		if _, err := txExec(a.dialect, tx, `DELETE FROM public.operational_platform_users WHERE tenant_id=? AND local_user_id=?`, session.CompanyID, id); err != nil {
			fail(w, 500, err.Error())
			return
		}
		if _, err := txExec(a.dialect, tx, `DELETE FROM users WHERE id_user=?`, id); err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeSave(w, tx.Commit())
		return
	}
	writeSave(w, execErr(a.exec(`DELETE FROM users WHERE id_user=?`, id)))
}

func (a *app) userMenuAccess(w http.ResponseWriter, userID int64) {
	rows, err := a.query(`SELECT m.id_menu, m.menu_key, m.menu_name, m.path, COALESCE(m.icon,''), COALESCE(m.is_restricted,0), COALESCE(m.sort_order,0),
		COALESCE(uma.has_access, CASE WHEN COALESCE(m.is_restricted,0)=1 THEN 0 ELSE 1 END)
		FROM menu_items m
		LEFT JOIN user_menu_access uma ON uma.menu_key=m.menu_key AND uma.user_id=?
		WHERE COALESCE(m.path,'')<>''
		ORDER BY m.sort_order, m.id_menu`, userID)
	writeRows(w, rows, err, []string{"id", "menu_key", "menu_name", "path", "icon", "is_restricted", "sort_order", "has_access"})
}

func (a *app) userMenus(userID int64, role string) []record {
	if role == "admin" {
		rows, err := a.query(`SELECT menu_key, menu_name, path, COALESCE(icon,''), COALESCE(is_restricted,0)
			FROM menu_items WHERE COALESCE(path,'')<>'' ORDER BY sort_order, id_menu`)
		if err != nil {
			return []record{}
		}
		defer rows.Close()
		out := []record{}
		for rows.Next() {
			var key, name, path, icon string
			var restricted int64
			_ = rows.Scan(&key, &name, &path, &icon, &restricted)
			out = append(out, record{"menu_key": key, "menu_name": name, "path": path, "icon": icon, "is_restricted": restricted, "has_access": 1})
		}
		return out
	}
	rows, err := a.query(`SELECT m.menu_key, m.menu_name, m.path, COALESCE(m.icon,''), COALESCE(m.is_restricted,0),
		COALESCE(uma.has_access, CASE WHEN COALESCE(m.is_restricted,0)=1 THEN 0 ELSE 1 END)
		FROM menu_items m
		LEFT JOIN user_menu_access uma ON uma.menu_key=m.menu_key AND uma.user_id=?
		WHERE COALESCE(m.path,'')<>''
		ORDER BY m.sort_order, m.id_menu`, userID)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	out := []record{}
	for rows.Next() {
		var key, name, path, icon string
		var restricted, has int64
		_ = rows.Scan(&key, &name, &path, &icon, &restricted, &has)
		if has == 1 {
			out = append(out, record{"menu_key": key, "menu_name": name, "path": path, "icon": icon, "is_restricted": restricted, "has_access": has})
		}
	}
	return out
}

func portalMenuKeySet(keys []string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" && key != "users" {
			out[key] = true
		}
	}
	return out
}

func (a *app) portalMenus(allowed map[string]bool) []record {
	rows, err := a.query(`SELECT menu_key, menu_name, path, COALESCE(icon,''), COALESCE(is_restricted,0)
		FROM menu_items WHERE COALESCE(path,'')<>'' AND menu_key<>'users' ORDER BY sort_order, id_menu`)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	out := []record{}
	for rows.Next() {
		var key, name, path, icon string
		var restricted int64
		_ = rows.Scan(&key, &name, &path, &icon, &restricted)
		if allowed["*"] || allowed[key] {
			out = append(out, record{"menu_key": key, "menu_name": name, "path": path, "icon": icon, "is_restricted": restricted, "has_access": 1})
		}
	}
	return out
}

func (a *app) saveUserMenuAccess(w http.ResponseWriter, r *http.Request, userID int64) {
	var p struct {
		MenuAccess map[string]int `json:"menu_access"`
	}
	if !decode(w, r, &p) {
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	for key, has := range p.MenuAccess {
		if has != 0 {
			has = 1
		}
		_, err = txExec(a.dialect, tx, `INSERT INTO user_menu_access (user_id, menu_key, has_access, granted_by) VALUES (?,?,?,1)
			ON CONFLICT(user_id, menu_key) DO UPDATE SET has_access=excluded.has_access, granted_by=excluded.granted_by, granted_at=datetime('now','localtime')`, userID, key, has)
		if err != nil {
			_ = tx.Rollback()
			fail(w, 500, err.Error())
			return
		}
	}
	writeSave(w, tx.Commit())
}

func (a *app) salonByPath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/salon/")
	if path == "formula" {
		a.salonFormula(w, r)
		return
	}
	if strings.HasPrefix(path, "recent-chelles/") {
		a.recentChelles(w, strings.TrimPrefix(path, "recent-chelles/"))
		return
	}
	if strings.HasPrefix(path, "defaults/") {
		a.salonDefaults(w, strings.TrimPrefix(path, "defaults/"))
		return
	}
	if strings.HasPrefix(path, "pod-options/") {
		rest := strings.TrimPrefix(path, "pod-options/")
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			chelleID, _ := strconv.ParseInt(parts[1], 10, 64)
			a.salonPodOptions(w, parts[0], chelleID)
			return
		}
	}
	if strings.HasPrefix(path, "pod-carryover-info/") {
		rest := strings.TrimPrefix(path, "pod-carryover-info/")
		parts := strings.Split(rest, "/")
		if len(parts) >= 2 {
			a.podCarryoverInfo(w, parts[0], parts[1])
			return
		}
	}
	if path == "pod-carryover" && r.Method == http.MethodPost {
		a.podCarryover(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		id, _ := strconv.Atoi(pathLast(r.URL.Path))
		var machine, shom string
		_ = a.queryRow(`SELECT COALESCE(machin_salon,''),COALESCE(shom_chelle_salon,'') FROM salon WHERE id_salon=?`, id).Scan(&machine, &shom)
		tx, err := a.db.Begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		_, err = txExec(a.dialect, tx, `DELETE FROM salon WHERE id_salon=?`, id)
		if err == nil {
			err = a.rebuildConsumptionTx(tx, machine, shom)
		}
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		writeSave(w, tx.Commit())
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (a *app) podCarryoverInfo(w http.ResponseWriter, machine, oldChelle string) {
	leftover, ham, mosh := a.podLeftover(machine, oldChelle)
	writeJSON(w, record{"success": true, "machine": machine, "old_chelle": oldChelle, "leftover_pod": leftover, "ham_nakh": ham, "mosh_name": mosh})
}

func (a *app) podCarryover(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Machine   string `json:"machine"`
		OldChelle string `json:"old_chelle"`
		NewChelle string `json:"new_chelle"`
		Action    string `json:"action"`
	}
	if !decode(w, r, &p) {
		return
	}
	p.Machine = strings.TrimSpace(p.Machine)
	p.OldChelle = strings.TrimSpace(p.OldChelle)
	p.NewChelle = strings.TrimSpace(p.NewChelle)
	if p.Machine == "" || p.OldChelle == "" || p.Action == "" {
		fail(w, 400, "اطلاعات انتقال پود کامل نیست")
		return
	}
	leftover, ham, mosh := a.podLeftover(p.Machine, p.OldChelle)
	if leftover <= 0 {
		writeJSON(w, record{"success": true, "leftover_pod": 0})
		return
	}
	tx, err := a.db.Begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	note := "مرجوع پود باقیمانده به انبار"
	var yarn string
	var oldChelleID, newChelleID int64
	_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(nakh_name_nakh_salon,''),COALESCE(chelle_id_nakh_salon,0) FROM nakh_salon WHERE shom_machin_nakh_salon=? AND shom_chelle_nakh_salon=? AND COALESCE(nakh_name_nakh_salon,'')<>'' ORDER BY id_nakh_salon DESC LIMIT 1`, p.Machine, p.OldChelle).Scan(&yarn, &oldChelleID)
	if oldChelleID == 0 {
		_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(id_chelle,0) FROM chelle WHERE shom_chelle=? ORDER BY id_chelle DESC LIMIT 1`, p.OldChelle).Scan(&oldChelleID)
	}
	if p.Action == "assign_new" {
		if p.NewChelle == "" {
			_ = tx.Rollback()
			fail(w, 400, "چله جدید مشخص نیست")
			return
		}
		note = "انتقال پود باقیمانده به چله جدید"
		_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(id_chelle,0) FROM chelle WHERE shom_chelle=? AND machin_chelle=? ORDER BY id_chelle DESC LIMIT 1`, p.NewChelle, p.Machine).Scan(&newChelleID)
	}
	_, err = txExec(a.dialect, tx, `INSERT INTO nakh_salon (tarikh_nakh_salon, shom_machin_nakh_salon, ham_nakh_salon, w_nakh_salon, shom_chelle_nakh_salon, mosh_name_nakh_salon, vor_khor_nakh_salon, nakh_name_nakh_salon, chelle_id_nakh_salon) VALUES (?,?,?,?,?,?,?,?,?)`, jalaliToday(), p.Machine, ham, -leftover, p.OldChelle, mosh, note, yarn, oldChelleID)
	if err == nil && p.Action == "assign_new" {
		_, err = txExec(a.dialect, tx, `INSERT INTO nakh_salon (tarikh_nakh_salon, shom_machin_nakh_salon, ham_nakh_salon, w_nakh_salon, shom_chelle_nakh_salon, mosh_name_nakh_salon, vor_khor_nakh_salon, nakh_name_nakh_salon, chelle_id_nakh_salon) VALUES (?,?,?,?,?,?,?,?,?)`, jalaliToday(), p.Machine, ham, leftover, p.NewChelle, mosh, note, yarn, newChelleID)
	}
	if err != nil {
		_ = tx.Rollback()
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, record{"success": tx.Commit() == nil, "leftover_pod": leftover})
}

func (a *app) podLeftover(machine, shom string) (float64, string, string) {
	assigned, output, podUsed, podWaste, generalWaste := 0.0, 0.0, 0.0, 0.0, 0.0
	ham, mosh := "", ""
	var chelleID int64
	_ = a.queryRow(`SELECT COALESCE(MAX(chelle_id_nakh_salon),0) FROM nakh_salon WHERE shom_machin_nakh_salon=? AND shom_chelle_nakh_salon=?`, machine, shom).Scan(&chelleID)
	if chelleID == 0 {
		_ = a.queryRow(`SELECT COALESCE(MAX(id_chelle),0) FROM chelle WHERE shom_chelle=?`, shom).Scan(&chelleID)
	}
	whereMachine, args := machineWhere("shom_machin_nakh_salon", machine)
	args = append(args, chelleID, shom)
	_ = a.queryRow(`SELECT COALESCE(SUM(w_nakh_salon),0), COALESCE(MAX(ham_nakh_salon),''), COALESCE(MAX(mosh_name_nakh_salon),'') FROM nakh_salon WHERE `+whereMachine+` AND (chelle_id_nakh_salon=? OR (COALESCE(chelle_id_nakh_salon,0)=0 AND shom_chelle_nakh_salon=?))`, args...).Scan(&assigned, &ham, &mosh)
	whereSalon, salonArgs := machineWhere("machin_salon", machine)
	salonArgs = append(salonArgs, chelleID, shom)
	_ = a.queryRow(`SELECT COALESCE(SUM(w_salon),0),COALESCE(SUM(w_salon*COALESCE(pod_percent_salon,50)/100.0),0) FROM salon WHERE `+whereSalon+` AND (chelle_id_salon=? OR (COALESCE(chelle_id_salon,0)=0 AND shom_chelle_salon=?))`, salonArgs...).Scan(&output, &podUsed)
	_ = a.queryRow(`SELECT COALESCE(SUM(CASE WHEN waste_type='pod' THEN weight ELSE 0 END),0), COALESCE(SUM(CASE WHEN waste_type NOT IN ('tar','pod') THEN weight ELSE 0 END),0) FROM production_waste WHERE machine=? AND (chelle_id_waste=? OR (COALESCE(chelle_id_waste,0)=0 AND shom_chelle=?))`, machine, chelleID, shom).Scan(&podWaste, &generalWaste)
	podRatio := 0.5
	if output > 0 {
		podRatio = podUsed / output
	}
	leftover := assigned - podUsed - podWaste - generalWaste*podRatio
	if leftover < 0 {
		leftover = 0
	}
	return leftover, ham, mosh
}

func (a *app) recentMachinePodHambafts(machine string, limit int) []string {
	where, args := machineWhere("shom_machin_nakh_salon", machine)
	rows, err := a.query(`SELECT COALESCE(ham_nakh_salon,'') FROM nakh_salon WHERE `+where+` AND COALESCE(ham_nakh_salon,'')<>'' AND COALESCE(w_nakh_salon,0)>0 ORDER BY id_nakh_salon DESC`, args...)
	if err != nil {
		return []string{}
	}
	defer rows.Close()
	items := []string{}
	seen := map[string]bool{}
	for rows.Next() {
		var item string
		if rows.Scan(&item) != nil {
			continue
		}
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		items = append(items, item)
		if len(items) == limit {
			break
		}
	}
	return items
}

func (a *app) salonDefaults(w http.ResponseWriter, machine string) {
	podHambafts := a.recentMachinePodHambafts(machine, 2)
	where, args := machineWhere("s.machin_salon", machine)
	row := a.queryRow(`SELECT s.kala_salon, COALESCE(k.id_kala_name,0), s.ham_pod_salon, s.ham_chelle_salon, s.shom_chelle_salon, COALESCE(s.chelle_id_salon,0) FROM salon s LEFT JOIN kala_name k ON k.name_kala_name=s.kala_salon WHERE `+where+` ORDER BY s.id_salon DESC LIMIT 1`, args...)
	var kala, hamPod, hamChelle, shom string
	var kalaID, chelleID int64
	if err := row.Scan(&kala, &kalaID, &hamPod, &hamChelle, &shom, &chelleID); err != nil {
		if len(podHambafts) > 0 {
			writeJSON(w, record{"success": true, "found": true, "ham_pod": podHambafts[0], "pod_hambafts": podHambafts})
			return
		}
		writeJSON(w, record{"success": true, "found": false, "pod_hambafts": podHambafts})
		return
	}
	if len(podHambafts) > 0 {
		hamPod = podHambafts[0]
	}
	writeJSON(w, record{
		"success": true, "found": true, "kala": kala, "kala_id": kalaID,
		"ham_pod": hamPod, "pod_hambafts": podHambafts, "ham_chelle": hamChelle, "shom_chelle": shom, "chelle_id": chelleID,
	})
}

func (a *app) salonPodOptions(w http.ResponseWriter, machine string, chelleID int64) {
	machine = normalizeStoredMachine(machine)
	if machine == "" || chelleID <= 0 {
		fail(w, http.StatusBadRequest, "شماره ماشین و چله الزامی است")
		return
	}
	resolvedID, _, shom, err := a.productionChelleInfo(machine, chelleID, "")
	if err != nil || resolvedID == 0 {
		fail(w, http.StatusBadRequest, "چله انتخاب‌شده روی این ماشین فعال نیست")
		return
	}
	whereMachine, args := machineWhere("ns.shom_machin_nakh_salon", machine)
	args = append(args, resolvedID, shom)
	rows, err := a.query(`SELECT TRIM(ns.ham_nakh_salon), COALESCE(MAX(ns.nakh_name_nakh_salon),''), COALESCE(MAX(ns.mosh_name_nakh_salon),''), COALESCE(SUM(ns.w_nakh_salon),0)
		FROM nakh_salon ns
		WHERE `+whereMachine+`
		  AND (chelle_id_nakh_salon=? OR (COALESCE(chelle_id_nakh_salon,0)=0 AND shom_chelle_nakh_salon=?))
		  AND TRIM(COALESCE(ns.ham_nakh_salon,''))<>''
		GROUP BY TRIM(ns.ham_nakh_salon)
		HAVING COALESCE(SUM(ns.w_nakh_salon),0)>0.001
		ORDER BY MAX(ns.id_nakh_salon) DESC
		LIMIT 2`, args...)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var hambaft, yarn, owner string
		var balance float64
		if err := rows.Scan(&hambaft, &yarn, &owner, &balance); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, record{"hambaft": hambaft, "yarn": yarn, "owner": owner, "balance": balance})
	}
	writeJSON(w, record{"success": true, "items": items})
}

func (a *app) recentChelles(w http.ResponseWriter, machine string) {
	where, args := machineWhere("g.machin_gere", machine)
	rows, err := a.query(`SELECT g.id_gere, COALESCE(g.tarikh_gere,''), g.shom_chelle_gere, g.machin_gere, COALESCE(c.w_chelle,0), COALESCE(c.hambaft_chelle,''), COALESCE(c.id_chelle,0) FROM gere g LEFT JOIN chelle c ON c.id_chelle=g.chelle_id_gere WHERE `+where+` AND COALESCE(g.shom_chelle_gere,'')<>'' ORDER BY COALESCE(g.tarikh_gere,'') DESC, g.id_gere DESC`, args...)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []record{}
	seen := map[string]bool{}
	for rows.Next() {
		var id, chelleID int64
		var tarikh, shom, mach, hambaft string
		var weight float64
		_ = rows.Scan(&id, &tarikh, &shom, &mach, &weight, &hambaft, &chelleID)
		if seen[shom] {
			continue
		}
		seen[shom] = true
		items = append(items, record{"id_gere": id, "chelle_id": chelleID, "tarikh": tarikh, "shom_chelle": shom, "machine": mach, "weight": weight, "hambaft": hambaft})
		if len(items) == 2 {
			break
		}
	}
	writeJSON(w, record{"success": true, "items": items})
}

func (a *app) resetCycle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.Confirm != "پاک شود" {
		fail(w, 400, "برای پاک کردن اطلاعات سیکل باید عبارت «پاک شود» ارسال شود")
		return
	}
	for _, tbl := range []string{"salon", "nakh_salon", "gere", "chelle_formul", "chelle", "nakh_vor", "machine_consumption"} {
		if _, err := a.exec("DELETE FROM " + tbl); err != nil {
			fail(w, 500, err.Error())
			return
		}
	}
	writeJSON(w, record{"success": true})
}

func (a *app) rebuildConsumptionTx(tx *sql.Tx, machine, shom string) error {
	machine = strings.TrimSpace(machine)
	shom = strings.TrimSpace(shom)
	if machine == "" || shom == "" {
		return nil
	}
	if _, err := txExec(a.dialect, tx, `DELETE FROM machine_consumption WHERE machine=? AND shom_chelle=?`, machine, shom); err != nil {
		return err
	}
	var output, tarUsed, podUsed, chelleWeight, podAssigned, waste float64
	var chelleID int64
	_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(MAX(id_chelle),0) FROM chelle WHERE machin_chelle=? AND shom_chelle=?`, machine, shom).Scan(&chelleID)
	if chelleID == 0 {
		_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(MAX(chelle_id_salon),0) FROM salon WHERE machin_salon=? AND shom_chelle_salon=?`, machine, shom).Scan(&chelleID)
	}
	if chelleID == 0 {
		_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(MAX(chelle_id_waste),0) FROM production_waste WHERE machine=? AND shom_chelle=?`, machine, shom).Scan(&chelleID)
	}
	if chelleID == 0 {
		_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(MAX(id_chelle),0) FROM chelle WHERE shom_chelle=?`, shom).Scan(&chelleID)
	}
	_ = txQueryRow(a.dialect, tx, `SELECT
		COALESCE(SUM(w_salon),0),
		COALESCE(SUM(w_salon*COALESCE(tar_percent_salon,50)/100.0),0),
		COALESCE(SUM(w_salon*COALESCE(pod_percent_salon,50)/100.0),0)
		FROM salon WHERE machin_salon=? AND (chelle_id_salon=? OR (COALESCE(chelle_id_salon,0)=0 AND shom_chelle_salon=?))`, machine, chelleID, shom).Scan(&output, &tarUsed, &podUsed)
	_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(w_chelle,0) FROM chelle WHERE id_chelle=?`, chelleID).Scan(&chelleWeight)
	_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(SUM(w_nakh_salon),0) FROM nakh_salon WHERE shom_machin_nakh_salon=? AND (chelle_id_nakh_salon=? OR (COALESCE(chelle_id_nakh_salon,0)=0 AND shom_chelle_nakh_salon=?))`, machine, chelleID, shom).Scan(&podAssigned)
	_ = txQueryRow(a.dialect, tx, `SELECT COALESCE(SUM(weight),0) FROM production_waste WHERE machine=? AND (chelle_id_waste=? OR (COALESCE(chelle_id_waste,0)=0 AND shom_chelle=?))`, machine, chelleID, shom).Scan(&waste)
	remaining := chelleWeight + podAssigned - output - waste
	_, err := txExec(a.dialect, tx, `INSERT INTO machine_consumption (machine,shom_chelle,tar_used,pod_used,total_weight,remaining_weight,tarikh_consumption) VALUES (?,?,?,?,?,?,?)`, machine, shom, tarUsed, podUsed, output, remaining, jalaliToday())
	return err
}

func (a *app) rebuildMachineConsumptionTx(tx *sql.Tx, machine string) error {
	rows, err := tx.Query(rebind(a.dialect, `SELECT DISTINCT shom_chelle_salon FROM salon WHERE machin_salon=? AND COALESCE(shom_chelle_salon,'')<>''`), machine)
	if err != nil {
		return err
	}
	chelles := []string{}
	for rows.Next() {
		var shom string
		if err := rows.Scan(&shom); err != nil {
			rows.Close()
			return err
		}
		chelles = append(chelles, shom)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, shom := range chelles {
		if err := a.rebuildConsumptionTx(tx, machine, shom); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) rebuildMachineConsumption(machine string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := a.rebuildMachineConsumptionTx(tx, machine); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *app) lookup(table, idCol, nameCol string) []lookupItem {
	rows, err := a.query(fmt.Sprintf("SELECT %s, %s FROM %s WHERE COALESCE(%s,'')<>'' ORDER BY %s", idCol, nameCol, table, nameCol, nameCol))
	if err != nil {
		return []lookupItem{}
	}
	defer rows.Close()
	items := []lookupItem{}
	for rows.Next() {
		var it lookupItem
		_ = rows.Scan(&it.ID, &it.Name)
		items = append(items, it)
	}
	return items
}

func (a *app) productionSummary(where string, args ...any) record {
	q := `SELECT COALESCE(SUM(metr_salon),0), COALESCE(SUM(w_salon),0), COUNT(*) FROM salon WHERE ` + where
	var metr, weight float64
	var pieces int64
	_ = a.queryRow(q, args...).Scan(&metr, &weight, &pieces)
	return record{"metr": metr, "weight": weight, "pieces": pieces}
}

func (a *app) productionByMachine(date string) []record {
	rows, err := a.query(`SELECT COALESCE(machin_salon,''), COUNT(*), COALESCE(SUM(metr_salon),0), COALESCE(SUM(w_salon),0)
		FROM salon
		WHERE tarikh_salon=?
		GROUP BY machin_salon
		ORDER BY CAST(machin_salon AS REAL), machin_salon`, date)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	out := []record{}
	for rows.Next() {
		var machine string
		var pieces int64
		var metr, weight float64
		_ = rows.Scan(&machine, &pieces, &metr, &weight)
		out = append(out, record{"machine": machine, "pieces": pieces, "metr": metr, "weight": weight})
	}
	return out
}

func (a *app) stockSummary() record {
	rows, err := a.query(`SELECT s.kala_salon, COUNT(*), COALESCE(SUM(s.metr_salon),0), COALESCE(SUM(s.w_salon),0)
		FROM salon s
		WHERE s.id_salon NOT IN (SELECT DISTINCT CAST(taghe_cod_f_khor AS INTEGER) FROM f_khor WHERE COALESCE(taghe_cod_f_khor,'')<>'')
		GROUP BY s.kala_salon ORDER BY s.kala_salon`)
	if err != nil {
		return record{"items": []record{}, "total_taghe": 0, "total_metr": 0, "total_weight": 0}
	}
	defer rows.Close()
	items := []record{}
	totalTaghe := int64(0)
	totalMetr, totalWeight := 0.0, 0.0
	for rows.Next() {
		var kala string
		var cnt int64
		var metr, weight float64
		_ = rows.Scan(&kala, &cnt, &metr, &weight)
		items = append(items, record{"kala": kala, "taghe_count": cnt, "metr": metr, "weight": weight})
		totalTaghe += cnt
		totalMetr += metr
		totalWeight += weight
	}
	return record{"items": items, "total_taghe": totalTaghe, "total_metr": totalMetr, "total_weight": totalWeight}
}

func (a *app) yarnInventory() []record {
	rows, err := a.query(`
		WITH yarn_keys AS (
			SELECT hambaft_nakh_vor AS hambaft, moshname_nakh_vor AS mosh, COALESCE(nakh_name_nakh_vor,'') AS yarn FROM nakh_vor
			UNION
			SELECT hambaft_nakh_khor AS hambaft, COALESCE(NULLIF(owner_mosh_nakh_khor,''),moshname_nakh_khor) AS mosh, COALESCE(nakh_name_nakh_khor,'') AS yarn FROM nakh_khor
			UNION
			SELECT ham_nakh_salon AS hambaft, mosh_name_nakh_salon AS mosh, COALESCE(nakh_name_nakh_salon,'') AS yarn FROM nakh_salon
		)
		SELECT k.hambaft, k.mosh, k.yarn,
			COALESCE((SELECT SUM(w_vor_nakh_vor) FROM nakh_vor v WHERE v.hambaft_nakh_vor=k.hambaft AND v.moshname_nakh_vor=k.mosh AND COALESCE(v.nakh_name_nakh_vor,'')=k.yarn),0) AS vorud,
			COALESCE((SELECT SUM(w_nakh_salon) FROM nakh_salon s WHERE s.ham_nakh_salon=k.hambaft AND s.mosh_name_nakh_salon=k.mosh AND COALESCE(s.nakh_name_nakh_salon,'')=k.yarn),0) AS salon,
			COALESCE((SELECT SUM(ABS(w_vor_nakh_khor)) FROM nakh_khor kh WHERE kh.hambaft_nakh_khor=k.hambaft AND COALESCE(NULLIF(kh.owner_mosh_nakh_khor,''),kh.moshname_nakh_khor)=k.mosh AND COALESCE(kh.nakh_name_nakh_khor,'')=k.yarn),0) AS khoroj
		FROM yarn_keys k
		WHERE COALESCE(k.hambaft,'')<>'' AND COALESCE(k.mosh,'')<>''
		ORDER BY k.mosh, k.hambaft, k.yarn`)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var h, m, yarn string
		var vor, salon, khor float64
		_ = rows.Scan(&h, &m, &yarn, &vor, &salon, &khor)
		inv := vor - salon - khor
		items = append(items, record{"hambaft": h, "mosh": m, "yarn": yarn, "inventory": inv, "vorud": vor, "to_salon": salon, "khoroj": khor, "data_complete": yarn != ""})
	}
	return items
}

func (a *app) machineStatus() []record {
	items, err := a.activeMachineStatus()
	if err != nil {
		return []record{}
	}
	return items
}

func (a *app) activeMachineStatus() ([]record, error) {
	rows, err := a.query(`
		SELECT machin_chelle AS machine, shom_chelle, COALESCE(tarikh_chelle,'') AS tarikh, id_chelle AS sort_id
		FROM chelle
		WHERE COALESCE(machin_chelle,'')<>'' AND COALESCE(shom_chelle,'')<>''
		ORDER BY machine, tarikh DESC, sort_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type activeChelle struct {
		machine string
		shom    string
		tarikh  string
		id      int64
	}
	active := []activeChelle{}
	seen := map[string]bool{}
	for rows.Next() {
		var machine, shom, tarikh string
		var sortID int64
		if err := rows.Scan(&machine, &shom, &tarikh, &sortID); err != nil {
			return nil, err
		}
		key := strings.TrimSpace(machine)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		active = append(active, activeChelle{machine: machine, shom: shom, tarikh: tarikh, id: sortID})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := []record{}
	for _, row := range active {
		machine, shom, tarikh, chelleID := row.machine, row.shom, row.tarikh, row.id
		var chelleWeight, totalWeight, totalMeter, podAssigned, tarPercent, podPercent, tarUsed, podUsed float64
		var tarWaste, podWaste, generalWaste float64
		tarPercent, podPercent = 50, 50
		_ = a.queryRow(`SELECT COALESCE(w_chelle,0) FROM chelle WHERE id_chelle=?`, chelleID).Scan(&chelleWeight)
		_ = a.queryRow(`SELECT
			COALESCE(SUM(w_salon),0), COALESCE(SUM(metr_salon),0),
			COALESCE(SUM(w_salon*COALESCE(tar_percent_salon,50)/100.0),0),
			COALESCE(SUM(w_salon*COALESCE(pod_percent_salon,50)/100.0),0)
			FROM salon WHERE machin_salon=? AND (chelle_id_salon=? OR (COALESCE(chelle_id_salon,0)=0 AND shom_chelle_salon=?))`, machine, chelleID, shom).Scan(&totalWeight, &totalMeter, &tarUsed, &podUsed)
		_ = a.queryRow(`SELECT COALESCE(SUM(w_nakh_salon),0) FROM nakh_salon WHERE shom_machin_nakh_salon=? AND (chelle_id_nakh_salon=? OR (COALESCE(chelle_id_nakh_salon,0)=0 AND shom_chelle_nakh_salon=?))`, machine, chelleID, shom).Scan(&podAssigned)
		if totalWeight > 0 {
			tarPercent = tarUsed * 100 / totalWeight
			podPercent = podUsed * 100 / totalWeight
		}
		_ = a.queryRow(`SELECT
			COALESCE(SUM(CASE WHEN waste_type='tar' THEN weight ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN waste_type='pod' THEN weight ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN waste_type NOT IN ('tar','pod') THEN weight ELSE 0 END),0)
			FROM production_waste WHERE machine=? AND (chelle_id_waste=? OR (COALESCE(chelle_id_waste,0)=0 AND shom_chelle=?))`, machine, chelleID, shom).Scan(&tarWaste, &podWaste, &generalWaste)
		actualWaste := tarWaste + podWaste + generalWaste
		used := tarUsed + podUsed
		totalAvailable := chelleWeight + podAssigned
		tarRemainingRaw := chelleWeight - tarUsed - tarWaste - generalWaste*tarPercent/100
		podRemainingRaw := podAssigned - podUsed - podWaste - generalWaste*podPercent/100
		materialShortage := 0.0
		if tarRemainingRaw < 0 {
			materialShortage += -tarRemainingRaw
		}
		if podRemainingRaw < 0 {
			materialShortage += -podRemainingRaw
		}
		tarRemaining := tarRemainingRaw
		podRemaining := podRemainingRaw
		if tarRemaining < 0 {
			tarRemaining = 0
		}
		if podRemaining < 0 {
			podRemaining = 0
		}
		remaining := tarRemaining + podRemaining
		remainingPercent := 0.0
		if totalAvailable > 0 {
			remainingPercent = remaining * 100 / totalAvailable
		}
		wastePerMeter := 0.0
		if totalMeter > 0 {
			wastePerMeter = actualWaste / totalMeter
		}
		wastePerKg := 0.0
		if totalWeight > 0 {
			wastePerKg = actualWaste / totalWeight
		}
		wastePercentInput := 0.0
		if totalAvailable > 0 {
			wastePercentInput = actualWaste * 100 / totalAvailable
		}
		wastePercentOutput := 0.0
		if totalWeight+actualWaste > 0 {
			wastePercentOutput = actualWaste * 100 / (totalWeight + actualWaste)
		}
		items = append(items, record{
			"machine": machine, "shom_chelle": shom, "chelle_id": chelleID, "tarikh": tarikh,
			"chelle_weight": chelleWeight, "pod_assigned": podAssigned, "total_available": totalAvailable,
			"tar_percent": tarPercent, "pod_percent": podPercent,
			"tar_used": tarUsed, "pod_used": podUsed, "total_used": used,
			"total_weight": totalWeight, "total_meter": totalMeter,
			"tar_remaining": tarRemaining, "pod_remaining": podRemaining,
			"remaining": remaining, "remaining_percent": remainingPercent, "material_shortage": materialShortage,
			"actual_waste": actualWaste, "tar_waste": tarWaste, "pod_waste": podWaste, "general_waste": generalWaste,
			"waste_weight": actualWaste, "waste_per_meter": wastePerMeter,
			"waste_per_kg": wastePerKg, "waste_percent_per_kg": wastePercentOutput,
			"waste_percent_input": wastePercentInput, "waste_percent_output": wastePercentOutput,
		})
	}
	return items, nil
}

func (a *app) latestSalon(limit int) []record {
	rows, err := a.query(`SELECT id_salon, tarikh_salon, machin_salon, kala_salon, metr_salon, w_salon FROM salon ORDER BY id_salon DESC LIMIT ?`, limit)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var id int64
		var tarikh, machine, kala string
		var metr, weight float64
		_ = rows.Scan(&id, &tarikh, &machine, &kala, &metr, &weight)
		items = append(items, record{"id": id, "tarikh": tarikh, "machine": machine, "kala": kala, "metr": metr, "weight": weight})
	}
	return items
}

func (a *app) monthProduction(month string) []record {
	rows, err := a.query(`SELECT machin_salon, COALESCE(SUM(metr_salon),0), COALESCE(SUM(w_salon),0), COUNT(*)
		FROM salon
		WHERE SUBSTR(tarikh_salon,1,7)=?
		GROUP BY machin_salon
		ORDER BY CAST(machin_salon AS REAL), machin_salon`, month)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var machine string
		var metr, weight float64
		var pieces int64
		_ = rows.Scan(&machine, &metr, &weight, &pieces)
		items = append(items, record{"machine": machine, "metr": metr, "weight": weight, "pieces": pieces})
	}
	return items
}

func (a *app) latestNakhKhor(limit int) []record {
	rows, err := a.query(`SELECT id_nakh_khor, tarikh_nakh_khor, hambaft_nakh_khor, ABS(COALESCE(w_vor_nakh_khor,0)), moshname_nakh_khor, nakh_name_nakh_khor FROM nakh_khor ORDER BY id_nakh_khor DESC LIMIT ?`, limit)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var id int64
		var tarikh, h, m, n string
		var weight float64
		_ = rows.Scan(&id, &tarikh, &h, &weight, &m, &n)
		items = append(items, record{"id": id, "tarikh": tarikh, "hambaft": h, "weight": weight, "mosh": m, "nakh": n})
	}
	return items
}

func (a *app) warperYarnBalances() []record {
	rows, err := a.query(`
		WITH keys AS (
			SELECT kh.moshname_nakh_khor AS warper, COALESCE(NULLIF(kh.owner_mosh_nakh_khor,''),kh.moshname_nakh_khor) AS owner, kh.hambaft_nakh_khor AS hambaft, kh.nakh_name_nakh_khor AS yarn
			FROM nakh_khor kh
			WHERE COALESCE(kh.destination_type_nakh_khor,'warper')='warper'
			UNION
			SELECT c.pich_chelle AS warper, c.mosh_chelle AS owner, c.hambaft_chelle AS hambaft, c.nakh_chelle AS yarn
			FROM chelle c
			WHERE COALESCE(c.pich_chelle,'')<>''
		)
		SELECT k.warper, k.owner, k.hambaft, k.yarn,
			COALESCE((SELECT SUM(ABS(COALESCE(kh.w_vor_nakh_khor,0))) FROM nakh_khor kh WHERE kh.moshname_nakh_khor=k.warper AND COALESCE(kh.destination_type_nakh_khor,'warper')='warper' AND COALESCE(NULLIF(kh.owner_mosh_nakh_khor,''),kh.moshname_nakh_khor)=k.owner AND kh.hambaft_nakh_khor=k.hambaft AND kh.nakh_name_nakh_khor=k.yarn),0) AS sent_weight,
			COALESCE((SELECT SUM(COALESCE(c.w_chelle,0)) FROM chelle c WHERE c.pich_chelle=k.warper AND c.mosh_chelle=k.owner AND c.hambaft_chelle=k.hambaft AND c.nakh_chelle=k.yarn),0) AS returned_weight,
			COALESCE((SELECT COUNT(*) FROM chelle c WHERE c.pich_chelle=k.warper AND c.mosh_chelle=k.owner AND c.hambaft_chelle=k.hambaft AND c.nakh_chelle=k.yarn),0) AS chelle_count,
			COALESCE((SELECT MAX(kh.tarikh_nakh_khor) FROM nakh_khor kh WHERE kh.moshname_nakh_khor=k.warper AND COALESCE(kh.destination_type_nakh_khor,'warper')='warper' AND COALESCE(NULLIF(kh.owner_mosh_nakh_khor,''),kh.moshname_nakh_khor)=k.owner AND kh.hambaft_nakh_khor=k.hambaft AND kh.nakh_name_nakh_khor=k.yarn),'') AS last_sent_date,
			COALESCE((SELECT MAX(c.tarikh_chelle) FROM chelle c WHERE c.pich_chelle=k.warper AND c.mosh_chelle=k.owner AND c.hambaft_chelle=k.hambaft AND c.nakh_chelle=k.yarn),'') AS last_return_date
		FROM keys k
		WHERE COALESCE(k.warper,'')<>'' AND COALESCE(k.owner,'')<>'' AND COALESCE(k.hambaft,'')<>'' AND COALESCE(k.yarn,'')<>''
		ORDER BY k.warper, k.owner, k.hambaft, k.yarn`)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var warper, owner, hambaft, yarn, lastSent, lastReturn string
		var sent, returned float64
		var chelleCount int64
		if err := rows.Scan(&warper, &owner, &hambaft, &yarn, &sent, &returned, &chelleCount, &lastSent, &lastReturn); err != nil {
			return []record{}
		}
		balance := sent - returned
		items = append(items, record{
			"warper": warper, "owner": owner, "hambaft": hambaft, "yarn": yarn,
			"sent_weight": sent, "returned_weight": returned, "balance_weight": balance,
			"chelle_count": chelleCount, "last_sent_date": lastSent, "last_return_date": lastReturn,
		})
	}
	if err := rows.Err(); err != nil {
		return []record{}
	}
	return items
}

func (a *app) warperYarnBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, a.warperYarnBalances())
}

func (a *app) latestOutInvoices(limit int) []record {
	rows, err := a.query(`SELECT shom_f_khor, MIN(tarikh_f_khor), MIN(mosh_f_khor), MIN(shomare_sanad), MIN(COALESCE(kala_name_f_khor,'')), COUNT(*)
		FROM f_khor GROUP BY shom_f_khor ORDER BY MAX(id_f_khor) DESC LIMIT ?`, limit)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var no, tarikh, mosh, sanad, kala string
		var cnt int64
		_ = rows.Scan(&no, &tarikh, &mosh, &sanad, &kala, &cnt)
		items = append(items, record{"invoice_no": no, "tarikh": tarikh, "mosh": mosh, "sanad": sanad, "kala": kala, "taghe_count": cnt})
	}
	return items
}

func (a *app) financialMismatchReports() []record {
	rows, err := a.query(`SELECT source_type, source_id, COALESCE(invoice_no,''), COALESCE(invoice_kind,''), title, message, COALESCE(reported_at,'') FROM financial_mismatch_reports WHERE COALESCE(status,'open')='open' ORDER BY id DESC LIMIT 50`)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var sourceType, sourceID, invoiceNo, invoiceKind, title, message, reportedAt string
		if err := rows.Scan(&sourceType, &sourceID, &invoiceNo, &invoiceKind, &title, &message, &reportedAt); err != nil {
			continue
		}
		path := "/reports"
		switch sourceType {
		case "operational_yarn_in":
			path = "/nakh-vor"
		case "operational_chelle_in":
			path = "/chelle"
		case "operational_spare_part":
			path = "/spare-parts"
		case "operational_misc":
			path = "/v-kh-moto"
		}
		if invoiceNo != "" {
			message = fmt.Sprintf("%s | فاکتور مالی: %s", message, invoiceNo)
		}
		if invoiceKind != "" {
			title = fmt.Sprintf("%s - %s", title, invoiceKind)
		}
		items = append(items, record{"type": "critical", "code": "financial-mismatch-" + sourceType + "-" + sourceID, "title": title, "message": message, "path": path, "source_type": sourceType, "source_id": sourceID, "reported_at": reportedAt})
	}
	return items
}

func (a *app) notifications() []record {
	items := []record{}
	items = append(items, a.financialMismatchReports()...)
	for _, y := range a.yarnInventory() {
		inv, _ := y["inventory"].(float64)
		if complete, _ := y["data_complete"].(bool); !complete {
			items = append(items, record{"type": "warning", "code": "yarn-type-missing", "title": "نوع نخ نامشخص", "message": fmt.Sprintf("گردش قدیمی هم‌بافت %v برای %v نوع نخ ندارد و باید تکمیل شود", y["hambaft"], y["mosh"])})
		} else if inv < -0.001 {
			items = append(items, record{"type": "critical", "code": "negative-yarn", "title": "کسری موجودی نخ", "message": fmt.Sprintf("%v / %v / %v دارای %.1f کیلو کسری است", y["mosh"], y["hambaft"], y["yarn"], -inv)})
		} else if inv < 10 {
			items = append(items, record{"type": "warning", "code": "low-yarn", "title": "موجودی نخ کم", "message": fmt.Sprintf("%v / %v / %v فقط %.1f کیلو موجودی دارد", y["mosh"], y["hambaft"], y["yarn"], inv)})
		}
	}
	for _, wb := range a.warperYarnBalances() {
		balance, _ := wb["balance_weight"].(float64)
		if balance < -0.001 {
			items = append(items, record{"type": "critical", "code": "warper-over-return", "title": "برگشت چله بیش از نخ ارسالی", "message": fmt.Sprintf("چله‌پیچ %v برای مالک %v مقدار %.1f کیلو بیش از ارسال، چله برگشت داده است", wb["warper"], wb["owner"], -balance)})
		} else if balance > 0.001 {
			items = append(items, record{"type": "info", "code": "warper-pending", "title": "نخ نزد چله‌پیچ", "message": fmt.Sprintf("%.1f کیلو نخ %v متعلق به %v نزد چله‌پیچ %v مانده است", balance, wb["yarn"], wb["owner"], wb["warper"])})
		}
	}
	if machines, err := a.activeMachineStatus(); err == nil {
		for _, machine := range machines {
			shortage, _ := machine["material_shortage"].(float64)
			remainingPct, _ := machine["remaining_percent"].(float64)
			wastePct, _ := machine["waste_percent_input"].(float64)
			if shortage > 0.001 {
				items = append(items, record{"type": "critical", "code": "machine-shortage", "title": "کسری مواد روی ماشین", "message": fmt.Sprintf("ماشین %v چله %v حدود %.1f کیلو کسری ثبت مواد دارد", machine["machine"], machine["shom_chelle"], shortage)})
			} else if remainingPct <= 10 {
				items = append(items, record{"type": "critical", "code": "machine-low", "title": "اتمام نزدیک مواد ماشین", "message": fmt.Sprintf("مانده مواد ماشین %v چله %v به %.1f%% رسیده است", machine["machine"], machine["shom_chelle"], remainingPct)})
			} else if remainingPct <= 25 {
				items = append(items, record{"type": "warning", "code": "machine-warning", "title": "مانده مواد ماشین کم", "message": fmt.Sprintf("مانده مواد ماشین %v چله %v برابر %.1f%% است", machine["machine"], machine["shom_chelle"], remainingPct)})
			}
			if wastePct > 5 {
				items = append(items, record{"type": "warning", "code": "high-waste", "title": "ضایعات بالاتر از حد کنترل", "message": fmt.Sprintf("ضایعات ماشین %v چله %v برابر %.1f%% ورودی مواد است", machine["machine"], machine["shom_chelle"], wastePct)})
			}
		}
	}
	var lowParts int64
	_ = a.queryRow(`SELECT COUNT(*) FROM spare_parts_inventory WHERE COALESCE(quantity,0)<=0`).Scan(&lowParts)
	if lowParts > 0 {
		items = append(items, record{"type": "warning", "code": "parts-empty", "title": "کسری قطعات یدکی", "message": fmt.Sprintf("موجودی %d قلم قطعه صفر است", lowParts)})
	}
	stock := a.stockSummary()
	if total, ok := stock["total_taghe"].(int64); ok && total > 0 {
		items = append(items, record{"type": "info", "code": "uninvoiced-production", "title": "طاقه‌های خروج نخورده", "message": fmt.Sprintf("%d طاقه در انبار موجود است که هنوز فاکتور خروج نخورده‌اند", total)})
	}
	for _, quality := range a.operationalDataQuality() {
		count, _ := quality["count"].(int64)
		if count == 0 {
			continue
		}
		code := fmt.Sprint(quality["code"])
		if code == "invalid-formula" || code == "multi-active" || code == "duplicate-chelle" {
			items = append(items, record{"type": "warning", "code": code, "title": quality["title"], "message": fmt.Sprintf("%d مورد نیازمند بررسی در کنترل کیفیت داده ثبت شده است", count)})
		}
	}
	for _, item := range items {
		if _, exists := item["path"]; exists {
			continue
		}
		code := fmt.Sprint(item["code"])
		path := "/reports"
		switch {
		case strings.Contains(code, "yarn") || strings.Contains(code, "warper"):
			path = "/nakh-khor"
		case strings.Contains(code, "waste") || strings.Contains(code, "machine"):
			path = "/consumption"
		case strings.Contains(code, "formula"):
			path = "/formulas"
		case strings.Contains(code, "chelle") || strings.Contains(code, "active"):
			path = "/gere"
		case strings.Contains(code, "parts"):
			path = "/spare-parts"
		case strings.Contains(code, "uninvoiced"):
			path = "/out-invoice"
		}
		item["path"] = path
	}
	return items
}

func (a *app) operationalDataQuality() []record {
	checks := []struct{ code, title, query string }{
		{"salon-yarn-missing", "گردش نخ سالن بدون نوع نخ، مالک یا شناسه چله", `SELECT COUNT(*) FROM nakh_salon WHERE COALESCE(nakh_name_nakh_salon,'')='' OR COALESCE(mosh_name_nakh_salon,'')='' OR COALESCE(chelle_id_nakh_salon,0)=0`},
		{"out-owner-ambiguous", "خروج نخ قدیمی با مالک/مقصد مبهم", `SELECT COUNT(*) FROM nakh_khor WHERE COALESCE(owner_mosh_nakh_khor,'')='' OR COALESCE(destination_type_nakh_khor,'') NOT IN ('warper','other') OR (COALESCE(destination_type_nakh_khor,'warper')='warper' AND COALESCE(owner_mosh_nakh_khor,'')=COALESCE(moshname_nakh_khor,''))`},
		{"duplicate-chelle", "شماره چله تکراری", `SELECT COUNT(*) FROM (SELECT shom_chelle FROM chelle WHERE COALESCE(shom_chelle,'')<>'' GROUP BY shom_chelle HAVING COUNT(*)>1) q`},
		{"invalid-formula", "فرمول تار و پود نامعتبر", `SELECT
			(SELECT COUNT(*) FROM machine_formul WHERE ABS(COALESCE(tar_percent,0)+COALESCE(pod_percent,0)-100)>0.001)+
			(SELECT COUNT(*) FROM chelle_formul WHERE ABS(COALESCE(tar_percent,0)+COALESCE(pod_percent,0)-100)>0.001)+
			(SELECT COUNT(*) FROM salon WHERE ABS(COALESCE(tar_percent_salon,0)+COALESCE(pod_percent_salon,0)-100)>0.001)`},
		{"multi-active", "بیش از یک چله فعال روی ماشین", `SELECT COUNT(*) FROM (SELECT machin_chelle FROM chelle WHERE COALESCE(machin_chelle,'')<>'' GROUP BY machin_chelle HAVING COUNT(*)>1) q`},
		{"invalid-production-link", "تولید با چله یا ماشین نامعتبر", `SELECT COUNT(*) FROM salon s LEFT JOIN chelle c ON c.id_chelle=s.chelle_id_salon WHERE COALESCE(s.chelle_id_salon,0)=0 OR c.id_chelle IS NULL OR NOT EXISTS(SELECT 1 FROM gere g WHERE g.chelle_id_gere=s.chelle_id_salon AND g.machin_gere=s.machin_salon)`},
		{"invalid-waste-link", "ضایعات بدون اتصال معتبر ماشین و چله", `SELECT COUNT(*) FROM production_waste w LEFT JOIN chelle c ON c.id_chelle=w.chelle_id_waste WHERE COALESCE(w.chelle_id_waste,0)=0 OR c.id_chelle IS NULL`},
	}
	out := []record{}
	for _, check := range checks {
		var count int64
		if err := a.queryRow(check.query).Scan(&count); err == nil {
			out = append(out, record{"code": check.code, "title": check.title, "count": count, "status": map[bool]string{true: "ok", false: "attention"}[count == 0]})
		}
	}
	negativeYarn := int64(0)
	for _, item := range a.yarnInventory() {
		if value, ok := item["inventory"].(float64); ok && value < -0.001 {
			negativeYarn++
		}
	}
	out = append(out, record{"code": "negative-yarn", "title": "موجودی منفی نخ", "count": negativeYarn, "status": map[bool]string{true: "ok", false: "attention"}[negativeYarn == 0]})
	overReturn := int64(0)
	for _, item := range a.warperYarnBalances() {
		if value, ok := item["balance_weight"].(float64); ok && value < -0.001 {
			overReturn++
		}
	}
	out = append(out, record{"code": "warper-over-return", "title": "برگشت چله بیش از نخ ارسالی", "count": overReturn, "status": map[bool]string{true: "ok", false: "attention"}[overReturn == 0]})
	shortage := int64(0)
	if machines, err := a.activeMachineStatus(); err == nil {
		for _, item := range machines {
			if value, ok := item["material_shortage"].(float64); ok && value > 0.001 {
				shortage++
			}
		}
	}
	out = append(out, record{"code": "machine-shortage", "title": "کسری مواد روی ماشین", "count": shortage, "status": map[bool]string{true: "ok", false: "attention"}[shortage == 0]})
	return out
}

func (a *app) productionWasteRows() []record {
	rows, err := a.query(`SELECT id_waste,COALESCE(waste_date,''),machine,shom_chelle,waste_type,weight,COALESCE(reason,''),COALESCE(operator_name,''),COALESCE(description,''),COALESCE(chelle_id_waste,0),COALESCE(corrective_action,'') FROM production_waste ORDER BY id_waste DESC LIMIT 500`)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	out := []record{}
	for rows.Next() {
		var id, chelleID int64
		var date, machine, shom, typ, reason, operator, description, corrective string
		var weight float64
		if rows.Scan(&id, &date, &machine, &shom, &typ, &weight, &reason, &operator, &description, &chelleID, &corrective) == nil {
			out = append(out, record{"id": id, "tarikh": date, "machine": machine, "shom_chelle": shom, "chelle_id": chelleID, "waste_type": typ, "weight": weight, "reason": reason, "operator_name": operator, "description": description, "corrective_action": corrective})
		}
	}
	return out
}

func (a *app) managementReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	today := jalaliToday()
	month := today
	if len(today) >= 7 {
		month = today[:7]
	}
	machines, _ := a.activeMachineStatus()
	writeJSON(w, record{
		"date":             today,
		"today":            a.productionSummary("tarikh_salon = ?", today),
		"month":            a.productionSummary("SUBSTR(tarikh_salon,1,7) = ?", month),
		"month_by_machine": a.monthProduction(month),
		"yarn_inventory":   a.yarnInventory(),
		"warper_balances":  a.warperYarnBalances(),
		"machines":         machines,
		"waste":            a.productionWasteRows(),
		"stock":            a.stockSummary(),
		"out_invoices":     a.latestOutInvoices(200),
		"notifications":    a.notifications(),
		"data_quality":     a.operationalDataQuality(),
	})
}

func (a *app) tagheInfo(w http.ResponseWriter, code string) {
	var id int64
	var metr, weight float64
	var machine, kala, hamPod, hamChelle, shom string
	err := a.queryRow(`SELECT id_salon, metr_salon, w_salon, machin_salon, kala_salon, ham_pod_salon, ham_chelle_salon, shom_chelle_salon FROM salon WHERE id_salon=?`, code).Scan(&id, &metr, &weight, &machine, &kala, &hamPod, &hamChelle, &shom)
	if err != nil {
		fail(w, 404, "کد طاقه یافت نشد")
		return
	}
	var existing string
	if err := a.queryRow(`SELECT shom_f_khor FROM f_khor WHERE taghe_cod_f_khor=? LIMIT 1`, code).Scan(&existing); err == nil && existing != "" {
		fail(w, 400, "این طاقه قبلا در فاکتور "+existing+" ثبت شده است")
		return
	}
	writeJSON(w, record{"success": true, "id": id, "metr": metr, "weight": weight, "machine": machine, "kala": kala, "ham_pod": hamPod, "ham_chelle": hamChelle, "shom_chelle": shom})
}

func (a *app) writeOutInvoices(w http.ResponseWriter, limit int) {
	codeList := "GROUP_CONCAT(f.taghe_cod_f_khor)"
	if a.dialect == "postgres" {
		codeList = "STRING_AGG(f.taghe_cod_f_khor, ',' ORDER BY f.id_f_khor)"
	}
	rows, err := a.query(`SELECT f.shom_f_khor, MIN(f.tarikh_f_khor), MIN(f.mosh_f_khor), MIN(f.shomare_sanad), MIN(COALESCE(f.kala_name_f_khor,'')), COUNT(*), COALESCE(SUM(s.metr_salon),0), COALESCE(SUM(s.w_salon),0), `+codeList+`
		FROM f_khor f LEFT JOIN salon s ON s.id_salon=CAST(f.taghe_cod_f_khor AS INTEGER)
		GROUP BY f.shom_f_khor ORDER BY MAX(f.id_f_khor) DESC LIMIT ?`, limit)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var no, tarikh, mosh, sanad, kala, codes string
		var cnt int64
		var metr, weight float64
		_ = rows.Scan(&no, &tarikh, &mosh, &sanad, &kala, &cnt, &metr, &weight, &codes)
		items = append(items, record{"id": no, "invoice_no": no, "tarikh": tarikh, "mosh": mosh, "sanad": sanad, "kala": kala, "taghe_count": cnt, "metr": metr, "weight": weight, "codes": codes})
	}
	writeJSON(w, items)
}

func (a *app) outInvoiceDetails(no string) record {
	rows, err := a.query(`SELECT f.taghe_cod_f_khor, COALESCE(s.metr_salon,0), COALESCE(s.w_salon,0), COALESCE(s.machin_salon,''), COALESCE(s.kala_salon, f.kala_name_f_khor, ''), COALESCE(s.ham_pod_salon,''), COALESCE(s.ham_chelle_salon,''), COALESCE(s.shom_chelle_salon,'')
		FROM f_khor f
		LEFT JOIN salon s ON s.id_salon=CAST(f.taghe_cod_f_khor AS INTEGER)
		WHERE f.shom_f_khor=?
		ORDER BY f.id_f_khor`, no)
	if err != nil {
		return record{"success": false}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var code, machine, kala, hamPod, hamChelle, shom string
		var metr, weight float64
		_ = rows.Scan(&code, &metr, &weight, &machine, &kala, &hamPod, &hamChelle, &shom)
		items = append(items, record{"id": code, "metr": metr, "weight": weight, "machine": machine, "kala": kala, "ham_pod": hamPod, "ham_chelle": hamChelle, "shom_chelle": shom})
	}
	return record{"success": true, "invoice_no": no, "items": items}
}

func (a *app) stockTaghes(w http.ResponseWriter) {
	rows, err := a.query(`SELECT s.id_salon, s.tarikh_salon, s.metr_salon, s.w_salon, s.machin_salon, s.kala_salon, s.ham_pod_salon, s.ham_chelle_salon, s.shom_chelle_salon
		FROM salon s
		WHERE NOT EXISTS (SELECT 1 FROM f_khor f WHERE f.taghe_cod_f_khor=CAST(s.id_salon AS TEXT))
		ORDER BY s.id_salon DESC`)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var id int64
		var tarikh, machine, kala, hamPod, hamChelle, shom string
		var metr, weight float64
		_ = rows.Scan(&id, &tarikh, &metr, &weight, &machine, &kala, &hamPod, &hamChelle, &shom)
		items = append(items, record{"id": id, "tarikh": tarikh, "metr": metr, "weight": weight, "machine": machine, "kala": kala, "ham_pod": hamPod, "ham_chelle": hamChelle, "shom_chelle": shom})
	}
	writeJSON(w, items)
}

func (a *app) nextSanadNumber() string {
	var max sql.NullInt64
	_ = a.queryRow(`SELECT MAX(CAST(shomare_sanad AS INTEGER)) FROM f_khor WHERE COALESCE(shomare_sanad,'')<>''`).Scan(&max)
	n := int64(1)
	if max.Valid {
		n = max.Int64 + 1
	}
	return fmt.Sprintf("%06d", n)
}

func escapeSQL(s string) string { return strings.ReplaceAll(s, `'`, `''`) }
func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (a *app) nameByID(table, idCol, nameCol string, id int64) (string, error) {
	var name string
	err := a.queryRow(fmt.Sprintf("SELECT %s FROM %s WHERE %s=?", nameCol, table, idCol), id).Scan(&name)
	return name, err
}

func basicMap(kind string) (string, string, string, bool) {
	switch kind {
	case "customers":
		return "mosh_name", "id_mosh_name", "name_mosh", true
	case "yarns":
		return "nakh_name", "id_nakh_name", "name_nakh_name", true
	case "fabrics":
		return "kala_name", "id_kala_name", "name_kala_name", true
	case "warpers":
		return "chellepich", "id_chellepich", "name_chellepich", true
	case "beams":
		return "kod_navard", "id_kod_navard", "kod_kod_navard", true
	case "tiers":
		return "gerezan", "id_gerezan", "name_gerezan", true
	case "operators":
		return "operator_name", "id_operator", "name_operator", true
	case "drivers":
		return "driver_name", "id_driver", "name_driver", true
	case "costs":
		return "hazine", "id_hazine", "onvan_hazine", true
	case "weavers":
		return "weaver_name", "id_weaver", "name_weaver", true
	case "serviceTypes":
		return "service_type", "id_service_type", "name_service_type", true
	case "spareParts":
		return "spare_part", "id_spare_part", "name_spare_part", true
	default:
		return "", "", "", false
	}
}

func machineDigits(value string) string {
	return strings.NewReplacer(
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
		"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
		"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
		"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
		"٫", ".", "٬", "",
	).Replace(strings.TrimSpace(value))
}

// normalizeStoredMachine repairs integer machine numbers imported from legacy
// SQLite as floating point text (for example 7.0). Non-numeric legacy labels
// are preserved so a migration never discards historical data.
func normalizeStoredMachine(value string) string {
	clean := machineDigits(value)
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 || f != math.Trunc(f) {
		return strings.TrimSpace(value)
	}
	return strconv.FormatInt(int64(f), 10)
}

func canonicalMachineNumber(value string) (string, error) {
	clean := machineDigits(value)
	if clean == "" {
		return "", errors.New("شماره یا شناسه ماشین الزامی است")
	}
	f, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		// Some established installations use identifiers such as M-1. Keep
		// those labels while still canonicalising numeric legacy values.
		return clean, nil
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 || f != math.Trunc(f) {
		return "", errors.New("شماره ماشین باید یک عدد صحیح مثبت مانند 7 باشد")
	}
	return strconv.FormatInt(int64(f), 10), nil
}

func (a *app) normalizeMachineNumbers() error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type formulaRow struct {
		id                     int64
		machine, tozih         string
		tarPercent, podPercent float64
	}
	formulaRows, err := tx.Query(rebind(a.dialect, `SELECT id_formul,COALESCE(machine,''),COALESCE(tar_percent,50),COALESCE(pod_percent,50),COALESCE(tozih_formul,'') FROM machine_formul ORDER BY id_formul`))
	if err != nil {
		return err
	}
	formulaGroups := map[string][]formulaRow{}
	for formulaRows.Next() {
		var row formulaRow
		if err := formulaRows.Scan(&row.id, &row.machine, &row.tarPercent, &row.podPercent, &row.tozih); err != nil {
			formulaRows.Close()
			return err
		}
		canonical := normalizeStoredMachine(row.machine)
		formulaGroups[canonical] = append(formulaGroups[canonical], row)
	}
	if err := formulaRows.Close(); err != nil {
		return err
	}
	for canonical, rows := range formulaGroups {
		keeper := rows[len(rows)-1]
		for _, row := range rows[:len(rows)-1] {
			if _, err := txExec(a.dialect, tx, `INSERT INTO machine_formul_archive(source_id,machine,canonical_machine,tar_percent,pod_percent,tozih_formul,reason) VALUES(?,?,?,?,?,?,?)`, row.id, row.machine, canonical, row.tarPercent, row.podPercent, row.tozih, "duplicate legacy machine number"); err != nil {
				return err
			}
			if _, err := txExec(a.dialect, tx, `INSERT INTO machine_number_normalization_audit(table_name,row_id,column_name,old_value,new_value) VALUES(?,?,?,?,?)`, "machine_formul", row.id, "machine", row.machine, canonical); err != nil {
				return err
			}
			if _, err := txExec(a.dialect, tx, `DELETE FROM machine_formul WHERE id_formul=?`, row.id); err != nil {
				return err
			}
		}
		if keeper.machine != canonical {
			if _, err := txExec(a.dialect, tx, `INSERT INTO machine_number_normalization_audit(table_name,row_id,column_name,old_value,new_value) VALUES(?,?,?,?,?)`, "machine_formul", keeper.id, "machine", keeper.machine, canonical); err != nil {
				return err
			}
			if _, err := txExec(a.dialect, tx, `UPDATE machine_formul SET machine=? WHERE id_formul=?`, canonical, keeper.id); err != nil {
				return err
			}
		}
	}

	type machineColumn struct{ table, id, column string }
	columns := []machineColumn{
		{"chelle", "id_chelle", "machin_chelle"},
		{"gere", "id_gere", "machin_gere"},
		{"nakh_salon", "id_nakh_salon", "shom_machin_nakh_salon"},
		{"salon", "id_salon", "machin_salon"},
		{"machine_consumption", "id_consumption", "machine"},
		{"production_waste", "id_waste", "machine"},
		{"chelle_formul", "id_formul", "machine"},
	}
	for _, col := range columns {
		rows, err := tx.Query(`SELECT ` + quoteIdent(col.id) + `,COALESCE(` + quoteIdent(col.column) + `,'') FROM ` + quoteIdent(col.table))
		if err != nil {
			return err
		}
		type change struct {
			id                 int64
			oldValue, newValue string
		}
		changes := []change{}
		for rows.Next() {
			var item change
			if err := rows.Scan(&item.id, &item.oldValue); err != nil {
				rows.Close()
				return err
			}
			item.newValue = normalizeStoredMachine(item.oldValue)
			if item.oldValue != item.newValue {
				changes = append(changes, item)
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, item := range changes {
			if _, err := txExec(a.dialect, tx, `INSERT INTO machine_number_normalization_audit(table_name,row_id,column_name,old_value,new_value) VALUES(?,?,?,?,?)`, col.table, item.id, col.column, item.oldValue, item.newValue); err != nil {
				return err
			}
			query := `UPDATE ` + quoteIdent(col.table) + ` SET ` + quoteIdent(col.column) + `=? WHERE ` + quoteIdent(col.id) + `=?`
			if _, err := txExec(a.dialect, tx, query, item.newValue, item.id); err != nil {
				return err
			}
		}
	}
	if _, err := txExec(a.dialect, tx, `DELETE FROM machine_consumption WHERE id_consumption NOT IN (SELECT MAX(id_consumption) FROM machine_consumption GROUP BY machine,shom_chelle)`); err != nil {
		return err
	}
	return tx.Commit()
}

func machineWhere(col, machine string) (string, []any) {
	variants := []string{strings.TrimSpace(machine)}
	if f, err := strconv.ParseFloat(strings.TrimSpace(machine), 64); err == nil {
		variants = append(variants, strconv.FormatFloat(f, 'f', 1, 64))
		if f == float64(int64(f)) {
			variants = append(variants, strconv.FormatInt(int64(f), 10))
		}
	}
	seen := map[string]bool{}
	args := []any{}
	holders := []string{}
	for _, v := range variants {
		if v != "" && !seen[v] {
			seen[v] = true
			args = append(args, v)
			holders = append(holders, "?")
		}
	}
	if len(holders) == 0 {
		return "1=0", args
	}
	return "TRIM(" + col + ") IN (" + strings.Join(holders, ",") + ")", args
}

func jalaliToday() string {
	jy, jm, jd := gregorianToJalali(time.Now().In(time.Local).Date())
	return fmt.Sprintf("%04d/%02d/%02d", jy, jm, jd)
}

func gregorianToJalali(gy int, gm time.Month, gd int) (int, int, int) {
	gdm := []int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2++
	}
	days := 355666 + 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 + gd + gdm[int(gm)-1]
	jy := -1595 + 33*(days/12053)
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	jm, jd := 0, 0
	if days < 186 {
		jm = 1 + days/31
		jd = 1 + days%31
	} else {
		jm = 7 + (days-186)/30
		jd = 1 + (days-186)%30
	}
	return jy, jm, jd
}

func writeRows(w http.ResponseWriter, rows *sql.Rows, err error, cols []string) {
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []record{}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		_ = rows.Scan(ptrs...)
		item := record{}
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				item[c] = string(v)
			default:
				item[c] = v
			}
		}
		list = append(list, item)
	}
	writeJSON(w, list)
}

func writeSave(w http.ResponseWriter, err error) {
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, record{"success": true})
}

func execErr(_ sql.Result, err error) error { return err }
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, 400, "داده ارسالی معتبر نیست")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	writeJSON(w, apiError{Success: false, Error: msg})
}

func withCORS(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, origin := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if origin != "" && !allowed[origin] {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func staticHandler() http.HandlerFunc {
	dist := filepath.Join("web", "dist")
	return func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dist, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.ServeFile(w, r, path)
			return
		}
		index := filepath.Join(dist, "index.html")
		if _, err := os.Stat(index); err == nil {
			http.ServeFile(w, r, index)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html dir="rtl"><body style="font-family:tahoma;background:#07101f;color:white;padding:30px"><h1>Operational Cycle Go</h1><p>فرانت هنوز build نشده است. دستور داخل web: npm install سپس npm run build یا npm run dev</p></body></html>`))
	}
}

func (a *app) count(table string) int64 {
	var n int64
	_ = a.queryRow("SELECT COUNT(*) FROM " + quoteIdent(table)).Scan(&n)
	return n
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteColumns(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = quoteIdent(c)
	}
	return strings.Join(out, ",")
}

func zipAdd(zw *zip.Writer, name, data string) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(data))
	return err
}

func xlsxWriteStaticFiles(zw *zip.Writer) error {
	files := map[string]string{
		"_rels/.rels":   `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/styles.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Tahoma"/></font><font><b/><sz val="11"/><name val="Tahoma"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0"/></cellXfs></styleSheet>`,
	}
	for name, data := range files {
		if err := zipAdd(zw, name, data); err != nil {
			return err
		}
	}
	return nil
}

func buildWorkbookXML(sheets []string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` + strings.Join(sheets, "") + `</sheets></workbook>`
}

func buildWorkbookRelsXML(rels []string) string {
	rels = append(rels, `<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + strings.Join(rels, "") + `</Relationships>`
}

func buildContentTypesXML(overrides []string) string {
	base := `<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + base + strings.Join(overrides, "") + `</Types>`
}

func buildSheetXML(cols []string, data [][]string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView rightToLeft="1" workbookViewId="0"/></sheetViews><sheetData>`)
	b.WriteString(`<row r="1">`)
	for i, c := range cols {
		b.WriteString(cellXML(1, i+1, c, true))
	}
	b.WriteString(`</row>`)
	for r, row := range data {
		rowNo := r + 2
		b.WriteString(fmt.Sprintf(`<row r="%d">`, rowNo))
		for c := range cols {
			v := ""
			if c < len(row) {
				v = row[c]
			}
			b.WriteString(cellXML(rowNo, c+1, v, false))
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

func cellXML(row, col int, value string, header bool) string {
	style := ""
	if header {
		style = ` s="1"`
	}
	ref := colName(col) + strconv.Itoa(row)
	return fmt.Sprintf(`<c r="%s" t="inlineStr"%s><is><t>%s</t></is></c>`, ref, style, xmlEsc(value))
}

func colName(n int) string {
	out := ""
	for n > 0 {
		n--
		out = string(rune('A'+(n%26))) + out
		n /= 26
	}
	return out
}

func sheetName(name string) string {
	repl := strings.NewReplacer("[", "_", "]", "_", "*", "_", "?", "_", "/", "_", "\\", "_", ":", "_")
	name = repl.Replace(name)
	if len([]rune(name)) > 31 {
		r := []rune(name)
		name = string(r[:31])
	}
	if strings.TrimSpace(name) == "" {
		return "Sheet"
	}
	return name
}

func xmlEsc(s string) string {
	return html.EscapeString(s)
}

func parseXLSX(content []byte) (map[string][][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, err
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	workbook, err := readZipText(files["xl/workbook.xml"])
	if err != nil {
		return nil, err
	}
	relsText, err := readZipText(files["xl/_rels/workbook.xml.rels"])
	if err != nil {
		return nil, err
	}
	rels := parseWorkbookRels(relsText)
	sheets := parseWorkbookSheets(workbook)
	sharedStrings := []string{}
	if sharedText, err := readZipText(files["xl/sharedStrings.xml"]); err == nil {
		sharedStrings = parseSharedStrings(sharedText)
	}
	out := map[string][][]string{}
	for _, sh := range sheets {
		target := rels[sh.relID]
		if target == "" {
			continue
		}
		path := "xl/" + strings.TrimPrefix(target, "/")
		if !strings.HasPrefix(target, "worksheets/") && strings.HasPrefix(target, "xl/") {
			path = target
		}
		if strings.HasPrefix(target, "worksheets/") {
			path = "xl/" + target
		}
		txt, err := readZipText(files[path])
		if err != nil {
			continue
		}
		out[sh.name] = parseSheetRows(txt, sharedStrings)
	}
	return out, nil
}

type workbookSheet struct {
	name  string
	relID string
}

func readZipText(f *zip.File) (string, error) {
	if f == nil {
		return "", os.ErrNotExist
	}
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	return string(b), err
}

func parseWorkbookRels(text string) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`<Relationship[^>]*Id="([^"]+)"[^>]*Target="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out[m[1]] = html.UnescapeString(m[2])
	}
	return out
}

func parseWorkbookSheets(text string) []workbookSheet {
	out := []workbookSheet{}
	re := regexp.MustCompile(`<sheet[^>]*name="([^"]+)"[^>]*r:id="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out = append(out, workbookSheet{name: html.UnescapeString(m[1]), relID: m[2]})
	}
	return out
}

func parseSharedStrings(text string) []string {
	out := []string{}
	siRe := regexp.MustCompile(`(?s)<si[^>]*>(.*?)</si>`)
	tRe := regexp.MustCompile(`(?s)<t[^>]*>(.*?)</t>`)
	for _, sm := range siRe.FindAllStringSubmatch(text, -1) {
		parts := []string{}
		for _, tm := range tRe.FindAllStringSubmatch(sm[1], -1) {
			parts = append(parts, html.UnescapeString(stripXML(tm[1])))
		}
		out = append(out, strings.Join(parts, ""))
	}
	return out
}

func parseSheetRows(text string, sharedStrings []string) [][]string {
	rows := [][]string{}
	rowRe := regexp.MustCompile(`(?s)<row[^>]*>(.*?)</row>`)
	cellRe := regexp.MustCompile(`(?s)<c([^>]*)>(.*?)</c>`)
	refRe := regexp.MustCompile(`r="([A-Z]+)\d+"`)
	typeRe := regexp.MustCompile(`t="([^"]+)"`)
	textRe := regexp.MustCompile(`(?s)<t[^>]*>(.*?)</t>`)
	vRe := regexp.MustCompile(`(?s)<v>(.*?)</v>`)
	for _, rm := range rowRe.FindAllStringSubmatch(text, -1) {
		row := []string{}
		for _, cm := range cellRe.FindAllStringSubmatch(rm[1], -1) {
			idx := len(row)
			attrs := cm[1]
			body := cm[2]
			if ref := refRe.FindStringSubmatch(attrs); len(ref) > 1 {
				idx = colIndex(ref[1]) - 1
			}
			for len(row) <= idx {
				row = append(row, "")
			}
			val := ""
			cellType := ""
			if tm := typeRe.FindStringSubmatch(attrs); len(tm) > 1 {
				cellType = tm[1]
			}
			if cellType == "s" {
				if vm := vRe.FindStringSubmatch(body); len(vm) > 1 {
					if i, err := strconv.Atoi(strings.TrimSpace(vm[1])); err == nil && i >= 0 && i < len(sharedStrings) {
						val = sharedStrings[i]
					}
				}
			} else if tm := textRe.FindStringSubmatch(body); len(tm) > 1 {
				val = html.UnescapeString(stripXML(tm[1]))
			} else if vm := vRe.FindStringSubmatch(body); len(vm) > 1 {
				val = html.UnescapeString(stripXML(vm[1]))
			}
			row[idx] = val
		}
		rows = append(rows, row)
	}
	return rows
}

func colIndex(s string) int {
	n := 0
	for _, r := range s {
		n = n*26 + int(r-'A'+1)
	}
	return n
}

func stripXML(s string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
}

func scalarFloat(db *sql.DB, q string) float64 {
	var n float64
	_ = db.QueryRow(q).Scan(&n)
	return n
}

func distinct(db *sql.DB, q string) []string {
	rows, err := db.Query(q)
	if err != nil {
		return []string{}
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func pathLast(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return parts[len(parts)-1]
}

func dbPath() string {
	p := env("OPERATIONAL_DB", filepath.Join("..", "operational", "database.db"))
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := pbkdf2SHA256([]byte(password), salt, 100000, 32)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(key), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, ":")
	if len(parts) != 2 {
		return false
	}
	salt, err1 := hex.DecodeString(parts[0])
	key, err2 := hex.DecodeString(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	test := pbkdf2SHA256([]byte(password), salt, 100000, len(key))
	return hmac.Equal(test, key)
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hLen := 32
	numBlocks := (keyLen + hLen - 1) / hLen
	var out []byte
	for block := 1; block <= numBlocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
