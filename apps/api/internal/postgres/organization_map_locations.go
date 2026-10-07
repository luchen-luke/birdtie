package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
)

const mapLocationColumns = `organization_id,city_id,latitude,longitude,visibility,review_status,revision`

func scanMapLocation(row pgx.Row) (organization.MapLocation, error) {
	var v organization.MapLocation
	err := row.Scan(&v.OrganizationID, &v.CityID, &v.Latitude, &v.Longitude,
		&v.Visibility, &v.ReviewStatus, &v.Revision)
	return v, err
}

func (s *Store) ListPublicMapPins(ctx context.Context, cityID string) ([]organization.PublicMapPin, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id,o.name,l.latitude,l.longitude,l.coordinate_system,l.precision
		FROM organization_map_locations l JOIN organizations o ON o.id=l.organization_id
		WHERE l.city_id=$1 AND l.visibility='public' AND l.review_status='approved'
		  AND o.status='active' AND o.visibility='public' AND o.verification_status='verified'
		ORDER BY lower(o.name),o.id`, cityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []organization.PublicMapPin{}
	for rows.Next() {
		var v organization.PublicMapPin
		if err := rows.Scan(&v.ID, &v.Name, &v.Latitude, &v.Longitude, &v.CoordinateSystem, &v.Precision); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetMapLocation(ctx context.Context, actorID, orgID string) (organization.MapLocation, error) {
	v, err := scanMapLocation(s.pool.QueryRow(ctx, `SELECT l.organization_id,l.city_id,l.latitude,l.longitude,l.visibility,l.review_status,l.revision
		FROM organization_map_locations l JOIN organization_memberships m ON m.organization_id=l.organization_id
		JOIN organizations o ON o.id=l.organization_id
		WHERE l.organization_id=$1 AND m.user_account_id=$2 AND m.status='active'
		AND m.role IN ('owner','admin') AND o.status='active'`, orgID, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.MapLocation{}, organization.ErrNotFound
	}
	return v, err
}

func (s *Store) SubmitMapLocation(ctx context.Context, actorID, orgID, cityID string, latitude, longitude float64) (organization.MapLocation, error) {
	tx, role, err := s.membershipTx(ctx, actorID, orgID)
	if err != nil {
		return organization.MapLocation{}, err
	}
	defer tx.Rollback(ctx)
	if role != "owner" && role != "admin" {
		return organization.MapLocation{}, organization.ErrForbidden
	}
	var v organization.MapLocation
	v, err = scanMapLocation(tx.QueryRow(ctx, `INSERT INTO organization_map_locations
		(organization_id,city_id,latitude,longitude,visibility,review_status,submitted_by)
		VALUES ($1,$2,$3,$4,'public','pending',$5)
		ON CONFLICT (organization_id) DO UPDATE SET
		city_id=EXCLUDED.city_id,latitude=EXCLUDED.latitude,longitude=EXCLUDED.longitude,
		visibility='public',review_status='pending',submitted_by=EXCLUDED.submitted_by,
		submitted_at=now(),reviewed_by=NULL,reviewed_at=NULL,review_note='',
		revision=organization_map_locations.revision+1
		RETURNING `+mapLocationColumns, orgID, cityID, latitude, longitude, actorID))
	if err != nil {
		return v, err
	}
	if _, err = auditExec(ctx, tx, `INSERT INTO organization_map_location_audit
		(organization_id,actor_account_id,action,revision) VALUES ($1,$2,'submit',$3)`, orgID, actorID, v.Revision); err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}

func (s *Store) HideMapLocation(ctx context.Context, actorID, orgID string) error {
	tx, role, err := s.membershipTx(ctx, actorID, orgID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if role != "owner" && role != "admin" {
		return organization.ErrForbidden
	}
	var revision int64
	err = tx.QueryRow(ctx, `UPDATE organization_map_locations SET visibility='hidden',revision=revision+1
		WHERE organization_id=$1 RETURNING revision`, orgID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = auditExec(ctx, tx, `INSERT INTO organization_map_location_audit
		(organization_id,actor_account_id,action,revision) VALUES ($1,$2,'hide',$3)`, orgID, actorID, revision); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ReviewMapLocation(ctx context.Context, actorID, orgID, decision, note string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Use the same organization lock order as owner submissions.
	var active bool
	err = tx.QueryRow(ctx, `SELECT status='active' FROM organizations WHERE id=$1 FOR UPDATE`, orgID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !active {
		return organization.ErrConflict
	}
	var cityID, submitter, status, visibility string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT city_id,submitted_by,review_status,visibility,revision
		FROM organization_map_locations WHERE organization_id=$1 FOR UPDATE`, orgID).
		Scan(&cityID, &submitter, &status, &visibility, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.ErrNotFound
	}
	if err != nil {
		return err
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM city_editor_memberships m
		JOIN accounts a ON a.id=m.account_id AND a.status='active' AND a.account_type='person'
		WHERE m.city_id=$1 AND m.account_id=$2 AND m.role='reviewer' AND m.state='active')`, cityID, actorID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return organization.ErrForbidden
	}
	if actorID == submitter || status != "pending" || visibility != "public" {
		return organization.ErrConflict
	}
	newStatus := "approved"
	action := "approve"
	if decision == "reject" {
		newStatus = "rejected"
		action = "reject"
	}
	_, err = tx.Exec(ctx, `UPDATE organization_map_locations SET review_status=$2,
		reviewed_by=$3,reviewed_at=now(),review_note=$4 WHERE organization_id=$1`, orgID, newStatus, actorID, note)
	if err != nil {
		return err
	}
	if _, err = auditExec(ctx, tx, `INSERT INTO organization_map_location_audit
		(organization_id,actor_account_id,action,revision) VALUES ($1,$2,$3,$4)`, orgID, actorID, action, revision); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
