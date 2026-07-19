package entity

import (
    "fmt"
    "time"
)

type ProductionStatus string

const (
    ProductionDraft      ProductionStatus = "Draft"
    ProductionReleased   ProductionStatus = "Released"
    ProductionInProgress ProductionStatus = "InProgress"
    ProductionCompleted  ProductionStatus = "Completed"
    ProductionClosed     ProductionStatus = "Closed"
)

type ProductionOrder struct {
    ID               int64             `json:"id"`
    OrderNo          string            `json:"order_no"`
    CustomerPartyID  int64             `json:"customer_party_id"`
    ProductItemID    int64             `json:"product_item_id"`
    WarehouseID      int64             `json:"warehouse_id"`
    StartDate        time.Time         `json:"start_date"`
    EndDate          *time.Time        `json:"end_date,omitempty"`
    Status           ProductionStatus  `json:"status"`
    TotalYarnInput   float64           `json:"total_yarn_input"`
    TotalFabricOutput float64          `json:"total_fabric_output"`
    CreatedAt        time.Time         `json:"created_at"`
    CreatedBy        int64             `json:"created_by"`
}

func NewProductionOrder(orderNo string, customerID, productID, warehouseID int64) *ProductionOrder {
    return &ProductionOrder{
        OrderNo:         orderNo,
        CustomerPartyID: customerID,
        ProductItemID:   productID,
        WarehouseID:     warehouseID,
        Status:          ProductionDraft,
        CreatedAt:       time.Now(),
    }
}

func (po *ProductionOrder) Release() error {
    if po.Status != ProductionDraft {
        return fmt.Errorf("invalid status transition")
    }
    po.Status = ProductionReleased
    po.StartDate = time.Now()
    return nil
}
