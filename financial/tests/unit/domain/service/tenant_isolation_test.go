package service_test

import (
	"testing"

	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/service"
	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

func TestCostServiceKeepsCompanyDataIsolated(t *testing.T) {
	svc := service.NewCostService()

	svc.AddCostForCompany(1, *entity.NewCost(1, entity.CostLabor, "company 1 labor", valueobject.NewMoney(1000)))
	svc.AddCostForCompany(2, *entity.NewCost(1, entity.CostLabor, "company 2 labor", valueobject.NewMoney(2000)))

	companyOneCosts := svc.GetCostsForCompany(1)
	companyTwoCosts := svc.GetCostsForCompany(2)

	if len(companyOneCosts) != 1 || companyOneCosts[0].Description != "company 1 labor" {
		t.Fatalf("company 1 costs leaked or missing: %#v", companyOneCosts)
	}
	if len(companyTwoCosts) != 1 || companyTwoCosts[0].Description != "company 2 labor" {
		t.Fatalf("company 2 costs leaked or missing: %#v", companyTwoCosts)
	}
}

func TestInventoryServiceKeepsCompanyStockIsolated(t *testing.T) {
	t.Setenv("SEED_DEMO_DATA", "true")
	svc := service.NewInventoryService()

	if _, err := svc.StockOutForCompany(1, 1, 100, "C1", "company 1 sale", "main", "tester"); err != nil {
		t.Fatalf("stock out company 1: %v", err)
	}
	if _, err := svc.StockInForCompany(2, 1, 250, valueobject.NewMoney(150000), "C2", "company 2 purchase", "main", "tester"); err != nil {
		t.Fatalf("stock in company 2: %v", err)
	}

	companyOneItem := svc.GetItemForCompany(1, 1)
	companyTwoItem := svc.GetItemForCompany(2, 1)
	if companyOneItem == nil || companyTwoItem == nil {
		t.Fatal("expected seeded inventory items for both companies")
	}
	if companyOneItem.QtyOnHand != 4900 {
		t.Fatalf("company 1 stock changed unexpectedly: %.2f", companyOneItem.QtyOnHand)
	}
	if companyTwoItem.QtyOnHand != 5250 {
		t.Fatalf("company 2 stock changed unexpectedly: %.2f", companyTwoItem.QtyOnHand)
	}
}

func TestInvoiceServiceKeepsCompanyInvoicesIsolated(t *testing.T) {
	t.Setenv("SEED_DEMO_DATA", "true")
	svc := service.NewInvoiceService()

	inv := svc.CreateInvoiceForCompany(2, 99, "company 2 customer", []service.InvoiceLineRequest{
		{ItemID: 1, ItemName: "fabric", Description: "tenant scoped", Qty: 2, Unit: "Meter", UnitPrice: 1000},
	})

	companyOneInvoices := svc.GetInvoicesForCompany(1)
	companyTwoInvoices := svc.GetInvoicesForCompany(2)

	if len(companyOneInvoices) != 3 {
		t.Fatalf("company 1 sample invoices changed: got %d", len(companyOneInvoices))
	}
	if len(companyTwoInvoices) != 4 {
		t.Fatalf("company 2 invoices missing new invoice or samples: got %d", len(companyTwoInvoices))
	}
	if got := svc.GetInvoiceForCompany(1, inv.ID); got != nil && got.CustomerName == "company 2 customer" {
		t.Fatalf("company 2 invoice leaked into company 1: %#v", got)
	}
	if got := svc.GetInvoiceForCompany(2, inv.ID); got == nil || got.CustomerName != "company 2 customer" {
		t.Fatalf("company 2 invoice not found in company 2 scope: %#v", got)
	}
}
