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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"

	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
)

const (
	pairingTTL                 = 10 * time.Minute
	telegramPollTimeoutSeconds = 5
	telegramPollCyclePause     = 20 * time.Second
)

type Config struct {
	Enabled     bool
	BotToken    string
	BotUsername string
	APIBase     string
	RelayURL    string
	RelayToken  string
	RelayMode   string
}

func ConfigFromEnv() Config {
	return Config{
		Enabled:     strings.EqualFold(strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_ENABLED")), "true"),
		BotToken:    strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_BOT_TOKEN")),
		BotUsername: strings.TrimPrefix(strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_BOT_USERNAME")), "@"),
		APIBase:     "https://api.telegram.org",
		RelayURL:    strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_RELAY_URL")),
		RelayToken:  strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_RELAY_TOKEN")),
		RelayMode:   strings.TrimSpace(os.Getenv("TEXTILE_TELEGRAM_RELAY_MODE")),
	}
}

type Service struct {
	db         *sql.DB
	cfg        Config
	client     *http.Client
	readyMu    sync.RWMutex
	ready      bool
	offsetMu   sync.Mutex
	nextOffset int64
	startOnce  sync.Once
}

type Settings struct {
	Available         bool       `json:"available"`
	Configured        bool       `json:"configured"`
	BotUsername       string     `json:"bot_username"`
	ChatID            string     `json:"-"`
	ChatTitle         string     `json:"chat_title"`
	Connected         bool       `json:"connected"`
	RecipientCount    int        `json:"recipient_count"`
	Enabled           bool       `json:"enabled"`
	AlertsEnabled     bool       `json:"alerts_enabled"`
	DailyTime         string     `json:"daily_time"`
	WeeklyEnabled     bool       `json:"weekly_enabled"`
	WeeklyDay         int        `json:"weekly_day"`
	WeeklyTime        string     `json:"weekly_time"`
	MonthlyEnabled    bool       `json:"monthly_enabled"`
	MonthlyDay        int        `json:"monthly_day"`
	MonthlyTime       string     `json:"monthly_time"`
	AccountingSLADays int        `json:"accounting_sla_days"`
	Timezone          string     `json:"timezone"`
	LastDailyOn       *time.Time `json:"last_daily_on,omitempty"`
	LastWeeklyOn      *time.Time `json:"last_weekly_on,omitempty"`
	LastMonthlyOn     *time.Time `json:"last_monthly_on,omitempty"`
}

type Recipient struct {
	ID             int64     `json:"id"`
	ChatTitle      string    `json:"chat_title"`
	Enabled        bool      `json:"enabled"`
	ReceiveDaily   bool      `json:"receive_daily"`
	ReceiveWeekly  bool      `json:"receive_weekly"`
	ReceiveMonthly bool      `json:"receive_monthly"`
	ReceiveAlerts  bool      `json:"receive_alerts"`
	CreatedAt      time.Time `json:"created_at"`
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
	Company           string
	Date              string
	PeriodStart       string
	PeriodEnd         string
	OperationalData   bool
	ActiveDays        int
	ProductionCount   int
	ProductionWeight  float64
	ProductionMeters  float64
	FabricOutInvoices int
	FabricOutPieces   int
	FabricOutWeight   float64
	FabricOutMeters   float64
	BeamInputCount    int
	BeamInputWeight   float64
	FabricStockPieces int
	FabricStockWeight float64
	FabricStockMeters float64
	YarnStockWeight   float64
	OutputCount       int
	OutputWeight      float64
	OutputMeters      float64
	OutputTotal       float64
	InputCount        int
	InputWeight       float64
	InputTotal        float64
	YarnOutCount      int
	YarnOutWeight     float64
	InventoryQty      float64
	InventoryValue    float64
	ScrapWeight       float64
	Accounting        accountingPerformance
}

type operationalReportRow struct {
	Kind      string
	Date      string
	Reference string
	Weight    float64
	Meters    float64
}

type accountingPerformance struct {
	SLADays         int
	Processed       int
	Measurable      int
	OnTime          int
	AverageDelay    float64
	MaxDelay        int
	Pending         int
	Overdue         int
	OldestPending   int
	InvalidDateRows int
	ByUser          []accountingUserPerformance
}

type accountingUserPerformance struct {
	UserID       int64
	Username     string
	Processed    int
	AverageDelay float64
	OnTimeRate   float64
}

type accountingUserTotals struct {
	processed  int
	measurable int
	delay      int
	onTime     int
}

type operationalAccountingItem struct {
	Key        string
	SourceType string
	SourceDate string
}

type financialAccountingItem struct {
	Key           string
	FinancialDate string
	ProcessedAt   string
	ProcessedBy   int64
}

func New(db *sql.DB, cfg Config) *Service {
	if strings.TrimSpace(cfg.APIBase) == "" {
		cfg.APIBase = "https://api.telegram.org"
	}
	return &Service{db: db, cfg: cfg, client: &http.Client{Timeout: 35 * time.Second}}
}

func (s *Service) Available() bool {
	if s == nil {
		return false
	}
	s.readyMu.RLock()
	defer s.readyMu.RUnlock()
	return s.ready
}

func (s *Service) Configured() bool {
	return s != nil && s.cfg.Enabled && strings.TrimSpace(s.cfg.BotToken) != "" && strings.TrimSpace(s.cfg.BotUsername) != ""
}

func (s *Service) setAvailable(available bool) {
	if s == nil {
		return
	}
	s.readyMu.Lock()
	s.ready = available
	s.readyMu.Unlock()
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || !s.cfg.Enabled || s.cfg.BotToken == "" {
		log.Printf("telegram daily reports disabled: configure TEXTILE_TELEGRAM_* secrets")
		return
	}
	s.startOnce.Do(func() {
		go s.startWithRetry(ctx)
	})
}

func (s *Service) startWithRetry(ctx context.Context) {
	retryDelay := 10 * time.Second
	for {
		if err := s.bootstrap(ctx); err == nil {
			log.Printf("telegram daily reports enabled for bot @%s", s.cfg.BotUsername)
			go s.pollLoop(ctx)
			go s.scheduleLoop(ctx)
			return
		} else {
			log.Printf("telegram daily reports temporarily unavailable; retrying: %v", err)
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if retryDelay < time.Minute {
			retryDelay *= 2
			if retryDelay > time.Minute {
				retryDelay = time.Minute
			}
		}
	}
}

func (s *Service) bootstrap(ctx context.Context) error {
	s.setAvailable(false)

	username, err := s.getMe(ctx)
	if err != nil {
		return fmt.Errorf("telegram bot validation failed: %w", err)
	}
	configuredUsername := strings.TrimPrefix(strings.TrimSpace(s.cfg.BotUsername), "@")
	if configuredUsername != "" && !strings.EqualFold(configuredUsername, username) {
		return errors.New("telegram bot username does not match the configured token")
	}
	if err := s.deleteWebhook(ctx); err != nil {
		return fmt.Errorf("telegram polling setup failed: %w", err)
	}
	s.readyMu.Lock()
	s.cfg.BotUsername = username
	s.ready = true
	s.readyMu.Unlock()
	return nil
}

func (s *Service) getMe(ctx context.Context) (string, error) {
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			IsBot    bool   `json:"is_bot"`
			Username string `json:"username"`
		} `json:"result"`
	}
	endpoint, err := s.telegramEndpoint("getMe", nil)
	if err != nil {
		return "", err
	}
	if err := s.call(ctx, http.MethodGet, "getMe", nil, endpoint, nil, &response); err != nil {
		return "", err
	}
	if !response.OK {
		return "", errors.New(s.redactToken(fallback(response.Description, "telegram bot validation failed")))
	}
	username := strings.TrimPrefix(strings.TrimSpace(response.Result.Username), "@")
	if !response.Result.IsBot || username == "" {
		return "", errors.New("telegram getMe response does not identify a bot username")
	}
	return username, nil
}

func (s *Service) deleteWebhook(ctx context.Context) error {
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      bool   `json:"result"`
	}
	endpoint, err := s.telegramEndpoint("deleteWebhook", nil)
	if err != nil {
		return err
	}
	if err := s.call(ctx, http.MethodPost, "deleteWebhook", nil, endpoint, map[string]any{"drop_pending_updates": false}, &response); err != nil {
		return err
	}
	if !response.OK || !response.Result {
		return errors.New(s.redactToken(fallback(response.Description, "telegram webhook removal failed")))
	}
	return nil
}

func (s *Service) GetSettings(ctx context.Context, companyID int64) (Settings, error) {
	result := Settings{
		Available:         s.Available(),
		Configured:        s.Configured(),
		BotUsername:       s.cfg.BotUsername,
		AlertsEnabled:     true,
		DailyTime:         "20:00",
		WeeklyEnabled:     true,
		WeeklyDay:         int(time.Thursday),
		WeeklyTime:        "20:00",
		MonthlyEnabled:    true,
		MonthlyDay:        1,
		MonthlyTime:       "20:00",
		AccountingSLADays: 2,
		Timezone:          "Asia/Tehran",
	}
	if s.db == nil {
		return result, errors.New("database is not available")
	}
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		var lastDaily, lastWeekly, lastMonthly sql.NullTime
		err := q.QueryRowContext(ctx, `
			SELECT enabled, alerts_enabled, daily_time,
			       weekly_enabled, weekly_day, weekly_time,
			       monthly_enabled, monthly_day, monthly_time,
			       accounting_sla_days, timezone,
			       last_daily_on, last_weekly_on, last_monthly_on
			FROM telegram_report_configs WHERE company_id=$1
		`, companyID).Scan(
			&result.Enabled, &result.AlertsEnabled, &result.DailyTime,
			&result.WeeklyEnabled, &result.WeeklyDay, &result.WeeklyTime,
			&result.MonthlyEnabled, &result.MonthlyDay, &result.MonthlyTime,
			&result.AccountingSLADays, &result.Timezone,
			&lastDaily, &lastWeekly, &lastMonthly,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return struct{}{}, nil
		}
		if err != nil {
			return struct{}{}, err
		}
		if lastDaily.Valid {
			result.LastDailyOn = &lastDaily.Time
		}
		if lastWeekly.Valid {
			result.LastWeeklyOn = &lastWeekly.Time
		}
		if lastMonthly.Valid {
			result.LastMonthlyOn = &lastMonthly.Time
		}
		return struct{}{}, q.QueryRowContext(ctx, `
			SELECT COUNT(*), COALESCE(MIN(chat_title), '')
			FROM telegram_report_recipients
			WHERE company_id=$1 AND enabled=TRUE
		`, companyID).Scan(&result.RecipientCount, &result.ChatTitle)
	})
	result.Connected = result.RecipientCount > 0
	return result, err
}

func (s *Service) SaveSettings(
	ctx context.Context,
	companyID int64,
	enabled, alertsEnabled bool,
	dailyTime string,
	weeklyEnabled bool,
	weeklyDay int,
	weeklyTime string,
	monthlyEnabled bool,
	monthlyDay int,
	monthlyTime string,
	accountingSLADays int,
	timezone string,
) (Settings, error) {
	if !validClock(dailyTime) || !validClock(weeklyTime) || !validClock(monthlyTime) {
		return Settings{}, errors.New("زمان ارسال باید به شکل ساعت و دقیقه معتبر باشد")
	}
	if weeklyDay < int(time.Sunday) || weeklyDay > int(time.Saturday) {
		return Settings{}, errors.New("روز گزارش هفتگی معتبر نیست")
	}
	if monthlyDay < 1 || monthlyDay > 28 {
		return Settings{}, errors.New("روز گزارش ماهانه باید بین ۱ تا ۲۸ باشد")
	}
	if accountingSLADays < 1 || accountingSLADays > 30 {
		return Settings{}, errors.New("مهلت رسیدگی حسابداری باید بین ۱ تا ۳۰ روز باشد")
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return Settings{}, errors.New("منطقه زمانی معتبر نیست")
	}
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		_, err := q.ExecContext(ctx, `
			INSERT INTO telegram_report_configs(
				company_id, enabled, alerts_enabled, daily_time,
				weekly_enabled, weekly_day, weekly_time,
				monthly_enabled, monthly_day, monthly_time,
				accounting_sla_days, timezone
			)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT(company_id) DO UPDATE SET
				enabled=EXCLUDED.enabled, alerts_enabled=EXCLUDED.alerts_enabled,
				daily_time=EXCLUDED.daily_time,
				weekly_enabled=EXCLUDED.weekly_enabled,
				weekly_day=EXCLUDED.weekly_day,
				weekly_time=EXCLUDED.weekly_time,
				monthly_enabled=EXCLUDED.monthly_enabled,
				monthly_day=EXCLUDED.monthly_day,
				monthly_time=EXCLUDED.monthly_time,
				accounting_sla_days=EXCLUDED.accounting_sla_days,
				timezone=EXCLUDED.timezone,
				updated_at=CURRENT_TIMESTAMP
		`, companyID, enabled, alertsEnabled, dailyTime,
			weeklyEnabled, weeklyDay, weeklyTime,
			monthlyEnabled, monthlyDay, monthlyTime,
			accountingSLADays, timezone)
		return struct{}{}, err
	})
	if err != nil {
		return Settings{}, err
	}
	return s.GetSettings(ctx, companyID)
}

func (s *Service) CreatePairing(ctx context.Context, companyID, userID int64) (Pairing, error) {
	if !s.Configured() {
		return Pairing{}, errors.New("بات تلگرام هنوز توسط مدیر سرور فعال نشده است")
	}
	recipients, err := s.Recipients(ctx, companyID)
	if err != nil {
		return Pairing{}, err
	}
	if len(recipients) >= 5 {
		return Pairing{}, errors.New("حداکثر پنج گیرنده برای هر شرکت قابل ثبت است")
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

func (s *Service) Recipients(ctx context.Context, companyID int64) ([]Recipient, error) {
	return postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) ([]Recipient, error) {
		rows, err := q.QueryContext(ctx, `
			SELECT id, chat_title, enabled, receive_daily, receive_weekly,
			       receive_monthly, receive_alerts, created_at
			FROM telegram_report_recipients
			WHERE company_id=$1
			ORDER BY created_at
		`, companyID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make([]Recipient, 0)
		for rows.Next() {
			var item Recipient
			if err := rows.Scan(
				&item.ID, &item.ChatTitle, &item.Enabled,
				&item.ReceiveDaily, &item.ReceiveWeekly,
				&item.ReceiveMonthly, &item.ReceiveAlerts, &item.CreatedAt,
			); err != nil {
				return nil, err
			}
			result = append(result, item)
		}
		return result, rows.Err()
	})
}

func (s *Service) UpdateRecipient(ctx context.Context, companyID, recipientID int64, input Recipient) error {
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		result, err := q.ExecContext(ctx, `
			UPDATE telegram_report_recipients
			SET enabled=$3, receive_daily=$4, receive_weekly=$5,
			    receive_monthly=$6, receive_alerts=$7,
			    updated_at=CURRENT_TIMESTAMP
			WHERE company_id=$1 AND id=$2
		`, companyID, recipientID, input.Enabled, input.ReceiveDaily,
			input.ReceiveWeekly, input.ReceiveMonthly, input.ReceiveAlerts)
		if err != nil {
			return struct{}{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return struct{}{}, err
		}
		if affected == 0 {
			return struct{}{}, errors.New("گیرنده موردنظر پیدا نشد")
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) DeleteRecipient(ctx context.Context, companyID, recipientID int64) error {
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		result, err := q.ExecContext(ctx, `
			DELETE FROM telegram_report_recipients
			WHERE company_id=$1 AND id=$2
		`, companyID, recipientID)
		if err != nil {
			return struct{}{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return struct{}{}, err
		}
		if affected == 0 {
			return struct{}{}, errors.New("گیرنده موردنظر پیدا نشد")
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) SendTest(ctx context.Context, companyID int64) error {
	if !s.Available() {
		return errors.New("بات تلگرام هنوز توسط مدیر سرور فعال نشده است")
	}
	recipients, err := s.Recipients(ctx, companyID)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return errors.New("ابتدا تلگرام را با کد QR متصل کنید")
	}
	settings, err := s.GetSettings(ctx, companyID)
	if err != nil {
		return err
	}
	now := time.Now()
	snapshot, err := s.collectPeriod(
		ctx,
		companyID,
		now.AddDate(0, 0, -29),
		now,
		settings.AccountingSLADays,
	)
	if err != nil {
		return err
	}
	sent := 0
	var lastErr error
	for _, recipient := range recipients {
		if !recipient.Enabled {
			continue
		}
		chatID, err := s.recipientChatID(ctx, companyID, recipient.ID)
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := s.sendMessage(ctx, chatID, formatTextileReport(snapshot, "test")); err != nil {
			lastErr = err
			continue
		}
		if _, err := s.sendMessage(ctx, chatID, formatAccountingReport(snapshot, "test")); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	if sent == 0 {
		if lastErr != nil {
			return lastErr
		}
		return errors.New("هیچ گیرنده فعالی برای گزارش وجود ندارد")
	}
	return nil
}

func (s *Service) recipientChatID(ctx context.Context, companyID, recipientID int64) (string, error) {
	return postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (string, error) {
		var chatID string
		err := q.QueryRowContext(ctx, `
			SELECT chat_id FROM telegram_report_recipients
			WHERE company_id=$1 AND id=$2
		`, companyID, recipientID).Scan(&chatID)
		return chatID, err
	})
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
		if err := s.pollOnce(ctx); err != nil {
			s.setAvailable(false)
			if ctx.Err() != nil {
				return
			}
			log.Printf("telegram polling error: %v", err)
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
			}
			continue
		}
		s.setAvailable(true)
		// Apps Script has a daily outbound-request quota.  A short pause after
		// each successful poll keeps pairing responsive while preventing an
		// always-on financial service from exhausting that quota.
		select {
		case <-time.After(telegramPollCyclePause):
		case <-ctx.Done():
			return
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
	query := url.Values{
		"timeout":         {strconv.Itoa(telegramPollTimeoutSeconds)},
		"allowed_updates": {`["message"]`},
		"offset":          {strconv.FormatInt(offset, 10)},
	}
	endpoint, err := s.telegramEndpoint("getUpdates", query)
	if err != nil {
		return err
	}
	if err := s.call(ctx, http.MethodGet, "getUpdates", query, endpoint, nil, &response); err != nil {
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
	var recipientCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM telegram_report_recipients WHERE company_id=$1
	`, companyID).Scan(&recipientCount); err != nil {
		return "", err
	}
	if recipientCount >= 5 {
		return "", errors.New("recipient limit reached")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO telegram_report_configs(company_id, chat_id, chat_title)
		VALUES($1,$2,$3)
		ON CONFLICT(company_id) DO UPDATE SET
			chat_id=CASE WHEN telegram_report_configs.chat_id='' THEN EXCLUDED.chat_id ELSE telegram_report_configs.chat_id END,
			chat_title=CASE WHEN telegram_report_configs.chat_id='' THEN EXCLUDED.chat_title ELSE telegram_report_configs.chat_title END,
			updated_at=CURRENT_TIMESTAMP
	`, companyID, chatID, cleanText(chatTitle, 120)); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO telegram_report_recipients(company_id, chat_id, chat_title)
		VALUES($1,$2,$3)
		ON CONFLICT(company_id, chat_id) DO UPDATE SET
			chat_title=EXCLUDED.chat_title,
			enabled=TRUE,
			updated_at=CURRENT_TIMESTAMP
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
		SELECT c.company_id, r.id, r.chat_id,
		       c.enabled, c.daily_time,
		       c.weekly_enabled, c.weekly_day, c.weekly_time,
		       c.monthly_enabled, c.monthly_day, c.monthly_time,
		       c.accounting_sla_days, c.timezone, c.alerts_enabled,
		       r.receive_daily, r.receive_weekly, r.receive_monthly, r.receive_alerts
		FROM telegram_report_configs c
		JOIN telegram_report_recipients r ON r.company_id=c.company_id
		WHERE r.enabled=TRUE AND r.chat_id<>''
	`)
	if err != nil {
		log.Printf("telegram schedule query error: %v", err)
		return
	}
	defer rows.Close()
	type target struct {
		companyID, recipientID                      int64
		chatID, dailyTime, weeklyTime, monthlyTime  string
		timezone                                    string
		dailyEnabled, weeklyEnabled, monthlyEnabled bool
		alertsEnabled                               bool
		weeklyDay, monthlyDay                       int
		accountingSLADays                           int
		receiveDaily, receiveWeekly, receiveMonthly bool
		receiveAlerts                               bool
	}
	var targets []target
	for rows.Next() {
		var item target
		if err := rows.Scan(
			&item.companyID, &item.recipientID, &item.chatID,
			&item.dailyEnabled, &item.dailyTime,
			&item.weeklyEnabled, &item.weeklyDay, &item.weeklyTime,
			&item.monthlyEnabled, &item.monthlyDay, &item.monthlyTime,
			&item.accountingSLADays, &item.timezone, &item.alertsEnabled,
			&item.receiveDaily, &item.receiveWeekly, &item.receiveMonthly, &item.receiveAlerts,
		); err == nil {
			targets = append(targets, item)
		}
	}
	for _, item := range targets {
		location, err := time.LoadLocation(item.timezone)
		if err != nil {
			continue
		}
		local := now.In(location)
		if item.alertsEnabled && item.receiveAlerts && local.Minute()%5 == 0 {
			s.sendAlert(ctx, item.companyID, item.recipientID, item.chatID, local, item.accountingSLADays)
		}
		if item.dailyEnabled && item.receiveDaily && local.Format("15:04") == item.dailyTime {
			s.sendPeriodic(ctx, item.companyID, item.recipientID, item.chatID, local, "daily", item.accountingSLADays)
		}
		if item.weeklyEnabled && item.receiveWeekly &&
			int(local.Weekday()) == item.weeklyDay &&
			local.Format("15:04") == item.weeklyTime {
			s.sendPeriodic(ctx, item.companyID, item.recipientID, item.chatID, local, "weekly", item.accountingSLADays)
		}
		if item.monthlyEnabled && item.receiveMonthly &&
			local.Day() == item.monthlyDay &&
			local.Format("15:04") == item.monthlyTime {
			s.sendPeriodic(ctx, item.companyID, item.recipientID, item.chatID, local, "monthly", item.accountingSLADays)
		}
	}
}

func (s *Service) sendAlert(ctx context.Context, companyID, recipientID int64, chatID string, local time.Time, accountingSLADays int) {
	snapshot, err := s.collectPeriod(ctx, companyID, local, local, accountingSLADays)
	if err != nil {
		return
	}
	s.sendAccountingAlert(ctx, companyID, recipientID, chatID, local, snapshot)
	if snapshot.InventoryQty >= 0 && snapshot.ScrapWeight <= 0 {
		return
	}
	date := local.Format("2006-01-02")
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO telegram_report_deliveries(company_id, recipient_id, report_type, report_date, status)
		VALUES($1,$2,'alert',$3,'sending')
		ON CONFLICT(company_id,recipient_id,report_type,report_date)
		WHERE recipient_id IS NOT NULL DO NOTHING
	`, companyID, recipientID, date)
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
		SET status=$4, summary=$5, error_message=$6, telegram_message_id=$7
		WHERE company_id=$1 AND recipient_id=$2 AND report_type='alert' AND report_date=$3
	`, companyID, recipientID, date, status, "هشدار فوری نساجی", errorText, nullableMessage(messageID))
}

func (s *Service) sendAccountingAlert(
	ctx context.Context,
	companyID, recipientID int64,
	chatID string,
	local time.Time,
	snapshot reportSnapshot,
) {
	if snapshot.Accounting.Overdue <= 0 {
		return
	}
	date := local.Format("2006-01-02")
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO telegram_report_deliveries(company_id, recipient_id, report_type, report_date, status)
		VALUES($1,$2,'accounting_alert',$3,'sending')
		ON CONFLICT(company_id,recipient_id,report_type,report_date)
		WHERE recipient_id IS NOT NULL DO NOTHING
	`, companyID, recipientID, date)
	if err != nil {
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return
	}
	messageID, sendErr := s.sendMessage(ctx, chatID, formatAccountingOverdueAlert(snapshot))
	status, errorText := "sent", ""
	if sendErr != nil {
		status, errorText = "failed", cleanText(sendErr.Error(), 500)
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE telegram_report_deliveries
		SET status=$4, summary=$5, error_message=$6, telegram_message_id=$7
		WHERE company_id=$1 AND recipient_id=$2
		  AND report_type='accounting_alert' AND report_date=$3
	`, companyID, recipientID, date, status, "هشدار تأخیر حسابداری", errorText, nullableMessage(messageID))
}

func (s *Service) sendPeriodic(ctx context.Context, companyID, recipientID int64, chatID string, local time.Time, reportType string, accountingSLADays int) {
	start, end := reportPeriod(local, reportType)
	date := local.Format("2006-01-02")
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO telegram_report_deliveries(company_id, recipient_id, report_type, report_date, status)
		VALUES($1,$2,$3,$4,'sending')
		ON CONFLICT(company_id,recipient_id,report_type,report_date)
		WHERE recipient_id IS NOT NULL DO NOTHING
	`, companyID, recipientID, reportType, date)
	if err != nil {
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return
	}
	snapshot, reportErr := s.collectPeriod(ctx, companyID, start, end, accountingSLADays)
	var messageID int64
	if reportErr == nil {
		messageID, reportErr = s.sendMessage(ctx, chatID, formatTextileReport(snapshot, reportType))
	}
	if reportErr == nil {
		_, reportErr = s.sendMessage(ctx, chatID, formatAccountingReport(snapshot, reportType))
	}
	status, errText := "sent", ""
	if reportErr != nil {
		status, errText = "failed", cleanText(reportErr.Error(), 500)
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE telegram_report_deliveries
		SET status=$5, summary=$6, error_message=$7, telegram_message_id=$8
		WHERE company_id=$1 AND recipient_id=$2 AND report_type=$3 AND report_date=$4
	`, companyID, recipientID, reportType, date, status, reportSummary(reportType), errText, nullableMessage(messageID))
	if reportErr == nil {
		column := map[string]string{
			"daily":   "last_daily_on",
			"weekly":  "last_weekly_on",
			"monthly": "last_monthly_on",
		}[reportType]
		if column != "" {
			_, _ = s.db.ExecContext(ctx,
				`UPDATE telegram_report_configs SET `+column+`=$2, updated_at=CURRENT_TIMESTAMP WHERE company_id=$1`,
				companyID, date)
		}
	}
}

func (s *Service) collectPeriod(ctx context.Context, companyID int64, start, end time.Time, accountingSLADays int) (reportSnapshot, error) {
	startDate := start.Format("2006-01-02")
	endDate := end.Format("2006-01-02")
	result := reportSnapshot{
		Date:        endDate,
		PeriodStart: startDate,
		PeriodEnd:   endDate,
	}
	var workspaceState map[string]any
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
		workspaceState = state
		collectWorkspacePeriod(&result, state, startDate, endDate)
		var good, scrapStd, scrapExcess float64
		var productionDays int
		_ = q.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(qty_good),0),
			       COALESCE(SUM(qty_scrap_std),0),
			       COALESCE(SUM(qty_scrap_excess),0),
			       COUNT(DISTINCT production_date::date)
			FROM production_outputs
			WHERE company_id=$1
			  AND production_date::date BETWEEN $2::date AND $3::date
		`, companyID, startDate, endDate).Scan(&good, &scrapStd, &scrapExcess, &productionDays)
		if good > result.OutputWeight {
			result.OutputWeight = good
		}
		result.ScrapWeight = scrapStd + scrapExcess
		result.ActiveDays = countDaysWithActivity(state, startDate, endDate)
		if productionDays > result.ActiveDays {
			result.ActiveDays = productionDays
		}
		return struct{}{}, nil
	})
	if err == nil {
		if operationalErr := s.collectOperationalPeriod(ctx, companyID, start, end, &result); operationalErr != nil {
			log.Printf(
				"telegram operational report source query failed for company %d: %v",
				companyID,
				operationalErr,
			)
		}
		result.Accounting = s.collectAccountingPerformance(
			ctx, companyID, workspaceState, start, end, accountingSLADays,
		)
	}
	return result, err
}

func (s *Service) collectOperationalPeriod(
	ctx context.Context,
	companyID int64,
	start, end time.Time,
	result *reportSnapshot,
) error {
	var schemaName string
	if err := s.db.QueryRowContext(ctx, `
		SELECT schema_name
		FROM public.operational_tenants
		WHERE external_company_id=$1 AND active=1
		ORDER BY id DESC
		LIMIT 1
	`, companyID).Scan(&schemaName); err != nil {
		return err
	}
	schema := pq.QuoteIdentifier(strings.TrimSpace(schemaName))
	query := fmt.Sprintf(`
		SELECT kind, source_date, reference, weight, meters
		FROM (
			SELECT 'production'::text AS kind,
			       COALESCE(tarikh_salon,'') AS source_date,
			       id_salon::text AS reference,
			       COALESCE(w_salon,0)::double precision AS weight,
			       COALESCE(metr_salon,0)::double precision AS meters
			FROM %[1]s.salon
			UNION ALL
			SELECT 'yarn_in', COALESCE(tarikh_nakh_vor,''), id_nakh_vor::text,
			       COALESCE(w_vor_nakh_vor,0)::double precision, 0::double precision
			FROM %[1]s.nakh_vor
			UNION ALL
			SELECT 'yarn_out', COALESCE(tarikh_nakh_khor,''), id_nakh_khor::text,
			       ABS(COALESCE(w_vor_nakh_khor,0))::double precision, 0::double precision
			FROM %[1]s.nakh_khor
			UNION ALL
			SELECT 'beam_in', COALESCE(tarikh_chelle,''), id_chelle::text,
			       COALESCE(w_chelle,0)::double precision, 0::double precision
			FROM %[1]s.chelle
			UNION ALL
			SELECT 'fabric_out', COALESCE(f.tarikh_f_khor,''),
			       TRIM(COALESCE(f.shom_f_khor,'')),
			       COALESCE(s.w_salon,0)::double precision,
			       COALESCE(s.metr_salon,0)::double precision
			FROM %[1]s.f_khor f
			LEFT JOIN %[1]s.salon s
			  ON s.id_salon::text=TRIM(COALESCE(f.taghe_cod_f_khor,''))
			UNION ALL
			SELECT 'waste', COALESCE(waste_date,''), id_waste::text,
			       COALESCE(weight,0)::double precision, 0::double precision
			FROM %[1]s.production_waste
		) operational_rows
	`, schema)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	movements := make([]operationalReportRow, 0)
	for rows.Next() {
		var row operationalReportRow
		if err := rows.Scan(
			&row.Kind,
			&row.Date,
			&row.Reference,
			&row.Weight,
			&row.Meters,
		); err != nil {
			return err
		}
		movements = append(movements, row)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var fabricPieces int
	var fabricMeters, fabricWeight float64
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*),
		       COALESCE(SUM(s.metr_salon),0),
		       COALESCE(SUM(s.w_salon),0)
		FROM %[1]s.salon s
		WHERE NOT EXISTS (
			SELECT 1
			FROM %[1]s.f_khor f
			WHERE TRIM(COALESCE(f.taghe_cod_f_khor,''))=s.id_salon::text
		)
	`, schema)).Scan(&fabricPieces, &fabricMeters, &fabricWeight); err != nil {
		return err
	}

	var yarnStock float64
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			COALESCE((SELECT SUM(w_vor_nakh_vor) FROM %[1]s.nakh_vor),0) -
			COALESCE((SELECT SUM(w_nakh_salon) FROM %[1]s.nakh_salon),0) -
			COALESCE((SELECT SUM(ABS(w_vor_nakh_khor)) FROM %[1]s.nakh_khor),0)
	`, schema)).Scan(&yarnStock); err != nil {
		return err
	}

	applyOperationalRows(result, movements, start, end)
	result.OperationalData = true
	result.FabricStockPieces = fabricPieces
	result.FabricStockMeters = fabricMeters
	result.FabricStockWeight = fabricWeight
	result.YarnStockWeight = yarnStock
	return nil
}

func applyOperationalRows(
	result *reportSnapshot,
	rows []operationalReportRow,
	start, end time.Time,
) {
	result.InputCount = 0
	result.InputWeight = 0
	result.YarnOutCount = 0
	result.YarnOutWeight = 0
	result.ScrapWeight = 0
	activeDates := make(map[string]struct{})
	outInvoices := make(map[string]struct{})
	for _, row := range rows {
		date, ok := parseAccountingDate(row.Date)
		if !ok || !dateWithinPeriod(date, start, end) {
			continue
		}
		switch row.Kind {
		case "production":
			result.ProductionCount++
			result.ProductionWeight += row.Weight
			result.ProductionMeters += row.Meters
			activeDates[date.Format("2006-01-02")] = struct{}{}
		case "fabric_out":
			result.FabricOutPieces++
			result.FabricOutWeight += row.Weight
			result.FabricOutMeters += row.Meters
			if reference := strings.TrimSpace(row.Reference); reference != "" {
				outInvoices[reference] = struct{}{}
			}
		case "yarn_in":
			result.InputCount++
			result.InputWeight += row.Weight
		case "yarn_out":
			result.YarnOutCount++
			result.YarnOutWeight += row.Weight
		case "beam_in":
			result.BeamInputCount++
			result.BeamInputWeight += row.Weight
		case "waste":
			result.ScrapWeight += row.Weight
		}
	}
	result.FabricOutInvoices = len(outInvoices)
	if len(activeDates) > result.ActiveDays {
		result.ActiveDays = len(activeDates)
	}
}

func dateWithinPeriod(value, start, end time.Time) bool {
	date := value.Format("2006-01-02")
	return date >= start.Format("2006-01-02") && date <= end.Format("2006-01-02")
}

func collectWorkspace(result *reportSnapshot, state map[string]any, date string) {
	collectWorkspacePeriod(result, state, date, date)
}

func collectWorkspacePeriod(result *reportSnapshot, state map[string]any, startDate, endDate string) {
	for _, row := range objectList(state["invoices"]) {
		if !dateInRange(stringValue(row["date"]), startDate, endDate) {
			continue
		}
		result.OutputCount++
		result.OutputWeight += numberValue(first(row, "weight", "quantity"))
		result.OutputMeters += numberValue(first(row, "meter", "meters", "metr"))
		result.OutputTotal += numberValue(first(row, "total", "amount"))
	}
	for _, row := range objectList(state["incomingInvoices"]) {
		if !dateInRange(stringValue(row["date"]), startDate, endDate) {
			continue
		}
		result.InputCount++
		result.InputWeight += numberValue(first(row, "quantity", "weight"))
		result.InputTotal += numberValue(first(row, "amount", "total"))
	}
	for _, row := range objectList(state["yarnOutInvoices"]) {
		if !dateInRange(stringValue(row["date"]), startDate, endDate) {
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

func countDaysWithActivity(state map[string]any, startDate, endDate string) int {
	dates := make(map[string]struct{})
	for _, key := range []string{"invoices", "incomingInvoices", "yarnOutInvoices"} {
		for _, row := range objectList(state[key]) {
			date := stringValue(row["date"])
			if dateInRange(date, startDate, endDate) {
				dates[date] = struct{}{}
			}
		}
	}
	return len(dates)
}

func dateInRange(value, startDate, endDate string) bool {
	return len(value) >= 10 && value[:10] >= startDate && value[:10] <= endDate
}

func reportPeriod(local time.Time, reportType string) (time.Time, time.Time) {
	end := local
	switch reportType {
	case "weekly":
		return end.AddDate(0, 0, -6), end
	case "monthly":
		firstCurrentMonth := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location())
		previousEnd := firstCurrentMonth.AddDate(0, 0, -1)
		return time.Date(previousEnd.Year(), previousEnd.Month(), 1, 0, 0, 0, 0, local.Location()), previousEnd
	default:
		return end, end
	}
}

func reportSummary(reportType string) string {
	switch reportType {
	case "weekly":
		return "گزارش هفتگی نساجی"
	case "monthly":
		return "گزارش ماهانه نساجی"
	default:
		return "گزارش روزانه نساجی"
	}
}

func formatTextileReport(r reportSnapshot, reportType string) string {
	if r.OperationalData {
		return formatOperationalTextileReport(r, reportType)
	}
	title := "📊 گزارش روزانه نساجی"
	periodLabel := r.PeriodEnd
	switch reportType {
	case "test":
		title = "🧪 گزارش آزمایشی نساجی"
	case "weekly":
		title = "📈 گزارش هفتگی نساجی"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	case "monthly":
		title = "📅 گزارش ماهانه نساجی"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	}
	periodDays := 1
	if start, err := time.Parse("2006-01-02", r.PeriodStart); err == nil {
		if end, err := time.Parse("2006-01-02", r.PeriodEnd); err == nil {
			periodDays = int(end.Sub(start).Hours()/24) + 1
		}
	}
	averageOutput := r.OutputWeight / float64(maxInt(periodDays, 1))
	scrapRate := 0.0
	if total := r.OutputWeight + r.ScrapWeight; total > 0 {
		scrapRate = r.ScrapWeight * 100 / total
	}
	base := fmt.Sprintf(
		"%s\n🏢 %s\n📅 %s\n\n"+
			"🏭 تولید و خروج\n• تعداد ثبت: %s\n• وزن: %s کیلو\n• متراژ: %s متر\n• مبلغ خروج: %s تومان\n"+
			"• ضایعات ثبت‌شده: %s کیلو\n• نرخ ضایعات: %s درصد\n• میانگین تولید روزانه: %s کیلو\n• روزهای دارای فعالیت: %s\n\n"+
			"📥 ورود کالا و نخ\n• تعداد فاکتور: %s\n• وزن/مقدار: %s\n• مبلغ: %s تومان\n\n"+
			"📤 خروج نخ\n• تعداد: %s\n• وزن: %s کیلو\n\n"+
			"📦 موجودی فعلی\n• مقدار خالص: %s\n• ارزش: %s تومان",
		title, fallback(r.Company, "نساجی"), periodLabel,
		formatNumber(float64(r.OutputCount)), formatNumber(r.OutputWeight), formatNumber(r.OutputMeters), formatNumber(r.OutputTotal),
		formatNumber(r.ScrapWeight), formatNumber(scrapRate), formatNumber(averageOutput), formatNumber(float64(r.ActiveDays)),
		formatNumber(float64(r.InputCount)), formatNumber(r.InputWeight), formatNumber(r.InputTotal),
		formatNumber(float64(r.YarnOutCount)), formatNumber(r.YarnOutWeight),
		formatNumber(r.InventoryQty), formatNumber(r.InventoryValue),
	)
	return base
}

func formatOperationalTextileReport(r reportSnapshot, reportType string) string {
	title := "📊 گزارش روزانه تولید و عملیات"
	periodLabel := r.PeriodEnd
	switch reportType {
	case "test":
		title = "🧪 گزارش آزمایشی ۳۰ روز اخیر تولید و عملیات"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	case "weekly":
		title = "📈 گزارش هفتگی تولید و عملیات"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	case "monthly":
		title = "🗓 گزارش ماهانه تولید و عملیات"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	}
	averageWeight := 0.0
	averageMeters := 0.0
	if r.ActiveDays > 0 {
		averageWeight = r.ProductionWeight / float64(r.ActiveDays)
		averageMeters = r.ProductionMeters / float64(r.ActiveDays)
	}
	wasteRate := 0.0
	if total := r.ProductionWeight + r.ScrapWeight; total > 0 {
		wasteRate = r.ScrapWeight * 100 / total
	}
	yarnStockLabel := "موجودی محاسبه‌شده نخ"
	if r.YarnStockWeight < 0 {
		yarnStockLabel = "کسری محاسبه‌شده نخ"
	}
	return fmt.Sprintf(
		"%s\n🏢 %s\n📅 %s\n\n"+
			"🏭 تولید پارچه\n"+
			"• تعداد طاقه تولیدشده: %s\n"+
			"• متراژ تولید: %s متر\n"+
			"• وزن تولید: %s کیلو\n"+
			"• روزهای دارای تولید: %s\n"+
			"• میانگین روز فعال: %s متر و %s کیلو\n\n"+
			"📥 ورود مواد و چله\n"+
			"• ورود نخ: %s ثبت، %s کیلو\n"+
			"• ورود چله: %s ثبت، %s کیلو\n\n"+
			"📤 خروج کالا و نخ\n"+
			"• فاکتور خروج پارچه: %s\n"+
			"• طاقه خروجی: %s\n"+
			"• متراژ خروجی: %s متر\n"+
			"• وزن خروجی: %s کیلو\n"+
			"• خروج نخ: %s ثبت، %s کیلو\n\n"+
			"📦 موجودی فعلی\n"+
			"• پارچه آماده: %s طاقه، %s متر، %s کیلو\n"+
			"• %s: %s کیلو\n\n"+
			"♻️ ضایعات تولید\n"+
			"• وزن ضایعات: %s کیلو\n"+
			"• نرخ ضایعات: %s درصد",
		title,
		fallback(r.Company, "نساجی"),
		periodLabel,
		formatNumber(float64(r.ProductionCount)),
		formatNumber(r.ProductionMeters),
		formatNumber(r.ProductionWeight),
		formatNumber(float64(r.ActiveDays)),
		formatNumber(averageMeters),
		formatNumber(averageWeight),
		formatNumber(float64(r.InputCount)),
		formatNumber(r.InputWeight),
		formatNumber(float64(r.BeamInputCount)),
		formatNumber(r.BeamInputWeight),
		formatNumber(float64(r.FabricOutInvoices)),
		formatNumber(float64(r.FabricOutPieces)),
		formatNumber(r.FabricOutMeters),
		formatNumber(r.FabricOutWeight),
		formatNumber(float64(r.YarnOutCount)),
		formatNumber(r.YarnOutWeight),
		formatNumber(float64(r.FabricStockPieces)),
		formatNumber(r.FabricStockMeters),
		formatNumber(r.FabricStockWeight),
		yarnStockLabel,
		formatNumber(absFloat(r.YarnStockWeight)),
		formatNumber(r.ScrapWeight),
		formatNumber(wasteRate),
	)
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func formatAccountingReport(r reportSnapshot, reportType string) string {
	title := "🧾 گزارش روزانه عملکرد حسابداری"
	periodLabel := r.PeriodEnd
	switch reportType {
	case "test":
		title = "🧪 گزارش آزمایشی ۳۰ روز اخیر عملکرد حسابداری"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	case "weekly":
		title = "🧾 گزارش هفتگی عملکرد حسابداری"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	case "monthly":
		title = "🧾 گزارش ماهانه عملکرد حسابداری"
		periodLabel = r.PeriodStart + " تا " + r.PeriodEnd
	}
	return fmt.Sprintf(
		"%s\n🏢 %s\n📅 %s%s",
		title,
		fallback(r.Company, "نساجی"),
		periodLabel,
		formatAccountingPerformance(r.Accounting),
	)
}

func formatAccountingOverdueAlert(r reportSnapshot) string {
	performance := r.Accounting
	return fmt.Sprintf(
		"🚨 هشدار تأخیر حسابداری\n🏢 %s\n📅 %s\n\n"+
			"• اسناد گذشته از مهلت %s روزه: %s\n"+
			"• کل اسناد در انتظار: %s\n"+
			"• قدیمی‌ترین انتظار: %s روز\n\n"+
			"لطفاً اسناد معطل بررسی و تعیین‌تکلیف شوند.",
		fallback(r.Company, "نساجی"),
		r.PeriodEnd,
		formatNumber(float64(maxInt(performance.SLADays, 2))),
		formatNumber(float64(performance.Overdue)),
		formatNumber(float64(performance.Pending)),
		formatNumber(float64(performance.OldestPending)),
	)
}

func formatAccountingPerformance(performance accountingPerformance) string {
	slaDays := maxInt(performance.SLADays, 2)
	onTimeRate := 0.0
	if performance.Measurable > 0 {
		onTimeRate = float64(performance.OnTime) * 100 / float64(performance.Measurable)
	}
	lines := []string{
		"",
		"",
		fmt.Sprintf("• اسناد تعیین‌تکلیف‌شده در دوره: %s", formatNumber(float64(performance.Processed))),
		fmt.Sprintf("• میانگین زمان رسیدگی: %s روز", formatDecimal(performance.AverageDelay)),
		fmt.Sprintf("• رسیدگی در مهلت %s روزه: %s درصد", formatNumber(float64(slaDays)), formatDecimal(onTimeRate)),
		fmt.Sprintf("• بیشترین تأخیر: %s روز", formatNumber(float64(performance.MaxDelay))),
		fmt.Sprintf("• اسناد در انتظار تعیین‌تکلیف: %s", formatNumber(float64(performance.Pending))),
		fmt.Sprintf("• اسناد گذشته از مهلت: %s", formatNumber(float64(performance.Overdue))),
		fmt.Sprintf("• قدیمی‌ترین انتظار: %s روز", formatNumber(float64(performance.OldestPending))),
	}
	if len(performance.ByUser) > 0 {
		lines = append(lines, "", "👤 عملکرد تفکیکی حسابداران")
		for _, row := range performance.ByUser {
			name := fallback(row.Username, fmt.Sprintf("کاربر شماره %d", row.UserID))
			lines = append(lines, fmt.Sprintf(
				"• %s: %s سند، میانگین %s روز، %s درصد در مهلت",
				name,
				formatNumber(float64(row.Processed)),
				formatDecimal(row.AverageDelay),
				formatDecimal(row.OnTimeRate),
			))
		}
	}
	if performance.InvalidDateRows > 0 {
		lines = append(lines, fmt.Sprintf(
			"⚠️ %s سند به‌دلیل تاریخ ناقص در محاسبه زمان لحاظ نشد.",
			formatNumber(float64(performance.InvalidDateRows)),
		))
	}
	return strings.Join(lines, "\n")
}

func (s *Service) collectAccountingPerformance(
	ctx context.Context,
	companyID int64,
	state map[string]any,
	start, end time.Time,
	slaDays int,
) accountingPerformance {
	if slaDays < 1 {
		slaDays = 2
	}
	operationalItems, err := s.loadOperationalAccountingItems(ctx, companyID)
	if err != nil {
		log.Printf("telegram accounting performance source query failed for company %d: %v", companyID, err)
		return accountingPerformance{SLADays: slaDays}
	}
	financialItems := financialAccountingItems(state)
	performance := calculateAccountingPerformance(operationalItems, financialItems, start, end, slaDays)
	usernames := s.accountingUsernames(ctx, companyID, performance.ByUser)
	for index := range performance.ByUser {
		performance.ByUser[index].Username = usernames[performance.ByUser[index].UserID]
	}
	return performance
}

func calculateAccountingPerformance(
	operationalItems []operationalAccountingItem,
	financialItems []financialAccountingItem,
	start, end time.Time,
	slaDays int,
) accountingPerformance {
	if slaDays < 1 {
		slaDays = 2
	}
	settled := make(map[string]financialAccountingItem, len(financialItems))
	for _, item := range financialItems {
		settled[item.Key] = item
	}

	performance := accountingPerformance{SLADays: slaDays}
	totalDelay := 0
	users := make(map[int64]*accountingUserTotals)

	for _, source := range operationalItems {
		financial, isSettled := settled[source.Key]
		sourceDate, sourceDateOK := parseAccountingDate(source.SourceDate)
		if !isSettled {
			if sourceDateOK && sourceDate.After(dayEnd(end)) {
				continue
			}
			performance.Pending++
			if !sourceDateOK {
				performance.InvalidDateRows++
				continue
			}
			age := calendarDays(sourceDate, end)
			if age < 0 {
				age = 0
			}
			if age > performance.OldestPending {
				performance.OldestPending = age
			}
			if age > slaDays {
				performance.Overdue++
			}
			continue
		}

		processedDate, processedOK := accountingProcessedDate(financial)
		processedDate = processedDate.In(end.Location())
		if !processedOK || processedDate.Before(dayStart(start)) || processedDate.After(dayEnd(end)) {
			continue
		}
		performance.Processed++
		var totals *accountingUserTotals
		if financial.ProcessedBy > 0 {
			totals = users[financial.ProcessedBy]
			if totals == nil {
				totals = &accountingUserTotals{}
				users[financial.ProcessedBy] = totals
			}
			totals.processed++
		}
		if !sourceDateOK {
			performance.InvalidDateRows++
			continue
		}
		delay := calendarDays(sourceDate, processedDate)
		if delay < 0 {
			delay = 0
		}
		performance.Measurable++
		totalDelay += delay
		if delay <= slaDays {
			performance.OnTime++
		}
		if delay > performance.MaxDelay {
			performance.MaxDelay = delay
		}
		if totals != nil {
			totals.measurable++
			totals.delay += delay
			if delay <= slaDays {
				totals.onTime++
			}
		}
	}
	if performance.Measurable > 0 {
		performance.AverageDelay = float64(totalDelay) / float64(performance.Measurable)
	}
	for userID, totals := range users {
		row := accountingUserPerformance{
			UserID:    userID,
			Processed: totals.processed,
		}
		if totals.measurable > 0 {
			row.AverageDelay = float64(totals.delay) / float64(totals.measurable)
			row.OnTimeRate = float64(totals.onTime) * 100 / float64(totals.measurable)
		}
		performance.ByUser = append(performance.ByUser, row)
	}
	sort.Slice(performance.ByUser, func(left, right int) bool {
		return performance.ByUser[left].Processed > performance.ByUser[right].Processed
	})
	if len(performance.ByUser) > 5 {
		performance.ByUser = performance.ByUser[:5]
	}
	return performance
}

func (s *Service) loadOperationalAccountingItems(ctx context.Context, companyID int64) ([]operationalAccountingItem, error) {
	var schemaName string
	if err := s.db.QueryRowContext(ctx, `
		SELECT schema_name
		FROM public.operational_tenants
		WHERE external_company_id=$1 AND active=1
		ORDER BY id DESC
		LIMIT 1
	`, companyID).Scan(&schemaName); err != nil {
		return nil, err
	}
	schema := pq.QuoteIdentifier(strings.TrimSpace(schemaName))
	query := fmt.Sprintf(`
		SELECT source_type, source_id, source_date
		FROM (
			SELECT 'operational_out_invoice'::text AS source_type,
			       TRIM(COALESCE(shom_f_khor,'')) AS source_id,
			       MIN(COALESCE(tarikh_f_khor,'')) AS source_date
			FROM %[1]s.f_khor
			WHERE TRIM(COALESCE(shom_f_khor,'')) <> ''
			GROUP BY TRIM(COALESCE(shom_f_khor,''))
			UNION ALL
			SELECT 'operational_yarn_in', id_nakh_vor::text, COALESCE(tarikh_nakh_vor,'')
			FROM %[1]s.nakh_vor
			UNION ALL
			SELECT 'operational_yarn_out', id_nakh_khor::text, COALESCE(tarikh_nakh_khor,'')
			FROM %[1]s.nakh_khor
			UNION ALL
			SELECT 'operational_chelle_in', id_chelle::text, COALESCE(tarikh_chelle,'')
			FROM %[1]s.chelle
			UNION ALL
			SELECT 'operational_expense', id_h_rozmare::text, COALESCE(tarikh_h_rozmare,'')
			FROM %[1]s.h_rozmare
			UNION ALL
			SELECT 'operational_spare_part', id_spare_inventory::text,
			       COALESCE(NULLIF(updated_at,''), created_at, '')
			FROM %[1]s.spare_parts_inventory
		) sources
	`, schema)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]operationalAccountingItem, 0)
	for rows.Next() {
		var sourceType, sourceID, sourceDate string
		if err := rows.Scan(&sourceType, &sourceID, &sourceDate); err != nil {
			return nil, err
		}
		result = append(result, operationalAccountingItem{
			Key:        sourceType + ":" + strings.TrimSpace(sourceID),
			SourceType: sourceType,
			SourceDate: sourceDate,
		})
	}
	return result, rows.Err()
}

func (s *Service) accountingUsernames(
	ctx context.Context,
	companyID int64,
	users []accountingUserPerformance,
) map[int64]string {
	result := make(map[int64]string)
	if len(users) == 0 {
		return result
	}
	ids := make([]int64, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.UserID)
	}
	_, err := postgres.WithCompanySession(ctx, s.db, companyID, func(q postgres.SessionQueryable) (struct{}, error) {
		rows, err := q.QueryContext(ctx, `
			SELECT id, username
			FROM financial_users
			WHERE company_id=$1 AND id=ANY($2)
		`, companyID, pq.Array(ids))
		if err != nil {
			return struct{}{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var username string
			if err := rows.Scan(&id, &username); err != nil {
				return struct{}{}, err
			}
			result[id] = strings.TrimSpace(username)
		}
		return struct{}{}, rows.Err()
	})
	if err != nil {
		log.Printf("telegram accounting usernames query failed for company %d: %v", companyID, err)
	}
	return result
}

func financialAccountingItems(state map[string]any) []financialAccountingItem {
	result := make([]financialAccountingItem, 0)
	for _, row := range objectList(state["invoices"]) {
		number := strings.TrimSpace(stringValue(row["number"]))
		operationalID := strings.TrimSpace(stringValue(row["operationalId"]))
		if number == "" || (operationalID == "" && strings.TrimSpace(stringValue(row["operationalDate"])) == "") {
			continue
		}
		result = append(result, financialAccountingItem{
			Key:           "operational_out_invoice:" + number,
			FinancialDate: stringValue(row["date"]),
			ProcessedAt:   stringValue(row["_accountingProcessedAt"]),
			ProcessedBy:   int64(numberValue(row["_accountingProcessedBy"])),
		})
	}
	for _, field := range []string{"incomingInvoices", "yarnOutInvoices", "expenses"} {
		for _, row := range objectList(state[field]) {
			sourceType := strings.TrimSpace(stringValue(row["source_type"]))
			sourceID := strings.TrimSpace(stringValue(row["sourceId"]))
			if !strings.HasPrefix(sourceType, "operational_") || sourceID == "" {
				continue
			}
			result = append(result, financialAccountingItem{
				Key:           sourceType + ":" + sourceID,
				FinancialDate: stringValue(row["date"]),
				ProcessedAt:   stringValue(row["_accountingProcessedAt"]),
				ProcessedBy:   int64(numberValue(row["_accountingProcessedBy"])),
			})
		}
	}
	return result
}

func accountingProcessedDate(item financialAccountingItem) (time.Time, bool) {
	if value := strings.TrimSpace(item.ProcessedAt); value != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed, true
		}
	}
	return parseAccountingDate(item.FinancialDate)
}

func parseAccountingDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(strings.NewReplacer(
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
		"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
		"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
		"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
	).Replace(value))
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return parsed, true
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '-' || r == '.'
	})
	if len(parts) != 3 {
		return time.Time{}, false
	}
	year, errYear := strconv.Atoi(parts[0])
	month, errMonth := strconv.Atoi(parts[1])
	day, errDay := strconv.Atoi(parts[2])
	if errYear != nil || errMonth != nil || errDay != nil ||
		month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, false
	}
	if year >= 1700 {
		parsed, err := time.Parse("2006-1-2", fmt.Sprintf("%d-%d-%d", year, month, day))
		return parsed, err == nil
	}
	gy, gm, gd := accountingJalaliToGregorian(year, month, day)
	return time.Date(gy, time.Month(gm), gd, 0, 0, 0, 0, time.UTC), true
}

func accountingJalaliToGregorian(jy, jm, jd int) (int, int, int) {
	jy += 1595
	days := -355668 + (365 * jy) + ((jy / 33) * 8) + (((jy % 33) + 3) / 4) + jd
	if jm < 7 {
		days += (jm - 1) * 31
	} else {
		days += ((jm - 7) * 30) + 186
	}
	gy := 400 * (days / 146097)
	days %= 146097
	if days > 36524 {
		days--
		gy += 100 * (days / 36524)
		days %= 36524
		if days >= 365 {
			days++
		}
	}
	gy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		gy += (days - 1) / 365
		days = (days - 1) % 365
	}
	gd := days + 1
	monthDays := []int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if (gy%4 == 0 && gy%100 != 0) || gy%400 == 0 {
		monthDays[2] = 29
	}
	gm := 1
	for gm <= 12 && gd > monthDays[gm] {
		gd -= monthDays[gm]
		gm++
	}
	return gy, gm, gd
}

func calendarDays(start, end time.Time) int {
	location := end.Location()
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	endDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, location)
	return int(endDay.Sub(startDay).Hours() / 24)
}

func dayStart(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func dayEnd(value time.Time) time.Time {
	return dayStart(value).Add(24*time.Hour - time.Nanosecond)
}

func (s *Service) sendMessage(ctx context.Context, chatID, text string) (int64, error) {
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}
	endpoint, err := s.telegramEndpoint("sendMessage", nil)
	if err != nil {
		return 0, err
	}
	err = s.call(ctx, http.MethodPost, "sendMessage", nil, endpoint, map[string]any{
		"chat_id": chatID, "text": text, "disable_web_page_preview": true,
	}, &response)
	if err != nil {
		return 0, err
	}
	if !response.OK {
		return 0, errors.New(s.redactToken(fallback(response.Description, "ارسال تلگرام ناموفق بود")))
	}
	return response.Result.MessageID, nil
}

func (s *Service) call(ctx context.Context, method, telegramMethod string, query url.Values, endpoint string, body any, out any) error {
	requestMethod := method
	requestBody := body
	if s.relayMode() == "apps_script" {
		requestMethod = http.MethodPost
		requestBody = map[string]any{
			"relayToken": s.cfg.RelayToken,
			"botToken":   s.cfg.BotToken,
			"method":     telegramMethod,
			"httpMethod": method,
			"query":      query,
			"body":       body,
		}
	}
	var reader io.Reader
	if requestBody != nil {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, requestMethod, endpoint, reader)
	if err != nil {
		return errors.New(s.redactToken(err.Error()))
	}
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(s.cfg.RelayURL) != "" && s.relayMode() != "apps_script" {
		request.Header.Set("Authorization", "Bearer "+s.cfg.RelayToken)
		request.Header.Set("X-Telegram-Bot-Token", s.cfg.BotToken)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return errors.New(s.redactToken(err.Error()))
	}
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
		return errors.New(s.redactToken(err.Error()))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("telegram http %d", response.StatusCode)
	}
	return nil
}

func (s *Service) telegramEndpoint(method string, query url.Values) (string, error) {
	method = strings.TrimSpace(method)
	if method == "" || strings.ContainsAny(method, "/?#") {
		return "", errors.New("invalid telegram method")
	}

	relayURL := strings.TrimSpace(s.cfg.RelayURL)
	if relayURL == "" {
		endpoint := fmt.Sprintf("%s/bot%s/%s", strings.TrimRight(s.cfg.APIBase, "/"), s.cfg.BotToken, method)
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}
		return endpoint, nil
	}
	if strings.TrimSpace(s.cfg.RelayToken) == "" {
		return "", errors.New("telegram relay token is not configured")
	}
	parsed, err := url.Parse(relayURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("telegram relay URL must be a clean HTTPS address")
	}
	if s.relayMode() == "apps_script" {
		return parsed.String(), nil
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + method
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func (s *Service) relayMode() string {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.RelayMode))
	if mode != "" {
		return mode
	}
	parsed, err := url.Parse(strings.TrimSpace(s.cfg.RelayURL))
	if err == nil {
		host := strings.ToLower(parsed.Hostname())
		if host == "script.google.com" || host == "script.googleusercontent.com" {
			return "apps_script"
		}
	}
	return "standard"
}

func (s *Service) redactToken(value string) string {
	if s == nil || s.cfg.BotToken == "" {
		return value
	}
	return strings.ReplaceAll(value, s.cfg.BotToken, "[redacted]")
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

func formatDecimal(value float64) string {
	if value == float64(int64(value)) {
		return formatNumber(value)
	}
	return strconv.FormatFloat(value, 'f', 1, 64)
}

func fallback(value, replacement string) string {
	if strings.TrimSpace(value) == "" {
		return replacement
	}
	return value
}
