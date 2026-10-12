// Package supervisoragent implements the finance supervisor's standing
// assistant: it reads the supervisor findings queue, classifies every finding
// into an action (monitor / needs-human / escalate), and produces a Persian
// action plan. It never mutates ledger data: execution stays with humans.
//
// Meta-agent contract (۰ اصل متا-ایجنت):
//  1. هدف واحد: صف ناظر بدون مورد کاذب، موارد واقعی مستند و ارجاع‌شده.
//  2. مشاهده پیش از عمل: هر اجرا فقط گزارش ناظر را می‌خواند.
//  3. جدایی قضاوت از اجرا: LLM فقط برچسب می‌دهد؛ اجرا انسانی است.
//  4. مرز ایمنی: هیچ مسیر نوشتن به دفاتر وجود ندارد.
//  5. شواهد و ردیابی: هر اجرا با hash ورودی در financial_agent_runs ثبت می‌شود.
//  6. کاهش‌یافتگی مطمئن: بدون LLM هم طبقه‌بندی قاعده‌محور خروجی می‌دهد.
//  7. سقف هزینه: تعداد فراخوانی LLM در ماه محدود می‌شود.
//  8. idempotency: ورود تکراری همان hash نتیجه کش‌شده برمی‌گرداند.
//  9. تست‌پذیری: منطق طبقه‌بندی خالص و بدون شبکه تست می‌شود.
//
// 10. شفافیت: خروجی فارسی با دلیل برای هر مورد.
package supervisoragent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Action is the decision the agent reached for one finding.
type Action string

const (
	// Monitor: the finding matches a known noise pattern; document it and
	// keep watching. No human work is requested.
	Monitor Action = "MONITOR"
	// NeedsHuman: a real integrity question a person must resolve.
	NeedsHuman Action = "NEEDS_HUMAN"
	// Escalate: suspected money impact or repeated recurrence; treat first.
	Escalate Action = "ESCALATE"
)

// Finding is the provider-neutral view of one supervisor finding.
type Finding struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	Page      string `json:"page"`
	Reference string `json:"reference"`
	Evidence  string `json:"evidence,omitempty"`
}

// TriageItem is one classified finding.
type TriageItem struct {
	Finding
	Action      Action `json:"action"`
	Explanation string `json:"explanation"`
	Advice      string `json:"advice"`
	By          string `json:"by"` // "rule" or "llm"
}

// RunResult is the full agent output for one sweep.
type RunResult struct {
	RunID     string         `json:"runId"`
	CompanyID int64          `json:"companyId"`
	CreatedAt time.Time      `json:"createdAt"`
	InputHash string         `json:"inputHash"`
	Total     int            `json:"total"`
	Counts    map[Action]int `json:"counts"`
	Items     []TriageItem   `json:"items"`
	Summary   string         `json:"summary"`
	Model     string         `json:"model,omitempty"`
	LLMUsed   bool           `json:"llmUsed"`
	Cached    bool           `json:"cached"`
}

// Classifier decides the action for findings the rule book does not cover.
// Implementations may call an LLM; they must degrade to NeedsHuman on failure.
type Classifier interface {
	Classify(ctx context.Context, f Finding) (Action, string, string, bool)
	Name() string
}

// Config controls the agent behaviour.
type Config struct {
	// RuleOnly disables the LLM classifier entirely (principle 6).
	RuleOnly bool
	// MonthlyLLMBudget caps classifier calls per calendar month (principle 7).
	MonthlyLLMBudget int
	// UsedThisMonth reports how many LLM calls were already spent.
	UsedThisMonth func(ctx context.Context, now time.Time) int
}

// Agent is the stateless triage engine; persistence lives with the caller.
type Agent struct {
	cfg        Config
	classifier Classifier
	now        func() time.Time
}

func New(cfg Config, classifier Classifier) *Agent {
	if cfg.MonthlyLLMBudget <= 0 {
		cfg.MonthlyLLMBudget = 100
	}
	return &Agent{cfg: cfg, classifier: classifier, now: func() time.Time { return time.Now().UTC() }}
}

// knownNoise lists finding codes whose modern detection rules already filter
// genuine data problems; a remaining hit is informational only.
var knownNoise = map[string]string{
	"payment-verification": "هزینه عملیاتیِ کامل دیگر نیازمند تأیید دوباره نیست؛ این مورد فقط برای نظارت دوره‌ای ثبت شده است.",
	"stock-effect":         "مغایرت برچسب نام مشتری با مقایسه تطبیقی پذیرفته می‌شود؛ فقط تفاوت واقعی مقدار/کالا معنادار است.",
	"check-party":          "تطبیق طرف حساب چک اکنون برچسب کوتاه و کامل را معادل می‌داند.",
	"cash-party":           "تطبیق طرف حساب نقدی اکنون برچسب کوتاه و کامل را معادل می‌داند.",
	"mobile-party":         "تطبیق طرف حساب حسابیار اکنون برچسب کوتاه و کامل را معادل می‌داند.",
	"mobile-bank":          "نگاشت نام بانک فقط هشدار نگاشت است و اثر مالی ندارد.",
	"party-unconfirmed":    "تأیید طرف حساب در فرم بانک/صندوق انجام می‌شود؛ لزوماً مغایرت نیست.",
	"doc-date":             "تاریخ غیرقابل‌تبدیل مانع ثبت می‌شود اما داده را خراب نمی‌کند.",
}

// realImpact lists codes that always deserve a human look, with guidance.
var realImpact = map[string]string{
	"duplicate":                    "شناسه تکراری می‌تواند اثر مالی دوباره بسازد؛ ردیف اضافی را حذف یا ادغام کنید.",
	"duplicate-source":             "یک رویداد مبدأ چندبار ثبت شده؛ سینک عملیاتی را بررسی و ردیف اضافی را حذف کنید.",
	"account":                      "حساب پرداخت معتبر نیست؛ حساب را در فرم مربوط اصلاح کنید.",
	"expense-link":                 "هزینه بدون گردش بانکی یا با چند گردش است؛ گردش متناظر را بسازید یا اضافی را حذف کنید.",
	"stock-link":                   "فاکتور ورود باید دقیقاً یک ردیف موجودی داشته باشد؛ ردیف گم‌شده یا اضافی را اصلاح کنید.",
	"cash-links":                   "تسویه نقدی فاکتور با گردش‌ها نمی‌خواند؛ پرداخت‌های نقدی را بازبینی کنید.",
	"check-links":                  "چک‌های ثبت‌شده با تسویه فاکتور نمی‌خوانند؛ ردیف‌های چک را تطبیق دهید.",
	"consignment":                  "فاکتور امانی نباید تسویه بانکی داشته باشد؛ ماهیت فاکتور را بررسی کنید.",
	"transfer":                     "انتقال بانکی با مقصد نامعتبر یا برابر مبدأ ثبت شده؛ گردش انتقال را اصلاح کنید.",
	"movement-value":               "مبلغ یا جهت گردش نامعتبر است؛ سند را اصلاح یا باطل کنید.",
	"party-direction":              "جهت گردش با ماهیت دریافت/پرداخت نمی‌خواند؛ نوع تراکنش را اصلاح کنید.",
	"orphan-sourceExpense":         "گردش بدون سند هزینه مبدأ مانده؛ سند را بازسازی یا گردش را حذف کنید.",
	"orphan-sourceIncomingInvoice": "گردش/چک/موجودی بدون فاکتور ورود مبدأ مانده؛ سند را بازسازی کنید.",
	"orphan-sourceInvoice":         "گردش بدون فاکتور فروش مبدأ مانده؛ سند را بازسازی کنید.",
	"orphan":                       "اثر موجودی یا چک بدون فاکتور مبدأ است؛ سند مبدأ را بازسازی کنید.",
	"mobile-link":                  "تراکنش حسابیار گردش مالی متناظر ندارد؛ همگام‌سازی را دوباره اجرا کنید.",
	"mobile-core":                  "مبلغ/جهت/تاریخ گردش با حسابیار نمی‌خواند؛ یکی از دو سمت را اصلاح کنید.",
	"mobile-classification":        "نوع گردش با نوع ثبت‌شده در حسابیار متفاوت است؛ دسته‌بندی را اصلاح کنید.",
	"mobile-voided":                "تراکنش باطل‌شده حسابیار هنوز گردش فعال دارد؛ گردش را باطل کنید.",
	"mobile-missing":               "رویداد حسابیار به مالی نرسیده؛ سرویس همگام‌سازی را بررسی کنید.",
	"ledger-invalid":               "موتور حسابداری از داده‌ها سند تراز نمی‌سازد؛ فوراً با پشتیبانی بررسی شود.",
	"ledger-difference":            "مانده دفتر ثبت‌شده با آثار اسناد مالی یکسان نیست؛ رسیدگی حسابداری و تطبیق آخرین اسناد دوره لازم است.",
}

// Triage classifies every finding (principles 3, 6, 9, 10).
func (a *Agent) Triage(ctx context.Context, findings []Finding) []TriageItem {
	items := make([]TriageItem, 0, len(findings))
	llmLeft := a.llmBudgetLeft(ctx)
	for _, f := range findings {
		item := TriageItem{Finding: f, By: "rule"}
		if guidance, ok := realImpact[f.Code()]; ok {
			item.Action = NeedsHuman
			item.Explanation = "کنترل درستی ارتباط اسناد یک ناهماهنگی واقعی یافت."
			item.Advice = guidance
			if f.Severity == "critical" && strings.HasPrefix(f.Code(), "ledger") {
				item.Action = Escalate
			}
			items = append(items, item)
			continue
		}
		if note, ok := knownNoise[f.Code()]; ok {
			item.Action = Monitor
			item.Explanation = note
			item.Advice = "نیازی به اقدام نیست؛ در گزارش دوره‌ای دیده می‌شود."
			items = append(items, item)
			continue
		}
		if a.cfg.RuleOnly || llmLeft <= 0 || a.classifier == nil {
			item.Action = NeedsHuman
			item.Explanation = "کنترل ناشناخته؛ به‌صورت محافظه‌کارانه برای بررسی انسانی ارجاع شد."
			item.Advice = "توضیح پیام ناظر را با سند مرتبط تطبیق دهید."
			items = append(items, item)
			continue
		}
		action, explanation, advice, ok := a.classifier.Classify(ctx, f)
		llmLeft--
		if !ok {
			item.Action = NeedsHuman
			item.Explanation = "طبقه‌بندی هوشمند در دسترس نبود؛ ارجاع محافظه‌کارانه."
			item.Advice = "توضیح پیام ناظر را با سند مرتبط تطبیق دهید."
		} else {
			item.Action = action
			item.Explanation = explanation
			item.Advice = advice
			item.By = "llm"
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		rank := map[Action]int{Escalate: 0, NeedsHuman: 1, Monitor: 2}
		if rank[items[i].Action] != rank[items[j].Action] {
			return rank[items[i].Action] < rank[items[j].Action]
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func (f Finding) Code() string {
	if i := strings.Index(f.ID, ":"); i >= 0 {
		return f.ID[:i]
	}
	return f.ID
}

func (a *Agent) llmBudgetLeft(ctx context.Context) int {
	if a.classifier == nil {
		return 0
	}
	used := 0
	if a.cfg.UsedThisMonth != nil {
		used = a.cfg.UsedThisMonth(ctx, a.now())
	}
	left := a.cfg.MonthlyLLMBudget - used
	if left < 0 {
		left = 0
	}
	return left
}

// InputHash is the idempotency key over the finding set (principle 8).
func InputHash(findings []Finding) string {
	ids := make([]string, 0, len(findings))
	for _, f := range findings {
		ids = append(ids, f.ID+"|"+f.Evidence)
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:])
}

// Summarize builds the Persian action plan (principle 10).
func Summarize(items []TriageItem) string {
	counts := map[Action]int{}
	for _, it := range items {
		counts[it.Action]++
	}
	var b strings.Builder
	if len(items) == 0 {
		return "صف ناظر مالی خالی است؛ دفاتر از نظر کنترل‌های ارتباطی سالم‌اند. ✅"
	}
	fmt.Fprintf(&b, "ناظر مالی %d مورد یافت: ", len(items))
	if counts[Escalate] > 0 {
		fmt.Fprintf(&b, "%d فوری، ", counts[Escalate])
	}
	fmt.Fprintf(&b, "%d نیازمند بررسی انسانی، %d فقط پایش.\n", counts[NeedsHuman], counts[Monitor])
	if counts[Escalate]+counts[NeedsHuman] == 0 {
		b.WriteString("همه موارد فقط پایشی‌اند و نیازی به اقدام نیست. ✅")
		return b.String()
	}
	b.WriteString("اولویت‌ها:")
	for _, it := range items {
		if it.Action == Monitor {
			continue
		}
		fmt.Fprintf(&b, "\n• [%s] %s — %s", it.Action, it.Title, it.Advice)
	}
	return b.String()
}

// CountsOf returns per-action totals.
func CountsOf(items []TriageItem) map[Action]int {
	counts := map[Action]int{}
	for _, it := range items {
		counts[it.Action]++
	}
	return counts
}

// MarshalTriage is a convenience for persistence layers.
func MarshalTriage(items []TriageItem) string {
	raw, _ := json.Marshal(items)
	return string(raw)
}
