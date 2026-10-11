package supervisoragent

import (
	"context"
	"testing"
	"time"
)

func triageState() []Finding {
	return []Finding{
		{ID: "payment-verification:costs:exp-operational-120", Severity: "warning", Title: "مبدأ عملیاتی بانک پرداخت‌کننده را مشخص نمی‌کند"},
		{ID: "stock-effect:incomingInvoices:IN-20260905-IH18", Severity: "critical", Title: "مقدار، کالا یا مالک موجودی با فاکتور ورود متفاوت است"},
		{ID: "mobile-link:mobileApp:89071410", Severity: "critical", Title: "تراکنش حسابیار باید دقیقاً یک گردش مالی متناظر داشته باشد"},
		{ID: "transfer:bankCash:m1", Severity: "critical", Title: "حساب مقصد انتقال معتبر نیست یا با مبدأ برابر است"},
		{ID: "mystery-code:page:ref1", Severity: "warning", Title: "کنترل ناشناخته"},
	}
}

func TestRulesClassifyKnownNoiseAndRealImpact(t *testing.T) {
	agent := New(Config{MonthlyLLMBudget: 10}, nil)
	items := agent.Triage(context.Background(), triageState())
	byCode := map[string]TriageItem{}
	for _, it := range items {
		byCode[it.Code()] = it
	}
	if byCode["payment-verification"].Action != Monitor {
		t.Fatalf("known noise not monitored: %+v", byCode["payment-verification"])
	}
	if byCode["stock-effect"].Action != Monitor {
		t.Fatalf("stock-effect label noise not monitored")
	}
	if byCode["mobile-link"].Action != NeedsHuman {
		t.Fatalf("real integrity finding not escalated to human: %+v", byCode["mobile-link"])
	}
	if byCode["transfer"].Action != NeedsHuman {
		t.Fatalf("broken transfer not routed to human")
	}
	for _, it := range items {
		if it.By != "rule" {
			t.Fatalf("rule-only agent produced %q triage", it.By)
		}
		if it.Explanation == "" || it.Advice == "" {
			t.Fatalf("missing Persian explanation/advice for %s", it.ID)
		}
	}
	if items[0].Action != NeedsHuman && items[0].Action != Escalate {
		t.Fatalf("sorted output must list human work first, got %s", items[0].Action)
	}
}

func TestUnknownCodeFallsBackToNeedsHumanWithoutLLM(t *testing.T) {
	agent := New(Config{RuleOnly: true}, nil)
	items := agent.Triage(context.Background(), []Finding{{ID: "mystery-code:page:ref1", Severity: "warning", Title: "کنترل ناشناخته"}})
	if items[0].Action != NeedsHuman {
		t.Fatalf("unknown finding must default to human review")
	}
}

type stubClassifier struct {
	calls  int
	action Action
}

func (s *stubClassifier) Classify(ctx context.Context, f Finding) (Action, string, string, bool) {
	s.calls++
	if s.action == "" {
		return "", "", "", false
	}
	return s.action, "توضیح آزمایشی", "توصیه آزمایشی", true
}

func (s *stubClassifier) Name() string { return "stub" }

func TestLLMClassifierUsedForUnknownOnlyAndBudgetRespected(t *testing.T) {
	stub := &stubClassifier{action: Monitor}
	used := 0
	agent := New(Config{MonthlyLLMBudget: 1, UsedThisMonth: func(ctx context.Context, now time.Time) int { return used }}, stub)
	items := agent.Triage(context.Background(), triageState())
	if stub.calls != 1 {
		t.Fatalf("classifier called %d times for 1 unknown finding", stub.calls)
	}
	byCode := map[string]TriageItem{}
	for _, it := range items {
		byCode[it.Code()] = it
	}
	if byCode["mystery-code"].By != "llm" || byCode["mystery-code"].Action != Monitor {
		t.Fatalf("LLM verdict not applied: %+v", byCode["mystery-code"])
	}
}

func TestBrokenClassifierDegradesToHuman(t *testing.T) {
	stub := &stubClassifier{}
	agent := New(Config{MonthlyLLMBudget: 10}, stub)
	items := agent.Triage(context.Background(), []Finding{{ID: "mystery-code:page:ref1", Severity: "warning"}})
	if items[0].Action != NeedsHuman || items[0].By == "llm" {
		t.Fatalf("failed classifier must degrade to human review, got %+v", items[0])
	}
}

func TestInputHashIsOrderInsensitiveAndEvidenceSensitive(t *testing.T) {
	a := []Finding{{ID: "x:1", Evidence: "e1"}, {ID: "y:2", Evidence: "e2"}}
	b := []Finding{{ID: "y:2", Evidence: "e2"}, {ID: "x:1", Evidence: "e1"}}
	if InputHash(a) != InputHash(b) {
		t.Fatal("hash must be order-insensitive")
	}
	c := []Finding{{ID: "x:1", Evidence: "changed"}}
	if InputHash(a) == InputHash(c) {
		t.Fatal("hash must change with evidence")
	}
}

func TestSummaryPersianAndEmptyQueue(t *testing.T) {
	if got := Summarize(nil); got == "" {
		t.Fatal("empty queue must still explain itself")
	}
	items := New(Config{RuleOnly: true}, nil).Triage(context.Background(), triageState())
	summary := Summarize(items)
	if len([]rune(summary)) < 30 {
		t.Fatalf("summary too short: %q", summary)
	}
}

func TestCountsOf(t *testing.T) {
	items := New(Config{RuleOnly: true}, nil).Triage(context.Background(), triageState())
	counts := CountsOf(items)
	total := 0
	for _, n := range counts {
		total += n
	}
	if total != len(items) {
		t.Fatal("counts must cover every item")
	}
}
