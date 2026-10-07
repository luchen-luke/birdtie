package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentplacememory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

var _ agentplacememory.HumanControlStore = (*Store)(nil)

func placeControlDigest(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }

// Only this owner's exact typed Place namespace. Never load other Memory
// kinds, public Place details, coordinates, Moments or inferred contents.
func loadOwnPlaceControl(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, place string) (agentplacememory.HumanControl, error) {
	out := agentplacememory.HumanControl{Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}, AgentID: b.agentID, PlaceID: place, Declarations: []agentplacememory.HumanDeclaration{}}
	rows, e := tx.Query(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND memory_type='PLACE' AND source_type='EXPLICIT' AND memory_key IN($3,$4) AND status IN('ACTIVE','EXPIRED') ORDER BY id LIMIT 101`, b.agentID, b.accountID, agentplacememory.DeclarationKey(place, agentplacememory.Liked), agentplacememory.DeclarationKey(place, agentplacememory.Visited))
	if e != nil {
		return agentplacememory.HumanControl{}, placeMemoryError(e)
	}
	var records []agentmemory.Record
	for rows.Next() {
		r, e := scanAgentMemory(rows)
		if e != nil {
			rows.Close()
			return agentplacememory.HumanControl{}, placeMemoryError(e)
		}
		records = append(records, r)
	}
	rows.Close()
	if rows.Err() != nil {
		return agentplacememory.HumanControl{}, agentplacememory.ErrUnavailable
	}
	if len(records) > agentplacememory.MaxSignals {
		return agentplacememory.HumanControl{}, agentplacememory.ErrUnavailable
	}
	var authority, source []byte
	e = tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at)
 SELECT stamp.at,LEAST(stamp.at+interval '2 minutes',s.expires_at,s.idle_expires_at),
 jsonb_build_object('sessionId',s.id,'account',to_jsonb(a),'accountRow',a.xmin::text,'agent',to_jsonb(ag),'agentRow',ag.xmin::text,'metadata',to_jsonb(meta),'metadataRow',meta.xmin::text),
 (SELECT coalesce(jsonb_agg(jsonb_build_object('id',m.id,'row',m.xmin::text) ORDER BY m.id),'[]'::jsonb) FROM (SELECT m.id,m.xmin::text AS xmin FROM agent_memories m WHERE m.agent_id=ag.id AND m.owner_id=a.id AND m.owner_type='PERSON' AND m.memory_type='PLACE' AND m.source_type='EXPLICIT' AND m.memory_key IN($4,$5) AND m.status IN('ACTIVE','EXPIRED') ORDER BY m.id LIMIT 101) m)
 FROM stamp,sessions s JOIN accounts a ON a.id=s.account_id AND a.account_type='person' AND a.status='active'
 JOIN agents ag ON ag.id=$3 AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles meta ON meta.agent_id=ag.id AND meta.owner_type='PERSON' AND meta.owner_id=a.id
 WHERE s.id=$1 AND a.id=$2 AND s.revoked_at IS NULL AND s.expires_at>stamp.at AND s.idle_expires_at>stamp.at
 AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=a.id AND other.agent_type='personal' AND other.status='active' AND other.id<>ag.id)`, b.sessionID, b.accountID, b.agentID, agentplacememory.DeclarationKey(place, agentplacememory.Liked), agentplacememory.DeclarationKey(place, agentplacememory.Visited)).Scan(&out.ObservedAt, &out.ExpiresAt, &authority, &source)
	if e != nil {
		return agentplacememory.HumanControl{}, placeMemoryError(e)
	}
	out.AuthorityStamp = placeControlDigest(authority)
	out.SourceStamp = placeControlDigest(source)
	for _, r := range records {
		d, e := agentplacememory.DecodeDeclaration(r)
		if e != nil || r.OwnerID != b.accountID || r.AgentID != b.agentID || d.PlaceID != place {
			return agentplacememory.HumanControl{}, agentplacememory.ErrUnavailable
		}
		status := agentmemory.StatusExpired
		if r.Status == agentmemory.StatusActive && r.ValidUntil.After(out.ObservedAt) {
			status = agentmemory.StatusActive
			if r.ValidUntil.Before(out.ExpiresAt) {
				out.ExpiresAt = r.ValidUntil
			}
		}
		out.Declarations = append(out.Declarations, agentplacememory.HumanDeclaration{MemoryID: r.ID, Version: r.Version, Kind: d.Kind, Basis: d.Basis, Visibility: r.Visibility, Status: status, ValidUntil: r.ValidUntil, UpdatedAt: r.UpdatedAt})
	}
	out.SnapshotID = agentplacememory.HumanControlSnapshot(out)
	if e = agentplacememory.ValidateHumanControl(out, out.ObservedAt); e != nil {
		return agentplacememory.HumanControl{}, e
	}
	return out, nil
}
func (s *Store) ReadOwnPlaceDeclarationControls(ctx context.Context, a agentprofile.PrivateAccess, place string) (agentplacememory.HumanControl, error) {
	if !agentplacememory.ValidID(place) {
		return agentplacememory.HumanControl{}, agentplacememory.ErrInvalid
	}
	tx, b, e := s.beginOwnPlaceMemory(ctx, a, false)
	if e != nil {
		return agentplacememory.HumanControl{}, e
	}
	defer tx.Rollback(context.Background())
	first, e := loadOwnPlaceControl(ctx, tx, b, place)
	if e != nil {
		return agentplacememory.HumanControl{}, e
	}
	last, e := loadOwnPlaceControl(ctx, tx, b, place)
	if e != nil {
		return agentplacememory.HumanControl{}, e
	}
	if first.SnapshotID != last.SnapshotID {
		return agentplacememory.HumanControl{}, agentplacememory.ErrForbidden
	}
	if e = tx.Commit(ctx); e != nil {
		return agentplacememory.HumanControl{}, agentplacememory.ErrUnavailable
	}
	if ctx.Err() != nil {
		return agentplacememory.HumanControl{}, agentplacememory.ErrUnavailable
	}
	return last, nil
}
func (s *Store) RevalidateOwnPlaceDeclarationControls(ctx context.Context, a agentprofile.PrivateAccess, old agentplacememory.HumanControl) error {
	if e := agentplacememory.ValidateHumanControl(old, old.ObservedAt); e != nil {
		return e
	}
	current, e := s.ReadOwnPlaceDeclarationControls(ctx, a, old.PlaceID)
	if e != nil {
		return e
	}
	if e = agentplacememory.ValidateHumanControl(old, current.ObservedAt); e != nil {
		return e
	}
	if old.SnapshotID != current.SnapshotID {
		return agentplacememory.ErrForbidden
	}
	return nil
}
