package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/jackc/pgx/v5"
	"math"
	"reflect"
	"strings"
	"time"
)

type MemoryCandidateService struct {
	store *Store
	flags *agentfeature.Controller
}

func NewMemoryCandidateService(s *Store, f *agentfeature.Controller) *MemoryCandidateService {
	return &MemoryCandidateService{s, f}
}

// Actual automatic CandidateSubmitter and source analysis authority do not exist.
func (*MemoryCandidateService) SubmitInferredCandidate(context.Context) (agentmemorycandidate.Record, error) {
	return agentmemorycandidate.Record{}, agentcognitive.ErrUnavailable
}

type candidateState struct {
	record   agentmemorycandidate.Record
	metadata int64
	intent   string
	decision *string
}

const memoryCandidateColumns = `id,agent_id,owner_id,version,metadata_version,status,predicate,category,assessment,sources,intent_digest,decision_digest,memory_id,memory_version,valid_until,created_at,updated_at`

func scanMemoryCandidate(row pgx.Row) (candidateState, error) {
	var s candidateState
	r := &s.record
	var predicate, category *string
	var assessment, sourceRaw []byte
	e := row.Scan(&r.ID, &r.AgentID, &r.Owner.ID, &r.Version, &s.metadata, &r.Status, &predicate, &category, &assessment, &sourceRaw, &s.intent, &s.decision, &r.MemoryID, &r.MemoryVersion, &r.ValidUntil, &r.CreatedAt, &r.UpdatedAt)
	if e != nil {
		return s, e
	}
	r.SchemaVersion = agentmemorycandidate.Schema
	r.Owner.Type = actorref.Person
	r.ModelAccess = "UNAVAILABLE"
	r.ValidUntil = r.ValidUntil.UTC()
	r.CreatedAt = r.CreatedAt.UTC()
	r.UpdatedAt = r.UpdatedAt.UTC()
	if predicate != nil {
		r.Predicate = *predicate
	}
	if category != nil {
		r.Category = *category
	}
	if assessment != nil {
		var a agentconfidence.Assessment
		if json.Unmarshal(assessment, &a) != nil {
			return s, agentmemory.ErrUnavailable
		}
		a, e = agentconfidence.NormalizeAssessment(a)
		if e != nil {
			return s, agentmemory.ErrUnavailable
		}
		r.Assessment = &a
	}
	if json.Unmarshal(sourceRaw, &r.Sources) != nil {
		return s, agentmemory.ErrUnavailable
	}
	return s, nil
}
func (s *MemoryCandidateService) begin(ctx context.Context, access agentprofile.PrivateAccess) (pgx.Tx, agentPrivateBinding, int64, agentfeature.Ticket, error) {
	fail := func(e error) (pgx.Tx, agentPrivateBinding, int64, agentfeature.Ticket, error) {
		return nil, agentPrivateBinding{}, 0, agentfeature.Ticket{}, e
	}
	if ctx.Err() != nil || s == nil || s.store == nil || s.store.pool == nil {
		return fail(agentmemory.ErrUnavailable)
	}
	ticket, e := s.flags.Capture(agentfeature.Memory)
	if e != nil {
		return fail(agentmemory.ErrUnavailable)
	}
	if e = agentprofile.ValidatePrivateAccess(access); e != nil {
		return fail(memoryError(e))
	}
	tx, e := s.store.pool.Begin(ctx)
	if e != nil {
		return fail(agentmemory.ErrUnavailable)
	}
	abort := func(e error) (pgx.Tx, agentPrivateBinding, int64, agentfeature.Ticket, error) {
		_ = tx.Rollback(ctx)
		return fail(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return abort(agentmemory.ErrUnavailable)
	}
	if e = memoryCorrectionGate(ctx, tx); e != nil {
		return abort(e)
	}
	b, e := lockOwnAgentPrivateBinding(ctx, tx, access, s.store.devPhoneEnabled)
	if e != nil {
		return abort(memoryError(e))
	}
	meta, e := lockAgentPrivateMetadata(ctx, tx, b, true)
	if e != nil {
		return abort(memoryError(e))
	}
	return tx, b, meta.ProfileVersion, ticket, nil
}
func (s *MemoryCandidateService) finish(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, ticket agentfeature.Ticket, deadline *time.Time, selectedIDs ...string) error {
	if ctx.Err() != nil || !s.flags.Current(ticket) {
		return agentmemory.ErrUnavailable
	}
	if e := memoryCorrectionGate(ctx, tx); e != nil {
		return e
	}
	multi, e := candidateMultiHistory(ctx, tx, selectedIDs)
	if e != nil {
		return e
	}
	query := `WITH n AS MATERIALIZED(SELECT clock_timestamp() instant) SELECT revoked_at IS NULL AND expires_at>n.instant AND idle_expires_at>n.instant,($2::timestamptz IS NULL OR $2>n.instant)`
	if multi {
		query += ` AND NOT EXISTS(SELECT 1 FROM agent_effect_ledger e JOIN agent_memory_candidates c ON c.id=e.candidate_id WHERE e.handler_version='mom-candidate-multi-v1' AND e.candidate_id=ANY($3::uuid[]) AND c.status IN('CANDIDATE','ACTIVE') AND NOT EXISTS(SELECT 1 FROM agent_multi_candidate_bindings b JOIN agent_multi_candidate_previews p ON p.id=b.preview_id WHERE b.grant_id=e.retention_grant_id AND p.session_id=$1::uuid AND birdtie_candidate_pipeline_current(e.retention_grant_id)))`
	}
	query += ` FROM sessions CROSS JOIN n WHERE id=$1`
	args := []any{b.sessionID, deadline}
	if multi {
		args = append(args, selectedIDs)
	}
	var sessionCurrent, deadlineCurrent bool
	e = tx.QueryRow(ctx, query, args...).Scan(&sessionCurrent, &deadlineCurrent)
	if errors.Is(e, pgx.ErrNoRows) || (e == nil && !sessionCurrent) {
		return agentmemory.ErrForbidden
	}
	if e != nil {
		return agentmemory.ErrUnavailable
	}
	if !deadlineCurrent {
		return agentmemory.ErrConflict
	}
	if ctx.Err() != nil || !s.flags.Current(ticket) {
		return agentmemory.ErrUnavailable
	}
	if e := tx.Commit(ctx); e != nil {
		return memoryError(e)
	}
	return nil
}
func candidateLoad(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, id string) (candidateState, error) {
	n, e := agentmemory.NormalizeMemoryID(id)
	if e != nil || n != id {
		return candidateState{}, agentmemory.ErrInvalid
	}
	c, e := scanMemoryCandidate(tx.QueryRow(ctx, `SELECT `+memoryCandidateColumns+` FROM agent_memory_candidates WHERE id=$1 AND agent_id=$2 AND owner_id=$3 FOR UPDATE`, id, b.agentID, b.accountID))
	if errors.Is(e, pgx.ErrNoRows) {
		return c, agentmemory.ErrNotFound
	}
	if e != nil {
		return c, agentmemory.ErrUnavailable
	}
	return c, nil
}

// ListOwnCandidates is a bounded owner review, not an analysis scan. Every row
// is refreshed through the same native source and lifecycle rules as Read.
func (s *MemoryCandidateService) ListOwnCandidates(ctx context.Context, access agentprofile.PrivateAccess) ([]agentmemorycandidate.Record, error) {
	tx, b, meta, ticket, e := s.begin(ctx, access)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT `+memoryCandidateColumns+` FROM agent_memory_candidates WHERE agent_id=$1 AND owner_id=$2 ORDER BY updated_at DESC,id LIMIT 50 FOR UPDATE`, b.agentID, b.accountID)
	if e != nil {
		return nil, agentmemory.ErrUnavailable
	}
	var states []candidateState
	for rows.Next() {
		c, e := scanMemoryCandidate(rows)
		if e != nil {
			rows.Close()
			return nil, agentmemory.ErrUnavailable
		}
		states = append(states, c)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, agentmemory.ErrUnavailable
	}
	out := make([]agentmemorycandidate.Record, 0, len(states))
	for _, c := range states {
		c, e = candidateRefresh(ctx, tx, b, meta, c)
		if e != nil {
			return nil, e
		}
		out = append(out, agentmemorycandidate.Clone(c.record))
	}
	// All source ACL/clock checks share the final statement after every selected
	// source lock has been acquired. A later row wait cannot expose an earlier
	// expired source or a newly blocked activity through this list.
	selected := []agentmemorycandidate.Selector{}
	seen := map[agentmemorycandidate.Selector]bool{}
	for _, c := range out {
		if c.Status == agentmemorycandidate.Candidate || c.Status == agentmemorycandidate.Active {
			for _, src := range c.Sources {
				if !seen[src.Selector] {
					seen[src.Selector] = true
					selected = append(selected, src.Selector)
				}
			}
		}
	}
	raw, _ := json.Marshal(selected)
	moment, _ := memoryEvidenceSourceSQL(agentevent.MomentSource)
	part, _ := memoryEvidenceSourceSQL(agentevent.ParticipationSource)
	place, _ := memoryEvidenceSourceSQL(agentevent.SavedPlaceSource)
	adapt := func(q string) string { return strings.ReplaceAll(q, "$2", "e.source_id") }
	var current bool
	q := `WITH e AS(SELECT x->>'type' source_type,(x->>'id')::uuid source_id FROM jsonb_array_elements($2::jsonb) x)
 SELECT NOT EXISTS(SELECT 1 FROM e WHERE NOT EXISTS(SELECT 1 FROM (` + adapt(moment) + `) z WHERE e.source_type='MOMENT' UNION ALL SELECT 1 FROM (` + adapt(part) + `) z WHERE e.source_type='ACTIVITY_PARTICIPATION' UNION ALL SELECT 1 FROM (` + adapt(place) + `) z WHERE e.source_type='SAVED_PLACE'))
 AND NOT EXISTS(SELECT 1 FROM agent_memory_candidates c WHERE c.id=ANY($3::uuid[]) AND c.status IN('CANDIDATE','ACTIVE') AND (c.valid_until<=clock_timestamp() OR (c.status='ACTIVE' AND NOT EXISTS(SELECT 1 FROM agent_memories m WHERE m.id=c.memory_id AND m.version=c.memory_version AND m.source_type='EXPLICIT' AND m.status='ACTIVE' AND m.valid_until>clock_timestamp()))))`
	ids := []string{}
	for _, c := range out {
		ids = append(ids, c.ID)
	}
	if tx.QueryRow(ctx, q, b.accountID, raw, ids).Scan(&current) != nil {
		return nil, agentmemory.ErrUnavailable
	}
	if !current {
		return nil, agentmemory.ErrConflict
	}
	if e = s.finish(ctx, tx, b, ticket, nil, ids...); e != nil {
		return nil, e
	}
	return out, nil
}

// One statement checks every selected source against the current native ACL,
// session and exact Agent metadata. UTC is local to this transaction.
func candidateSources(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, meta int64, selectors []agentmemorycandidate.Selector) ([]agentmemorycandidate.Source, time.Time, error) {
	selectors, e := agentmemorycandidate.NormalizeSelectors(selectors)
	if e != nil {
		return nil, time.Time{}, e
	}
	for _, v := range selectors {
		if e = lockMemoryEvidenceSource(ctx, tx, b.accountID, v.Type, v.ID); e != nil {
			return nil, time.Time{}, e
		}
	}
	moment, _ := memoryEvidenceSourceSQL(agentevent.MomentSource)
	part, _ := memoryEvidenceSourceSQL(agentevent.ParticipationSource)
	place, _ := memoryEvidenceSourceSQL(agentevent.SavedPlaceSource)
	adapt := func(q string) string { return strings.ReplaceAll(q, "$2", "e.source_id") }
	raw, _ := json.Marshal(selectors)
	q := `WITH e AS(SELECT x->>'type' source_type,(x->>'id')::uuid source_id FROM jsonb_array_elements($2::jsonb) x)
 SELECT e.source_type,e.source_id,src.revision,src.event_time,src.canonical,clock_timestamp(),
 CASE e.source_type WHEN 'MOMENT' THEN COALESCE((SELECT array_agg('ACTIVITY:'||l.activity_id::text ORDER BY l.activity_id) FROM moment_activity_links l WHERE l.moment_id=e.source_id),ARRAY['MOMENT:'||e.source_id::text]) WHEN 'ACTIVITY_PARTICIPATION' THEN ARRAY['ACTIVITY:'||(SELECT activity_id::text FROM activity_participations WHERE id=e.source_id)] WHEN 'SAVED_PLACE' THEN ARRAY['PLACE:'||(SELECT place_id::text FROM saved_items WHERE id=e.source_id)] END,
 CASE e.source_type WHEN 'MOMENT' THEN jsonb_build_object('epoch',(SELECT xmin::text FROM moments WHERE id=e.source_id),'links',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',activity_id,'epoch',xmin::text) ORDER BY activity_id) FROM moment_activity_links WHERE moment_id=e.source_id),'[]'::jsonb)) WHEN 'ACTIVITY_PARTICIPATION' THEN jsonb_build_object('epoch',(SELECT xmin::text FROM activity_participations WHERE id=e.source_id)) WHEN 'SAVED_PLACE' THEN jsonb_build_object('epoch',(SELECT xmin::text FROM saved_items WHERE id=e.source_id)) END
 FROM e JOIN sessions ses ON ses.id=$3 AND ses.account_id=$1 JOIN accounts ac ON ac.id=$1 AND ac.status='active' AND ac.account_type='person' JOIN agents ag ON ag.id=$4 AND ag.principal_account_id=$1 AND ag.agent_type='personal' AND ag.status='active' JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.profile_version=$5 AND ap.owner_id=$1 AND ap.owner_type='PERSON'
 CROSS JOIN LATERAL(SELECT * FROM(` + adapt(moment) + `) z WHERE e.source_type='MOMENT' UNION ALL SELECT * FROM(` + adapt(part) + `) z WHERE e.source_type='ACTIVITY_PARTICIPATION' UNION ALL SELECT * FROM(` + adapt(place) + `) z WHERE e.source_type='SAVED_PLACE') src
 WHERE ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp() ORDER BY e.source_type,e.source_id`
	rows, e := tx.Query(ctx, q, b.accountID, raw, b.sessionID, b.agentID, meta)
	if e != nil {
		return nil, time.Time{}, agentmemory.ErrUnavailable
	}
	out := []agentmemorycandidate.Source{}
	var now time.Time
	for rows.Next() {
		var v agentmemorycandidate.Source
		var rev int64
		var canonical, epoch []byte
		var observed time.Time
		if rows.Scan(&v.Selector.Type, &v.Selector.ID, &rev, &v.EventTime, &canonical, &observed, &v.Anchors, &epoch) != nil {
			rows.Close()
			return nil, now, agentmemory.ErrUnavailable
		}
		now = observed.UTC()
		v.EventTime = v.EventTime.UTC()
		if v.EventTime.IsZero() || v.EventTime.Year() < 1 || v.EventTime.Year() > 9999 || v.EventTime.After(now) {
			rows.Close()
			return nil, now, agentmemory.ErrForbidden
		}
		v.Version = agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: rev}
		if v.Selector.Type != agentevent.MomentSource {
			kind := agentevent.UpdatedAtDigestVersion
			if v.Selector.Type == agentevent.SavedPlaceSource {
				kind = agentevent.CreatedAtDigestVersion
			}
			v.Version, e = agentevent.SnapshotVersion(v.Selector.Type, kind, v.EventTime, canonical)
			if e != nil {
				rows.Close()
				return nil, now, agentmemory.ErrUnavailable
			}
		}
		v.Fingerprint, _ = agentreinforcement.Digest(struct {
			Selector agentmemorycandidate.Selector
			Version  agentevent.SourceVersion
			Time     time.Time
			Epoch    json.RawMessage
		}{v.Selector, v.Version, v.EventTime, epoch})
		out = append(out, v)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, now, agentmemory.ErrUnavailable
	}
	if len(out) != len(selectors) {
		return nil, now, agentmemory.ErrForbidden
	}
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
		return nil, now, agentmemory.ErrUnavailable
	}
	return out, now.UTC(), nil
}
func candidateSelectors(s []agentmemorycandidate.Source) []agentmemorycandidate.Selector {
	r := make([]agentmemorycandidate.Selector, len(s))
	for i, v := range s {
		r[i] = v.Selector
	}
	return r
}
func candidateTransition(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, c candidateState, status agentmemorycandidate.Status) (candidateState, error) {
	if c.record.Version == math.MaxInt64 {
		return c, agentmemory.ErrConflict
	}
	r, e := scanMemoryCandidate(tx.QueryRow(ctx, `UPDATE agent_memory_candidates SET version=version+1,status=$4,predicate=NULL,category=NULL,assessment=NULL,sources='[]',memory_id=NULL,memory_version=NULL,decision_digest=NULL WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND version=$5 RETURNING `+memoryCandidateColumns, c.record.ID, b.agentID, b.accountID, status, c.record.Version))
	if e != nil {
		return c, memoryError(e)
	}
	return r, nil
}

// Invalid source/expiry clears proposal payload. The minimal suppression digest
// survives so a stale worker or new ID cannot revive the rejected same proposal.
func candidateRefresh(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, meta int64, c candidateState) (candidateState, error) {
	if c.record.Status == agentmemorycandidate.Candidate || c.record.Status == agentmemorycandidate.Active {
		ok, e := candidateCorrectionAllowed(ctx, tx, b.agentID, b.accountID, c.record.Predicate, c.record.Category)
		if e != nil {
			return c, e
		}
		if !ok {
			return candidateTransition(ctx, tx, b, c, agentmemorycandidate.Expired)
		}
	}
	if c.record.Status != agentmemorycandidate.Candidate && c.record.Status != agentmemorycandidate.Active {
		return c, nil
	}
	current, now, e := candidateSources(ctx, tx, b, meta, candidateSelectors(c.record.Sources))
	if e != nil && !errors.Is(e, agentmemory.ErrForbidden) {
		return c, e
	}
	support, e2 := candidateMultiSupportCurrent(ctx, tx, b, c.record.ID)
	if e2 != nil {
		return c, e2
	}
	stale := !support || e != nil || meta != c.metadata || !c.record.ValidUntil.After(now) || !reflect.DeepEqual(current, c.record.Sources)
	if c.record.Status == agentmemorycandidate.Active && !stale {
		var good bool
		if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memories WHERE id=$1 AND version=$2 AND status='ACTIVE' AND source_type='EXPLICIT' AND valid_until>clock_timestamp())`, c.record.MemoryID, c.record.MemoryVersion).Scan(&good) != nil {
			return c, agentmemory.ErrUnavailable
		}
		stale = !good
	}
	if stale {
		return candidateTransition(ctx, tx, b, c, agentmemorycandidate.Expired)
	}
	return c, nil
}

// SaveOwnCandidate is a human's manual hypothesis, never an inferred fact.
// Replacing a pending hypothesis creates a new ID and supersedes the old CAS.
func (s *MemoryCandidateService) SaveOwnCandidate(ctx context.Context, access agentprofile.PrivateAccess, id string, d agentmemorycandidate.Draft, replaceID string, replaceVersion int64) (agentmemorycandidate.Record, error) {
	n, e := agentmemory.NormalizeMemoryID(id)
	if e != nil || n != id {
		return agentmemorycandidate.Record{}, agentmemory.ErrInvalid
	}
	tx, b, meta, t, e := s.begin(ctx, access)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	defer tx.Rollback(ctx)
	var validationTime time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&validationTime) != nil {
		return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
	}
	d.ValidUntil = d.ValidUntil.UTC().Truncate(time.Microsecond)
	d, e = agentmemorycandidate.NormalizeDraft(d, validationTime)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	sources, now, e := candidateSources(ctx, tx, b, meta, d.Sources)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	if _, e = agentmemorycandidate.ClusterCount(sources); e != nil {
		return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
	}
	d.ValidUntil = d.ValidUntil.UTC().Truncate(time.Microsecond)
	d, e = agentmemorycandidate.NormalizeDraft(d, now)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	if ok, e := candidateCorrectionAllowed(ctx, tx, b.agentID, b.accountID, d.Predicate, d.Category); e != nil || !ok {
		if e != nil {
			return agentmemorycandidate.Record{}, e
		}
		return agentmemorycandidate.Record{}, agentmemory.ErrConflict
	}
	intent, _ := agentreinforcement.Digest(struct {
		Predicate, Category string
		Sources             []agentmemorycandidate.Source
	}{d.Predicate, d.Category, sources})
	old, e := scanMemoryCandidate(tx.QueryRow(ctx, `SELECT `+memoryCandidateColumns+` FROM agent_memory_candidates WHERE agent_id=$1 AND intent_digest=$2 FOR UPDATE`, b.agentID, intent))
	if e == nil {
		old, e = candidateRefresh(ctx, tx, b, meta, old)
		if e != nil {
			return agentmemorycandidate.Record{}, e
		}
		if e = s.finish(ctx, tx, b, t, nil); e != nil {
			return agentmemorycandidate.Record{}, e
		}
		return agentmemorycandidate.Clone(old.record), nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
	}
	if replaceID != "" {
		old, e = candidateLoad(ctx, tx, b, replaceID)
		if e != nil {
			return agentmemorycandidate.Record{}, e
		}
		old, e = candidateRefresh(ctx, tx, b, meta, old)
		if e != nil {
			return agentmemorycandidate.Record{}, e
		}
		if old.record.Status != agentmemorycandidate.Candidate || old.record.Version != replaceVersion {
			return agentmemorycandidate.Record{}, agentmemory.ErrConflict
		}
		if _, e = candidateTransition(ctx, tx, b, old, agentmemorycandidate.Superseded); e != nil {
			return agentmemorycandidate.Record{}, e
		}
	} else if replaceVersion != 0 {
		return agentmemorycandidate.Record{}, agentmemory.ErrInvalid
	}
	a, _ := json.Marshal(d.Assessment)
	refs, _ := json.Marshal(sources)
	c, e := scanMemoryCandidate(tx.QueryRow(ctx, `INSERT INTO agent_memory_candidates(id,agent_id,owner_id,version,metadata_version,status,predicate,category,assessment,sources,intent_digest,valid_until,created_at,updated_at) VALUES($1,$2,$3,1,$4,'CANDIDATE',$5,$6,$7,$8,$9,$10,$11,$11) RETURNING `+memoryCandidateColumns, id, b.agentID, b.accountID, meta, d.Predicate, d.Category, a, refs, intent, d.ValidUntil, now))
	if e != nil {
		return agentmemorycandidate.Record{}, memoryError(e)
	}
	final, _, e := candidateSources(ctx, tx, b, meta, d.Sources)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	if !reflect.DeepEqual(final, sources) {
		return agentmemorycandidate.Record{}, agentmemory.ErrConflict
	}
	if e = s.finish(ctx, tx, b, t, &d.ValidUntil); e != nil {
		return agentmemorycandidate.Record{}, e
	}
	return agentmemorycandidate.Clone(c.record), nil
}
func (s *MemoryCandidateService) ReadOwnCandidate(ctx context.Context, access agentprofile.PrivateAccess, id string) (agentmemorycandidate.Record, error) {
	tx, b, meta, t, e := s.begin(ctx, access)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	defer tx.Rollback(ctx)
	c, e := candidateLoad(ctx, tx, b, id)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	c, e = candidateRefresh(ctx, tx, b, meta, c)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	var deadline *time.Time
	if c.record.Status == agentmemorycandidate.Candidate || c.record.Status == agentmemorycandidate.Active {
		deadline = &c.record.ValidUntil
	}
	if e = s.finish(ctx, tx, b, t, deadline, c.record.ID); e != nil {
		return agentmemorycandidate.Record{}, e
	}
	return agentmemorycandidate.Clone(c.record), nil
}
func (s *MemoryCandidateService) RejectOwnCandidate(ctx context.Context, access agentprofile.PrivateAccess, id string, expected int64) (agentmemorycandidate.Record, error) {
	if expected <= 0 || expected == math.MaxInt64 {
		return agentmemorycandidate.Record{}, agentmemory.ErrInvalid
	}
	tx, b, meta, t, e := s.begin(ctx, access)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	defer tx.Rollback(ctx)
	c, e := candidateLoad(ctx, tx, b, id)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	c, e = candidateRefresh(ctx, tx, b, meta, c)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	if c.record.Status == agentmemorycandidate.Rejected && c.record.Version == expected+1 {
	} else {
		if c.record.Status != agentmemorycandidate.Candidate || c.record.Version != expected {
			return agentmemorycandidate.Record{}, agentmemory.ErrConflict
		}
		c, e = candidateTransition(ctx, tx, b, c, agentmemorycandidate.Rejected)
		if e != nil {
			return agentmemorycandidate.Record{}, e
		}
		if e = insertEnrichmentDomainAudit(ctx, tx, b.accountID, b.agentID, "reject", "memory_candidate", c.record.ID, "human_candidate_decision", "memory_rejected"); e != nil {
			return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
		}
	}
	if e = s.finish(ctx, tx, b, t, nil); e != nil {
		return agentmemorycandidate.Record{}, e
	}
	return agentmemorycandidate.Clone(c.record), nil
}

type MemoryCandidatePreview struct {
	service  *MemoryCandidateService
	binding  agentPrivateBinding
	metadata int64
	ticket   agentfeature.Ticket
	review   agentmemorycandidate.Review
}

func (p *MemoryCandidatePreview) Review() agentmemorycandidate.Review {
	if p == nil {
		return agentmemorycandidate.Review{}
	}
	v := p.review
	v.Candidate = agentmemorycandidate.Clone(v.Candidate)
	if v.PreviousMemory != nil {
		raw, _ := json.Marshal(v.PreviousMemory)
		var m agentmemory.Record
		_ = json.Unmarshal(raw, &m)
		v.PreviousMemory = &m
	}
	return v
}
func candidatePreviewDigest(p *MemoryCandidatePreview) string {
	v := p.review
	v.PlanDigest = ""
	d, _ := agentreinforcement.Digest(struct {
		Review   agentmemorycandidate.Review
		Binding  [3]string
		Metadata int64
	}{v, [3]string{p.binding.accountID, p.binding.agentID, p.binding.sessionID}, p.metadata})
	return d
}
func (*MemoryCandidatePreview) MarshalJSON() ([]byte, error) {
	return nil, agentmemorycandidate.ErrServerOnly
}
func (p *MemoryCandidatePreview) UnmarshalJSON([]byte) error {
	*p = MemoryCandidatePreview{}
	return agentmemorycandidate.ErrServerOnly
}
func (s *MemoryCandidateService) PreviewOwnAcceptance(ctx context.Context, access agentprofile.PrivateAccess, id string, expected int64, memoryID string, memoryVersion int64, memoryUntil time.Time) (*MemoryCandidatePreview, error) {
	n, e := agentmemory.NormalizeMemoryID(memoryID)
	if e != nil || n != memoryID || expected <= 0 || expected == math.MaxInt64 || memoryVersion < 0 || memoryVersion == math.MaxInt64 {
		return nil, agentmemory.ErrInvalid
	}
	tx, b, meta, t, e := s.begin(ctx, access)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	c, e := candidateLoad(ctx, tx, b, id)
	if e != nil {
		return nil, e
	}
	c, e = candidateRefresh(ctx, tx, b, meta, c)
	if e != nil {
		return nil, e
	}
	if c.record.Status != agentmemorycandidate.Candidate || c.record.Version != expected {
		return nil, agentmemory.ErrConflict
	}
	clusters, e := agentmemorycandidate.ClusterCount(c.record.Sources)
	if e != nil || clusters < 2 {
		return nil, agentmemory.ErrForbidden
	}
	var now, sessionUntil time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp(),LEAST(expires_at,idle_expires_at) FROM sessions WHERE id=$1`, b.sessionID).Scan(&now, &sessionUntil) != nil {
		return nil, agentmemory.ErrUnavailable
	}
	memoryUntil = memoryUntil.UTC().Truncate(time.Microsecond)
	input := candidateMemoryInput(c.record, memoryVersion, memoryUntil)
	if _, e = agentmemory.NormalizePutInput(input, now); e != nil {
		return nil, e
	}
	// Check target authority/CAS before presenting concrete overwrite consequences.
	m, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 FOR UPDATE`, memoryID, b.agentID, b.accountID))
	if errors.Is(e, pgx.ErrNoRows) {
		if memoryVersion != 0 {
			return nil, agentmemory.ErrNotFound
		}
		var exists bool
		if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_memories WHERE id=$1)`, memoryID).Scan(&exists) != nil {
			return nil, agentmemory.ErrUnavailable
		}
		if exists {
			return nil, agentmemory.ErrForbidden
		}
	} else if e != nil {
		return nil, agentmemory.ErrUnavailable
	} else if m.Version != memoryVersion || m.SourceType != agentmemory.SourceExplicit || m.Status == agentmemory.StatusDeleted {
		return nil, agentmemory.ErrConflict
	}
	expires := now.Add(agentmemorycandidate.PreviewLease)
	for _, limit := range []time.Time{c.record.ValidUntil, sessionUntil, memoryUntil} {
		if limit.Before(expires) {
			expires = limit
		}
	}
	p := &MemoryCandidatePreview{service: s, binding: b, metadata: meta, ticket: t, review: agentmemorycandidate.Review{Candidate: agentmemorycandidate.Clone(c.record), Statement: agentmemorycandidate.Statement(c.record.Category), TargetMemoryID: memoryID, ExpectedMemoryVersion: memoryVersion, MemoryValidUntil: memoryUntil, Clusters: clusters, ExpiresAt: expires, Purpose: "HUMAN_EXPLICIT_DECLARATION"}}
	if m.ID != "" {
		p.review.PreviousMemory = &m
	}
	p.review.PlanDigest = candidatePreviewDigest(p)
	if e = s.finish(ctx, tx, b, t, &expires, c.record.ID); e != nil {
		return nil, e
	}
	return p, nil
}
func candidateMemoryInput(c agentmemorycandidate.Record, expected int64, until time.Time) agentmemory.PutInput {
	raw, _ := json.Marshal(map[string]string{"activityCategory": c.Category, "nature": "human-declaration"})
	return agentmemory.PutInput{ExpectedVersion: expected, MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:" + c.Category, Summary: agentmemorycandidate.Statement(c.Category), StructuredValue: raw, Visibility: agentmemory.VisibilityPrivate, ValidUntil: until}
}

// Accept is an explicit human declaration, never promotion of an inferred score.
// It commits Memory, new manual Evidence references and the candidate together.
func (s *MemoryCandidateService) AcceptOwnCandidate(ctx context.Context, access agentprofile.PrivateAccess, p *MemoryCandidatePreview) (agentmemorycandidate.Record, error) {
	if p == nil || s == nil || p.service != s || !s.flags.Current(p.ticket) || p.review.PlanDigest == "" || candidatePreviewDigest(p) != p.review.PlanDigest {
		return agentmemorycandidate.Record{}, agentmemory.ErrForbidden
	}
	tx, b, meta, t, e := s.begin(ctx, access)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	defer tx.Rollback(ctx)
	if b != p.binding || meta != p.metadata {
		return agentmemorycandidate.Record{}, agentmemory.ErrConflict
	}
	c, e := candidateLoad(ctx, tx, b, p.review.Candidate.ID)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	c, e = candidateRefresh(ctx, tx, b, meta, c)
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	if c.record.Status == agentmemorycandidate.Active && c.decision != nil && *c.decision == p.review.PlanDigest {
		if e = s.finish(ctx, tx, b, t, &p.review.ExpiresAt, c.record.ID); e != nil {
			return agentmemorycandidate.Record{}, e
		}
		return agentmemorycandidate.Clone(c.record), nil
	}
	if c.record.Status != agentmemorycandidate.Candidate || c.record.Version != p.review.Candidate.Version || !reflect.DeepEqual(c.record, p.review.Candidate) {
		return agentmemorycandidate.Record{}, agentmemory.ErrConflict
	}
	var now time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
		return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
	}
	if !p.review.ExpiresAt.After(now) {
		return agentmemorycandidate.Record{}, agentmemory.ErrConflict
	}
	// The legacy public writer's expected+1 same-content retry is valid for its
	// own operation. A still-pending candidate has not committed that operation,
	// so another target creation/change must invalidate this concrete approval.
	target, se := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 FOR UPDATE`, p.review.TargetMemoryID, b.agentID, b.accountID))
	if errors.Is(se, pgx.ErrNoRows) {
		if p.review.ExpectedMemoryVersion != 0 {
			return agentmemorycandidate.Record{}, agentmemory.ErrConflict
		}
	} else if se != nil {
		return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
	} else if target.Version != p.review.ExpectedMemoryVersion || target.SourceType != agentmemory.SourceExplicit || target.Status == agentmemory.StatusDeleted {
		return agentmemorycandidate.Record{}, agentmemory.ErrConflict
	}
	m, _, e := putOwnMemoryInTx(ctx, tx, b, access.WorkspacePrincipal, p.review.TargetMemoryID, candidateMemoryInput(c.record, p.review.ExpectedMemoryVersion, p.review.MemoryValidUntil))
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	for _, ref := range c.record.Sources {
		source, event, observed, se := resolveMemoryEvidenceSource(ctx, tx, b, m.ID, m.Version, ref.Selector.Type, ref.Selector.ID)
		if se != nil {
			return agentmemorycandidate.Record{}, se
		}
		if source.Version != ref.Version || !event.Equal(ref.EventTime) {
			return agentmemorycandidate.Record{}, agentmemory.ErrConflict
		}
		var rev, token any
		if source.Version.Revision > 0 {
			rev = source.Version.Revision
		}
		if source.Version.Token != "" {
			token = source.Version.Token
		}
		var count int
		if tx.QueryRow(ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1 AND status='CURRENT'`, m.ID).Scan(&count) != nil {
			return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
		}
		existing, se := scanMemoryEvidence(tx.QueryRow(ctx, `SELECT `+memoryEvidenceColumns+` FROM agent_memory_evidence WHERE memory_id=$1 AND memory_version=$2 AND source_type=$3 AND source_id=$4 AND status='CURRENT' FOR UPDATE`, m.ID, m.Version, source.Type, source.ID))
		if se == nil {
			if existing.Source == nil || *existing.Source != source || existing.EventTime == nil || !existing.EventTime.Equal(event) {
				return agentmemorycandidate.Record{}, agentmemory.ErrConflict
			}
			continue
		}
		if !errors.Is(se, pgx.ErrNoRows) {
			return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
		}
		if count >= 100 {
			return agentmemorycandidate.Record{}, agentmemory.ErrConflict
		}
		_, e = tx.Exec(ctx, `INSERT INTO agent_memory_evidence(id,memory_id,memory_version,agent_id,owner_id,source_type,source_id,source_version_kind,source_revision,source_token,signal_type,weight,observed_at,event_time,created_at,updated_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,$8,$9,'MANUAL_REFERENCE',1,$10,$11,$10,$10)`, m.ID, m.Version, b.agentID, b.accountID, source.Type, source.ID, source.Version.Kind, rev, token, observed, event)
		if e != nil {
			return agentmemorycandidate.Record{}, memoryError(e)
		}
	}
	result, e := scanMemoryCandidate(tx.QueryRow(ctx, `UPDATE agent_memory_candidates SET version=version+1,status='ACTIVE',memory_id=$4,memory_version=$5,decision_digest=$6 WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND version=$7 RETURNING `+memoryCandidateColumns, c.record.ID, b.agentID, b.accountID, m.ID, m.Version, p.review.PlanDigest, c.record.Version))
	if e != nil {
		return agentmemorycandidate.Record{}, memoryError(e)
	}
	if e = insertEnrichmentDomainAudit(ctx, tx, b.accountID, b.agentID, "approve", "memory_candidate", result.record.ID, "human_candidate_decision", "candidate_promoted"); e != nil {
		return agentmemorycandidate.Record{}, agentmemory.ErrUnavailable
	}
	// This is after the final write, including any waiting candidate trigger.
	final, _, e := candidateSources(ctx, tx, b, meta, candidateSelectors(c.record.Sources))
	if e != nil {
		return agentmemorycandidate.Record{}, e
	}
	if !reflect.DeepEqual(final, c.record.Sources) {
		return agentmemorycandidate.Record{}, agentmemory.ErrConflict
	}
	if e = s.finish(ctx, tx, b, t, &p.review.ExpiresAt, c.record.ID); e != nil {
		return agentmemorycandidate.Record{}, e
	}
	return agentmemorycandidate.Clone(result.record), nil
}
