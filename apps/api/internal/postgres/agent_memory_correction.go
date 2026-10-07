package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"time"
)

var _ mc.HumanStore = (*Store)(nil)

const memoryCorrectionGuardSQL = `SELECT to_regclass('public.agent_memory_corrections') IS NOT NULL
 AND to_regclass('public.agent_memory_suppressions') IS NOT NULL AND to_regclass('public.agent_memory_source_invalidations') IS NOT NULL
 AND to_regprocedure('public.birdtie_memory_correction_binding(uuid,uuid,text,uuid,text)') IS NOT NULL
 AND to_regprocedure('public.birdtie_memory_correction_authority(uuid,uuid,uuid)') IS NOT NULL
 AND (SELECT count(*)=7 FROM pg_trigger t WHERE NOT t.tgisinternal AND t.tgenabled='O' AND
 ((t.tgrelid=to_regclass('public.agent_memory_corrections') AND t.tgname='memory_correction_guard' AND t.tgfoid=to_regprocedure('public.birdtie_memory_correction_guard()'))
 OR(t.tgrelid=to_regclass('public.agent_memory_suppressions') AND t.tgname='memory_suppression_guard' AND t.tgfoid=to_regprocedure('public.birdtie_memory_suppression_guard()'))
 OR(t.tgrelid=to_regclass('public.agent_memory_candidates') AND t.tgname='memory_candidate_correction_guard' AND t.tgfoid=to_regprocedure('public.birdtie_memory_candidate_correction_guard()'))
 OR(t.tgrelid=to_regclass('public.agent_memory_source_invalidations') AND t.tgname='memory_source_invalidation_guard' AND t.tgfoid=to_regprocedure('public.birdtie_memory_source_invalidation_guard()'))
 OR(t.tgrelid=to_regclass('public.moments') AND t.tgname='memory_moment_invalidated' AND t.tgfoid=to_regprocedure('public.birdtie_memory_source_invalidated()'))
 OR(t.tgrelid=to_regclass('public.activity_participations') AND t.tgname='memory_participation_invalidated' AND t.tgfoid=to_regprocedure('public.birdtie_memory_source_invalidated()'))
 OR(t.tgrelid=to_regclass('public.saved_items') AND t.tgname='memory_saved_invalidated' AND t.tgfoid=to_regprocedure('public.birdtie_memory_source_invalidated()'))))`

func memoryCorrectionGate(ctx context.Context, tx pgx.Tx) error {
	var ok bool
	if tx.QueryRow(ctx, memoryCorrectionGuardSQL).Scan(&ok) != nil || !ok {
		return agentmemory.ErrUnavailable
	}
	if _, e := tx.Exec(ctx, `LOCK TABLE agent_memory_corrections,agent_memory_suppressions,agent_memory_source_invalidations IN ACCESS SHARE MODE`); e != nil {
		return agentmemory.ErrUnavailable
	}
	if tx.QueryRow(ctx, memoryCorrectionGuardSQL).Scan(&ok) != nil || !ok {
		return agentmemory.ErrUnavailable
	}
	return nil
}
func candidateCorrectionAllowed(ctx context.Context, tx pgx.Tx, agent, owner, predicate, category string) (bool, error) {
	if e := memoryCorrectionGate(ctx, tx); e != nil {
		return false, e
	}
	var ok bool
	e := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_memory_suppressions WHERE agent_id=$1 AND owner_id=$2 AND predicate=$3 AND category=$4)`, agent, owner, predicate, category).Scan(&ok)
	if e != nil {
		return false, agentmemory.ErrUnavailable
	}
	return ok, nil
}
func correctionError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return agentmemory.ErrNotFound
	}
	if errors.Is(e, agentmemory.ErrInvalid) || errors.Is(e, agentmemory.ErrForbidden) || errors.Is(e, agentmemory.ErrConflict) || errors.Is(e, agentmemory.ErrNotFound) || errors.Is(e, agentmemory.ErrUnavailable) {
		return e
	}
	var p *pgconn.PgError
	if errors.As(e, &p) && (p.Code == "23505" || p.Code == "23514" || p.Code == "P0001" || p.Code == "40001" || p.Code == "40P01") {
		return agentmemory.ErrConflict
	}
	return agentmemory.ErrUnavailable
}
func (s *Store) beginMemoryCorrection(ctx context.Context, a agentprofile.PrivateAccess) (pgx.Tx, agentPrivateBinding, error) {
	if s == nil || s.pool == nil || ctx == nil || ctx.Err() != nil {
		return nil, agentPrivateBinding{}, agentmemory.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, agentPrivateBinding{}, agentmemory.ErrForbidden
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, agentPrivateBinding{}, agentmemory.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, agentPrivateBinding, error) {
		tx.Rollback(context.Background())
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
	if _, e = lockAgentPrivateMetadata(ctx, tx, b, true); e != nil {
		return fail(memoryError(e))
	}
	if _, e = tx.Exec(ctx, `WITH expired AS(SELECT id FROM agent_memory_corrections WHERE owner_id=$1 AND agent_id=$2 AND committed_at IS NULL AND expires_at<=clock_timestamp() AND input<>'{}' ORDER BY expires_at,id LIMIT 100 FOR UPDATE) UPDATE agent_memory_corrections c SET input='{}' FROM expired WHERE c.id=expired.id`, b.accountID, b.agentID); e != nil {
		return fail(e)
	}
	return tx, b, nil
}

type correctionCapture struct {
	memories           []agentmemory.Record
	candidates         []candidateState
	affected           []mc.Target
	binding, authority string
	at, end            time.Time
}

// The shared owner metadata lock precedes candidate and Memory locks. Source
// mutations only append markers; no Moment->Candidate lock path is introduced.
func captureMemoryCorrection(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, in mc.Input) (correctionCapture, error) {
	c := correctionCapture{memories: []agentmemory.Record{}, candidates: []candidateState{}, affected: []mc.Target{}}
	var cat any
	if in.Category != "" {
		cat = in.Category
	}
	rows, e := tx.Query(ctx, `SELECT `+memoryCandidateColumns+` FROM agent_memory_candidates WHERE owner_id=$1 AND agent_id=$2 AND(( $3='CANDIDATE' AND id=$4) OR($5::text IS NOT NULL AND predicate='ACTIVITY_CATEGORY' AND category=$5 AND status IN('CANDIDATE','ACTIVE'))) ORDER BY id FOR UPDATE`, b.accountID, b.agentID, in.TargetKind, in.TargetID, cat)
	if e != nil {
		return c, correctionError(e)
	}
	for rows.Next() {
		v, e := scanMemoryCandidate(rows)
		if e != nil {
			rows.Close()
			return c, correctionError(e)
		}
		c.candidates = append(c.candidates, v)
		c.affected = append(c.affected, mc.Target{Kind: "CANDIDATE", ID: v.record.ID, Version: v.record.Version})
	}
	rows.Close()
	if rows.Err() != nil {
		return c, agentmemory.ErrUnavailable
	}
	rows, e = tx.Query(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE owner_id=$1 AND agent_id=$2 AND owner_type='PERSON' AND(($3='MEMORY' AND id=$4) OR($5::text IS NOT NULL AND memory_type='PREFERENCE' AND memory_key='activity_category:'||$5 AND status IN('ACTIVE','PENDING_REVIEW'))) ORDER BY id FOR UPDATE`, b.accountID, b.agentID, in.TargetKind, in.TargetID, cat)
	if e != nil {
		return c, correctionError(e)
	}
	for rows.Next() {
		v, e := scanAgentMemory(rows)
		if e != nil {
			rows.Close()
			return c, correctionError(e)
		}
		c.memories = append(c.memories, v)
		c.affected = append(c.affected, mc.Target{Kind: "MEMORY", ID: v.ID, Version: v.Version})
	}
	rows.Close()
	if rows.Err() != nil || len(c.affected) > 100 {
		return c, agentmemory.ErrUnavailable
	}
	e = tx.QueryRow(ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at) SELECT birdtie_memory_correction_authority($1,$2,$3),birdtie_memory_correction_binding($1,$2,$4,$5,$6),clk.at,least(clk.at+interval '5 minutes',s.expires_at,s.idle_expires_at) FROM sessions s CROSS JOIN clk WHERE s.id=$3`, b.accountID, b.agentID, b.sessionID, in.TargetKind, in.TargetID, cat).Scan(&c.authority, &c.binding, &c.at, &c.end)
	if e != nil || !mc.ValidDigest(c.authority) || !mc.ValidDigest(c.binding) {
		return c, agentmemory.ErrForbidden
	}
	found := false
	for _, m := range c.memories {
		if m.ID == in.TargetID && in.TargetKind == "MEMORY" {
			found = true
			if m.Version != in.ExpectedVersion || m.Status == agentmemory.StatusDeleted || m.ValidFrom.After(c.at) || !m.ValidUntil.After(c.at) {
				return c, agentmemory.ErrConflict
			}
			if in.Action == "EDIT" && m.SourceType != agentmemory.SourceExplicit {
				return c, agentmemory.ErrForbidden
			}
			if in.Action == "NEGATE" && (m.MemoryType != agentmemory.TypePreference || m.MemoryKey != "activity_category:"+in.Category) {
				return c, agentmemory.ErrInvalid
			}
			if m.ValidUntil.Before(c.end) {
				c.end = m.ValidUntil
			}
		}
	}
	for _, v := range c.candidates {
		if v.record.ID == in.TargetID && in.TargetKind == "CANDIDATE" {
			found = true
			support, e := candidateMultiSupportCurrent(ctx, tx, b, v.record.ID)
			if e != nil {
				return c, e
			}
			if !support || v.metadata == 0 {
				return c, agentmemory.ErrConflict
			}
			var meta int64
			if tx.QueryRow(ctx, `SELECT profile_version FROM agent_profiles WHERE agent_id=$1 AND owner_id=$2`, b.agentID, b.accountID).Scan(&meta) != nil || meta != v.metadata {
				return c, agentmemory.ErrConflict
			}
			if v.record.Version != in.ExpectedVersion || v.record.Status != agentmemorycandidate.Candidate || !v.record.ValidUntil.After(c.at) || in.Action == "NEGATE" && v.record.Category != in.Category {
				return c, agentmemory.ErrConflict
			}
			// Use the existing full native resolver; the input grants no source access.
			current, now, e := candidateSources(ctx, tx, b, v.metadata, candidateSelectors(v.record.Sources))
			if e != nil {
				return c, e
			}
			if !reflect.DeepEqual(current, v.record.Sources) || !v.record.ValidUntil.After(now) {
				return c, agentmemory.ErrConflict
			}
			if v.record.ValidUntil.Before(c.end) {
				c.end = v.record.ValidUntil
			}
		}
	}
	if !found {
		return c, agentmemory.ErrNotFound
	}
	if in.Replacement != nil {
		if _, e = agentmemory.NormalizePutInput(*in.Replacement, c.at); e != nil {
			return c, e
		}
		if in.Replacement.ValidUntil.Before(c.end) {
			c.end = in.Replacement.ValidUntil
		}
	}
	// Source locks above may have waited. Every preview decision uses late PG time.
	var late time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&late) != nil {
		return c, agentmemory.ErrUnavailable
	}
	if !c.end.After(late) {
		return c, agentmemory.ErrConflict
	}
	return c, nil
}

type correctionStored struct {
	input                              mc.Input
	authority, binding, digest, result string
	at, end                            time.Time
	until                              *time.Time
	committed                          *time.Time
	memoryID                           *string
	memoryVersion                      *int64
}

const correctionColumns = `input,authority,binding,plan_digest,result_id,observed_at,expires_at,memory_until,committed_at,result_memory_id,result_memory_version`

func loadMemoryCorrection(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, id string) (correctionStored, error) {
	var s correctionStored
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT `+correctionColumns+` FROM agent_memory_corrections WHERE id=$1 AND owner_id=$2 AND agent_id=$3 FOR UPDATE`, id, b.accountID, b.agentID).Scan(&raw, &s.authority, &s.binding, &s.digest, &s.result, &s.at, &s.end, &s.until, &s.committed, &s.memoryID, &s.memoryVersion)
	if e != nil {
		return s, correctionError(e)
	}
	// Committed rows scrub the private proposal. Metadata alone cannot restore it.
	if s.committed == nil && string(raw) != "{}" {
		if json.Unmarshal(raw, &s.input) != nil {
			return s, agentmemory.ErrUnavailable
		}
	}
	return s, nil
}
func finishMemoryCorrection(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, end *time.Time) (time.Time, error) {
	if e := memoryCorrectionGate(ctx, tx); e != nil {
		return time.Time{}, e
	}
	var at time.Time
	var valid bool
	e := tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT n.at,s.revoked_at IS NULL AND s.expires_at>n.at AND s.idle_expires_at>n.at AND($2::timestamptz IS NULL OR $2>n.at) FROM sessions s CROSS JOIN n WHERE s.id=$1`, b.sessionID, end).Scan(&at, &valid)
	if e != nil {
		return at, correctionError(e)
	}
	if !valid {
		return at, agentmemory.ErrForbidden
	}
	if ctx.Err() != nil {
		return at, agentmemory.ErrUnavailable
	}
	return at, nil
}
func (s *Store) PreviewOwnMemoryCorrection(ctx context.Context, a agentprofile.PrivateAccess, in mc.Input) (mc.Preview, error) {
	in, e := mc.NormalizeInput(in)
	if e != nil {
		return mc.Preview{}, e
	}
	tx, b, e := s.beginMemoryCorrection(ctx, a)
	if e != nil {
		return mc.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	c, e := captureMemoryCorrection(ctx, tx, b, in)
	if e != nil {
		return mc.Preview{}, e
	}
	p := mc.Preview{SchemaVersion: mc.Schema, ID: in.ID, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID, Input: in, Memories: c.memories, Affected: c.affected, ObservedAt: c.at.UTC(), ExpiresAt: c.end.UTC(), Explanation: mc.Explanation}
	if in.Action == "NEGATE" {
		u := c.at.Add(365 * 24 * time.Hour).UTC()
		p.NewMemoryValidUntil = &u
	}
	p.PlanDigest, _ = agentreinforcement.Digest(struct {
		Preview                     mc.Preview
		Authority, Binding, Session string
	}{p, c.authority, c.binding, b.sessionID})
	if mc.ValidatePreview(p) != nil {
		return mc.Preview{}, agentmemory.ErrUnavailable
	}
	raw, _ := json.Marshal(in)
	var cat any
	if in.Category != "" {
		cat = in.Category
	}
	old, e := loadMemoryCorrection(ctx, tx, b, in.ID)
	if e == nil {
		if old.committed != nil || !reflect.DeepEqual(old.input, in) || old.authority != c.authority || old.binding != c.binding {
			return mc.Preview{}, agentmemory.ErrConflict
		}
		p.ObservedAt = old.at
		p.ExpiresAt = old.end
		p.NewMemoryValidUntil = old.until
		p.PlanDigest = old.digest
	} else if errors.Is(e, agentmemory.ErrNotFound) {
		_, e = tx.Exec(ctx, `INSERT INTO agent_memory_corrections(id,owner_id,agent_id,session_id,target_kind,target_id,expected_version,action,category,input,authority,binding,plan_digest,result_id,observed_at,expires_at,memory_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,gen_random_uuid(),$14,$15,$16)`, in.ID, b.accountID, b.agentID, b.sessionID, in.TargetKind, in.TargetID, in.ExpectedVersion, in.Action, cat, raw, c.authority, c.binding, p.PlanDigest, c.at, c.end, p.NewMemoryValidUntil)
		if e != nil {
			return mc.Preview{}, correctionError(e)
		}
	} else {
		return mc.Preview{}, e
	}
	if _, e = finishMemoryCorrection(ctx, tx, b, &p.ExpiresAt); e != nil {
		return mc.Preview{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return mc.Preview{}, correctionError(e)
	}
	return p, nil
}
func memoryCorrectionReceipt(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, id string) (mc.Receipt, error) {
	r := mc.Receipt{SchemaVersion: mc.Schema, ID: id, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID}
	var category *string
	e := tx.QueryRow(ctx, `SELECT target_kind,target_id,expected_version,action,plan_digest,expires_at,committed_at,result_memory_id,result_memory_version,category FROM agent_memory_corrections WHERE id=$1 AND owner_id=$2 AND agent_id=$3`, id, b.accountID, b.agentID).Scan(&r.Target.Kind, &r.Target.ID, &r.Target.Version, &r.Action, &r.PlanDigest, &r.ExpiresAt, &r.CommittedAt, &r.ResultMemoryID, &r.ResultMemoryVersion, &category)
	if e != nil {
		return mc.Receipt{}, correctionError(e)
	}
	r.ObservedAt, e = finishMemoryCorrection(ctx, tx, b, nil)
	if e != nil {
		return mc.Receipt{}, e
	}
	r.State = "PENDING"
	if !r.ExpiresAt.After(r.ObservedAt) {
		r.State = "EXPIRED"
	}
	if r.CommittedAt != nil {
		r.State = "COMMITTED"
		if r.ResultMemoryID != nil {
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND version=$4 AND (( $5 IN('DELETE','REJECT') AND status='DELETED') OR($5 NOT IN('DELETE','REJECT') AND status='ACTIVE' AND valid_until>clock_timestamp())))`, r.ResultMemoryID, b.agentID, b.accountID, r.ResultMemoryVersion, r.Action).Scan(&r.CurrentResultMatches)
			if e != nil {
				return mc.Receipt{}, correctionError(e)
			}
		}
		if category != nil {
			if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memory_suppressions WHERE agent_id=$1 AND owner_id=$2 AND category=$3)`, b.agentID, b.accountID, category).Scan(&r.SuppressionActive) != nil {
				return mc.Receipt{}, agentmemory.ErrUnavailable
			}
		}
	}
	if mc.ValidateReceipt(r) != nil {
		return mc.Receipt{}, agentmemory.ErrUnavailable
	}
	return r, nil
}
func (s *Store) ReadOwnMemoryCorrection(ctx context.Context, a agentprofile.PrivateAccess, id string) (mc.Receipt, error) {
	if !mc.ValidID(id) {
		return mc.Receipt{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.beginMemoryCorrection(ctx, a)
	if e != nil {
		return mc.Receipt{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `UPDATE agent_memory_corrections SET input='{}' WHERE id=$1 AND owner_id=$2 AND agent_id=$3 AND committed_at IS NULL AND expires_at<=clock_timestamp() AND input<>'{}'`, id, b.accountID, b.agentID); e != nil {
		return mc.Receipt{}, correctionError(e)
	}
	r, e := memoryCorrectionReceipt(ctx, tx, b, id)
	if e != nil {
		return mc.Receipt{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return mc.Receipt{}, correctionError(e)
	}
	return r, nil
}
func (s *Store) ConfirmOwnMemoryCorrection(ctx context.Context, a agentprofile.PrivateAccess, id string, in mc.ConfirmInput) (mc.Receipt, error) {
	if !mc.ValidID(id) || !mc.ValidDigest(in.PlanDigest) {
		return mc.Receipt{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.beginMemoryCorrection(ctx, a)
	if e != nil {
		return mc.Receipt{}, e
	}
	defer tx.Rollback(context.Background())
	old, e := loadMemoryCorrection(ctx, tx, b, id)
	if e != nil {
		return mc.Receipt{}, e
	}
	if old.digest != in.PlanDigest {
		return mc.Receipt{}, agentmemory.ErrConflict
	}
	var sid string
	if tx.QueryRow(ctx, `SELECT session_id FROM agent_memory_corrections WHERE id=$1`, id).Scan(&sid) != nil || sid != b.sessionID {
		return mc.Receipt{}, agentmemory.ErrForbidden
	}
	if old.committed != nil {
		r, e := memoryCorrectionReceipt(ctx, tx, b, id)
		if e != nil {
			return mc.Receipt{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return mc.Receipt{}, correctionError(e)
		}
		return r, nil
	}
	if old.input.ID == "" {
		return mc.Receipt{}, agentmemory.ErrConflict
	}
	c, e := captureMemoryCorrection(ctx, tx, b, old.input)
	if e != nil {
		return mc.Receipt{}, e
	}
	if c.authority != old.authority || c.binding != old.binding || !old.end.After(c.at) {
		return mc.Receipt{}, agentmemory.ErrConflict
	}
	var result agentmemory.Record
	switch old.input.Action {
	case "EDIT":
		result, _, e = putOwnMemoryInTx(ctx, tx, b, a.WorkspacePrincipal, old.input.TargetID, *old.input.Replacement)
	case "DELETE":
		result, e = deleteOwnMemoryInTx(ctx, tx, b, old.input.TargetID, old.input.ExpectedVersion)
	case "REJECT":
		if old.input.TargetKind == "MEMORY" {
			result, e = deleteOwnMemoryInTx(ctx, tx, b, old.input.TargetID, old.input.ExpectedVersion)
		} else {
			for _, v := range c.candidates {
				if v.record.ID == old.input.TargetID {
					_, e = candidateTransition(ctx, tx, b, v, agentmemorycandidate.Rejected)
					if e == nil {
						e = insertEnrichmentDomainAudit(ctx, tx, b.accountID, b.agentID, "reject", "memory_candidate", v.record.ID, "human_candidate_decision", "memory_rejected")
					}
				}
			}
		}
	case "NEGATE":
		// All affected proposals were explicitly shown and locked before Memory.
		for _, v := range c.candidates {
			if v.record.Status == agentmemorycandidate.Candidate || v.record.Status == agentmemorycandidate.Active {
				_, e = candidateTransition(ctx, tx, b, v, agentmemorycandidate.Expired)
				if e != nil {
					return mc.Receipt{}, e
				}
			}
		}
		chosen := ""
		expected := int64(0)
		for _, m := range c.memories {
			if m.MemoryKey != "activity_category:"+old.input.Category || m.MemoryType != agentmemory.TypePreference {
				continue
			}
			if m.SourceType == agentmemory.SourceInferred {
				if _, e = deleteOwnMemoryInTx(ctx, tx, b, m.ID, m.Version); e != nil {
					return mc.Receipt{}, e
				}
			} else if m.Status != agentmemory.StatusDeleted {
				chosen = m.ID
				expected = m.Version
			}
		}
		if chosen == "" {
			chosen = old.result
		}
		raw, _ := json.Marshal(map[string]string{"activityCategory": old.input.Category, "nature": "human-correction"})
		result, _, e = putOwnMemoryInTx(ctx, tx, b, a.WorkspacePrincipal, chosen, agentmemory.PutInput{ExpectedVersion: expected, MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:" + old.input.Category, Summary: mc.NegativeStatement(old.input.Category), StructuredValue: raw, Visibility: agentmemory.VisibilityPrivate, ValidUntil: *old.until})
	}
	if e != nil {
		return mc.Receipt{}, e
	}
	at, e := finishMemoryCorrection(ctx, tx, b, &old.end)
	if e != nil {
		return mc.Receipt{}, e
	}
	var mid, ver any
	if result.ID != "" {
		mid = result.ID
		ver = result.Version
	}
	_, e = tx.Exec(ctx, `UPDATE agent_memory_corrections SET committed_at=$2,result_memory_id=$3,result_memory_version=$4,input='{}' WHERE id=$1`, id, at, mid, ver)
	if e != nil {
		return mc.Receipt{}, correctionError(e)
	}
	if old.input.Action == "NEGATE" {
		_, e = tx.Exec(ctx, `INSERT INTO agent_memory_suppressions(agent_id,owner_id,predicate,category,operation_id,created_at) VALUES($1,$2,'ACTIVITY_CATEGORY',$3,$4,$5) ON CONFLICT(agent_id,predicate,category) DO NOTHING`, b.agentID, b.accountID, old.input.Category, id, at)
		if e != nil {
			return mc.Receipt{}, correctionError(e)
		}
	}
	if e = insertDomainAudit(ctx, tx, b.accountID, "correct", "agent_memory_correction", id, "human_specific_memory_correction", nil); e != nil {
		return mc.Receipt{}, agentmemory.ErrUnavailable
	}
	if _, e = finishMemoryCorrection(ctx, tx, b, &old.end); e != nil {
		return mc.Receipt{}, e
	}
	r, e := memoryCorrectionReceipt(ctx, tx, b, id)
	if e != nil {
		return mc.Receipt{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return mc.Receipt{}, correctionError(e)
	}
	return r, nil
}
