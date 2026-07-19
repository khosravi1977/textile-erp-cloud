package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/erpsystem/textile-erp/internal/domain/entity"
	"github.com/erpsystem/textile-erp/internal/domain/valueobject"
)

// CostService handles cost management logic
type CostService struct {
	mu      sync.RWMutex
	costs   map[int64][]entity.Cost
	nextIDs map[int64]int64
}

// NewCostService creates a new cost service
func NewCostService() *CostService {
	return &CostService{
		costs:   make(map[int64][]entity.Cost),
		nextIDs: make(map[int64]int64),
	}
}

// AddCost records a new cost
func (cs *CostService) AddCost(cost entity.Cost) entity.Cost {
	return cs.AddCostForCompany(1, cost)
}

// AddCostForCompany records a new cost for one tenant.
func (cs *CostService) AddCostForCompany(companyID int64, cost entity.Cost) entity.Cost {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	companyID = normalizedCompanyID(companyID)
	cs.nextIDs[companyID]++
	cost.ID = cs.nextIDs[companyID]
	cost.CreatedAt = time.Now()
	cs.costs[companyID] = append(cs.costs[companyID], cost)
	return cost
}

// GetCosts returns all costs
func (cs *CostService) GetCosts() []entity.Cost {
	return cs.GetCostsForCompany(1)
}

// GetCostsForCompany returns costs for one tenant.
func (cs *CostService) GetCostsForCompany(companyID int64) []entity.Cost {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	costs := cs.costs[normalizedCompanyID(companyID)]
	return append([]entity.Cost(nil), costs...)
}

// GetCostsByCategory returns costs filtered by category
func (cs *CostService) GetCostsByCategory(category entity.CostCategory) []entity.Cost {
	return cs.GetCostsByCategoryForCompany(1, category)
}

// GetCostsByCategoryForCompany returns tenant-scoped costs filtered by category.
func (cs *CostService) GetCostsByCategoryForCompany(companyID int64, category entity.CostCategory) []entity.Cost {
	var result []entity.Cost
	for _, c := range cs.GetCostsForCompany(companyID) {
		if c.Category == category {
			result = append(result, c)
		}
	}
	return result
}

// GetSummary returns cost summary for a period
func (cs *CostService) GetSummary(days int) entity.CostSummary {
	return cs.GetSummaryForCompany(1, days)
}

// GetSummaryForCompany returns a tenant-scoped cost summary for a period.
func (cs *CostService) GetSummaryForCompany(companyID int64, days int) entity.CostSummary {
	cutoff := time.Now().AddDate(0, 0, -days)
	summary := entity.CostSummary{
		ByCategory:       make(map[entity.CostCategory]valueobject.Money),
		PeriodStart:      cutoff,
		PeriodEnd:        time.Now(),
		TransactionCount: 0,
	}

	total := valueobject.Zero()
	for _, c := range cs.GetCostsForCompany(companyID) {
		if c.CostDate.After(cutoff) {
			total = total.Add(c.Amount)
			summary.ByCategory[c.Category] = summary.ByCategory[c.Category].Add(c.Amount)
			summary.TransactionCount++
		}
	}
	summary.TotalCosts = total
	return summary
}

// GetProfitability calculates profit/loss vs revenue
func (cs *CostService) GetProfitability(revenue valueobject.Money, days int) map[string]interface{} {
	return cs.GetProfitabilityForCompany(1, revenue, days)
}

// GetProfitabilityForCompany calculates tenant-scoped profit/loss vs revenue.
func (cs *CostService) GetProfitabilityForCompany(companyID int64, revenue valueobject.Money, days int) map[string]interface{} {
	summary := cs.GetSummaryForCompany(companyID, days)
	profit := revenue.Subtract(summary.TotalCosts)

	var marginPercent float64
	if revenue.ToRials() > 0 {
		marginPercent = (profit.ToToman() / revenue.ToToman()) * 100
	}

	return map[string]interface{}{
		"revenue":        revenue.ToToman(),
		"total_costs":    summary.TotalCosts.ToToman(),
		"profit":         profit.ToToman(),
		"margin_percent": fmt.Sprintf("%.1f%%", marginPercent),
		"is_profitable":  !profit.IsNegative(),
		"period_days":    days,
	}
}

func normalizedCompanyID(companyID int64) int64 {
	if companyID <= 0 {
		return 1
	}
	return companyID
}
