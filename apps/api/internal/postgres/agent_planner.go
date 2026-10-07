package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"time"
)

type nativePlannerGoal struct {
	store                                   *Store
	access                                  agentevent.Access
	session, source, agent, task, operation string
	generation                              string
	ready                                   bool
	view                                    agentplanner.View
	bound                                   modelrequestrun.ClockBound
}

func (g *nativePlannerGoal) View() agentplanner.View { return agentplanner.Clone(g.view) }
func (g *nativePlannerGoal) NeedsModel() bool        { return g != nil && g.ready }
func (g *nativePlannerGoal) Remaining(now time.Time) time.Duration {
	if g == nil {
		return 0
	}
	return g.bound.Remaining(now)
}
func (*nativePlannerGoal) MarshalJSON() ([]byte, error) { return nil, agentplanner.ErrServerOnly }
func (g *nativePlannerGoal) UnmarshalJSON([]byte) error {
	*g = nativePlannerGoal{}
	return agentplanner.ErrServerOnly
}

var _ agentplanner.TaskPort = (*Store)(nil)

// This is an ordinary current owner Task read. The operation selector follows
// the existing metadata-only UserQuery producer: it grants no model purpose,
// budget, approval or action. A ready goal still requires all original 062/088
// approvals; it cannot be recreated from a model or JSON View.
func (s *Store) PrepareOwnReadonlyPlan(ctx context.Context, a agentevent.Access, taskID, operation string) (agentplanner.PreparedGoal, error) {
	if ctx == nil || !agentplanner.ValidID(taskID) || !agentplanner.ValidID(operation) {
		return nil, agentplanner.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	current, e := lockModelConfigurationTask(ctx, tx, a, taskID, s.devPhoneEnabled)
	if e != nil {
		return nil, errors.Join(agentplanner.ErrDenied, e)
	}
	task, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND principal_type='person' FOR SHARE`, taskID, current.agent.Principal.ID))
	if e != nil {
		return nil, agentplanner.ErrDenied
	}
	var generation string
	if tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, taskID).Scan(&generation) != nil {
		return nil, agentplanner.ErrUnavailable
	}
	reason, questions := "model_plan_pending", []string{}
	ready := true
	switch task.Intent {
	case agentworkspace.FindActivity, agentworkspace.AreaDiscovery, agentworkspace.RefineResults, agentworkspace.CompareResults:
		if task.CityID == "" {
			ready = false
			reason = "city_required"
			questions = []string{"你想在哪个城市查找活动？"}
		}
		if (task.Intent == agentworkspace.RefineResults || task.Intent == agentworkspace.CompareResults) && task.Filters["targetIntent"] != agentworkspace.FindActivity {
			ready = false
			reason = "previous_results_required"
			questions = []string{"你想继续筛选哪一轮活动？请先选择原查询。"}
		}
		if task.Intent == agentworkspace.CompareResults && task.Filters["resultIDs"] == "" {
			ready = false
			reason = "entity_selection_required"
			questions = []string{"你想比较哪两个活动？请先选择具体活动。"}
		}
		if task.Intent == agentworkspace.AreaDiscovery {
			b, e := agentworkspace.BoundsFromFilters(task.Filters)
			if e != nil || b == nil {
				ready = false
				reason = "area_required"
				questions = []string{"你想查看地图上的哪片区域？请先选定搜索范围。"}
			}
		}
	default:
		ready = false
		reason = "goal_required"
		questions = []string{"你想查找活动，还是查看已有候选记忆？"}
	}
	if ready && task.Status != agentworkspace.TaskActive {
		ready = false
		reason = "new_query_required"
		questions = []string{"请先发起一次新的活动查询，再审阅当前候选。"}
	}
	if ready && len(task.Query) > 240 {
		return nil, agentplanner.ErrInvalid
	}
	started := time.Now()
	var observed, until time.Time
	e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() instant) SELECT n.instant,LEAST(ses.expires_at,ses.idle_expires_at,n.instant+interval '30 seconds') FROM sessions ses CROSS JOIN n WHERE ses.id=$1 AND ses.revoked_at IS NULL AND ses.expires_at>n.instant AND ses.idle_expires_at>n.instant`, current.sessionID).Scan(&observed, &until)
	if e != nil {
		return nil, agentplanner.ErrDenied
	}
	if d, ok := ctx.Deadline(); ok && d.Before(until) {
		until = d
	}
	bound, e := modelrequestrun.NewClockBound(observed, until, started)
	if e != nil {
		return nil, agentplanner.ErrChanged
	}
	status := agentplanner.Unavailable
	if !ready {
		status = agentplanner.Clarification
	}
	view := agentplanner.NewView(status, taskID, operation, reason, observed, until)
	view.Clarifications = questions
	if !view.Valid(observed) {
		return nil, agentplanner.ErrInvalid
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, agentplanner.ErrUnavailable
	}
	if ctx.Err() != nil || bound.Remaining(time.Now()) <= 0 {
		return nil, agentplanner.ErrChanged
	}
	return &nativePlannerGoal{store: s, access: a, session: current.sessionID, source: current.version.Token, agent: current.agent.AgentID, task: taskID, operation: operation, generation: generation, ready: ready, view: view, bound: bound}, nil
}
