package service

import (
    "fmt"
    "math"
    "time"
    "github.com/erpsystem/textile-erp/internal/domain/entity"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// FinancialAdvisor is the AI-powered financial consultant
type FinancialAdvisor struct {
    creditCalculator *CreditScoreCalculator
    wasteCalculator  *WasteCalculator
    downtimeCalc     *DowntimeCalculator
    settlementValidator *SettlementValidator
    commissionCalc   *CommissionInvoiceCalculator
}

// NewFinancialAdvisor creates a new financial advisor
func NewFinancialAdvisor() *FinancialAdvisor {
    return &FinancialAdvisor{
        creditCalculator:    NewCreditScoreCalculator(),
        wasteCalculator:     NewWasteCalculator(),
        downtimeCalc:        NewDowntimeCalculator(10),
        settlementValidator: NewSettlementValidator(),
        commissionCalc:      NewCommissionInvoiceCalculator(NewWasteCalculator(), NewDowntimeCalculator(10)),
    }
}

// AdviceType defines types of financial advice
type AdviceType string

const (
    AdviceCreditLimit     AdviceType = "CREDIT_LIMIT"
    AdviceSettlement      AdviceType = "SETTLEMENT"
    AdviceWaste           AdviceType = "WASTE"
    AdviceDowntime        AdviceType = "DOWNTIME"
    AdviceCommission      AdviceType = "COMMISSION"
    AdviceRisk            AdviceType = "RISK"
    AdviceBlockCustomer   AdviceType = "BLOCK_CUSTOMER"
    AdvicePrepayment      AdviceType = "PREPAYMENT"
)

// AdviceSeverity defines urgency level
type AdviceSeverity string

const (
    SeverityInfo     AdviceSeverity = "INFO"
    SeverityWarning  AdviceSeverity = "WARNING"
    SeverityCritical AdviceSeverity = "CRITICAL"
    SeverityBlock    AdviceSeverity = "BLOCK"
)

// FinancialAdvice represents a piece of financial advice
type FinancialAdvice struct {
    Type        AdviceType     `json:"type"`
    Severity    AdviceSeverity `json:"severity"`
    Title       string         `json:"title"`
    Message     string         `json:"message"`
    Action      string         `json:"action"`
    Impact      string         `json:"impact"`
    Amount      valueobject.Money `json:"amount,omitempty"`
    Timestamp   time.Time      `json:"timestamp"`
}

// AdviceRequest is the input for requesting advice
type AdviceRequest struct {
    PartyID        int64              `json:"party_id"`
    Context        string             `json:"context"`        // e.g., "new_order", "settlement", "production_done"
    Data           map[string]float64 `json:"data"`
}

// GetAdvice provides financial advice based on context
func (fa *FinancialAdvisor) GetAdvice(req AdviceRequest, profile *entity.CustomerCreditProfile) []FinancialAdvice {
    var advices []FinancialAdvice
    
    switch req.Context {
    case "new_order":
        advices = fa.adviseNewOrder(req, profile)
    case "settlement":
        advices = fa.adviseSettlement(req, profile)
    case "production_done":
        advices = fa.adviseProductionDone(req, profile)
    case "credit_review":
        advices = fa.adviseCreditReview(profile)
    case "check_payment":
        advices = fa.adviseCheckPayment(req, profile)
    default:
        advices = fa.adviseGeneral(profile)
    }
    
    return advices
}

// adviseNewOrder provides advice before creating a new order
func (fa *FinancialAdvisor) adviseNewOrder(req AdviceRequest, profile *entity.CustomerCreditProfile) []FinancialAdvice {
    var advices []FinancialAdvice
    
    // Check if blocked
    if profile.IsBlocked {
        advices = append(advices, FinancialAdvice{
            Type:     AdviceBlockCustomer,
            Severity: SeverityBlock,
            Title:    "🚫 مشتری مسدود است!",
            Message:  fmt.Sprintf("این مشتری به دلیل «%s» مسدود شده و امکان ثبت سفارش جدید ندارد.", profile.BlockReason),
            Action:   "ابتدا با مدیر مالی برای رفع مسدودیت هماهنگ کنید.",
            Impact:   "ثبت سفارش جدید غیرممکن است.",
            Timestamp: time.Now(),
        })
        return advices
    }
    
    // Risk-based advice
    switch profile.RiskGroup {
    case "High":
        advices = append(advices, FinancialAdvice{
            Type:     AdvicePrepayment,
            Severity: SeverityCritical,
            Title:    "⚠️ مشتری پرریسک - نیاز به پیش‌پرداخت کامل",
            Message:  fmt.Sprintf("گروه ریسک: %s | امتیاز: %d", profile.RiskGroup, profile.BaseScore),
            Action:   "دریافت ۱۰۰٪ پیش‌پرداخت قبل از شروع تولید الزامی است.",
            Impact:   "در صورت عدم دریافت پیش‌پرداخت، ریسک نکول بسیار بالاست.",
            Amount:   profile.CreditLimit,
            Timestamp: time.Now(),
        })
    case "Medium":
        advices = append(advices, FinancialAdvice{
            Type:     AdvicePrepayment,
            Severity: SeverityWarning,
            Title:    "⚡ مشتری متوسط - ۲۰٪ پیش‌پرداخت",
            Message:  fmt.Sprintf("گروه ریسک: %s | امتیاز: %d", profile.RiskGroup, profile.BaseScore),
            Action:   "دریافت حداقل ۲۰٪ پیش‌پرداخت توصیه می‌شود.",
            Impact:   "کاهش ریسک مالی با پیش‌پرداخت جزئی.",
            Amount:   profile.CreditLimit.Multiply(0.2),
            Timestamp: time.Now(),
        })
    case "Low":
        advices = append(advices, FinancialAdvice{
            Type:     AdviceCreditLimit,
            Severity: SeverityInfo,
            Title:    "✅ مشتری خوش‌حساب - شرایط ویژه",
            Message:  fmt.Sprintf("گروه ریسک: %s | امتیاز: %d | دوره اعتبار: %d روز", profile.RiskGroup, profile.BaseScore, profile.CreditDays),
            Action:   "می‌توانید بدون پیش‌پرداخت و با اعتبار ۶۰ روزه سفارش ثبت کنید.",
            Impact:   "ریسک پایین، گردش مالی مطلوب.",
            Amount:   profile.CreditLimit,
            Timestamp: time.Now(),
        })
    }
    
    // Calculate estimated order value
    if yarnQty, ok := req.Data["yarn_qty"]; ok {
        if yarnPrice, ok2 := req.Data["yarn_price"]; ok2 {
            estimatedValue := valueobject.NewMoney(yarnQty * yarnPrice)
            if estimatedValue.IsGreaterThan(profile.CreditLimit) {
                advices = append(advices, FinancialAdvice{
                    Type:     AdviceCreditLimit,
                    Severity: SeverityCritical,
                    Title:    "🔴 هشدار: بیش از سقف اعتبار!",
                    Message:  fmt.Sprintf("ارزش تخمینی سفارش (%s) از سقف اعتبار مشتری (%s) بیشتر است.",
                        estimatedValue.String(), profile.CreditLimit.String()),
                    Action:   "کاهش حجم سفارش یا افزایش سقف اعتبار مشتری.",
                    Impact:   "در صورت ثبت، بدهی مشتری از سقف مجاز فراتر می‌رود.",
                    Amount:   estimatedValue,
                    Timestamp: time.Now(),
                })
            }
        }
    }
    
    return advices
}

// adviseSettlement provides advice for settlement operations
func (fa *FinancialAdvisor) adviseSettlement(req AdviceRequest, profile *entity.CustomerCreditProfile) []FinancialAdvice {
    var advices []FinancialAdvice
    
    if profile.RiskGroup == "High" {
        advices = append(advices, FinancialAdvice{
            Type:     AdviceSettlement,
            Severity: SeverityCritical,
            Title:    "⚠️ فقط تسویه نقدی مجاز است",
            Message:  "مشتری پرریسک فقط مجاز به تسویه نقدی است.",
            Action:   "چک، تهاتر و انتقال اعتبار برای این مشتری غیرمجاز است.",
            Impact:   "پذیرش چک یا تهاتر ریسک مالی شدید دارد.",
            Timestamp: time.Now(),
        })
    }
    
    // Check for overdue invoices
    if daysOverdue, ok := req.Data["days_overdue"]; ok && daysOverdue > 30 {
        advices = append(advices, FinancialAdvice{
            Type:     AdviceSettlement,
            Severity: SeverityCritical,
            Title:    "🔴 بدهی معوق!",
            Message:  fmt.Sprintf("این مشتری %.0f روز بدهی معوق دارد.", daysOverdue),
            Action:   "تسویه کامل بدهی قبل از هرگونه عملیات جدید الزامی است.",
            Impact:   "ادامه همکاری بدون تسویه، مطالبات مشکوک‌الوصول ایجاد می‌کند.",
            Timestamp: time.Now(),
        })
    }
    
    return advices
}

// adviseProductionDone provides advice after production completion
func (fa *FinancialAdvisor) adviseProductionDone(req AdviceRequest, profile *entity.CustomerCreditProfile) []FinancialAdvice {
    var advices []FinancialAdvice
    
    // Check excess waste
    if excessWasteRate, ok := req.Data["excess_waste_rate"]; ok {
        if excessWasteRate > 0.05 {
            advices = append(advices, FinancialAdvice{
                Type:     AdviceWaste,
                Severity: SeverityWarning,
                Title:    "⚠️ ضایعات مازاد بالا",
                Message:  fmt.Sprintf("نرخ ضایعات مازاد: %.1f%% (حد مجاز: 5%%)", excessWasteRate*100),
                Action:   "بررسی علت ضایعات و منظور کردن به حساب بافنده/پیمانکار.",
                Impact:   fmt.Sprintf("کسر از مطالبات پیمانکار و کاهش %.1f امتیاز اعتباری مشتری.", excessWasteRate*50),
                Timestamp: time.Now(),
            })
        }
    }
    
    // Check downtime
    if downtimeDays, ok := req.Data["downtime_days"]; ok && downtimeDays > 0 {
        severity := SeverityWarning
        if downtimeDays > 10 {
            severity = SeverityBlock
        }
        
        advices = append(advices, FinancialAdvice{
            Type:     AdviceDowntime,
            Severity: severity,
            Title:    "⏱️ جریمه خواب ماشین",
            Message:  fmt.Sprintf("%.1f روز توقف به دلیل تاخیر مشتری.", downtimeDays),
            Action:   "منظور کردن جریمه در فاکتور نهایی و بررسی مسدودیت مشتری.",
            Impact:   fmt.Sprintf("افزایش بدهی مشتری به میزان %.1f روز نرخ خواب ماشین.", downtimeDays),
            Timestamp: time.Now(),
        })
    }
    
    return advices
}

// adviseCreditReview provides periodic credit review advice
func (fa *FinancialAdvisor) adviseCreditReview(profile *entity.CustomerCreditProfile) []FinancialAdvice {
    var advices []FinancialAdvice
    
    // Score improvement suggestions
    if profile.BaseScore < 40 {
        advices = append(advices, FinancialAdvice{
            Type:     AdviceRisk,
            Severity: SeverityCritical,
            Title:    "🔴 امتیاز اعتباری بحرانی",
            Message:  fmt.Sprintf("امتیاز فعلی: %d/100 - گروه ریسک: %s", profile.BaseScore, profile.RiskGroup),
            Action:   "کاهش سقف اعتبار به ۵۰٪، عدم پذیرش چک، و الزام به تسویه نقدی.",
            Impact:   "تداوم همکاری با شرایط نقدی.",
            Timestamp: time.Now(),
        })
    } else if profile.BaseScore < 70 {
        advices = append(advices, FinancialAdvice{
            Type:     AdviceRisk,
            Severity: SeverityWarning,
            Title:    "🟡 امتیاز اعتباری متوسط",
            Message:  fmt.Sprintf("امتیاز فعلی: %d/100 - می‌توان با بهبود شرایط به Low ارتقا یافت.", profile.BaseScore),
            Action:   "تشویق مشتری به تسویه به‌موقع و کاهش ضایعات.",
            Impact:   "با بهبود امتیاز، شرایط اعتباری بهتری دریافت می‌کند.",
            Timestamp: time.Now(),
        })
    }
    
    return advices
}

// adviseCheckPayment provides advice when receiving checks
func (fa *FinancialAdvisor) adviseCheckPayment(req AdviceRequest, profile *entity.CustomerCreditProfile) []FinancialAdvice {
    var advices []FinancialAdvice
    
    if !fa.settlementValidator.isCheckAllowed(profile) {
        advices = append(advices, FinancialAdvice{
            Type:     AdviceSettlement,
            Severity: SeverityBlock,
            Title:    "🚫 پذیرش چک ممنوع!",
            Message:  fmt.Sprintf("گروه ریسک %s اجازه پرداخت با چک ندارد.", profile.RiskGroup),
            Action:   "فقط نقدی یا کارت به کارت.",
            Impact:   "چک برگشتی ریسک بالایی دارد.",
            Timestamp: time.Now(),
        })
    }
    
    if checkDays, ok := req.Data["check_days"]; ok && checkDays > float64(profile.CreditDays) {
        advices = append(advices, FinancialAdvice{
            Type:     AdviceSettlement,
            Severity: SeverityWarning,
            Title:    "⚠️ سررسید چک طولانی",
            Message:  fmt.Sprintf("سررسید چک (%.0f روز) از دوره اعتبار (%d روز) بیشتر است.", checkDays, profile.CreditDays),
            Action:   "درخواست چک با سررسید کوتاه‌تر یا دریافت بخشی نقدی.",
            Impact:   "افزایش ریسک وصول.",
            Timestamp: time.Now(),
        })
    }
    
    return advices
}

// adviseGeneral provides general financial health advice
func (fa *FinancialAdvisor) adviseGeneral(profile *entity.CustomerCreditProfile) []FinancialAdvice {
    return []FinancialAdvice{{
        Type:     AdviceCreditLimit,
        Severity: SeverityInfo,
        Title:    "📊 وضعیت مالی مشتری",
        Message:  fmt.Sprintf("گروه ریسک: %s | امتیاز: %d | سقف اعتبار: %s",
            profile.RiskGroup, profile.BaseScore, profile.CreditLimit.String()),
        Action:   "بررسی دوره‌ای وضعیت اعتباری هر ۳ ماه توصیه می‌شود.",
        Impact:   "مدیریت ریسک مستمر.",
        Timestamp: time.Now(),
    }}
}

// DTOs for API
type AdvisorResponse struct {
    PartyID   int64             `json:"party_id"`
    Advices   []FinancialAdvice `json:"advices"`
    Summary   string            `json:"summary"`
    RiskLevel string            `json:"risk_level"`
}

// Helper methods
func (sv *SettlementValidator) isCheckAllowed(profile *entity.CustomerCreditProfile) bool {
    return profile.RiskGroup != "High"
}

// CreditReport provides detailed credit analysis
type CreditReport struct {
    PartyID         int64              `json:"party_id"`
    CurrentScore    int                `json:"current_score"`
    RiskGroup       string             `json:"risk_group"`
    CreditLimit     valueobject.Money  `json:"credit_limit"`
    Recommendations []string           `json:"recommendations"`
    Alerts          []string           `json:"alerts"`
}

func (fa *FinancialAdvisor) GenerateCreditReport(profile *entity.CustomerCreditProfile) CreditReport {
    report := CreditReport{
        PartyID:      profile.PartyID,
        CurrentScore: profile.BaseScore,
        RiskGroup:    profile.RiskGroup,
        CreditLimit:  profile.CreditLimit,
    }
    
    // Generate recommendations
    switch {
    case profile.BaseScore < 40:
        report.Recommendations = append(report.Recommendations,
            "کاهش ۵۰٪ سقف اعتبار",
            "الزام به تسویه نقدی",
            "عدم پذیرش چک و تهاتر",
        )
        report.Alerts = append(report.Alerts, "مشتری پرریسک - بازبینی فوری")
    case profile.BaseScore < 70:
        report.Recommendations = append(report.Recommendations,
            "دریافت ۲۰٪ پیش‌پرداخت",
            "کاهش دوره اعتبار به ۳۰ روز",
        )
    default:
        report.Recommendations = append(report.Recommendations,
            "حفظ شرایط فعلی",
            "بررسی دوره‌ای ۶ ماهه",
        )
    }
    
    if profile.IsBlocked {
        report.Alerts = append(report.Alerts, "🚫 مشتری مسدود: " + profile.BlockReason)
    }
    
    return report
}

// Suppress unused import warnings
var _ = math.Abs
