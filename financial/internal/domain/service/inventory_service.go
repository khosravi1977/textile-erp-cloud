package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// InventoryItem represents a stock item
type InventoryItem struct {
	ID          int64             `json:"id"`
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	Type        string            `json:"type"` // Yarn, Fabric, RawMaterial, Product, Waste
	Unit        string            `json:"unit"` // KG, Meter, Roll, Piece
	QtyOnHand   float64           `json:"qty_on_hand"`
	QtyReserved float64           `json:"qty_reserved"`
	UnitCost    valueobject.Money `json:"unit_cost"`
	MinStock    float64           `json:"min_stock"`
	MaxStock    float64           `json:"max_stock"`
	Warehouse   string            `json:"warehouse"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// InventoryTransaction represents stock movement
type InventoryTransaction struct {
	ID          int64             `json:"id"`
	ItemID      int64             `json:"item_id"`
	Type        string            `json:"type"` // IN, OUT, TRANSFER, ADJUSTMENT
	Qty         float64           `json:"qty"`
	UnitCost    valueobject.Money `json:"unit_cost"`
	TotalValue  valueobject.Money `json:"total_value"`
	ReferenceNo string            `json:"reference_no"`
	Description string            `json:"description"`
	Warehouse   string            `json:"warehouse"`
	CreatedBy   string            `json:"created_by"`
	CreatedAt   time.Time         `json:"created_at"`
}

// InventoryService handles all inventory operations
type InventoryService struct {
	mu           sync.RWMutex
	items        map[int64][]InventoryItem
	transactions map[int64][]InventoryTransaction
	nextItemID   map[int64]int64
	nextTxnID    map[int64]int64
}

// NewInventoryService creates a new inventory service
func NewInventoryService() *InventoryService {
	is := &InventoryService{
		items:        make(map[int64][]InventoryItem),
		transactions: make(map[int64][]InventoryTransaction),
		nextItemID:   make(map[int64]int64),
		nextTxnID:    make(map[int64]int64),
	}

	is.seedCompany(1)
	return is
}

// AddItem adds a new inventory item
func (is *InventoryService) AddItem(item InventoryItem) InventoryItem {
	return is.AddItemForCompany(1, item)
}

// AddItemForCompany adds a new inventory item for one tenant.
func (is *InventoryService) AddItemForCompany(companyID int64, item InventoryItem) InventoryItem {
	is.mu.Lock()
	defer is.mu.Unlock()

	companyID = normalizedCompanyID(companyID)
	if is.nextItemID[companyID] == 0 {
		is.nextItemID[companyID] = 1
	}
	item.ID = is.nextItemID[companyID]
	is.nextItemID[companyID]++
	item.UpdatedAt = time.Now()
	is.items[companyID] = append(is.items[companyID], item)
	return item
}

// GetItems returns all inventory items
func (is *InventoryService) GetItems() []InventoryItem {
	return is.GetItemsForCompany(1)
}

// GetItemsForCompany returns all inventory items for one tenant.
func (is *InventoryService) GetItemsForCompany(companyID int64) []InventoryItem {
	is.ensureCompanySeeded(companyID)
	is.mu.RLock()
	defer is.mu.RUnlock()

	items := is.items[normalizedCompanyID(companyID)]
	return append([]InventoryItem(nil), items...)
}

// GetItem returns a specific item
func (is *InventoryService) GetItem(id int64) *InventoryItem {
	return is.GetItemForCompany(1, id)
}

// GetItemForCompany returns a specific tenant-scoped item.
func (is *InventoryService) GetItemForCompany(companyID, id int64) *InventoryItem {
	is.ensureCompanySeeded(companyID)
	is.mu.RLock()
	defer is.mu.RUnlock()

	companyID = normalizedCompanyID(companyID)
	for _, item := range is.items[companyID] {
		if item.ID == id {
			itemCopy := item
			return &itemCopy
		}
	}
	return nil
}

// StockIn adds stock to inventory
func (is *InventoryService) StockIn(itemID int64, qty float64, unitCost valueobject.Money, referenceNo, description, warehouse, createdBy string) (*InventoryTransaction, error) {
	return is.StockInForCompany(1, itemID, qty, unitCost, referenceNo, description, warehouse, createdBy)
}

// StockInForCompany adds stock to tenant-scoped inventory.
func (is *InventoryService) StockInForCompany(companyID, itemID int64, qty float64, unitCost valueobject.Money, referenceNo, description, warehouse, createdBy string) (*InventoryTransaction, error) {
	is.ensureCompanySeeded(companyID)
	is.mu.Lock()
	defer is.mu.Unlock()

	companyID = normalizedCompanyID(companyID)
	item := is.itemRef(companyID, itemID)
	if item == nil {
		return nil, fmt.Errorf("item not found: %d", itemID)
	}

	item.QtyOnHand += qty
	if unitCost.ToRials() > 0 {
		totalOld := item.UnitCost.Multiply(item.QtyOnHand - qty)
		totalNew := unitCost.Multiply(qty)
		item.UnitCost = totalOld.Add(totalNew).Multiply(1.0 / item.QtyOnHand)
	}
	item.UpdatedAt = time.Now()

	txn := InventoryTransaction{
		ID:          is.nextTransactionID(companyID),
		ItemID:      itemID,
		Type:        "IN",
		Qty:         qty,
		UnitCost:    unitCost,
		TotalValue:  unitCost.Multiply(qty),
		ReferenceNo: referenceNo,
		Description: description,
		Warehouse:   warehouse,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now(),
	}
	is.transactions[companyID] = append(is.transactions[companyID], txn)

	return &txn, nil
}

// StockOut removes stock from inventory
func (is *InventoryService) StockOut(itemID int64, qty float64, referenceNo, description, warehouse, createdBy string) (*InventoryTransaction, error) {
	return is.StockOutForCompany(1, itemID, qty, referenceNo, description, warehouse, createdBy)
}

// StockOutForCompany removes stock from tenant-scoped inventory.
func (is *InventoryService) StockOutForCompany(companyID, itemID int64, qty float64, referenceNo, description, warehouse, createdBy string) (*InventoryTransaction, error) {
	is.ensureCompanySeeded(companyID)
	is.mu.Lock()
	defer is.mu.Unlock()

	companyID = normalizedCompanyID(companyID)
	item := is.itemRef(companyID, itemID)
	if item == nil {
		return nil, fmt.Errorf("item not found: %d", itemID)
	}

	if item.QtyOnHand < qty {
		return nil, fmt.Errorf("insufficient stock: have %.2f, need %.2f", item.QtyOnHand, qty)
	}

	item.QtyOnHand -= qty
	item.UpdatedAt = time.Now()

	txn := InventoryTransaction{
		ID:          is.nextTransactionID(companyID),
		ItemID:      itemID,
		Type:        "OUT",
		Qty:         qty,
		UnitCost:    item.UnitCost,
		TotalValue:  item.UnitCost.Multiply(qty),
		ReferenceNo: referenceNo,
		Description: description,
		Warehouse:   warehouse,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now(),
	}
	is.transactions[companyID] = append(is.transactions[companyID], txn)

	if item.QtyOnHand <= item.MinStock {
		fmt.Printf("LOW STOCK ALERT: %s (%.2f %s remaining, min: %.2f)\n",
			item.Name, item.QtyOnHand, item.Unit, item.MinStock)
	}

	return &txn, nil
}

// GetTransactions returns all transactions
func (is *InventoryService) GetTransactions() []InventoryTransaction {
	return is.GetTransactionsForCompany(1)
}

// GetTransactionsForCompany returns all tenant-scoped transactions.
func (is *InventoryService) GetTransactionsForCompany(companyID int64) []InventoryTransaction {
	is.ensureCompanySeeded(companyID)
	is.mu.RLock()
	defer is.mu.RUnlock()

	txns := is.transactions[normalizedCompanyID(companyID)]
	return append([]InventoryTransaction(nil), txns...)
}

// GetTransactionsByItem returns transactions for a specific item
func (is *InventoryService) GetTransactionsByItem(itemID int64) []InventoryTransaction {
	return is.GetTransactionsByItemForCompany(1, itemID)
}

// GetTransactionsByItemForCompany returns tenant-scoped transactions for a specific item.
func (is *InventoryService) GetTransactionsByItemForCompany(companyID, itemID int64) []InventoryTransaction {
	var result []InventoryTransaction
	for _, t := range is.GetTransactionsForCompany(companyID) {
		if t.ItemID == itemID {
			result = append(result, t)
		}
	}
	return result
}

// GetStockSummary returns inventory summary
func (is *InventoryService) GetStockSummary() map[string]interface{} {
	return is.GetStockSummaryForCompany(1)
}

// GetStockSummaryForCompany returns tenant-scoped inventory summary.
func (is *InventoryService) GetStockSummaryForCompany(companyID int64) map[string]interface{} {
	items := is.GetItemsForCompany(companyID)
	txns := is.GetTransactionsForCompany(companyID)
	totalValue := valueobject.Zero()
	totalItems := 0
	lowStock := 0
	outOfStock := 0

	for _, item := range items {
		totalItems++
		totalValue = totalValue.Add(item.UnitCost.Multiply(item.QtyOnHand))
		if item.QtyOnHand <= 0 {
			outOfStock++
		} else if item.QtyOnHand <= item.MinStock {
			lowStock++
		}
	}

	return map[string]interface{}{
		"total_items":  totalItems,
		"total_value":  totalValue.ToToman(),
		"low_stock":    lowStock,
		"out_of_stock": outOfStock,
		"transactions": len(txns),
	}
}

// GetStockAlerts returns items that need attention
func (is *InventoryService) GetStockAlerts() []map[string]interface{} {
	return is.GetStockAlertsForCompany(1)
}

// GetStockAlertsForCompany returns tenant-scoped stock alerts.
func (is *InventoryService) GetStockAlertsForCompany(companyID int64) []map[string]interface{} {
	var alerts []map[string]interface{}
	for _, item := range is.GetItemsForCompany(companyID) {
		if item.QtyOnHand <= item.MinStock {
			severity := "warning"
			if item.QtyOnHand <= 0 {
				severity = "danger"
			}
			alerts = append(alerts, map[string]interface{}{
				"item_id":   item.ID,
				"item_name": item.Name,
				"qty":       item.QtyOnHand,
				"min_stock": item.MinStock,
				"severity":  severity,
				"message":   fmt.Sprintf("%s: %.2f %s remaining (min: %.2f)", item.Name, item.QtyOnHand, item.Unit, item.MinStock),
			})
		}
	}
	return alerts
}

func (is *InventoryService) ensureCompanySeeded(companyID int64) {
	companyID = normalizedCompanyID(companyID)
	is.mu.RLock()
	seeded := len(is.items[companyID]) > 0
	is.mu.RUnlock()
	if seeded {
		return
	}
	is.seedCompany(companyID)
}

func (is *InventoryService) seedCompany(companyID int64) {
	samples := []InventoryItem{
		{Code: "YARN-POLY", Name: "نخ پلی استر", Type: "Yarn", Unit: "KG", QtyOnHand: 5000, UnitCost: valueobject.NewMoney(150000), MinStock: 500, MaxStock: 10000, Warehouse: "انبار اصلی"},
		{Code: "YARN-COTT", Name: "نخ پنبه", Type: "Yarn", Unit: "KG", QtyOnHand: 3000, UnitCost: valueobject.NewMoney(200000), MinStock: 300, MaxStock: 8000, Warehouse: "انبار اصلی"},
		{Code: "FAB-POLY", Name: "پارچه پلی استر", Type: "Fabric", Unit: "Meter", QtyOnHand: 2000, UnitCost: valueobject.NewMoney(250000), MinStock: 200, MaxStock: 5000, Warehouse: "انبار محصول"},
		{Code: "FAB-COTT", Name: "پارچه نخی", Type: "Fabric", Unit: "Meter", QtyOnHand: 1500, UnitCost: valueobject.NewMoney(300000), MinStock: 150, MaxStock: 4000, Warehouse: "انبار محصول"},
	}
	for _, item := range samples {
		is.AddItemForCompany(companyID, item)
	}
}

func (is *InventoryService) itemRef(companyID, itemID int64) *InventoryItem {
	for i := range is.items[companyID] {
		if is.items[companyID][i].ID == itemID {
			return &is.items[companyID][i]
		}
	}
	return nil
}

func (is *InventoryService) nextTransactionID(companyID int64) int64 {
	if is.nextTxnID[companyID] == 0 {
		is.nextTxnID[companyID] = 1
	}
	id := is.nextTxnID[companyID]
	is.nextTxnID[companyID]++
	return id
}
