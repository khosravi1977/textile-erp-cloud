package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// Short-lived, process-local signing key. A restart/another replica invalidates
// a preview safely: the user must review again; it never authorizes a new draft.
var supervisorReviewKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}()

type supervisorApprovalKey struct{}
type supervisorApproval struct {
	Company     int64  `json:"company"`
	User        int64  `json:"user"`
	Revision    int64  `json:"revision"`
	Checksum    string `json:"checksum"`
	SourceStamp string `json:"sourceStamp"`
	Expires     int64  `json:"expires"`
}

func signSupervisorApproval(claim supervisorApproval) string {
	raw, _ := json.Marshal(claim)
	mac := hmac.New(sha256.New, supervisorReviewKey)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func checkSupervisorApproval(token string, expected supervisorApproval, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, supervisorReviewKey)
	mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return false
	}
	var claim supervisorApproval
	if json.Unmarshal(raw, &claim) != nil {
		return false
	}
	return claim.Company == expected.Company && claim.User == expected.User && claim.Revision == expected.Revision && claim.Checksum == expected.Checksum && claim.SourceStamp == expected.SourceStamp && claim.Expires > now.Unix() && claim.Expires <= now.Add(11*time.Minute).Unix()
}

// The preview is read-only. Commit binds tenant, actor, revision and the entire
// permission-scoped draft; optimistic locking and the journal are one DB tx.
func (h *APIHandler) SupervisorReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondError(w, 405, "Method not allowed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxWorkspacePayload)
	var input struct {
		State    json.RawMessage `json:"state"`
		Revision *int64          `json:"revision"`
		Approval string          `json:"approval"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil || input.Revision == nil {
		RespondError(w, 400, "نسخه داده و پیش‌نویس الزامی است")
		return
	}
	state, checksum, err := validateWorkspaceState(input.State)
	if err != nil {
		RespondError(w, 400, err.Error())
		return
	}
	current, err := loadWorkspace(r, requestctx.CompanyID(r.Context()))
	if err != nil {
		RespondError(w, 503, "دریافت نسخه مالی برای بررسی ممکن نشد")
		return
	}
	if current.Revision != *input.Revision {
		RespondError(w, 409, "اطلاعات تغییر کرده است؛ تازه‌سازی و بررسی مجدد لازم است")
		return
	}
	if requestctx.IsPortalAccess(r.Context()) {
		state, checksum, err = mergeWorkspaceState(current.State, state, writableWorkspaceFields(r.Context()))
		if err != nil {
			RespondError(w, 403, "مجوز ثبت این تغییرات وجود ندارد")
			return
		}
	}
	oldState, proposed := decodeWorkspaceMap(current.State), decodeWorkspaceMap(state)
	sourceStamp, err := h.supervisorSourceStamp(r, oldState, proposed)
	if err != nil {
		RespondError(w, 422, err.Error())
		return
	}
	for _, validate := range []func(map[string]any, map[string]any) error{validateWorkspaceLifecycleChanges, validateWorkspaceAccountingChanges, validateWorkspaceSupervisorChanges} {
		if err := validate(oldState, proposed); err != nil {
			RespondError(w, 422, err.Error())
			return
		}
	}
	ledgerBefore := oldState
	hasLedger, err := supervisorHasPersistedLedger(r.Context(), requestctx.CompanyID(r.Context()))
	if err != nil {
		RespondError(w, 503, "کنترل سند ثبت‌شده حسابداری ممکن نشد")
		return
	}
	if !hasLedger {
		ledgerBefore = map[string]any{}
	}
	lines, err := supervisorLedgerDelta(ledgerBefore, proposed)
	if err != nil {
		RespondError(w, 422, err.Error())
		return
	}
	claim := supervisorApproval{Company: requestctx.CompanyID(r.Context()), User: requestctx.UserID(r.Context()), Revision: current.Revision, Checksum: checksum, SourceStamp: sourceStamp, Expires: time.Now().Add(10 * time.Minute).Unix()}
	if strings.HasSuffix(r.URL.Path, "/preview") {
		RespondJSON(w, 200, map[string]any{"approval": signSupervisorApproval(claim), "revision": current.Revision, "expires_at": claim.Expires, "lines": lines, "findings": supervisorStateFindings(proposed)})
		return
	}
	if !checkSupervisorApproval(input.Approval, claim, time.Now()) {
		RespondError(w, 409, "پیش‌نمایش منقضی یا تغییر کرده است؛ دوباره بررسی و تأیید کنید")
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), supervisorApprovalKey{}, claim))
	doc, err := saveWorkspace(r, claim.Company, claim.User, input.Revision, state, checksum, writableWorkspaceFields(r.Context()))
	if err != nil {
		var conflict workspaceConflict
		if errors.As(err, &conflict) {
			RespondError(w, 409, "ثبت هم‌زمان دیگری انجام شد؛ دوباره بررسی کنید")
			return
		}
		RespondError(w, 422, "ثبت نهایی انجام نشد: "+err.Error())
		return
	}
	RespondJSON(w, 200, filterWorkspaceDocument(doc, r.Context()))
}
