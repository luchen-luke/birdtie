package postgres

import (
	"context"
	"reflect"
	"sort"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/jackc/pgx/v5"
)

var _ agentmemory.CurrentHumanStore = (*Store)(nil)

// This read boundary never calls beginMemoryCorrection: its expired-preview
// scrub belongs to the old writer, not a GET detail or rejected wrong path.
func (s *Store) beginMemoryAPI(ctx context.Context, a agentprofile.PrivateAccess) (pgx.Tx, agentPrivateBinding, error) {
	if ctx == nil {
		return nil, agentPrivateBinding{}, agentmemory.ErrInvalid
	}
	if ctx.Err() != nil {
		return nil, agentPrivateBinding{}, ctx.Err()
	}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, agentPrivateBinding{}, agentmemory.ErrForbidden
	}
	if s == nil || s.pool == nil {
		return nil, agentPrivateBinding{}, agentmemory.ErrUnavailable
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, agentPrivateBinding{}, agentmemory.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, agentPrivateBinding, error) {
		_ = tx.Rollback(context.Background())
		return nil, agentPrivateBinding{}, correctionError(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(e)
	}
	if e = memoryCorrectionGate(ctx, tx); e != nil {
		return fail(e)
	}
	b, e := lockOwnAgentPrivateBinding(ctx, tx, a, s.devPhoneEnabled)
	if e != nil {
		return fail(memoryError(e))
	}
	if _, e = lockAgentPrivateMetadata(ctx, tx, b, false); e != nil {
		return fail(memoryError(e))
	}
	return tx, b, nil
}

func memoryAPICapture(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, id string) (agentmemory.Record, string, time.Time, time.Time, error) {
	var xmin string
	m, e := scanAgentMemory(extraEvidenceRow{row: tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+`,xmin::text FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' FOR SHARE`, id, b.agentID, b.accountID), targets: []any{&xmin}})
	if e != nil {
		return m, "", time.Time{}, time.Time{}, correctionError(e)
	}
	current := map[string]agentreinforcement.Entry{}
	// EXPLICIT remains an independent human declaration. Current support is
	// hashed privately; stale source IDs/body never appear in this detail DTO.
	if m.SourceType == agentmemory.SourceExplicit && m.Status == agentmemory.StatusActive {
		current, _, e = reinforcementNativeSources(ctx, tx, b, m)
		if e != nil {
			return m, "", time.Time{}, time.Time{}, correctionError(e)
		}
	}
	ids := make([]string, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var authority *string
	var native string
	var supportDeadline *time.Time
	var evidenceCount int
	e = tx.QueryRow(ctx, `SELECT birdtie_memory_correction_authority($1,$2,$3),
 encode(sha256(convert_to(jsonb_build_object('memory',to_jsonb(m),'xmin',m.xmin::text,
 'evidence',COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM(SELECT ev.*,ev.xmin::text AS native_epoch FROM agent_memory_evidence ev WHERE memory_id=m.id ORDER BY id LIMIT 101)e),'[]'),
 'suppression',COALESCE((SELECT jsonb_agg(jsonb_build_object('row',to_jsonb(c),'xmin',c.xmin::text) ORDER BY c.category) FROM agent_memory_suppressions c WHERE c.agent_id=$2 AND c.owner_id=$1),'[]'),
 'currentSession',(SELECT jsonb_build_object('row',to_jsonb(s),'xmin',s.xmin::text) FROM sessions s WHERE s.id=$3),
 'guards',(SELECT jsonb_agg(jsonb_build_object('name',t.tgname,'definition',pg_get_triggerdef(t.oid),'enabled',t.tgenabled,'epoch',t.xmin::text,'function',pg_get_functiondef(t.tgfoid),'functionEpoch',pr.xmin::text) ORDER BY t.tgname) FROM pg_trigger t JOIN pg_proc pr ON pr.oid=t.tgfoid WHERE NOT t.tgisinternal AND t.tgname IN('memory_correction_guard','memory_suppression_guard','memory_candidate_correction_guard','memory_source_invalidation_guard','memory_moment_invalidated','memory_participation_invalidated','memory_saved_invalidated'))
 )::text,'UTF8')),'hex'),
 (SELECT count(*) FROM agent_memory_evidence WHERE memory_id=m.id),
 (SELECT min(LEAST(a.expires_at,p.expires_at,c.expires_at)) FROM agent_memory_evidence e
 LEFT JOIN activity_participations ap ON e.source_type='ACTIVITY_PARTICIPATION' AND ap.id=e.source_id
 LEFT JOIN activities a ON a.id=ap.activity_id
 LEFT JOIN saved_items si ON e.source_type='SAVED_PLACE' AND si.id=e.source_id
 LEFT JOIN places p ON p.id=si.place_id
 LEFT JOIN cities c ON c.id=COALESCE(a.city_id,p.city_id)
 WHERE e.id=ANY($5::uuid[]))
 FROM agent_memories m WHERE m.id=$4 AND m.owner_id=$1 AND m.agent_id=$2 AND m.owner_type='PERSON'`, b.accountID, b.agentID, b.sessionID, id, ids).Scan(&authority, &native, &evidenceCount, &supportDeadline)
	if e != nil || evidenceCount > 100 || (m.SourceType == agentmemory.SourceInferred && evidenceCount != 0) {
		return m, "", time.Time{}, time.Time{}, agentmemory.ErrUnavailable
	}
	// The original authority is NULL when the Session expires during a source
	// or relation wait. This is a current permission refusal, not a scan error.
	if authority == nil {
		return m, "", time.Time{}, time.Time{}, agentmemory.ErrForbidden
	}
	if e = memoryCorrectionGate(ctx, tx); e != nil {
		return m, "", time.Time{}, time.Time{}, e
	}
	var at, end time.Time
	var valid bool
	e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT n.at,LEAST(n.at+interval '2 minutes',s.expires_at,s.idle_expires_at),
 isfinite(s.expires_at) AND isfinite(s.idle_expires_at) AND s.revoked_at IS NULL AND s.expires_at>n.at AND s.idle_expires_at>n.at FROM sessions s CROSS JOIN n WHERE s.id=$1`, b.sessionID).Scan(&at, &end, &valid)
	if e != nil {
		return m, "", at, end, agentmemory.ErrUnavailable
	}
	if !valid {
		return m, "", at, end, agentmemory.ErrForbidden
	}
	if supportDeadline != nil {
		if supportDeadline.UTC().Year() < 1 || supportDeadline.UTC().Year() > 9999 || !supportDeadline.After(at) {
			return m, "", at, end, agentmemory.ErrConflict
		}
		if supportDeadline.Before(end) {
			end = *supportDeadline
		}
	}
	if (m.Status == agentmemory.StatusActive || m.Status == agentmemory.StatusPendingReview) && m.ValidUntil.After(at) && m.ValidUntil.Before(end) {
		end = m.ValidUntil
	}
	fingerprint, e := agentreinforcement.Digest(struct {
		Authority, Native, Xmin string
		Sources                 map[string]agentreinforcement.Entry
	}{*authority, native, xmin, current})
	if e != nil {
		return m, "", at, end, agentmemory.ErrUnavailable
	}
	return m, fingerprint, at.UTC(), end.UTC(), nil
}
func (s *Store) ReadOwnMemoryDetail(ctx context.Context, a agentprofile.PrivateAccess, id string) (agentmemory.DetailProjection, error) {
	normal, e := agentmemory.NormalizeMemoryID(id)
	if e != nil || normal != id {
		return agentmemory.DetailProjection{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.beginMemoryAPI(ctx, a)
	if e != nil {
		return agentmemory.DetailProjection{}, e
	}
	defer tx.Rollback(context.Background())
	m, first, _, _, e := memoryAPICapture(ctx, tx, b, id)
	if e != nil {
		return agentmemory.DetailProjection{}, e
	}
	m2, proof, at, end, e := memoryAPICapture(ctx, tx, b, id)
	if e != nil {
		return agentmemory.DetailProjection{}, e
	}
	if first != proof || !reflect.DeepEqual(m, m2) {
		return agentmemory.DetailProjection{}, agentmemory.ErrConflict
	}
	p := agentmemory.DetailProjection{SchemaVersion: agentmemory.DetailSchema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID, Target: agentmemory.DetailTarget{ID: m.ID, Version: m.Version, Status: m.Status}, ObservedAt: at, ExpiresAt: end, Explanation: agentmemory.DetailExplanation}
	if m.Status != agentmemory.StatusDeleted && !m.ValidUntil.After(at) {
		p.Target.Status = agentmemory.StatusExpired
	}
	if p.Target.Status == agentmemory.StatusActive || p.Target.Status == agentmemory.StatusPendingReview {
		if m.ValidFrom.After(at) {
			return agentmemory.DetailProjection{}, agentmemory.ErrConflict
		}
		p.Memory = &m
	}
	p, e = agentmemory.IssueDetail(p, a, proof)
	if e != nil {
		return agentmemory.DetailProjection{}, agentmemory.ErrUnavailable
	}
	late, e := finishMemoryCorrection(ctx, tx, b, &p.ExpiresAt)
	if e != nil {
		return agentmemory.DetailProjection{}, e
	}
	if agentmemory.ValidateDetail(p, late) != nil {
		return agentmemory.DetailProjection{}, agentmemory.ErrConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return agentmemory.DetailProjection{}, correctionError(e)
	}
	if ctx.Err() != nil {
		return agentmemory.DetailProjection{}, ctx.Err()
	}
	return p, nil
}
func (s *Store) RevalidateOwnMemoryDetail(ctx context.Context, a agentprofile.PrivateAccess, old agentmemory.DetailProjection) error {
	proof, e := agentmemory.DetailNativeProof(old, a)
	if e != nil {
		return e
	}
	p, e := s.ReadOwnMemoryDetail(ctx, a, old.Target.ID)
	if e != nil {
		return e
	}
	current, e := agentmemory.DetailNativeProof(p, a)
	if e != nil {
		return e
	}
	if proof != current || agentmemory.ValidateDetail(old, p.ObservedAt) != nil || old.ExpiresAt.After(p.ExpiresAt) {
		return agentmemory.ErrConflict
	}
	return nil
}

// RejectOwnMemory is only a path adapter to the immutable original094 human
// operation. It neither creates a preview nor calls the old DELETE writer.
func (s *Store) RejectOwnMemory(ctx context.Context, a agentprofile.PrivateAccess, id string, in agentmemory.RejectInput) (mc.Receipt, error) {
	normal, e := agentmemory.NormalizeMemoryID(id)
	if e != nil || normal != id || agentmemory.ValidateRejectInput(in) != nil {
		return mc.Receipt{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.beginMemoryAPI(ctx, a)
	if e != nil {
		return mc.Receipt{}, e
	}
	defer tx.Rollback(context.Background())
	var operation, session string
	e = tx.QueryRow(ctx, `SELECT id,session_id FROM agent_memory_corrections WHERE id=$1 AND owner_id=$2 AND agent_id=$3 FOR SHARE`, in.OperationID, b.accountID, b.agentID).Scan(&operation, &session)
	if e != nil {
		return mc.Receipt{}, correctionError(e)
	}
	r, e := memoryCorrectionReceipt(ctx, tx, b, operation)
	if e != nil {
		return mc.Receipt{}, e
	}
	if r.Target.Kind != "MEMORY" || r.Target.ID != id || r.Action != "REJECT" || r.PlanDigest != in.PlanDigest {
		return mc.Receipt{}, agentmemory.ErrConflict
	}
	if session != b.sessionID {
		return mc.Receipt{}, agentmemory.ErrForbidden
	}
	if r.State == "EXPIRED" {
		return mc.Receipt{}, agentmemory.ErrConflict
	}
	if _, e = finishMemoryCorrection(ctx, tx, b, nil); e != nil {
		return mc.Receipt{}, e
	}
	if r.State == "PENDING" {
		if _, e = finishMemoryCorrection(ctx, tx, b, &r.ExpiresAt); e != nil {
			return mc.Receipt{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return mc.Receipt{}, correctionError(e)
	}
	if r.State == "COMMITTED" {
		return r, nil
	}
	// Original094 guard makes target/action/digest immutable. Its original
	// Confirm independently rechecks Session, binding, source, CAS and late clock.
	r, e = s.ConfirmOwnMemoryCorrection(ctx, a, in.OperationID, mc.ConfirmInput{PlanDigest: in.PlanDigest})
	if e != nil {
		return mc.Receipt{}, e
	}
	if mc.ValidateReceipt(r) != nil || r.Owner.ID != a.WorkspacePrincipal.ID || r.Target.Kind != "MEMORY" || r.Target.ID != id || r.Action != "REJECT" || r.PlanDigest != in.PlanDigest || r.State != "COMMITTED" {
		return mc.Receipt{}, agentmemory.ErrUnavailable
	}
	return r, nil
}
