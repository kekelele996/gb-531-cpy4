package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hazop-safeguard-coverage/backend/internal/dto"
	"hazop-safeguard-coverage/backend/internal/model"
	"hazop-safeguard-coverage/backend/internal/repository"
	"hazop-safeguard-coverage/backend/internal/util"
)

func newClosureService(t *testing.T) (ProcessNodeService, repository.ProcessNodeRepository, repository.DeviationScenarioRepository, repository.SafeguardRepository, repository.CoverageEvaluationRepository, model.ProcessNode) {
	t.Helper()
	db := testDB(t)
	nodeRepo := repository.NewProcessNodeRepository(db)
	scenarioRepo := repository.NewDeviationScenarioRepository(db)
	safeguardRepo := repository.NewSafeguardRepository(db)
	evaluationRepo := repository.NewCoverageEvaluationRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	svc := NewProcessNodeService(nodeRepo, scenarioRepo, auditRepo)
	now := time.Now().UTC()
	node := model.ProcessNode{
		NodeCode: "C-201", Name: "Closure Node", UnitName: "Test Unit", Medium: "steam",
		DesignPressure: 2, DesignTemperature: 180, OwnerTeam: "工艺一组", Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := nodeRepo.Create(context.Background(), &node); err != nil {
		t.Fatalf("create node: %v", err)
	}
	return svc, nodeRepo, scenarioRepo, safeguardRepo, evaluationRepo, node
}

func createClosureScenario(t *testing.T, repo repository.DeviationScenarioRepository, nodeID uint, state string, likelihood, severity int) model.DeviationScenario {
	t.Helper()
	now := time.Now().UTC()
	scenario := model.DeviationScenario{
		ProcessNodeID: nodeID, Guideword: "more", Parameter: "pressure",
		Cause: "cooling loss", Consequence: "overpressure rupture",
		Likelihood: likelihood, Severity: severity, ScenarioState: state, Version: 1,
		CreatedBy: 7, CreatedByName: "engineer", CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(context.Background(), &scenario); err != nil {
		t.Fatalf("create scenario: %v", err)
	}
	return scenario
}

func createClosureSafeguard(t *testing.T, repo repository.SafeguardRepository, scenarioID uint, name, lifecycle string, lastVerified *time.Time, intervalDays int) model.Safeguard {
	t.Helper()
	now := time.Now().UTC()
	safeguard := model.Safeguard{
		Name: name, SafeguardType: "interlock", TargetScenarioID: scenarioID,
		IndependenceKey: name, Effectiveness: 0.9, TestIntervalDays: intervalDays,
		LastVerifiedAt: lastVerified, LifecycleState: lifecycle,
		EvidenceNote: "closure gate fixture", CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(context.Background(), &safeguard); err != nil {
		t.Fatalf("create safeguard: %v", err)
	}
	return safeguard
}

func createClosureEvaluation(t *testing.T, repo repository.CoverageEvaluationRepository, scenarioID uint, key, state string, evaluatedAt time.Time) model.CoverageEvaluation {
	t.Helper()
	now := time.Now().UTC()
	evaluation := model.CoverageEvaluation{
		ScenarioID: scenarioID, AlgorithmVersion: "v1", InputSnapshot: "{}",
		InputHash: key, CoverageScore: 0.2, UncoveredPaths: "[]",
		DeduplicatedSafeguards: "[]", RiskRankBefore: "critical", RiskRankAfter: "high",
		EvaluationState: state, Explanation: "fixture", EvaluatedBy: 7,
		EvaluatedByName: "engineer", EvaluatedAt: evaluatedAt, CreatedAt: now, UpdatedAt: now,
		IdempotencyKey: key,
	}
	if err := repo.Create(context.Background(), &evaluation); err != nil {
		t.Fatalf("create evaluation: %v", err)
	}
	return evaluation
}

func findBlockerGroup(t *testing.T, check dto.DeactivationCheckResponse, key string) dto.DeactivationBlockerGroup {
	t.Helper()
	for _, group := range check.Groups {
		if group.Key == key {
			return group
		}
	}
	t.Fatalf("blocker group %s missing", key)
	return dto.DeactivationBlockerGroup{}
}

func TestDeactivationCheckListsAllBlockerTypes(t *testing.T) {
	svc, _, scenarioRepo, safeguardRepo, evaluationRepo, node := newClosureService(t)
	ctx := context.Background()

	openScenario := createClosureScenario(t, scenarioRepo, node.ID, "draft", 5, 5)
	closedScenario := createClosureScenario(t, scenarioRepo, node.ID, "accepted", 1, 1)
	expiredAt := time.Now().UTC().AddDate(-1, 0, 0)
	expiredSafeguard := createClosureSafeguard(t, safeguardRepo, openScenario.ID, "SIS-EXPIRED", "expired", &expiredAt, 30)
	verifiedAt := time.Now().UTC().AddDate(0, -1, 0)
	createClosureSafeguard(t, safeguardRepo, openScenario.ID, "SIS-VALID", "active", &verifiedAt, 365)
	createClosureEvaluation(t, evaluationRepo, closedScenario.ID, "eval-old-completed", "completed", time.Now().Add(-2*time.Hour))
	createClosureEvaluation(t, evaluationRepo, closedScenario.ID, "eval-new-confirmed", "confirmed", time.Now().Add(-1*time.Hour))
	pendingEvaluation := createClosureEvaluation(t, evaluationRepo, openScenario.ID, "eval-open-completed", "completed", time.Now().Add(-30*time.Minute))

	check, err := svc.DeactivationCheck(ctx, node.ID)
	if err != nil {
		t.Fatalf("deactivation check: %v", err)
	}
	if !check.Blocked || check.TotalItems != 3 {
		t.Fatalf("expected 3 blockers, got blocked=%v total=%d", check.Blocked, check.TotalItems)
	}
	openGroup := findBlockerGroup(t, check, dto.DeactivationGateOpenDeviations)
	if openGroup.Count != 1 || openGroup.Items[0].ID != openScenario.ID || openGroup.HighestRisk != 25 || openGroup.HighestRank != "critical" {
		t.Fatalf("unexpected open deviation group: %#v", openGroup)
	}
	expiredGroup := findBlockerGroup(t, check, dto.DeactivationGateExpiredSafeguards)
	if expiredGroup.Count != 1 || expiredGroup.Items[0].ID != expiredSafeguard.ID {
		t.Fatalf("unexpected expired safeguard group: %#v", expiredGroup)
	}
	coverageGroup := findBlockerGroup(t, check, dto.DeactivationGateUnconfirmedCoverage)
	if coverageGroup.Count != 1 || coverageGroup.Items[0].ID != pendingEvaluation.ID {
		t.Fatalf("unexpected coverage group: %#v", coverageGroup)
	}
	for _, group := range check.Groups {
		if group.Count > 0 && (len(group.OwnerTeams) != 1 || group.OwnerTeams[0] != "工艺一组") {
			t.Fatalf("group %s missing owner team: %#v", group.Key, group.OwnerTeams)
		}
	}

	_, err = svc.Deactivate(ctx, node.ID, util.Actor{UserID: 1, Username: "admin", Role: "admin", RequestID: "req-blocked"})
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 || appErr.Code != util.CodeDeactivationBlocked {
		t.Fatalf("deactivation should be blocked with 409 DEACTIVATION_BLOCKED, got %v", err)
	}
	details, ok := appErr.Details.(dto.DeactivationCheckResponse)
	if !ok || !details.Blocked || details.TotalItems != 3 {
		t.Fatalf("blocked error should carry check details, got %#v", appErr.Details)
	}
	reloaded, err := svc.Get(ctx, node.ID)
	if err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if reloaded.Status != "active" {
		t.Fatalf("node state must remain active when blocked, got %s", reloaded.Status)
	}
}

func TestDeactivationProceedsAfterClosureCleared(t *testing.T) {
	svc, nodeRepo, scenarioRepo, safeguardRepo, evaluationRepo, node := newClosureService(t)
	ctx := context.Background()

	openScenario := createClosureScenario(t, scenarioRepo, node.ID, "draft", 4, 4)
	expiredAt := time.Now().UTC().AddDate(-1, 0, 0)
	expiredSafeguard := createClosureSafeguard(t, safeguardRepo, openScenario.ID, "SIS-EXPIRED", "expired", &expiredAt, 30)
	pendingEvaluation := createClosureEvaluation(t, evaluationRepo, openScenario.ID, "eval-pending", "completed", time.Now().Add(-10*time.Minute))

	// PUT status=inactive must go through the same gate and be rejected.
	_, err := svc.Update(ctx, node.ID, dto.UpdateProcessNodeRequest{Status: strPtr("inactive")},
		util.Actor{UserID: 1, Username: "admin", Role: "admin", RequestID: "req-put-blocked"})
	var appErr *util.AppError
	if !errors.As(err, &appErr) || appErr.Code != util.CodeDeactivationBlocked {
		t.Fatalf("update bypass should be blocked, got %v", err)
	}

	// Close every item: accept the scenario, verify the safeguard, confirm the evaluation.
	transitions := []struct{ from, to string }{{"draft", "analyzed"}, {"analyzed", "verified"}, {"verified", "accepted"}}
	version := openScenario.Version
	for _, step := range transitions {
		changed, transitionErr := scenarioRepo.Transition(ctx, openScenario.ID, step.from, step.to, version, nil, "")
		if transitionErr != nil || !changed {
			t.Fatalf("transition %s->%s failed: changed=%v err=%v", step.from, step.to, changed, transitionErr)
		}
		version++
	}
	now := time.Now().UTC()
	changed, err := safeguardRepo.SetLifecycle(ctx, expiredSafeguard.ID, []string{"expired", "active", "pending"}, "active", map[string]any{
		"last_verified_at": now,
	})
	if err != nil || !changed {
		t.Fatalf("verify safeguard failed: changed=%v err=%v", changed, err)
	}
	changed, err = evaluationRepo.Transition(ctx, pendingEvaluation.ID, []string{"completed"}, "confirmed", map[string]any{})
	if err != nil || !changed {
		t.Fatalf("confirm evaluation failed: changed=%v err=%v", changed, err)
	}

	check, err := svc.DeactivationCheck(ctx, node.ID)
	if err != nil {
		t.Fatalf("deactivation check after clearing: %v", err)
	}
	if check.Blocked || check.TotalItems != 0 {
		t.Fatalf("check should pass after items cleared, got %#v", check)
	}
	result, err := svc.Deactivate(ctx, node.ID, util.Actor{UserID: 1, Username: "admin", Role: "admin", RequestID: "req-allowed"})
	if err != nil {
		t.Fatalf("deactivation after closure: %v", err)
	}
	if result.Status != "inactive" {
		t.Fatalf("node should be inactive, got %s", result.Status)
	}
	stored, err := nodeRepo.GetByID(ctx, node.ID)
	if err != nil || stored.Status != "inactive" {
		t.Fatalf("stored node should be inactive, status=%s err=%v", stored.Status, err)
	}
}

func strPtr(value string) *string { return &value }
