package postgres

import (
	"context"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ mp.Store = (*Store)(nil)

func messageOwnError(e error) error {
	if errors.Is(e, agentprofile.ErrForbidden) || errors.Is(e, agentprofile.ErrNotFound) {
		return mp.ErrDenied
	}
	return mp.ErrUnavailable
}
func (s *Store) beginMessageOwn(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, agentPrivateBinding, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, agentPrivateBinding{}, mp.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, agentPrivateBinding{}, mp.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, agentPrivateBinding{}, mp.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, agentPrivateBinding, error) {
		tx.Rollback(context.Background())
		return nil, agentPrivateBinding{}, e
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(mp.ErrUnavailable)
	}
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.WorkspacePrincipal.ID, s.devPhoneEnabled); e != nil {
		return fail(e)
	}
	var id string
	if tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR NO KEY UPDATE`, a.WorkspacePrincipal.ID).Scan(&id) != nil {
		return fail(mp.ErrDenied)
	}
	mode := "SHARE"
	if write {
		mode = "ROW EXCLUSIVE"
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE agent_message_request_policies IN `+mode+` MODE`); e != nil {
		return fail(mp.ErrUnavailable)
	}
	b, e := lockOwnAgentPrivateBinding(ctx, tx, a, s.devPhoneEnabled)
	if e != nil {
		if currentErr := checkHumanMomentSession(ctx, tx, a.SessionDigest, a.WorkspacePrincipal.ID, s.devPhoneEnabled); currentErr != nil {
			return fail(currentErr)
		}
		return fail(messageOwnError(e))
	}
	if _, e = lockAgentPrivateMetadata(ctx, tx, b, false); e != nil {
		return fail(messageOwnError(e))
	}
	var one bool
	if tx.QueryRow(ctx, `SELECT count(*)=1 FROM agents WHERE principal_account_id=$1 AND agent_type='personal' AND status='active'`, id).Scan(&one) != nil || !one {
		return fail(mp.ErrDenied)
	}
	return tx, b, nil
}
func messagePolicyRecord(ctx context.Context, tx pgx.Tx, b agentPrivateBinding) (mp.Record, error) {
	r := mp.Record{Schema: mp.Schema, OwnerID: b.accountID, AgentID: b.agentID, Status: "UNCONFIGURED", IncomingRequests: mp.Request}
	var from, until time.Time
	var policyAgent string
	e := tx.QueryRow(ctx, `SELECT agent_id,native_revision,incoming_requests,valid_from,expires_at FROM agent_message_request_policies WHERE owner_id=$1 FOR SHARE`, b.accountID).Scan(&policyAgent, &r.NativeRevision, &r.IncomingRequests, &from, &until)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return mp.Record{}, mp.ErrUnavailable
	}
	if e == nil {
		if policyAgent != b.agentID {
			return mp.Record{}, mp.ErrChanged
		}
		r.Configured = true
		r.ValidFrom = &from
		r.ExpiresAt = &until
		r.Status = "ACTIVE"
	}
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&r.ObservedAt) != nil {
		return mp.Record{}, mp.ErrUnavailable
	}
	r.ObservedAt = r.ObservedAt.UTC()
	if r.Configured && (!r.ObservedAt.Before(until) || r.ObservedAt.Before(from)) {
		r.Status = "EXPIRED"
	}
	return r, nil
}
func (s *Store) GetOwnMessagePolicy(ctx context.Context, a agentprofile.PrivateAccess) (mp.Record, error) {
	tx, b, e := s.beginMessageOwn(ctx, a, false)
	if e != nil {
		return mp.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := messagePolicyRecord(ctx, tx, b)
	if e != nil {
		return r, e
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		if errors.Is(e, agentprofile.ErrForbidden) {
			return mp.Record{}, identity.ErrUnauthorized
		}
		return mp.Record{}, messageOwnError(e)
	}
	if tx.Commit(ctx) != nil {
		return mp.Record{}, mp.ErrUnavailable
	}
	return r, nil
}
func (s *Store) PutOwnMessagePolicy(ctx context.Context, a agentprofile.PrivateAccess, in mp.PutInput) (mp.Record, error) {
	if !mp.ValidInput(in) {
		return mp.Record{}, mp.ErrInvalid
	}
	tx, b, e := s.beginMessageOwn(ctx, a, true)
	if e != nil {
		return mp.Record{}, e
	}
	defer tx.Rollback(context.Background())
	var version int64
	e = tx.QueryRow(ctx, `SELECT native_revision FROM agent_message_request_policies WHERE agent_id=$1 FOR UPDATE`, b.agentID).Scan(&version)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return mp.Record{}, mp.ErrUnavailable
	}
	if version != in.ExpectedVersion {
		return mp.Record{}, mp.ErrChanged
	}
	var now time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
		return mp.Record{}, mp.ErrUnavailable
	}
	now = now.UTC()
	if !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(30*24*time.Hour)) {
		return mp.Record{}, mp.ErrInvalid
	}
	tag, e := tx.Exec(ctx, `INSERT INTO agent_message_request_policies(agent_id,owner_id,owner_type,native_revision,incoming_requests,valid_from,expires_at) VALUES($1,$2,'PERSON',1,$3,$4,$5) ON CONFLICT(agent_id) DO UPDATE SET native_revision=agent_message_request_policies.native_revision+1,incoming_requests=EXCLUDED.incoming_requests,valid_from=EXCLUDED.valid_from,expires_at=EXCLUDED.expires_at WHERE agent_message_request_policies.native_revision=$6 AND agent_message_request_policies.owner_id=$2`, b.agentID, b.accountID, in.IncomingRequests, now, in.ExpiresAt, version)
	if e != nil || tag.RowsAffected() != 1 {
		return mp.Record{}, mp.ErrUnavailable
	}
	if _, e = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,'replace','agent_policy',$2,'allowed','human_message_policy_edit')`, b.accountID, b.agentID); e != nil {
		return mp.Record{}, mp.ErrUnavailable
	}
	r, e := messagePolicyRecord(ctx, tx, b)
	if e != nil {
		return r, e
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		if errors.Is(e, agentprofile.ErrForbidden) {
			return mp.Record{}, identity.ErrUnauthorized
		}
		return mp.Record{}, messageOwnError(e)
	}
	if r.Status != "ACTIVE" {
		return mp.Record{}, mp.ErrChanged
	}
	if tx.Commit(ctx) != nil {
		return mp.Record{}, mp.ErrUnavailable
	}
	return r, nil
}
