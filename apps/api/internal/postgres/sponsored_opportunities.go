package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	so "github.com/birdtie/birdtie/apps/api/internal/sponsoredopportunity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"time"
)

var _ so.Store = (*Store)(nil)

func sponsorError(e error) error {
	for _, v := range []error{so.ErrInvalid, so.ErrForbidden, so.ErrConflict, so.ErrNotFound, so.ErrUnavailable, identity.ErrUnauthorized} {
		if errors.Is(e, v) {
			return v
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return so.ErrForbidden
	}
	var p *pgconn.PgError
	if errors.As(e, &p) {
		if p.Code == "23505" || p.Code == "40001" || p.Code == "40P01" {
			return so.ErrConflict
		}
		if p.Code == "23514" || p.Code == "22003" || p.Code == "22P02" {
			return so.ErrInvalid
		}
	}
	return so.ErrUnavailable
}
func (s *Store) beginSponsor(ctx context.Context, a so.Access, bid string) (pgx.Tx, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, so.ErrUnavailable
	}
	if so.ValidateAccess(a, false) != nil || (bid != "" && !so.ValidID(bid)) {
		return nil, so.ErrForbidden
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, so.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, error) { _ = tx.Rollback(context.Background()); return nil, sponsorError(e) }
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(e)
	}
	ids := []string{a.ActorID}
	if bid != "" {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,70003))`, bid); e != nil {
			return fail(e)
		}
		var principal string
		e = tx.QueryRow(ctx, `SELECT account_id FROM businesses WHERE id=$1 AND status='active' FOR NO KEY UPDATE`, bid).Scan(&principal)
		if e != nil {
			return fail(e)
		}
		ids = append(ids, principal)
	}
	rows, e := tx.Query(ctx, `SELECT id,status,account_type FROM accounts WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, ids)
	if e != nil {
		return fail(e)
	}
	n := 0
	for rows.Next() {
		var id, state, kind string
		if e = rows.Scan(&id, &state, &kind); e != nil {
			rows.Close()
			return fail(e)
		}
		if state != "active" || (id == a.ActorID && kind != "person") || (id != a.ActorID && kind != "business") {
			rows.Close()
			return fail(so.ErrForbidden)
		}
		n++
	}
	rows.Close()
	if rows.Err() != nil || n != len(ids) {
		return fail(so.ErrForbidden)
	}
	// Session is never refreshed and is locked only after business/resource waits.
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.ActorID, s.devPhoneEnabled); e != nil {
		return fail(e)
	}
	return tx, nil
}
func (s *Store) sponsorFinish(ctx context.Context, tx pgx.Tx, a so.Access, expectedAuthority, bid, city string, review bool, sourceCheck *so.Declaration) error {
	if e := s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.ActorID); e != nil {
		return sponsorError(e)
	}
	at, e := s.sponsorClock(ctx, tx)
	if e != nil {
		return e
	}
	auth, e := s.sponsorAuthority(ctx, tx, a, bid, city, review, at)
	if e != nil || auth != expectedAuthority {
		if e != nil {
			return e
		}
		return so.ErrConflict
	}
	if sourceCheck != nil {
		current, e := s.sponsorSource(ctx, tx, sourceCheck.BusinessID, sourceCheckSubmittedBy(ctx, tx, sourceCheck.ID), sourceCheck.TargetType, sourceCheck.TargetID, at)
		if e != nil {
			return e
		}
		var original string
		e = tx.QueryRow(ctx, `SELECT source_snapshot FROM sponsored_opportunity_declarations WHERE id=$1`, sourceCheck.ID).Scan(&original)
		if e != nil {
			return so.ErrUnavailable
		}
		if current == "" || current != original || !sourceCheck.ExpiresAt.After(at) {
			return so.ErrConflict
		}
	}
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.ActorID, s.devPhoneEnabled); e != nil {
		return sponsorError(e)
	}
	if ctx.Err() != nil {
		return so.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return sponsorError(e)
	}
	return nil
}
func sourceCheckSubmittedBy(ctx context.Context, tx pgx.Tx, id string) string {
	var actor string
	if tx.QueryRow(ctx, `SELECT submitted_by FROM sponsored_opportunity_declarations WHERE id=$1`, id).Scan(&actor) != nil {
		return ""
	}
	return actor
}
func (s *Store) sponsorClock(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	var t time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t); e != nil || ctx.Err() != nil {
		return t, so.ErrUnavailable
	}
	return t.UTC(), nil
}
func (s *Store) sponsorAuthority(ctx context.Context, tx pgx.Tx, a so.Access, bid, city string, review bool, at time.Time) (string, error) {
	var hash string
	if review {
		var member bool
		if bid != "" {
			if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM business_memberships WHERE business_id=$1 AND user_account_id=$2 AND status='active')`, bid, a.ActorID).Scan(&member); e != nil {
				return "", so.ErrUnavailable
			}
			if member {
				return "", so.ErrForbidden
			}
		}
		e := tx.QueryRow(ctx, `SELECT COALESCE(birdtie_sponsor_review_snapshot($1,$2,$3),'')`, city, a.ActorID, at).Scan(&hash)
		if e != nil {
			return "", so.ErrUnavailable
		}
	} else {
		e := tx.QueryRow(ctx, `SELECT COALESCE((SELECT encode(sha256(convert_to(jsonb_build_object('actorEpoch',a.xmin::text,'principalEpoch',bp.xmin::text,'businessEpoch',b.xmin::text,'member',to_jsonb(m),'memberEpoch',m.xmin::text)::text,'UTF8')),'hex') FROM businesses b JOIN accounts bp ON bp.id=b.account_id JOIN accounts a ON a.id=$2 JOIN business_memberships m ON m.business_id=b.id AND m.user_account_id=a.id WHERE b.id=$1 AND b.status='active' AND bp.status='active' AND bp.account_type='business' AND a.status='active' AND a.account_type='person' AND m.status='active' AND m.role IN ('owner','admin') AND m.updated_at<=$3),'')`, bid, a.ActorID, at).Scan(&hash)
		if e != nil {
			return "", so.ErrUnavailable
		}
	}
	if hash == "" {
		return "", so.ErrForbidden
	}
	return hash, nil
}
func (s *Store) sponsorSource(ctx context.Context, tx pgx.Tx, bid, author, kind, target string, at time.Time) (string, error) {
	var hash string
	e := tx.QueryRow(ctx, `SELECT COALESCE(birdtie_sponsor_source_snapshot($1,$2,$3,$4,$5),'')`, bid, author, kind, target, at).Scan(&hash)
	if e != nil || ctx.Err() != nil {
		return "", so.ErrUnavailable
	}
	return hash, nil
}

const sponsorColumns = `d.id,d.business_id,d.city_id,d.revision,CASE WHEN d.activity_id IS NOT NULL THEN 'ACTIVITY' ELSE 'PLACE' END,COALESCE(d.activity_id,d.place_id),d.statement,d.source_url,d.rights_note,d.observed_at,d.expires_at,d.status,d.created_at,d.reviewed_at,d.review_note`

func scanSponsor(row pgx.Row) (so.Declaration, error) {
	var d so.Declaration
	e := row.Scan(&d.ID, &d.BusinessID, &d.CityID, &d.Revision, &d.TargetType, &d.TargetID, &d.Statement, &d.SourceURL, &d.RightsNote, &d.ObservedAt, &d.ExpiresAt, &d.Status, &d.CreatedAt, &d.ReviewedAt, &d.ReviewNote)
	d.ObservedAt = d.ObservedAt.UTC()
	d.ExpiresAt = d.ExpiresAt.UTC()
	d.CreatedAt = d.CreatedAt.UTC()
	if d.ReviewedAt != nil {
		t := d.ReviewedAt.UTC()
		d.ReviewedAt = &t
	}
	return d, e
}
func (s *Store) sponsorView(ctx context.Context, tx pgx.Tx, id, authority string, at time.Time) (so.Declaration, error) {
	d, e := scanSponsor(tx.QueryRow(ctx, `SELECT `+sponsorColumns+` FROM sponsored_opportunity_declarations d WHERE d.id=$1`, id))
	if e != nil {
		return d, sponsorError(e)
	}
	var src, original, hash string
	e = tx.QueryRow(ctx, `SELECT COALESCE(birdtie_sponsor_source_snapshot(d.business_id,d.submitted_by,CASE WHEN d.activity_id IS NOT NULL THEN 'ACTIVITY' ELSE 'PLACE' END,COALESCE(d.activity_id,d.place_id),$2),''),d.source_snapshot,encode(sha256(convert_to(jsonb_build_object('declaration',to_jsonb(d),'epoch',d.xmin::text,'authority',$3::text,'currentSource',COALESCE(birdtie_sponsor_source_snapshot(d.business_id,d.submitted_by,CASE WHEN d.activity_id IS NOT NULL THEN 'ACTIVITY' ELSE 'PLACE' END,COALESCE(d.activity_id,d.place_id),$2),''))::text,'UTF8')),'hex') FROM sponsored_opportunity_declarations d WHERE d.id=$1`, id, at, authority).Scan(&src, &original, &hash)
	if e != nil {
		return so.Declaration{}, so.ErrUnavailable
	}
	d.SourceCurrent = src != "" && src == original && d.ObservedAt.Compare(at) <= 0 && d.ExpiresAt.After(at)
	d.Snapshot = hash
	return d, nil
}
func sponsorAudit(ctx context.Context, tx pgx.Tx, actor, action, id string) error {
	_, e := tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,'sponsored_opportunity',$3,'allowed','human_commercial_declaration')`, actor, action, id)
	return sponsorErrorNil(e)
}
func sponsorErrorNil(e error) error {
	if e == nil {
		return nil
	}
	return sponsorError(e)
}
func (s *Store) SubmitSponsoredOpportunity(ctx context.Context, a so.Access, bid string, in so.SubmitInput) (so.Declaration, bool, error) {
	tx, e := s.beginSponsor(ctx, a, bid)
	if e != nil {
		return so.Declaration{}, false, e
	}
	defer tx.Rollback(context.Background())
	at, e := s.sponsorClock(ctx, tx)
	if e != nil {
		return so.Declaration{}, false, e
	}
	in.ObservedAt = in.ObservedAt.UTC()
	in.ExpiresAt = in.ExpiresAt.UTC()
	if so.ValidateSubmit(in, at) != nil {
		return so.Declaration{}, false, so.ErrInvalid
	}
	auth, e := s.sponsorAuthority(ctx, tx, a, bid, "", false, at)
	if e != nil {
		return so.Declaration{}, false, e
	}
	src, e := s.sponsorSource(ctx, tx, bid, a.ActorID, in.TargetType, in.TargetID, at)
	if e != nil {
		return so.Declaration{}, false, e
	}
	if src == "" || src != in.SourceSnapshot {
		return so.Declaration{}, false, so.ErrConflict
	}
	var city string
	if in.TargetType == "ACTIVITY" {
		e = tx.QueryRow(ctx, `SELECT city_id FROM activities WHERE id=$1`, in.TargetID).Scan(&city)
	} else {
		e = tx.QueryRow(ctx, `SELECT city_id FROM places WHERE id=$1`, in.TargetID).Scan(&city)
	}
	if e != nil || city != in.CityID {
		return so.Declaration{}, false, so.ErrInvalid
	}
	raw, _ := json.Marshal(in)
	hash := sha256.Sum256(raw)
	var id string
	var oldHash []byte
	e = tx.QueryRow(ctx, `SELECT id,request_hash FROM sponsored_opportunity_declarations WHERE business_id=$1 AND submitted_by=$2 AND operation_id=$3 FOR SHARE`, bid, a.ActorID, in.OperationID).Scan(&id, &oldHash)
	created := false
	if e == nil {
		if !reflect.DeepEqual(hash[:], oldHash) {
			return so.Declaration{}, false, so.ErrConflict
		}
	} else if errors.Is(e, pgx.ErrNoRows) {
		var activity, place *string
		if in.TargetType == "ACTIVITY" {
			activity = &in.TargetID
		} else {
			place = &in.TargetID
		}
		e = tx.QueryRow(ctx, `INSERT INTO sponsored_opportunity_declarations(business_id,city_id,activity_id,place_id,submitted_by,operation_id,request_hash,source_snapshot,statement,source_url,rights_note,observed_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, bid, in.CityID, activity, place, a.ActorID, in.OperationID, hash[:], src, in.Statement, in.SourceURL, in.RightsNote, in.ObservedAt, in.ExpiresAt).Scan(&id)
		if e != nil {
			return so.Declaration{}, false, sponsorError(e)
		}
		created = true
		if e = sponsorAudit(ctx, tx, a.ActorID, "sponsorship_declared", id); e != nil {
			return so.Declaration{}, false, e
		}
	} else {
		return so.Declaration{}, false, sponsorError(e)
	}
	d, e := s.sponsorView(ctx, tx, id, auth, at)
	if e != nil {
		return d, false, e
	}
	if e = s.sponsorFinish(ctx, tx, a, auth, bid, "", false, &d); e != nil {
		return so.Declaration{}, false, e
	}
	return d, created, nil
}
func (s *Store) ListOwnSponsoredOpportunities(ctx context.Context, a so.Access, bid string) (so.Management, error) {
	result := so.Management{Declarations: []so.Declaration{}, Targets: []so.EligibleTarget{}}
	tx, e := s.beginSponsor(ctx, a, bid)
	if e != nil {
		return result, e
	}
	defer tx.Rollback(context.Background())
	at, e := s.sponsorClock(ctx, tx)
	if e != nil {
		return result, e
	}
	auth, e := s.sponsorAuthority(ctx, tx, a, bid, "", false, at)
	if e != nil {
		return result, e
	}
	rows, e := tx.Query(ctx, `SELECT id FROM sponsored_opportunity_declarations WHERE business_id=$1 ORDER BY created_at DESC,id LIMIT 100`, bid)
	if e != nil {
		return result, so.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return result, so.ErrUnavailable
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, so.ErrUnavailable
	}
	for _, id := range ids {
		d, e := s.sponsorView(ctx, tx, id, auth, at)
		if e != nil {
			return result, e
		}
		result.Declarations = append(result.Declarations, d)
	}
	rows, e = tx.Query(ctx, `SELECT 'ACTIVITY',a.id,a.title,a.city_id FROM activities a JOIN activity_organizers o ON o.activity_id=a.id WHERE o.business_id=$1 AND a.publication_status='published' AND a.visibility='public' AND a.cancelled_at IS NULL AND a.ends_at>$2 UNION ALL SELECT 'PLACE',p.id,p.name,p.city_id FROM places p JOIN business_venue_relations r ON r.place_id=p.id WHERE r.business_id=$1 AND r.status='verified' AND p.publication_status='published' ORDER BY 1,2 LIMIT 100`, bid, at)
	if e != nil {
		return result, so.ErrUnavailable
	}
	targets := []so.EligibleTarget{}
	for rows.Next() {
		var t so.EligibleTarget
		if e = rows.Scan(&t.Target.Type, &t.Target.ID, &t.Target.Title, &t.CityID); e != nil {
			rows.Close()
			return result, so.ErrUnavailable
		}
		targets = append(targets, t)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, so.ErrUnavailable
	}
	for _, t := range targets {
		src, e := s.sponsorSource(ctx, tx, bid, a.ActorID, t.Target.Type, t.Target.ID, at)
		if e != nil {
			return result, e
		}
		if src != "" {
			t.SourceSnapshot = src
			result.Targets = append(result.Targets, t)
		}
	}
	if e = s.sponsorFinish(ctx, tx, a, auth, bid, "", false, nil); e != nil {
		return result, e
	}
	return result, nil
}
func (s *Store) ListSponsoredOpportunityReview(ctx context.Context, a so.Access, city string) ([]so.Declaration, error) {
	result := []so.Declaration{}
	if !so.City(city) {
		return result, so.ErrInvalid
	}
	tx, e := s.beginSponsor(ctx, a, "")
	if e != nil {
		return result, e
	}
	defer tx.Rollback(context.Background())
	at, e := s.sponsorClock(ctx, tx)
	if e != nil {
		return result, e
	}
	auth, e := s.sponsorAuthority(ctx, tx, a, "", city, true, at)
	if e != nil {
		return result, e
	}
	rows, e := tx.Query(ctx, `SELECT d.id FROM sponsored_opportunity_declarations d WHERE d.city_id=$1 AND NOT EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=d.business_id AND m.user_account_id=$2 AND m.status='active') ORDER BY d.created_at DESC,d.id LIMIT 100`, city, a.ActorID)
	if e != nil {
		return result, so.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return result, so.ErrUnavailable
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, so.ErrUnavailable
	}
	for _, id := range ids {
		d, e := s.sponsorView(ctx, tx, id, auth, at)
		if e != nil {
			return result, e
		}
		result = append(result, d)
	}
	if e = s.sponsorFinish(ctx, tx, a, auth, "", city, true, nil); e != nil {
		return nil, e
	}
	return result, nil
}
func (s *Store) ReviewSponsoredOpportunity(ctx context.Context, a so.Access, id string, in so.ReviewInput) (so.Declaration, error) {
	return s.mutateSponsor(ctx, a, id, in, false)
}
func (s *Store) RevokeSponsoredOpportunity(ctx context.Context, a so.Access, id string, in so.ReviewInput) (so.Declaration, error) {
	return s.mutateSponsor(ctx, a, id, in, true)
}
func (s *Store) mutateSponsor(ctx context.Context, a so.Access, id string, in so.ReviewInput, revoke bool) (so.Declaration, error) {
	if !so.ValidID(id) || so.ValidateReview(in, revoke) != nil || ctx == nil || s == nil || s.pool == nil {
		return so.Declaration{}, so.ErrInvalid
	}
	var bid string
	if e := s.pool.QueryRow(ctx, `SELECT business_id FROM sponsored_opportunity_declarations WHERE id=$1`, id).Scan(&bid); e != nil {
		return so.Declaration{}, so.ErrNotFound
	}
	tx, e := s.beginSponsor(ctx, a, bid)
	if e != nil {
		return so.Declaration{}, e
	}
	defer tx.Rollback(context.Background())
	d, e := scanSponsor(tx.QueryRow(ctx, `SELECT `+sponsorColumns+` FROM sponsored_opportunity_declarations d WHERE d.id=$1 FOR UPDATE`, id))
	if e != nil {
		return d, sponsorError(e)
	}
	at, e := s.sponsorClock(ctx, tx)
	if e != nil {
		return d, e
	}
	review := true
	if revoke {
		if _, e = s.sponsorAuthority(ctx, tx, a, bid, "", false, at); e == nil {
			review = false
		}
	}
	auth, e := s.sponsorAuthority(ctx, tx, a, bid, d.CityID, review, at)
	if e != nil {
		return so.Declaration{}, e
	}
	var author string
	if e = tx.QueryRow(ctx, `SELECT submitted_by FROM sponsored_opportunity_declarations WHERE id=$1`, id).Scan(&author); e != nil {
		return d, so.ErrUnavailable
	}
	if !revoke && author == a.ActorID {
		return so.Declaration{}, so.ErrForbidden
	}
	d, e = s.sponsorView(ctx, tx, id, auth, at)
	if e != nil {
		return d, e
	}
	// Exact repeat uses the original request plus the current authority epoch.
	// It cannot replay after account/grant/member restore or cross actors.
	raw, _ := json.Marshal(struct {
		Input     so.ReviewInput
		Authority string
	}{in, auth})
	hash := sha256.Sum256(raw)
	var previous []byte
	if e = tx.QueryRow(ctx, `SELECT review_request_hash FROM sponsored_opportunity_declarations WHERE id=$1`, id).Scan(&previous); e != nil {
		return d, so.ErrUnavailable
	}
	status := "rejected"
	if in.Decision == "approve" {
		status = "approved"
	}
	if revoke {
		status = "revoked"
	}
	if d.Status == status && reflect.DeepEqual(hash[:], previous) {
		var source *so.Declaration
		if status == "approved" {
			source = &d
		}
		if e = s.sponsorFinish(ctx, tx, a, auth, bid, d.CityID, review, source); e != nil {
			return so.Declaration{}, e
		}
		return d, nil
	}
	if d.Revision != in.ExpectedRevision || d.Snapshot != in.Snapshot {
		return so.Declaration{}, so.ErrConflict
	}
	if !revoke && d.Status != "pending" || revoke && d.Status != "pending" && d.Status != "approved" {
		return so.Declaration{}, so.ErrConflict
	}
	if status == "approved" && !d.SourceCurrent {
		return so.Declaration{}, so.ErrConflict
	}
	e = tx.QueryRow(ctx, `UPDATE sponsored_opportunity_declarations d SET revision=d.revision+1,status=$2,reviewed_by=$3,reviewed_at=clock_timestamp(),review_note=$4,review_generation=$5,review_request_hash=$6 WHERE id=$1 RETURNING `+sponsorColumns, id, status, a.ActorID, in.Note, auth, hash[:]).Scan(&d.ID, &d.BusinessID, &d.CityID, &d.Revision, &d.TargetType, &d.TargetID, &d.Statement, &d.SourceURL, &d.RightsNote, &d.ObservedAt, &d.ExpiresAt, &d.Status, &d.CreatedAt, &d.ReviewedAt, &d.ReviewNote)
	if e != nil {
		return so.Declaration{}, sponsorError(e)
	}
	if e = sponsorAudit(ctx, tx, a.ActorID, "sponsorship_"+status, id); e != nil {
		return so.Declaration{}, e
	}
	d, e = s.sponsorView(ctx, tx, id, auth, at)
	if e != nil {
		return d, e
	}
	var source *so.Declaration
	if status == "approved" {
		source = &d
	}
	if e = s.sponsorFinish(ctx, tx, a, auth, bid, d.CityID, review, source); e != nil {
		return so.Declaration{}, e
	}
	return d, nil
}
func (s *Store) ReadSponsoredOpportunities(ctx context.Context, a so.Access, targets []so.Target) (so.Disclosure, error) {
	out := so.Empty(true)
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return so.Empty(false), so.ErrUnavailable
	}
	if so.ValidateAccess(a, true) != nil {
		return so.Empty(false), so.ErrForbidden
	}
	if len(targets) > 200 {
		return so.Empty(false), so.ErrInvalid
	}
	seen := map[string]bool{}
	for _, t := range targets {
		if so.ValidateTarget(t) != nil || seen[t.Type+":"+t.ID] {
			return so.Empty(false), so.ErrInvalid
		}
		seen[t.Type+":"+t.ID] = true
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return so.Empty(false), so.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return so.Empty(false), so.ErrUnavailable
	}
	if a != (so.Access{}) {
		var id string
		if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.ActorID).Scan(&id); e != nil {
			return so.Empty(false), so.ErrForbidden
		}
		if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.ActorID); e != nil {
			return so.Empty(false), sponsorError(e)
		}
	}
	// Re-read current source at one SQL statement clock. No natural ranks, scores,
	// ResultSet or Pin references enter this query; only its already visible refs.
	raw, _ := json.Marshal(targets)
	rows, e := tx.Query(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() at), allowed AS(SELECT * FROM jsonb_to_recordset($1::jsonb) AS t(type text,id uuid,title text)), eligible AS (
 SELECT d.id,d.revision,d.created_at,b.id business_id,b.name,t.type,t.id target_id,t.title,d.source_url,d.observed_at,d.reviewed_at,d.expires_at,clock.at,
 row_number() OVER(PARTITION BY t.type,t.id ORDER BY d.created_at DESC,d.id) AS target_rank
 FROM allowed t JOIN sponsored_opportunity_declarations d ON (t.type='ACTIVITY' AND d.activity_id=t.id) OR (t.type='PLACE' AND d.place_id=t.id)
 JOIN businesses b ON b.id=d.business_id CROSS JOIN clock
 WHERE d.status='approved' AND d.observed_at<=clock.at AND d.expires_at>clock.at AND d.reviewed_at<=clock.at
 AND d.source_snapshot=birdtie_sponsor_source_snapshot(d.business_id,d.submitted_by,t.type,t.id,clock.at)
 AND d.review_generation=birdtie_sponsor_review_snapshot(d.city_id,d.reviewed_by,clock.at)
 AND NOT EXISTS(SELECT 1 FROM business_memberships m WHERE m.business_id=d.business_id AND m.user_account_id=d.reviewed_by AND m.status='active')
 AND ((t.type='ACTIVITY' AND EXISTS(SELECT 1 FROM activities x WHERE x.id=t.id AND x.title=t.title)) OR (t.type='PLACE' AND EXISTS(SELECT 1 FROM places x WHERE x.id=t.id AND x.name=t.title)))
 ) SELECT id,revision,business_id,name,type,target_id,title,source_url,observed_at,reviewed_at,expires_at,at FROM eligible WHERE target_rank=1 ORDER BY created_at DESC,id LIMIT 5`, raw)
	if e != nil {
		return so.Empty(false), so.ErrUnavailable
	}
	for rows.Next() {
		var p so.Public
		p.Kind = "SPONSORED"
		p.Label = "赞助"
		p.Sponsor.Type = "BUSINESS"
		if e = rows.Scan(&p.ID, &p.Revision, &p.Sponsor.ID, &p.Sponsor.Name, &p.Target.Type, &p.Target.ID, &p.Target.Title, &p.Source.URL, &p.Source.ObservedAt, &p.Source.ReviewedAt, &p.Source.ExpiresAt, &p.CheckedAt); e != nil {
			rows.Close()
			return so.Empty(false), so.ErrUnavailable
		}
		p.Source.ObservedAt = p.Source.ObservedAt.UTC()
		p.Source.ReviewedAt = p.Source.ReviewedAt.UTC()
		p.Source.ExpiresAt = p.Source.ExpiresAt.UTC()
		p.CheckedAt = p.CheckedAt.UTC()
		if so.ValidatePublic(p) != nil {
			rows.Close()
			return so.Empty(false), so.ErrUnavailable
		}
		out.SponsoredOpportunities = append(out.SponsoredOpportunities, p)
	}
	rows.Close()
	if rows.Err() != nil {
		return so.Empty(false), so.ErrUnavailable
	}
	if a != (so.Access{}) {
		if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.ActorID, s.devPhoneEnabled); e != nil {
			return so.Empty(false), sponsorError(e)
		}
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return so.Empty(false), so.ErrUnavailable
	}
	return out, nil
}
