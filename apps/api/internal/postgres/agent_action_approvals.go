package postgres

import (
	"context"
	"encoding/json"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"time"
)

func (s *Store) PreviewOwnSandboxAction(ctx context.Context, a agentevent.Access, prepared agentplanner.PreparedGoal, input agenttool.SandboxProposal, c *agentfeature.Controller) (aa.Preview, error) {
	ticket, e := actionGate(ctx, c)
	if e != nil {
		return aa.Preview{}, e
	}
	// Reuses the real old preparation check. Its JSON view is not authority.
	d, e := s.CheckOwnSandboxTool(ctx, a, prepared, input, c)
	if e != nil || d.Disposition != agenttool.Confirm {
		return aa.Preview{}, aa.ErrDenied
	}
	g, ok := prepared.(*nativePlannerGoal)
	if !ok || g == nil || g.store != s || g.access != a {
		return aa.Preview{}, aa.ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, g.Remaining(time.Now()))
	defer cancel()
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return aa.Preview{}, aa.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = actionSchema(ctx, tx); e != nil {
		return aa.Preview{}, e
	}
	cur, e := lockModelConfigurationTask(ctx, tx, a, g.task, s.devPhoneEnabled)
	if e != nil || cur.sessionID != g.session || cur.agent.AgentID != g.agent || cur.version.Token != g.source {
		return aa.Preview{}, aa.ErrChanged
	}
	policy, e := captureNativeToolPolicy(ctx, tx, agentPrivateBinding{g.session, cur.agent.Principal.ID, g.agent}, d.ExpiresAt)
	if e != nil || policy.token != d.PolicyVersion {
		return aa.Preview{}, aa.ErrChanged
	}
	id, e := actionUUID()
	if e != nil {
		return aa.Preview{}, e
	}
	grant, e := actionUUID()
	if e != nil {
		return aa.Preview{}, e
	}
	b := aa.Binding{Schema: aa.Schema, ApprovalID: id, TenantID: cur.agent.Principal.ID, ActorID: cur.agent.Principal.ID, SubjectType: "PERSON", SubjectID: cur.agent.Principal.ID, AgentID: g.agent, SessionID: g.session, TaskID: g.task, LogicalOperationID: input.LogicalOperationID, ActionID: input.ActionID, Tool: agenttool.SandboxWrite, ToolVersion: aa.ToolVersion, TargetID: input.TargetID, PayloadDigest: agenttool.Digest(input), SourceVersion: g.source, SourceGeneration: g.generation, PolicyVersion: policy.token, ConsentPurpose: aa.Purpose, GrantID: grant, GrantRevision: 1, MembershipVersion: "NONE_PERSON_ONLY"}
	e = tx.QueryRow(ctx, `SELECT birdtie_sandbox_action_authority($1,$2,$3),clock_timestamp(),LEAST($4::timestamptz,s.expires_at,s.idle_expires_at) FROM sessions s WHERE s.id=$1`, g.session, b.ActorID, b.AgentID, policy.until).Scan(&b.AuthorityVersion, &b.ObservedAt, &b.ExpiresAt)
	if e != nil {
		return aa.Preview{}, aa.ErrUnavailable
	}
	b.ObservedAt = b.ObservedAt.UTC()
	b.ExpiresAt = b.ExpiresAt.UTC()
	canonical, digest, e := aa.Canonical(b, input)
	if e != nil {
		return aa.Preview{}, e
	}
	payload, _ := json.Marshal(input)
	_, e = tx.Exec(ctx, `INSERT INTO agent_action_approvals(id,owner_id,agent_id,task_id,session_id,grant_id,binding_canonical,binding_digest,payload_canonical,observed_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, b.ActorID, b.AgentID, b.TaskID, b.SessionID, grant, canonical, digest, string(payload), b.ObservedAt, b.ExpiresAt)
	if e != nil {
		return aa.Preview{}, aa.ErrUnavailable
	}
	if e = actionAudit(ctx, tx, b.ActorID, "sandbox_preview", id, "allowed"); e != nil {
		return aa.Preview{}, e
	}
	if e = actionFinal(ctx, tx, id, c, ticket); e != nil {
		return aa.Preview{}, e
	}
	if e = actionCommit(ctx, tx, c, ticket); e != nil {
		return aa.Preview{}, e
	}
	return aa.Preview{Binding: b, BindingDigest: digest, Proposal: input, State: aa.Pending}, nil
}

// Explicit human operation with current Session and the exact inspected digest.
// Neither the model service nor a JSON confirmed/approved flag has this port.
func (s *Store) ApproveOwnSandboxAction(ctx context.Context, a agentevent.Access, id, digest string, c *agentfeature.Controller) (aa.Preview, error) {
	t, e := actionGate(ctx, c)
	if e != nil {
		return aa.Preview{}, e
	}
	if !agentplanner.ValidID(id) || len(digest) != 64 {
		return aa.Preview{}, aa.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return aa.Preview{}, aa.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = actionSchema(ctx, tx); e != nil {
		return aa.Preview{}, e
	}
	p, e := actionPreviewTx(ctx, tx, id, false)
	if e != nil {
		return aa.Preview{}, e
	}
	if _, e = s.actionCurrent(ctx, tx, a, p); e != nil {
		return aa.Preview{}, e
	}
	// Authenticate the actual current owner before comparing a private binding.
	// Foreign callers must not learn whether a guessed digest matched.
	if p.BindingDigest != digest {
		return aa.Preview{}, aa.ErrChanged
	}
	p, e = actionPreviewTx(ctx, tx, id, true)
	if e != nil {
		return aa.Preview{}, e
	}
	if p.State == aa.Consumed {
		return aa.Preview{}, aa.ErrConsumed
	}
	if p.State == aa.Cancelled {
		return aa.Preview{}, aa.ErrDenied
	}
	if p.State != aa.Approved {
		_, e = tx.Exec(ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,purpose,actions,revision,expires_at,created_at) VALUES($1,$2,$2,'agent_context',$3,'OWN_SANDBOX_ACTION',ARRAY['sandbox_write']::text[],1,$4,clock_timestamp())`, p.Binding.GrantID, p.Binding.ActorID, id, p.Binding.ExpiresAt)
		if e != nil {
			return aa.Preview{}, aa.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `UPDATE agent_action_approvals SET approved_at=clock_timestamp() WHERE id=$1`, id); e != nil {
			return aa.Preview{}, aa.ErrUnavailable
		}
		if e = actionAudit(ctx, tx, p.Binding.ActorID, "sandbox_approve", id, "allowed"); e != nil {
			return aa.Preview{}, e
		}
	}
	if e = actionFinal(ctx, tx, id, c, t); e != nil {
		return aa.Preview{}, e
	}
	if e = actionCommit(ctx, tx, c, t); e != nil {
		return aa.Preview{}, e
	}
	p.State = aa.Approved
	return p, nil
}
func (s *Store) CancelOwnSandboxAction(ctx context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (aa.Preview, error) {
	t, e := actionGate(ctx, c)
	if e != nil {
		return aa.Preview{}, e
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return aa.Preview{}, aa.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if e = actionSchema(ctx, tx); e != nil {
		return aa.Preview{}, e
	}
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return aa.Preview{}, aa.ErrDenied
	}
	p, e := actionPreviewTx(ctx, tx, id, true)
	if e != nil || p.Binding.ActorID != owner {
		return aa.Preview{}, aa.ErrDenied
	}
	// A post-commit revoke stops later steps, but cannot undo the commitment or
	// imply that its already-in-flight actual effect did not happen.
	var revoked *time.Time
	e = tx.QueryRow(ctx, `SELECT revoked_at FROM consent_grants WHERE id=$1 FOR UPDATE`, p.Binding.GrantID).Scan(&revoked)
	if e == nil && revoked == nil {
		if _, e = tx.Exec(ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, p.Binding.GrantID); e != nil {
			return aa.Preview{}, aa.ErrUnavailable
		}
	}
	if p.State != aa.Cancelled {
		if _, e = tx.Exec(ctx, `UPDATE agent_action_approvals SET cancelled_at=clock_timestamp() WHERE id=$1`, id); e != nil {
			return aa.Preview{}, aa.ErrUnavailable
		}
		if e = actionAudit(ctx, tx, owner, "sandbox_revoke", id, "allowed"); e != nil {
			return aa.Preview{}, e
		}
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return aa.Preview{}, aa.ErrDenied
	}
	if e = actionCommit(ctx, tx, c, t); e != nil {
		return aa.Preview{}, e
	}
	if p.State != aa.Consumed {
		p.State = aa.Cancelled
	}
	return p, nil
}
