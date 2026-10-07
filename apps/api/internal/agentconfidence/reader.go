package agentconfidence

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HumanReader is an internal self-management port. No HTTP route or machine
// adapter installs it. Access is server-only; every call resolves current native
// session/account/exact Agent/metadata and Memory in one metadata statement,
// followed by a final current boundary statement before returning. Neither
// statement reads Memory content or grants any machine access.
type HumanReader struct {
	pool            *pgxpool.Pool
	devPhoneEnabled bool
}

func NewHumanReader(pool *pgxpool.Pool, devPhoneEnabled bool) *HumanReader {
	return &HumanReader{pool: pool, devPhoneEnabled: devPhoneEnabled}
}

const currentOwnMemorySQL = `WITH boundary AS MATERIALIZED (SELECT clock_timestamp() AS now), authorized AS (
 SELECT a.id AS owner_id,ag.id AS agent_id,ap.agent_id AS metadata_id,ses.expires_at,ses.idle_expires_at
 FROM boundary b CROSS JOIN sessions ses
 JOIN accounts a ON a.id=ses.account_id AND a.account_type='person' AND a.status='active'
 JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 LEFT JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 WHERE ses.token_sha256=$1 AND a.id=$2 AND ses.revoked_at IS NULL
 AND ses.expires_at>b.now AND ses.idle_expires_at>b.now AND ($4::boolean OR ses.authentication_method<>'dev_phone')
)
SELECT auth.owner_id,auth.agent_id,auth.metadata_id IS NOT NULL,clock_timestamp(),auth.expires_at,auth.idle_expires_at,
 m.id,m.version,m.confidence,m.source_type,m.status,m.valid_from,m.valid_until
FROM authorized auth LEFT JOIN agent_memories m ON m.id=$3 AND m.owner_id=auth.owner_id
 AND m.agent_id=auth.agent_id AND m.owner_type='PERSON' AND auth.metadata_id IS NOT NULL`

func (r *HumanReader) ReadOwnMemoryConfidence(ctx context.Context, access agentprofile.PrivateAccess, memoryID string, expectedVersion int64) (OwnMemoryView, error) {
	if ctx == nil {
		return OwnMemoryView{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return OwnMemoryView{}, err
	}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		return OwnMemoryView{}, ErrForbidden
	}
	principal, err := actorref.ParsePrincipal(string(access.WorkspacePrincipal.Type), access.WorkspacePrincipal.ID)
	if err != nil {
		return OwnMemoryView{}, ErrForbidden
	}
	id, err := agentmemory.NormalizeMemoryID(memoryID)
	if err != nil || expectedVersion <= 0 {
		return OwnMemoryView{}, ErrInvalid
	}
	if r == nil || r.pool == nil {
		return OwnMemoryView{}, ErrUnavailable
	}
	first, err := r.readCurrent(ctx, currentOwnMemorySQL, access, principal.ID, id, expectedVersion)
	if err != nil {
		return OwnMemoryView{}, err
	}
	// The first clock can become old while a request is delayed. This second
	// read re-resolves the real session, exact active Agent, metadata and native
	// Memory version/validity. Its statement is the final permission boundary;
	// this is not an absolute recall promise after a network response is sent.
	final, err := r.readCurrent(ctx, "/* agentconfidence final boundary */ "+currentOwnMemorySQL, access, principal.ID, id, expectedVersion)
	if err != nil {
		return OwnMemoryView{}, err
	}
	if final.OwnerID != first.OwnerID || final.AgentID != first.AgentID {
		return OwnMemoryView{}, ErrForbidden
	}
	if err := ctx.Err(); err != nil {
		return OwnMemoryView{}, err
	}
	return final, nil
}

func (r *HumanReader) readCurrent(ctx context.Context, sql string, access agentprofile.PrivateAccess, principalID, id string, expectedVersion int64) (OwnMemoryView, error) {
	if err := ctx.Err(); err != nil {
		return OwnMemoryView{}, err
	}
	var owner, agent string
	var metadata bool
	var now, sessionUntil, idleUntil time.Time
	var mid, source, status *string
	var version *int64
	var score *float64
	var from, until *time.Time
	err := r.pool.QueryRow(ctx, sql, access.SessionDigest[:], principalID, id, r.devPhoneEnabled).Scan(&owner, &agent, &metadata, &now, &sessionUntil, &idleUntil, &mid, &version, &score, &source, &status, &from, &until)
	if err := ctx.Err(); err != nil {
		return OwnMemoryView{}, err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return OwnMemoryView{}, ErrForbidden
	}
	if err != nil {
		return OwnMemoryView{}, ErrUnavailable
	}
	if !sessionUntil.After(now) || !idleUntil.After(now) {
		return OwnMemoryView{}, ErrForbidden
	}
	if !metadata || mid == nil {
		return OwnMemoryView{}, ErrNotFound
	}
	if version == nil || score == nil || source == nil || status == nil || from == nil || until == nil {
		return OwnMemoryView{}, ErrUnavailable
	}
	if *version != expectedVersion {
		return OwnMemoryView{}, ErrConflict
	}
	if *source == string(agentmemory.SourceInferred) {
		return OwnMemoryView{}, ErrUnavailable
	}
	if *source != string(agentmemory.SourceExplicit) {
		return OwnMemoryView{}, ErrUnavailable
	}
	if *status != string(agentmemory.StatusActive) || from.After(now) || !until.After(now) {
		return OwnMemoryView{}, ErrNotFound
	}
	assessment, err := NormalizeAssessment(Assessment{Semantics: DirectDeclaration, Value: score})
	if err != nil {
		return OwnMemoryView{}, ErrUnavailable
	}
	explanation, _ := Description(assessment)
	view := OwnMemoryView{SchemaVersion: SchemaV1, MemoryID: *mid, MemoryVersion: *version, AgentID: agent, OwnerType: actorref.Person, OwnerID: owner,
		SourceType: agentmemory.SourceExplicit, Assessment: assessment, Explanation: explanation, ValidFrom: from.UTC(), ValidUntil: until.UTC(), ReadAt: now.UTC()}
	if owner != principalID || ValidateOwnMemoryView(view) != nil {
		return OwnMemoryView{}, ErrUnavailable
	}
	return view, nil
}
