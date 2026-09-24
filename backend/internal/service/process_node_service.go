package service

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"hazop-safeguard-coverage/backend/internal/dto"
	"hazop-safeguard-coverage/backend/internal/model"
	"hazop-safeguard-coverage/backend/internal/repository"
	"hazop-safeguard-coverage/backend/internal/util"
	"net/http"
	"sort"
	"strings"
	"time"
)

type ProcessNodeService interface {
	Create(context.Context, dto.CreateProcessNodeRequest, util.Actor) (dto.ProcessNodeResponse, error)
	Get(context.Context, uint) (dto.ProcessNodeResponse, error)
	List(context.Context, dto.ProcessNodeQuery) (dto.ProcessNodeListResponse, error)
	Update(context.Context, uint, dto.UpdateProcessNodeRequest, util.Actor) (dto.ProcessNodeResponse, error)
	Deactivate(context.Context, uint, util.Actor) (dto.ProcessNodeResponse, error)
	DeactivationCheck(context.Context, uint) (dto.DeactivationCheckResponse, error)
}
type processNodeService struct {
	nodes     repository.ProcessNodeRepository
	scenarios repository.DeviationScenarioRepository
	audits    repository.AuditRepository
	now       func() time.Time
}

func NewProcessNodeService(
	nodes repository.ProcessNodeRepository,
	scenarios repository.DeviationScenarioRepository,
	audits repository.AuditRepository,
) ProcessNodeService {
	return &processNodeService{
		nodes:     nodes,
		scenarios: scenarios,
		audits:    audits,
		now:       func() time.Time { return time.Now().UTC() },
	}
}
func (s *processNodeService) Create(
	ctx context.Context,
	request dto.CreateProcessNodeRequest,
	actor util.Actor,
) (dto.ProcessNodeResponse, error) {
	request.Normalize()
	if _, err := s.nodes.GetByCode(ctx, request.NodeCode); err == nil {
		return dto.ProcessNodeResponse{}, util.Conflict("node_code already exists")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to check node code", err)
	}
	now := s.now()
	node := model.ProcessNode{
		NodeCode: request.NodeCode, Name: request.Name, UnitName: request.UnitName,
		Medium: request.Medium, DesignPressure: request.DesignPressure,
		DesignTemperature: request.DesignTemperature, OwnerTeam: request.OwnerTeam,
		Status: "active", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.nodes.Create(ctx, &node); err != nil {
		if uniqueViolation(err) {
			return dto.ProcessNodeResponse{}, util.Conflict("node_code already exists")
		}
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to create process node", err)
	}
	if err := s.recordAudit(ctx, actor, "process_node", node.ID, "create", nil, node, ""); err != nil {
		return dto.ProcessNodeResponse{}, err
	}
	return dto.NewProcessNodeResponse(node, model.ProcessNodeSummary{}), nil
}
func (s *processNodeService) Get(ctx context.Context, id uint) (dto.ProcessNodeResponse, error) {
	node, err := s.nodes.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ProcessNodeResponse{}, util.NotFound("process node")
		}
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load process node", err)
	}
	summary, err := s.nodes.Summary(ctx, id)
	if err != nil {
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load node coverage summary", err)
	}
	return dto.NewProcessNodeResponse(node, summary), nil
}
func (s *processNodeService) List(ctx context.Context, query dto.ProcessNodeQuery) (dto.ProcessNodeListResponse, error) {
	nodes, total, err := s.nodes.List(ctx, query)
	if err != nil {
		return dto.ProcessNodeListResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to list process nodes", err)
	}
	response := dto.ProcessNodeListResponse{
		Items: make([]dto.ProcessNodeResponse, 0, len(nodes)),
		Total: total, Page: query.Page, Size: query.PageSize,
	}
	for _, node := range nodes {
		summary, summaryErr := s.nodes.Summary(ctx, node.ID)
		if summaryErr != nil {
			return dto.ProcessNodeListResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load node coverage summary", summaryErr)
		}
		response.Items = append(response.Items, dto.NewProcessNodeResponse(node, summary))
	}
	return response, nil
}
func (s *processNodeService) Update(
	ctx context.Context,
	id uint,
	request dto.UpdateProcessNodeRequest,
	actor util.Actor,
) (dto.ProcessNodeResponse, error) {
	request.Normalize()
	node, err := s.nodes.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ProcessNodeResponse{}, util.NotFound("process node")
		}
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load process node", err)
	}
	if request.Status != nil && *request.Status == "inactive" && node.Status == "active" {
		check, checkErr := s.DeactivationCheck(ctx, id)
		if checkErr != nil {
			return dto.ProcessNodeResponse{}, checkErr
		}
		if check.Blocked {
			return dto.ProcessNodeResponse{}, blockedDeactivationError(check)
		}
	}
	before := node
	if request.Name != nil {
		node.Name = *request.Name
	}
	if request.UnitName != nil {
		node.UnitName = *request.UnitName
	}
	if request.Medium != nil {
		node.Medium = *request.Medium
	}
	if request.DesignPressure != nil {
		node.DesignPressure = *request.DesignPressure
	}
	if request.DesignTemperature != nil {
		node.DesignTemperature = *request.DesignTemperature
	}
	if request.OwnerTeam != nil {
		node.OwnerTeam = *request.OwnerTeam
	}
	if request.Status != nil {
		node.Status = *request.Status
	}
	node.UpdatedAt = s.now()
	if err := s.nodes.Update(ctx, &node); err != nil {
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to update process node", err)
	}
	action := "update"
	summary := ""
	if before.Status != node.Status && node.Status == "inactive" {
		action = "deactivate"
		summary = "deactivation closure check passed"
	}
	if err := s.recordAudit(ctx, actor, "process_node", node.ID, action, before, node, summary); err != nil {
		return dto.ProcessNodeResponse{}, err
	}
	summaryResult, err := s.nodes.Summary(ctx, id)
	if err != nil {
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load node coverage summary", err)
	}
	return dto.NewProcessNodeResponse(node, summaryResult), nil
}
func (s *processNodeService) Deactivate(ctx context.Context, id uint, actor util.Actor) (dto.ProcessNodeResponse, error) {
	node, err := s.nodes.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.ProcessNodeResponse{}, util.NotFound("process node")
		}
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load process node", err)
	}
	if node.Status == "inactive" {
		return dto.ProcessNodeResponse{}, util.NewError(http.StatusConflict, util.CodeStateTransition, "process node is already inactive")
	}
	check, err := s.DeactivationCheck(ctx, id)
	if err != nil {
		return dto.ProcessNodeResponse{}, err
	}
	if check.Blocked {
		return dto.ProcessNodeResponse{}, blockedDeactivationError(check)
	}
	before := node
	changed, err := s.nodes.Deactivate(ctx, id)
	if err != nil {
		return dto.ProcessNodeResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to deactivate process node", err)
	}
	if !changed {
		return dto.ProcessNodeResponse{}, util.NewError(http.StatusConflict, util.CodeConflict, "process node changed concurrently")
	}
	node.Status = "inactive"
	node.UpdatedAt = s.now()
	if err := s.recordAudit(ctx, actor, "process_node", node.ID, "deactivate", before, node, "deactivation closure check passed"); err != nil {
		return dto.ProcessNodeResponse{}, err
	}
	return s.Get(ctx, id)
}

// DeactivationCheck runs the explainable closure gate that a node must pass
// before it can be deactivated: unaccepted deviations, expired safeguards and
// coverage evaluations awaiting confirmation all keep the node in its state.
func (s *processNodeService) DeactivationCheck(ctx context.Context, id uint) (dto.DeactivationCheckResponse, error) {
	node, err := s.nodes.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.DeactivationCheckResponse{}, util.NotFound("process node")
		}
		return dto.DeactivationCheckResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load process node", err)
	}
	now := s.now()
	openScenarios, err := s.scenarios.ListOpenByNode(ctx, id)
	if err != nil {
		return dto.DeactivationCheckResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load open deviation scenarios", err)
	}
	expiredSafeguards, err := s.nodes.ListExpiredSafeguardsForNode(ctx, id, now)
	if err != nil {
		return dto.DeactivationCheckResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load expired safeguards", err)
	}
	evaluations, err := s.nodes.ListCoverageEvaluationsForNode(ctx, id)
	if err != nil {
		return dto.DeactivationCheckResponse{}, util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to load coverage evaluations", err)
	}
	scenarioByID := make(map[uint]model.DeviationScenario, len(openScenarios))
	for _, scenario := range openScenarios {
		scenarioByID[scenario.ID] = scenario
	}
	// Only the latest evaluation per scenario decides whether coverage is
	// closed: older completed runs are moot once a newer one is confirmed.
	latestByScenario := make(map[uint]model.CoverageEvaluation)
	for _, evaluation := range evaluations {
		// Evaluations are ordered newest first; the first one seen is the latest.
		if _, exists := latestByScenario[evaluation.ScenarioID]; !exists {
			latestByScenario[evaluation.ScenarioID] = evaluation
		}
	}

	openItems := make([]dto.DeactivationBlockerItem, 0, len(openScenarios))
	for _, scenario := range openScenarios {
		score := scenario.InitialRisk()
		openItems = append(openItems, dto.DeactivationBlockerItem{
			ID:        scenario.ID,
			Title:     fmt.Sprintf("%s %s", strings.ToUpper(scenario.Guideword), scenario.Parameter),
			Detail:    scenario.Consequence,
			State:     scenario.ScenarioState,
			RiskScore: score,
			RiskRank:  dto.RiskRank(score),
			OwnerTeam: node.OwnerTeam,
		})
	}

	expiredItems := make([]dto.DeactivationBlockerItem, 0, len(expiredSafeguards))
	for _, safeguard := range expiredSafeguards {
		scenario, hasScenario := scenarioByID[safeguard.TargetScenarioID]
		score := 0
		consequence := ""
		if hasScenario {
			score = scenario.InitialRisk()
			consequence = scenario.Consequence
		}
		detail := "verification missing or past due"
		if safeguard.LastVerifiedAt == nil {
			detail = "never verified"
		} else if expires := safeguard.VerificationExpiresAt(); expires != nil {
			detail = fmt.Sprintf("verification expired at %s", expires.UTC().Format(time.RFC3339))
		}
		if consequence != "" {
			detail = detail + "; " + consequence
		}
		expiredItems = append(expiredItems, dto.DeactivationBlockerItem{
			ID: safeguard.ID, ScenarioID: safeguard.TargetScenarioID,
			Title: safeguard.Name, Detail: detail, State: safeguard.LifecycleState,
			RiskScore: score, RiskRank: dto.RiskRank(score), OwnerTeam: node.OwnerTeam,
			VerificationExpires: safeguard.VerificationExpiresAt(),
		})
	}

	coverageItems := make([]dto.DeactivationBlockerItem, 0, len(latestByScenario))
	for _, evaluation := range latestByScenario {
		if evaluation.EvaluationState == "confirmed" || evaluation.EvaluationState == "voided" {
			continue
		}
		scenario := evaluation.Scenario
		score := scenario.InitialRisk()
		detail := fmt.Sprintf("evaluation #%d %s; await reviewer confirmation", evaluation.ID, evaluation.EvaluationState)
		if evaluation.EvaluationState == "failed" {
			detail = fmt.Sprintf("evaluation #%d failed and was not voided: %s", evaluation.ID, util.CompactText(evaluation.FailureReason, 200))
		}
		coverageItems = append(coverageItems, dto.DeactivationBlockerItem{
			ID: evaluation.ID, ScenarioID: evaluation.ScenarioID,
			Title:  fmt.Sprintf("%s %s", strings.ToUpper(scenario.Guideword), scenario.Parameter),
			Detail: detail, State: evaluation.EvaluationState,
			RiskScore: score, RiskRank: dto.RiskRank(score), OwnerTeam: node.OwnerTeam,
		})
	}
	sortBlockerItems(openItems)
	sortBlockerItems(expiredItems)
	sortBlockerItems(coverageItems)

	groups := []dto.DeactivationBlockerGroup{
		blockerGroup(dto.DeactivationGateOpenDeviations, openItems, node.OwnerTeam),
		blockerGroup(dto.DeactivationGateExpiredSafeguards, expiredItems, node.OwnerTeam),
		blockerGroup(dto.DeactivationGateUnconfirmedCoverage, coverageItems, node.OwnerTeam),
	}
	total := 0
	for _, group := range groups {
		total += group.Count
	}
	return dto.DeactivationCheckResponse{
		NodeID: node.ID, NodeCode: node.NodeCode, NodeStatus: node.Status,
		Blocked: total > 0, TotalItems: total, Groups: groups, CheckedAt: now,
	}, nil
}

func blockerGroup(key string, items []dto.DeactivationBlockerItem, fallbackTeam string) dto.DeactivationBlockerGroup {
	highest := 0
	teams := make(map[string]struct{})
	for _, item := range items {
		if item.RiskScore > highest {
			highest = item.RiskScore
		}
		team := item.OwnerTeam
		if team == "" {
			team = fallbackTeam
		}
		if team != "" {
			teams[team] = struct{}{}
		}
	}
	teamNames := make([]string, 0, len(teams))
	for team := range teams {
		teamNames = append(teamNames, team)
	}
	sort.Strings(teamNames)
	return dto.DeactivationBlockerGroup{
		Key: key, Count: len(items), HighestRisk: highest,
		HighestRank: dto.RiskRank(highest), OwnerTeams: teamNames, Items: items,
	}
}

func sortBlockerItems(items []dto.DeactivationBlockerItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].RiskScore != items[j].RiskScore {
			return items[i].RiskScore > items[j].RiskScore
		}
		if items[i].ScenarioID != items[j].ScenarioID {
			return items[i].ScenarioID < items[j].ScenarioID
		}
		return items[i].ID < items[j].ID
	})
}

func blockedDeactivationError(check dto.DeactivationCheckResponse) error {
	return util.NewErrorWithDetails(http.StatusConflict, util.CodeDeactivationBlocked,
		fmt.Sprintf("process node %s cannot be deactivated until %d closure item(s) are cleared", check.NodeCode, check.TotalItems),
		check)
}

func (s *processNodeService) recordAudit(
	ctx context.Context,
	actor util.Actor,
	entityType string,
	entityID uint,
	action string,
	before any,
	after any,
	summary string,
) error {
	beforeJSON, err := snapshotJSON(before)
	if err != nil {
		return util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to serialize audit snapshot", err)
	}
	afterJSON, err := snapshotJSON(after)
	if err != nil {
		return util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to serialize audit snapshot", err)
	}
	log := model.AuditLog{
		RequestID: actor.RequestID, ActorID: actor.UserID, ActorName: actor.Username,
		ActorRole: actor.Role, EntityType: entityType, EntityID: entityID, Action: action,
		BeforeSnapshot: beforeJSON, AfterSnapshot: afterJSON,
		ResultSummary: summary, CreatedAt: s.now(),
	}
	if err := s.audits.Record(ctx, log); err != nil {
		return util.WrapError(http.StatusInternalServerError, util.CodeInternal, "unable to record write audit", err)
	}
	return nil
}
func snapshotJSON(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	return util.CanonicalJSON(value)
}
func uniqueViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") || strings.Contains(message, "duplicate")
}
func serviceError(operation string, err error) error {
	return fmt.Errorf("%s: %w", operation, err)
}
