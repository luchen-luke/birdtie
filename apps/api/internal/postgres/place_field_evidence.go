package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/jackc/pgx/v5"
)

const sourcePermissionColumns = `id,candidate_id,version,state,acquisition,origin_provider,source_url,license_url,license_version,attribution,acquired_at,retention_until,declared_by,reviewed_by,reviewed_at,revoked_at,created_at,updated_at`

func scanSourcePermission(row scanner) (cityseed.SourcePermission, error) {
	var p cityseed.SourcePermission
	e := row.Scan(&p.ID, &p.CandidateID, &p.Version, &p.State, &p.Acquisition, &p.OriginProvider, &p.SourceURL, &p.LicenseURL, &p.LicenseVersion, &p.Attribution, &p.AcquiredAt, &p.RetentionUntil, &p.DeclaredBy, &p.ReviewedBy, &p.ReviewedAt, &p.RevokedAt, &p.CreatedAt, &p.UpdatedAt)
	return p, e
}
func sourcePermissionTx(ctx context.Context, tx pgx.Tx, id string, write bool) (cityseed.SourcePermission, error) {
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	p, e := scanSourcePermission(tx.QueryRow(ctx, `SELECT `+sourcePermissionColumns+` FROM place_source_permissions WHERE id=$1`+lock, id))
	if errors.Is(e, pgx.ErrNoRows) {
		return p, cityseed.ErrSourcePermission
	}
	return p, e
}

func lockCitySeedHumanTx(ctx context.Context, tx pgx.Tx, actor, city string, reviewer bool, dev bool) (string, error) {
	session, ok := cityseed.CurrentHumanSession(ctx)
	if !ok || session.ActorID != actor {
		return "", cityseed.ErrForbidden
	}
	var id string
	if e := tx.QueryRow(ctx, `SELECT a.id FROM accounts a WHERE a.id=$1 AND a.status='active' AND a.account_type='person' FOR SHARE`, actor).Scan(&id); e != nil {
		return "", cityseed.ErrForbidden
	}
	if e := tx.QueryRow(ctx, `SELECT m.city_id FROM city_editor_memberships m JOIN cities c ON c.id=m.city_id WHERE m.city_id=$1 AND m.account_id=$2 AND m.state='active' AND c.publication_status='published' AND (NOT $3::boolean OR m.role='reviewer') FOR SHARE OF m,c`, city, actor, reviewer).Scan(&id); e != nil {
		return "", cityseed.ErrForbidden
	}
	if e := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE account_id=$1 AND token_sha256=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone')`, actor, session.Digest[:], dev).Scan(&id); e != nil {
		return "", cityseed.ErrForbidden
	}
	return id, nil
}
func finalCitySeedSessionTx(ctx context.Context, tx pgx.Tx, session string) error {
	var id string
	if e := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id=$1 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() FOR SHARE`, session).Scan(&id); e != nil || ctx.Err() != nil {
		return cityseed.ErrForbidden
	}
	return nil
}

func finalCandidatePermissionTx(ctx context.Context, tx pgx.Tx, permissionID string, version int64) error {
	var current bool
	e := tx.QueryRow(ctx, `SELECT state='CONFIRMED' AND revoked_at IS NULL AND version=$2 AND retention_until>clock_timestamp() FROM place_source_permissions WHERE id=$1`, permissionID, version).Scan(&current)
	if e != nil || !current || ctx.Err() != nil {
		return cityseed.ErrSourcePermission
	}
	return nil
}

func insertCandidatePermissionTx(ctx context.Context, tx pgx.Tx, candidateID, actor, source string, r cityseed.SourcePermissionInput) (cityseed.SourcePermission, error) {
	id, e := humanCandidateUUID()
	if e != nil {
		return cityseed.SourcePermission{}, e
	}
	return scanSourcePermission(tx.QueryRow(ctx, `INSERT INTO place_source_permissions(id,candidate_id,version,state,acquisition,origin_provider,source_url,license_url,license_version,attribution,acquired_at,retention_until,declared_by) VALUES($1,$2,1,'CONFIRMED',$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+sourcePermissionColumns, id, candidateID, r.Acquisition, r.OriginProvider, source, r.LicenseURL, r.LicenseVersion, r.Attribution, r.AcquiredAt, r.RetentionUntil, actor))
}
func publishCandidateFieldsTx(ctx context.Context, tx pgx.Tx, c cityseed.Candidate, placeID string, p cityseed.SourcePermission) error {
	var at time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return e
	}
	if !cityseed.PermissionUsable(p, at) {
		return cityseed.ErrSourcePermission
	}
	for _, f := range c.FieldClaims {
		if _, e := tx.Exec(ctx, `INSERT INTO place_field_evidence(place_id,source_candidate_id,field_key,value,observed_at,verified_at,valid_from,valid_to,refresh_due,permission_id,permission_version,version,conflict_state) VALUES($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10,$11,1,$12)`, placeID, c.ID, f.Key, f.Value, f.ObservedAt, at, f.ValidFrom, f.ValidTo, f.RefreshDue, p.ID, p.Version, f.ConflictState); e != nil {
			return e
		}
	}
	return nil
}

// This reader is intentionally in the original transaction. Its caller keeps
// that transaction/row locks through its final response-source fence. Empty
// evidence on an old place is unknown, never grandfathered confirmed rights.
func ReadPlaceFieldEvidenceTx(ctx context.Context, tx pgx.Tx, placeID string, fields []string, at time.Time) ([]cityseed.PlaceFieldEvidence, error) {
	if ctx == nil || ctx.Err() != nil || tx == nil || !egressUUID(placeID) || at.IsZero() || len(fields) == 0 || len(fields) > 6 {
		return nil, cityseed.ErrConflict
	}
	seen := map[string]bool{}
	for _, field := range fields {
		if !cityseed.ValidPlaceFieldKey(field) || seen[field] {
			return nil, cityseed.ErrConflict
		}
		seen[field] = true
	}
	rows, e := tx.Query(ctx, `SELECT f.id,f.place_id,f.source_candidate_id,f.field_key,f.value,f.observed_at,f.verified_at,f.valid_from,f.valid_to,f.refresh_due,f.permission_id,f.permission_version,f.version,f.conflict_state,
 p.id,p.candidate_id,p.version,p.state,p.acquisition,p.origin_provider,p.source_url,p.license_url,p.license_version,p.attribution,p.acquired_at,p.retention_until,p.declared_by,p.reviewed_by,p.reviewed_at,p.revoked_at,p.created_at,p.updated_at,concat(f.xmin::text,':',p.xmin::text)
 FROM place_field_evidence f JOIN place_source_permissions p ON p.id=f.permission_id JOIN place_candidates c ON c.id=f.source_candidate_id AND c.permission_id=p.id AND c.status IN('published','linked_duplicate')
 JOIN places pl ON pl.id=f.place_id AND pl.publication_status='published' JOIN cities city ON city.id=pl.city_id AND city.publication_status='published'
 WHERE f.place_id=$1 AND f.field_key=ANY($2::text[]) AND p.state='CONFIRMED' AND p.revoked_at IS NULL AND p.version=f.permission_version AND p.reviewed_by IS NOT NULL AND p.reviewed_by<>p.declared_by AND p.reviewed_at<=clock_timestamp() AND p.retention_until>clock_timestamp()
 AND f.valid_from<=clock_timestamp() AND f.valid_to>clock_timestamp() AND f.refresh_due>clock_timestamp() AND f.conflict_state='NONE' AND f.observed_at<=clock_timestamp() AND f.verified_at<=clock_timestamp()
 AND (pl.expires_at IS NULL OR pl.expires_at>clock_timestamp()) AND (city.expires_at IS NULL OR city.expires_at>clock_timestamp()) ORDER BY f.field_key,f.verified_at DESC,f.id FOR SHARE OF f,p,c,pl,city`, placeID, fields)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []cityseed.PlaceFieldEvidence{}
	for rows.Next() {
		var f cityseed.PlaceFieldEvidence
		p := &f.Permission
		e = rows.Scan(&f.ID, &f.PlaceID, &f.SourceCandidateID, &f.FieldKey, &f.Value, &f.ObservedAt, &f.VerifiedAt, &f.ValidFrom, &f.ValidTo, &f.RefreshDue, &f.PermissionID, &f.PermissionVersion, &f.Version, &f.ConflictState, &p.ID, &p.CandidateID, &p.Version, &p.State, &p.Acquisition, &p.OriginProvider, &p.SourceURL, &p.LicenseURL, &p.LicenseVersion, &p.Attribution, &p.AcquiredAt, &p.RetentionUntil, &p.DeclaredBy, &p.ReviewedBy, &p.ReviewedAt, &p.RevokedAt, &p.CreatedAt, &p.UpdatedAt, &f.RowToken)
		if e != nil {
			return nil, e
		}
		if !cityseed.ValidPlaceFieldValue(f.FieldKey, f.Value) || !cityseed.PermissionUsable(*p, at) || f.PermissionVersion != p.Version || f.ConflictState != "NONE" || !f.ValidTo.After(at) || !f.RefreshDue.After(at) {
			return nil, fmt.Errorf("invalid retained place field evidence")
		}
		out = append(out, f)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return removeConflictingPlaceFields(out), nil
}

// Conflicting current independent source values leave that field unknown.
// Verification time or row order never silently wins a disagreement.
func removeConflictingPlaceFields(fields []cityseed.PlaceFieldEvidence) []cityseed.PlaceFieldEvidence {
	values := map[string]string{}
	conflicts := map[string]bool{}
	for _, f := range fields {
		raw := string(f.Value)
		if old, ok := values[f.FieldKey]; ok && old != raw {
			conflicts[f.FieldKey] = true
		}
		values[f.FieldKey] = raw
	}
	out := []cityseed.PlaceFieldEvidence{}
	for _, f := range fields {
		if !conflicts[f.FieldKey] {
			out = append(out, f)
		}
	}
	return out
}
func encodeCandidateFields(fields []cityseed.PlaceFieldInput) ([]byte, error) {
	return json.Marshal(fields)
}
