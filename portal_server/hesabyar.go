package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

//go:embed hesabyar_pwa/*
var hesabyarAssets embed.FS

const hesabyarPath = "/executive/hesabyar/"
const hesabyarConsentCookie = "__Secure-hesabyar-consent"
const hesabyarGrantTTL = 10 * time.Minute

type hesabyarCommand struct {
	key, label, endpoint string
	aliases              []string
}

var hesabyarCommands = []hesabyarCommand{
	{"customer", "جستجوی مشتری", "/api/v1/hesabyar/parties", []string{"/customer", "جستجوی مشتری", "مشتری"}},
	{"fabric", "جستجوی پارچه", "/api/operational/kala-items", []string{"/fabric", "جستجوی پارچه", "پارچه"}},
	{"yarn", "جستجوی نخ", "/api/operational/yarn-items", []string{"/yarn", "جستجوی نخ", "نخ"}},
	{"stock", "موجودی کد کالا", "/api/v1/hesabyar/stock", []string{"/stock", "موجودی کد کالا", "موجودی کد", "موجودی"}},
	{"ledger", "خلاصه صورت‌حساب مشتری", "/api/v1/hesabyar/ledger", []string{"/statement", "خلاصه صورت حساب مشتری", "صورت حساب مشتری", "صورتحساب مشتری", "مانده مشتری"}},
}

var hesabyarAssetTypes = map[string]string{
	"index.html": "text/html; charset=utf-8", "app.js": "application/javascript; charset=utf-8",
	"sw.js": "application/javascript; charset=utf-8", "style.css": "text/css; charset=utf-8",
	"manifest.webmanifest": "application/manifest+json", "icon-192.png": "image/png", "icon-512.png": "image/png",
}

func (a *portalApp) hesabyarPWA(w http.ResponseWriter, r *http.Request) {
	base := env("HESABYAR_PWA_ORIGIN", "")
	pilotAccounts := strings.FieldsFunc(env("HESABYAR_PWA_ACCOUNTS", ""), func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	origin, err := url.Parse(base)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil ||
		origin.Host != r.Host || base != "https://"+origin.Host || len(pilotAccounts) == 0 || len(a.financialJWTKey) < 32 || len(a.sessionSecret) < 32 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; manifest-src 'self'; worker-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	key := strings.TrimPrefix(r.URL.Path, hesabyarPath)
	if key == "" {
		key = "index.html"
	}
	if contentType, ok := hesabyarAssetTypes[key]; ok {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, err := hesabyarAssets.ReadFile("hesabyar_pwa/" + key)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
		return
	}
	if r.URL.RawQuery != "" || (key != "api/config" && key != "api/grant" && key != "api/disconnect" && key != "api/command") {
		http.NotFound(w, r)
		return
	}
	if key != "api/config" {
		origins := r.Header.Values("Origin")
		if r.Method != http.MethodPost || len(origins) != 1 || origins[0] != base ||
			(r.Header.Get("Sec-Fetch-Site") != "" && r.Header.Get("Sec-Fetch-Site") != "same-origin") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Re-read the official session and account record before every use.
	record, err := a.accessRecordFromRequest(r)
	if err != nil {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"code": "login_required"})
		return
	}
	pilotAccount := fmt.Sprintf("%d:%d", record.FinancialCompanyID, record.ID)
	match := false
	for _, account := range pilotAccounts {
		match = match || account == pilotAccount
	}
	if !match {
		respondJSON(w, http.StatusForbidden, map[string]string{"code": "permission_required"})
		return
	}
	allowed := hesabyarAllowed(record)
	if len(allowed) == 0 {
		respondJSON(w, http.StatusForbidden, map[string]string{"code": "permission_required"})
		return
	}
	now := time.Now()
	granted := a.hesabyarHasConsent(r, record, now)
	if key == "api/config" {
		labels := []string{}
		for _, cmd := range hesabyarCommands {
			if allowed[cmd.key] {
				labels = append(labels, cmd.label)
			}
		}
		if allowed["draft"] {
			labels = append(labels, "پیش‌نمایش فاکتور")
		}
		respondJSON(w, http.StatusOK, map[string]any{"kind": "site", "live": true, "granted": granted, "commands": labels})
		return
	}
	var body map[string]json.RawMessage
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	decoder := json.NewDecoder(r.Body)
	if r.Header.Get("Content-Type") != "application/json" || decoder.Decode(&body) != nil || body == nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if key == "api/grant" || key == "api/disconnect" {
		if len(body) != 0 {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		cookie := &http.Cookie{Name: hesabyarConsentCookie, Path: hesabyarPath, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: -1}
		if key == "api/grant" {
			exp := minTime(record.ExpiresAt, now.Add(hesabyarGrantTTL))
			cookie.Value, cookie.Expires, cookie.MaxAge = a.hesabyarConsent(record, exp.Unix()), exp, int(time.Until(exp).Seconds())
			if cookie.MaxAge < 1 {
				http.Error(w, "session expired", http.StatusUnauthorized)
				return
			}
		}
		http.SetCookie(w, cookie)
		respondJSON(w, http.StatusOK, map[string]bool{"granted": key == "api/grant"})
		return
	}
	if !granted {
		respondJSON(w, http.StatusForbidden, map[string]string{"code": "consent_required"})
		return
	}
	var text string
	if len(body) != 1 || json.Unmarshal(body["text"], &text) != nil || !utf8.ValidString(text) ||
		utf8.RuneCountInString(text) > 240 || strings.ContainsAny(text, "\r\n\x00") {
		http.Error(w, "invalid command", http.StatusBadRequest)
		return
	}
	if draft := hesabyarDraftPattern.FindStringSubmatch(hesabyarText(text)); draft != nil {
		respondJSON(w, http.StatusOK, a.hesabyarInvoicePreview(r, record, allowed, draft, now))
		return
	}
	cmd, query := parseHesabyarCommand(text)
	if cmd == nil {
		message := "یکی از فرمان‌های مجاز و بخشی از نام را بگو؛ مثلاً «جستجوی پارچه متقال»."
		if allowed["draft"] && strings.Contains(text, "فاکتور") {
			message = "مثلاً بگو «پیش‌نمایش فاکتور برای مشتری نام کامل، کد SKU-123، تعداد ۲، قیمت ۲۵۰۰۰۰ ریال». ثبت رسمی در اینجا فعال نیست."
		}
		respondJSON(w, http.StatusOK, map[string]any{"code": "clarify", "text": message})
		return
	}
	if !allowed[cmd.key] {
		respondJSON(w, http.StatusOK, map[string]any{"code": "forbidden", "text": "حسابت برای این فرمان دسترسی ندارد."})
		return
	}
	answer, err := a.hesabyarSearch(r, record, cmd, query, now, false)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]any{"code": "unavailable", "text": "دادهٔ این بخش در Textile ERP اکنون در دسترس نیست."})
		return
	}
	respondJSON(w, http.StatusOK, answer)
}

func hesabyarAllowed(record projectAccess) map[string]bool {
	allowed := map[string]bool{}
	if record.ProjectKey != "textile-erp" || !record.IsActive || record.MustChangePassword || accessRequiresSetup(record) ||
		record.ID <= 0 || record.ID >= 1<<53 || record.FinancialCompanyID <= 0 || record.FinancialCompanyID >= 1<<53 {
		return allowed
	}
	for _, permission := range effectivePermissions(record) {
		if effectiveAllowFinancial(record) {
			switch permission {
			case "initialData":
				allowed["customer"] = true
			case "inventory":
				allowed["stock"] = true
			case "accounting":
				allowed["ledger"] = true
			case "invoices":
				allowed["draft"] = true
			}
		}
	}
	if effectiveAllowOperational(record) {
		for _, key := range operationalMenuKeys(record) {
			if key == "*" || key == "initial" {
				allowed["fabric"], allowed["yarn"] = true, true
			}
		}
	}
	if !allowed["stock"] || !allowed["customer"] {
		delete(allowed, "draft")
	}
	return allowed
}

func (a *portalApp) hesabyarConsent(record projectAccess, exp int64) string {
	mac := hmac.New(sha256.New, []byte(a.sessionSecret))
	_, _ = fmt.Fprintf(mac, "hesabyar-consent-v1\x00%d\x00%d\x00%d\x00%s", record.ID, record.FinancialCompanyID, exp, record.AccessToken)
	return strconv.FormatInt(exp, 10) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *portalApp) hesabyarHasConsent(r *http.Request, record projectAccess, now time.Time) bool {
	cookie, err := r.Cookie(hesabyarConsentCookie)
	if err != nil || len(cookie.Value) > 70 {
		return false
	}
	value, _, ok := strings.Cut(cookie.Value, ".")
	exp, err := strconv.ParseInt(value, 10, 64)
	return ok && err == nil && exp > now.Unix() && exp <= now.Add(hesabyarGrantTTL).Unix() && exp <= record.ExpiresAt.Unix() &&
		hmac.Equal([]byte(cookie.Value), []byte(a.hesabyarConsent(record, exp)))
}

var hesabyarLetters = strings.NewReplacer("ي", "ی", "ى", "ی", "ك", "ک", "\u200c", " ", "\u200d", "", "۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4", "۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9", "٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4", "٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9")

func hesabyarText(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsMark(r) || (unicode.IsControl(r) && !unicode.IsSpace(r)) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return unicode.ToLower(r)
	}, hesabyarLetters.Replace(text))
	return strings.Join(strings.Fields(text), " ")
}

func parseHesabyarCommand(raw string) (*hesabyarCommand, string) {
	text := strings.TrimRight(hesabyarText(raw), "؟?!.، ")
	text = strings.NewReplacer("جست و جوی ", "جستجوی ", "جست وجوی ", "جستجوی ", "جستجو کن ", "جستجوی ").Replace(text)
	for i := range hesabyarCommands {
		command := &hesabyarCommands[i]
		for _, alias := range command.aliases {
			if !strings.HasPrefix(text, alias+" ") {
				continue
			}
			query := strings.TrimSuffix(strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(text, alias), " :")), " را پیدا کن")
			query = strings.TrimSuffix(query, " را بگو")
			if utf8.RuneCountInString(query) > 80 || strings.IndexFunc(query, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) < 0 {
				return nil, ""
			}
			return command, query
		}
	}
	return nil, ""
}

func (a *portalApp) hesabyarServiceToken(record projectAccess, endpoint string, now time.Time) string {
	claims, _ := json.Marshal(map[string]any{"user_id": record.ID, "company_id": record.FinancialCompanyID,
		"role": "hesabyar_lookup", "aud": "hesabyar-portal", "allow_financial": true, "hsbyr_path": endpoint,
		"iat": now.Unix(), "exp": minTime(record.ExpiresAt, now.Add(time.Minute)).Unix()})
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	mac := hmac.New(sha256.New, []byte(a.financialJWTKey))
	_, _ = mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

var hesabyarHTTP = &http.Client{Timeout: 6 * time.Second,
	Transport:     &http.Transport{MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

var hesabyarDecimal = regexp.MustCompile(`^-?[0-9]{1,26}(\.[0-9]{1,4})?$`)
var hesabyarDraftPattern = regexp.MustCompile(`^پیش ?نمایش فاکتور برای مشتری ([^،,]{1,80})[،,] کد ([a-z0-9][a-z0-9_-]{0,19})[،,] تعداد ([0-9]{1,9}(?:[.٫][0-9]{1,3})?)[،,] قیمت ([0-9]{1,12}) ریال$`)

func hesabyarAmount(amount *big.Rat, digits int) string {
	return strings.TrimRight(strings.TrimRight(amount.FloatString(digits), "0"), ".")
}

func hesabyarLabel(text string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '\u200c') {
			return -1
		}
		return r
	}, text)), " ")
}

func (a *portalApp) hesabyarInvoicePreview(r *http.Request, record projectAccess, allowed map[string]bool, fields []string, now time.Time) map[string]any {
	answer := func(code, text string) map[string]any { return map[string]any{"code": code, "text": text} }
	if !allowed["draft"] {
		return answer("forbidden", "حسابت برای این فرمان دسترسی ندارد.")
	}
	quantity := strings.ReplaceAll(fields[3], "٫", ".")
	qty, qtyOK := new(big.Rat).SetString(quantity)
	price, priceOK := new(big.Rat).SetString(fields[4])
	if !qtyOK || !priceOK || qty.Sign() <= 0 || price.Sign() <= 0 {
		return answer("clarify", "تعداد و قیمت باید مثبت باشند.")
	}
	var customer, item, unit string
	for _, command := range hesabyarCommands {
		if command.key != "customer" && command.key != "stock" {
			continue
		}
		query := strings.TrimSpace(fields[1])
		if command.key == "stock" {
			query = fields[2]
		}
		result, err := a.hesabyarSearch(r, record, &command, query, now, true)
		if err != nil {
			return answer("unavailable", "دادهٔ این بخش در Textile ERP اکنون در دسترس نیست.")
		}
		if result["code"] != "found" {
			return answer("clarify", "کد کالا و نام دقیق و یکتای مشتری را بررسی کن.")
		}
		if command.key == "customer" {
			customer, _ = result["name"].(string)
		} else {
			item, _ = result["name"].(string)
			unit, _ = result["unit"].(string)
		}
	}
	if customer == "" || item == "" || unit == "" {
		return answer("clarify", "اطلاعات مشتری، کالا یا واحد کامل نیست؛ پیش‌نمایش ساخته نشد.")
	}
	total := new(big.Rat).Mul(qty, price)
	return map[string]any{"code": "draft_preview", "shareable": true, "text": fmt.Sprintf(
		"پیش‌نمایش فاکتور؛ در ERP ثبت نشده\nمشتری: %s\nکالا: %s (%s)\nمقدار: %s %s\nقیمت هر واحد: %s ریال\nجمع پیش از مالیات: %s ریال",
		customer, item, fields[2], quantity, unit, fields[4], hesabyarAmount(total, 3))}
}

func (a *portalApp) hesabyarSearch(r *http.Request, record projectAccess, cmd *hesabyarCommand, query string, now time.Time, detail bool) (map[string]any, error) {
	upstream, err := url.Parse(strings.TrimRight(a.financialAPIURL, "/"))
	if err != nil || (upstream.Scheme != "http" && upstream.Scheme != "https") || upstream.Host == "" ||
		upstream.User != nil || upstream.Path != "" || upstream.RawQuery != "" || upstream.Fragment != "" {
		return nil, fmt.Errorf("invalid financial service configuration")
	}
	upstream.Path = cmd.endpoint
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.hesabyarServiceToken(record, cmd.endpoint, now))
	req.Header.Set("Accept", "application/json")
	res, err := hesabyarHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	const limit = 2 * 1024 * 1024
	mimeType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if res.StatusCode != http.StatusOK || mimeType != "application/json" {
		return nil, fmt.Errorf("unexpected financial service response")
	}
	payload, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || len(payload) > limit {
		return nil, fmt.Errorf("financial response exceeds limit")
	}
	var catalog struct {
		Success bool `json:"success"`
		Total   *int `json:"total"`
		Rows    []struct {
			ID     int64    `json:"id"`
			Name   string   `json:"name"`
			Status string   `json:"status"`
			Roles  []string `json:"roles"`
			Code   string   `json:"code"`
			Unit   string   `json:"unit"`
			Qty    string   `json:"qty"`
			Debit  string   `json:"debit"`
			Credit string   `json:"credit"`
			Count  int64    `json:"count"`
		} `json:"rows"`
	}
	if !utf8.Valid(payload) || json.Unmarshal(payload, &catalog) != nil ||
		!catalog.Success || catalog.Rows == nil || catalog.Total == nil || *catalog.Total != len(catalog.Rows) {
		return nil, fmt.Errorf("incomplete financial service response")
	}
	names, count, summary, matchUnit := []string{}, 0, "", ""
	seen := map[int64]bool{}
	for _, row := range catalog.Rows {
		if row.ID <= 0 || seen[row.ID] || utf8.RuneCountInString(row.Name) > 256 {
			return nil, fmt.Errorf("invalid catalog item")
		}
		seen[row.ID] = true
		if cmd.key == "customer" && (row.Status != "ACTIVE" || len(row.Roles) != 1 || row.Roles[0] != "CUSTOMER") {
			return nil, fmt.Errorf("invalid customer catalog item")
		}
		label, normalized := hesabyarLabel(row.Name), hesabyarText(row.Name)
		match := strings.Contains(normalized, query)
		if detail {
			match = normalized == query
		}
		switch cmd.key {
		case "stock":
			if row.Code == "" || !hesabyarDecimal.MatchString(row.Qty) {
				return nil, fmt.Errorf("invalid stock record")
			}
			match = hesabyarText(row.Code) == query
			if match {
				matchUnit = hesabyarLabel(row.Unit)
				summary = fmt.Sprintf("موجودی ثبت‌شدهٔ ملکی در انبار مالی برای %s: %s %s", label, row.Qty, hesabyarLabel(row.Unit))
			}
		case "ledger":
			if row.Count <= 0 || !hesabyarDecimal.MatchString(row.Debit) || !hesabyarDecimal.MatchString(row.Credit) {
				return nil, fmt.Errorf("invalid ledger record")
			}
			match = normalized == query
			if match {
				debit, _ := new(big.Rat).SetString(row.Debit)
				credit, _ := new(big.Rat).SetString(row.Credit)
				net := new(big.Rat).Sub(debit, credit)
				remaining := "مانده صفر ریال"
				if net.Sign() > 0 {
					remaining = "مانده بدهکار " + hesabyarAmount(net, 4) + " ریال"
				} else if net.Sign() < 0 {
					remaining = "مانده بستانکار " + hesabyarAmount(net.Abs(net), 4) + " ریال"
				}
				summary = fmt.Sprintf("خلاصهٔ اسناد ثبت‌شدهٔ %s (%d ردیف): بدهکار %s ریال، بستانکار %s ریال، %s.", label, row.Count, row.Debit, row.Credit, remaining)
			}
		}
		if match {
			count++
			if len(names) < 5 {
				names = append(names, label)
			}
		}
	}
	if detail {
		if count != 1 {
			return map[string]any{"code": "ambiguous"}, nil
		}
		return map[string]any{"code": "found", "name": names[0], "unit": matchUnit}, nil
	}
	if count == 0 {
		if cmd.key == "stock" || cmd.key == "ledger" {
			return map[string]any{"code": "not_found", "text": "برای این کد کالا یا نام کامل مشتری، در این بخش رکوردی پیدا نشد."}, nil
		}
		return map[string]any{"code": "not_found", "text": "در اطلاعات شرکتت موردی پیدا نشد."}, nil
	}
	if summary != "" {
		if count != 1 {
			return map[string]any{"code": "ambiguous", "text": "بیش از یک رکورد با این نام یا کد وجود دارد؛ نتیجه مالی اعلام نشد."}, nil
		}
		return map[string]any{"code": "found", "text": summary, "shareable": true}, nil
	}
	return map[string]any{"code": "found", "text": fmt.Sprintf("تعداد نتیجه: %d\n%s", count, strings.Join(names, "\n")), "shareable": true}, nil
}
