package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type app struct {
	db                  *sql.DB
	conn                *sql.Conn
	dialect             string
	dbLabel             string
	companyID           int64
	sessionSecret       string
	portalSessionSecret string
}

type sessionInfo struct {
	UserID    int64    `json:"user_id"`
	CompanyID int64    `json:"company_id"`
	Username  string   `json:"username"`
	Role      string   `json:"role"`
	Portal    bool     `json:"portal,omitempty"`
	MenuKeys  []string `json:"menu_keys,omitempty"`
	CanManage bool     `json:"can_manage,omitempty"`
	ExpiresAt int64    `json:"exp"`
}

type appHandler func(*app, http.ResponseWriter, *http.Request)

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
	if a.conn != nil {
		return a.conn.ExecContext(context.Background(), rebind(a.dialect, q), args...)
	}
	return a.db.Exec(rebind(a.dialect, q), args...)
}

func (a *app) query(q string, args ...any) (*sql.Rows, error) {
	if a.conn != nil {
		return a.conn.QueryContext(context.Background(), rebind(a.dialect, q), args...)
	}
	return a.db.Query(rebind(a.dialect, q), args...)
}

func (a *app) queryRow(q string, args ...any) *sql.Row {
	if a.conn != nil {
		return a.conn.QueryRowContext(context.Background(), rebind(a.dialect, q), args...)
	}
	return a.db.QueryRow(rebind(a.dialect, q), args...)
}

func (a *app) begin() (*sql.Tx, error) {
	if a.conn != nil {
		return a.conn.BeginTx(context.Background(), nil)
	}
	return a.db.Begin()
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
	validateOperationalProductionConfig()
	db, dialect, label, err := openOperationalDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if dialect == "sqlite" {
		db.SetMaxOpenConns(1)
	}

	a := &app{
		db:                  db,
		dialect:             dialect,
		dbLabel:             label,
		sessionSecret:       env("OPERATIONAL_SESSION_SECRET", env("PORTAL_OPERATIONAL_SECRET", "textile-operational-local-session-secret")),
		portalSessionSecret: env("PORTAL_OPERATIONAL_SECRET", "textile-operational-local-portal-secret"),
	}
	if err := a.migrate(); err != nil {
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
	log.Fatal(http.ListenAndServe(":"+port, withCORS(mux)))
}

func validateOperationalProductionConfig() {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return
	}
	driver := strings.ToLower(strings.TrimSpace(env("OPERATIONAL_DB_DRIVER", "postgres")))
	if driver != "postgres" && driver != "postgresql" && driver != "pg" && strings.TrimSpace(os.Getenv("OPERATIONAL_DATABASE_URL")) == "" {
		log.Fatal("PostgreSQL is required for production multi-tenant isolation")
	}
	for _, key := range []string{"DB_PASSWORD", "OPERATIONAL_ADMIN_PASSWORD", "OPERATIONAL_SESSION_SECRET", "PORTAL_OPERATIONAL_SECRET"} {
		value := strings.TrimSpace(os.Getenv(key))
		if len(value) < 12 || value == "change_me" || value == "admin123" {
			log.Fatalf("%s must be configured securely for production", key)
		}
	}
	if len(strings.TrimSpace(os.Getenv("OPERATIONAL_SESSION_SECRET"))) < 32 || len(strings.TrimSpace(os.Getenv("PORTAL_OPERATIONAL_SECRET"))) < 32 {
		log.Fatal("operational session secrets must contain at least 32 characters in production")
	}
}

func (a *app) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", a.health)
	mux.HandleFunc("/api/login", a.login)
	mux.HandleFunc("/api/portal-session", a.portalSession)
	mux.HandleFunc("/api/logout", a.logout)
	mux.HandleFunc("/api/session", a.requireAuth((*app).sessionStatus))
	mux.HandleFunc("/api/dashboard", a.requireMenu("dashboard", (*app).dashboard))
	mux.HandleFunc("/api/lookups", a.requireAuth((*app).lookups))
	mux.HandleFunc("/api/basic/", a.requireMenu("initial", (*app).basic))
	mux.HandleFunc("/api/nakh-vor", a.requireMenu("nakh-vor", (*app).nakhVor))
	mux.HandleFunc("/api/nakh-vor/", a.requireMenu("nakh-vor", (*app).nakhVorByID))
	mux.HandleFunc("/api/chelle", a.requireMenu("chelle", (*app).chelle))
	mux.HandleFunc("/api/chelle/", a.requireMenu("chelle", (*app).chelleByID))
	mux.HandleFunc("/api/gere", a.requireMenu("gere", (*app).gere))
	mux.HandleFunc("/api/gere/", a.requireMenu("gere", (*app).gereByID))
	mux.HandleFunc("/api/nakh-salon", a.requireMenu("nakh-salon", (*app).nakhSalon))
	mux.HandleFunc("/api/nakh-salon/", a.requireMenu("nakh-salon", (*app).nakhSalonByID))
	mux.HandleFunc("/api/nakh-khor", a.requireMenuWithReadFallback("yarn-out", "reports", (*app).nakhKhor))
	mux.HandleFunc("/api/nakh-khor/", a.requireMenuWithReadFallback("yarn-out", "reports", (*app).nakhKhorByID))
	mux.HandleFunc("/api/warper-yarn-balance", a.requireMenu("yarn-out", (*app).warperYarnBalance))
	mux.HandleFunc("/api/empty-beam-out", a.requireMenu("empty-beam-out", (*app).emptyBeamOut))
	mux.HandleFunc("/api/empty-beam-out/", a.requireMenu("empty-beam-out", (*app).emptyBeamOutByID))
	mux.HandleFunc("/api/salon", a.requireMenu("salon", (*app).salon))
	mux.HandleFunc("/api/salon/", a.requireMenu("salon", (*app).salonByPath))
	mux.HandleFunc("/api/out-invoice", a.requireMenuWithReadFallback("out-invoice", "reports", (*app).outInvoice))
	mux.HandleFunc("/api/out-invoice/mobile-sessions", a.requireMenu("out-invoice", (*app).createMobileLoadingSession))
	mux.HandleFunc("/api/local/printers", a.requireMenu("out-invoice", (*app).localPrinters))
	mux.HandleFunc("/api/out-invoice/", a.requireMenuWithReadFallback("out-invoice", "reports", (*app).outInvoiceByPath))
	mux.HandleFunc("/api/mobile-loading/", a.mobileLoadingPublic)
	mux.HandleFunc("/api/expenses", a.requireMenuWithReadFallback("expenses", "reports", (*app).expenses))
	mux.HandleFunc("/api/expenses/", a.requireMenuWithReadFallback("expenses", "reports", (*app).expenseByID))
	mux.HandleFunc("/api/formulas", a.requireMenu("formulas", (*app).formulas))
	mux.HandleFunc("/api/formulas/", a.requireMenu("formulas", (*app).formulaByID))
	mux.HandleFunc("/api/database/", a.requireAdmin((*app).databaseTools))
	mux.HandleFunc("/api/spare-parts", a.requireMenu("spare-parts", (*app).spareParts))
	mux.HandleFunc("/api/spare-parts/", a.requireMenu("spare-parts", (*app).sparePartByID))
	mux.HandleFunc("/api/machinery-services", a.requireMenu("machinery-services", (*app).machineryServices))
	mux.HandleFunc("/api/machinery-services/", a.requireMenu("machinery-services", (*app).machineryServiceByID))
	mux.HandleFunc("/api/menus", a.requireAdmin((*app).menus))
	mux.HandleFunc("/api/users", a.requireAdmin((*app).users))
	mux.HandleFunc("/api/users/", a.requireAdmin((*app).userByID))
	mux.HandleFunc("/api/next-salon-id", a.requireMenu("salon", (*app).nextSalonID))
	mux.HandleFunc("/api/consumption/machines", a.requireMenu("consumption", (*app).consumptionMachines))
	mux.HandleFunc("/api/reset-cycle", a.requireAdmin((*app).resetCycle))
	mux.HandleFunc("/", staticHandler())
}

func (a *app) requireAuth(next appHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := a.currentSession(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "نشست شما معتبر نیست. دوباره وارد شوید.")
			return
		}
		tenant, closeTenant, err := a.forCompany(r.Context(), session.CompanyID)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "اتصال امن به داده‌های شرکت برقرار نشد.")
			return
		}
		defer closeTenant()
		next(tenant, w, r)
	}
}

func (a *app) requireMenu(menuKey string, next appHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := a.currentSession(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "نشست شما معتبر نیست. دوباره وارد شوید.")
			return
		}
		tenant, closeTenant, err := a.forCompany(r.Context(), session.CompanyID)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "اتصال امن به داده‌های شرکت برقرار نشد.")
			return
		}
		defer closeTenant()
		allowed, err := tenant.userHasMenuAccess(session, menuKey)
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			fail(w, http.StatusForbidden, "شما به این بخش دسترسی ندارید.")
			return
		}
		next(tenant, w, r)
	}
}

func (a *app) requireMenuWithReadFallback(menuKey, readFallback string, next appHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := a.currentSession(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "نشست شما معتبر نیست. دوباره وارد شوید.")
			return
		}
		tenant, closeTenant, err := a.forCompany(r.Context(), session.CompanyID)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "اتصال امن به داده‌های شرکت برقرار نشد.")
			return
		}
		defer closeTenant()
		allowed, err := tenant.userHasMenuAccess(session, menuKey)
		if err == nil && !allowed && (r.Method == http.MethodGet || r.Method == http.MethodHead) && readFallback != "" {
			allowed, err = tenant.userHasMenuAccess(session, readFallback)
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !allowed {
			fail(w, http.StatusForbidden, "شما به این بخش دسترسی ندارید.")
			return
		}
		next(tenant, w, r)
	}
}

func (a *app) requireAdmin(next appHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := a.currentSession(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "نشست شما معتبر نیست. دوباره وارد شوید.")
			return
		}
		role := strings.ToLower(strings.TrimSpace(session.Role))
		if role != "admin" && role != "owner" {
			fail(w, http.StatusForbidden, "این عملیات فقط برای مدیر اصلی مجاز است.")
			return
		}
		tenant, closeTenant, err := a.forCompany(r.Context(), session.CompanyID)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "اتصال امن به داده‌های شرکت برقرار نشد.")
			return
		}
		defer closeTenant()
		next(tenant, w, r)
	}
}

func (a *app) createSession(w http.ResponseWriter, r *http.Request, session sessionInfo) error {
	if session.CompanyID <= 0 {
		session.CompanyID = 1
	}
	if session.ExpiresAt <= time.Now().Unix() {
		session.ExpiresAt = time.Now().Add(8 * time.Hour).Unix()
	}
	token, err := a.signSession(session)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "operational_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
		Expires:  time.Unix(session.ExpiresAt, 0),
		MaxAge:   int(time.Until(time.Unix(session.ExpiresAt, 0)).Seconds()),
	})
	return nil
}

func (a *app) clearSession(w http.ResponseWriter, r *http.Request) {
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
	session, err := a.verifySession(cookie.Value)
	if err != nil || session.CompanyID <= 0 || session.UserID <= 0 || session.ExpiresAt <= time.Now().Unix() {
		return sessionInfo{}, false
	}
	return session, true
}

func (a *app) userHasMenuAccess(session sessionInfo, menuKey string) (bool, error) {
	role := strings.ToLower(strings.TrimSpace(session.Role))
	if role == "admin" || role == "owner" || (role == "manager" && !session.Portal) {
		return true, nil
	}
	if session.Portal {
		for _, key := range session.MenuKeys {
			if key == menuKey || key == "*" {
				return true, nil
			}
		}
		var restricted int64
		err := a.queryRow(`SELECT COALESCE(is_restricted,0) FROM menu_items WHERE menu_key=?`, menuKey).Scan(&restricted)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return restricted == 0, err
	}
	var hasAccess int64
	err := a.queryRow(`SELECT COALESCE(uma.has_access, CASE WHEN COALESCE(m.is_restricted,0)=1 THEN 0 ELSE 1 END)
		FROM menu_items m
		LEFT JOIN user_menu_access uma ON uma.menu_key=m.menu_key AND uma.user_id=?
		WHERE m.menu_key=?`, session.UserID, menuKey).Scan(&hasAccess)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return hasAccess == 1, nil
}

func (a *app) forCompany(ctx context.Context, companyID int64) (*app, func(), error) {
	if companyID <= 0 {
		return nil, func() {}, errors.New("invalid company id")
	}
	clone := *a
	clone.companyID = companyID
	if a.dialect != "postgres" {
		return &clone, func() {}, nil
	}
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return nil, func() {}, err
	}
	if _, err := conn.ExecContext(ctx, `SELECT set_config('app.company_id', $1, false)`, strconv.FormatInt(companyID, 10)); err != nil {
		_ = conn.Close()
		return nil, func() {}, err
	}
	clone.conn = conn
	closeFn := func() {
		_, _ = conn.ExecContext(context.Background(), `RESET app.company_id`)
		_ = conn.Close()
	}
	return &clone, closeFn, nil
}

func (a *app) signSession(session sessionInfo) (string, error) {
	payload, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(a.sessionSecret))
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *app) verifySession(token string) (sessionInfo, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return sessionInfo{}, errors.New("invalid session")
	}
	mac := hmac.New(sha256.New, []byte(a.sessionSecret))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return sessionInfo{}, errors.New("invalid session signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sessionInfo{}, err
	}
	var session sessionInfo
	if err := json.Unmarshal(payload, &session); err != nil {
		return sessionInfo{}, err
	}
	return session, nil
}

type portalSessionClaims struct {
	UserID           int64    `json:"user_id"`
	CompanyID        int64    `json:"company_id"`
	Username         string   `json:"username"`
	Role             string   `json:"role"`
	MenuKeys         []string `json:"menu_keys,omitempty"`
	CanManage        bool     `json:"can_manage_team,omitempty"`
	AllowOperational bool     `json:"allow_operational"`
	ExpiresAt        int64    `json:"exp"`
}

func (a *app) verifyPortalSession(token string) (portalSessionClaims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 {
		return portalSessionClaims{}, errors.New("invalid portal token")
	}
	mac := hmac.New(sha256.New, []byte(a.portalSessionSecret))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return portalSessionClaims{}, errors.New("invalid portal token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return portalSessionClaims{}, err
	}
	var claims portalSessionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return portalSessionClaims{}, err
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		return portalSessionClaims{}, errors.New("portal token expired")
	}
	return claims, nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
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
	if a.dialect == "postgres" {
		if _, err := a.exec(`CREATE OR REPLACE FUNCTION operational_current_company_id() RETURNS BIGINT AS $$
			SELECT COALESCE(NULLIF(current_setting('app.company_id', true), '')::BIGINT, 1)
		$$ LANGUAGE SQL STABLE`); err != nil {
			return err
		}
	}
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
		`CREATE TABLE IF NOT EXISTS users (id_user INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT DEFAULT 'viewer', is_active INTEGER DEFAULT 1, created_at TEXT DEFAULT (datetime('now','localtime')))`,
		`CREATE TABLE IF NOT EXISTS menu_items (id_menu INTEGER PRIMARY KEY AUTOINCREMENT, menu_key TEXT UNIQUE, menu_name TEXT, path TEXT, icon TEXT, is_restricted INTEGER DEFAULT 0, sort_order INTEGER DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS user_menu_access (id_access INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER, menu_key TEXT, has_access INTEGER DEFAULT 1, granted_by INTEGER, granted_at TEXT DEFAULT (datetime('now','localtime')), UNIQUE(user_id, menu_key))`,
		`CREATE TABLE IF NOT EXISTS mobile_loading_sessions (id_mobile_session INTEGER PRIMARY KEY AUTOINCREMENT, token_hash TEXT NOT NULL UNIQUE, company_id INTEGER NOT NULL, created_by INTEGER, invoice_no TEXT, customer TEXT, kala TEXT, created_at TEXT DEFAULT (datetime('now','localtime')), expires_at TEXT NOT NULL, closed_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS mobile_loading_items (id_mobile_item INTEGER PRIMARY KEY AUTOINCREMENT, session_id INTEGER NOT NULL, taghe_code TEXT NOT NULL, created_at TEXT DEFAULT (datetime('now','localtime')), UNIQUE(session_id, taghe_code))`,
	}
	for _, stmt := range stmts {
		if _, err := a.exec(ddl(a.dialect, stmt)); err != nil {
			return err
		}
	}
	for _, col := range []struct{ table, name, typ string }{
		{"gere", "tarikh_gere", "TEXT"},
		{"empty_beam_out", "returned_at", "TEXT"},
		{"empty_beam_out", "returned_chelle_no", "TEXT"},
		{"f_khor", "kala_name_f_khor", "TEXT"},
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
	} {
		if err := a.ensureColumn(col.table, col.name, col.typ); err != nil {
			return err
		}
	}
	companyType := "INTEGER NOT NULL DEFAULT 1"
	if a.dialect == "postgres" {
		companyType = "BIGINT NOT NULL DEFAULT operational_current_company_id()"
	}
	for _, table := range operationalTenantTables() {
		if err := a.ensureColumn(table, "company_id", companyType); err != nil {
			return fmt.Errorf("add company_id to %s: %w", table, err)
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
	if a.dialect == "postgres" {
		if err := a.configurePostgresTenantIsolation(); err != nil {
			return err
		}
	} else {
		for _, statement := range []string{
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_machine_formul_company_machine ON machine_formul (company_id, machine)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_operational_users_company_username ON users (company_id, username)`,
		} {
			if _, err := a.exec(statement); err != nil {
				return err
			}
		}
	}
	return nil
}

func operationalTenantTables() []string {
	return []string{
		"mosh_name", "nakh_name", "kala_name", "chellepich", "kod_navard", "gerezan",
		"nakh_vor", "nakh_khor", "empty_beam_out", "chelle", "gere", "nakh_salon", "salon",
		"machine_consumption", "machine_formul", "f_khor", "hazine", "operator_name", "driver_name",
		"weaver_name", "h_rozmare", "service_type", "spare_part", "spare_parts_inventory",
		"machinery_service", "users", "user_menu_access",
	}
}

func (a *app) configurePostgresTenantIsolation() error {
	for _, constraint := range []struct{ table, name string }{
		{"mosh_name", "mosh_name_name_mosh_key"},
		{"nakh_name", "nakh_name_name_nakh_name_key"},
		{"kala_name", "kala_name_name_kala_name_key"},
		{"chellepich", "chellepich_name_chellepich_key"},
		{"kod_navard", "kod_navard_kod_kod_navard_key"},
		{"gerezan", "gerezan_name_gerezan_key"},
		{"machine_formul", "machine_formul_machine_key"},
		{"spare_parts_inventory", "spare_parts_inventory_spare_part_id_key"},
		{"users", "users_username_key"},
	} {
		if _, err := a.exec(`ALTER TABLE ` + quoteIdent(constraint.table) + ` DROP CONSTRAINT IF EXISTS ` + quoteIdent(constraint.name)); err != nil {
			return err
		}
	}
	uniqueIndexes := []struct {
		name, table, columns string
	}{
		{"uq_mosh_name_company_name", "mosh_name", "company_id, name_mosh"},
		{"uq_nakh_name_company_name", "nakh_name", "company_id, name_nakh_name"},
		{"uq_kala_name_company_name", "kala_name", "company_id, name_kala_name"},
		{"uq_chellepich_company_name", "chellepich", "company_id, name_chellepich"},
		{"uq_kod_navard_company_code", "kod_navard", "company_id, kod_kod_navard"},
		{"uq_gerezan_company_name", "gerezan", "company_id, name_gerezan"},
		{"uq_machine_formul_company_machine", "machine_formul", "company_id, machine"},
		{"uq_spare_inventory_company_part", "spare_parts_inventory", "company_id, spare_part_id"},
		{"uq_operational_users_company_username", "users", "company_id, username"},
	}
	for _, index := range uniqueIndexes {
		if _, err := a.exec(`CREATE UNIQUE INDEX IF NOT EXISTS ` + quoteIdent(index.name) + ` ON ` + quoteIdent(index.table) + ` (` + index.columns + `)`); err != nil {
			return err
		}
	}
	for _, table := range operationalTenantTables() {
		policy := "tenant_isolation_" + table
		statements := []string{
			`ALTER TABLE ` + quoteIdent(table) + ` ALTER COLUMN company_id SET DEFAULT operational_current_company_id()`,
			`ALTER TABLE ` + quoteIdent(table) + ` ENABLE ROW LEVEL SECURITY`,
			`ALTER TABLE ` + quoteIdent(table) + ` FORCE ROW LEVEL SECURITY`,
			`DROP POLICY IF EXISTS ` + quoteIdent(policy) + ` ON ` + quoteIdent(table),
			`CREATE POLICY ` + quoteIdent(policy) + ` ON ` + quoteIdent(table) + ` USING (company_id = operational_current_company_id()) WITH CHECK (company_id = operational_current_company_id())`,
			`CREATE INDEX IF NOT EXISTS ` + quoteIdent("idx_"+table+"_company_id") + ` ON ` + quoteIdent(table) + ` (company_id)`,
		}
		for _, statement := range statements {
			if _, err := a.exec(statement); err != nil {
				return fmt.Errorf("tenant isolation %s: %w", table, err)
			}
		}
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
		if err := a.queryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name=? AND column_name=?`, table, column).Scan(&n); err != nil {
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
		{"formulas", "فرمول تولید ماشین‌ها", "/formulas", "📐", 0, 7},
		{"salon", "سالن تولید", "/salon", "🏭", 0, 8},
		{"consumption", "مصرف تار و پود", "/consumption", "📊", 0, 9},
		{"yarn-out", "خروج نخ", "/yarn-out", "🚪", 0, 10},
		{"empty-beam-out", "خروج نورد خالی", "/empty-beam-out", "📍", 0, 11},
		{"out-invoice", "فاکتور خروج", "/out-invoice", "🧾", 0, 11},
		{"expenses", "هزینه‌ها", "/expenses", "💰", 1, 12},
		{"reports", "گزارشات", "/reports", "📊", 0, 13},
		{"database", "مدیریت دیتابیس", "/database", "🗄️", 1, 14},
		{"machinery-services", "خدمات ماشین‌آلات", "/machinery-services", "🔧", 1, 15},
		{"spare-parts", "موجودی انبار قطعات", "/spare-parts", "⚙️", 1, 16},
		{"users", "مدیریت کاربران", "/users", "👥", 1, 17},
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

func (a *app) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, record{"ok": true, "service": "operational-cycle-go", "date": jalaliToday()})
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		CompanyID int64  `json:"company_id"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.CompanyID <= 0 {
		p.CompanyID = int64Env("OPERATIONAL_DEFAULT_COMPANY_ID", 1)
	}
	tenant, closeTenant, err := a.forCompany(r.Context(), p.CompanyID)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "اتصال به اطلاعات شرکت برقرار نشد")
		return
	}
	defer closeTenant()
	var id, active int64
	var username, hash, role string
	err = tenant.queryRow(`SELECT id_user, username, password_hash, role, COALESCE(is_active,1) FROM users WHERE username=?`, strings.TrimSpace(p.Username)).Scan(&id, &username, &hash, &role, &active)
	if err != nil || active != 1 || !verifyPassword(p.Password, hash) {
		fail(w, http.StatusUnauthorized, "نام کاربری یا رمز عبور معتبر نیست")
		return
	}
	session := sessionInfo{UserID: id, CompanyID: p.CompanyID, Username: username, Role: role, ExpiresAt: time.Now().Add(8 * time.Hour).Unix()}
	if err := a.createSession(w, r, session); err != nil {
		fail(w, http.StatusInternalServerError, "ایجاد نشست امکان‌پذیر نیست")
		return
	}
	menus := tenant.userMenus(session)
	writeJSON(w, record{"success": true, "user": record{"id": id, "company_id": p.CompanyID, "username": username, "role": role}, "menus": menus})
}

func (a *app) portalSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var p struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &p) {
		return
	}
	claims, err := a.verifyPortalSession(p.Token)
	if err != nil || !claims.AllowOperational || claims.CompanyID <= 0 || claims.UserID <= 0 {
		fail(w, http.StatusUnauthorized, "مجوز پورتال برای بخش عملیاتی معتبر نیست")
		return
	}
	role := strings.ToLower(strings.TrimSpace(claims.Role))
	if role == "" {
		role = "customer"
	}
	session := sessionInfo{
		UserID: claims.UserID, CompanyID: claims.CompanyID, Username: claims.Username,
		Role: role, Portal: true, MenuKeys: claims.MenuKeys, CanManage: claims.CanManage,
		ExpiresAt: minInt64(claims.ExpiresAt, time.Now().Add(8*time.Hour).Unix()),
	}
	if err := a.createSession(w, r, session); err != nil {
		fail(w, http.StatusInternalServerError, "ایجاد نشست عملیاتی امکان‌پذیر نیست")
		return
	}
	tenant, closeTenant, err := a.forCompany(r.Context(), claims.CompanyID)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "اتصال به اطلاعات شرکت برقرار نشد")
		return
	}
	defer closeTenant()
	writeJSON(w, record{
		"success": true,
		"user":    record{"id": session.UserID, "company_id": session.CompanyID, "username": session.Username, "role": session.Role},
		"menus":   tenant.userMenus(session),
	})
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
	menus := a.userMenus(session)
	writeJSON(w, record{
		"success": true,
		"user":    record{"id": session.UserID, "company_id": session.CompanyID, "username": session.Username, "role": session.Role},
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
		"nakh_salon_net":    a.scalarFloat("SELECT COALESCE(SUM(w_nakh_salon),0) FROM nakh_salon"),
		"salon_count":       a.count("salon"),
		"salon_metr":        a.scalarFloat("SELECT COALESCE(SUM(metr_salon),0) FROM salon"),
		"salon_weight":      a.scalarFloat("SELECT COALESCE(SUM(w_salon),0) FROM salon"),
		"out_invoice_count": a.count("f_khor"),
		"expense_total":     a.scalarFloat("SELECT COALESCE(SUM(mablagh_h_rozmare),0) FROM h_rozmare"),
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
		"hambaftYarn":  a.distinct("SELECT DISTINCT hambaft_nakh_vor FROM nakh_vor WHERE COALESCE(hambaft_nakh_vor,'')<>'' ORDER BY hambaft_nakh_vor"),
		"hamPod":       a.distinct("SELECT DISTINCT ham_nakh_salon FROM nakh_salon WHERE COALESCE(ham_nakh_salon,'')<>'' ORDER BY ham_nakh_salon"),
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
		tx, err := a.begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if p.ID > 0 {
			_, err = txExec(a.dialect, tx, `UPDATE chelle SET shom_chelle=?, nakh_chelle=?, w_chelle=?, pich_chelle=?, mosh_chelle=?, hambaft_chelle=?, codnavard_chelle=? WHERE id_chelle=?`, p.ShomChelle, nakh, p.Weight, pich, mosh, p.Hambaft, kod, p.ID)
		} else {
			_, err = txExec(a.dialect, tx, `INSERT INTO chelle (tarikh_chelle,shom_chelle,nakh_chelle,w_chelle,pich_chelle,mosh_chelle,hambaft_chelle,codnavard_chelle,machin_chelle) VALUES (?,?,?,?,?,?,?,?,?)`, jalaliToday(), p.ShomChelle, nakh, p.Weight, pich, mosh, p.Hambaft, kod, "")
		}
		if err == nil && strings.TrimSpace(kod) != "" {
			_, err = txExec(a.dialect, tx, `UPDATE empty_beam_out SET returned_at=datetime('now','localtime'), returned_chelle_no=?
				WHERE id_empty_beam_out=(SELECT id_empty_beam_out FROM empty_beam_out WHERE kod_navard=? AND chellepich_name=? AND COALESCE(returned_at,'')='' ORDER BY id_empty_beam_out DESC LIMIT 1)`, p.ShomChelle, kod, pich)
		}
		if err != nil {
			_ = tx.Rollback()
			writeSave(w, err)
			return
		}
		writeSave(w, tx.Commit())
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
	tx, err := a.begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	var shom, beam string
	if err := txQueryRow(a.dialect, tx, `SELECT COALESCE(shom_chelle,''), COALESCE(codnavard_chelle,'') FROM chelle WHERE id_chelle=?`, id).Scan(&shom, &beam); err != nil {
		_ = tx.Rollback()
		fail(w, 404, "چله پیدا نشد")
		return
	}
	if _, err := txExec(a.dialect, tx, `DELETE FROM chelle WHERE id_chelle=?`, id); err != nil {
		_ = tx.Rollback()
		writeSave(w, err)
		return
	}
	if beam != "" {
		_, _ = txExec(a.dialect, tx, `UPDATE empty_beam_out SET returned_at=NULL, returned_chelle_no=NULL WHERE kod_navard=? AND returned_chelle_no=?`, beam, shom)
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
		rows, err := a.query(`SELECT g.id_gere, g.tarikh_gere, g.name_gere, g.shom_chelle_gere, g.machin_gere, COALESCE(gr.id_gerezan,0), COALESCE(c.id_chelle,0) FROM gere g LEFT JOIN gerezan gr ON gr.name_gerezan=g.name_gere LEFT JOIN chelle c ON c.id_chelle=(SELECT MAX(c2.id_chelle) FROM chelle c2 WHERE c2.shom_chelle=g.shom_chelle_gere) ORDER BY COALESCE(g.tarikh_gere,'') DESC, g.id_gere DESC LIMIT 200`)
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
		gerezan, err := a.nameByID("gerezan", "id_gerezan", "name_gerezan", p.GerezanID)
		if err != nil || p.ChelleID == 0 || strings.TrimSpace(p.Machine) == "" {
			fail(w, 400, "اطلاعات گره کامل نیست")
			return
		}
		var shom string
		var old string
		if p.ID > 0 {
			_ = a.queryRow(`SELECT shom_chelle_gere FROM gere WHERE id_gere=?`, p.ID).Scan(&old)
		}
		if err := a.queryRow(`SELECT shom_chelle FROM chelle WHERE id_chelle=?`, p.ChelleID).Scan(&shom); err != nil {
			fail(w, 400, "چله معتبر نیست")
			return
		}
		tx, err := a.begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if p.ID > 0 {
			if old != "" && old != shom {
				_, _ = txExec(a.dialect, tx, `UPDATE chelle SET machin_chelle='' WHERE shom_chelle=?`, old)
			}
			_, err = txExec(a.dialect, tx, `UPDATE gere SET name_gere=?, shom_chelle_gere=?, machin_gere=?, tarikh_gere=? WHERE id_gere=?`, gerezan, shom, p.Machine, jalaliToday(), p.ID)
		} else {
			_, err = txExec(a.dialect, tx, `INSERT INTO gere (name_gere, shom_chelle_gere, machin_gere, tarikh_gere) VALUES (?,?,?,?)`, gerezan, shom, p.Machine, jalaliToday())
		}
		if err == nil {
			_, err = txExec(a.dialect, tx, `UPDATE chelle SET machin_chelle=? WHERE id_chelle=?`, p.Machine, p.ChelleID)
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
	var shom string
	_ = a.queryRow(`SELECT shom_chelle_gere FROM gere WHERE id_gere=?`, id).Scan(&shom)
	tx, err := a.begin()
	if err == nil {
		_, err = txExec(a.dialect, tx, `DELETE FROM gere WHERE id_gere=?`, id)
	}
	if err == nil && shom != "" {
		_, err = txExec(a.dialect, tx, `UPDATE chelle SET machin_chelle='' WHERE shom_chelle=?`, shom)
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
			rows, err := a.query(`SELECT id_chelle, shom_chelle, machin_chelle, w_chelle, hambaft_chelle FROM chelle WHERE COALESCE(machin_chelle,'')<>'' ORDER BY id_chelle DESC`)
			writeRows(w, rows, err, []string{"id", "shom_chelle", "machine", "weight", "hambaft"})
			return
		}
		rows, err := a.query(`SELECT ns.id_nakh_salon, ns.tarikh_nakh_salon, ns.shom_machin_nakh_salon, ns.ham_nakh_salon, ns.w_nakh_salon, ns.shom_chelle_nakh_salon, ns.mosh_name_nakh_salon, ns.vor_khor_nakh_salon, COALESCE(c.id_chelle,0) FROM nakh_salon ns LEFT JOIN chelle c ON c.id_chelle=(SELECT MAX(c2.id_chelle) FROM chelle c2 WHERE c2.shom_chelle=ns.shom_chelle_nakh_salon) ORDER BY ns.id_nakh_salon DESC LIMIT 200`)
		writeRows(w, rows, err, []string{"id", "tarikh", "machine", "ham_nakh", "weight", "shom_chelle", "mosh_name", "vor_khor", "chelle_id"})
	case http.MethodPost:
		var p struct {
			ID       int64   `json:"id"`
			Machine  string  `json:"machine"`
			HamNakh  string  `json:"ham_nakh"`
			Weight   float64 `json:"weight"`
			ChelleID int64   `json:"chelle_id"`
			MoshName string  `json:"mosh_name"`
			VorKhor  string  `json:"vor_khor"`
		}
		if !decode(w, r, &p) {
			return
		}
		if p.Machine == "" || p.HamNakh == "" || p.Weight <= 0 || p.ChelleID == 0 || p.MoshName == "" || p.VorKhor == "" {
			fail(w, 400, "اطلاعات نخ سالن کامل نیست")
			return
		}
		var shom string
		if err := a.queryRow(`SELECT shom_chelle FROM chelle WHERE id_chelle=?`, p.ChelleID).Scan(&shom); err != nil {
			fail(w, 400, "چله معتبر نیست")
			return
		}
		finalWeight := p.Weight
		if p.VorKhor == "khoroj" {
			finalWeight = -p.Weight
		}
		var err error
		if p.ID > 0 {
			_, err = a.exec(`UPDATE nakh_salon SET shom_machin_nakh_salon=?, ham_nakh_salon=?, w_nakh_salon=?, shom_chelle_nakh_salon=?, mosh_name_nakh_salon=?, vor_khor_nakh_salon=? WHERE id_nakh_salon=?`, p.Machine, p.HamNakh, finalWeight, shom, p.MoshName, p.VorKhor, p.ID)
		} else {
			_, err = a.exec(`INSERT INTO nakh_salon (tarikh_nakh_salon, shom_machin_nakh_salon, ham_nakh_salon, w_nakh_salon, shom_chelle_nakh_salon, mosh_name_nakh_salon, vor_khor_nakh_salon) VALUES (?,?,?,?,?,?,?)`, jalaliToday(), p.Machine, p.HamNakh, finalWeight, shom, p.MoshName, p.VorKhor)
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
		rows, err := a.query(`SELECT id_nakh_khor, tarikh_nakh_khor, hambaft_nakh_khor, ABS(COALESCE(w_vor_nakh_khor,0)), moshname_nakh_khor, nakh_name_nakh_khor FROM nakh_khor ORDER BY id_nakh_khor DESC LIMIT 300`)
		writeRows(w, rows, err, []string{"id", "tarikh", "hambaft", "weight", "mosh", "nakh"})
	case http.MethodPost:
		var p struct {
			ID       int64   `json:"id"`
			Hambaft  string  `json:"hambaft"`
			Weight   float64 `json:"weight"`
			MoshName string  `json:"mosh_name"`
			NakhName string  `json:"nakh_name"`
		}
		if !decode(w, r, &p) {
			return
		}
		if p.Hambaft == "" || p.Weight <= 0 || p.MoshName == "" || p.NakhName == "" {
			fail(w, 400, "اطلاعات خروج نخ کامل نیست")
			return
		}
		var err error
		if p.ID > 0 {
			_, err = a.exec(`UPDATE nakh_khor SET hambaft_nakh_khor=?, w_vor_nakh_khor=?, moshname_nakh_khor=?, nakh_name_nakh_khor=? WHERE id_nakh_khor=?`, p.Hambaft, -p.Weight, p.MoshName, p.NakhName, p.ID)
		} else {
			_, err = a.exec(`INSERT INTO nakh_khor (tarikh_nakh_khor,hambaft_nakh_khor,w_vor_nakh_khor,moshname_nakh_khor,nakh_name_nakh_khor) VALUES (?,?,?,?,?)`, jalaliToday(), p.Hambaft, -p.Weight, p.MoshName, p.NakhName)
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
			       COALESCE(e.returned_at,'') AS return_date,
			       COALESCE(e.returned_chelle_no,'') AS return_chelle
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
		var unresolved int64
		if p.ID > 0 {
			err = a.queryRow(`SELECT COUNT(*) FROM empty_beam_out WHERE kod_navard=? AND id_empty_beam_out<>? AND COALESCE(returned_at,'')=''`, beam, p.ID).Scan(&unresolved)
		} else {
			err = a.queryRow(`SELECT COUNT(*) FROM empty_beam_out WHERE kod_navard=? AND COALESCE(returned_at,'')=''`, beam).Scan(&unresolved)
		}
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if unresolved > 0 {
			fail(w, http.StatusConflict, "این نورد هنوز نزد چله‌پیچ است و خروج دوباره آن مجاز نیست")
			return
		}
		if p.ID > 0 {
			_, err = a.exec(`UPDATE empty_beam_out SET kod_navard=?, chellepich_name=?, description=?, returned_at=NULL, returned_chelle_no=NULL WHERE id_empty_beam_out=?`, beam, warper, strings.TrimSpace(p.Description), p.ID)
		} else {
			_, err = a.exec(`INSERT INTO empty_beam_out (tarikh_empty_beam_out,kod_navard,chellepich_name,description) VALUES (?,?,?,?)`, jalaliToday(), beam, warper, strings.TrimSpace(p.Description))
		}
		writeSave(w, err)
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

func (a *app) salon(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := a.query(`SELECT s.id_salon, s.tarikh_salon, s.metr_salon, s.w_salon, s.machin_salon, s.user_salon, s.kala_salon, s.ham_pod_salon, s.ham_chelle_salon, s.shom_chelle_salon, COALESCE(k.id_kala_name,0) FROM salon s LEFT JOIN kala_name k ON k.name_kala_name=s.kala_salon ORDER BY s.id_salon DESC LIMIT 200`)
		writeRows(w, rows, err, []string{"id", "tarikh", "metr", "weight", "machine", "user", "kala", "ham_pod", "ham_chelle", "shom_chelle", "kala_id"})
	case http.MethodPost:
		var p struct {
			ID         int64   `json:"id"`
			Metr       float64 `json:"metr"`
			Weight     float64 `json:"weight"`
			Machine    string  `json:"machine"`
			KalaID     int64   `json:"kala_id"`
			HamPod     string  `json:"ham_pod"`
			HamChelle  string  `json:"ham_chelle"`
			ShomChelle string  `json:"shom_chelle"`
			User       string  `json:"user"`
		}
		if !decode(w, r, &p) {
			return
		}
		kala, err := a.nameByID("kala_name", "id_kala_name", "name_kala_name", p.KalaID)
		if err != nil || p.Metr <= 0 || p.Weight <= 0 || p.Machine == "" || p.HamPod == "" || p.HamChelle == "" || p.ShomChelle == "" {
			fail(w, 400, "اطلاعات تولید کامل نیست")
			return
		}
		if p.User == "" {
			p.User = "admin"
		}
		if p.ID > 0 {
			_, err = a.exec(`UPDATE salon SET metr_salon=?, w_salon=?, machin_salon=?, user_salon=?, kala_salon=?, ham_pod_salon=?, ham_chelle_salon=?, shom_chelle_salon=? WHERE id_salon=?`, p.Metr, p.Weight, p.Machine, p.User, kala, p.HamPod, p.HamChelle, p.ShomChelle, p.ID)
			writeSave(w, err)
			return
		}
		next := int64(1)
		_ = a.queryRow(`SELECT COALESCE(MAX(id_salon),0)+1 FROM salon`).Scan(&next)
		_, err = a.exec(`INSERT INTO salon (id_salon, metr_salon, w_salon, machin_salon, user_salon, tarikh_salon, kala_salon, ham_pod_salon, ham_chelle_salon, shom_chelle_salon) VALUES (?,?,?,?,?,?,?,?,?,?)`, next, p.Metr, p.Weight, p.Machine, p.User, jalaliToday(), kala, p.HamPod, p.HamChelle, p.ShomChelle)
		if err == nil {
			_ = a.updateConsumption(p.Machine, p.ShomChelle, p.Weight)
		}
		writeJSON(w, record{"success": err == nil, "id": next, "error": errString(err)})
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
			InvoiceNo string   `json:"invoice_no"`
			SanadNo   string   `json:"sanad_no"`
			Customer  string   `json:"customer"`
			Kala      string   `json:"kala"`
			Items     []string `json:"items"`
			OldNo     string   `json:"old_invoice_no"`
		}
		if !decode(w, r, &p) {
			return
		}
		p.InvoiceNo = strings.TrimSpace(p.InvoiceNo)
		p.SanadNo = strings.TrimSpace(p.SanadNo)
		p.Customer = strings.TrimSpace(p.Customer)
		p.Kala = strings.TrimSpace(p.Kala)
		p.OldNo = strings.TrimSpace(p.OldNo)
		uniqueItems := make([]string, 0, len(p.Items))
		seenItems := make(map[string]bool, len(p.Items))
		for _, rawCode := range p.Items {
			code := strings.TrimSpace(rawCode)
			if code == "" || seenItems[code] {
				continue
			}
			seenItems[code] = true
			uniqueItems = append(uniqueItems, code)
		}
		p.Items = uniqueItems
		if p.InvoiceNo == "" || p.Customer == "" || p.Kala == "" || len(p.Items) == 0 {
			fail(w, 400, "اطلاعات فاکتور خروج کامل نیست")
			return
		}
		tx, err := a.begin()
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		defer tx.Rollback()
		var duplicateInvoice int
		if p.OldNo == "" {
			err = txQueryRow(a.dialect, tx, `SELECT COUNT(*) FROM f_khor WHERE shom_f_khor=?`, p.InvoiceNo).Scan(&duplicateInvoice)
		} else if p.InvoiceNo != p.OldNo {
			err = txQueryRow(a.dialect, tx, `SELECT COUNT(*) FROM f_khor WHERE shom_f_khor=? AND shom_f_khor<>?`, p.InvoiceNo, p.OldNo).Scan(&duplicateInvoice)
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		if duplicateInvoice > 0 {
			fail(w, http.StatusConflict, "شماره فاکتور "+p.InvoiceNo+" قبلاً ثبت شده است")
			return
		}
		for _, code := range p.Items {
			var exists int
			if err = txQueryRow(a.dialect, tx, `SELECT COUNT(*) FROM salon WHERE CAST(id_salon AS TEXT)=?`, code).Scan(&exists); err != nil {
				fail(w, http.StatusInternalServerError, err.Error())
				return
			}
			if exists == 0 {
				fail(w, http.StatusBadRequest, "طاقه با کد "+code+" در سالن تولید یافت نشد")
				return
			}
			var existingInvoice string
			if p.OldNo == "" {
				err = txQueryRow(a.dialect, tx, `SELECT COALESCE(shom_f_khor,'') FROM f_khor WHERE taghe_cod_f_khor=? LIMIT 1`, code).Scan(&existingInvoice)
			} else {
				err = txQueryRow(a.dialect, tx, `SELECT COALESCE(shom_f_khor,'') FROM f_khor WHERE taghe_cod_f_khor=? AND shom_f_khor<>? LIMIT 1`, code, p.OldNo).Scan(&existingInvoice)
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				fail(w, http.StatusInternalServerError, err.Error())
				return
			}
			if existingInvoice != "" {
				fail(w, http.StatusConflict, "طاقه "+code+" قبلاً در فاکتور "+existingInvoice+" ثبت شده است")
				return
			}
			err = nil
		}
		if p.OldNo != "" {
			_, err = txExec(a.dialect, tx, `DELETE FROM f_khor WHERE shom_f_khor=?`, p.OldNo)
		}
		invoiceDate := jalaliToday()
		for _, code := range p.Items {
			if err == nil {
				_, err = txExec(a.dialect, tx, `INSERT INTO f_khor (tarikh_f_khor, shom_f_khor, taghe_cod_f_khor, mosh_f_khor, shomare_sanad, kala_name_f_khor) VALUES (?,?,?,?,?,?)`, invoiceDate, p.InvoiceNo, code, p.Customer, p.SanadNo, p.Kala)
			}
		}
		if err != nil {
			_ = tx.Rollback()
			fail(w, 500, err.Error())
			return
		}
		if err = tx.Commit(); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, record{"success": true, "invoice_no": p.InvoiceNo, "sanad_no": p.SanadNo, "tarikh": invoiceDate, "item_count": len(p.Items)})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type mobileLoadingSession struct {
	ID        int64
	CompanyID int64
	InvoiceNo string
	Customer  string
	Kala      string
	ExpiresAt string
	ClosedAt  sql.NullString
}

type localPrinterInfo struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

func (a *app) localPrinters(w http.ResponseWriter, r *http.Request) {
	if strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))) != "local" || runtime.GOOS != "windows" {
		fail(w, http.StatusNotFound, "انتخاب چاپگر فقط در نسخه لوکال ویندوز فعال است")
		return
	}
	printers, err := installedWindowsPrinters()
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "دریافت چاپگرهای ویندوز انجام نشد: "+err.Error())
		return
	}
	defaultName := ""
	for _, printer := range printers {
		if printer.IsDefault {
			defaultName = printer.Name
			break
		}
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, record{"success": true, "printers": printers, "default_printer": defaultName})
	case http.MethodPost:
		var payload struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &payload) {
			return
		}
		name := strings.TrimSpace(payload.Name)
		if name == "" {
			fail(w, http.StatusBadRequest, "چاپگر انتخاب نشده است")
			return
		}
		found := false
		for _, printer := range printers {
			if printer.Name == name {
				found = true
				break
			}
		}
		if !found {
			fail(w, http.StatusBadRequest, "چاپگر انتخاب‌شده در ویندوز یافت نشد")
			return
		}
		if err := setDefaultWindowsPrinter(name); err != nil {
			fail(w, http.StatusInternalServerError, "انتخاب چاپگر در ویندوز انجام نشد: "+err.Error())
			return
		}
		writeJSON(w, record{"success": true, "printer": name, "previous_printer": defaultName})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func installedWindowsPrinters() ([]localPrinterInfo, error) {
	const script = `$utf8 = New-Object System.Text.UTF8Encoding($false); [Console]::OutputEncoding = $utf8; $OutputEncoding = $utf8; $items = @(Get-CimInstance Win32_Printer | Sort-Object Name | ForEach-Object { [pscustomobject]@{ name = [string]$_.Name; is_default = [bool]$_.Default } }); ConvertTo-Json -Compress -InputObject @($items)`
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	output = bytes.TrimPrefix(bytes.TrimSpace(output), []byte{0xef, 0xbb, 0xbf})
	printers := []localPrinterInfo{}
	if len(output) == 0 {
		return printers, nil
	}
	if err := json.Unmarshal(output, &printers); err != nil {
		return nil, err
	}
	return printers, nil
}

func setDefaultWindowsPrinter(name string) error {
	const script = `$name = $env:TEXTILE_PRINTER_NAME; $printer = Get-CimInstance Win32_Printer | Where-Object { $_.Name -eq $name } | Select-Object -First 1; if ($null -eq $printer) { throw 'Printer not found' }; $network = New-Object -ComObject WScript.Network; $network.SetDefaultPrinter($name)`
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "TEXTILE_PRINTER_NAME="+name)
	if output, err := cmd.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return fmt.Errorf("%s", message)
		}
		return err
	}
	return nil
}

func mobileLoadingTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func (a *app) createMobileLoadingSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		InvoiceNo string `json:"invoice_no"`
		Customer  string `json:"customer"`
		Kala      string `json:"kala"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decode(w, r, &payload) {
		return
	}
	session, ok := a.currentSession(r)
	if !ok {
		fail(w, http.StatusUnauthorized, "نشست کاربری معتبر نیست")
		return
	}
	token, err := randomSessionToken()
	if err != nil {
		fail(w, http.StatusInternalServerError, "ساخت کد اتصال موبایل انجام نشد")
		return
	}
	expiresAt := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	_, _ = a.exec(`DELETE FROM mobile_loading_sessions WHERE expires_at < ? OR closed_at IS NOT NULL`, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339Nano))
	_, err = a.exec(`INSERT INTO mobile_loading_sessions (token_hash,company_id,created_by,invoice_no,customer,kala,expires_at) VALUES (?,?,?,?,?,?,?)`,
		mobileLoadingTokenHash(token), normalizedCompanyID(a.companyID), session.UserID,
		strings.TrimSpace(payload.InvoiceNo), strings.TrimSpace(payload.Customer), strings.TrimSpace(payload.Kala), expiresAt)
	if err != nil {
		fail(w, http.StatusInternalServerError, "ذخیره نشست بارگیری موبایل انجام نشد")
		return
	}
	writeJSON(w, record{"success": true, "token": token, "expires_at": expiresAt})
}

func (a *app) loadMobileLoadingSession(token string) (mobileLoadingSession, error) {
	var session mobileLoadingSession
	err := a.queryRow(`SELECT id_mobile_session,company_id,COALESCE(invoice_no,''),COALESCE(customer,''),COALESCE(kala,''),expires_at,closed_at FROM mobile_loading_sessions WHERE token_hash=?`, mobileLoadingTokenHash(token)).Scan(
		&session.ID, &session.CompanyID, &session.InvoiceNo, &session.Customer, &session.Kala, &session.ExpiresAt, &session.ClosedAt)
	if err != nil {
		return mobileLoadingSession{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, session.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) || session.ClosedAt.Valid {
		return mobileLoadingSession{}, errors.New("mobile loading session expired")
	}
	return session, nil
}

func (a *app) mobileLoadingPublic(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/mobile-loading/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || len(strings.TrimSpace(parts[0])) < 32 {
		fail(w, http.StatusNotFound, "کد اتصال موبایل معتبر نیست")
		return
	}
	token := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			a.writeMobileLoadingSession(w, r, token)
		case http.MethodDelete:
			_, _ = a.exec(`UPDATE mobile_loading_sessions SET closed_at=? WHERE token_hash=?`, time.Now().UTC().Format(time.RFC3339Nano), mobileLoadingTokenHash(token))
			writeJSON(w, record{"success": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) == 2 && parts[1] == "items" && r.Method == http.MethodPost {
		a.addMobileLoadingItem(w, r, token)
		return
	}
	if len(parts) == 2 && parts[1] == "preview" && r.Method == http.MethodPost {
		a.previewMobileLoadingItem(w, r, token)
		return
	}
	http.NotFound(w, r)
}

func (a *app) writeMobileLoadingSession(w http.ResponseWriter, r *http.Request, token string) {
	session, err := a.loadMobileLoadingSession(token)
	if err != nil {
		fail(w, http.StatusGone, "نشست بارگیری موبایل منقضی یا بسته شده است")
		return
	}
	tenant, closeTenant, err := a.forCompany(r.Context(), session.CompanyID)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "دسترسی به اطلاعات شرکت برقرار نشد")
		return
	}
	defer closeTenant()
	rows, err := tenant.query(`SELECT m.taghe_code,COALESCE(s.metr_salon,0),COALESCE(s.w_salon,0),COALESCE(s.machin_salon,''),COALESCE(s.kala_salon,''),COALESCE(s.ham_pod_salon,''),COALESCE(s.ham_chelle_salon,''),COALESCE(s.shom_chelle_salon,'') FROM mobile_loading_items m LEFT JOIN salon s ON s.id_salon=CAST(m.taghe_code AS INTEGER) WHERE m.session_id=? ORDER BY m.id_mobile_item`, session.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	items := []record{}
	var totalMetr, totalWeight float64
	for rows.Next() {
		var code, machine, kala, hamPod, hamChelle, shom string
		var metr, weight float64
		if err := rows.Scan(&code, &metr, &weight, &machine, &kala, &hamPod, &hamChelle, &shom); err != nil {
			continue
		}
		totalMetr += metr
		totalWeight += weight
		items = append(items, record{"id": code, "metr": metr, "weight": weight, "machine": machine, "kala": kala, "ham_pod": hamPod, "ham_chelle": hamChelle, "shom_chelle": shom})
	}
	writeJSON(w, record{"success": true, "invoice_no": session.InvoiceNo, "customer": session.Customer, "kala": session.Kala, "expires_at": session.ExpiresAt, "items": items, "count": len(items), "total_metr": totalMetr, "total_weight": totalWeight})
}

func (a *app) addMobileLoadingItem(w http.ResponseWriter, r *http.Request, token string) {
	session, err := a.loadMobileLoadingSession(token)
	if err != nil {
		fail(w, http.StatusGone, "نشست بارگیری موبایل منقضی یا بسته شده است")
		return
	}
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
	tenant, closeTenant, err := a.forCompany(r.Context(), session.CompanyID)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "دسترسی به اطلاعات شرکت برقرار نشد")
		return
	}
	defer closeTenant()
	item, err := tenant.availableTaghe(code)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := a.exec(`INSERT INTO mobile_loading_items (session_id,taghe_code) VALUES (?,?)`, session.ID, code); err != nil {
		fail(w, http.StatusConflict, "این طاقه قبلاً در همین بارگیری ثبت شده است")
		return
	}
	writeJSON(w, record{"success": true, "item": item})
}

func (a *app) previewMobileLoadingItem(w http.ResponseWriter, r *http.Request, token string) {
	session, err := a.loadMobileLoadingSession(token)
	if err != nil {
		fail(w, http.StatusGone, "نشست بارگیری موبایل منقضی یا بسته شده است")
		return
	}
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
	var duplicate int
	if err := a.queryRow(`SELECT 1 FROM mobile_loading_items WHERE session_id=? AND taghe_code=? LIMIT 1`, session.ID, code).Scan(&duplicate); err == nil {
		fail(w, http.StatusConflict, "این طاقه قبلاً در همین بارگیری ثبت شده است")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		fail(w, http.StatusInternalServerError, "بررسی سابقه طاقه انجام نشد")
		return
	}
	tenant, closeTenant, err := a.forCompany(r.Context(), session.CompanyID)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "دسترسی به اطلاعات شرکت برقرار نشد")
		return
	}
	defer closeTenant()
	item, err := tenant.availableTaghe(code)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, record{"success": true, "item": item})
}

func (a *app) availableTaghe(code string) (record, error) {
	var id int64
	var metr, weight float64
	var machine, kala, hamPod, hamChelle, shom string
	err := a.queryRow(`SELECT id_salon,COALESCE(metr_salon,0),COALESCE(w_salon,0),COALESCE(machin_salon,''),COALESCE(kala_salon,''),COALESCE(ham_pod_salon,''),COALESCE(ham_chelle_salon,''),COALESCE(shom_chelle_salon,'') FROM salon WHERE id_salon=?`, code).Scan(&id, &metr, &weight, &machine, &kala, &hamPod, &hamChelle, &shom)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("کد طاقه یافت نشد")
	}
	if err != nil {
		return nil, err
	}
	var existing string
	if err := a.queryRow(`SELECT shom_f_khor FROM f_khor WHERE taghe_cod_f_khor=? LIMIT 1`, code).Scan(&existing); err == nil && existing != "" {
		return nil, errors.New("این طاقه قبلاً در فاکتور " + existing + " ثبت شده است")
	}
	return record{"id": id, "metr": metr, "weight": weight, "machine": machine, "kala": kala, "ham_pod": hamPod, "ham_chelle": hamChelle, "shom_chelle": shom}, nil
}

func (a *app) outInvoiceByPath(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/out-invoice/"), "/")
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
		p.Machine = strings.TrimSpace(p.Machine)
		if p.Machine == "" || p.TarPercent < 0 || p.PodPercent < 0 || p.TarPercent+p.PodPercent <= 0 {
			fail(w, 400, "اطلاعات فرمول ماشین کامل نیست")
			return
		}
		var err error
		if p.ID > 0 {
			_, err = a.exec(`UPDATE machine_formul SET machine=?, tar_percent=?, pod_percent=?, tozih_formul=? WHERE id_formul=?`, p.Machine, p.TarPercent, p.PodPercent, p.Tozih, p.ID)
		} else {
			_, err = a.exec(`INSERT INTO machine_formul (machine, tar_percent, pod_percent, tozih_formul) VALUES (?,?,?,?)
				ON CONFLICT(company_id, machine) DO UPDATE SET tar_percent=excluded.tar_percent, pod_percent=excluded.pod_percent, tozih_formul=excluded.tozih_formul`, p.Machine, p.TarPercent, p.PodPercent, p.Tozih)
		}
		writeSave(w, err)
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
	writeSave(w, execErr(a.exec(`DELETE FROM machine_formul WHERE id_formul=?`, id)))
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
		if err := a.importXLSX(r); err != nil {
			fail(w, 500, err.Error())
			return
		}
		a.writeDatabaseSummary(w)
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
		return filepath.Join(env("OPERATIONAL_BACKUP_DIR", "/app/backups"), fmt.Sprintf("company_%d", normalizedCompanyID(a.companyID)))
	}
	return filepath.Join(filepath.Dir(dbPath()), "backups", fmt.Sprintf("company_%d", normalizedCompanyID(a.companyID)))
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
	tx, err := a.begin()
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
	out := make([]string, 0, len(operationalTenantTables())+1)
	for _, table := range operationalTenantTables() {
		if a.tableExists(table) {
			out = append(out, table)
		}
	}
	return out, nil
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
		colRows, err := a.query(`SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=? ORDER BY ordinal_position`, table)
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

func (a *app) importXLSX(r *http.Request) error {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return err
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return err
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	sheets, err := parseXLSX(content)
	if err != nil {
		return err
	}
	tableList, err := a.tableNames()
	if err != nil {
		return err
	}
	existingTables := map[string]bool{}
	for _, table := range tableList {
		existingTables[table] = true
	}
	tx, err := a.begin()
	if err != nil {
		return err
	}
	for table, rows := range sheets {
		if len(rows) < 1 || !existingTables[table] {
			continue
		}
		cols := rows[0]
		if len(cols) == 0 {
			continue
		}
		if _, err := txExec(a.dialect, tx, "DELETE FROM "+quoteIdent(table)); err != nil {
			_ = tx.Rollback()
			return err
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
				return fmt.Errorf("%s: %w", table, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return a.migrate()
}

func (a *app) tableExists(table string) bool {
	var n int
	if a.dialect == "postgres" {
		_ = a.queryRow(`SELECT COUNT(*) FROM pg_tables WHERE schemaname='public' AND tablename=?`, table).Scan(&n)
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
		tx, err := a.begin()
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
	tx, err := a.begin()
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
		p.Username = strings.TrimSpace(p.Username)
		p.Role = strings.ToLower(strings.TrimSpace(p.Role))
		if p.Username == "" || p.Password == "" {
			fail(w, 400, "نام کاربری و رمز عبور الزامی است")
			return
		}
		if len([]rune(p.Password)) < 10 {
			fail(w, 400, "رمز عبور باید حداقل ۱۰ کاراکتر داشته باشد")
			return
		}
		if p.Role == "" {
			p.Role = "viewer"
		}
		if p.Role != "manager" && p.Role != "operator" && p.Role != "viewer" {
			fail(w, 400, "نقش کاربر معتبر نیست")
			return
		}
		hash, err := hashPassword(p.Password)
		if err != nil {
			fail(w, 500, err.Error())
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
		var role string
		if err := a.queryRow(`SELECT role FROM users WHERE id_user=?`, id).Scan(&role); err != nil {
			fail(w, http.StatusNotFound, "کاربر پیدا نشد")
			return
		}
		if strings.EqualFold(role, "admin") || strings.EqualFold(role, "owner") {
			fail(w, http.StatusBadRequest, "حساب مدیر اصلی قابل غیرفعال‌سازی نیست")
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
	var role string
	if err := a.queryRow(`SELECT role FROM users WHERE id_user=?`, id).Scan(&role); err != nil {
		fail(w, http.StatusNotFound, "کاربر پیدا نشد")
		return
	}
	if strings.EqualFold(role, "admin") || strings.EqualFold(role, "owner") {
		fail(w, 400, "ادمین اصلی قابل حذف نیست")
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

func (a *app) userMenus(session sessionInfo) []record {
	role := strings.ToLower(strings.TrimSpace(session.Role))
	if role == "admin" || role == "owner" || role == "manager" {
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
	if session.Portal {
		rows, err := a.query(`SELECT menu_key, menu_name, path, COALESCE(icon,''), COALESCE(is_restricted,0)
			FROM menu_items WHERE COALESCE(path,'')<>'' ORDER BY sort_order, id_menu`)
		if err != nil {
			return []record{}
		}
		defer rows.Close()
		allowed := map[string]bool{}
		for _, key := range session.MenuKeys {
			allowed[key] = true
		}
		out := []record{}
		for rows.Next() {
			var key, name, path, icon string
			var restricted int64
			_ = rows.Scan(&key, &name, &path, &icon, &restricted)
			if restricted == 1 && !allowed[key] && !allowed["*"] {
				continue
			}
			out = append(out, record{"menu_key": key, "menu_name": name, "path": path, "icon": icon, "is_restricted": restricted, "has_access": 1})
		}
		return out
	}
	rows, err := a.query(`SELECT m.menu_key, m.menu_name, m.path, COALESCE(m.icon,''), COALESCE(m.is_restricted,0),
		COALESCE(uma.has_access, CASE WHEN COALESCE(m.is_restricted,0)=1 THEN 0 ELSE 1 END)
		FROM menu_items m
		LEFT JOIN user_menu_access uma ON uma.menu_key=m.menu_key AND uma.user_id=?
		WHERE COALESCE(m.path,'')<>''
		ORDER BY m.sort_order, m.id_menu`, session.UserID)
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

func (a *app) saveUserMenuAccess(w http.ResponseWriter, r *http.Request, userID int64) {
	var p struct {
		MenuAccess map[string]int `json:"menu_access"`
	}
	if !decode(w, r, &p) {
		return
	}
	tx, err := a.begin()
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
	if strings.HasPrefix(path, "recent-chelles/") {
		a.recentChelles(w, strings.TrimPrefix(path, "recent-chelles/"))
		return
	}
	if strings.HasPrefix(path, "defaults/") {
		a.salonDefaults(w, strings.TrimPrefix(path, "defaults/"))
		return
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
		writeSave(w, execErr(a.exec(`DELETE FROM salon WHERE id_salon=?`, id)))
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
	tx, err := a.begin()
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	note := "مرجوع پود باقیمانده به انبار"
	if p.Action == "assign_new" {
		if p.NewChelle == "" {
			_ = tx.Rollback()
			fail(w, 400, "چله جدید مشخص نیست")
			return
		}
		note = "انتقال پود باقیمانده به چله جدید"
	}
	_, err = txExec(a.dialect, tx, `INSERT INTO nakh_salon (tarikh_nakh_salon, shom_machin_nakh_salon, ham_nakh_salon, w_nakh_salon, shom_chelle_nakh_salon, mosh_name_nakh_salon, vor_khor_nakh_salon) VALUES (?,?,?,?,?,?,?)`, jalaliToday(), p.Machine, ham, -leftover, p.OldChelle, mosh, note)
	if err == nil && p.Action == "assign_new" {
		_, err = txExec(a.dialect, tx, `INSERT INTO nakh_salon (tarikh_nakh_salon, shom_machin_nakh_salon, ham_nakh_salon, w_nakh_salon, shom_chelle_nakh_salon, mosh_name_nakh_salon, vor_khor_nakh_salon) VALUES (?,?,?,?,?,?,?)`, jalaliToday(), p.Machine, ham, leftover, p.NewChelle, mosh, note)
	}
	if err != nil {
		_ = tx.Rollback()
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, record{"success": tx.Commit() == nil, "leftover_pod": leftover})
}

func (a *app) podLeftover(machine, shom string) (float64, string, string) {
	assigned, used := 0.0, 0.0
	ham, mosh := "", ""
	whereMachine, args := machineWhere("shom_machin_nakh_salon", machine)
	args = append(args, shom)
	_ = a.queryRow(`SELECT COALESCE(SUM(w_nakh_salon),0), COALESCE(MAX(ham_nakh_salon),''), COALESCE(MAX(mosh_name_nakh_salon),'') FROM nakh_salon WHERE `+whereMachine+` AND shom_chelle_nakh_salon=?`, args...).Scan(&assigned, &ham, &mosh)
	tar, pod := 50.0, 50.0
	_ = a.queryRow(`SELECT COALESCE(tar_percent,50), COALESCE(pod_percent,50) FROM machine_formul WHERE machine=?`, machine).Scan(&tar, &pod)
	if tar+pod <= 0 {
		pod = 50
	} else {
		pod = pod * 100 / (tar + pod)
	}
	whereSalon, salonArgs := machineWhere("machin_salon", machine)
	salonArgs = append(salonArgs, shom)
	_ = a.queryRow(`SELECT COALESCE(SUM(w_salon),0) FROM salon WHERE `+whereSalon+` AND shom_chelle_salon=?`, salonArgs...).Scan(&used)
	leftover := assigned - (used * pod / 100)
	if leftover < 0 {
		leftover = 0
	}
	return leftover, ham, mosh
}

func (a *app) salonDefaults(w http.ResponseWriter, machine string) {
	where, args := machineWhere("s.machin_salon", machine)
	row := a.queryRow(`SELECT s.kala_salon, COALESCE(k.id_kala_name,0), s.ham_pod_salon, s.ham_chelle_salon, s.shom_chelle_salon FROM salon s LEFT JOIN kala_name k ON k.name_kala_name=s.kala_salon WHERE `+where+` ORDER BY s.id_salon DESC LIMIT 1`, args...)
	var kala, hamPod, hamChelle, shom string
	var kalaID int64
	if err := row.Scan(&kala, &kalaID, &hamPod, &hamChelle, &shom); err != nil {
		wherePod, podArgs := machineWhere("shom_machin_nakh_salon", machine)
		var latestPod string
		_ = a.queryRow(`SELECT COALESCE(ham_nakh_salon,'') FROM nakh_salon WHERE `+wherePod+` AND COALESCE(ham_nakh_salon,'')<>'' ORDER BY id_nakh_salon DESC LIMIT 1`, podArgs...).Scan(&latestPod)
		if latestPod != "" {
			writeJSON(w, record{"success": true, "found": true, "ham_pod": latestPod})
			return
		}
		writeJSON(w, record{"success": true, "found": false})
		return
	}
	wherePod, podArgs := machineWhere("shom_machin_nakh_salon", machine)
	var latestPod string
	_ = a.queryRow(`SELECT COALESCE(ham_nakh_salon,'') FROM nakh_salon WHERE `+wherePod+` AND COALESCE(ham_nakh_salon,'')<>'' ORDER BY id_nakh_salon DESC LIMIT 1`, podArgs...).Scan(&latestPod)
	if latestPod != "" {
		hamPod = latestPod
	}
	writeJSON(w, record{
		"success": true, "found": true, "kala": kala, "kala_id": kalaID,
		"ham_pod": hamPod, "ham_chelle": hamChelle, "shom_chelle": shom,
	})
}

func (a *app) recentChelles(w http.ResponseWriter, machine string) {
	where, args := machineWhere("g.machin_gere", machine)
	rows, err := a.query(`SELECT g.id_gere, COALESCE(g.tarikh_gere,''), g.shom_chelle_gere, g.machin_gere, COALESCE(c.w_chelle,0), COALESCE(c.hambaft_chelle,'') FROM gere g LEFT JOIN chelle c ON c.shom_chelle=g.shom_chelle_gere WHERE `+where+` AND COALESCE(g.shom_chelle_gere,'')<>'' ORDER BY COALESCE(g.tarikh_gere,'') DESC, g.id_gere DESC`, args...)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []record{}
	seen := map[string]bool{}
	for rows.Next() {
		var id int64
		var tarikh, shom, mach, hambaft string
		var weight float64
		_ = rows.Scan(&id, &tarikh, &shom, &mach, &weight, &hambaft)
		if seen[shom] {
			continue
		}
		seen[shom] = true
		items = append(items, record{"id_gere": id, "tarikh": tarikh, "shom_chelle": shom, "machine": mach, "weight": weight, "hambaft": hambaft})
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
	for _, tbl := range []string{"salon", "nakh_salon", "gere", "chelle", "nakh_vor", "machine_consumption"} {
		if _, err := a.exec("DELETE FROM " + tbl); err != nil {
			fail(w, 500, err.Error())
			return
		}
	}
	writeJSON(w, record{"success": true})
}

func (a *app) updateConsumption(machine, shom string, weight float64) error {
	tar, pod := 50.0, 50.0
	_ = a.queryRow(`SELECT COALESCE(tar_percent,50), COALESCE(pod_percent,50) FROM machine_formul WHERE machine=?`, machine).Scan(&tar, &pod)
	tarUsed := weight * tar / 100
	podUsed := weight * pod / 100
	initial := 0.0
	_ = a.queryRow(`SELECT COALESCE(w_chelle,0) FROM chelle WHERE shom_chelle=?`, shom).Scan(&initial)
	prevTar, prevPod, prevTotal := 0.0, 0.0, 0.0
	_ = a.queryRow(`SELECT COALESCE(tar_used,0), COALESCE(pod_used,0), COALESCE(total_weight,0) FROM machine_consumption WHERE machine=? AND shom_chelle=? ORDER BY id_consumption DESC LIMIT 1`, machine, shom).Scan(&prevTar, &prevPod, &prevTotal)
	_, err := a.exec(`INSERT INTO machine_consumption (machine, shom_chelle, tar_used, pod_used, total_weight, remaining_weight, tarikh_consumption) VALUES (?,?,?,?,?,?,?)`, machine, shom, prevTar+tarUsed, prevPod+podUsed, prevTotal+weight, initial-(prevTar+tarUsed)-(prevPod+podUsed), jalaliToday())
	return err
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
			SELECT hambaft_nakh_vor AS hambaft, moshname_nakh_vor AS mosh FROM nakh_vor
			UNION
			SELECT hambaft_nakh_khor AS hambaft, moshname_nakh_khor AS mosh FROM nakh_khor
			UNION
			SELECT ham_nakh_salon AS hambaft, mosh_name_nakh_salon AS mosh FROM nakh_salon
		)
		SELECT k.hambaft, k.mosh,
			COALESCE((SELECT SUM(w_vor_nakh_vor) FROM nakh_vor v WHERE v.hambaft_nakh_vor=k.hambaft AND v.moshname_nakh_vor=k.mosh),0) AS vorud,
			COALESCE((SELECT SUM(w_nakh_salon) FROM nakh_salon s WHERE s.ham_nakh_salon=k.hambaft AND s.mosh_name_nakh_salon=k.mosh),0) AS salon,
			COALESCE((SELECT SUM(w_vor_nakh_khor) FROM nakh_khor kh WHERE kh.hambaft_nakh_khor=k.hambaft AND kh.moshname_nakh_khor=k.mosh),0) AS khoroj
		FROM yarn_keys k
		WHERE COALESCE(k.hambaft,'')<>'' AND COALESCE(k.mosh,'')<>''
		ORDER BY k.hambaft, k.mosh`)
	if err != nil {
		return []record{}
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var h, m string
		var vor, salon, khor float64
		_ = rows.Scan(&h, &m, &vor, &salon, &khor)
		if salon < 0 {
			salon = 0
		}
		inv := vor - absFloat(salon) - absFloat(khor)
		if inv != 0 {
			items = append(items, record{"hambaft": h, "mosh": m, "inventory": inv, "vorud": vor, "to_salon": absFloat(salon), "khoroj": absFloat(khor)})
		}
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
		SELECT machine, shom_chelle, tarikh, sort_id
		FROM (
			SELECT machin_gere AS machine, shom_chelle_gere AS shom_chelle, COALESCE(tarikh_gere,'') AS tarikh, id_gere AS sort_id
			FROM gere
			WHERE COALESCE(machin_gere,'')<>'' AND COALESCE(shom_chelle_gere,'')<>''
			UNION ALL
			SELECT machin_chelle AS machine, shom_chelle AS shom_chelle, COALESCE(tarikh_chelle,'') AS tarikh, id_chelle AS sort_id
			FROM chelle
			WHERE COALESCE(machin_chelle,'')<>'' AND COALESCE(shom_chelle,'')<>''
		) active_sources
		ORDER BY machine, tarikh DESC, sort_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type activeChelle struct {
		machine string
		shom    string
		tarikh  string
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
		active = append(active, activeChelle{machine: machine, shom: shom, tarikh: tarikh})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := []record{}
	for _, row := range active {
		machine, shom, tarikh := row.machine, row.shom, row.tarikh
		var chelleWeight, totalWeight, totalMeter, podAssigned, tarPercent, podPercent float64
		tarPercent, podPercent = 50, 50
		_ = a.queryRow(`SELECT COALESCE(w_chelle,0) FROM chelle WHERE shom_chelle=? ORDER BY id_chelle DESC LIMIT 1`, shom).Scan(&chelleWeight)
		_ = a.queryRow(`SELECT COALESCE(SUM(w_salon),0), COALESCE(SUM(metr_salon),0) FROM salon WHERE machin_salon=? AND shom_chelle_salon=?`, machine, shom).Scan(&totalWeight, &totalMeter)
		_ = a.queryRow(`SELECT COALESCE(SUM(w_nakh_salon),0) FROM nakh_salon WHERE shom_machin_nakh_salon=? AND shom_chelle_nakh_salon=?`, machine, shom).Scan(&podAssigned)
		_ = a.queryRow(`SELECT COALESCE(tar_percent,50), COALESCE(pod_percent,50) FROM machine_formul WHERE machine=?`, machine).Scan(&tarPercent, &podPercent)
		tarUsed := totalWeight * tarPercent / 100
		podUsed := totalWeight * podPercent / 100
		used := tarUsed + podUsed
		totalAvailable := chelleWeight + podAssigned
		remaining := totalAvailable - used
		if remaining < 0 {
			remaining = 0
		}
		remainingPercent := 0.0
		if totalAvailable > 0 {
			remainingPercent = remaining * 100 / totalAvailable
		}
		wastePerMeter := 0.0
		if totalMeter > 0 {
			wastePerMeter = remaining / totalMeter
		}
		wastePerKg := 0.0
		if totalWeight > 0 {
			wastePerKg = remaining / totalWeight
		}
		wastePercentPerKg := 0.0
		if totalWeight+remaining > 0 {
			wastePercentPerKg = remaining * 100 / (totalWeight + remaining)
		}
		items = append(items, record{
			"machine": machine, "shom_chelle": shom, "tarikh": tarikh,
			"chelle_weight": chelleWeight, "pod_assigned": podAssigned, "total_available": totalAvailable,
			"tar_percent": tarPercent, "pod_percent": podPercent,
			"tar_used": tarUsed, "pod_used": podUsed, "total_used": used,
			"total_weight": totalWeight, "total_meter": totalMeter,
			"remaining": remaining, "remaining_percent": remainingPercent,
			"waste_weight": remaining, "waste_per_meter": wastePerMeter,
			"waste_per_kg": wastePerKg, "waste_percent_per_kg": wastePercentPerKg,
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

func (a *app) warperYarnBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rows, err := a.query(`
		WITH keys AS (
			SELECT kh.moshname_nakh_khor AS warper, kh.hambaft_nakh_khor AS hambaft, kh.nakh_name_nakh_khor AS yarn
			FROM nakh_khor kh
			INNER JOIN chellepich cp ON cp.name_chellepich=kh.moshname_nakh_khor
			UNION
			SELECT c.pich_chelle AS warper, c.hambaft_chelle AS hambaft, c.nakh_chelle AS yarn
			FROM chelle c
			WHERE COALESCE(c.pich_chelle,'')<>''
		)
		SELECT k.warper, k.hambaft, k.yarn,
			COALESCE((SELECT SUM(ABS(COALESCE(kh.w_vor_nakh_khor,0))) FROM nakh_khor kh WHERE kh.moshname_nakh_khor=k.warper AND kh.hambaft_nakh_khor=k.hambaft AND kh.nakh_name_nakh_khor=k.yarn),0) AS sent_weight,
			COALESCE((SELECT SUM(COALESCE(c.w_chelle,0)) FROM chelle c WHERE c.pich_chelle=k.warper AND c.hambaft_chelle=k.hambaft AND c.nakh_chelle=k.yarn),0) AS returned_weight,
			COALESCE((SELECT COUNT(*) FROM chelle c WHERE c.pich_chelle=k.warper AND c.hambaft_chelle=k.hambaft AND c.nakh_chelle=k.yarn),0) AS chelle_count,
			COALESCE((SELECT MAX(kh.tarikh_nakh_khor) FROM nakh_khor kh WHERE kh.moshname_nakh_khor=k.warper AND kh.hambaft_nakh_khor=k.hambaft AND kh.nakh_name_nakh_khor=k.yarn),'') AS last_sent_date,
			COALESCE((SELECT MAX(c.tarikh_chelle) FROM chelle c WHERE c.pich_chelle=k.warper AND c.hambaft_chelle=k.hambaft AND c.nakh_chelle=k.yarn),'') AS last_return_date
		FROM keys k
		WHERE COALESCE(k.warper,'')<>'' AND COALESCE(k.hambaft,'')<>'' AND COALESCE(k.yarn,'')<>''
		ORDER BY k.warper, k.hambaft, k.yarn`)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	defer rows.Close()
	items := []record{}
	for rows.Next() {
		var warper, hambaft, yarn, lastSent, lastReturn string
		var sent, returned float64
		var chelleCount int64
		if err := rows.Scan(&warper, &hambaft, &yarn, &sent, &returned, &chelleCount, &lastSent, &lastReturn); err != nil {
			fail(w, 500, err.Error())
			return
		}
		balance := sent - returned
		items = append(items, record{
			"warper": warper, "hambaft": hambaft, "yarn": yarn,
			"sent_weight": sent, "returned_weight": returned, "balance_weight": balance,
			"chelle_count": chelleCount, "last_sent_date": lastSent, "last_return_date": lastReturn,
		})
	}
	if err := rows.Err(); err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, items)
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

func (a *app) notifications() []record {
	items := []record{}
	for _, y := range a.yarnInventory() {
		inv, _ := y["inventory"].(float64)
		if inv < -0.01 {
			items = append(items, record{"type": "critical", "title": "کسری موجودی نخ", "message": fmt.Sprintf("موجودی همبافت %v برای %v منفی و برابر %.1f کیلو است", y["hambaft"], y["mosh"], inv)})
		} else if inv > 0 && inv < 10 {
			items = append(items, record{"type": "warning", "title": "موجودی نخ کم", "message": fmt.Sprintf("همبافت %v برای %v فقط %.1f کیلو موجودی دارد", y["hambaft"], y["mosh"], inv)})
		}
	}
	stock := a.stockSummary()
	if total, ok := stock["total_taghe"].(int64); ok && total > 30 {
		items = append(items, record{"type": "info", "title": "طاقه‌های خروج نخورده", "message": fmt.Sprintf("%d طاقه در انبار موجود است که هنوز فاکتور خروج نخورده‌اند", total)})
	}
	rows, err := a.query(`SELECT COALESCE(part_name,''), COALESCE(quantity,0) FROM spare_parts_inventory WHERE COALESCE(quantity,0)<=0 ORDER BY id_spare_inventory DESC LIMIT 10`)
	if err == nil {
		for rows.Next() {
			var name string
			var quantity float64
			_ = rows.Scan(&name, &quantity)
			items = append(items, record{"type": "warning", "title": "اتمام موجودی قطعه", "message": fmt.Sprintf("موجودی قطعه %s برابر %.0f است", name, quantity)})
		}
		_ = rows.Close()
	}
	rows, err = a.query(`SELECT machine, shom_chelle, COALESCE(remaining_weight,0) FROM machine_consumption WHERE COALESCE(remaining_weight,0)<0 ORDER BY id_consumption DESC LIMIT 10`)
	if err == nil {
		for rows.Next() {
			var machine, chelle string
			var remaining float64
			_ = rows.Scan(&machine, &chelle, &remaining)
			items = append(items, record{"type": "critical", "title": "مصرف بیش از موجودی چله", "message": fmt.Sprintf("ماشین %s برای چله %s دارای مانده منفی %.1f کیلو است", machine, chelle, remaining)})
		}
		_ = rows.Close()
	}
	rows, err = a.query(`SELECT e.kod_navard, COALESCE(e.chellepich_name,'') FROM empty_beam_out e
		WHERE COALESCE(e.kod_navard,'')<>'' AND COALESCE(e.returned_at,'')=''
		ORDER BY e.id_empty_beam_out DESC LIMIT 10`)
	if err == nil {
		for rows.Next() {
			var beam, warper string
			_ = rows.Scan(&beam, &warper)
			items = append(items, record{"type": "info", "title": "نورد خالی نزد چله‌پیچ", "message": fmt.Sprintf("نورد %s هنوز از %s بازنگشته است", beam, warper)})
		}
		_ = rows.Close()
	}
	return items
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
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

func (a *app) scalarFloat(q string) float64 {
	var n float64
	_ = a.queryRow(q).Scan(&n)
	return n
}

func (a *app) distinct(q string) []string {
	rows, err := a.query(q)
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

func int64Env(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func normalizedCompanyID(companyID int64) int64 {
	if companyID > 0 {
		return companyID
	}
	return 1
}
