package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentplacememory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

var _ agentplacememory.Store = (*Store)(nil)

func placeMemoryError(e error) error {
	switch {
	case e == nil:
		return nil
	case errors.Is(e, context.Canceled):
		return context.Canceled
	case errors.Is(e, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(e, agentprofile.ErrForbidden), errors.Is(e, agentmemory.ErrForbidden):
		return agentplacememory.ErrForbidden
	case errors.Is(e, agentprofile.ErrInvalid), errors.Is(e, agentmemory.ErrInvalid):
		return agentplacememory.ErrInvalid
	case errors.Is(e, pgx.ErrNoRows), errors.Is(e, agentprofile.ErrNotFound), errors.Is(e, agentmemory.ErrNotFound):
		return agentplacememory.ErrNotFound
	case errors.Is(e, agentmemory.ErrConflict):
		return agentplacememory.ErrConflict
	default:
		return agentplacememory.ErrUnavailable
	}
}
func (s *Store) beginOwnPlaceMemory(ctx context.Context, access agentprofile.PrivateAccess, write bool) (pgx.Tx, agentPrivateBinding, error) {
	if ctx == nil {
		return nil, agentPrivateBinding{}, agentplacememory.ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return nil, agentPrivateBinding{}, e
	}
	if e := agentprofile.ValidatePrivateAccess(access); e != nil {
		return nil, agentPrivateBinding{}, placeMemoryError(e)
	}
	if s == nil || s.pool == nil {
		return nil, agentPrivateBinding{}, agentplacememory.ErrUnavailable
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, agentPrivateBinding{}, placeMemoryError(e)
	}
	fail := func(e error) (pgx.Tx, agentPrivateBinding, error) {
		_ = tx.Rollback(context.Background())
		return nil, agentPrivateBinding{}, placeMemoryError(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(e)
	}
	b, e := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if e != nil {
		return fail(e)
	}
	if _, e = lockAgentPrivateMetadata(ctx, tx, b, write); e != nil {
		return fail(e)
	}
	return tx, b, nil
}

// One current statement checks the exact self binding, all target states and
// every selected source. Row timestamps describe records, not real visits.
const ownPlaceMemoryCurrentSQL = `/* own_place_memory_current_v1 */
 WITH observed AS MATERIALIZED(SELECT clock_timestamp() AS at)
 SELECT actor.id::text,ag.id::text,p.city_id,
 encode(sha256(convert_to(jsonb_build_object('session',ses.id,'actor',to_jsonb(actor),'actor_xmin',actor.xmin::text,
  'agent',to_jsonb(ag),'agent_xmin',ag.xmin::text,'metadata',to_jsonb(ap),'metadata_xmin',ap.xmin::text)::text,'UTF8')),'hex'),
 encode(sha256(convert_to(jsonb_build_object('place',p.id,'place_city',p.city_id,'place_status',p.publication_status,
  'place_expiry',p.expires_at,'place_xmin',p.xmin::text,'city',c.id,'city_status',c.publication_status,
  'city_expiry',c.expires_at,'city_xmin',c.xmin::text)::text,'UTF8')),'hex'),
 observed.at,LEAST(observed.at+interval '2 minutes',ses.expires_at,ses.idle_expires_at,p.expires_at,c.expires_at),
 COALESCE((SELECT jsonb_agg(x.value ORDER BY x.kind,x.id) FROM (SELECT native.* FROM (
  SELECT 'SAVED' kind,s.id::text id,jsonb_build_object('kind','SAVED','sourceId',s.id,
    'createdAt',s.created_at,'updatedAt',s.created_at,'revision',0,'canonical',to_jsonb(s)) value
  FROM saved_items s WHERE s.owner_account_id=actor.id AND s.place_id=p.id AND s.activity_id IS NULL AND s.community_id IS NULL
  UNION ALL
  SELECT 'CREATED_MOMENT_AT',m.id::text,jsonb_build_object('kind','CREATED_MOMENT_AT','sourceId',m.id,
    'createdAt',m.created_at,'updatedAt',m.updated_at,'revision',m.revision)
  FROM moments m WHERE m.author_account_id=actor.id AND m.place_id=p.id AND m.city_id=c.id AND m.visibility='private'
    AND m.status='draft' AND m.revision>0 AND length(btrim(m.title||' '||m.body))>0
 ) native ORDER BY native.kind,native.id LIMIT 101) x),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('schemaVersion','agent-memory-v1','id',m.id,'agentId',m.agent_id,
  'ownerType',m.owner_type,'ownerId',m.owner_id,'version',m.version,'memoryType',m.memory_type,'memoryKey',m.memory_key,
  'summary',m.summary,'structuredValue',m.structured_value,'confidence',m.confidence,'sourceType',m.source_type,
  'visibility',m.visibility,'status',m.status,'validFrom',m.valid_from,'validUntil',m.valid_until,
  'lastReinforcedAt',m.last_reinforced_at,'createdAt',m.created_at,'updatedAt',m.updated_at) ORDER BY m.memory_key,m.id)
 FROM (SELECT memory.* FROM agent_memories memory WHERE memory.owner_id=actor.id AND memory.owner_type='PERSON'
  AND memory.agent_id=ag.id AND memory.memory_type='PLACE' AND memory.structured_value->>'placeId'=p.id::text
  AND memory.structured_value->>'schemaVersion'='agent.place_declaration.v1' AND memory.source_type='EXPLICIT'
  AND memory.status='ACTIVE' AND memory.valid_from<=clock_timestamp() AND memory.valid_until>clock_timestamp()
  ORDER BY memory.memory_key,memory.id LIMIT 101) m WHERE m.owner_id=actor.id AND m.owner_type='PERSON' AND m.agent_id=ag.id AND m.memory_type='PLACE'
  AND m.source_type='EXPLICIT' AND m.status='ACTIVE' AND m.valid_from<=clock_timestamp() AND m.valid_until>clock_timestamp()
  AND m.structured_value->>'schemaVersion'='agent.place_declaration.v1' AND m.structured_value->>'placeId'=p.id::text),'[]'::jsonb)
 FROM places p JOIN cities c ON c.id=p.city_id CROSS JOIN observed
 JOIN sessions ses ON ses.id=$1 AND ses.account_id=$2
 JOIN accounts actor ON actor.id=ses.account_id AND actor.id=$2 AND actor.account_type='person' AND actor.status='active'
 JOIN agents ag ON ag.id=$3 AND ag.principal_account_id=actor.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=actor.id AND ap.owner_type='PERSON'
 WHERE p.id=$4 AND p.publication_status='published' AND c.publication_status='published'
 AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=actor.id AND other.agent_type='personal'
   AND other.status='active' AND other.id<>ag.id)`

func readOwnPlaceMemory(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, place string) (agentplacememory.Projection, error) {
	p := agentplacememory.Projection{SchemaVersion: agentplacememory.Schema, PlaceID: place, Owner: actorref.PrincipalRef{Type: actorref.Person},
		Signals: []agentplacememory.Signal{}, VerifiedVisit: "UNAVAILABLE", Attendance: "UNAVAILABLE", ProcessingStatus: "UNAVAILABLE"}
	var sourceRaw, memoryRaw []byte
	e := tx.QueryRow(ctx, ownPlaceMemoryCurrentSQL, b.sessionID, b.accountID, b.agentID, place).Scan(&p.Owner.ID, &p.AgentID, &p.CityID, &p.AuthorityDigest, &p.TargetDigest, &p.ObservedAt, &p.ExpiresAt, &sourceRaw, &memoryRaw)
	if e != nil {
		return agentplacememory.Projection{}, placeMemoryError(e)
	}
	p.ObservedAt = p.ObservedAt.UTC()
	p.ExpiresAt = p.ExpiresAt.UTC()
	var sources []struct {
		Kind      agentplacememory.Kind `json:"kind"`
		SourceID  string                `json:"sourceId"`
		CreatedAt time.Time             `json:"createdAt"`
		UpdatedAt time.Time             `json:"updatedAt"`
		Revision  int64                 `json:"revision"`
		Canonical json.RawMessage       `json:"canonical"`
	}
	var memories []agentmemory.Record
	if json.Unmarshal(sourceRaw, &sources) != nil || json.Unmarshal(memoryRaw, &memories) != nil || len(sources)+len(memories) > agentplacememory.MaxSignals {
		return agentplacememory.Projection{}, agentplacememory.ErrUnavailable
	}
	for _, r := range sources {
		signal := agentplacememory.Signal{Kind: r.Kind, RecordCreatedAt: r.CreatedAt.UTC(), SourceUpdatedAt: r.UpdatedAt.UTC(), Visibility: agentmemory.VisibilityPrivate}
		if r.Kind == agentplacememory.Saved {
			version, err := agentevent.SnapshotVersion(agentevent.SavedPlaceSource, agentevent.CreatedAtDigestVersion, r.CreatedAt, r.Canonical)
			if err != nil {
				return agentplacememory.Projection{}, agentplacememory.ErrUnavailable
			}
			signal.Basis = agentplacememory.CurrentBookmark
			signal.Source = agentplacememory.Source{Kind: agentplacememory.BookmarkSource, ID: r.SourceID, Version: version}
		} else {
			signal.Basis = agentplacememory.CurrentMomentLink
			signal.Source = agentplacememory.Source{Kind: agentplacememory.MomentSource, ID: r.SourceID, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: r.Revision}}
		}
		p.Signals = append(p.Signals, signal)
	}
	for _, r := range memories {
		v, e := agentplacememory.DecodeDeclaration(r)
		if e != nil || v.PlaceID != place || v.CityID != p.CityID {
			return agentplacememory.Projection{}, agentplacememory.ErrUnavailable
		}
		until := r.ValidUntil.UTC()
		if until.Before(p.ExpiresAt) {
			p.ExpiresAt = until
		}
		p.Signals = append(p.Signals, agentplacememory.Signal{Kind: v.Kind, Basis: agentplacememory.SelfDeclaration,
			Source:          agentplacememory.Source{Kind: agentplacememory.DeclarationSource, ID: r.ID, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: r.Version}},
			RecordCreatedAt: r.CreatedAt.UTC(), SourceUpdatedAt: r.UpdatedAt.UTC(), ValidUntil: &until, Visibility: r.Visibility})
	}
	p.SnapshotID = agentplacememory.SnapshotID(p)
	if e = agentplacememory.Validate(p, p.ObservedAt); e != nil {
		return agentplacememory.Projection{}, agentplacememory.ErrUnavailable
	}
	return p, nil
}

// ReadOwnPlaceMemory exposes only current own metadata for this one currently
// public Place. An empty slice is not proof of absence of real-world experience.
func (s *Store) ReadOwnPlaceMemory(ctx context.Context, access agentprofile.PrivateAccess, place string) (agentplacememory.Projection, error) {
	if !agentplacememory.ValidID(place) {
		return agentplacememory.Projection{}, agentplacememory.ErrInvalid
	}
	tx, b, e := s.beginOwnPlaceMemory(ctx, access, false)
	if e != nil {
		return agentplacememory.Projection{}, e
	}
	defer tx.Rollback(context.Background())
	first, e := readOwnPlaceMemory(ctx, tx, b, place)
	if e != nil {
		return agentplacememory.Projection{}, e
	}
	current, e := readOwnPlaceMemory(ctx, tx, b, place)
	if e != nil {
		return agentplacememory.Projection{}, e
	}
	if current.SnapshotID != first.SnapshotID {
		return agentplacememory.Projection{}, agentplacememory.ErrForbidden
	}
	if e = tx.Commit(ctx); e != nil {
		return agentplacememory.Projection{}, placeMemoryError(e)
	}
	if e = ctx.Err(); e != nil {
		return agentplacememory.Projection{}, e
	}
	return current, nil
}

// Revalidate checks current source/target/authority and the old short lease;
// reading a fresh snapshot cannot extend an old response's expiry.
func (s *Store) RevalidateOwnPlaceMemory(ctx context.Context, access agentprofile.PrivateAccess, old agentplacememory.Projection) error {
	if e := agentplacememory.Validate(old, old.ObservedAt); e != nil {
		return e
	}
	current, e := s.ReadOwnPlaceMemory(ctx, access, old.PlaceID)
	if e != nil {
		return e
	}
	if e = agentplacememory.Validate(old, current.ObservedAt); e != nil {
		return e
	}
	if old.SnapshotID != current.SnapshotID {
		return agentplacememory.ErrForbidden
	}
	return nil
}

const lockPlaceDeclarationTargetSQL = `SELECT p.city_id FROM places p JOIN cities c ON c.id=p.city_id
 WHERE p.id=$1 AND p.publication_status='published' AND c.publication_status='published'
 AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()) FOR SHARE OF p,c`

func (s *Store) PutOwnPlaceDeclaration(ctx context.Context, access agentprofile.PrivateAccess, id string, input agentplacememory.PutDeclarationInput) (agentmemory.Record, error) {
	return s.putOwnPlaceDeclaration(ctx, access, "", id, input)
}

var _ agentplacememory.HumanBoundDeclarationStore = (*Store)(nil)

func (s *Store) PutOwnPlaceDeclarationBound(ctx context.Context, access agentprofile.PrivateAccess, expectedAgentID, id string, input agentplacememory.PutDeclarationInput) (agentmemory.Record, error) {
	if !agentplacememory.ValidID(expectedAgentID) {
		return agentmemory.Record{}, agentplacememory.ErrInvalid
	}
	return s.putOwnPlaceDeclaration(ctx, access, expectedAgentID, id, input)
}
func (s *Store) putOwnPlaceDeclaration(ctx context.Context, access agentprofile.PrivateAccess, expectedAgentID, id string, input agentplacememory.PutDeclarationInput) (agentmemory.Record, error) {
	if !agentplacememory.ValidID(id) {
		return agentmemory.Record{}, agentplacememory.ErrInvalid
	}
	if !agentplacememory.ValidID(input.PlaceID) {
		return agentmemory.Record{}, agentplacememory.ErrInvalid
	}
	tx, b, e := s.beginOwnPlaceMemory(ctx, access, true)
	if e != nil {
		return agentmemory.Record{}, e
	}
	defer tx.Rollback(context.Background())
	if expectedAgentID != "" && expectedAgentID != b.agentID {
		return agentmemory.Record{}, agentplacememory.ErrForbidden
	}
	var city string
	var now time.Time
	if e = tx.QueryRow(ctx, lockPlaceDeclarationTargetSQL, input.PlaceID).Scan(&city); e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	in, e := agentplacememory.NewMemoryInput(input, city, now)
	if e != nil {
		return agentmemory.Record{}, e
	}
	current, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' FOR UPDATE`, id, b.agentID, b.accountID))
	if e == nil {
		v, err := agentplacememory.DecodeDeclaration(current)
		if err != nil || v.PlaceID != input.PlaceID || v.CityID != city || v.Kind != input.Kind {
			return agentmemory.Record{}, agentplacememory.ErrConflict
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	record, _, e := putOwnMemoryInTx(ctx, tx, b, actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, id, in)
	if e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	// Place/City may expire while waiting on the canonical Memory writer. Its
	// row locks serialize changes, but expiry still needs the current PG clock.
	var currentID string
	e = tx.QueryRow(ctx, `SELECT p.id::text FROM places p JOIN cities c ON c.id=p.city_id
	 JOIN sessions ses ON ses.id=$2 AND ses.account_id=$3
	 JOIN agent_memories m ON m.id=$4 AND m.agent_id=$5 AND m.owner_id=$3 AND m.owner_type='PERSON' AND m.version=$6
	 WHERE p.id=$1 AND p.publication_status='published' AND c.publication_status='published'
	 AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
	 AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp()
	 AND m.status='ACTIVE' AND m.source_type='EXPLICIT' AND m.valid_until>clock_timestamp()`, input.PlaceID, b.sessionID, b.accountID, id, b.agentID, record.Version).Scan(&currentID)
	if e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	if e = ctx.Err(); e != nil {
		return agentmemory.Record{}, e
	}
	return record, nil
}

// Deletion uses the one original CAS/tombstone writer. The first transaction
// restricts this convenience path to the typed declaration namespace; a change
// in between increments the canonical version and causes the original CAS to
// refuse it. Hidden/expired Places never prevent the owner from deleting data.
func (s *Store) DeleteOwnPlaceDeclaration(ctx context.Context, access agentprofile.PrivateAccess, id string, expected int64) (agentmemory.Record, error) {
	if !agentplacememory.ValidID(id) {
		return agentmemory.Record{}, agentplacememory.ErrInvalid
	}
	tx, b, e := s.beginOwnPlaceMemory(ctx, access, false)
	if e != nil {
		return agentmemory.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' FOR SHARE`, id, b.agentID, b.accountID))
	if e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	if r.Status == agentmemory.StatusDeleted {
		if r.MemoryType != agentmemory.TypePlace || r.SourceType != agentmemory.SourceExplicit {
			return agentmemory.Record{}, agentplacememory.ErrConflict
		}
		validKey := false
		for _, kind := range []agentplacememory.Kind{agentplacememory.Liked, agentplacememory.Visited} {
			// The original tombstone retains only its control key, not the value.
			if len(r.MemoryKey) > 45 {
				place := r.MemoryKey[9:45]
				validKey = validKey || agentplacememory.ValidID(place) && r.MemoryKey == agentplacememory.DeclarationKey(place, kind)
			}
		}
		if !validKey {
			return agentmemory.Record{}, agentplacememory.ErrConflict
		}
	} else if _, e = agentplacememory.DecodeDeclaration(r); e != nil {
		return agentmemory.Record{}, agentplacememory.ErrConflict
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	deleted, e := s.DeleteOwnMemory(ctx, access, id, expected)
	if e != nil {
		return agentmemory.Record{}, placeMemoryError(e)
	}
	return deleted, nil
}
