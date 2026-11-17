package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"

	"github.com/erpsystem/textile-erp/internal/application/financecore"
)

type RialSourceRow struct {
	Row       int    `json:"row"`
	Date      string `json:"date"`
	Time      string `json:"time"`
	Bank      string `json:"bank"`
	Direction string `json:"direction"`
	Amount    int64  `json:"amount"`
	Balance   *int64 `json:"balance,omitempty"`
}

type RialSourceEvidence struct {
	SHA256     string          `json:"sha256"`
	Currency   string          `json:"currency"`
	Rows       []RialSourceRow `json:"rows"`
	RestoreIDs []string        `json:"owner_approved_audit_restore_ids"`
}

// The unit is supplied by the owner, never guessed from magnitude or a filename.
// Excel 2003 XML is read without running formulas or interpreting text as code.
func ParseRialSourceExcel(raw []byte, currency string, restoreIDs []string) (RialSourceEvidence, error) {
	e := RialSourceEvidence{Currency: currency, RestoreIDs: restoreIDs}
	if currency != "IRR" {
		return e, fmt.Errorf("source unit must be explicitly confirmed as IRR")
	}
	hash := sha256.Sum256(raw)
	e.SHA256 = hex.EncodeToString(hash[:])
	var book struct {
		Sheets []struct {
			Table struct {
				Rows []struct {
					Cells []struct {
						Index   int    `xml:"Index,attr"`
						Formula string `xml:"Formula,attr"`
						Data    string `xml:"Data"`
					} `xml:"Cell"`
				} `xml:"Row"`
			} `xml:"Table"`
		} `xml:"Worksheet"`
	}
	if err := xml.NewDecoder(bytes.NewReader(raw)).Decode(&book); err != nil {
		return e, err
	}
	if len(book.Sheets) != 1 {
		return e, fmt.Errorf("expected one HesabYar worksheet")
	}
	for i, row := range book.Sheets[0].Table.Rows {
		cells := []string{}
		for _, c := range row.Cells {
			if c.Formula != "" {
				return e, fmt.Errorf("source formulas are not accepted")
			}
			if c.Index > 100 {
				return e, fmt.Errorf("invalid source cell index")
			}
			for c.Index > len(cells)+1 {
				cells = append(cells, "")
			}
			cells = append(cells, c.Data)
		}
		if len(cells) < 12 {
			return e, fmt.Errorf("source row %d has missing columns", i+1)
		}
		if i == 0 {
			if cells[0] != "تاریخ شمسی" || cells[3] != "مبلغ" || cells[4] != "مانده بانک" {
				return e, fmt.Errorf("unexpected HesabYar headers")
			}
			continue
		}
		date, err := financecore.AccountingDate(sourceDigits(cells[0]))
		if err != nil {
			return e, fmt.Errorf("source date row %d: %w", i+1, err)
		}
		amount, err := rialInteger(cells[3])
		if err != nil || amount <= 0 {
			return e, fmt.Errorf("invalid source amount row %d", i+1)
		}
		var balance *int64
		if cells[4] != "" {
			b, err := rialInteger(cells[4])
			if err != nil {
				return e, err
			}
			balance = &b
		}
		direction := ""
		switch cells[2] {
		case "واریز":
			direction = "in"
		case "برداشت":
			direction = "out"
		default:
			return e, fmt.Errorf("invalid direction row %d", i+1)
		}
		e.Rows = append(e.Rows, RialSourceRow{i + 1, date.Format("2006-01-02"), sourceTime(cells[0]), sourceBank(cells[1]), direction, amount, balance})
	}
	if len(e.Rows) == 0 {
		return e, fmt.Errorf("empty source workbook")
	}
	return e, nil
}

var sourceClock = regexp.MustCompile(`[0-9]{1,2}:[0-9]{2}`)

func sourceDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '۰' && r <= '۹' {
			return '0' + r - '۰'
		}
		if r >= '٠' && r <= '٩' {
			return '0' + r - '٠'
		}
		return r
	}, s)
}
func sourceTime(s string) string {
	v := sourceClock.FindString(sourceDigits(s))
	if len(v) == 4 {
		return "0" + v
	}
	return v
}
func sourceBank(s string) string {
	s = normalizeMobileText(s)
	s = strings.TrimSpace(strings.ReplaceAll(s, "بانک", ""))
	s = strings.ToLower(s)
	switch s {
	case "b.pasargad", "pasargad":
		return "پاسارگاد"
	case "b.mellat", "mellat":
		return "ملت"
	case "melli":
		return "ملی"
	}
	return s
}

type sourceVerifiedAmount struct {
	Amount  int64
	Balance *int64
	Rows    []int
	Basis   string
}

func matchRialSource(row, typed map[string]any, e RialSourceEvidence, old map[string]any) (sourceVerifiedAmount, error) {
	result := sourceVerifiedAmount{Basis: "excel"}
	amount, err := rialInteger(row["amount"])
	if err != nil {
		return result, err
	}
	target, err := rialInteger(row["sourceAmount"])
	if err != nil {
		return result, err
	}
	// Only the recorded, erroneous conversion is corrected. A large number alone
	// never establishes either the unit or a factor of ten.
	if stringValue(row["sourceCurrency"]) != "IRT" || amount != target*10 {
		return result, fmt.Errorf("unproven prior conversion for %s", stringValue(row["id"]))
	}
	parsed, dateErr := financecore.AccountingDate(sourceDigits(stringValue(row["occurredJalali"])))
	date := ""
	if dateErr == nil {
		date = parsed.Format("2006-01-02")
	}
	clock := sourceTime(stringValue(row["occurredJalali"]))
	if strings.Contains(stringValue(row["occurredJalali"]), "T00:00:00Z") {
		clock = ""
	}
	if date == "" {
		date = stringValue(typed["transaction_date"])
		if len(date) > 10 {
			date = date[:10]
		}
	}
	if clock == "" {
		clock = sourceTime(stringValue(typed["transaction_time"]))
	}
	bank := sourceBank(stringValue(row["bank"]))
	direction := stringValue(row["direction"])
	var expectedBalance *int64
	if row["reportedBalance"] != nil {
		v, err := rialInteger(row["reportedBalance"])
		if err != nil || v%10 != 0 {
			return result, fmt.Errorf("invalid converted balance")
		}
		v /= 10
		expectedBalance = &v
	}
	for _, candidate := range e.Rows {
		if candidate.Date != date || candidate.Bank != bank || candidate.Direction != direction || candidate.Amount != target || (clock != "" && candidate.Time != clock) {
			continue
		}
		if expectedBalance != nil && (candidate.Balance == nil || *candidate.Balance != *expectedBalance) {
			continue
		}
		result.Amount = target
		result.Rows = append(result.Rows, candidate.Row)
		result.Balance = expectedBalance
	}
	if len(result.Rows) > 0 {
		return result, nil
	}
	for _, id := range e.RestoreIDs {
		if id != stringValue(row["externalId"]) {
			continue
		}
		prior, err := rialInteger(old["amount"])
		if old == nil || err != nil || prior != target {
			return result, fmt.Errorf("approved audit restore does not match original %s", id)
		}
		result.Amount = prior
		result.Balance = expectedBalance
		result.Basis = "owner_approved_policy_1_restore"
		return result, nil
	}
	return result, fmt.Errorf("no verified source match for %s (date=%s, time=%s)", stringValue(row["externalId"]), date, clock)
}

func sourceID(row map[string]any) string {
	return firstText(row, "sourceMobileTransaction", "sourceId", "externalId")
}

func sourceTypedIndex(typed []map[string]any) map[string]map[string]any {
	result := map[string]map[string]any{}
	for _, row := range typed {
		if stringValue(row["source"]) == "OPERATIONAL" {
			continue
		}
		id := strings.TrimPrefix(stringValue(row["external_transaction_id"]), "HY-")
		result[id] = row
	}
	return result
}
