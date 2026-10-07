package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcitymemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ agentcitymemory.Store = (*Store)(nil)

func cityMemoryError(e error) error {
	switch {
	case e == nil:
		return nil
	case errors.Is(e, context.Canceled):
		return context.Canceled
	case errors.Is(e, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(e, agentprofile.ErrForbidden), errors.Is(e, agentmemory.ErrForbidden):
		return agentcitymemory.ErrForbidden
	case errors.Is(e, agentprofile.ErrInvalid), errors.Is(e, agentmemory.ErrInvalid):
		return agentcitymemory.ErrInvalid
	case errors.Is(e, pgx.ErrNoRows), errors.Is(e, agentprofile.ErrNotFound), errors.Is(e, agentmemory.ErrNotFound):
		return agentcitymemory.ErrNotFound
	case errors.Is(e, agentmemory.ErrConflict):
		return agentcitymemory.ErrConflict
	default:
		return agentcitymemory.ErrUnavailable
	}
}
func (s *Store) beginOwnCityMemory(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, agentPrivateBinding, error) {
	if ctx == nil {
		return nil, agentPrivateBinding{}, agentcitymemory.ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return nil, agentPrivateBinding{}, e
	}
	if e := agentprofile.ValidatePrivateAccess(a); e != nil {
		return nil, agentPrivateBinding{}, cityMemoryError(e)
	}
	if s == nil || s.pool == nil {
		return nil, agentPrivateBinding{}, agentcitymemory.ErrUnavailable
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, agentPrivateBinding{}, cityMemoryError(e)
	}
	fail := func(e error) (pgx.Tx, agentPrivateBinding, error) {
		_ = tx.Rollback(context.Background())
		return nil, agentPrivateBinding{}, cityMemoryError(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(e)
	}
	b, e := lockOwnAgentPrivateBinding(ctx, tx, a, s.devPhoneEnabled)
	if e != nil {
		return fail(e)
	}
	if _, e = lockAgentPrivateMetadata(ctx, tx, b, write); e != nil {
		return fail(e)
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		return fail(e)
	}
	return tx, b, nil
}

// The payload's final statement is the source/authority linearization point.
// Context has no revision: its canonical native row + xmin token is deliberately
// local to this projection, not a new agentevent version or an analysis permit.
const ownCityMemoryCurrentSQL = `/* own_city_memory_current_v1 */
 WITH observed AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT actor.id::text,ag.id::text,
 encode(sha256(convert_to(jsonb_build_object('session',ses.id,'actor',to_jsonb(actor),'actor_xmin',actor.xmin::text,
 'agent',to_jsonb(ag),'agent_xmin',ag.xmin::text,'metadata',to_jsonb(ap),'metadata_xmin',ap.xmin::text)::text,'UTF8')),'hex'),
 encode(sha256(convert_to(jsonb_build_object('city',to_jsonb(city),'city_xmin',city.xmin::text)::text,'UTF8')),'hex'),
 observed.at,LEAST(observed.at+interval '2 minutes',ses.expires_at,ses.idle_expires_at,city.expires_at),
 (SELECT count(*) FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id
  WHERE pc.person_account_id=actor.id AND pc.relation='current' AND c.context_type='CITY'),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',c.id,'createdAt',pc.created_at,
  'token',encode(sha256(convert_to(jsonb_build_object('row',to_jsonb(pc),'row_xmin',pc.xmin::text,
   'context',to_jsonb(c),'context_xmin',c.xmin::text)::text,'UTF8')),'hex')) ORDER BY c.id)
  FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id
  WHERE pc.person_account_id=actor.id AND pc.relation='current' AND pc.visibility='private' AND c.context_type='CITY' AND c.city_id=city.id),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('schemaVersion','agent-memory-v1','id',m.id,'agentId',m.agent_id,
 'ownerType',m.owner_type,'ownerId',m.owner_id,'version',m.version,'memoryType',m.memory_type,'memoryKey',m.memory_key,
 'summary',m.summary,'structuredValue',m.structured_value,'confidence',m.confidence,'sourceType',m.source_type,
 'visibility',m.visibility,'status',m.status,'validFrom',m.valid_from,'validUntil',m.valid_until,
 'lastReinforcedAt',m.last_reinforced_at,'createdAt',m.created_at,'updatedAt',m.updated_at) ORDER BY m.memory_key,m.id)
 FROM(SELECT memory.* FROM agent_memories memory WHERE memory.owner_id=actor.id AND memory.owner_type='PERSON'
 AND memory.agent_id=ag.id AND memory.memory_type='CITY' AND memory.source_type='EXPLICIT'
 AND memory.status='ACTIVE' AND memory.valid_from<=clock_timestamp() AND memory.valid_until>clock_timestamp()
 AND memory.structured_value->>'schemaVersion'='agent.city_declaration.v1' AND memory.structured_value->>'cityId'=city.id
 ORDER BY memory.memory_key,memory.id LIMIT 5)m),'[]'::jsonb)
 FROM cities city CROSS JOIN observed
 JOIN sessions ses ON ses.id=$1 AND ses.account_id=$2
 JOIN accounts actor ON actor.id=ses.account_id AND actor.id=$2 AND actor.account_type='person' AND actor.status='active'
 JOIN agents ag ON ag.id=$3 AND ag.principal_account_id=actor.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=actor.id AND ap.owner_type='PERSON'
 WHERE city.id=$4 AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>clock_timestamp())
 AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp()`

func readOwnCityMemory(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, city string) (agentcitymemory.Projection, error) {
	p := agentcitymemory.Projection{SchemaVersion: agentcitymemory.Schema, Owner: actorref.PrincipalRef{Type: actorref.Person}, CityID: city, Signals: []agentcitymemory.Signal{}, Verification: "UNAVAILABLE", HistoricalDates: "NOT_PROVIDED", ProcessingStatus: "UNAVAILABLE"}
	var contextRaw, memoryRaw []byte
	var currentCount int
	e := tx.QueryRow(ctx, ownCityMemoryCurrentSQL, b.sessionID, b.accountID, b.agentID, city).Scan(&p.Owner.ID, &p.AgentID, &p.AuthorityDigest, &p.TargetDigest, &p.ObservedAt, &p.ExpiresAt, &currentCount, &contextRaw, &memoryRaw)
	if e != nil {
		return agentcitymemory.Projection{}, cityMemoryError(e)
	}
	if currentCount > 1 {
		return agentcitymemory.Projection{}, agentcitymemory.ErrUnavailable
	}
	p.ObservedAt = p.ObservedAt.UTC()
	p.ExpiresAt = p.ExpiresAt.UTC()
	var contexts []struct {
		ID        string    `json:"id"`
		CreatedAt time.Time `json:"createdAt"`
		Token     string    `json:"token"`
	}
	var memories []agentmemory.Record
	if json.Unmarshal(contextRaw, &contexts) != nil || json.Unmarshal(memoryRaw, &memories) != nil || len(contexts)+len(memories) > agentcitymemory.MaxSignals {
		return agentcitymemory.Projection{}, agentcitymemory.ErrUnavailable
	}
	for _, c := range contexts {
		p.Signals = append(p.Signals, agentcitymemory.Signal{Kind: agentcitymemory.Current, Basis: agentcitymemory.SelfDeclaration, Explanation: agentcitymemory.Explanation(agentcitymemory.Current), Source: agentcitymemory.Source{Kind: agentcitymemory.ContextSource, ID: c.ID, Token: c.Token}, RecordCreatedAt: c.CreatedAt.UTC(), Visibility: agentmemory.VisibilityPrivate})
	}
	for _, r := range memories {
		d, e := agentcitymemory.DecodeDeclaration(r)
		if e != nil || d.CityID != city || r.OwnerID != b.accountID || r.AgentID != b.agentID {
			return agentcitymemory.Projection{}, agentcitymemory.ErrUnavailable
		}
		until, updated := r.ValidUntil.UTC(), r.UpdatedAt.UTC()
		if until.Before(p.ExpiresAt) {
			p.ExpiresAt = until
		}
		p.Signals = append(p.Signals, agentcitymemory.Signal{Kind: d.Kind, Basis: agentcitymemory.SelfDeclaration, Explanation: agentcitymemory.Explanation(d.Kind), Source: agentcitymemory.Source{Kind: agentcitymemory.MemorySource, ID: r.ID, Revision: r.Version}, RecordCreatedAt: r.CreatedAt.UTC(), SourceUpdatedAt: &updated, ValidUntil: &until, Visibility: r.Visibility})
	}
	p.SnapshotID = agentcitymemory.SnapshotID(p)
	if e = agentcitymemory.Validate(p, p.ObservedAt); e != nil {
		return agentcitymemory.Projection{}, agentcitymemory.ErrUnavailable
	}
	issued, e := agentcitymemory.IssueProjection(p)
	if e != nil {
		return agentcitymemory.Projection{}, agentcitymemory.ErrUnavailable
	}
	return issued, nil
}

func (s *Store) ReadOwnCityMemory(ctx context.Context, a agentprofile.PrivateAccess, city string) (agentcitymemory.Projection, error) {
	if !agentcitymemory.ValidCity(city) {
		return agentcitymemory.Projection{}, agentcitymemory.ErrInvalid
	}
	tx, b, e := s.beginOwnCityMemory(ctx, a, false)
	if e != nil {
		return agentcitymemory.Projection{}, e
	}
	defer tx.Rollback(context.Background())
	first, e := readOwnCityMemory(ctx, tx, b, city)
	if e != nil {
		return agentcitymemory.Projection{}, e
	}
	p, e := readOwnCityMemory(ctx, tx, b, city)
	if e != nil {
		return agentcitymemory.Projection{}, e
	}
	if p.SnapshotID != first.SnapshotID {
		return agentcitymemory.Projection{}, agentcitymemory.ErrForbidden
	}
	// A finite source/session lease must still be live after SQL/decoding waits.
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return agentcitymemory.Projection{}, cityMemoryError(e)
	}
	if e = agentcitymemory.Validate(p, now.UTC()); e != nil {
		return agentcitymemory.Projection{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return agentcitymemory.Projection{}, cityMemoryError(e)
	}
	if e = ctx.Err(); e != nil {
		return agentcitymemory.Projection{}, e
	}
	return p, nil
}
func (s *Store) RevalidateOwnCityMemory(ctx context.Context, a agentprofile.PrivateAccess, old agentcitymemory.Projection) error {
	if e := agentcitymemory.ValidateIssued(old); e != nil {
		return e
	}
	p, e := s.ReadOwnCityMemory(ctx, a, old.CityID)
	if e != nil {
		return e
	}
	if e = agentcitymemory.Validate(old, p.ObservedAt); e != nil {
		return e
	}
	if old.SnapshotID != p.SnapshotID {
		return agentcitymemory.ErrForbidden
	}
	return nil
}

func (s *Store) PutOwnCityDeclaration(ctx context.Context, a agentprofile.PrivateAccess, id string, in agentcitymemory.PutDeclarationInput) (agentmemory.Record, error) {
	if !agentcitymemory.ValidID(id) || !agentcitymemory.ValidCity(in.CityID) {
		return agentmemory.Record{}, agentcitymemory.ErrInvalid
	}
	tx, b, e := s.beginOwnCityMemory(ctx, a, true)
	if e != nil {
		return agentmemory.Record{}, e
	}
	defer tx.Rollback(context.Background())
	var city string
	if e = tx.QueryRow(ctx, `SELECT id FROM cities WHERE id=$1 AND publication_status='published'
 AND(expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE`, in.CityID).Scan(&city); e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	input, e := agentcitymemory.NewMemoryInput(in, now)
	if e != nil {
		return agentmemory.Record{}, e
	}
	current, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' FOR UPDATE`, id, b.agentID, b.accountID))
	if e == nil {
		d, decode := agentcitymemory.DecodeDeclaration(current)
		if decode != nil || d.CityID != city || d.Kind != in.Kind {
			return agentmemory.Record{}, agentcitymemory.ErrConflict
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	record, _, e := putOwnMemoryInTx(ctx, tx, b, a.WorkspacePrincipal, id, input)
	if e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	var live bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cities c JOIN sessions ses ON ses.id=$2 AND ses.account_id=$3
 JOIN agent_memories m ON m.id=$4 AND m.agent_id=$5 AND m.owner_id=$3 AND m.owner_type='PERSON' AND m.version=$6
 WHERE c.id=$1 AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp()
 AND m.status='ACTIVE' AND m.source_type='EXPLICIT' AND m.valid_until>clock_timestamp())`, city, b.sessionID, b.accountID, id, b.agentID, record.Version).Scan(&live)
	if e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	if !live {
		return agentmemory.Record{}, agentcitymemory.ErrForbidden
	}
	if e = tx.Commit(ctx); e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	if e = ctx.Err(); e != nil {
		return agentmemory.Record{}, e
	}
	return record, nil
}

// The initial namespace check plus native CAS prevents a typed convenience
// path from deleting unrelated Memory. Hidden/expired cities do not block scrub.
func (s *Store) DeleteOwnCityDeclaration(ctx context.Context, a agentprofile.PrivateAccess, id string, version int64) (agentmemory.Record, error) {
	if !agentcitymemory.ValidID(id) {
		return agentmemory.Record{}, agentcitymemory.ErrInvalid
	}
	if e := agentmemory.ValidateDeleteInput(agentmemory.DeleteInput{ExpectedVersion: version}); e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	tx, b, e := s.beginOwnCityMemory(ctx, a, false)
	if e != nil {
		return agentmemory.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' FOR SHARE`, id, b.agentID, b.accountID))
	if e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	// Bind namespace review to this exact native version before releasing the
	// first transaction. A caller cannot preselect a future version and delete
	// a different Memory type written during the second transaction's gap.
	if r.Version != version && !(r.Status == agentmemory.StatusDeleted && r.Version > 1 && r.Version-1 == version) {
		return agentmemory.Record{}, agentcitymemory.ErrConflict
	}
	if r.Status == agentmemory.StatusDeleted {
		if r.MemoryType != agentmemory.TypeCity || r.SourceType != agentmemory.SourceExplicit || !agentcitymemory.ValidDeclarationKey(r.MemoryKey) {
			return agentmemory.Record{}, agentcitymemory.ErrConflict
		}
	} else if _, e = agentcitymemory.DecodeDeclaration(r); e != nil {
		return agentmemory.Record{}, agentcitymemory.ErrConflict
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	gone, e := s.DeleteOwnMemory(ctx, a, id, version)
	if e != nil {
		return agentmemory.Record{}, cityMemoryError(e)
	}
	if e = ctx.Err(); e != nil {
		return agentmemory.Record{}, e
	}
	return gone, nil
}
