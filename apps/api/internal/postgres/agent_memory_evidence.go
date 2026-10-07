package postgres

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

// No raw source body or title is copied into Evidence. Current native rows are
// checked again at the final SQL snapshot. A manual reference is not permission
// for source analysis, inference, model egress or another principal's data.
const memoryEvidenceColumns = `id,memory_id,memory_version,agent_id,owner_type,owner_id,version,status,
source_type,source_id,source_version_kind,source_revision,source_token,signal_type,weight,observed_at,event_time,created_at`

func scanMemoryEvidence(row pgx.Row) (agentmemory.Evidence, error) {
	var e agentmemory.Evidence
	var kind, id, versionKind, token, signal *string
	var revision *int64
	e.SchemaVersion = "agent-memory-evidence-v1"
	err := row.Scan(&e.ID, &e.MemoryID, &e.MemoryVersion, &e.AgentID, &e.OwnerType, &e.OwnerID, &e.Version, &e.Status,
		&kind, &id, &versionKind, &revision, &token, &signal, &e.Weight, &e.ObservedAt, &e.EventTime, &e.CreatedAt)
	if err != nil {
		return agentmemory.Evidence{}, err
	}
	if kind != nil && id != nil && versionKind != nil {
		e.Source = &agentevent.SourceReference{Type: agentevent.SourceType(*kind), ID: *id,
			Owner: actorref.PrincipalRef{Type: e.OwnerType, ID: e.OwnerID}, Version: agentevent.SourceVersion{Kind: agentevent.VersionKind(*versionKind)}}
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
	if agentmemory.ValidateEvidence(e) != nil {
		return agentmemory.Evidence{}, agentmemory.ErrUnavailable
	}
	return e, nil
}

func memoryEvidenceSourceSQL(kind agentevent.SourceType) (string, error) {
	switch kind {
	case agentevent.MomentSource:
		return `SELECT m.revision,m.updated_at AS event_time,NULL::jsonb AS canonical FROM moments m
		 WHERE m.author_account_id=$1 AND m.id=$2 AND m.status='draft' AND m.visibility='private'
		 AND length(btrim(m.title||' '||m.body))>0`, nil
	case agentevent.ParticipationSource:
		// Participation's updated_at is retained state, not attendance or the
		// historic action's author. Public target access and blocks stay current.
		return `SELECT 0::bigint AS revision,p.updated_at AS event_time,to_jsonb(p) AS canonical
		 FROM activity_participations p JOIN activities a ON a.id=p.activity_id JOIN cities c ON c.id=a.city_id
		 WHERE p.participant_account_id=$1 AND p.id=$2 AND p.status='going' AND p.cancelled_at IS NULL
		 AND a.publication_status='published' AND c.publication_status='published' AND a.cancelled_at IS NULL
		 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(a.id,$1::uuid)
		 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE a.host_account_id IS NOT NULL AND
		 ((b.blocker_account_id=$1 AND b.blocked_account_id=a.host_account_id) OR
		 (b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$1)))`, nil
	case agentevent.SavedPlaceSource:
		return `SELECT 0::bigint AS revision,s.created_at AS event_time,to_jsonb(s) AS canonical FROM saved_items s
		 JOIN places p ON p.id=s.place_id JOIN cities c ON c.id=p.city_id
		 WHERE s.owner_account_id=$1 AND s.id=$2 AND s.activity_id IS NULL AND s.community_id IS NULL
		 AND p.publication_status='published' AND c.publication_status='published'
		 AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())`, nil
	default:
		return "", agentmemory.ErrInvalid
	}
}

func lockMemoryEvidenceSource(ctx context.Context, tx pgx.Tx, owner string, kind agentevent.SourceType, id string) error {
	// Lock the native row only. Do not acquire Activity after Participation:
	// native Join locks Activity first, so that inverse order can deadlock.
	var sql string
	switch kind {
	case agentevent.MomentSource:
		sql = `SELECT id FROM moments WHERE author_account_id=$1 AND id=$2 FOR SHARE`
	case agentevent.ParticipationSource:
		sql = `SELECT id FROM activity_participations WHERE participant_account_id=$1 AND id=$2 FOR SHARE`
	case agentevent.SavedPlaceSource:
		sql = `SELECT id FROM saved_items WHERE owner_account_id=$1 AND id=$2 FOR SHARE`
	default:
		return agentmemory.ErrInvalid
	}
	var current string
	err := tx.QueryRow(ctx, sql, owner, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentmemory.ErrForbidden
	}
	if err != nil {
		return memoryError(err)
	}
	return nil
}

// resolveMemoryEvidenceSource uses one final SQL snapshot for source status /
// target ACL, exact Memory version, native Agent and wall-clock session/expiry.
func resolveMemoryEvidenceSource(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, memoryID string, version int64,
	kind agentevent.SourceType, sourceID string) (agentevent.SourceReference, time.Time, time.Time, error) {
	query, err := memoryEvidenceSourceSQL(kind)
	if err != nil {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, err
	}
	var revision int64
	var eventTime, observed time.Time
	var canonical []byte
	err = tx.QueryRow(ctx, `SELECT src.revision,src.event_time,src.canonical,clock_timestamp() FROM (`+query+`) src
	 JOIN agent_memories m ON m.id=$3 AND m.agent_id=$4 AND m.owner_id=$1 AND m.owner_type='PERSON'
	 JOIN sessions ses ON ses.id=$5 AND ses.account_id=$1
	 WHERE m.version=$6 AND m.source_type='EXPLICIT' AND m.status='ACTIVE' AND m.valid_from<=clock_timestamp() AND m.valid_until>clock_timestamp()
	 AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp()`,
		b.accountID, sourceID, memoryID, b.agentID, b.sessionID, version).Scan(&revision, &eventTime, &canonical, &observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, agentmemory.ErrForbidden
	}
	if err != nil {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, agentmemory.ErrUnavailable
	}
	ref := agentevent.SourceReference{Type: kind, ID: sourceID, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.accountID}}
	if kind == agentevent.MomentSource {
		ref.Version = agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: revision}
	} else {
		versionKind := agentevent.UpdatedAtDigestVersion
		if kind == agentevent.SavedPlaceSource {
			versionKind = agentevent.CreatedAtDigestVersion
		}
		ref.Version, err = agentevent.SnapshotVersion(kind, versionKind, eventTime, canonical)
		if err != nil {
			return agentevent.SourceReference{}, time.Time{}, time.Time{}, agentmemory.ErrUnavailable
		}
	}
	if eventTime.After(observed) {
		return agentevent.SourceReference{}, time.Time{}, time.Time{}, agentmemory.ErrUnavailable
	}
	return ref, eventTime, observed, nil
}

func (s *Store) beginMemoryEvidence(ctx context.Context, access agentprofile.PrivateAccess, id string, write bool) (pgx.Tx, agentPrivateBinding, agentmemory.Record, error) {
	if s == nil || s.pool == nil {
		return nil, agentPrivateBinding{}, agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		return nil, agentPrivateBinding{}, agentmemory.Record{}, agentmemory.ErrForbidden
	}
	if _, err := agentmemory.NormalizeMemoryID(id); err != nil {
		return nil, agentPrivateBinding{}, agentmemory.Record{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, agentPrivateBinding{}, agentmemory.Record{}, agentmemory.ErrUnavailable
	}
	fail := func(err error) (pgx.Tx, agentPrivateBinding, agentmemory.Record, error) {
		_ = tx.Rollback(ctx)
		return nil, agentPrivateBinding{}, agentmemory.Record{}, err
	}
	// Native timestamp JSON is canonical in UTC on every transaction, even
	// when a pool connection was configured with another presentation timezone.
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return fail(agentmemory.ErrUnavailable)
	}
	b, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return fail(memoryError(err))
	}
	if _, err = lockAgentPrivateMetadata(ctx, tx, b, false); err != nil {
		return fail(memoryError(err))
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	m, err := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories
	 WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON'`+lock, id, b.agentID, b.accountID))
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(agentmemory.ErrNotFound)
	}
	if err != nil {
		return fail(agentmemory.ErrUnavailable)
	}
	return tx, b, m, nil
}

func (s *Store) PutOwnMemoryEvidence(ctx context.Context, access agentprofile.PrivateAccess, memoryID, evidenceID string, input agentmemory.EvidenceReferenceInput) (agentmemory.Evidence, error) {
	id, err := agentmemory.NormalizeEvidenceID(evidenceID)
	if err != nil {
		return agentmemory.Evidence{}, err
	}
	input, err = agentmemory.NormalizeReference(input)
	if err != nil {
		return agentmemory.Evidence{}, err
	}
	tx, b, m, err := s.beginMemoryEvidence(ctx, access, memoryID, true)
	if err != nil {
		return agentmemory.Evidence{}, err
	}
	defer tx.Rollback(ctx)
	if m.SourceType != agentmemory.SourceExplicit {
		return agentmemory.Evidence{}, agentmemory.ErrUnavailable
	}
	if m.Version != input.ExpectedMemoryVersion || m.Status != agentmemory.StatusActive {
		return agentmemory.Evidence{}, agentmemory.ErrConflict
	}
	if err = lockMemoryEvidenceSource(ctx, tx, b.accountID, input.SourceType, input.SourceID); err != nil {
		return agentmemory.Evidence{}, err
	}
	source, eventTime, observed, err := resolveMemoryEvidenceSource(ctx, tx, b, memoryID, m.Version, input.SourceType, input.SourceID)
	if err != nil {
		return agentmemory.Evidence{}, err
	}
	current, err := scanMemoryEvidence(tx.QueryRow(ctx, `SELECT `+memoryEvidenceColumns+` FROM agent_memory_evidence
	 WHERE id=$1 AND memory_id=$2 AND agent_id=$3 AND owner_id=$4 FOR UPDATE`, id, memoryID, b.agentID, b.accountID))
	missing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !missing {
		return agentmemory.Evidence{}, agentmemory.ErrUnavailable
	}
	if !missing {
		if current.Status != "CURRENT" || current.MemoryVersion != m.Version || current.Source == nil || !reflect.DeepEqual(*current.Source, source) {
			return agentmemory.Evidence{}, agentmemory.ErrConflict
		}
	} else {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM agent_memory_evidence WHERE memory_id=$1 AND status='CURRENT'`, memoryID).Scan(&count); err != nil {
			return agentmemory.Evidence{}, agentmemory.ErrUnavailable
		}
		if count >= 100 {
			return agentmemory.Evidence{}, agentmemory.ErrConflict
		}
		var rev any
		var token any
		if source.Version.Revision > 0 {
			rev = source.Version.Revision
		}
		if source.Version.Token != "" {
			token = source.Version.Token
		}
		current, err = scanMemoryEvidence(tx.QueryRow(ctx, `INSERT INTO agent_memory_evidence
		 (id,memory_id,memory_version,agent_id,owner_id,source_type,source_id,source_version_kind,source_revision,
		 source_token,signal_type,weight,observed_at,event_time,created_at,updated_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'MANUAL_REFERENCE',1,$11,$12,$11,$11) RETURNING `+memoryEvidenceColumns,
			id, memoryID, m.Version, b.agentID, b.accountID, source.Type, source.ID, source.Version.Kind, rev, token, observed, eventTime))
		if err != nil {
			return agentmemory.Evidence{}, memoryError(err)
		}
	}
	// A lock or trigger can consume time. Final re-resolution must still refer
	// to the identical source snapshot and a currently valid Memory/session.
	final, _, _, err := resolveMemoryEvidenceSource(ctx, tx, b, memoryID, m.Version, input.SourceType, input.SourceID)
	if err != nil {
		return agentmemory.Evidence{}, err
	}
	if !reflect.DeepEqual(final, source) {
		return agentmemory.Evidence{}, agentmemory.ErrConflict
	}
	if err = tx.Commit(ctx); err != nil {
		return agentmemory.Evidence{}, memoryError(err)
	}
	return current, nil
}

func (s *Store) RemoveOwnMemoryEvidence(ctx context.Context, access agentprofile.PrivateAccess, memoryID, evidenceID string, expected int64) (agentmemory.Evidence, error) {
	id, err := agentmemory.NormalizeEvidenceID(evidenceID)
	if err != nil {
		return agentmemory.Evidence{}, err
	}
	if expected <= 0 || expected == math.MaxInt64 {
		return agentmemory.Evidence{}, agentmemory.ErrInvalid
	}
	tx, b, _, err := s.beginMemoryEvidence(ctx, access, memoryID, true)
	if err != nil {
		return agentmemory.Evidence{}, err
	}
	defer tx.Rollback(ctx)
	e, err := scanMemoryEvidence(tx.QueryRow(ctx, `SELECT `+memoryEvidenceColumns+` FROM agent_memory_evidence WHERE
	 id=$1 AND memory_id=$2 AND agent_id=$3 AND owner_id=$4 FOR UPDATE`, id, memoryID, b.agentID, b.accountID))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentmemory.Evidence{}, agentmemory.ErrNotFound
	}
	if err != nil {
		return agentmemory.Evidence{}, agentmemory.ErrUnavailable
	}
	if e.Status == "REMOVED" {
		if e.Version != expected && e.Version != expected+1 {
			return agentmemory.Evidence{}, agentmemory.ErrConflict
		}
	} else {
		if e.Version != expected {
			return agentmemory.Evidence{}, agentmemory.ErrConflict
		}
		e, err = scanMemoryEvidence(tx.QueryRow(ctx, `UPDATE agent_memory_evidence SET version=version+1,status='REMOVED',
		 source_type=NULL,source_id=NULL,source_version_kind=NULL,source_revision=NULL,source_token=NULL,signal_type=NULL,weight=0,event_time=NULL
		 WHERE id=$1 AND memory_id=$2 AND owner_id=$3 AND version=$4 RETURNING `+memoryEvidenceColumns, id, memoryID, b.accountID, expected))
		if err != nil {
			return agentmemory.Evidence{}, memoryError(err)
		}
	}
	if err = recheckAgentPrivateSession(ctx, tx, b); err != nil {
		return agentmemory.Evidence{}, memoryError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return agentmemory.Evidence{}, memoryError(err)
	}
	return e, nil
}

func (s *Store) ReadOwnMemoryProvenance(ctx context.Context, access agentprofile.PrivateAccess, memoryID string) (agentmemory.Provenance, error) {
	tx, b, m, err := s.beginMemoryEvidence(ctx, access, memoryID, true)
	if err != nil {
		return agentmemory.Provenance{}, err
	}
	defer tx.Rollback(ctx)
	if m.SourceType != agentmemory.SourceExplicit {
		return agentmemory.Provenance{}, agentmemory.ErrUnavailable
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentmemory.Provenance{}, agentmemory.ErrUnavailable
	}
	if m.Status != agentmemory.StatusActive || m.ValidFrom.After(now) || !m.ValidUntil.After(now) {
		return agentmemory.Provenance{}, agentmemory.ErrNotFound
	}
	rows, err := tx.Query(ctx, `SELECT `+memoryEvidenceColumns+` FROM agent_memory_evidence WHERE memory_id=$1 AND agent_id=$2 AND owner_id=$3
	 AND status='CURRENT' AND memory_version=$4 ORDER BY observed_at,id`, memoryID, b.agentID, b.accountID, m.Version)
	if err != nil {
		return agentmemory.Provenance{}, agentmemory.ErrUnavailable
	}
	all := []agentmemory.Evidence{}
	for rows.Next() {
		e, err := scanMemoryEvidence(rows)
		if err != nil {
			rows.Close()
			return agentmemory.Provenance{}, agentmemory.ErrUnavailable
		}
		all = append(all, e)
	}
	rows.Close()
	if rows.Err() != nil {
		return agentmemory.Provenance{}, agentmemory.ErrUnavailable
	}
	// One final statement returns all currently accessible sources. Previous
	// source IDs are only used as private DB lookup inputs; stale ones disappear.
	// Source locks do not certify a permanent ACL or grant analysis permission.
	current := []agentmemory.Evidence{}
	for _, e := range all {
		if err = lockMemoryEvidenceSource(ctx, tx, b.accountID, e.Source.Type, e.Source.ID); errors.Is(err, agentmemory.ErrForbidden) {
			continue
		}
		if err != nil {
			return agentmemory.Provenance{}, err
		}
	}
	// Every ACL-dependent result is rechecked together in one SQL statement.
	// The LATERAL union permits different source kinds without resolving them
	// in independent snapshots and accidentally retaining a revoked earlier row.
	moment, _ := memoryEvidenceSourceSQL(agentevent.MomentSource)
	participation, _ := memoryEvidenceSourceSQL(agentevent.ParticipationSource)
	place, _ := memoryEvidenceSourceSQL(agentevent.SavedPlaceSource)
	// Source selectors are correlated trusted DB columns, never SQL fragments
	// supplied by a caller. Keep $1 for the actual owner in all three branches.
	correlate := func(q string) string { return stringsReplaceEvidenceSourceID(q) }
	query := `SELECT ` + prefixMemoryEvidenceColumns("e") + `,src.revision,src.event_time,src.canonical,clock_timestamp()
	 FROM agent_memory_evidence e JOIN agent_memories m ON m.id=e.memory_id
	 JOIN sessions ses ON ses.id=$5 AND ses.account_id=$1
	 CROSS JOIN LATERAL (` + `SELECT * FROM (` + correlate(moment) + `) z WHERE e.source_type='MOMENT'
	 UNION ALL SELECT * FROM (` + correlate(participation) + `) z WHERE e.source_type='ACTIVITY_PARTICIPATION'
	 UNION ALL SELECT * FROM (` + correlate(place) + `) z WHERE e.source_type='SAVED_PLACE') src
	 WHERE $2::uuid IS NULL AND e.memory_id=$3 AND e.agent_id=$4 AND e.owner_id=$1 AND e.status='CURRENT' AND e.memory_version=$6
	 AND m.version=$6 AND m.agent_id=$4 AND m.owner_id=$1 AND m.status='ACTIVE' AND m.source_type='EXPLICIT'
	 AND m.valid_from<=clock_timestamp() AND m.valid_until>clock_timestamp() AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp()
	 AND ses.idle_expires_at>clock_timestamp() ORDER BY e.observed_at,e.id`
	finalRows, err := tx.Query(ctx, query, b.accountID, nil, memoryID, b.agentID, b.sessionID, m.Version)
	if err != nil {
		return agentmemory.Provenance{}, agentmemory.ErrUnavailable
	}
	for finalRows.Next() {
		var extraRevision int64
		var extraTime, readTime time.Time
		var extraRaw []byte
		var rec extraEvidenceRow
		rec.row = finalRows
		rec.targets = []any{&extraRevision, &extraTime, &extraRaw, &readTime}
		e, scanErr := scanMemoryEvidence(rec)
		if scanErr != nil {
			finalRows.Close()
			return agentmemory.Provenance{}, agentmemory.ErrUnavailable
		}
		v := agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: extraRevision}
		if e.Source.Type != agentevent.MomentSource {
			kind := agentevent.UpdatedAtDigestVersion
			if e.Source.Type == agentevent.SavedPlaceSource {
				kind = agentevent.CreatedAtDigestVersion
			}
			v, scanErr = agentevent.SnapshotVersion(e.Source.Type, kind, extraTime, extraRaw)
		}
		if scanErr != nil {
			finalRows.Close()
			return agentmemory.Provenance{}, agentmemory.ErrUnavailable
		}
		if v == e.Source.Version && e.EventTime != nil && extraTime.Equal(*e.EventTime) && !e.ObservedAt.After(readTime) && !extraTime.After(readTime) {
			current = append(current, e)
		}
	}
	finalRows.Close()
	if finalRows.Err() != nil {
		return agentmemory.Provenance{}, agentmemory.ErrUnavailable
	}
	// Retire invalid references at this authoritative human read boundary.
	// The same Memory lock serializes new references and source invalidation.
	// Only IDs/versions remain; no inaccessible source address is returned or
	// kept blocking the per-Memory source uniqueness/capacity limit. This is
	// lazy local cleanup, not an asynchronous all-domain deletion service.
	validIDs := map[string]bool{}
	for _, e := range current {
		validIDs[e.ID] = true
	}
	invalidIDs := []string{}
	for _, e := range all {
		if !validIDs[e.ID] {
			invalidIDs = append(invalidIDs, e.ID)
		}
	}
	if len(invalidIDs) > 0 {
		if _, err = tx.Exec(ctx, `UPDATE agent_memory_evidence SET status='REMOVED',version=version+1,
		 source_type=NULL,source_id=NULL,source_version_kind=NULL,source_revision=NULL,source_token=NULL,signal_type=NULL,weight=0,event_time=NULL
		 WHERE memory_id=$1 AND agent_id=$2 AND owner_id=$3 AND id=ANY($4::uuid[]) AND status='CURRENT'`, memoryID, b.agentID, b.accountID, invalidIDs); err != nil {
			return agentmemory.Provenance{}, memoryError(err)
		}
	}
	if err = recheckMemoryPutBoundary(ctx, tx, b, m.ValidUntil); err != nil {
		return agentmemory.Provenance{}, err
	}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentmemory.Provenance{}, agentmemory.ErrUnavailable
	}
	provenance, err := agentmemory.BuildOwnProvenance(m, current, now)
	if err != nil {
		return agentmemory.Provenance{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return agentmemory.Provenance{}, memoryError(err)
	}
	return provenance, nil
}

type extraEvidenceRow struct {
	row     pgx.Row
	targets []any
}

func (r extraEvidenceRow) Scan(dest ...any) error { return r.row.Scan(append(dest, r.targets...)...) }

func stringsReplaceEvidenceSourceID(q string) string {
	return strings.ReplaceAll(q, "$2", "e.source_id")
}
func prefixMemoryEvidenceColumns(prefix string) string {
	parts := strings.Split(memoryEvidenceColumns, ",")
	for i := range parts {
		parts[i] = prefix + "." + strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ",")
}
