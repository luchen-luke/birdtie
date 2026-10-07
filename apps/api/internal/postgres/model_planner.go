package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"time"
)

var _ modelegressbudget.LocalPlannerPort = (*Store)(nil)
var _ modelegressbudget.LocalPlannerHandle = (*nativeModelRunHandle)(nil)

func (s *Store) CreateOwnLocalPlannerRun(ctx context.Context, a agentevent.Access, id string, bindings []modelegressbudget.LocalRetryBinding, c *agentfeature.Controller, ticket agentfeature.Ticket, prepared agentplanner.PreparedGoal) (modelegressbudget.LocalModelRunHandle, modelrequestrun.Control, error) {
	goal, ok := prepared.(*nativePlannerGoal)
	if !ok || goal == nil || ctx == nil || len(bindings) < 1 || len(bindings) > agentplanner.MaxModelCalls {
		return nil, modelrequestrun.Control{}, agentplanner.ErrDenied
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > agentplanner.MaxElapsed || time.Until(deadline) <= 0 {
		return nil, modelrequestrun.Control{}, agentplanner.ErrLimit
	}
	return s.createOwnLocalModelRun(ctx, a, id, bindings, c, ticket, goal)
}
func (h *nativeModelRunHandle) buildReadonlyPlan(result modelgateway.Result) (agentplanner.View, error) {
	if h.planner == nil || h.outputScope == nil || result.Status != modelgateway.Completed || result.Answer == nil {
		return agentplanner.View{}, modelgateway.ErrOutputSchema
	}
	if len(result.Answer.EntityRefs)+1 > agentplanner.MaxSteps {
		return agentplanner.View{}, agentplanner.ErrLimit
	}
	g := h.planner
	s := h.outputScope
	v := agentplanner.NewView(agentplanner.Proposed, g.task, g.operation, "readonly_candidate_review", g.view.ObservedAt, retryMinimum(g.view.ValidUntil, s.initial.ValidUntil))
	query := ""
	for _, m := range h.proof.routes[0].preview.Request.Messages {
		if m.Role == "user" {
			query = m.Content
		}
	}
	v.Actions = append(v.Actions, agentplanner.Proposal(g.operation, 1, agentplanner.ActivitySearch, agentplanner.Arguments{Query: query, CityID: s.query.CityID}, g.source, "查看当前明确条件下可见的活动候选；尚未执行任何领域操作。"))
	for i, ref := range result.Answer.EntityRefs {
		version := ""
		for _, item := range s.initial.Items {
			if item.Entity.Type == "activity" && item.Entity.ID == ref.ID && item.Detail != nil && item.Detail.ID == ref.ID {
				version = item.SourceVersion
				break
			}
		}
		if version == "" {
			return agentplanner.View{}, modelgateway.ErrOutputEntity
		}
		v.Actions = append(v.Actions, agentplanner.Proposal(g.operation, i+2, agentplanner.ActivityDetail, agentplanner.Arguments{ActivityID: ref.ID}, version, "查看该活动的当前详情，由你检查参与条件；这不是报名或批准。"))
	}
	if g.Remaining(time.Now()) <= 0 || !v.Valid(v.ObservedAt) {
		return agentplanner.View{}, agentplanner.ErrChanged
	}
	return v, nil
}

// Only the exact process-local native handle has this projection. Reading
// historical Control/FINISHED/SETTLED cannot reconstruct it. The final read
// reuses the original transaction/source SQL and ticket, not a new authority.
func (h *nativeModelRunHandle) ReadOwnReadonlyPlan(ctx context.Context, a agentevent.Access, c *agentfeature.Controller) (agentplanner.View, modelegressbudget.LocalReleaseCheckpoint, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var empty modelegressbudget.LocalReleaseCheckpoint
	if h.planner == nil || h.plannerView == nil || ctx == nil || a != h.access || c != h.controller {
		return agentplanner.View{}, empty, agentplanner.ErrDenied
	}
	tx, r, e := h.begin(ctx, a, c, true)
	if e != nil {
		return agentplanner.View{}, empty, e
	}
	defer tx.Rollback(ctx)
	if r.control.State != modelrequestrun.RunFinished {
		return agentplanner.View{}, empty, agentplanner.ErrDenied
	}
	cp, e := h.commit(ctx, tx, r, h.proof.routes[0].binding.Input.OperationID, true)
	if e != nil {
		return agentplanner.View{}, empty, e
	}
	v := agentplanner.Clone(*h.plannerView)
	v.ValidUntil = retryMinimum(v.ValidUntil, cp.ValidUntil())
	if ctx.Err() != nil || !c.Current(h.ticket) || cp.Remaining(time.Now()) <= 0 || !v.Valid(v.ObservedAt) {
		return agentplanner.View{}, empty, agentplanner.ErrChanged
	}
	return v, cp, nil
}
