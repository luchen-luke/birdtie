package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"time"
)

var _ agenttool.SandboxPort = (*Store)(nil)

// This only prepares a version-bound CONFIRM view. No Call, approved receipt,
// sandbox effect, real message/profile writer or dispatch can be returned.
func (s *Store) CheckOwnSandboxTool(ctx context.Context, a agentevent.Access, prepared agentplanner.PreparedGoal, input agenttool.SandboxProposal, c *agentfeature.Controller) (agenttool.Decision, error) {
	deny := func(code string, e error) (agenttool.Decision, error) {
		return agenttool.Decision{SchemaVersion: agenttool.Schema, Disposition: agenttool.Deny, ReasonCodes: []string{code}}, e
	}
	if ctx == nil || !input.Valid() {
		return deny("INVALID_SANDBOX_PROPOSAL", agenttool.ErrInvalid)
	}
	goal, ok := prepared.(*nativePlannerGoal)
	if !ok || goal == nil || goal.store != s || goal.access != a || !goal.ready || goal.Remaining(time.Now()) <= 0 || input.LogicalOperationID != goal.operation || input.ResourceVersion != goal.source {
		return deny("NATIVE_GOAL_REQUIRED", agenttool.ErrDenied)
	}
	ctx, cancel := context.WithTimeout(ctx, goal.Remaining(time.Now()))
	defer cancel()
	ticket, e := c.Capture(agentfeature.Enrichment)
	if e != nil {
		return deny("DISABLED", agenttool.ErrUnavailable)
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return deny("NATIVE_STORE_UNAVAILABLE", agenttool.ErrUnavailable)
	}
	defer tx.Rollback(ctx)
	current, e := lockModelConfigurationTask(ctx, tx, a, goal.task, s.devPhoneEnabled)
	if e != nil || current.sessionID != goal.session || current.agent.AgentID != goal.agent || current.version.Token != goal.source || input.TargetID != current.agent.Principal.ID {
		return deny("CURRENT_OWNER_TASK_REQUIRED", agenttool.ErrDenied)
	}
	var generation string
	if tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, goal.task).Scan(&generation) != nil || generation != goal.generation {
		return deny("TASK_SOURCE_CHANGED", agenttool.ErrChanged)
	}
	policy, e := captureNativeToolPolicy(ctx, tx, agentPrivateBinding{goal.session, current.agent.Principal.ID, goal.agent}, goal.view.ValidUntil)
	if e != nil {
		return deny("POLICY_UNAVAILABLE_OR_EXPIRED", e)
	}
	restriction := agenttool.Restrict(agenttool.SandboxWrite, actorref.Person, policy.level)
	if restriction.Denied {
		return deny(restriction.Reason, agenttool.ErrDenied)
	}
	var now, until time.Time
	var live bool
	e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() t) SELECT n.t,LEAST(s.expires_at,s.idle_expires_at,$4::timestamptz),s.revoked_at IS NULL AND s.expires_at>n.t AND s.idle_expires_at>n.t AND n.t<$4::timestamptz AND t.xmin::text=$5 AND ($6 OR s.authentication_method<>'dev_phone') FROM sessions s JOIN accounts a ON a.id=s.account_id AND a.status='active' AND a.account_type='person' JOIN agents ag ON ag.principal_account_id=a.id AND ag.status='active' AND ag.agent_type='personal' JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' JOIN agent_tasks t ON t.id=$3 AND t.owner_account_id=a.id AND t.principal_type='person' AND t.acting_user_account_id=a.id CROSS JOIN n WHERE s.id=$1 AND a.id=$2 AND ag.id=$7`, goal.session, policy.owner, goal.task, policy.until, goal.generation, s.devPhoneEnabled, goal.agent).Scan(&now, &until, &live)
	if e != nil || !live || ctx.Err() != nil || goal.Remaining(time.Now()) <= 0 || !c.Current(ticket) {
		return deny("EXPIRED_OR_CHANGED", agenttool.ErrChanged)
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil || !c.Current(ticket) || goal.Remaining(time.Now()) <= 0 {
		return deny("COMMIT_UNCONFIRMED", agenttool.ErrChanged)
	}
	policy.observed = now.UTC()
	policy.until = until.UTC()
	return nativeToolDecision(agenttool.SandboxWrite, input.ActionID, input.LogicalOperationID, input.ResourceVersion, agenttool.Digest(input), policy.owner, policy.agent, policy, agenttool.Confirm, "EXACT_VERSION_APPROVAL_REQUIRED_NO_EFFECT"), nil
}
