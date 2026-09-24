package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hazop-safeguard-coverage/backend/internal/constants"
	"hazop-safeguard-coverage/backend/internal/dto"
	"hazop-safeguard-coverage/backend/internal/model"
	"hazop-safeguard-coverage/backend/internal/repository"
	"hazop-safeguard-coverage/backend/internal/util"

	"gorm.io/gorm"
)

func closureFixture(t *testing.T) (*gorm.DB, repository.ProcessNodeRepository, repository.AuditRepository, model.ProcessNode, model.DeviationScenario) {
	t.Helper()
	db := testDB(t)
	nodeRepo := repository.NewProcessNodeRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	node := model.ProcessNode{
		NodeCode: "N-900", Name: "Closure Node", UnitName: "Closure Unit", Medium: "gas",
		DesignPressure: 2, DesignTemperature: 80, OwnerTeam: "Reaction Safety",
		Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := nodeRepo.Create(context.Background(), &node); err != nil {
		t.Fatalf("create node: %v", err)
	}
	scenario := model.DeviationScenario{
		ProcessNodeID: node.ID, Guideword: "more", Parameter: "pressure",
		Cause: "outlet blocked", Consequence: "rupture", Likelihood: 4, Severity: 5,
		ScenarioState: "analyzed", Version: 1, CreatedBy: 1, CreatedByName: "engineer",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&scenario).Error; err != nil {
		t.Fatalf("create scenario: %v", err)
	}
	return db, nodeRepo, auditRepo, node, scenario
}

func findCategory(check dto.DeactivationCheckResponse, key string) dto.ClosureCategoryResponse {
	for _, category := range check.Categories {
		if category.Key == key {
			return category
		}
	}
	return dto.ClosureCategoryResponse{}
}

func TestDeactivationBlockedByOpenDeviation(t *testing.T) {
	_, nodeRepo, auditRepo, node, _ := closureFixture(t)
	svc := NewProcessNodeService(nodeRepo, auditRepo)
	check, err := svc.DeactivationCheck(context.Background(), node.ID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if check.CanDeactivate {
		t.Fatalf("node with open scenario should not be deactivatable")
	}
	open := findCategory(check, dto.ClosureCategoryOpenDeviation)
	if open.Count != 1 || open.HighestRisk != 20 || open.OwnerTeam != "Reaction Safety" {
		t.Fatalf("unexpected open category: %+v", open)
	}
	pending := findCategory(check, dto.ClosureCategoryPendingCoverage)
	if pending.Count != 1 || pending.Items[0].Reason != "coverage_not_evaluated" {
		t.Fatalf("unevaluated scenario should block coverage: %+v", pending)
	}
}

func TestDeactivationBlockedByExpiredSafeguardAndUnconfirmedCoverage(t *testing.T) {
	db, nodeRepo, auditRepo, node, scenario := closureFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	verifiedAt := now.AddDate(0, 0, -400)
	expired := model.Safeguard{
		Name: "Expired trip", SafeguardType: "interlock", TargetScenarioID: scenario.ID,
		IndependenceKey: "SIS-EXP", Effectiveness: 0.9, TestIntervalDays: 365,
		LastVerifiedAt: &verifiedAt, LifecycleState: "expired", EvidenceNote: "old cert",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&expired).Error; err != nil {
		t.Fatalf("create safeguard: %v", err)
	}
	evaluation := model.CoverageEvaluation{
		ScenarioID: scenario.ID, AlgorithmVersion: "v1", InputSnapshot: "{}", InputHash: "h",
		CoverageScore: 0.5, UncoveredPaths: "[]", DeduplicatedSafeguards: "[]",
		RiskRankBefore: "critical", RiskRankAfter: "high",
		EvaluationState: string(constants.CoverageCompleted), Explanation: "{}",
		EvaluatedBy: 2, EvaluatedByName: "reviewer", EvaluatedAt: now,
		CreatedAt: now, UpdatedAt: now, IdempotencyKey: "key-0001-closure",
	}
	if err := db.Create(&evaluation).Error; err != nil {
		t.Fatalf("create evaluation: %v", err)
	}
	svc := NewProcessNodeService(nodeRepo, auditRepo)
	check, err := svc.DeactivationCheck(context.Background(), node.ID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if check.CanDeactivate || check.BlockingItemCount != 3 {
		t.Fatalf("expected 3 blockers (deviation+expired+pending), got %+v", check)
	}
	expiredCategory := findCategory(check, dto.ClosureCategoryExpiredLayer)
	if expiredCategory.Count != 1 || expiredCategory.Items[0].Reference != "Expired trip" ||
		expiredCategory.Items[0].Reason != "verification_expired" || expiredCategory.HighestRisk != 20 {
		t.Fatalf("unexpected expired category: %+v", expiredCategory)
	}
	pending := findCategory(check, dto.ClosureCategoryPendingCoverage)
	if pending.Count != 1 || pending.Items[0].State != "completed" {
		t.Fatalf("completed-but-unconfirmed evaluation should block: %+v", pending)
	}
}

func TestDeactivationSucceedsOnceClosureItemsCleared(t *testing.T) {
	db, nodeRepo, auditRepo, node, scenario := closureFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	verifiedAt := now.AddDate(0, 0, -10)
	safeguard := model.Safeguard{
		Name: "Healthy trip", SafeguardType: "interlock", TargetScenarioID: scenario.ID,
		IndependenceKey: "SIS-OK", Effectiveness: 0.9, TestIntervalDays: 365,
		LastVerifiedAt: &verifiedAt, LifecycleState: "active", EvidenceNote: "fresh cert",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&safeguard).Error; err != nil {
		t.Fatalf("create safeguard: %v", err)
	}
	if err := db.Model(&model.DeviationScenario{}).Where("id = ?", scenario.ID).
		Update("scenario_state", "accepted").Error; err != nil {
		t.Fatalf("accept scenario: %v", err)
	}
	evaluation := model.CoverageEvaluation{
		ScenarioID: scenario.ID, AlgorithmVersion: "v1", InputSnapshot: "{}", InputHash: "h2",
		CoverageScore: 0.9, UncoveredPaths: "[]", DeduplicatedSafeguards: "[]",
		RiskRankBefore: "critical", RiskRankAfter: "low",
		EvaluationState: string(constants.CoverageConfirmed), Explanation: "{}",
		EvaluatedBy: 2, EvaluatedByName: "reviewer", EvaluatedAt: now,
		ConfirmedBy: ptrUint(2), ConfirmedAt: &now,
		CreatedAt: now, UpdatedAt: now, IdempotencyKey: "key-0002-closure",
	}
	if err := db.Create(&evaluation).Error; err != nil {
		t.Fatalf("create evaluation: %v", err)
	}
	svc := NewProcessNodeService(nodeRepo, auditRepo)
	check, err := svc.DeactivationCheck(context.Background(), node.ID)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !check.CanDeactivate {
		t.Fatalf("cleared node should be deactivatable: %+v", check.Categories)
	}
	actor := util.Actor{UserID: 1, Username: "engineer", Role: "admin", RequestID: "req-ok"}
	result, err := svc.Deactivate(context.Background(), node.ID, actor)
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if result.Status != "inactive" {
		t.Fatalf("expected inactive, got %s", result.Status)
	}
	var count int64
	db.Model(&model.AuditLog{}).Where("entity_id = ? AND action = ?", node.ID, "deactivate").Count(&count)
	if count != 1 {
		t.Fatalf("expected one deactivate audit, got %d", count)
	}
}

func TestBlockedDeactivationKeepsNodeActiveAndAudits(t *testing.T) {
	db, nodeRepo, auditRepo, node, _ := closureFixture(t)
	svc := NewProcessNodeService(nodeRepo, auditRepo)
	actor := util.Actor{UserID: 1, Username: "engineer", Role: "admin", RequestID: "req-blocked"}
	_, err := svc.Deactivate(context.Background(), node.ID, actor)
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.Code != util.CodeClosureBlocked || appErr.Details == nil {
		t.Fatalf("expected closure blocked error with details, got %v", err)
	}
	reloaded, err := nodeRepo.GetByID(context.Background(), node.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Status != "active" {
		t.Fatalf("node status changed despite block: %s", reloaded.Status)
	}
	var count int64
	db.Model(&model.AuditLog{}).Where("entity_id = ? AND action = ?", node.ID, "deactivate_blocked").Count(&count)
	if count != 1 {
		t.Fatalf("expected one deactivate_blocked audit, got %d", count)
	}
}

func ptrUint(value uint) *uint { return &value }
