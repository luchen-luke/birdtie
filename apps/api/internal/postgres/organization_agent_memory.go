package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/jackc/pgx/v5"
	"math"
	"time"
)

type orgMemoryBinding struct{ orgID, principalID, actorID, sessionID, agentID, membershipID string }

func (s *Store) ValidateOrganizationMemoryAccess(ctx context.Context, a agentorganizationmemory.Access) (time.Time, error) {
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return time.Time{}, e
	}
	defer tx.Rollback(context.Background())
	now, e := s.orgMemoryBoundary(ctx, tx, b, nil)
	if e != nil {
		return time.Time{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return time.Time{}, agentmemory.ErrUnavailable
	}
	return now, nil
}
func orgMemoryError(err error) error {
	for _, known := range []error{agentorganizationmemory.ErrLimit, agentmemory.ErrForbidden, agentmemory.ErrNotFound, agentmemory.ErrInvalid, agentmemory.ErrConflict, agentmemory.ErrUnavailable} {
		if errors.Is(err, known) {
			return known
		}
	}
	return memoryError(err)
}
func (s *Store) orgMemoryTx(ctx context.Context, a agentorganizationmemory.Access) (pgx.Tx, orgMemoryBinding, error) {
	var b orgMemoryBinding
	if ctx == nil || s == nil || s.pool == nil || agentorganizationmemory.ValidateAccess(a) != nil {
		return nil, b, agentmemory.ErrForbidden
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, b, agentmemory.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, orgMemoryBinding, error) {
		tx.Rollback(context.Background())
		if errors.Is(e, pgx.ErrNoRows) {
			e = agentmemory.ErrForbidden
		}
		return nil, b, orgMemoryError(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(e)
	}
	// Matches real membership mutation's Organization-first lock order. Accounts
	// precede Session, avoiding a reverse edge with native Human Profile updates.
	var orgStatus string
	e = tx.QueryRow(ctx, `SELECT id,account_id,status FROM organizations WHERE id=$1 FOR UPDATE`, a.OrganizationID).Scan(&b.orgID, &b.principalID, &orgStatus)
	if e != nil {
		return fail(e)
	}
	if orgStatus != "active" {
		return fail(agentmemory.ErrForbidden)
	}
	rows, e := tx.Query(ctx, `SELECT id,account_type,status FROM accounts WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, []string{a.ActingPersonID, b.principalID})
	if e != nil {
		return fail(e)
	}
	person, principal := false, false
	for rows.Next() {
		var id, kind, status string
		if e = rows.Scan(&id, &kind, &status); e != nil {
			rows.Close()
			return fail(e)
		}
		if id == a.ActingPersonID && kind == "person" && status == "active" {
			person = true
		}
		if id == b.principalID && kind == "organization" && status == "active" {
			principal = true
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return fail(e)
	}
	if !person || !principal {
		return fail(agentmemory.ErrForbidden)
	}
	b.actorID = a.ActingPersonID
	var role, status string
	e = tx.QueryRow(ctx, `SELECT id,role,status FROM organization_memberships WHERE organization_id=$1 AND user_account_id=$2 FOR SHARE`, b.orgID, b.actorID).Scan(&b.membershipID, &role, &status)
	if e != nil {
		return fail(e)
	}
	if status != "active" || (role != "owner" && role != "admin") {
		return fail(agentmemory.ErrForbidden)
	}
	e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2 FOR SHARE`, a.SessionDigest[:], b.actorID).Scan(&b.sessionID)
	if e != nil {
		return fail(e)
	}
	e = tx.QueryRow(ctx, `SELECT id FROM agents WHERE principal_account_id=$1 AND agent_type='organization' AND status='active' FOR SHARE`, b.principalID).Scan(&b.agentID)
	if e != nil {
		return fail(e)
	}
	var version int64
	e = tx.QueryRow(ctx, `SELECT profile_version FROM agent_profiles WHERE agent_id=$1 AND owner_id=$2 AND owner_type='ORGANIZATION' FOR SHARE`, b.agentID, b.principalID).Scan(&version)
	if e != nil {
		return fail(e)
	}
	if version < 1 {
		return fail(agentmemory.ErrForbidden)
	}
	if _, e = s.orgMemoryBoundary(ctx, tx, b, nil); e != nil {
		return fail(e)
	}
	return tx, b, nil
}

// All current bindings remain locked, but time can advance at any later wait,
// including Memory/audit triggers. A fresh PostgreSQL clock owns the boundary.
func (s *Store) orgMemoryBoundary(ctx context.Context, tx pgx.Tx, b orgMemoryBinding, deadline *time.Time) (time.Time, error) {
	var now time.Time
	var current, validDeadline bool
	e := tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at)
 SELECT stamp.at,EXISTS(SELECT 1 FROM organizations o JOIN accounts principal ON principal.id=o.account_id
 JOIN organization_memberships m ON m.organization_id=o.id JOIN accounts actor ON actor.id=m.user_account_id
 JOIN sessions se ON se.account_id=actor.id JOIN agents ag ON ag.principal_account_id=principal.id
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=principal.id AND ap.owner_type='ORGANIZATION'
 WHERE o.id=$1 AND o.account_id=$2 AND o.status='active' AND principal.account_type='organization' AND principal.status='active'
 AND actor.id=$3 AND actor.account_type='person' AND actor.status='active' AND m.id=$4 AND m.status='active' AND m.role IN ('owner','admin')
 AND se.id=$5 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at
 AND ($7::boolean OR se.authentication_method<>'dev_phone') AND ag.id=$6 AND ag.agent_type='organization' AND ag.status='active' AND ap.profile_version>0),
 ($8::timestamptz IS NULL OR $8>stamp.at) FROM stamp`, b.orgID, b.principalID, b.actorID, b.membershipID, b.sessionID, b.agentID, s.devPhoneEnabled, deadline).Scan(&now, &current, &validDeadline)
	if e != nil {
		return time.Time{}, agentmemory.ErrUnavailable
	}
	if !current {
		return time.Time{}, agentmemory.ErrForbidden
	}
	if !validDeadline {
		return time.Time{}, agentmemory.ErrInvalid
	}
	if ctx.Err() != nil {
		return time.Time{}, agentmemory.ErrUnavailable
	}
	return now, nil
}
func scanOrgMemory(row pgx.Row) (agentmemory.Record, error) {
	var r agentmemory.Record
	r.SchemaVersion = agentmemory.SchemaV1
	e := row.Scan(&r.ID, &r.AgentID, &r.OwnerType, &r.OwnerID, &r.Version, &r.MemoryType, &r.MemoryKey, &r.Summary, &r.StructuredValue, &r.Confidence, &r.SourceType, &r.Visibility, &r.Status, &r.ValidFrom, &r.ValidUntil, &r.LastReinforcedAt, &r.CreatedAt, &r.UpdatedAt)
	if e != nil {
		return agentmemory.Record{}, e
	}
	r.StructuredValue, e = agentmemory.NormalizeOrganizationStructuredValue(r.StructuredValue)
	if e != nil || agentmemory.ValidateOrganizationRecord(r) != nil {
		return agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	return r, nil
}
func (s *Store) ListOrganizationMemories(ctx context.Context, a agentorganizationmemory.Access) ([]agentorganizationmemory.View, error) {
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	rows, e := tx.Query(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE agent_id=$1 AND owner_id=$2 AND owner_type='ORGANIZATION' AND status<>'DELETED' ORDER BY updated_at DESC,id LIMIT 129 FOR SHARE`, b.agentID, b.principalID)
	if e != nil {
		return nil, agentmemory.ErrUnavailable
	}
	records := []agentmemory.Record{}
	for rows.Next() {
		r, e := scanOrgMemory(rows)
		if e != nil {
			rows.Close()
			return nil, agentmemory.ErrUnavailable
		}
		records = append(records, r)
	}
	rows.Close()
	if rows.Err() != nil || len(records) > 128 {
		return nil, agentmemory.ErrUnavailable
	}
	now, e := s.orgMemoryBoundary(ctx, tx, b, nil)
	if e != nil {
		return nil, e
	}
	out := []agentorganizationmemory.View{}
	for _, r := range records {
		if !r.ValidUntil.After(now) {
			r.Status = agentmemory.StatusExpired
		}
		v, e := agentorganizationmemory.NewView(b.orgID, r)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, agentmemory.ErrUnavailable
	}
	return out, nil
}
func orgMemoryAudit(ctx context.Context, tx pgx.Tx, b orgMemoryBinding, r agentmemory.Record, action, metric string) error {
	category, _, e := agentmemory.OrganizationCategoryForKey(r.MemoryType, r.MemoryKey)
	if e != nil {
		return e
	}
	var auditID int64
	var occurred time.Time
	e = auditQueryRow(ctx, tx, `INSERT INTO admin_audit_events(actor_account_id,organization_id,resource_type,resource_id,action,details,occurred_at)
 VALUES($1,$2,'organization_memory',$3,$4,jsonb_build_object('version',$5::bigint,'category',$6::text,'origin',$7::text),clock_timestamp()) RETURNING id,occurred_at`, b.actorID, b.orgID, r.ID, action, r.Version, category, agentorganizationmemory.Origin).Scan(&auditID, &occurred)
	if e != nil {
		return e
	}
	return insertEnrichmentObservation(ctx, tx, "ORGANIZATION", b.principalID, b.agentID, metric, nil, &auditID, occurred)
}
func (s *Store) PutOrganizationMemory(ctx context.Context, a agentorganizationmemory.Access, id string, input agentorganizationmemory.PutInput) (agentorganizationmemory.View, error) {
	id, e := agentmemory.NormalizeMemoryID(id)
	if e != nil {
		return agentorganizationmemory.View{}, e
	}
	if input.ExpectedVersion == math.MaxInt64 {
		return agentorganizationmemory.View{}, agentmemory.ErrConflict
	}
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return agentorganizationmemory.View{}, e
	}
	defer tx.Rollback(context.Background())
	current, e := scanOrgMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='ORGANIZATION' FOR UPDATE`, id, b.agentID, b.principalID))
	missing := errors.Is(e, pgx.ErrNoRows)
	if e != nil && !missing {
		return agentorganizationmemory.View{}, orgMemoryError(e)
	}
	now, e := s.orgMemoryBoundary(ctx, tx, b, nil)
	if e != nil {
		return agentorganizationmemory.View{}, e
	}
	native, e := input.Native(now)
	if e != nil {
		return agentorganizationmemory.View{}, e
	}
	if missing && native.ExpectedVersion != 0 {
		return agentorganizationmemory.View{}, agentmemory.ErrConflict
	}
	if missing {
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM agent_memories WHERE agent_id=$1 AND owner_id=$2 AND owner_type='ORGANIZATION' AND status<>'DELETED'`, b.agentID, b.principalID).Scan(&count); e != nil {
			return agentorganizationmemory.View{}, agentmemory.ErrUnavailable
		}
		if count >= agentorganizationmemory.MaxRecords {
			return agentorganizationmemory.View{}, agentorganizationmemory.ErrLimit
		}
	}
	if !missing {
		if current.MemoryType != native.MemoryType || current.MemoryKey != native.MemoryKey || current.Status == agentmemory.StatusDeleted {
			return agentorganizationmemory.View{}, agentmemory.ErrConflict
		}
		var valueMatches bool
		if e = tx.QueryRow(ctx, `SELECT $1::jsonb=$2::jsonb`, current.StructuredValue, native.StructuredValue).Scan(&valueMatches); e != nil {
			return agentorganizationmemory.View{}, agentmemory.ErrUnavailable
		}
		if current.ValidUntil.After(now) && memoryContentMatches(current, native, valueMatches) && (current.Version == native.ExpectedVersion || current.Version == native.ExpectedVersion+1) {
			if _, e = s.orgMemoryBoundary(ctx, tx, b, &current.ValidUntil); e != nil {
				return agentorganizationmemory.View{}, e
			}
			if e = tx.Commit(ctx); e != nil {
				return agentorganizationmemory.View{}, agentmemory.ErrUnavailable
			}
			return agentorganizationmemory.NewView(b.orgID, current)
		}
		if current.Version != native.ExpectedVersion {
			return agentorganizationmemory.View{}, agentmemory.ErrConflict
		}
	}
	createdAt := now
	if !missing {
		createdAt = current.CreatedAt
	}
	record, e := agentmemory.NewOrganizationExplicit(id, b.agentID, actorref.PrincipalRef{Type: actorref.Organization, ID: b.principalID}, native.ExpectedVersion+1, native, now, createdAt)
	if e != nil {
		return agentorganizationmemory.View{}, e
	}
	if missing {
		record, e = scanOrgMemory(tx.QueryRow(ctx, `INSERT INTO agent_memories(id,agent_id,owner_type,owner_id,version,memory_type,memory_key,summary,structured_value,confidence,source_type,visibility,status,valid_from,valid_until,created_at,updated_at)
  VALUES($1,$2,'ORGANIZATION',$3,1,$4,$5,$6,$7::jsonb,1,'EXPLICIT',$8,'ACTIVE',$9,$10,$9,$9) RETURNING `+agentMemoryColumns, id, b.agentID, b.principalID, record.MemoryType, record.MemoryKey, record.Summary, record.StructuredValue, record.Visibility, now, record.ValidUntil))
	} else {
		record, e = scanOrgMemory(tx.QueryRow(ctx, `UPDATE agent_memories SET version=version+1,summary=$4,structured_value=$5::jsonb,visibility=$6,status='ACTIVE',valid_from=$7,valid_until=$8,updated_at=$7
  WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='ORGANIZATION' AND version=$9 RETURNING `+agentMemoryColumns, id, b.agentID, b.principalID, record.Summary, record.StructuredValue, record.Visibility, now, record.ValidUntil, native.ExpectedVersion))
	}
	if e != nil {
		return agentorganizationmemory.View{}, orgMemoryError(e)
	}
	metric := "memory_corrected"
	if missing {
		metric = "memory_created"
	}
	if e = orgMemoryAudit(ctx, tx, b, record, "organization_memory_put", metric); e != nil {
		return agentorganizationmemory.View{}, orgMemoryError(e)
	}
	if _, e = s.orgMemoryBoundary(ctx, tx, b, &record.ValidUntil); e != nil {
		return agentorganizationmemory.View{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return agentorganizationmemory.View{}, orgMemoryError(e)
	}
	return agentorganizationmemory.NewView(b.orgID, record)
}
func (s *Store) DeleteOrganizationMemory(ctx context.Context, a agentorganizationmemory.Access, id string, expected int64) (agentorganizationmemory.View, error) {
	id, e := agentmemory.NormalizeMemoryID(id)
	if e != nil {
		return agentorganizationmemory.View{}, e
	}
	if expected < 1 {
		return agentorganizationmemory.View{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return agentorganizationmemory.View{}, e
	}
	defer tx.Rollback(context.Background())
	current, e := scanOrgMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='ORGANIZATION' FOR UPDATE`, id, b.agentID, b.principalID))
	if errors.Is(e, pgx.ErrNoRows) {
		return agentorganizationmemory.View{}, agentmemory.ErrNotFound
	}
	if e != nil {
		return agentorganizationmemory.View{}, orgMemoryError(e)
	}
	retry := current.Status == agentmemory.StatusDeleted && expected < math.MaxInt64 && current.Version == expected+1
	if !retry {
		if current.Version != expected || current.Status == agentmemory.StatusDeleted || expected == math.MaxInt64 {
			return agentorganizationmemory.View{}, agentmemory.ErrConflict
		}
		current, e = scanOrgMemory(tx.QueryRow(ctx, `UPDATE agent_memories SET version=version+1,status='DELETED',summary='',structured_value='{}'::jsonb,last_reinforced_at=NULL,updated_at=clock_timestamp() WHERE id=$1 AND version=$2 RETURNING `+agentMemoryColumns, id, expected))
		if e != nil {
			return agentorganizationmemory.View{}, orgMemoryError(e)
		}
		if e = orgMemoryAudit(ctx, tx, b, current, "organization_memory_delete", "memory_deleted"); e != nil {
			return agentorganizationmemory.View{}, orgMemoryError(e)
		}
	}
	if _, e = s.orgMemoryBoundary(ctx, tx, b, nil); e != nil {
		return agentorganizationmemory.View{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return agentorganizationmemory.View{}, orgMemoryError(e)
	}
	return agentorganizationmemory.NewView(b.orgID, current)
}
