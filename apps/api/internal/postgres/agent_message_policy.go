package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ mp.Store = (*Store)(nil)
var _ mp.ResponseValidator = (*Store)(nil)

// The unexported concrete type cannot be reconstructed from a wire DTO. Its
// originating Store, captured caller and original bytes are all required.
type messagePolicyResponseProof struct {
	store                                       *Store
	owner, peer, agent, epoch, version, session string
	generation                                  int64
	digest                                      [32]byte
	wire                                        [32]byte
	until                                       time.Time
}

func messagePolicyWireHash(v any) [32]byte {
	raw, _ := json.Marshal(v)
	return sha256.Sum256(raw)
}

const messageOwnEpochSQL = `SELECT encode(sha256(convert_to(jsonb_build_array(
 (SELECT to_jsonb(a)||jsonb_build_object('xmin',a.xmin::text) FROM accounts a WHERE a.id=$1),
 COALESCE((SELECT jsonb_agg(to_jsonb(ag)||jsonb_build_object('xmin',ag.xmin::text,'profile',to_jsonb(ap),'apxmin',ap.xmin::text) ORDER BY ag.id) FROM agents ag LEFT JOIN agent_profiles ap ON ap.agent_id=ag.id WHERE ag.principal_account_id=$1 AND ag.agent_type='personal'),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('xmin',p.xmin::text) ORDER BY p.agent_id) FROM agent_message_request_policies p WHERE p.owner_id=$1),'[]'::jsonb))::text,'UTF8')),'hex')`

func messageOwnResponseClock(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, b agentPrivateBinding, dev bool) (time.Time, string, int64, error) {
	var now time.Time
	var epoch string
	var current bool
	var generation int64
	if tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() AS checked_at),se AS MATERIALIZED(SELECT s.authorization_generation FROM sessions s JOIN accounts a ON a.id=s.account_id CROSS JOIN n WHERE s.id=$2 AND s.token_sha256=$3 AND a.id=$1 AND a.account_type='person' AND a.status='active' AND s.revoked_at IS NULL AND s.expires_at>n.checked_at AND s.idle_expires_at>n.checked_at AND ($4::boolean OR s.authentication_method<>'dev_phone')) SELECT n.checked_at,(`+messageOwnEpochSQL+`),EXISTS(SELECT 1 FROM se),COALESCE((SELECT authorization_generation FROM se),0) FROM n`, b.accountID, b.sessionID, a.SessionDigest[:], dev).Scan(&now, &epoch, &current, &generation) != nil {
		return time.Time{}, "", 0, mp.ErrUnavailable
	}
	if !current {
		return time.Time{}, "", 0, identity.ErrUnauthorized
	}
	return now.UTC(), epoch, generation, nil
}

func finishMessagePolicyRecord(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, b agentPrivateBinding, r *mp.Record, dev bool) (string, int64, error) {
	now, epoch, generation, e := messageOwnResponseClock(ctx, tx, a, b, dev)
	if e != nil {
		return "", 0, e
	}
	r.ObservedAt = now.UTC()
	if r.Configured {
		r.Status = "ACTIVE"
		if now.Before(*r.ValidFrom) || !now.Before(*r.ExpiresAt) {
			r.Status = "EXPIRED"
		}
	}
	return epoch, generation, nil
}

func (s *Store) sealOwnMessagePolicy(a agentprofile.PrivateAccess, b agentPrivateBinding, r *mp.Record, epoch string, generation int64) {
	until := r.ObservedAt.Add(30 * time.Second)
	if r.Status == "ACTIVE" && r.ExpiresAt.Before(until) {
		until = *r.ExpiresAt
	}
	if r.Configured && r.ObservedAt.Before(*r.ValidFrom) && r.ValidFrom.Before(until) {
		until = *r.ValidFrom
	}
	r.ResponseProof = &messagePolicyResponseProof{store: s, owner: a.WorkspacePrincipal.ID, agent: r.AgentID, session: b.sessionID, generation: generation, digest: a.SessionDigest, epoch: epoch, wire: messagePolicyWireHash(*r), until: until}
}

func (s *Store) ValidateOwnMessagePolicyResponse(ctx context.Context, a agentprofile.PrivateAccess, expected mp.Record) error {
	p, ok := expected.ResponseProof.(*messagePolicyResponseProof)
	if !ok || p == nil || p.store != s || p.owner != a.WorkspacePrincipal.ID || p.peer != "" || p.digest != a.SessionDigest || p.agent != expected.AgentID || p.wire != messagePolicyWireHash(expected) || !mp.ValidRecord(expected, a.WorkspacePrincipal.ID) {
		return mp.ErrUnavailable
	}
	tx, b, e := s.beginMessageOwn(ctx, a, false)
	if e != nil {
		if errors.Is(e, mp.ErrDenied) {
			return mp.ErrChanged
		}
		return e
	}
	defer tx.Rollback(context.Background())
	// All source/session locks precede this single original-deadline gate.
	now, epoch, generation, e := messageOwnResponseClock(ctx, tx, a, b, s.devPhoneEnabled)
	if e != nil {
		return e
	}
	if b.agentID != p.agent || b.sessionID != p.session || generation != p.generation || epoch != p.epoch || !now.Before(p.until) {
		return mp.ErrChanged
	}
	if ctx.Err() != nil || tx.Commit(ctx) != nil {
		return mp.ErrUnavailable
	}
	return nil
}

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
	epoch, generation, e := finishMessagePolicyRecord(ctx, tx, a, b, &r, s.devPhoneEnabled)
	if e != nil {
		return mp.Record{}, e
	}
	s.sealOwnMessagePolicy(a, b, &r, epoch, generation)
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
	epoch, generation, e := finishMessagePolicyRecord(ctx, tx, a, b, &r, s.devPhoneEnabled)
	if e != nil {
		return mp.Record{}, e
	}
	if r.Status != "ACTIVE" {
		return mp.Record{}, mp.ErrChanged
	}
	s.sealOwnMessagePolicy(a, b, &r, epoch, generation)
	if tx.Commit(ctx) != nil {
		return mp.Record{}, mp.ErrUnavailable
	}
	return r, nil
}
