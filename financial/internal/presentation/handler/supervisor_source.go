package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/erpsystem/textile-erp/internal/platform/requestctx"
)

// Bind approval to the current operational document, not a stale browser list.
// Historical/manual invoices need no source lookup. The cap fails closed for a
// missing row rather than treating truncated data as proof of absence.
func (h *APIHandler) supervisorSourceStamp(r *http.Request, oldState, newState map[string]any) (string, error) {
	old := indexSupervisorRows(rowsFrom(oldState, "incomingInvoices"), "id")
	changed := []map[string]any{}
	for _, row := range rowsFrom(newState, "incomingInvoices") {
		if strings.HasPrefix(firstText(row, "source_type"), "operational_") && supervisorRowChanged(old[firstText(row, "id")], row) {
			changed = append(changed, row)
		}
	}
	if len(changed) == 0 {
		return "", nil
	}
	if h.operational == nil {
		return "", fmt.Errorf("برای تأیید فاکتور عملیاتی، دسترسی به سند مبدأ لازم است")
	}
	b, closeBridge, err := h.operational.ForCompany(r.Context(), requestctx.CompanyID(r.Context()))
	if err != nil {
		return "", fmt.Errorf("دریافت سند مبدأ عملیاتی ممکن نشد")
	}
	defer closeBridge()
	evidence := []any{}
	for _, invoice := range changed {
		id := firstText(invoice, "sourceId")
		found := false
		var item, customer, date string
		var quantity float64
		switch firstText(invoice, "source_type") {
		case "operational_yarn_in":
			rows, e := b.YarnIncoming(10001)
			if e != nil {
				return "", fmt.Errorf("خواندن ورود نخ عملیاتی ممکن نشد")
			}
			for _, row := range rows {
				if strconv.FormatInt(row.ID, 10) == id {
					found = true
					item = row.YarnName
					customer = row.CustomerName
					quantity = row.Weight
					date = row.Date
					evidence = append(evidence, row)
					break
				}
			}
		case "operational_chelle_in":
			rows, e := b.ChelleIncoming(10001)
			if e != nil {
				return "", fmt.Errorf("خواندن ورود چله ممکن نشد")
			}
			for _, row := range rows {
				if strconv.FormatInt(row.ID, 10) == id {
					found = true
					item = row.YarnName
					if item == "" {
						item = row.Hambaft
					}
					customer = row.Warper
					if customer == "" {
						customer = row.CustomerName
					}
					quantity = row.Weight
					date = row.Date
					evidence = append(evidence, row)
					break
				}
			}
		case "operational_spare_part":
			rows, e := b.SparePartsInventory(10001)
			if e != nil {
				return "", fmt.Errorf("خواندن ورود قطعه ممکن نشد")
			}
			for _, row := range rows {
				if strconv.FormatInt(row.ID, 10) == id {
					found = true
					item = row.PartName
					if item == "" {
						item = row.PartNumber
					}
					customer = row.VendorName
					if customer == "" {
						customer = "تأمین‌کننده قطعات"
					}
					quantity = row.Quantity
					date = row.Date
					evidence = append(evidence, row)
					break
				}
			}
		default:
			return "", fmt.Errorf("این نوع سند مبدأ هنوز توسط ناظر قابل تأیید نیست")
		}
		if !found {
			return "", fmt.Errorf("سند مبدأ %s پیدا نشد؛ تازه‌سازی یا رسیدگی به مغایرت لازم است", id)
		}
		if quantity < 0 {
			quantity = -quantity
		}
		if strings.TrimSpace(item) != firstText(invoice, "itemName") || strings.TrimSpace(customer) != firstText(invoice, "customer") || quantity != number(invoice["quantity"]) || date != firstText(invoice, "operationalDate") {
			return "", fmt.Errorf("مشخصات فاکتور با سند عملیاتی %s متفاوت است؛ ابتدا مغایرت در مبدأ اصلاح شود", id)
		}
	}
	raw, _ := json.Marshal(evidence)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
