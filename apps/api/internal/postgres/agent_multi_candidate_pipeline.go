package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	original "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	acp "github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"time"
)

// One trusted feature controller is shared with the server's other boundaries.
// No request can choose a handler, switch, owner, candidate payload or fence.
func NewMultiCandidatePipeline(s *Store, c *agentfeature.Controller) *acp.Service {
	return acp.NewService(&multiCandidateExecutor{store: s, Store: s}, c)
}

type multiCandidateExecutor struct {
	*Store
	store     *Store
	errorHook func(error)
	// Native tests inject rollback/crash boundaries here; no HTTP option exposes it.
	hook   func(string) error
	txHook func(context.Context, pgx.Tx, string) error
}

func (s *multiCandidateExecutor) pointTx(ctx context.Context, tx pgx.Tx, stage string) error {
	if s.txHook != nil {
		if e := s.txHook(ctx, tx, stage); e != nil {
			return e
		}
	}
	if s.hook != nil {
		return s.hook(stage)
	}
	return nil
}

func (x *multiCandidateExecutor) failure(e error) error {
	if x.errorHook != nil {
		x.errorHook(e)
	}
	return multiPipelineError(e)
}

const multiCandidateGateSQL = `SELECT to_regprocedure('public.birdtie_candidate_pipeline_current(uuid)') IS NOT NULL
 AND to_regprocedure('public.birdtie_expire_candidate_pipeline(integer)') IS NOT NULL
 AND(SELECT count(*)=5 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND NOT t.tgisinternal AND t.tgenabled='O' AND
 ((c.relname='agent_effect_ledger' AND t.tgname='agent_effect_writer_unavailable' AND t.tgfoid=to_regprocedure('public.birdtie_agent_effect_writer_unavailable()'))
 OR(c.relname='agent_effect_ledger' AND t.tgname='candidate_pipeline_atomic_final' AND t.tgfoid=to_regprocedure('public.birdtie_candidate_pipeline_final()') AND t.tgdeferrable AND t.tginitdeferred)
 OR(c.relname='consent_grants' AND t.tgname='candidate_pipeline_revoke' AND t.tgfoid=to_regprocedure('public.birdtie_candidate_pipeline_revoke()'))
 OR(c.relname='agent_domain_outbox' AND t.tgname='agent_outbox_guard' AND t.tgfoid=to_regprocedure('public.birdtie_guard_agent_outbox()'))
 OR(c.relname='agent_consumer_inbox' AND t.tgname='agent_consumer_inbox_guard' AND t.tgfoid=to_regprocedure('public.birdtie_guard_agent_consumer_inbox()'))))
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='public.agent_effect_ledger'::regclass AND conname='candidate_effect_no_persisted_proof' AND convalidated)`

func multiPipelineError(e error) error {
	switch {
	case errors.Is(e, acr.ErrInvalid):
		return acp.ErrInvalid
	case errors.Is(e, acr.ErrDenied), errors.Is(e, pgx.ErrNoRows):
		return acp.ErrDenied
	case errors.Is(e, acr.ErrExpired):
		return acp.ErrExpired
	case errors.Is(e, acr.ErrConflict):
		return acp.ErrConflict
	}
	var p *pgconn.PgError
	if errors.As(e, &p) {
		switch p.Code {
		case "55P03", "40P01":
			return acp.ErrBusy
		case "23505":
			return acp.ErrConflict
		}
	}
	return acp.ErrUnavailable
}
func multiPipelineGate(ctx context.Context, tx pgx.Tx, write bool) error {
	if e := memoryCorrectionGate(ctx, tx); e != nil {
		return acp.ErrUnavailable
	}
	var multiReady bool
	if tx.QueryRow(ctx, multiRetentionGuardSQL).Scan(&multiReady) != nil || !multiReady {
		return acp.ErrUnavailable
	}
	var ready bool
	if tx.QueryRow(ctx, multiCandidateGateSQL).Scan(&ready) != nil || !ready {
		return acp.ErrUnavailable
	}
	mode := "ACCESS SHARE"
	if write {
		mode = "ROW EXCLUSIVE"
	}
	if _, e := tx.Exec(ctx, `LOCK TABLE agent_memory_candidates,agent_effect_ledger,agent_consumer_inbox,agent_domain_outbox IN `+mode+` MODE`); e != nil {
		return multiPipelineError(e)
	}
	if tx.QueryRow(ctx, multiCandidateGateSQL).Scan(&ready) != nil || !ready {
		return acp.ErrUnavailable
	}
	return nil
}
func multiPipelineKey(b contextBuilderBinding, p multiRetentionStored) (string, error) {
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}
	return agentoutbox.EffectKey(agentoutbox.EffectAddress{Tenant: owner, Subject: owner, AgentID: b.agent, LogicalOperationID: p.operation, ActionID: original.StageActionID, Kind: agentoutbox.MemoryCandidateEffect})
}
func multiPipelineLegacy(ref *agentcognitive.CandidateSubmission, b contextBuilderBinding, p multiRetentionStored, c multiRetentionCapture) error {
	if ref == nil {
		return nil
	}
	r := ref.Request
	if r.Version != agentcognitive.ContractVersion || !aep.ValidID(r.RequestID) || agentcognitive.ValidateAgentReference(r.Agent) != nil || r.Scope != agentruntime.Private || r.Purpose != agentcognitive.SubmitMemoryCandidate || r.TaskID != c.review.TaskID || r.Agent.AgentID != b.agent || r.Agent.Principal != (actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}) || ref.PayloadDigest != p.digest || len(ref.Sources) != len(c.review.Sources) || r.ExpiresAt.IsZero() || r.ExpiresAt.After(c.end) || !r.ExpiresAt.After(c.at) {
		return acp.ErrDenied
	}
	if string(r.Source.Type) != "MOMENT" || r.Source.ID != c.review.AnchorSource.Selector.ID || r.Source.Version != c.review.AnchorSource.Version.Revision || r.Source.Owner != r.Agent.Principal {
		return acp.ErrDenied
	}
	for i, v := range ref.Sources {
		src := c.review.Sources[i]
		if string(v.Type) != "MOMENT" || v.ID != src.Selector.ID || v.Owner != r.Agent.Principal || v.Version != src.Version.Revision {
			return acp.ErrDenied
		}
	}
	return nil
}
func multiPipelineReceipt(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, gid, key string) (acp.Receipt, error) {
	r := acp.Receipt{SchemaVersion: acp.Schema, State: "CANDIDATE_STAGED", RetentionGrantID: gid, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}, AgentID: b.agent, Committed: true, Explanation: "已在原生事务中保留一项私密待核验候选；关键词不证明偏好或到场，不调用模型，不写正式记忆。"}
	e := tx.QueryRow(ctx, `SELECT event_id,logical_operation_id,effect_key,handler_version,candidate_id FROM agent_effect_ledger WHERE subject_id=$1 AND effect_key=$2 AND agent_id=$3 AND retention_grant_id=$4`, b.owner, key, b.agent, gid).Scan(&r.EventID, &r.LogicalOperationID, &r.EffectKey, &r.HandlerVersion, &r.CandidateID)
	return r, e
}
func (x *multiCandidateExecutor) StageOwnMultiCandidatePipeline(ctx context.Context, a agentprofile.PrivateAccess, gid string, flags *agentfeature.Controller, ticket agentfeature.Ticket, ref *agentcognitive.CandidateSubmission) (acp.Receipt, error) {
	if x == nil || x.store == nil || flags == nil || !flags.Current(ticket) {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	s := x.store
	tx, b, e := s.beginMultiRetention(ctx, a, true)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	defer tx.Rollback(context.Background())
	if e = multiPipelineGate(ctx, tx, true); e != nil {
		return acp.Receipt{}, e
	}
	g, p, e := multiRetentionGrantTx(ctx, tx, b, gid)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	if g.RevokedAt != nil || p.session != b.session {
		return acp.Receipt{}, acp.ErrDenied
	}
	c, e := s.captureMultiRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	if !multiRetentionMatch(p, c) || g.ExpiresAt.After(p.end) {
		return acp.Receipt{}, acp.ErrConflict
	}
	contextPurposeLimit(&c.end, g.ExpiresAt)
	if e = multiPipelineLegacy(ref, b, p, c); e != nil {
		return acp.Receipt{}, e
	}
	key, e := multiPipelineKey(b, p)
	if e != nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	// Sources/grants first, then NOWAIT. Old control claims lock the event first;
	// waiting here would reverse that order and create an avoidable lock cycle.
	var state string
	var attempt, fence int64
	var lease *time.Time
	e = tx.QueryRow(ctx, `SELECT delivery_state,attempt,fence,lease_until FROM agent_domain_outbox WHERE event_id=$1 AND subject_id=$2 FOR UPDATE NOWAIT`, p.event, b.owner).Scan(&state, &attempt, &fence, &lease)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}

	if r, e := multiPipelineReceipt(ctx, tx, b, gid, key); e == nil {
		at, e := s.finishContextBuilder(ctx, tx, b, nil)
		if e != nil {
			return acp.Receipt{}, x.failure(e)
		}
		r.ObservedAt = at.UTC()

		if !flags.Current(ticket) || acp.ValidateReceipt(r) != nil || tx.Commit(ctx) != nil {
			return acp.Receipt{}, acp.ErrUnavailable
		}
		return r, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return acp.Receipt{}, x.failure(e)
	}
	if state != "PENDING" && state != "UNAVAILABLE" {
		return acp.Receipt{}, acp.ErrBusy
	}
	if attempt >= 20 || fence == int64(^uint64(0)>>1) {
		return acp.Receipt{}, acp.ErrConflict
	}
	var worker string
	var now time.Time
	e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE agent_domain_outbox d SET delivery_state='LEASED',attempt=attempt+1,fence=fence+1,lease_owner=gen_random_uuid(),lease_until=least(n.at+interval '30 seconds',$2::timestamptz,d.expires_at),updated_at=n.at FROM n WHERE event_id=$1 RETURNING d.lease_owner,d.lease_until,d.fence,d.attempt,n.at`, p.event, c.end).Scan(&worker, &lease, &fence, &attempt, &now)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	contextPurposeLimit(&c.end, *lease)
	_, e = tx.Exec(ctx, `INSERT INTO agent_consumer_inbox(event_id,subject_id,handler_version,control_state,fence,attempt,reason_code,created_at,updated_at) VALUES($1,$2,$3,'LEASED',$4,$5,'',$6,$6)`, p.event, b.owner, acp.Handler, fence, attempt, now)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	if e = x.pointTx(ctx, tx, "after_claim"); e != nil {
		return acp.Receipt{}, e
	}
	intent, _ := agentreinforcement.Digest(struct {
		Predicate, Category string
		Sources             []agentmemorycandidate.Source
	}{c.review.Proposal.Predicate, c.review.Proposal.Category, c.review.Sources})
	var exists bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memory_candidates WHERE agent_id=$1 AND intent_digest=$2)`, b.agent, intent).Scan(&exists) != nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	if exists {
		return acp.Receipt{}, acp.ErrConflict
	}
	allowed, gateError := candidateCorrectionAllowed(ctx, tx, b.agent, b.owner, c.review.Proposal.Predicate, c.review.Proposal.Category)
	if gateError != nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	if !allowed {
		return acp.Receipt{}, acp.ErrConflict
	}
	rawSource, _ := json.Marshal(c.review.Sources)
	assessment, _ := json.Marshal(c.review.Proposal.Assessment)
	cand, e := scanMemoryCandidate(tx.QueryRow(ctx, `INSERT INTO agent_memory_candidates(id,agent_id,owner_id,version,metadata_version,status,predicate,category,assessment,sources,intent_digest,valid_until,created_at,updated_at) SELECT gen_random_uuid(),$1,$2,1,profile_version,'CANDIDATE',$3,$4,$5,$6,$7,$8,$9,$9 FROM agent_profiles WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' RETURNING `+memoryCandidateColumns, b.agent, b.owner, c.review.Proposal.Predicate, c.review.Proposal.Category, assessment, rawSource, intent, c.end, now))
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	if e = x.pointTx(ctx, tx, "after_candidate"); e != nil {
		return acp.Receipt{}, e
	}
	proof, _ := json.Marshal(c.review)
	_, e = tx.Exec(ctx, `INSERT INTO agent_effect_ledger(subject_id,effect_key,agent_id,logical_operation_id,action_id,effect_kind,action_digest,source_id,source_revision,candidate_id,retention_grant_id,event_id,handler_version,fence,attempt,proof_review) VALUES($1,$2,$3,$4,$5,'MEMORY_CANDIDATE',$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, b.owner, key, b.agent, p.operation, original.StageActionID, p.digest, c.review.AnchorSource.Selector.ID, c.review.AnchorSource.Version.Revision, cand.record.ID, gid, p.event, acp.Handler, fence, attempt, string(proof))
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	if e = x.pointTx(ctx, tx, "after_effect"); e != nil {
		return acp.Receipt{}, e
	}
	_, e = tx.Exec(ctx, `UPDATE agent_consumer_inbox SET control_state='CANDIDATE_STAGED',reason_code='CANDIDATE_STAGED',updated_at=clock_timestamp() WHERE event_id=$1 AND subject_id=$2 AND handler_version=$3 AND fence=$4 AND control_state='LEASED'`, p.event, b.owner, acp.Handler, fence)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	if e = x.pointTx(ctx, tx, "after_inbox"); e != nil {
		return acp.Receipt{}, e
	}
	_, e = tx.Exec(ctx, `UPDATE agent_domain_outbox SET delivery_state='CANDIDATE_STAGED',lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE event_id=$1 AND fence=$2 AND lease_owner=$3`, p.event, fence, worker)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	if e = x.pointTx(ctx, tx, "after_checkpoint"); e != nil {
		return acp.Receipt{}, e
	}
	last, e := s.captureMultiRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	if !multiRetentionMatch(p, last) || !reflect.DeepEqual(c.review, last.review) || !c.end.After(last.at) {
		return acp.Receipt{}, acp.ErrConflict
	}
	if e = x.pointTx(ctx, tx, "before_commit"); e != nil {
		return acp.Receipt{}, e
	}
	// Constraint triggers are run before the last clock/controller check; forcing
	// them now avoids a deferred relation/row wait after that final boundary.
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS candidate_pipeline_atomic_final IMMEDIATE`); e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	r, e := multiPipelineReceipt(ctx, tx, b, gid, key)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	r.Candidate = &cand.record
	at, alive, current, e := s.multiPipelineFinal(ctx, tx, b, gid, c.end)
	if e != nil || !alive || !current {
		return acp.Receipt{}, acp.ErrDenied
	}
	r.ObservedAt = at.UTC()
	if !flags.Current(ticket) || ctx.Err() != nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	if acp.ValidateReceipt(r) != nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}

	if e = tx.Commit(ctx); e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	return r, nil
}
func (x *multiCandidateExecutor) ReadOwnMultiCandidatePipeline(ctx context.Context, a agentprofile.PrivateAccess, gid string) (acp.Receipt, error) {
	if x == nil || x.store == nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	s := x.store
	tx, b, e := s.beginMultiRetention(ctx, a, false)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	defer tx.Rollback(context.Background())
	if e = multiPipelineGate(ctx, tx, false); e != nil {
		return acp.Receipt{}, e
	}
	g, p, e := multiRetentionGrantTx(ctx, tx, b, gid)
	if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	key, e := multiPipelineKey(b, p)
	if e != nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	r, e := multiPipelineReceipt(ctx, tx, b, gid, key)
	if errors.Is(e, pgx.ErrNoRows) {
		r = acp.Receipt{SchemaVersion: acp.Schema, State: "NOT_STAGED", RetentionGrantID: gid, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}, AgentID: b.agent, Explanation: "当前没有原生候选提交回执；现状不证明先前未知提交成功。"}
	} else if e != nil {
		return acp.Receipt{}, x.failure(e)
	}
	// Metadata reconciliation may use a new own Session. Candidate content never
	// inherits authority: re-resolve both original grants and all current frames.
	if r.Committed && g.RevokedAt == nil && p.session == b.session {
		if c, e := s.captureMultiRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm); e == nil && multiRetentionMatch(p, c) {
			cs, e := scanMemoryCandidate(tx.QueryRow(ctx, `SELECT `+memoryCandidateColumns+` FROM agent_memory_candidates WHERE id=$1 AND agent_id=$2 AND owner_id=$3 FOR SHARE`, r.CandidateID, b.agent, b.owner))
			if e != nil {
				return acp.Receipt{}, x.failure(e)
			}
			if last, e := s.captureMultiRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm); e == nil && multiRetentionMatch(p, last) && cs.record.Status == agentmemorycandidate.Candidate && cs.record.ValidUntil.After(last.at) {
				r.Candidate = &cs.record
			}
		}
	}
	deadline := p.end
	if r.Candidate != nil {
		contextPurposeLimit(&deadline, r.Candidate.ValidUntil)
	}
	at, alive, current, e := s.multiPipelineFinal(ctx, tx, b, gid, deadline)
	if e != nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	if !alive {
		return acp.Receipt{}, acp.ErrDenied
	}
	r.ObservedAt = at.UTC()
	if !current {
		r.Candidate = nil
	}
	if acp.ValidateReceipt(r) != nil || tx.Commit(ctx) != nil {
		return acp.Receipt{}, acp.ErrUnavailable
	}
	return r, nil
}

// No database query or write follows this final statement. Current owner
// authority and every original member approval share its snapshot and clock.
func (s *Store) multiPipelineFinal(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, gid string, until time.Time) (time.Time, bool, bool, error) {
	var at time.Time
	var alive, current bool
	e := tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT n.at,
 EXISTS(SELECT 1 FROM sessions se JOIN accounts a ON a.id=se.account_id JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id
 WHERE se.id=$1 AND a.id=$2 AND ag.id=$3 AND a.account_type='person' AND a.status='active' AND ag.agent_type='personal' AND ag.status='active' AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 AND se.revoked_at IS NULL AND se.expires_at>n.at AND se.idle_expires_at>n.at AND ($4 OR se.authentication_method<>'dev_phone')),
 birdtie_candidate_pipeline_current($5) AND $6::timestamptz>n.at FROM n`, b.session, b.owner, b.agent, s.devPhoneEnabled, gid, until).Scan(&at, &alive, &current)
	return at, alive, current, e
}

var _ acp.Executor = (*multiCandidateExecutor)(nil)
