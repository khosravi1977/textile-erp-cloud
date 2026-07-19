package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// InvoiceService handles all invoice operations
type InvoiceService struct {
	mu             sync.RWMutex
	invoices       map[int64][]entity.Invoice
	nextID         map[int64]int64
	nextInvoiceNum map[int64]int
}

// NewInvoiceService creates a new invoice service
func NewInvoiceService() *InvoiceService {
	is := &InvoiceService{
		invoices:       make(map[int64][]entity.Invoice),
		nextID:         make(map[int64]int64),
		nextInvoiceNum: make(map[int64]int),
	}

	is.createSampleInvoicesForCompany(1)
	return is
}

func (is *InvoiceService) createSampleInvoices() {
	is.createSampleInvoicesForCompany(1)
}

func (is *InvoiceService) createSampleInvoicesForCompany(companyID int64) {
	companyID = normalizedCompanyID(companyID)
	is.mu.RLock()
	seeded := len(is.invoices[companyID]) > 0
	is.mu.RUnlock()
	if seeded {
		return
	}

	inv1 := entity.NewInvoice("INV-1402-001", 1, "شرکت نساجی نمونه")
	inv1.AddLine(1, "نخ پلی استر", "نخ پلی استر درجه ۱", 100, "KG", valueobject.NewMoney(200000))
	inv1.AddLine(2, "نخ پنبه", "نخ پنبه مرغوب", 50, "KG", valueobject.NewMoney(280000))
	inv1.Issue()
	inv1.AddPayment(inv1.TotalAmount)
	is.AddInvoiceForCompany(companyID, inv1)

	inv2 := entity.NewInvoice("INV-1402-002", 1, "شرکت نساجی نمونه")
	inv2.AddLine(3, "پارچه پلی استر", "پارچه عرض ۱۵۰", 200, "Meter", valueobject.NewMoney(350000))
	inv2.Issue()
	inv2.AddPayment(valueobject.NewMoney(50000000))
	is.AddInvoiceForCompany(companyID, inv2)

	inv3 := entity.NewInvoice("INV-1402-003", 2, "کارگاه بافندگی امید")
	inv3.AddLine(4, "پارچه نخی", "پارچه نخی سفید", 150, "Meter", valueobject.NewMoney(400000))
	inv3.DueDate = time.Now().AddDate(0, 0, -15)
	inv3.Issue()
	inv3.CheckOverdue()
	is.AddInvoiceForCompany(companyID, inv3)
}

// AddInvoice adds a new invoice
func (is *InvoiceService) AddInvoice(inv *entity.Invoice) entity.Invoice {
	return is.AddInvoiceForCompany(1, inv)
}

// AddInvoiceForCompany adds a new invoice for one tenant.
func (is *InvoiceService) AddInvoiceForCompany(companyID int64, inv *entity.Invoice) entity.Invoice {
	is.mu.Lock()
	defer is.mu.Unlock()

	companyID = normalizedCompanyID(companyID)
	if is.nextID[companyID] == 0 {
		is.nextID[companyID] = 1
	}
	inv.ID = is.nextID[companyID]
	is.nextID[companyID]++
	is.invoices[companyID] = append(is.invoices[companyID], *inv)
	return *inv
}

// CreateInvoice creates a new invoice from request
func (is *InvoiceService) CreateInvoice(customerID int64, customerName string, lines []InvoiceLineRequest) *entity.Invoice {
	return is.CreateInvoiceForCompany(1, customerID, customerName, lines)
}

// CreateInvoiceForCompany creates a new tenant-scoped invoice from request.
func (is *InvoiceService) CreateInvoiceForCompany(companyID, customerID int64, customerName string, lines []InvoiceLineRequest) *entity.Invoice {
	companyID = normalizedCompanyID(companyID)
	is.createSampleInvoicesForCompany(companyID)
	is.mu.Lock()
	if is.nextInvoiceNum[companyID] == 0 {
		is.nextInvoiceNum[companyID] = 1001
	}
	invoiceNo := fmt.Sprintf("INV-%d-1402-%03d", companyID, is.nextInvoiceNum[companyID])
	is.nextInvoiceNum[companyID]++
	is.mu.Unlock()

	inv := entity.NewInvoice(invoiceNo, customerID, customerName)

	for _, line := range lines {
		inv.AddLine(line.ItemID, line.ItemName, line.Description, line.Qty, line.Unit, valueobject.NewMoney(line.UnitPrice))
	}

	inv.Issue()
	saved := is.AddInvoiceForCompany(companyID, inv)
	return &saved
}

// InvoiceLineRequest represents a line item request
type InvoiceLineRequest struct {
	ItemID      int64   `json:"item_id"`
	ItemName    string  `json:"item_name"`
	Description string  `json:"description"`
	Qty         float64 `json:"qty"`
	Unit        string  `json:"unit"`
	UnitPrice   float64 `json:"unit_price"`
}

// GetInvoices returns all invoices
func (is *InvoiceService) GetInvoices() []entity.Invoice {
	return is.GetInvoicesForCompany(1)
}

// GetInvoicesForCompany returns all tenant-scoped invoices.
func (is *InvoiceService) GetInvoicesForCompany(companyID int64) []entity.Invoice {
	is.createSampleInvoicesForCompany(companyID)
	is.mu.Lock()
	defer is.mu.Unlock()

	companyID = normalizedCompanyID(companyID)
	for i := range is.invoices[companyID] {
		is.invoices[companyID][i].CheckOverdue()
	}
	return append([]entity.Invoice(nil), is.invoices[companyID]...)
}

// GetInvoice returns a specific invoice
func (is *InvoiceService) GetInvoice(id int64) *entity.Invoice {
	return is.GetInvoiceForCompany(1, id)
}

// GetInvoiceForCompany returns a specific tenant-scoped invoice.
func (is *InvoiceService) GetInvoiceForCompany(companyID, id int64) *entity.Invoice {
	is.createSampleInvoicesForCompany(companyID)
	is.mu.Lock()
	defer is.mu.Unlock()

	companyID = normalizedCompanyID(companyID)
	for i := range is.invoices[companyID] {
		if is.invoices[companyID][i].ID == id {
			is.invoices[companyID][i].CheckOverdue()
			inv := is.invoices[companyID][i]
			return &inv
		}
	}
	return nil
}

// AddPaymentToInvoice records a payment
func (is *InvoiceService) AddPaymentToInvoice(invoiceID int64, amount float64) (*entity.Invoice, error) {
	return is.AddPaymentToInvoiceForCompany(1, invoiceID, amount)
}

// AddPaymentToInvoiceForCompany records a tenant-scoped payment.
func (is *InvoiceService) AddPaymentToInvoiceForCompany(companyID, invoiceID int64, amount float64) (*entity.Invoice, error) {
	is.createSampleInvoicesForCompany(companyID)
	is.mu.Lock()
	defer is.mu.Unlock()

	companyID = normalizedCompanyID(companyID)
	for i := range is.invoices[companyID] {
		if is.invoices[companyID][i].ID != invoiceID {
			continue
		}
		if is.invoices[companyID][i].Status == entity.InvoicePaid {
			return nil, fmt.Errorf("invoice already fully paid")
		}
		is.invoices[companyID][i].AddPayment(valueobject.NewMoney(amount))
		inv := is.invoices[companyID][i]
		return &inv, nil
	}
	return nil, fmt.Errorf("invoice not found: %d", invoiceID)
}

// GetInvoiceReport generates a sales report
func (is *InvoiceService) GetInvoiceReport() entity.InvoiceReport {
	return is.GetInvoiceReportForCompany(1)
}

// GetInvoiceReportForCompany generates a tenant-scoped sales report.
func (is *InvoiceService) GetInvoiceReportForCompany(companyID int64) entity.InvoiceReport {
	report := entity.InvoiceReport{
		PeriodStart: time.Now().AddDate(0, -1, 0),
		PeriodEnd:   time.Now(),
	}

	for _, inv := range is.GetInvoicesForCompany(companyID) {
		inv.CheckOverdue()
		report.TotalInvoices++
		report.TotalSales = report.TotalSales.Add(inv.TotalAmount)
		report.TotalCollected = report.TotalCollected.Add(inv.PaidAmount)
		report.TotalOutstanding = report.TotalOutstanding.Add(inv.Balance)

		switch inv.Status {
		case entity.InvoicePaid:
			report.PaidCount++
		case entity.InvoiceOverdue:
			report.OverdueCount++
		}
	}

	return report
}
