package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentdecay"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"time"
)

// No HTTP or cognition adapter installs this human metadata port. The final
// statement re-resolves all native permissions and computes using PG time.
const currentOwnDecaySQL = `WITH boundary AS MATERIALIZED (SELECT clock_timestamp() AS now), authorized AS (
 SELECT a.id AS owner_id,ag.id AS agent_id,ap.agent_id AS metadata_id,ses.expires_at,ses.idle_expires_at
 FROM boundary b CROSS JOIN sessions ses
 JOIN accounts a ON a.id=ses.account_id AND a.account_type='person' AND a.status='active'
 JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 LEFT JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 WHERE ses.token_sha256=$1 AND a.id=$2 AND ses.revoked_at IS NULL AND ses.expires_at>b.now AND ses.idle_expires_at>b.now
 AND ($4::boolean OR ses.authentication_method<>'dev_phone'))
 SELECT auth.owner_id,auth.agent_id,auth.metadata_id IS NOT NULL,clock_timestamp(),auth.expires_at,auth.idle_expires_at,
 m.id,m.version,m.confidence,m.source_type,m.status,m.valid_from,m.valid_until,m.created_at,m.updated_at,m.last_reinforced_at
 FROM authorized auth LEFT JOIN agent_memories m ON m.id=$3 AND m.owner_id=auth.owner_id AND m.agent_id=auth.agent_id AND m.owner_type='PERSON' AND auth.metadata_id IS NOT NULL`

func (s *Store) ReadOwnMemoryDecay(ctx context.Context, access agentprofile.PrivateAccess, memoryID string, expectedVersion int64) (agentdecay.View, error) {
	if ctx == nil {
		return agentdecay.View{}, agentdecay.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return agentdecay.View{}, err
	}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		return agentdecay.View{}, agentdecay.ErrForbidden
	}
	principal, err := actorref.ParsePrincipal(string(access.WorkspacePrincipal.Type), access.WorkspacePrincipal.ID)
	if err != nil || principal.Type != actorref.Person {
		return agentdecay.View{}, agentdecay.ErrForbidden
	}
	id, err := agentmemory.NormalizeMemoryID(memoryID)
	if err != nil || expectedVersion <= 0 {
		return agentdecay.View{}, agentdecay.ErrInvalid
	}
	if s == nil || s.pool == nil {
		return agentdecay.View{}, agentdecay.ErrUnavailable
	}
	first, err := s.readCurrentMemoryDecay(ctx, currentOwnDecaySQL, access, principal.ID, id, expectedVersion)
	if err != nil {
		return agentdecay.View{}, err
	}
	final, err := s.readCurrentMemoryDecay(ctx, "/* agentdecay final boundary */ "+currentOwnDecaySQL, access, principal.ID, id, expectedVersion)
	if err != nil {
		return agentdecay.View{}, err
	}
	if first.OwnerID != final.OwnerID || first.AgentID != final.AgentID {
		return agentdecay.View{}, agentdecay.ErrForbidden
	}
	if err := ctx.Err(); err != nil {
		return agentdecay.View{}, err
	}
	return final, nil
}
func (s *Store) readCurrentMemoryDecay(ctx context.Context, query string, access agentprofile.PrivateAccess, owner, id string, expected int64) (agentdecay.View, error) {
	var m agentdecay.Metadata
	var metadata bool
	var now, sessionUntil, idleUntil time.Time
	var mid, source, status *string
	var version *int64
	var baseline *float64
	var from, until, created, updated, reinforced *time.Time
	err := s.pool.QueryRow(ctx, query, access.SessionDigest[:], owner, id, s.devPhoneEnabled).Scan(&m.OwnerID, &m.AgentID, &metadata, &now, &sessionUntil, &idleUntil, &mid, &version, &baseline, &source, &status, &from, &until, &created, &updated, &reinforced)
	if ctx.Err() != nil {
		return agentdecay.View{}, ctx.Err()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return agentdecay.View{}, agentdecay.ErrForbidden
	}
	if err != nil {
		return agentdecay.View{}, agentdecay.ErrUnavailable
	}
	if !sessionUntil.After(now) || !idleUntil.After(now) {
		return agentdecay.View{}, agentdecay.ErrForbidden
	}
	if !metadata || mid == nil {
		return agentdecay.View{}, agentdecay.ErrNotFound
	}
	if version == nil || baseline == nil || source == nil || status == nil || from == nil || until == nil || created == nil || updated == nil {
		return agentdecay.View{}, agentdecay.ErrUnavailable
	}
	if *version != expected {
		return agentdecay.View{}, agentdecay.ErrConflict
	}
	m.MemoryID = *mid
	m.Version = *version
	m.Baseline = *baseline
	m.SourceType = agentmemory.SourceType(*source)
	m.Status = agentmemory.Status(*status)
	m.ValidFrom = *from
	m.ValidUntil = *until
	m.CreatedAt = *created
	m.UpdatedAt = *updated
	m.LastReinforcedAt = reinforced
	v, err := agentdecay.Project(m, now)
	if errors.Is(err, agentdecay.ErrNotFound) {
		return agentdecay.View{}, err
	}
	if err != nil || m.OwnerID != owner {
		return agentdecay.View{}, agentdecay.ErrUnavailable
	}
	return v, nil
}
