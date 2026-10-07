package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ aa.Port = (*Store)(nil)

func actionUUID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", aa.ErrUnavailable
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func actionError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return aa.ErrDenied
	}
	return aa.ErrUnavailable
}
func actionGate(ctx context.Context, c *agentfeature.Controller) (agentfeature.Ticket, error) {
	if ctx == nil || ctx.Err() != nil {
		return agentfeature.Ticket{}, aa.ErrUnavailable
	}
	t, e := c.Capture(agentfeature.Enrichment)
	if e != nil {
		return t, aa.ErrUnavailable
	}
	return t, nil
}
func actionSchema(ctx context.Context, tx pgx.Tx) error {
	var n int
	e := tx.QueryRow(ctx, `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace ns ON ns.oid=c.relnamespace WHERE ns.nspname='public' AND t.tgenabled='O' AND ((c.relname='agent_action_approvals' AND t.tgname IN('agent_action_approval_guard','agent_action_atomic_guard')) OR(c.relname='agent_action_dispatches' AND t.tgname='agent_action_dispatch_guard') OR(c.relname='agent_sandbox_writes' AND t.tgname='agent_sandbox_write_guard') OR(c.relname='consent_grants' AND t.tgname='agent_action_grant_guard'))`).Scan(&n)
	if e != nil || n != 5 {
		return aa.ErrUnavailable
	}
	_, e = tx.Exec(ctx, `LOCK TABLE agent_action_approvals,agent_action_dispatches,agent_sandbox_writes,consent_grants,audit_events IN ROW EXCLUSIVE MODE`)
	return actionError(e)
}
func actionPreviewTx(ctx context.Context, tx pgx.Tx, id string, lock bool) (aa.Preview, error) {
	var p aa.Preview
	var binding, payload string
	var approved, consumed, cancelled *time.Time
	q := `SELECT binding_canonical,binding_digest,payload_canonical,approved_at,consumed_at,cancelled_at FROM agent_action_approvals WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	e := tx.QueryRow(ctx, q, id).Scan(&binding, &p.BindingDigest, &payload, &approved, &consumed, &cancelled)
	if e != nil {
		return p, actionError(e)
	}
	if json.Unmarshal([]byte(binding), &p.Binding) != nil || json.Unmarshal([]byte(payload), &p.Proposal) != nil || !aa.ValidBinding(p.Binding, p.Proposal) || aa.Digest(binding) != p.BindingDigest {
		return aa.Preview{}, aa.ErrUnavailable
	}
	p.State = aa.Pending
	if approved != nil {
		var valid bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM consent_grants WHERE id=$1 AND owner_account_id=$2 AND recipient_account_id=$2 AND purpose='OWN_SANDBOX_ACTION' AND resource_type='agent_context' AND resource_id=$3 AND actions=ARRAY['sandbox_write']::text[] AND revision=1 AND revoked_at IS NULL AND expires_at>clock_timestamp())`, p.Binding.GrantID, p.Binding.ActorID, id).Scan(&valid)
		if e != nil {
			return aa.Preview{}, aa.ErrUnavailable
		}
		if valid {
			p.State = aa.Approved
		} else {
			p.State = aa.Cancelled
		}
	}
	if consumed != nil {
		p.State = aa.Consumed
	}
	if cancelled != nil {
		p.State = aa.Cancelled
	}
	return p, nil
}

// Uses the old current source/Session/identity locks, original 065 policy and
// xmin. No private data is sent to a provider or stored in the audit.
func (s *Store) actionCurrent(ctx context.Context, tx pgx.Tx, a agentevent.Access, p aa.Preview) (*nativeToolPolicy, error) {
	cur, e := lockModelConfigurationTask(ctx, tx, a, p.Binding.TaskID, s.devPhoneEnabled)
	if e != nil || cur.sessionID != p.Binding.SessionID || cur.agent.Principal.ID != p.Binding.ActorID || cur.agent.AgentID != p.Binding.AgentID {
		return nil, aa.ErrDenied
	}
	if cur.status != "ACTIVE" || cur.version.Token != p.Binding.SourceVersion {
		return nil, aa.ErrChanged
	}
	var generation, authority string
	e = tx.QueryRow(ctx, `SELECT t.xmin::text,birdtie_sandbox_action_authority($1,$2,$3) FROM agent_tasks t WHERE t.id=$4`, cur.sessionID, p.Binding.ActorID, p.Binding.AgentID, p.Binding.TaskID).Scan(&generation, &authority)
	if e != nil {
		return nil, aa.ErrUnavailable
	}
	if generation != p.Binding.SourceGeneration || authority != p.Binding.AuthorityVersion {
		return nil, aa.ErrChanged
	}
	policy, e := captureNativeToolPolicy(ctx, tx, agentPrivateBinding{cur.sessionID, p.Binding.ActorID, p.Binding.AgentID}, p.Binding.ExpiresAt)
	if e != nil {
		return nil, aa.ErrChanged
	}
	if policy.token != p.Binding.PolicyVersion || agenttool.Restrict(agenttool.SandboxWrite, actorref.Person, policy.level).Denied {
		return nil, aa.ErrChanged
	}
	return policy, nil
}
func actionFinal(ctx context.Context, tx pgx.Tx, id string, c *agentfeature.Controller, t agentfeature.Ticket) error {
	var live bool
	if e := tx.QueryRow(ctx, `SELECT birdtie_sandbox_action_current($1)`, id).Scan(&live); e != nil {
		return aa.ErrUnavailable
	}
	if !live {
		return aa.ErrExpired
	}
	if ctx.Err() != nil || !c.Current(t) {
		return aa.ErrChanged
	}
	return nil
}
func actionCommit(ctx context.Context, tx pgx.Tx, c *agentfeature.Controller, t agentfeature.Ticket) error {
	if ctx.Err() != nil || !c.Current(t) {
		return aa.ErrChanged
	}
	if e := tx.Commit(ctx); e != nil {
		return aa.ErrUnknown
	}
	if ctx.Err() != nil || !c.Current(t) {
		return aa.ErrUnknown
	}
	return nil
}
