package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/jackc/pgx/v5"
)

// MemoryReinforcementService is an ordinary owner management service. A flag
// is only a rollout brake. No injected source, clock or approval resolver exists.
type MemoryReinforcementService struct {
	store *Store
	flags *agentfeature.Controller
}

func NewMemoryReinforcementService(s *Store, flags *agentfeature.Controller) *MemoryReinforcementService {
	return &MemoryReinforcementService{store: s, flags: flags}
}

type MemoryReinforcementPreview struct {
	service                *MemoryReinforcementService
	binding                agentPrivateBinding
	metadataVersion        int64
	memoryID               string
	memoryVersion, version int64
	entries                []agentreinforcement.Entry
	digest                 string
	created, expires       time.Time
	ticket                 agentfeature.Ticket
	view                   agentreinforcement.View
}

func (p *MemoryReinforcementPreview) View() agentreinforcement.View {
	if p == nil {
		return agentreinforcement.View{}
	}
	v := p.view
	if v.Assessment.Value != nil {
		value := *v.Assessment.Value
		v.Assessment.Value = &value
	}
	if v.LastSupportAt != nil {
		t := *v.LastSupportAt
		v.LastSupportAt = &t
	}
	return v
}
func (p *MemoryReinforcementPreview) Review() agentreinforcement.Review {
	if p == nil {
		return agentreinforcement.Review{}
	}
	items := make([]agentreinforcement.ReviewedEvidence, len(p.entries))
	for i, e := range p.entries {
		items[i] = agentreinforcement.ReviewedEvidence{EvidenceID: e.EvidenceID, EvidenceVersion: e.EvidenceVersion, SnapshotDigest: e.Fingerprint}
	}
	return agentreinforcement.Review{Memory: p.View(), Evidence: items, PlanDigest: p.digest, Purpose: "HUMAN_EXPLICIT_REINFORCEMENT", PreviewedAt: p.created, ExpiresAt: p.expires}
}
func (*MemoryReinforcementPreview) MarshalJSON() ([]byte, error) {
	return nil, agentreinforcement.ErrServerOnly
}
func (p *MemoryReinforcementPreview) UnmarshalJSON([]byte) error {
	*p = MemoryReinforcementPreview{}
	return agentreinforcement.ErrServerOnly
}

type reinforcementState struct {
	version int64
	entries []agentreinforcement.Entry
	digest  *string
	last    *time.Time
}

func (s *MemoryReinforcementService) capture(ctx context.Context) (agentfeature.Ticket, error) {
	if ctx.Err() != nil || s == nil || s.store == nil || s.store.pool == nil {
		return agentfeature.Ticket{}, agentmemory.ErrUnavailable
	}
	t, e := s.flags.Capture(agentfeature.Memory)
	if e != nil {
		return t, agentmemory.ErrUnavailable
	}
	return t, nil
}
func (s *MemoryReinforcementService) begin(ctx context.Context, access agentprofile.PrivateAccess, id string, expected int64) (pgx.Tx, agentPrivateBinding, agentmemory.Record, int64, error) {
	if expected <= 0 || expected == math.MaxInt64 {
		return nil, agentPrivateBinding{}, agentmemory.Record{}, 0, agentmemory.ErrInvalid
	}
	tx, b, m, e := s.store.beginMemoryEvidence(ctx, access, id, true)
	if e != nil {
		return nil, b, m, 0, e
	}
	fail := func(e error) (pgx.Tx, agentPrivateBinding, agentmemory.Record, int64, error) {
		_ = tx.Rollback(ctx)
		return nil, b, agentmemory.Record{}, 0, e
	}
	if m.SourceType != agentmemory.SourceExplicit {
		return fail(agentmemory.ErrUnavailable)
	}
	if m.Version != expected || m.Status != agentmemory.StatusActive {
		return fail(agentmemory.ErrConflict)
	}
	meta, e := lockAgentPrivateMetadata(ctx, tx, b, false)
	if e != nil {
		return fail(memoryError(e))
	}
	return tx, b, m, meta.ProfileVersion, nil
}
func reinforcementReadState(ctx context.Context, tx pgx.Tx, m agentmemory.Record) (reinforcementState, error) {
	var r reinforcementState
	var raw []byte
	var mv int64
	e := tx.QueryRow(ctx, `SELECT memory_version,version,entries,last_plan_digest,last_support_at FROM agent_memory_reinforcement WHERE memory_id=$1 FOR UPDATE`, m.ID).Scan(&mv, &r.version, &raw, &r.digest, &r.last)
	if errors.Is(e, pgx.ErrNoRows) {
		r.entries = []agentreinforcement.Entry{}
		return r, nil
	}
	if e != nil || mv != m.Version {
		return r, agentmemory.ErrUnavailable
	}
	if json.Unmarshal(raw, &r.entries) != nil {
		return r, agentmemory.ErrUnavailable
	}
	r.entries, e = agentreinforcement.NormalizeEntries(r.entries)
	if e != nil {
		return r, agentmemory.ErrUnavailable
	}
	return r, nil
}

// NativeSourceSnapshot has one SQL snapshot for every source ACL, session,
// Memory and cluster link. Raw native data is hashed locally and never returned.
func reinforcementNativeSources(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, m agentmemory.Record) (map[string]agentreinforcement.Entry, time.Time, error) {
	// Source row locks use the same native ordering as 005. ACL-dependent rows
	// are intentionally checked in the final combined statement, not pretrusted.
	rows, e := tx.Query(ctx, `SELECT source_type,source_id FROM agent_memory_evidence WHERE memory_id=$1 AND memory_version=$2 AND status='CURRENT' ORDER BY source_type,source_id`, m.ID, m.Version)
	if e != nil {
		return nil, time.Time{}, agentmemory.ErrUnavailable
	}
	type selector struct {
		kind agentevent.SourceType
		id   string
	}
	var selectors []selector
	for rows.Next() {
		var v selector
		if rows.Scan(&v.kind, &v.id) != nil {
			rows.Close()
			return nil, time.Time{}, agentmemory.ErrUnavailable
		}
		selectors = append(selectors, v)
	}
	rows.Close()
	if rows.Err() != nil || len(selectors) > 100 {
		return nil, time.Time{}, agentmemory.ErrUnavailable
	}
	for _, v := range selectors {
		e = lockMemoryEvidenceSource(ctx, tx, b.accountID, v.kind, v.id)
		if e != nil && !errors.Is(e, agentmemory.ErrForbidden) {
			return nil, time.Time{}, e
		}
	}
	moment, _ := memoryEvidenceSourceSQL(agentevent.MomentSource)
	participation, _ := memoryEvidenceSourceSQL(agentevent.ParticipationSource)
	place, _ := memoryEvidenceSourceSQL(agentevent.SavedPlaceSource)
	query := `SELECT ` + prefixMemoryEvidenceColumns("e") + `,src.revision,src.event_time,src.canonical,clock_timestamp(),
 CASE e.source_type WHEN 'MOMENT' THEN COALESCE((SELECT array_agg('ACTIVITY:'||l.activity_id::text ORDER BY l.activity_id) FROM moment_activity_links l WHERE l.moment_id=e.source_id),ARRAY['MOMENT:'||e.source_id::text])
 WHEN 'ACTIVITY_PARTICIPATION' THEN ARRAY['ACTIVITY:'||(SELECT activity_id::text FROM activity_participations WHERE id=e.source_id)]
 WHEN 'SAVED_PLACE' THEN ARRAY['PLACE:'||(SELECT place_id::text FROM saved_items WHERE id=e.source_id)] END,
 CASE e.source_type WHEN 'MOMENT' THEN jsonb_build_object('epoch',(SELECT xmin::text FROM moments WHERE id=e.source_id),'links',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',activity_id,'epoch',xmin::text) ORDER BY activity_id) FROM moment_activity_links WHERE moment_id=e.source_id),'[]'::jsonb))
 WHEN 'ACTIVITY_PARTICIPATION' THEN jsonb_build_object('epoch',(SELECT xmin::text FROM activity_participations WHERE id=e.source_id))
 WHEN 'SAVED_PLACE' THEN jsonb_build_object('epoch',(SELECT xmin::text FROM saved_items WHERE id=e.source_id)) END
 FROM agent_memory_evidence e JOIN agent_memories m ON m.id=e.memory_id
 JOIN sessions ses ON ses.id=$5 AND ses.account_id=$1
 CROSS JOIN LATERAL (SELECT * FROM (` + stringsReplaceEvidenceSourceID(moment) + `) z WHERE e.source_type='MOMENT'
 UNION ALL SELECT * FROM (` + stringsReplaceEvidenceSourceID(participation) + `) z WHERE e.source_type='ACTIVITY_PARTICIPATION'
 UNION ALL SELECT * FROM (` + stringsReplaceEvidenceSourceID(place) + `) z WHERE e.source_type='SAVED_PLACE') src
 WHERE $2::uuid IS NULL AND e.memory_id=$3 AND e.agent_id=$4 AND e.owner_id=$1 AND e.owner_type='PERSON'
 AND e.status='CURRENT' AND e.memory_version=$6 AND m.version=$6 AND m.owner_id=$1 AND m.agent_id=$4 AND m.source_type='EXPLICIT' AND m.status='ACTIVE'
 AND m.valid_from<=clock_timestamp() AND m.valid_until>clock_timestamp() AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp() ORDER BY e.id`
	final, e := tx.Query(ctx, query, b.accountID, nil, m.ID, b.agentID, b.sessionID, m.Version)
	if e != nil {
		return nil, time.Time{}, agentmemory.ErrUnavailable
	}
	out := map[string]agentreinforcement.Entry{}
	var now time.Time
	for final.Next() {
		var rev int64
		var event, observed time.Time
		var canonical, anchorRaw []byte
		var anchors []string
		ev, se := scanMemoryEvidence(extraEvidenceRow{row: final, targets: []any{&rev, &event, &canonical, &observed, &anchors, &anchorRaw}})
		if se != nil {
			final.Close()
			return nil, now, agentmemory.ErrUnavailable
		}
		v := agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: rev}
		if ev.Source.Type != agentevent.MomentSource {
			kind := agentevent.UpdatedAtDigestVersion
			if ev.Source.Type == agentevent.SavedPlaceSource {
				kind = agentevent.CreatedAtDigestVersion
			}
			v, se = agentevent.SnapshotVersion(ev.Source.Type, kind, event, canonical)
		}
		if se != nil || event.Year() < 1 || event.Year() > 9999 || observed.Year() < 1 || observed.Year() > 9999 {
			final.Close()
			return nil, now, agentmemory.ErrUnavailable
		}
		now = observed.UTC()
		if v != ev.Source.Version || ev.EventTime == nil || !event.Equal(*ev.EventTime) || event.After(now) || ev.ObservedAt.After(now) {
			continue
		}
		// Include Evidence metadata, source epoch and retained link epochs. A delete
		// plus same-ID rebuild and link edits invalidate the old human preview.
		digest, _ := agentreinforcement.Digest(struct {
			Evidence agentmemory.Evidence
			Native   json.RawMessage
		}{ev, anchorRaw})
		ent := agentreinforcement.Entry{EvidenceID: ev.ID, EvidenceVersion: ev.Version, Fingerprint: digest, Anchors: anchors}
		valid, se := agentreinforcement.NormalizeEntries([]agentreinforcement.Entry{ent})
		if se != nil {
			final.Close()
			return nil, now, agentmemory.ErrUnavailable
		}
		out[ev.ID] = valid[0]
	}
	final.Close()
	if final.Err() != nil {
		return nil, now, agentmemory.ErrUnavailable
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return nil, now, agentmemory.ErrUnavailable
	}
	return out, now.UTC(), nil
}
func reinforcementCurrentEntries(old []agentreinforcement.Entry, current map[string]agentreinforcement.Entry) []agentreinforcement.Entry {
	out := []agentreinforcement.Entry{}
	for _, v := range old {
		if c, ok := current[v.EvidenceID]; ok && reflect.DeepEqual(c, v) {
			out = append(out, v)
		}
	}
	return out
}
func reinforcementClearStale(ctx context.Context, tx pgx.Tx, m agentmemory.Record, r reinforcementState, current map[string]agentreinforcement.Entry) (reinforcementState, error) {
	// Conservative whole clear: a lost support requires a new concrete approval
	// for the surviving set, never silent reuse of its obsolete combined plan.
	if len(reinforcementCurrentEntries(r.entries, current)) == len(r.entries) {
		return r, nil
	}
	if r.version == math.MaxInt64 {
		return r, agentmemory.ErrConflict
	}
	_, e := tx.Exec(ctx, `UPDATE agent_memory_reinforcement SET version=version+1,entries='[]',last_plan_digest=NULL,last_support_at=NULL WHERE memory_id=$1 AND version=$2`, m.ID, r.version)
	if e != nil {
		return r, memoryError(e)
	}
	r.version++
	r.entries = []agentreinforcement.Entry{}
	r.digest = nil
	r.last = nil
	return r, nil
}
func (s *MemoryReinforcementService) finish(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, m agentmemory.Record, ticket agentfeature.Ticket, expires *time.Time) error {
	if ctx.Err() != nil || !s.flags.Current(ticket) {
		return agentmemory.ErrUnavailable
	}
	if e := recheckMemoryPutBoundary(ctx, tx, b, m.ValidUntil); e != nil {
		return e
	}
	if expires != nil {
		var now time.Time
		if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
			return agentmemory.ErrUnavailable
		}
		if !expires.After(now) {
			return agentmemory.ErrConflict
		}
	}
	if ctx.Err() != nil || !s.flags.Current(ticket) {
		return agentmemory.ErrUnavailable
	}
	if e := tx.Commit(ctx); e != nil {
		return memoryError(e)
	}
	return nil
}

// Preview is native human review only, not a model-generated suggestion.
func (s *MemoryReinforcementService) PreviewOwnReinforcement(ctx context.Context, access agentprofile.PrivateAccess, id string, expectedMemory, expectedSupport int64, evidenceIDs []string) (*MemoryReinforcementPreview, error) {
	ticket, e := s.capture(ctx)
	if e != nil {
		return nil, e
	}
	if expectedSupport < 0 || expectedSupport == math.MaxInt64 || len(evidenceIDs) == 0 || len(evidenceIDs) > 100 {
		return nil, agentmemory.ErrInvalid
	}
	tx, b, m, meta, e := s.begin(ctx, access, id, expectedMemory)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	r, e := reinforcementReadState(ctx, tx, m)
	if e != nil {
		return nil, e
	}
	current, now, e := reinforcementNativeSources(ctx, tx, b, m)
	if e != nil {
		return nil, e
	}
	r, e = reinforcementClearStale(ctx, tx, m, r, current)
	if e != nil {
		return nil, e
	}
	if r.version != expectedSupport {
		return nil, agentmemory.ErrConflict
	}
	merged := map[string]agentreinforcement.Entry{}
	for _, v := range r.entries {
		merged[v.EvidenceID] = v
	}
	seen := map[string]bool{}
	for _, eid := range evidenceIDs {
		n, se := agentmemory.NormalizeEvidenceID(eid)
		if se != nil || n != eid || seen[eid] {
			return nil, agentmemory.ErrInvalid
		}
		seen[eid] = true
		v, ok := current[eid]
		if !ok {
			return nil, agentmemory.ErrForbidden
		}
		merged[eid] = v
	}
	entries := make([]agentreinforcement.Entry, 0, len(merged))
	for _, v := range merged {
		entries = append(entries, v)
	}
	entries, e = agentreinforcement.NormalizeEntries(entries)
	if e != nil {
		return nil, e
	}
	expires := now.Add(agentreinforcement.PreviewLease)
	if m.ValidUntil.Before(expires) {
		expires = m.ValidUntil
	}
	var sessionUntil time.Time
	if tx.QueryRow(ctx, `SELECT LEAST(expires_at,idle_expires_at) FROM sessions WHERE id=$1`, b.sessionID).Scan(&sessionUntil) != nil {
		return nil, agentmemory.ErrUnavailable
	}
	if sessionUntil.Before(expires) {
		expires = sessionUntil
	}
	if !expires.After(now) {
		return nil, agentmemory.ErrConflict
	}
	p := &MemoryReinforcementPreview{service: s, binding: b, metadataVersion: meta, memoryID: m.ID, memoryVersion: m.Version, version: r.version, entries: entries, created: now, expires: expires, ticket: ticket}
	p.digest, e = agentreinforcement.Digest(struct {
		Memory                                  string
		MemoryVersion, SupportVersion, Metadata int64
		Binding                                 [3]string
		Entries                                 []agentreinforcement.Entry
		Expires                                 time.Time
	}{m.ID, m.Version, r.version, meta, [3]string{b.accountID, b.agentID, b.sessionID}, entries, expires})
	if e != nil {
		return nil, e
	}
	p.view, e = agentreinforcement.BuildView(m, r.version, entries, &now, now)
	if e != nil {
		return nil, e
	}
	if e = s.finish(ctx, tx, b, m, ticket, &expires); e != nil {
		return nil, e
	}
	return p, nil
}

// Approve consumes exactly this native preview as an explicit owner action.
// It persists support, never Memory content, a confidence probability or grant.
func (s *MemoryReinforcementService) ApproveOwnReinforcement(ctx context.Context, access agentprofile.PrivateAccess, p *MemoryReinforcementPreview) (agentreinforcement.View, error) {
	if p == nil || p.service != s || s == nil || !s.flags.Current(p.ticket) || p.digest == "" {
		return agentreinforcement.View{}, agentmemory.ErrForbidden
	}
	tx, b, m, meta, e := s.begin(ctx, access, p.memoryID, p.memoryVersion)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	defer tx.Rollback(ctx)
	if b != p.binding || meta != p.metadataVersion {
		return agentreinforcement.View{}, agentmemory.ErrConflict
	}
	r, e := reinforcementReadState(ctx, tx, m)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	current, now, e := reinforcementNativeSources(ctx, tx, b, m)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	if now.Before(p.created) || !p.expires.After(now) {
		return agentreinforcement.View{}, agentmemory.ErrConflict
	}
	for _, v := range p.entries {
		if c, ok := current[v.EvidenceID]; !ok || !reflect.DeepEqual(v, c) {
			return agentreinforcement.View{}, agentmemory.ErrConflict
		}
	}
	retry := r.digest != nil && *r.digest == p.digest && r.version == p.version+1 && reflect.DeepEqual(r.entries, p.entries)
	if !retry {
		if r.version != p.version {
			return agentreinforcement.View{}, agentmemory.ErrConflict
		}
		raw, _ := json.Marshal(p.entries)
		if r.version == 0 {
			e = tx.QueryRow(ctx, `INSERT INTO agent_memory_reinforcement(memory_id,memory_version,entries,last_plan_digest,last_support_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5,$5) RETURNING version,last_support_at`, m.ID, m.Version, raw, p.digest, now).Scan(&r.version, &r.last)
		} else {
			e = tx.QueryRow(ctx, `UPDATE agent_memory_reinforcement SET version=version+1,entries=$3,last_plan_digest=$4,last_support_at=$5 WHERE memory_id=$1 AND version=$2 RETURNING version,last_support_at`, m.ID, r.version, raw, p.digest, now).Scan(&r.version, &r.last)
		}
		if e != nil {
			return agentreinforcement.View{}, memoryError(e)
		}
		r.entries = p.entries
	}
	final, checked, e := reinforcementNativeSources(ctx, tx, b, m)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	for _, v := range p.entries {
		if c, ok := final[v.EvidenceID]; !ok || !reflect.DeepEqual(v, c) {
			return agentreinforcement.View{}, agentmemory.ErrConflict
		}
	}
	view, e := agentreinforcement.BuildView(m, r.version, r.entries, r.last, checked)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	if e = s.finish(ctx, tx, b, m, p.ticket, &p.expires); e != nil {
		return agentreinforcement.View{}, e
	}
	return view, nil
}
func (s *MemoryReinforcementService) ReadOwnReinforcement(ctx context.Context, access agentprofile.PrivateAccess, id string, expectedMemory int64) (agentreinforcement.View, error) {
	ticket, e := s.capture(ctx)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	tx, b, m, _, e := s.begin(ctx, access, id, expectedMemory)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	defer tx.Rollback(ctx)
	r, e := reinforcementReadState(ctx, tx, m)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	current, now, e := reinforcementNativeSources(ctx, tx, b, m)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	r, e = reinforcementClearStale(ctx, tx, m, r, current)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	// An authenticated owner read may scrub expired support, but must not
	// return an expired Memory projection. Time expiry has no native trigger.
	if m.ValidFrom.After(now) || !m.ValidUntil.After(now) {
		if ctx.Err() != nil || !s.flags.Current(ticket) {
			return agentreinforcement.View{}, agentmemory.ErrUnavailable
		}
		if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
			return agentreinforcement.View{}, memoryError(e)
		}
		if e = tx.Commit(ctx); e != nil {
			return agentreinforcement.View{}, memoryError(e)
		}
		return agentreinforcement.View{}, agentmemory.ErrNotFound
	}
	v, e := agentreinforcement.BuildView(m, r.version, r.entries, r.last, now)
	if e != nil {
		return agentreinforcement.View{}, e
	}
	if e = s.finish(ctx, tx, b, m, ticket, nil); e != nil {
		return agentreinforcement.View{}, e
	}
	return v, nil
}
func (*MemoryReinforcementService) ReinforceForCognition(context.Context) error {
	return agentcognitive.ErrUnavailable
}
