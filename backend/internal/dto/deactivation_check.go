package dto

import (
	"sort"
	"time"

	"hazop-safeguard-coverage/backend/internal/model"
)

const (
	ClosureCategoryOpenDeviation   = "open_deviation"
	ClosureCategoryExpiredLayer    = "expired_safeguard"
	ClosureCategoryPendingCoverage = "pending_coverage"
)

type DeactivationCheckResponse struct {
	NodeID            uint                      `json:"node_id"`
	NodeCode          string                    `json:"node_code"`
	NodeStatus        string                    `json:"node_status"`
	CheckedAt         time.Time                 `json:"checked_at"`
	CanDeactivate     bool                      `json:"can_deactivate"`
	BlockingItemCount int                       `json:"blocking_item_count"`
	Categories        []ClosureCategoryResponse `json:"categories"`
}

type ClosureCategoryResponse struct {
	Key         string                `json:"key"`
	Label       string                `json:"label"`
	Blocked     bool                  `json:"blocked"`
	Count       int                   `json:"count"`
	HighestRisk int                   `json:"highest_risk"`
	RiskRank    string                `json:"risk_rank"`
	OwnerTeam   string                `json:"owner_team"`
	Items       []ClosureItemResponse `json:"items"`
}

type ClosureItemResponse struct {
	ID         uint   `json:"id"`
	Reference  string `json:"reference"`
	Reason     string `json:"reason"`
	State      string `json:"state"`
	RiskScore  int    `json:"risk_score"`
	RiskRank   string `json:"risk_rank"`
	OwnerTeam  string `json:"owner_team"`
	ScenarioID uint   `json:"scenario_id,omitempty"`
}

type ScenarioRiskInput struct {
	ID        uint
	Reference string
	State     string
	Risk      int
}

type SafeguardExpiryInput struct {
	ID         uint
	ScenarioID uint
	Name       string
	State      string
	Reason     string
	Expired    bool
}

type CoverageLatestInput struct {
	ID    uint
	State string
}

// BuildDeactivationCheck evaluates the three closure gate categories for a node.
func BuildDeactivationCheck(
	node model.ProcessNode,
	scenarios []ScenarioRiskInput,
	safeguards []SafeguardExpiryInput,
	latestByScenario map[uint]CoverageLatestInput,
	now time.Time,
) DeactivationCheckResponse {
	ownerTeam := node.OwnerTeam
	response := DeactivationCheckResponse{
		NodeID: node.ID, NodeCode: node.NodeCode, NodeStatus: node.Status,
		CheckedAt: now, CanDeactivate: true,
	}

	openItems := make([]ClosureItemResponse, 0)
	pendingItems := make([]ClosureItemResponse, 0)
	scenarioRisk := make(map[uint]int, len(scenarios))
	for _, scenario := range scenarios {
		scenarioRisk[scenario.ID] = scenario.Risk
		if scenario.State != "accepted" {
			openItems = append(openItems, ClosureItemResponse{
				ID: scenario.ID, Reference: scenario.Reference,
				Reason: "deviation_not_accepted", State: scenario.State,
				RiskScore: scenario.Risk, RiskRank: RiskRank(scenario.Risk),
				OwnerTeam: ownerTeam, ScenarioID: scenario.ID,
			})
		}
		latest, hasLatest := latestByScenario[scenario.ID]
		if !hasLatest {
			pendingItems = append(pendingItems, ClosureItemResponse{
				ID: scenario.ID, Reference: scenario.Reference,
				Reason: "coverage_not_evaluated", State: "never_run",
				RiskScore: scenario.Risk, RiskRank: RiskRank(scenario.Risk),
				OwnerTeam: ownerTeam, ScenarioID: scenario.ID,
			})
		} else if latest.State != "confirmed" {
			pendingItems = append(pendingItems, ClosureItemResponse{
				ID: latest.ID, Reference: scenario.Reference,
				Reason: "coverage_not_confirmed", State: latest.State,
				RiskScore: scenario.Risk, RiskRank: RiskRank(scenario.Risk),
				OwnerTeam: ownerTeam, ScenarioID: scenario.ID,
			})
		}
	}

	expiredItems := make([]ClosureItemResponse, 0)
	for _, safeguard := range safeguards {
		if !safeguard.Expired {
			continue
		}
		risk := scenarioRisk[safeguard.ScenarioID]
		expiredItems = append(expiredItems, ClosureItemResponse{
			ID: safeguard.ID, Reference: safeguard.Name,
			Reason: safeguard.Reason, State: safeguard.State,
			RiskScore: risk, RiskRank: RiskRank(risk),
			OwnerTeam: ownerTeam, ScenarioID: safeguard.ScenarioID,
		})
	}

	response.Categories = []ClosureCategoryResponse{
		closureCategory(ClosureCategoryOpenDeviation, "未接受偏差", ownerTeam, openItems),
		closureCategory(ClosureCategoryExpiredLayer, "过期保护层", ownerTeam, expiredItems),
		closureCategory(ClosureCategoryPendingCoverage, "待确认覆盖", ownerTeam, pendingItems),
	}
	for _, category := range response.Categories {
		if category.Blocked {
			response.CanDeactivate = false
			response.BlockingItemCount += category.Count
		}
	}
	return response
}

func closureCategory(key, label, ownerTeam string, items []ClosureItemResponse) ClosureCategoryResponse {
	category := ClosureCategoryResponse{
		Key: key, Label: label, OwnerTeam: ownerTeam,
		Items: items, Count: len(items), Blocked: len(items) > 0,
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].RiskScore != items[j].RiskScore {
			return items[i].RiskScore > items[j].RiskScore
		}
		return items[i].ID < items[j].ID
	})
	for _, item := range items {
		if item.RiskScore > category.HighestRisk {
			category.HighestRisk = item.RiskScore
		}
	}
	if len(items) > 0 {
		category.RiskRank = RiskRank(category.HighestRisk)
	}
	return category
}
