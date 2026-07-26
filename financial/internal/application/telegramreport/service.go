package telegramreport

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

const pairingTTL = 10 * time.Minute

type Config struct {
	Enabled     bool
	BotToken    string
	BotUsername string
	APIBase     string
}

func ConfigFromEnv() Config {
	return Config{
		Enabled:     strings.EqualFold(strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_ENABLED")), "true"),
		BotToken:    strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_BOT_TOKEN")),
		BotUsername: strings.TrimPrefix(strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_BOT_USERNAME")), "@"),
		APIBase:     "https://api.telegram.org",
	}
}

type Service struct {
	db         *sql.DB
	cfg        Config
	client     *http.Client
	offsetMu   sync.Mutex
	nextOffset int64
}

type Settings struct {
	Available     bool       `json:"available"`
	BotUsername   string     `json:"bot_username"`
	ChatID        string     `json:"-"`
	ChatTitle     string     `json:"chat_title"`
	Connected     bool       `json:"connected"`
	Enabled       bool       `json:"enabled"`
	AlertsEnabled bool       `json:"alerts_enabled"`
	DailyTime     string     `json:"daily_time"`
	Timezone      string     `json:"timezone"`
	LastDailyOn   *time.Time `json:"last_daily_on,omitempty"`
}

type Pairing struct {
	DeepLink  string    `json:"deep_link"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Delivery struct {
	ID              int64     `json:"id"`
	ReportType      string    `json:"report_type"`
	ReportDate      time.Time `json:"report_date"`
	Status          string    `json:"status"`
	Summary         string    `json:"summary"`
	ErrorMessage    string    `json:"error_message"`
	TelegramMessage *int64    `json:"telegram_message_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type reportSnapshot struct {
	Company        string
	Date           string
	OutputCount    int
	OutputWeight   float64
	OutputMeters   float64
	OutputTotal    float64
	InputCount     int
	InputWeight    float64
	InputTotal     float64
	YarnOutCount   int
	YarnOutWeight  float64
	InventoryQty   float64
	InventoryValue float64
	ScrapWeight    float64
}

func New(db *sql.DB, cfg Config) *Service {
	if strings.TrimSpace(cfg.APIBase) == "" {
		cfg.APIBase = "https://api.telegram.org"
	}
	return &Service{db: db, cfg: cfg, client: &http.Client{Timeout: 35 * time.Second}}
}

func (s *Service) Available() bool {
	return s != nil && s.cfg.Enabled && s.cfg.BotToken != "" && s.cfg.BotUsername != ""
}

func (s *Service) Start(ctx context.Context) {
	if !s.Available() {
		log.Printf("telegram daily reports disabled: configure TEXTILE_TELEGRAM_* secrets")
		return
	}
	go s.pollLoop(ctx)
	go s.scheduleLoop(ctx)
}

func (s *Service) GetSettings(ctx context.Context, companyID int64) (Settings, error) {
	result := Settings{Available: s.Available(), BotUsername: s.cfg.BotUsername, AlertsEnabled: true, DailyTime: "20:00", Timezone: "Asia/Tehran"}
	if s.db == nil {
		return result, errors.New("database is not available")
	}
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		var last sql.NullTime
		err := q.QueryRowContext(ctx, `
			SELECT chat_id, chat_title, enabled, alerts_enabled, daily_time, timezone, last_daily_on
			FROM telegram_report_configs WHERE company_id=$1
		`, companyID).Scan(&result.ChatID, &result.ChatTitle, &result.Enabled, &result.AlertsEnabled, &result.DailyTime, &result.Timezone, &last)
		if errors.Is(err, sql.ErrNoRows) {
			return struct{}{}, nil
		}
		if last.Valid {
			result.LastDailyOn = &last.Time
		}
		return struct{}{}, err
	})
	result.Connected = strings.TrimSpace(result.ChatID) != ""
	return result, err
}

func (s *Service) SaveSettings(ctx context.Context, companyID int64, enabled, alertsEnabled bool, dailyTime, timezone string) (Settings, error) {
	if !validClock(dailyTime) {
		return Settings{}, errors.New("زمان ارسال باید به شکل HH:MM باشد")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Settings{}, errors.New("منطقه زمانی معتبر نیست")
	}
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		_, err := q.ExecContext(ctx, `
			INSERT INTO telegram_report_configs(company_id, enabled, alerts_enabled, daily_time, timezone)
			VALUES($1,$2,$3,$4,$5)
			ON CONFLICT(company_id) DO UPDATE SET
				enabled=EXCLUDED.enabled, alerts_enabled=EXCLUDED.alerts_enabled,
				daily_time=EXCLUDED.daily_time, timezone=EXCLUDED.timezone, updated_at=CURRENT_TIMESTAMP
		`, companyID, enabled, alertsEnabled, dailyTime, timezone)
		return struct{}{}, err
	})
	if err != nil {
		return Settings{}, err
	}
	return s.GetSettings(ctx, companyID)
}

func (s *Service) CreatePairing(ctx context.Context, companyID, userID int64) (Pairing, error) {
	if !s.Available() {
		return Pairing{}, errors.New("بات تلگرام هنوز توسط مدیر سرور فعال نشده است")
	}
	code, err := randomCode(24)
	if err != nil {
		return Pairing{}, err
	}
	expires := time.Now().UTC().Add(pairingTTL)
	_, err = postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		if _, err := q.ExecContext(ctx, `
			DELETE FROM telegram_pairing_codes
			WHERE company_id=$1 AND (used_at IS NOT NULL OR expires_at<CURRENT_TIMESTAMP)
		`, companyID); err != nil {
			return struct{}{}, err
		}
		_, err := q.ExecContext(ctx, `
			INSERT INTO telegram_pairing_codes(company_id, code_hash, created_by, expires_at)
			VALUES($1,$2,$3,$4)
		`, companyID, hashCode(code), nullableUser(userID), expires)
		return struct{}{}, err
	})
	if err != nil {
		return Pairing{}, err
	}
	return Pairing{
		DeepLink:  "https://t.me/" + url.PathEscape(s.cfg.BotUsername) + "?start=" + url.QueryEscape(code),
		ExpiresAt: expires,
	}, nil
}

func (s *Service) SendTest(ctx context.Context, companyID int64) error {
	if !s.Available() {
		return errors.New("بات تلگرام هنوز توسط مدیر سرور فعال نشده است")
	}
	settings, err := s.GetSettings(ctx, companyID)
	if err != nil {
		return err
	}
	if !settings.Connected {
		return errors.New("ابتدا تلگرام را با کد QR متصل کنید")
	}
	snapshot, err := s.collect(ctx, companyID, time.Now())
	if err != nil {
		return err
	}
	_, err = s.sendMessage(ctx, settings.ChatID, formatTextileReport(snapshot, true))
	return err
}

func (s *Service) History(ctx context.Context, companyID int64, limit int) ([]Delivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	return postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) ([]Delivery, error) {
		rows, err := q.QueryContext(ctx, `
			SELECT id, report_type, report_date, status, summary, error_message, telegram_message_id, created_at
			FROM telegram_report_deliveries
			WHERE company_id=$1 ORDER BY created_at DESC LIMIT $2
		`, companyID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]Delivery, 0)
		for rows.Next() {
			var item Delivery
			var messageID sql.NullInt64
			if err := rows.Scan(&item.ID, &item.ReportType, &item.ReportDate, &item.Status, &item.Summary, &item.ErrorMessage, &messageID, &item.CreatedAt); err != nil {
				return nil, err
			}
			if messageID.Valid {
				value := messageID.Int64
				item.TelegramMessage = &value
			}
			result = append(result, item)
		}
		return result, rows.Err()
	})
}

func (s *Service) pollLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if err := s.pollOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("telegram polling error: %v", err)
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
			}
		}
	}
}

func (s *Service) pollOnce(ctx context.Context) error {
	s.offsetMu.Lock()
	offset := s.nextOffset
	s.offsetMu.Unlock()
	var response struct {
		OK     bool `json:"ok"`
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				Text string `json:"text"`
				Chat struct {
					ID    int64  `json:"id"`
					Type  string `json:"type"`
					Title string `json:"title"`
					First string `json:"first_name"`
					Last  string `json:"last_name"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}
	endpoint := fmt.Sprintf("%s/bot%s/getUpdates?timeout=25&allowed_updates=%%5B%%22message%%22%%5D&offset=%d", strings.TrimRight(s.cfg.APIBase, "/"), s.cfg.BotToken, offset)
	if err := s.call(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return err
	}
	for _, update := range response.Result {
		s.offsetMu.Lock()
		if update.UpdateID >= s.nextOffset {
			s.nextOffset = update.UpdateID + 1
		}
		s.offsetMu.Unlock()
		if update.Message == nil || update.Message.Chat.Type != "private" {
			continue
		}
		parts := strings.Fields(strings.TrimSpace(update.Message.Text))
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "/start") {
			continue
		}
		title := strings.TrimSpace(update.Message.Chat.First + " " + update.Message.Chat.Last)
		if title == "" {
			title = update.Message.Chat.Title
		}
		company, err := s.consumePairing(ctx, parts[1], strconv.FormatInt(update.Message.Chat.ID, 10), title)
		if err != nil {
			_, _ = s.sendMessage(ctx, strconv.FormatInt(update.Message.Chat.ID, 10), "کد اتصال نامعتبر یا منقضی شده است. از داخل Textile ERP یک QR جدید بسازید.")
			continue
		}
		_, _ = s.sendMessage(ctx, strconv.FormatInt(update.Message.Chat.ID, 10), "✅ اتصال امن گزارش روزانه «"+company+"» انجام شد.")
	}
	return nil
}

func (s *Service) consumePairing(ctx context.Context, code, chatID, chatTitle string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id, companyID int64
	var companyName string
	if err := tx.QueryRowContext(ctx, `
		SELECT p.id, p.company_id, c.name
		FROM telegram_pairing_codes p JOIN companies c ON c.id=p.company_id
		WHERE p.code_hash=$1 AND p.used_at IS NULL AND p.expires_at>CURRENT_TIMESTAMP
		FOR UPDATE
	`, hashCode(code)).Scan(&id, &companyID, &companyName); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO telegram_report_configs(company_id, chat_id, chat_title)
		VALUES($1,$2,$3)
		ON CONFLICT(company_id) DO UPDATE SET chat_id=EXCLUDED.chat_id, chat_title=EXCLUDED.chat_title, updated_at=CURRENT_TIMESTAMP
	`, companyID, chatID, cleanText(chatTitle, 120)); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE telegram_pairing_codes SET used_at=CURRENT_TIMESTAMP WHERE id=$1`, id); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return companyName, nil
}

func (s *Service) scheduleLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.runScheduled(ctx, time.Now())
	for {
		select {
		case now := <-ticker.C:
			s.runScheduled(ctx, now)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) runScheduled(ctx context.Context, now time.Time) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT company_id, chat_id, daily_time, timezone, alerts_enabled
		FROM telegram_report_configs
		WHERE enabled=TRUE AND chat_id<>''
	`)
	if err != nil {
		log.Printf("telegram schedule query error: %v", err)
		return
	}
	defer rows.Close()
	type target struct {
		companyID                   int64
		chatID, dailyTime, timezone string
		alertsEnabled               bool
	}
	var targets []target
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.companyID, &item.chatID, &item.dailyTime, &item.timezone, &item.alertsEnabled); err == nil {
			targets = append(targets, item)
		}
	}
	for _, item := range targets {
		location, err := time.LoadLocation(item.timezone)
		if err != nil {
			continue
		}
		local := now.In(location)
		if item.alertsEnabled && local.Minute()%5 == 0 {
			s.sendAlert(ctx, item.companyID, item.chatID, local)
		}
		if local.Format("15:04") != item.dailyTime {
			continue
		}
		s.sendDaily(ctx, item.companyID, item.chatID, local)
	}
}

func (s *Service) sendAlert(ctx context.Context, companyID int64, chatID string, local time.Time) {
	snapshot, err := s.collect(ctx, companyID, local)
	if err != nil || (snapshot.InventoryQty >= 0 && snapshot.ScrapWeight <= 0) {
		return
	}
	date := local.Format("2006-01-02")
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO telegram_report_deliveries(company_id, report_type, report_date, status)
		VALUES($1,'alert',$2,'sending')
		ON CONFLICT(company_id,report_type,report_date) DO NOTHING
	`, companyID, date)
	if err != nil {
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return
	}
	text := fmt.Sprintf("🚨 هشدار Textile ERP\n🏢 %s\n📅 %s\n• موجودی خالص: %s\n• ضایعات امروز: %s کیلو\nلطفاً وضعیت انبار و تولید بررسی شود.",
		fallback(snapshot.Company, "نساجی"), date, formatNumber(snapshot.InventoryQty), formatNumber(snapshot.ScrapWeight))
	messageID, sendErr := s.sendMessage(ctx, chatID, text)
	status, errorText := "sent", ""
	if sendErr != nil {
		status, errorText = "failed", cleanText(sendErr.Error(), 500)
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE telegram_report_deliveries
		SET status=$3, summary=$4, error_message=$5, telegram_message_id=$6
		WHERE company_id=$1 AND report_type='alert' AND report_date=$2
	`, companyID, date, status, "هشدار فوری Textile ERP", errorText, nullableMessage(messageID))
}

func (s *Service) sendDaily(ctx context.Context, companyID int64, chatID string, local time.Time) {
	date := local.Format("2006-01-02")
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO telegram_report_deliveries(company_id, report_type, report_date, status)
		VALUES($1,'daily',$2,'sending')
		ON CONFLICT(company_id,report_type,report_date) DO NOTHING
	`, companyID, date)
	if err != nil {
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return
	}
	snapshot, reportErr := s.collect(ctx, companyID, local)
	var messageID int64
	if reportErr == nil {
		messageID, reportErr = s.sendMessage(ctx, chatID, formatTextileReport(snapshot, false))
	}
	status, errText := "sent", ""
	if reportErr != nil {
		status, errText = "failed", cleanText(reportErr.Error(), 500)
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE telegram_report_deliveries
		SET status=$3, summary=$4, error_message=$5, telegram_message_id=$6
		WHERE company_id=$1 AND report_type='daily' AND report_date=$2
	`, companyID, date, status, "گزارش روزانه Textile ERP", errText, nullableMessage(messageID))
	if reportErr == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE telegram_report_configs SET last_daily_on=$2, updated_at=CURRENT_TIMESTAMP WHERE company_id=$1`, companyID, date)
	}
}

func (s *Service) collect(ctx context.Context, companyID int64, now time.Time) (reportSnapshot, error) {
	result := reportSnapshot{Date: now.Format("2006-01-02")}
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		if err := q.QueryRowContext(ctx, `SELECT name FROM companies WHERE id=$1`, companyID).Scan(&result.Company); err != nil {
			return struct{}{}, err
		}
		var raw []byte
		err := q.QueryRowContext(ctx, `SELECT state FROM financial_workspace_states WHERE company_id=$1`, companyID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return struct{}{}, err
		}
		var state map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &state)
		}
		collectWorkspace(&result, state, result.Date)
		var good, scrapStd, scrapExcess float64
		_ = q.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(qty_good),0), COALESCE(SUM(qty_scrap_std),0), COALESCE(SUM(qty_scrap_excess),0)
			FROM production_outputs
			WHERE company_id=$1 AND production_date::date=$2::date
		`, companyID, result.Date).Scan(&good, &scrapStd, &scrapExcess)
		if good > result.OutputWeight {
			result.OutputWeight = good
		}
		result.ScrapWeight = scrapStd + scrapExcess
		return struct{}{}, nil
	})
	return result, err
}

func collectWorkspace(result *reportSnapshot, state map[string]any, date string) {
	for _, row := range objectList(state["invoices"]) {
		if stringValue(row["date"]) != date {
			continue
		}
		result.OutputCount++
		result.OutputWeight += numberValue(first(row, "weight", "quantity"))
		result.OutputMeters += numberValue(first(row, "meter", "meters", "metr"))
		result.OutputTotal += numberValue(first(row, "total", "amount"))
	}
	for _, row := range objectList(state["incomingInvoices"]) {
		if stringValue(row["date"]) != date {
			continue
		}
		result.InputCount++
		result.InputWeight += numberValue(first(row, "quantity", "weight"))
		result.InputTotal += numberValue(first(row, "amount", "total"))
	}
	for _, row := range objectList(state["yarnOutInvoices"]) {
		if stringValue(row["date"]) != date {
			continue
		}
		result.YarnOutCount++
		result.YarnOutWeight += numberValue(first(row, "quantity", "weight"))
	}
	for _, row := range objectList(state["ownedInventory"]) {
		result.InventoryQty += numberValue(row["quantity"])
		result.InventoryValue += numberValue(row["amount"])
	}
}

func formatTextileReport(r reportSnapshot, test bool) string {
	title := "📊 گزارش روزانه Textile ERP"
	if test {
		title = "🧪 گزارش آزمایشی Textile ERP"
	}
	return fmt.Sprintf(
		"%s\n🏢 %s\n📅 %s\n\n"+
			"🏭 تولید/خروج امروز\n• تعداد ثبت: %s\n• وزن: %s کیلو\n• متراژ: %s متر\n• مبلغ خروج: %s تومان\n"+
			"• ضایعات ثبت‌شده: %s کیلو\n\n"+
			"📥 ورود کالا و نخ\n• تعداد فاکتور: %s\n• وزن/مقدار: %s\n• مبلغ: %s تومان\n\n"+
			"📤 خروج نخ\n• تعداد: %s\n• وزن: %s کیلو\n\n"+
			"📦 موجودی فعلی\n• مقدار خالص: %s\n• ارزش: %s تومان",
		title, fallback(r.Company, "نساجی"), r.Date,
		formatNumber(float64(r.OutputCount)), formatNumber(r.OutputWeight), formatNumber(r.OutputMeters), formatNumber(r.OutputTotal),
		formatNumber(r.ScrapWeight),
		formatNumber(float64(r.InputCount)), formatNumber(r.InputWeight), formatNumber(r.InputTotal),
		formatNumber(float64(r.YarnOutCount)), formatNumber(r.YarnOutWeight),
		formatNumber(r.InventoryQty), formatNumber(r.InventoryValue),
	)
}

func (s *Service) sendMessage(ctx context.Context, chatID, text string) (int64, error) {
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(s.cfg.APIBase, "/"), s.cfg.BotToken)
	err := s.call(ctx, http.MethodPost, endpoint, map[string]any{
		"chat_id": chatID, "text": text, "disable_web_page_preview": true,
	}, &response)
	if err != nil {
		return 0, err
	}
	if !response.OK {
		return 0, errors.New(fallback(response.Description, "ارسال تلگرام ناموفق بود"))
	}
	return response.Result.MessageID, nil
}

func (s *Service) call(ctx context.Context, method, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.client.Do(request)
	if err != nil {
		return errors.New(strings.ReplaceAll(err.Error(), s.cfg.BotToken, "[redacted]"))
	}
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("telegram http %d", response.StatusCode)
	}
	return nil
}

func validClock(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}

func randomCode(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashCode(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func cleanText(value string, limit int) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}

func nullableUser(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableMessage(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func objectList(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if row, ok := item.(map[string]any); ok {
			result = append(result, row)
		}
	}
	return result
}

func first(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return value
		}
	}
	return nil
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func numberValue(value any) float64 {
	switch item := value.(type) {
	case float64:
		return item
	case json.Number:
		result, _ := item.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(item), ",", ""), 64)
		return result
	default:
		result, _ := strconv.ParseFloat(fmt.Sprint(item), 64)
		return result
	}
}

func formatNumber(value float64) string {
	text := strconv.FormatFloat(value, 'f', 0, 64)
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	if negative {
		return "-" + text
	}
	return text
}

func fallback(value, replacement string) string {
	if strings.TrimSpace(value) == "" {
		return replacement
	}
	return value
}
