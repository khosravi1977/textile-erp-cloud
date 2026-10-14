package handler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/application/supervisoragent"
	"github.com/erpsystem/textile-erp/internal/infrastructure/persistence/postgres"
	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// supervisorAgentFindings converts the API-layer findings into the agent's
// neutral view. The agent itself never learns about HTTP or workspace shape.
func supervisorAgentFindings(report supervisorReport) []supervisoragent.Finding {
	out := make([]supervisoragent.Finding, 0, len(report.Findings))
	for _, f := range report.Findings {
		out = append(out, supervisoragent.Finding{
			ID: f.ID, Severity: f.Severity, Category: f.Category,
			Title: f.Title, Page: f.Page, Reference: f.Reference, Evidence: f.Evidence,
		})
	}
	return out
}

// SupervisorAgentReport returns the latest stored agent sweep for the caller.
func (h *APIHandler) SupervisorAgentReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if postgres.DB == nil {
		RespondError(w, http.StatusServiceUnavailable, "پایگاه داده در دسترس نیست")
		return
	}
	var payload []byte
	_, err := postgres.WithCompanySession(r.Context(), postgres.DB, requestctx.CompanyID(r.Context()), func(q postgres.SessionQueryable) (bool, error) {
		return true, q.QueryRowContext(r.Context(),
			`SELECT triage FROM financial_agent_runs WHERE company_id=$1 ORDER BY run_at DESC LIMIT 1`,
			requestctx.CompanyID(r.Context())).Scan(&payload)
	})
	if err != nil {
		RespondJSON(w, http.StatusOK, map[string]any{"runId": "", "summary": "هنوز اجرایی ثبت نشده است؛ با درخواست اجرا، ناظر را تحلیل کنید."})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	RespondJSON(w, http.StatusOK, json.RawMessage(payload))
}

// SupervisorAgentRun executes a triage sweep over the live supervisor report.
// It is strictly read-only over the ledger: only the agent's own audit row is
// written.
func (h *APIHandler) SupervisorAgentRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if postgres.DB == nil {
		RespondError(w, http.StatusServiceUnavailable, "پایگاه داده در دسترس نیست")
		return
	}
	report, err := h.buildSupervisorReport(r)
	if err != nil {
		RespondError(w, http.StatusServiceUnavailable, "بررسی مالی در دسترس نیست؛ اجرای ایجنت ممکن نشد")
		return
	}
	findings := supervisorAgentFindings(report)
	inputHash := supervisoragent.InputHash(findings)
	companyID := requestctx.CompanyID(r.Context())

	if cached := loadAgentRunByHash(r.Context(), companyID, inputHash); cached != nil {
		w.Header().Set("Cache-Control", "no-store")
		RespondJSON(w, http.StatusOK, cached)
		return
	}

	result := runSupervisorAgent(r.Context(), companyID, report)
	if err := storeAgentRun(r.Context(), result); err != nil {
		log.Printf("supervisor agent run store failed company=%d: %v", companyID, err)
	}
	w.Header().Set("Cache-Control", "no-store")
	RespondJSON(w, http.StatusOK, result)
}

// runSupervisorAgent triages one report. Facts in, plan out; no side effects
// beyond the returned value.
func runSupervisorAgent(ctx context.Context, companyID int64, report supervisorReport) supervisoragent.RunResult {
	classifier, _ := supervisoragent.NewLLMClassifierFromEnv()
	budget := supervisoragent.Config{MonthlyLLMBudget: envIntDefault("SUPERVISOR_AGENT_LLM_BUDGET", 60)}
	if os.Getenv("SUPERVISOR_AGENT_RULE_ONLY") == "true" {
		classifier = nil
	}
	agent := supervisoragent.New(budget, classifier)
	findings := supervisorAgentFindings(report)
	items := agent.Triage(ctx, findings)
	result := supervisoragent.RunResult{
		RunID:     newAgentRunID(),
		CompanyID: companyID,
		CreatedAt: time.Now().UTC(),
		InputHash: supervisoragent.InputHash(findings),
		Total:     len(items),
		Counts:    supervisoragent.CountsOf(items),
		Items:     items,
		Summary:   supervisoragent.Summarize(items),
	}
	if classifier != nil {
		result.Model = classifier.Name()
	}
	for _, it := range items {
		if it.By == "llm" {
			result.LLMUsed = true
			break
		}
	}
	return result
}

// SweepSupervisorAgentForCompany runs the triage from background workers.
func SweepSupervisorAgentForCompany(ctx context.Context, companyID int64) {
	if postgres.DB == nil {
		return
	}
	h := NewAPIHandler(nil)
	defer h.Close()
	h.sweepSupervisorAgentForCompany(ctx, companyID)
}

func (h *APIHandler) sweepSupervisorAgentForCompany(ctx context.Context, companyID int64) {
	scanCtx, cancel := context.WithTimeout(requestctx.WithIdentity(ctx, companyID, 0, "system", "supervisor-agent"), 60*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(scanCtx, http.MethodPost, "http://internal/api/supervisor/agent", nil)
	report, err := h.buildSupervisorReport(r)
	if err != nil {
		return
	}
	findings := supervisorAgentFindings(report)
	inputHash := supervisoragent.InputHash(findings)
	if loadAgentRunByHash(scanCtx, companyID, inputHash) != nil {
		return
	}
	result := runSupervisorAgent(scanCtx, companyID, report)
	if err := storeAgentRun(scanCtx, result); err != nil {
		log.Printf("supervisor agent sweep store failed company=%d: %v", companyID, err)
	}
}

func newAgentRunID() string {
	raw := make([]byte, 8)
	_, _ = rand.Read(raw)
	return "agent_" + hex.EncodeToString(raw)
}

func envIntDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func loadAgentRunByHash(ctx context.Context, companyID int64, inputHash string) *supervisoragent.RunResult {
	var payload []byte
	_, err := postgres.WithCompanySession(ctx, postgres.DB, companyID, func(q postgres.SessionQueryable) (bool, error) {
		return true, q.QueryRowContext(ctx,
			`SELECT triage FROM financial_agent_runs WHERE company_id=$1 AND input_hash=$2 ORDER BY run_at DESC LIMIT 1`,
			companyID, inputHash).Scan(&payload)
	})
	if err != nil || len(payload) == 0 {
		return nil
	}
	var result supervisoragent.RunResult
	if json.Unmarshal(payload, &result) != nil {
		return nil
	}
	result.Cached = true
	return &result
}

func storeAgentRun(ctx context.Context, result supervisoragent.RunResult) error {
	triage := supervisoragent.MarshalTriage(result.Items)
	return postgres.WithCompanyTx(ctx, postgres.DB, result.CompanyID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO financial_agent_runs
			(company_id, run_at, input_hash, findings_total, escalate, needs_human, monitor, summary, model, llm_used, triage)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			result.CompanyID, result.CreatedAt, result.InputHash, result.Total,
			result.Counts[supervisoragent.Escalate], result.Counts[supervisoragent.NeedsHuman], result.Counts[supervisoragent.Monitor],
			result.Summary, result.Model, result.LLMUsed, triage)
		return err
	})
}
