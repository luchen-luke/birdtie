package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationevidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/jackc/pgx/v5"
)

func scanOrganizationEvidence(row pgx.Row) (agentmemory.Evidence, error) {
	var e agentmemory.Evidence
	var kind, id, vkind, token, signal *string
	var revision *int64
	e.SchemaVersion = agentmemory.EvidenceSchemaV1
	err := row.Scan(&e.ID, &e.MemoryID, &e.MemoryVersion, &e.AgentID, &e.OwnerType, &e.OwnerID, &e.Version, &e.Status, &kind, &id, &vkind, &revision, &token, &signal, &e.Weight, &e.ObservedAt, &e.EventTime, &e.CreatedAt)
	if err != nil {
		return e, err
	}
	if kind != nil && id != nil && vkind != nil {
		e.Source = &agentevent.SourceReference{Type: agentevent.SourceType(*kind), ID: *id, Owner: actorref.PrincipalRef{Type: e.OwnerType, ID: e.OwnerID}, Version: agentevent.SourceVersion{Kind: agentevent.VersionKind(*vkind)}}
		if revision != nil {
			e.Source.Version.Revision = *revision
		}
		if token != nil {
			e.Source.Version.Token = *token
		}
	}
	if signal != nil {
		e.SignalType = agentmemory.EvidenceSignalType(*signal)
	}
	if agentorganizationevidence.ValidateEvidence(e) != nil {
		return agentmemory.Evidence{}, agentmemory.ErrUnavailable
	}
	return e, nil
}

// $1 is the Organization entity; $2 source entity; $3 the authenticated Person.
// Public source readability is NOT a cognitive purpose. This remains a human
// association API and does not register any source in AIR/model ingress.
func organizationEvidenceSourceSQL(kind agentevent.SourceType) (string, error) {
	const publicOrg = ` JOIN organizations o ON o.id=$1 AND o.status='active' AND o.visibility='public' AND o.verification_status='verified'
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$3 AND b.blocked_account_id=o.account_id) OR (b.blocker_account_id=o.account_id AND b.blocked_account_id=$3)) `
	switch kind {
	case agentorganizationevidence.Profile:
		return `SELECT 0::bigint revision,o.updated_at event_time,jsonb_build_object('id',o.id,'account_id',o.account_id,'name',o.name,'description',o.description,'official_links',o.official_links,'organization_type',o.organization_type,'visibility',o.visibility,'verification_status',o.verification_status,'status',o.status) canonical
 FROM organizations target ` + publicOrg + ` WHERE target.id=$2 AND target.id=o.id`, nil
	case agentorganizationevidence.PublicContent:
		return `SELECT 0::bigint revision,f.updated_at event_time,jsonb_build_object('id',f.id,'organization_id',f.organization_id,'question',f.question,'answer',f.answer,'published',f.published,'created_at',f.created_at) canonical
 FROM organization_faqs f ` + publicOrg + ` WHERE f.id=$2 AND f.organization_id=o.id AND f.published`, nil
	case agentorganizationevidence.Activity:
		return `SELECT a.revision,a.updated_at event_time,NULL::jsonb canonical FROM activities a ` + publicOrg + `
 JOIN activity_organizers ao ON ao.activity_id=a.id AND ao.organization_id=o.id AND ao.person_account_id IS NULL AND ao.community_id IS NULL AND ao.business_id IS NULL
 JOIN cities c ON c.id=a.city_id AND c.publication_status='published'
 WHERE a.id=$2 AND a.organization_id=o.id AND a.publication_status='published' AND a.visibility='public' AND a.cancelled_at IS NULL
 AND a.starts_at>clock_timestamp() AND a.ends_at>a.starts_at AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(a.id,$3::uuid)
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE a.host_account_id IS NOT NULL AND ((block.blocker_account_id=$3 AND block.blocked_account_id=a.host_account_id) OR (block.blocker_account_id=a.host_account_id AND block.blocked_account_id=$3)))`, nil
	case agentorganizationevidence.AdminInput:
		return `SELECT src.version revision,src.updated_at event_time,NULL::jsonb canonical FROM agent_memories src JOIN organizations o ON o.id=$1
 JOIN agents ag ON ag.id=src.agent_id AND ag.principal_account_id=o.account_id AND ag.agent_type='organization' AND ag.status='active'
 WHERE src.id=$2 AND src.owner_type='ORGANIZATION' AND src.owner_id=o.account_id AND src.source_type='EXPLICIT' AND src.status='ACTIVE'
 AND src.valid_from<=clock_timestamp() AND src.valid_until>clock_timestamp()`, nil
	case agentorganizationevidence.Announcement:
		return `SELECT n.revision,n.updated_at event_time,NULL::jsonb canonical FROM organization_announcements n JOIN organizations o ON o.id=$1 WHERE n.id=$2 AND ` + announcementPublicPredicate, nil
	default:
		return "", agentmemory.ErrInvalid
	}
}
func organizationSourceVersion(kind agentevent.SourceType, revision int64, at time.Time, raw []byte) (agentevent.SourceVersion, error) {
	if kind == agentorganizationevidence.Activity || kind == agentorganizationevidence.AdminInput || kind == agentorganizationevidence.Announcement {
		if revision < 1 {
			return agentevent.SourceVersion{}, agentmemory.ErrUnavailable
		}
		return agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: revision}, nil
	}
	if at.IsZero() || len(raw) == 0 || len(raw) > 1024*1024 {
		return agentevent.SourceVersion{}, agentmemory.ErrUnavailable
	}
	digest := sha256.Sum256([]byte("birdtie.organization.retained-source.v1\x00" + string(kind) + "\x00" + at.UTC().Format(time.RFC3339Nano) + "\x00" + string(raw)))
	return agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: hex.EncodeToString(digest[:])}, nil
}
func (s *Store) beginOrganizationEvidence(ctx context.Context, a agentorganizationmemory.Access, id string) (pgx.Tx, orgMemoryBinding, agentmemory.Record, error) {
	normalized, e := agentmemory.NormalizeMemoryID(id)
	if e != nil || normalized != id {
		return nil, orgMemoryBinding{}, agentmemory.Record{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return nil, b, agentmemory.Record{}, e
	}
	fail := func(e error) (pgx.Tx, orgMemoryBinding, agentmemory.Record, error) {
		tx.Rollback(context.Background())
		return nil, b, agentmemory.Record{}, e
	}
	m, e := scanOrgMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='ORGANIZATION' FOR UPDATE`, id, b.agentID, b.principalID))
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(agentmemory.ErrNotFound)
	}
	if e != nil {
		return fail(orgMemoryError(e))
	}
	if m.SourceType != agentmemory.SourceExplicit {
		return fail(agentmemory.ErrUnavailable)
	}
	return tx, b, m, nil
}
func organizationEvidenceSourceBoundary(query string) string {
	return `SELECT src.revision,src.event_time,src.canonical,clock_timestamp() FROM (` + query + `) src
 JOIN organizations own ON own.id=$1 JOIN agent_memories m ON m.id=$4 AND m.agent_id=$5 AND m.owner_id=own.account_id AND m.owner_type='ORGANIZATION'
 JOIN sessions se ON se.id=$6 AND se.account_id=$3
 WHERE m.version=$7 AND m.source_type='EXPLICIT' AND m.status='ACTIVE' AND m.valid_from<=clock_timestamp() AND m.valid_until>clock_timestamp()
 AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp()`
}
func resolveOrganizationEvidence(ctx context.Context, tx pgx.Tx, b orgMemoryBinding, m agentmemory.Record, kind agentevent.SourceType, id string) (agentevent.SourceReference, time.Time, time.Time, error) {
	if id == m.ID && kind == agentorganizationevidence.AdminInput {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, agentmemory.ErrInvalid
	}
	q, e := organizationEvidenceSourceSQL(kind)
	if e != nil {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, e
	}
	var revision int64
	var at, observed time.Time
	var raw []byte
	e = tx.QueryRow(ctx, organizationEvidenceSourceBoundary(q), b.orgID, id, b.actorID, m.ID, b.agentID, b.sessionID, m.Version).Scan(&revision, &at, &raw, &observed)
	if errors.Is(e, pgx.ErrNoRows) {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, agentmemory.ErrForbidden
	}
	if e != nil {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, agentmemory.ErrUnavailable
	}
	v, e := organizationSourceVersion(kind, revision, at, raw)
	if e != nil || at.After(observed) {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, agentmemory.ErrUnavailable
	}
	return agentevent.SourceReference{Type: kind, ID: id, Owner: actorref.PrincipalRef{Type: actorref.Organization, ID: b.principalID}, Version: v}, at, observed, nil
}
func (s *Store) PutOrganizationMemoryEvidence(ctx context.Context, a agentorganizationmemory.Access, mid, eid string, in agentmemory.EvidenceReferenceInput) (agentmemory.Evidence, error) {
	normalized, e := agentmemory.NormalizeEvidenceID(eid)
	if e != nil || normalized != eid {
		return agentmemory.Evidence{}, agentmemory.ErrInvalid
	}
	in, e = agentorganizationevidence.NormalizeReference(in)
	if e != nil {
		return agentmemory.Evidence{}, e
	}
	tx, b, m, e := s.beginOrganizationEvidence(ctx, a, mid)
	if e != nil {
		return agentmemory.Evidence{}, e
	}
	defer tx.Rollback(context.Background())
	if m.Version != in.ExpectedMemoryVersion || m.Status != agentmemory.StatusActive {
		return agentmemory.Evidence{}, agentmemory.ErrConflict
	}
	source, at, observed, e := resolveOrganizationEvidence(ctx, tx, b, m, in.SourceType, in.SourceID)
	if e != nil {
		return agentmemory.Evidence{}, e
	}
	current, e := scanOrganizationEvidence(tx.QueryRow(ctx, `SELECT `+memoryEvidenceColumns+` FROM agent_memory_evidence WHERE id=$1 AND memory_id=$2 AND agent_id=$3 AND owner_id=$4 AND owner_type='ORGANIZATION' FOR UPDATE`, eid, mid, b.agentID, b.principalID))
	missing := errors.Is(e, pgx.ErrNoRows)
	if e != nil && !missing {
		return agentmemory.Evidence{}, orgMemoryError(e)
	}
	if !missing {
		if current.Status != agentmemory.EvidenceCurrent || current.MemoryVersion != m.Version || current.Source == nil || *current.Source != source || current.EventTime == nil || !current.EventTime.Equal(at) {
			return agentmemory.Evidence{}, agentmemory.ErrConflict
		}
	} else {
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1 AND status='CURRENT'`, mid).Scan(&count); e != nil {
			return agentmemory.Evidence{}, agentmemory.ErrUnavailable
		}
		if count >= agentmemory.MaxProvenanceEvidence {
			return agentmemory.Evidence{}, agentmemory.ErrConflict
		}
		var rev, token any
		if source.Version.Revision > 0 {
			rev = source.Version.Revision
		}
		if source.Version.Token != "" {
			token = source.Version.Token
		}
		current, e = scanOrganizationEvidence(tx.QueryRow(ctx, `INSERT INTO agent_memory_evidence(id,memory_id,memory_version,agent_id,owner_type,owner_id,source_type,source_id,source_version_kind,source_revision,source_token,signal_type,weight,observed_at,event_time,created_at,updated_at)
 VALUES($1,$2,$3,$4,'ORGANIZATION',$5,$6,$7,$8,$9,$10,'MANUAL_REFERENCE',1,$11,$12,$11,$11) RETURNING `+memoryEvidenceColumns, eid, mid, m.Version, b.agentID, b.principalID, source.Type, source.ID, source.Version.Kind, rev, token, observed, at))
		if e != nil {
			return agentmemory.Evidence{}, orgMemoryError(e)
		}
	}
	if _, e = s.orgMemoryBoundary(ctx, tx, b, &m.ValidUntil); e != nil {
		return agentmemory.Evidence{}, e
	}
	final, finalAt, _, e := resolveOrganizationEvidence(ctx, tx, b, m, in.SourceType, in.SourceID)
	if e != nil {
		return agentmemory.Evidence{}, e
	}
	if final != source || !finalAt.Equal(at) {
		return agentmemory.Evidence{}, agentmemory.ErrConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return agentmemory.Evidence{}, orgMemoryError(e)
	}
	return current, nil
}
func (s *Store) RemoveOrganizationMemoryEvidence(ctx context.Context, a agentorganizationmemory.Access, mid, eid string, expected int64) (agentmemory.Evidence, error) {
	normalized, e := agentmemory.NormalizeEvidenceID(eid)
	if e != nil || normalized != eid || expected < 1 || expected == math.MaxInt64 {
		return agentmemory.Evidence{}, agentmemory.ErrInvalid
	}
	tx, b, _, e := s.beginOrganizationEvidence(ctx, a, mid)
	if e != nil {
		return agentmemory.Evidence{}, e
	}
	defer tx.Rollback(context.Background())
	current, e := scanOrganizationEvidence(tx.QueryRow(ctx, `SELECT `+memoryEvidenceColumns+` FROM agent_memory_evidence WHERE id=$1 AND memory_id=$2 AND agent_id=$3 AND owner_id=$4 AND owner_type='ORGANIZATION' FOR UPDATE`, eid, mid, b.agentID, b.principalID))
	if errors.Is(e, pgx.ErrNoRows) {
		return agentmemory.Evidence{}, agentmemory.ErrNotFound
	}
	if e != nil {
		return agentmemory.Evidence{}, orgMemoryError(e)
	}
	if current.Status == agentmemory.EvidenceRemoved {
		if current.Version != expected && current.Version != expected+1 {
			return agentmemory.Evidence{}, agentmemory.ErrConflict
		}
	} else {
		if current.Version != expected {
			return agentmemory.Evidence{}, agentmemory.ErrConflict
		}
		current, e = scanOrganizationEvidence(tx.QueryRow(ctx, `UPDATE agent_memory_evidence SET version=version+1,status='REMOVED',source_type=NULL,source_id=NULL,source_version_kind=NULL,source_revision=NULL,source_token=NULL,signal_type=NULL,weight=0,event_time=NULL WHERE id=$1 AND version=$2 RETURNING `+memoryEvidenceColumns, eid, expected))
		if e != nil {
			return agentmemory.Evidence{}, orgMemoryError(e)
		}
	}
	if _, e = s.orgMemoryBoundary(ctx, tx, b, nil); e != nil {
		return agentmemory.Evidence{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return agentmemory.Evidence{}, orgMemoryError(e)
	}
	return current, nil
}

// All sources, target Memory and session are resolved in ONE final SQL snapshot.
// No FAQ/Activity row locks are acquired after Organization FOR UPDATE: native
// source writers audit through an Organization FK in the opposite order.
func readCurrentOrganizationEvidence(ctx context.Context, tx pgx.Tx, b orgMemoryBinding, m agentmemory.Record) ([]agentmemory.Evidence, error) {
	parts := []string{}
	for _, kind := range []agentevent.SourceType{agentorganizationevidence.Profile, agentorganizationevidence.Activity, agentorganizationevidence.AdminInput, agentorganizationevidence.PublicContent, agentorganizationevidence.Announcement} {
		q, _ := organizationEvidenceSourceSQL(kind)
		q = strings.ReplaceAll(q, "$2", "e.source_id")
		parts = append(parts, `SELECT * FROM (`+q+`) z WHERE e.source_type='`+string(kind)+`'`)
	}
	query := `SELECT ` + prefixMemoryEvidenceColumns("e") + `,src.revision,src.event_time,src.canonical,clock_timestamp()
 FROM agent_memory_evidence e JOIN agent_memories m ON m.id=e.memory_id JOIN sessions se ON se.id=$6 AND se.account_id=$3
 CROSS JOIN LATERAL (` + strings.Join(parts, " UNION ALL ") + `) src
 WHERE $2::uuid IS NULL AND e.memory_id=$4 AND e.agent_id=$5 AND e.owner_id=$7 AND e.owner_type='ORGANIZATION' AND e.status='CURRENT' AND e.memory_version=$8
 AND m.version=$8 AND m.agent_id=$5 AND m.owner_id=$7 AND m.owner_type='ORGANIZATION' AND m.source_type='EXPLICIT' AND m.status='ACTIVE'
 AND m.valid_from<=clock_timestamp() AND m.valid_until>clock_timestamp() AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp()
 ORDER BY e.observed_at,e.id LIMIT 101`
	rows, e := tx.Query(ctx, query, b.orgID, nil, b.actorID, m.ID, b.agentID, b.sessionID, b.principalID, m.Version)
	if e != nil {
		return nil, agentmemory.ErrUnavailable
	}
	defer rows.Close()
	out := []agentmemory.Evidence{}
	for rows.Next() {
		var revision int64
		var at, now time.Time
		var raw []byte
		rec, e := scanOrganizationEvidence(extraEvidenceRow{row: rows, targets: []any{&revision, &at, &raw, &now}})
		if e != nil {
			return nil, agentmemory.ErrUnavailable
		}
		v, e := organizationSourceVersion(rec.Source.Type, revision, at, raw)
		if e != nil {
			return nil, e
		}
		if v == rec.Source.Version && rec.EventTime != nil && at.Equal(*rec.EventTime) && !rec.ObservedAt.After(now) && !at.After(now) {
			out = append(out, rec)
		}
	}
	if rows.Err() != nil || len(out) > 100 {
		return nil, agentmemory.ErrUnavailable
	}
	return out, nil
}
func (s *Store) ReadOrganizationMemoryProvenance(ctx context.Context, a agentorganizationmemory.Access, mid string) (agentorganizationevidence.Provenance, error) {
	tx, b, m, e := s.beginOrganizationEvidence(ctx, a, mid)
	if e != nil {
		return agentorganizationevidence.Provenance{}, e
	}
	defer tx.Rollback(context.Background())
	now, e := s.orgMemoryBoundary(ctx, tx, b, nil)
	if e != nil {
		return agentorganizationevidence.Provenance{}, e
	}
	if m.Status != agentmemory.StatusActive || m.ValidFrom.After(now) || !m.ValidUntil.After(now) {
		return agentorganizationevidence.Provenance{}, agentmemory.ErrNotFound
	}
	current, e := readCurrentOrganizationEvidence(ctx, tx, b, m)
	if e != nil {
		return agentorganizationevidence.Provenance{}, e
	}
	ids := []string{}
	for _, rec := range current {
		ids = append(ids, rec.ID)
	}
	if _, e = tx.Exec(ctx, `UPDATE agent_memory_evidence SET status='REMOVED',version=version+1,source_type=NULL,source_id=NULL,source_version_kind=NULL,source_revision=NULL,source_token=NULL,signal_type=NULL,weight=0,event_time=NULL WHERE memory_id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='ORGANIZATION' AND status='CURRENT' AND NOT(id=ANY($4::uuid[]))`, m.ID, b.agentID, b.principalID, ids); e != nil {
		return agentorganizationevidence.Provenance{}, orgMemoryError(e)
	}
	now, e = s.orgMemoryBoundary(ctx, tx, b, &m.ValidUntil)
	if e != nil {
		return agentorganizationevidence.Provenance{}, e
	}
	current, e = readCurrentOrganizationEvidence(ctx, tx, b, m)
	if e != nil {
		return agentorganizationevidence.Provenance{}, e
	}
	p, e := agentorganizationevidence.Build(b.orgID, m, current, now)
	if e != nil {
		return agentorganizationevidence.Provenance{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return agentorganizationevidence.Provenance{}, orgMemoryError(e)
	}
	return p, nil
}
