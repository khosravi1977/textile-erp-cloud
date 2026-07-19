package usecase

import (
    "fmt"
    "time"

    "github.com/erpsystem/textile-erp/internal/domain/entity"
    "github.com/erpsystem/textile-erp/internal/domain/service"
    "github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

type ProductionUseCase struct {
    wasteCalc      *service.WasteCalculator
    downtimeCalc   *service.DowntimeCalculator
    commissionCalc *service.CommissionInvoiceCalculator
    advisor        *service.FinancialAdvisor
}

func NewProductionUseCase() *ProductionUseCase {
    wasteCalc := service.NewWasteCalculator()
    downtimeCalc := service.NewDowntimeCalculator(10)
    return &ProductionUseCase{
        wasteCalc:      wasteCalc,
        downtimeCalc:   downtimeCalc,
        commissionCalc: service.NewCommissionInvoiceCalculator(wasteCalc, downtimeCalc),
        advisor:        service.NewFinancialAdvisor(),
    }
}

type CreateOrderRequest struct {
    CustomerID     int64   `json:"customer_id"`
    ProductID      int64   `json:"product_id"`
    WarehouseID    int64   `json:"warehouse_id"`
    YarnQty        float64 `json:"yarn_qty"`
    YarnPrice      float64 `json:"yarn_price"`
    FabricPrice    float64 `json:"fabric_price"`
    StdWasteRate   float64 `json:"std_waste_rate"`
    CommitmentDate string  `json:"commitment_date"`
}

type CreateOrderResponse struct {
    OrderNo        string                    `json:"order_no"`
    Status         string                    `json:"status"`
    Warnings       []string                  `json:"warnings,omitempty"`
    Advice         []service.FinancialAdvice `json:"advice"`
    EstimatedValue float64                   `json:"estimated_value"`
    CreditOK       bool                      `json:"credit_ok"`
}

func (uc *ProductionUseCase) CreateProductionOrder(req CreateOrderRequest) (*CreateOrderResponse, error) {
    orderNo := fmt.Sprintf("PO-%d-%d", req.CustomerID, time.Now().Unix())

    adviceReq := service.AdviceRequest{
        PartyID: req.CustomerID,
        Context: "new_order",
        Data: map[string]float64{
            "yarn_qty":   req.YarnQty,
            "yarn_price": req.YarnPrice,
        },
    }

    profile := &entity.CustomerCreditProfile{
        PartyID:     req.CustomerID,
        CreditLimit: valueobject.NewMoney(300000000),
        CreditDays:  60,
        RiskGroup:   "Low",
        BaseScore:   85,
        IsBlocked:   false,
    }

    advices := uc.advisor.GetAdvice(adviceReq, profile)

    creditOK := true
    var warnings []string
    for _, a := range advices {
        if a.Severity == service.SeverityBlock {
            creditOK = false
            warnings = append(warnings, a.Title+": "+a.Message)
        }
    }

    estimatedValue := req.YarnQty * req.YarnPrice

    if estimatedValue > profile.CreditLimit.ToToman() {
        creditOK = false
        warnings = append(warnings, fmt.Sprintf(
            "Estimated value (%.0f) exceeds credit limit (%.0f)",
            estimatedValue, profile.CreditLimit.ToToman(),
        ))
    }

    return &CreateOrderResponse{
        OrderNo:        orderNo,
        Status:         "created",
        Warnings:       warnings,
        Advice:         advices,
        EstimatedValue: estimatedValue,
        CreditOK:       creditOK,
    }, nil
}

type CompleteProductionRequest struct {
    OrderID       int64   `json:"order_id"`
    YarnInput     float64 `json:"yarn_input"`
    FabricOutput  float64 `json:"fabric_output"`
    DowntimeDays  float64 `json:"downtime_days"`
    DowntimeRate  float64 `json:"downtime_rate"`
    YarnPrice     float64 `json:"yarn_price"`
    FabricPrice   float64 `json:"fabric_price"`
    StdWasteRate  float64 `json:"std_waste_rate"`
}

type CompleteProductionResponse struct {
    OrderID         int64                         `json:"order_id"`
    WasteResult     service.WasteResult           `json:"waste_result"`
    DowntimePenalty float64                       `json:"downtime_penalty"`
    Commission      service.CommissionCalculation `json:"commission"`
    Advice          []service.FinancialAdvice     `json:"advice"`
    Summary         string                        `json:"summary"`
}

func (uc *ProductionUseCase) CompleteProduction(req CompleteProductionRequest) (*CompleteProductionResponse, error) {
    yarnPrice := valueobject.NewMoney(req.YarnPrice)
    fabricPrice := valueobject.NewMoney(req.FabricPrice)
    downtimeRate := valueobject.NewMoney(req.DowntimeRate)

    wasteResult := uc.wasteCalc.Calculate(req.YarnInput, req.FabricOutput, req.StdWasteRate, yarnPrice)

    downtimeResult := uc.downtimeCalc.Calculate(
        time.Now().AddDate(0, 0, -int(req.DowntimeDays)),
        time.Now(),
        downtimeRate,
    )

    commission := uc.commissionCalc.CalculateFromScenario(
        req.YarnInput, req.FabricOutput, yarnPrice, fabricPrice,
        req.StdWasteRate, req.DowntimeDays, downtimeRate,
    )

    adviceReq := service.AdviceRequest{
        PartyID: 1,
        Context: "production_done",
        Data: map[string]float64{
            "excess_waste_rate": wasteResult.ExcessWasteQty / req.YarnInput,
            "downtime_days":     req.DowntimeDays,
        },
    }
    profile := &entity.CustomerCreditProfile{RiskGroup: "Medium", BaseScore: 65}
    advices := uc.advisor.GetAdvice(adviceReq, profile)

    summary := fmt.Sprintf(
        "Production completed: Input=%.0fkg, Output=%.0fkg, Waste=%.0fkg(std)+%.0fkg(excess), Downtime=%.0fdays, Net=%.0f",
        req.YarnInput, req.FabricOutput,
        wasteResult.StdWasteQty, wasteResult.ExcessWasteQty,
        req.DowntimeDays, commission.NetAmount.ToToman(),
    )

    return &CompleteProductionResponse{
        OrderID:         req.OrderID,
        WasteResult:     wasteResult,
        DowntimePenalty: downtimeResult.Amount.ToToman(),
        Commission:      commission,
        Advice:          advices,
        Summary:         summary,
    }, nil
}

type SettlementUseCase struct {
    advisor *service.FinancialAdvisor
}

func NewSettlementUseCase() *SettlementUseCase {
    return &SettlementUseCase{advisor: service.NewFinancialAdvisor()}
}

type SettlementRequest struct {
    PartyID   int64                   `json:"party_id"`
    TotalDebt float64                 `json:"total_debt"`
    Lines     []SettlementLineRequest `json:"lines"`
}

type SettlementLineRequest struct {
    Type         string   `json:"type"`
    Amount       float64  `json:"amount"`
    CheckNo      string   `json:"check_no,omitempty"`
    CheckDueDate string   `json:"check_due_date,omitempty"`
    BankName     string   `json:"bank_name,omitempty"`
    ItemID       *int64   `json:"item_id,omitempty"`
    Qty          *float64 `json:"qty,omitempty"`
}

type SettlementResponse struct {
    SettlementID int64                      `json:"settlement_id"`
    TotalPaid    float64                    `json:"total_paid"`
    Remaining    float64                    `json:"remaining"`
    Advice       []service.FinancialAdvice  `json:"advice"`
    Status       string                     `json:"status"`
}

func (uc *SettlementUseCase) CreateSettlement(req SettlementRequest) (*SettlementResponse, error) {
    var totalPaid float64
    for _, lineReq := range req.Lines {
        totalPaid += lineReq.Amount
    }

    adviceReq := service.AdviceRequest{
        PartyID: req.PartyID,
        Context: "check_payment",
        Data:    map[string]float64{"check_days": 30},
    }
    profile := &entity.CustomerCreditProfile{RiskGroup: "Medium", CreditDays: 30}
    advices := uc.advisor.GetAdvice(adviceReq, profile)

    remaining := req.TotalDebt - totalPaid
    status := "PartiallySettled"
    if remaining <= 0 {
        status = "Settled"
    }

    return &SettlementResponse{
        SettlementID: time.Now().Unix(),
        TotalPaid:    totalPaid,
        Remaining:    remaining,
        Advice:       advices,
        Status:       status,
    }, nil
}