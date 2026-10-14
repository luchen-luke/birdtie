package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const candidateColumns = `id, city_id, submitted_by, name, category_code,
    summary, COALESCE(address_label,''), latitude, longitude, location_precision, source_label, source_url,
    rights_note, provider_code, provider_place_id, attribution, expires_at,
    status, reviewed_by, reviewed_at, review_note, resolved_place_id, created_at, permission_id, field_claims`

func scanCandidate(row scanner) (cityseed.Candidate, error) {
	var c cityseed.Candidate
	var fields []byte
	err := row.Scan(&c.ID, &c.CityID, &c.SubmittedBy, &c.Name,
		&c.CategoryCode, &c.Summary, &c.AddressLabel, &c.Latitude, &c.Longitude,
		&c.LocationPrecision, &c.SourceLabel, &c.SourceURL, &c.RightsNote,
		&c.ProviderCode, &c.ProviderPlaceID, &c.Attribution,
		&c.ExpiresAt, &c.Status, &c.ReviewedBy, &c.ReviewedAt,
		&c.ReviewNote, &c.ResolvedPlaceID, &c.CreatedAt, &c.PermissionID, &fields)
	if err == nil && len(fields) > 0 {
		err = json.Unmarshal(fields, &c.FieldClaims)
	}
	return c, err
}

func (s *Store) Submit(ctx context.Context, actorID, cityID string, input cityseed.SubmitInput) (cityseed.Candidate, error) {
	rights, fields, err := cityseed.NormalizeSourcePermission(input, time.Now().UTC())
	if err != nil {
		return cityseed.Candidate{}, err
	} // No transaction or restricted content write before this gate.
	session, ok := cityseed.CurrentHumanSession(ctx)
	if !ok || session.ActorID != actorID || s == nil || s.pool == nil {
		return cityseed.Candidate{}, cityseed.ErrForbidden
	}
	var providerCode, providerPlaceID, attribution, addressLabel any
	if input.AddressLabel != "" {
		addressLabel = input.AddressLabel
	}
	if input.ProviderCode != "" {
		providerCode, providerPlaceID, attribution =
			input.ProviderCode, input.ProviderPlaceID, input.Attribution
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cityseed.Candidate{}, err
	}
	defer tx.Rollback(ctx)
	sessionID, err := lockCitySeedHumanTx(ctx, tx, actorID, cityID, false, s.devPhoneEnabled)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	var nativeNow time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&nativeNow); err != nil {
		return cityseed.Candidate{}, err
	}
	rights, fields, err = cityseed.NormalizeSourcePermission(input, nativeNow)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	reused, err := currentCitySeedSourceReuseTx(ctx, tx, cityID, input, nativeNow)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	if reused.placeID != "" {
		if err = finalCitySeedSessionTx(ctx, tx, sessionID); err != nil {
			return cityseed.Candidate{}, err
		}
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&nativeNow); err != nil {
			return cityseed.Candidate{}, err
		}
		// All identity/Place/field/permission rows remain locked until this
		// return. Recheck native time after session/row waits, without renewal.
		if ctx.Err() != nil || !reused.validUntil.After(nativeNow) {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
		return cityseed.Candidate{}, errCurrentCitySeedSourceDuplicate
	}
	candidateID, err := humanCandidateUUID()
	if err != nil {
		return cityseed.Candidate{}, err
	}
	permission, err := insertCandidatePermissionTx(ctx, tx, candidateID, actorID, input.SourceURL, rights)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	fieldRaw, err := encodeCandidateFields(fields)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	row := tx.QueryRow(ctx, `INSERT INTO place_candidates (
        id, city_id, submitted_by, name, category_code, summary, address_label, latitude, longitude,
        location_precision, source_label, source_url, rights_note,
        provider_code, provider_place_id, attribution, expires_at, permission_id, field_claims
    )
    SELECT $17, c.id, a.id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
        $13, $14, $15, $16, $18, $19::jsonb
    FROM city_editor_memberships m
    JOIN cities c ON c.id = m.city_id AND c.publication_status = 'published'
    JOIN accounts a ON a.id = m.account_id AND a.status = 'active'
    WHERE m.city_id = $1 AND m.account_id = $2 AND m.state = 'active'
    RETURNING `+candidateColumns,
		cityID, actorID, input.Name, input.CategoryCode, input.Summary, addressLabel,
		input.Latitude, input.Longitude, input.LocationPrecision,
		input.SourceLabel, input.SourceURL, input.RightsNote,
		providerCode, providerPlaceID, attribution, input.ExpiresAt, candidateID, permission.ID, fieldRaw)
	c, err := scanCandidate(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return cityseed.Candidate{}, cityseed.ErrForbidden
	}
	if err != nil {
		return cityseed.Candidate{}, seedWriteError(err)
	}
	c.Permission = &permission
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'submit', 'place_candidate', $2, 'allowed', 'city_seed')`,
		actorID, c.ID)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	if err = finalCitySeedSessionTx(ctx, tx, sessionID); err != nil {
		return cityseed.Candidate{}, err
	}
	if err = finalCandidatePermissionTx(ctx, tx, permission.ID, permission.Version); err != nil {
		return cityseed.Candidate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return cityseed.Candidate{}, err
	}
	return c, nil
}

func (s *Store) List(ctx context.Context, actorID, cityID string) ([]cityseed.Candidate, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM city_editor_memberships m
        JOIN accounts a ON a.id = m.account_id
        WHERE m.city_id = $1 AND m.account_id = $2
          AND m.state = 'active' AND a.status = 'active'
    )`, cityID, actorID).Scan(&allowed)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, cityseed.ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT `+candidateColumns+`
        FROM place_candidates WHERE city_id = $1 AND status = 'pending'
        ORDER BY created_at, id LIMIT 100`, cityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]cityseed.Candidate, 0)
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range candidates {
		if candidates[i].PermissionID != nil {
			p, e := scanSourcePermission(s.pool.QueryRow(ctx, `SELECT `+sourcePermissionColumns+` FROM place_source_permissions WHERE id=$1`, *candidates[i].PermissionID))
			if e != nil {
				return nil, e
			}
			candidates[i].Permission = &p
		}
	}
	return candidates, nil
}

func (s *Store) Review(ctx context.Context, actorID, candidateID string, input cityseed.ReviewInput) (cityseed.Candidate, error) {
	session, ok := cityseed.CurrentHumanSession(ctx)
	if !ok || session.ActorID != actorID || s == nil || s.pool == nil {
		return cityseed.Candidate{}, cityseed.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return cityseed.Candidate{}, err
	}
	defer tx.Rollback(ctx)
	var cityID string
	if err = tx.QueryRow(ctx, `SELECT city_id FROM place_candidates WHERE id=$1`, candidateID).Scan(&cityID); err != nil {
		return cityseed.Candidate{}, cityseed.ErrForbidden
	}
	sessionID, err := lockCitySeedHumanTx(ctx, tx, actorID, cityID, true, s.devPhoneEnabled)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	c, err := scanCandidate(tx.QueryRow(ctx, `SELECT `+candidateColumns+`
        FROM place_candidates
        WHERE id = $1 AND city_id IN (
            SELECT m.city_id FROM city_editor_memberships m
            JOIN accounts a ON a.id = m.account_id
            WHERE m.account_id = $2 AND m.role = 'reviewer'
              AND m.state = 'active' AND a.status = 'active'
        ) FOR UPDATE`, candidateID, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return cityseed.Candidate{}, cityseed.ErrForbidden
	}
	if err != nil {
		return cityseed.Candidate{}, err
	}
	if c.SubmittedBy == actorID {
		return cityseed.Candidate{}, cityseed.ErrConflict
	}
	if input.Decision == "revoke_permission" {
		if c.PermissionID == nil || input.ExpectedPermissionVersion <= 0 || (input.PermissionState != cityseed.PermissionPending && input.PermissionState != cityseed.PermissionProhibited) || input.TargetPlaceID != "" {
			return cityseed.Candidate{}, cityseed.ErrSourcePermission
		}
		p, e := sourcePermissionTx(ctx, tx, *c.PermissionID, true)
		if e != nil {
			return cityseed.Candidate{}, e
		}
		if p.Version != input.ExpectedPermissionVersion {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
		p, e = scanSourcePermission(tx.QueryRow(ctx, `UPDATE place_source_permissions SET state=$2,version=version+1,revoked_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND version=$3 RETURNING `+sourcePermissionColumns, p.ID, input.PermissionState, p.Version))
		if e != nil {
			return cityseed.Candidate{}, e
		}
		c.Permission = &p
		if _, e = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,'revoke','place_source_permission',$2,'allowed','city_seed_rights_review')`, actorID, p.ID); e != nil {
			return cityseed.Candidate{}, e
		}
		if e = finalCitySeedSessionTx(ctx, tx, sessionID); e != nil {
			return cityseed.Candidate{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return cityseed.Candidate{}, e
		}
		return c, nil
	}
	if c.Status != "pending" {
		return cityseed.Candidate{}, cityseed.ErrConflict
	}
	var permission cityseed.SourcePermission
	if input.Decision != "reject" {
		if c.PermissionID == nil || !input.RightsConfirmed || !input.FieldsConfirmed || input.ExpectedPermissionVersion <= 0 {
			return cityseed.Candidate{}, cityseed.ErrSourcePermission
		}
		permission, err = sourcePermissionTx(ctx, tx, *c.PermissionID, true)
		if err != nil {
			return cityseed.Candidate{}, err
		}
		var at time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
			return cityseed.Candidate{}, err
		}
		if permission.Version != input.ExpectedPermissionVersion || permission.State != cityseed.PermissionConfirmed || permission.RevokedAt != nil || !permission.RetentionUntil.After(at) || permission.RetentionUntil.Before(c.ExpiresAt) {
			return cityseed.Candidate{}, cityseed.ErrSourcePermission
		}
		provider := ""
		if c.ProviderCode != nil {
			provider = *c.ProviderCode
		}
		if _, _, err = cityseed.NormalizeSourcePermission(cityseed.SubmitInput{Name: c.Name, AddressLabel: c.AddressLabel, Latitude: c.Latitude, Longitude: c.Longitude, LocationPrecision: c.LocationPrecision, SourceURL: c.SourceURL, ProviderCode: provider, ExpiresAt: c.ExpiresAt, Rights: &permission.SourcePermissionInput, Fields: c.FieldClaims}, at); err != nil {
			return cityseed.Candidate{}, err
		}
		permission, err = scanSourcePermission(tx.QueryRow(ctx, `UPDATE place_source_permissions SET reviewed_by=$2,reviewed_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND version=$3 RETURNING `+sourcePermissionColumns, permission.ID, actorID, permission.Version))
		if err != nil {
			return cityseed.Candidate{}, err
		}
	}
	if input.Decision != "reject" && !c.ExpiresAt.After(time.Now()) {
		return cityseed.Candidate{}, cityseed.ErrConflict
	}
	var resolvedPlaceID any
	switch input.Decision {
	case "publish":
		if input.TargetPlaceID != "" {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
		var placeID string
		if err := tx.QueryRow(ctx, `INSERT INTO places (
            id, city_id, name, category_code, summary, address_label, latitude, longitude,
            location_precision, publication_status, source_label, source_ref,
            maintainer_label, maintainer_account_id, verified_at, expires_at
        )
        SELECT gen_random_uuid(), $1, $2, $3, $4, NULLIF($5,''), $6, $7, $8, 'published',
            $9, $10, '城市维护者',
            a.id, now(), $11
        FROM accounts a WHERE a.id = $12 AND a.status = 'active'
        RETURNING id`, c.CityID, c.Name, c.CategoryCode, c.Summary,
			c.AddressLabel, c.Latitude, c.Longitude, c.LocationPrecision, c.SourceLabel,
			c.SourceURL, c.ExpiresAt, actorID).Scan(&placeID); err != nil {
			return cityseed.Candidate{}, seedWriteError(err)
		}
		resolvedPlaceID = placeID
	case "link_existing":
		if input.TargetPlaceID == "" {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
		var placeID string
		err := tx.QueryRow(ctx, `SELECT id FROM places
            WHERE id = $1 AND city_id = $2 AND publication_status = 'published'
            FOR SHARE`, input.TargetPlaceID, c.CityID).Scan(&placeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
		if err != nil {
			return cityseed.Candidate{}, err
		}
		resolvedPlaceID = placeID
		_, err = tx.Exec(ctx, `INSERT INTO place_aliases (place_id, locale, alias, source_ref)
            SELECT p.id, 'und', $2, $3 FROM places p
            WHERE p.id = $1 AND p.name <> $2
            ON CONFLICT DO NOTHING`, placeID, c.Name, c.SourceURL)
		if err != nil {
			return cityseed.Candidate{}, err
		}
	case "reject":
		if input.TargetPlaceID != "" {
			return cityseed.Candidate{}, cityseed.ErrConflict
		}
	default:
		return cityseed.Candidate{}, cityseed.ErrConflict
	}
	if resolvedPlaceID != nil {
		if err = publishCandidateFieldsTx(ctx, tx, c, resolvedPlaceID.(string), permission); err != nil {
			return cityseed.Candidate{}, err
		}
		if err := s.linkCandidateSource(ctx, tx, c, actorID, resolvedPlaceID); err != nil {
			return cityseed.Candidate{}, err
		}
		if input.Decision == "link_existing" {
			if c.AddressLabel != "" {
				if _, err := tx.Exec(ctx, `UPDATE places SET address_label=$2,updated_at=now()
					WHERE id=$1 AND address_label IS NULL AND location_precision='point'`,
					resolvedPlaceID, c.AddressLabel); err != nil {
					return cityseed.Candidate{}, err
				}
			}
			_, err = tx.Exec(ctx, `UPDATE places p
                SET source_label = $2, source_ref = $3,
                    maintainer_label = '城市维护者',
                    maintainer_account_id = a.id, updated_at = now(),
                    verified_at = now(), expires_at = $4
                FROM accounts a
                WHERE p.id = $1 AND a.id = $5
                  AND (p.expires_at IS NULL OR p.expires_at < $4)`,
				resolvedPlaceID, c.SourceLabel, c.SourceURL, c.ExpiresAt, actorID)
			if err != nil {
				return cityseed.Candidate{}, err
			}
		}
	}
	status := map[string]string{
		"publish":       "published",
		"link_existing": "linked_duplicate",
		"reject":        "rejected",
	}[input.Decision]
	c, err = scanCandidate(tx.QueryRow(ctx, `UPDATE place_candidates
        SET status = $2, reviewed_by = $3, reviewed_at = now(),
            review_note = $4, resolved_place_id = $5
        WHERE id = $1 AND status = 'pending'
        RETURNING `+candidateColumns,
		candidateID, status, actorID, input.Note, resolvedPlaceID))
	if err != nil {
		return cityseed.Candidate{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, $2, 'place_candidate', $3, 'allowed', 'city_seed')`,
		actorID, status, candidateID)
	if err != nil {
		return cityseed.Candidate{}, err
	}
	detail := "Your place suggestion was not published after review."
	if status == "published" || status == "linked_duplicate" {
		detail = "Your place suggestion has been added to Birdtie's public city records."
	}
	if err := insertReviewInboxItem(ctx, tx, c.SubmittedBy, "place_candidate", c.ID,
		"Place suggestion reviewed", detail); err != nil {
		return cityseed.Candidate{}, err
	}
	if input.Decision != "reject" {
		c.Permission = &permission
	}
	if err = finalCitySeedSessionTx(ctx, tx, sessionID); err != nil {
		return cityseed.Candidate{}, err
	}
	if input.Decision != "reject" {
		if err = finalCandidatePermissionTx(ctx, tx, permission.ID, permission.Version); err != nil {
			return cityseed.Candidate{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return cityseed.Candidate{}, err
	}
	return c, nil
}

func (s *Store) linkCandidateSource(ctx context.Context, tx pgx.Tx, c cityseed.Candidate, reviewerID string, placeID any) error {
	_, err := tx.Exec(ctx, `INSERT INTO place_sources (
        place_id, candidate_id, source_label, source_url, rights_note,
        reviewer_account_id, verified_at, expires_at, permission_id
    ) VALUES ($1, $2, $3, $4, $5, $6, now(), $7, $8)`,
		placeID, c.ID, c.SourceLabel, c.SourceURL, c.RightsNote,
		reviewerID, c.ExpiresAt, c.PermissionID)
	if err != nil {
		return seedWriteError(err)
	}
	if c.ProviderCode != nil {
		var linkedPlaceID string
		err = tx.QueryRow(ctx, `INSERT INTO place_external_refs (
            place_id, provider_code, provider_place_id, source_url,
            attribution, rights_basis, retrieved_at
        ) VALUES ($1, $2, $3, $4, $5, $6, now())
        ON CONFLICT (provider_code, provider_place_id)
        DO UPDATE SET place_id = EXCLUDED.place_id
        WHERE place_external_refs.place_id = EXCLUDED.place_id
        RETURNING place_id`, placeID, c.ProviderCode, c.ProviderPlaceID,
			c.SourceURL, c.Attribution, c.RightsNote).Scan(&linkedPlaceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return cityseed.ErrDuplicate
		}
		if err != nil {
			return seedWriteError(err)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO city_seed_items (
        city_id, place_id, maintainer_account_id, valid_until
    ) VALUES ($1, $2, $3, $4)
    ON CONFLICT (city_id, place_id) DO UPDATE
    SET valid_until = GREATEST(city_seed_items.valid_until, EXCLUDED.valid_until),
        state = 'active', updated_at = now()`,
		c.CityID, placeID, reviewerID, c.ExpiresAt)
	return err
}

func seedWriteError(err error) error {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return cityseed.ErrDuplicate
	}
	return err
}
