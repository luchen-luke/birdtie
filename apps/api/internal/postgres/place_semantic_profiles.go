package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var _ pp.Store = (*Store)(nil)

type placeSemanticAccess struct{ actor, session, city, accountGeneration, editorGeneration, cityGeneration string }

func placeSemanticError(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return pp.ErrDenied
	}
	var p *pgconn.PgError
	if errors.As(e, &p) {
		if p.Code == "23505" {
			return pp.ErrConflict
		}
		if p.Code == "23514" || p.Code == "22003" || p.Code == "22P02" {
			return pp.ErrInvalid
		}
	}
	return pp.ErrUnavailable
}
func (s *Store) beginPlaceSemantic(ctx context.Context, a pp.Access, cityID string, reviewer bool) (pgx.Tx, placeSemanticAccess, error) {
	var b placeSemanticAccess
	if ctx == nil || s == nil || s.pool == nil || ctx.Err() != nil {
		return nil, b, pp.ErrUnavailable
	}
	if pp.ValidateAccess(a) != nil || len(cityID) < 1 || len(cityID) > 80 {
		return nil, b, pp.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, b, pp.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, placeSemanticAccess, error) {
		tx.Rollback(context.Background())
		return nil, placeSemanticAccess{}, e
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(pp.ErrUnavailable)
	}
	e = tx.QueryRow(ctx, `SELECT id,xmin::text FROM accounts WHERE id=$1 AND account_type=$2 AND status='active' FOR SHARE`, a.ActorID, a.AccountType).Scan(&b.actor, &b.accountGeneration)
	if e != nil {
		return fail(placeSemanticError(e))
	}
	e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE account_id=$1 AND token_sha256=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, b.actor, a.SessionDigest[:], s.devPhoneEnabled).Scan(&b.session)
	if e != nil {
		return fail(placeSemanticError(e))
	}
	e = tx.QueryRow(ctx, `SELECT xmin::text FROM city_editor_memberships WHERE city_id=$1 AND account_id=$2 AND state='active' AND role IN ('contributor','reviewer') AND (NOT $3::boolean OR role='reviewer') AND created_at<=clock_timestamp() FOR SHARE`, cityID, b.actor, reviewer).Scan(&b.editorGeneration)
	if e != nil {
		return fail(placeSemanticError(e))
	}
	e = tx.QueryRow(ctx, `SELECT id,xmin::text FROM cities WHERE id=$1 AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND updated_at<=clock_timestamp() FOR SHARE`, cityID).Scan(&b.city, &b.cityGeneration)
	if e != nil {
		return fail(placeSemanticError(e))
	}
	return tx, b, nil
}
func (s *Store) finishPlaceSemantic(ctx context.Context, tx pgx.Tx, a pp.Access, b placeSemanticAccess, placeID string, candidateIDs ...string) (time.Time, error) {
	var at time.Time
	var alive bool
	e := tx.QueryRow(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() at) SELECT at,EXISTS(SELECT 1 FROM accounts ac JOIN sessions se ON se.account_id=ac.id JOIN city_editor_memberships m ON m.account_id=ac.id JOIN cities city ON city.id=m.city_id CROSS JOIN clock cl WHERE ac.id=$1 AND ac.account_type=$2 AND ac.status='active' AND ac.xmin::text=$3 AND se.id=$4 AND se.token_sha256=$5 AND se.revoked_at IS NULL AND se.expires_at>cl.at AND se.idle_expires_at>cl.at AND ($6::boolean OR se.authentication_method<>'dev_phone') AND m.city_id=$7 AND m.state='active' AND m.xmin::text=$8 AND city.publication_status='published' AND city.xmin::text=$9 AND (city.expires_at IS NULL OR city.expires_at>cl.at) AND ($10::text='' OR EXISTS(SELECT 1 FROM places p WHERE p.id=NULLIF($10,'')::uuid AND p.city_id=city.id AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>cl.at)))) AND NOT EXISTS(SELECT 1 FROM unnest($11::uuid[]) source_id WHERE NOT EXISTS(SELECT 1 FROM place_semantic_candidates c JOIN accounts a ON a.id=c.submitted_by JOIN city_editor_memberships m ON m.city_id=c.city_id AND m.account_id=c.submitted_by JOIN cities city ON city.id=c.city_id JOIN places p ON p.id=c.place_id AND p.city_id=c.city_id WHERE c.id=source_id AND `+placeSemanticOriginPredicate+`)) FROM clock`, b.actor, a.AccountType, b.accountGeneration, b.session, a.SessionDigest[:], s.devPhoneEnabled, b.city, b.editorGeneration, b.cityGeneration, placeID, candidateIDs).Scan(&at, &alive)
	if e != nil || ctx.Err() != nil {
		return time.Time{}, pp.ErrUnavailable
	}
	if !alive {
		return time.Time{}, pp.ErrDenied
	}
	return at.UTC(), nil
}
func lockPlaceSemantic(ctx context.Context, tx pgx.Tx, cityID, placeID string) (string, error) {
	var generation string
	e := tx.QueryRow(ctx, `SELECT xmin::text FROM places WHERE id=$1 AND city_id=$2 AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND updated_at<=clock_timestamp() FOR SHARE`, placeID, cityID).Scan(&generation)
	if e != nil {
		return "", placeSemanticError(e)
	}
	return generation, nil
}
func placeSemanticSlot(ctx context.Context, tx pgx.Tx, placeID string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('place_semantic:'||$1,0))`, placeID)
	if e != nil {
		return pp.ErrUnavailable
	}
	return nil
}
func currentPlaceSemanticVersion(ctx context.Context, tx pgx.Tx, placeID string) (int64, error) {
	var v int64
	e := tx.QueryRow(ctx, `SELECT version FROM place_semantic_profiles WHERE place_id=$1 FOR UPDATE`, placeID).Scan(&v)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, nil
	}
	if e != nil {
		return 0, pp.ErrUnavailable
	}
	return v, nil
}

const placeSemanticCandidateColumns = `id,place_id,city_id,version,expected_version,facts,source_label,source_url,rights_note,observed_at,expires_at,confidence_kind,confidence_level,status,created_at,reviewed_at`

func scanPlaceSemanticCandidate(row scanner) (pp.Candidate, error) {
	var c pp.Candidate
	var raw []byte
	e := row.Scan(&c.ID, &c.PlaceID, &c.CityID, &c.Version, &c.ExpectedVersion, &raw, &c.Source.Label, &c.Source.URL, &c.Source.RightsNote, &c.Source.ObservedAt, &c.Source.ExpiresAt, &c.Confidence.Kind, &c.Confidence.Level, &c.Status, &c.CreatedAt, &c.ReviewedAt)
	if e != nil {
		return c, e
	}
	if json.Unmarshal(raw, &c.Facts) != nil || pp.ValidateFacts(c.Facts) != nil || pp.ValidateAssessment(c.Confidence) != nil {
		return pp.Candidate{}, pp.ErrUnavailable
	}
	return c, nil
}

const placeSemanticOriginPredicate = `a.status='active' AND a.xmin::text=c.submitter_generation AND m.state='active' AND m.xmin::text=c.editor_generation AND city.publication_status='published' AND city.xmin::text=c.city_generation AND (city.expires_at IS NULL OR city.expires_at>clock.at) AND p.publication_status='published' AND p.xmin::text=c.place_generation AND (p.expires_at IS NULL OR p.expires_at>clock.at) AND c.observed_at<=clock.at AND c.expires_at>clock.at`

func placeSemanticOriginAt(ctx context.Context, tx pgx.Tx, id string) (time.Time, bool, error) {
	var at time.Time
	var valid bool
	e := tx.QueryRow(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() at) SELECT clock.at,EXISTS(SELECT 1 FROM place_semantic_candidates c JOIN accounts a ON a.id=c.submitted_by JOIN city_editor_memberships m ON m.city_id=c.city_id AND m.account_id=c.submitted_by JOIN cities city ON city.id=c.city_id JOIN places p ON p.id=c.place_id AND p.city_id=c.city_id WHERE c.id=$1 AND `+placeSemanticOriginPredicate+`) FROM clock`, id).Scan(&at, &valid)
	if e != nil || ctx.Err() != nil {
		return time.Time{}, false, pp.ErrUnavailable
	}
	return at.UTC(), valid, nil
}
func placeSemanticOriginCurrent(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	_, ok, e := placeSemanticOriginAt(ctx, tx, id)
	return ok, e
}
func placeSemanticAudit(ctx context.Context, tx pgx.Tx, actor, action, kind, id string) error {
	_, e := tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,$3,$4,'allowed','public_place_semantics')`, actor, action, kind, id)
	if e != nil {
		return pp.ErrUnavailable
	}
	return nil
}

func (s *Store) SubmitPlaceSemanticCandidate(ctx context.Context, a pp.Access, cityID, placeID string, in pp.SubmitInput) (pp.Candidate, bool, error) {
	if !pp.ValidID(placeID) {
		return pp.Candidate{}, false, pp.ErrInvalid
	}
	tx, b, e := s.beginPlaceSemantic(ctx, a, cityID, false)
	if e != nil {
		return pp.Candidate{}, false, e
	}
	defer tx.Rollback(context.Background())
	placeGeneration, e := lockPlaceSemantic(ctx, tx, cityID, placeID)
	if e != nil {
		return pp.Candidate{}, false, e
	}
	if e = placeSemanticSlot(ctx, tx, placeID); e != nil {
		return pp.Candidate{}, false, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return pp.Candidate{}, false, pp.ErrUnavailable
	}
	in.Source.ObservedAt = in.Source.ObservedAt.UTC()
	in.Source.ExpiresAt = in.Source.ExpiresAt.UTC()
	if pp.ValidateSubmit(in, now) != nil {
		return pp.Candidate{}, false, pp.ErrInvalid
	}
	encoded, _ := json.Marshal(in)
	hash := sha256.Sum256(encoded)
	var oldID string
	var oldHash []byte
	e = tx.QueryRow(ctx, `SELECT id,request_hash FROM place_semantic_candidates WHERE city_id=$1 AND submitted_by=$2 AND operation_id=$3`, cityID, b.actor, in.OperationID).Scan(&oldID, &oldHash)
	if e == nil {
		if string(oldHash) != string(hash[:]) {
			return pp.Candidate{}, false, pp.ErrConflict
		}
		valid, e := placeSemanticOriginCurrent(ctx, tx, oldID)
		if e != nil {
			return pp.Candidate{}, false, e
		}
		if !valid {
			return pp.Candidate{}, false, pp.ErrDenied
		}
		c, e := scanPlaceSemanticCandidate(tx.QueryRow(ctx, `SELECT `+placeSemanticCandidateColumns+` FROM place_semantic_candidates WHERE id=$1 FOR SHARE`, oldID))
		if e != nil {
			return pp.Candidate{}, false, placeSemanticError(e)
		}
		if _, e = s.finishPlaceSemantic(ctx, tx, a, b, placeID, oldID); e != nil {
			return pp.Candidate{}, false, e
		}
		if e = tx.Commit(ctx); e != nil {
			return pp.Candidate{}, false, pp.ErrUnavailable
		}
		return c, false, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return pp.Candidate{}, false, pp.ErrUnavailable
	}
	v, e := currentPlaceSemanticVersion(ctx, tx, placeID)
	if e != nil {
		return pp.Candidate{}, false, e
	}
	if v != in.ExpectedVersion {
		return pp.Candidate{}, false, pp.ErrConflict
	}
	raw, e := pp.CanonicalFacts(in.Facts)
	if e != nil {
		return pp.Candidate{}, false, e
	}
	c, e := scanPlaceSemanticCandidate(tx.QueryRow(ctx, `INSERT INTO place_semantic_candidates(place_id,city_id,submitted_by,operation_id,request_hash,submitter_generation,editor_generation,place_generation,city_generation,expected_version,facts,source_label,source_url,rights_note,observed_at,expires_at,confidence_kind,confidence_level) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING `+placeSemanticCandidateColumns, placeID, cityID, b.actor, in.OperationID, hash[:], b.accountGeneration, b.editorGeneration, placeGeneration, b.cityGeneration, in.ExpectedVersion, raw, in.Source.Label, in.Source.URL, in.Source.RightsNote, in.Source.ObservedAt, in.Source.ExpiresAt, in.Confidence.Kind, in.Confidence.Level))
	if e != nil {
		return pp.Candidate{}, false, placeSemanticError(e)
	}
	if e = placeSemanticAudit(ctx, tx, b.actor, "place_semantic_submitted", "place_semantic_candidate", c.ID); e != nil {
		return pp.Candidate{}, false, e
	}
	final, e := s.finishPlaceSemantic(ctx, tx, a, b, placeID, c.ID)
	if e != nil {
		return pp.Candidate{}, false, e
	}
	if !c.Source.ExpiresAt.After(final) {
		return pp.Candidate{}, false, pp.ErrDenied
	}
	if e = tx.Commit(ctx); e != nil {
		return pp.Candidate{}, false, pp.ErrUnavailable
	}
	return c, true, nil
}
func (s *Store) ListPlaceSemanticCandidates(ctx context.Context, a pp.Access, cityID string) ([]pp.Candidate, error) {
	tx, b, e := s.beginPlaceSemantic(ctx, a, cityID, true)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	rows, e := tx.Query(ctx, `SELECT `+placeSemanticCandidateColumns+` FROM place_semantic_candidates WHERE city_id=$1 AND status='pending' AND expires_at>clock_timestamp() ORDER BY created_at,id LIMIT 100 FOR SHARE`, cityID)
	if e != nil {
		return nil, pp.ErrUnavailable
	}
	out := []pp.Candidate{}
	for rows.Next() {
		c, e := scanPlaceSemanticCandidate(rows)
		if e != nil {
			rows.Close()
			return nil, pp.ErrUnavailable
		}
		out = append(out, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, pp.ErrUnavailable
	}
	filtered := []pp.Candidate{}
	for _, c := range out {
		ok, e := placeSemanticOriginCurrent(ctx, tx, c.ID)
		if e != nil {
			return nil, e
		}
		if ok {
			filtered = append(filtered, c)
		}
	}
	ids := make([]string, 0, len(filtered))
	for _, c := range filtered {
		ids = append(ids, c.ID)
	}
	final, e := s.finishPlaceSemantic(ctx, tx, a, b, "", ids...)
	if e != nil {
		return nil, e
	}
	for _, c := range filtered {
		if !c.Source.ExpiresAt.After(final) {
			return nil, pp.ErrDenied
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, pp.ErrUnavailable
	}
	return filtered, nil
}
func (s *Store) ReviewPlaceSemanticCandidate(ctx context.Context, a pp.Access, id string, in pp.ReviewInput) (pp.Candidate, error) {
	if ctx == nil || s == nil || s.pool == nil {
		return pp.Candidate{}, pp.ErrUnavailable
	}
	if !pp.ValidID(id) || pp.ValidateReview(in) != nil || pp.ValidateAccess(a) != nil {
		return pp.Candidate{}, pp.ErrInvalid
	}
	var cityID, placeID string
	e := s.pool.QueryRow(ctx, `SELECT c.city_id,c.place_id FROM place_semantic_candidates c JOIN city_editor_memberships m ON m.city_id=c.city_id WHERE c.id=$1 AND m.account_id=$2 AND m.role='reviewer' AND m.state='active'`, id, a.ActorID).Scan(&cityID, &placeID)
	if e != nil {
		return pp.Candidate{}, placeSemanticError(e)
	}
	tx, b, e := s.beginPlaceSemantic(ctx, a, cityID, true)
	if e != nil {
		return pp.Candidate{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = lockPlaceSemantic(ctx, tx, cityID, placeID); e != nil {
		return pp.Candidate{}, e
	}
	if e = placeSemanticSlot(ctx, tx, placeID); e != nil {
		return pp.Candidate{}, e
	}
	c, e := scanPlaceSemanticCandidate(tx.QueryRow(ctx, `SELECT `+placeSemanticCandidateColumns+` FROM place_semantic_candidates WHERE id=$1 FOR UPDATE`, id))
	if e != nil {
		return pp.Candidate{}, placeSemanticError(e)
	}
	var submitter string
	if e = tx.QueryRow(ctx, `SELECT submitted_by FROM place_semantic_candidates WHERE id=$1`, id).Scan(&submitter); e != nil {
		return pp.Candidate{}, pp.ErrUnavailable
	}
	if c.Status != "pending" || submitter == b.actor || c.Version != in.CandidateVersion || c.ExpectedVersion != in.ExpectedVersion {
		return pp.Candidate{}, pp.ErrConflict
	}
	ok, e := placeSemanticOriginCurrent(ctx, tx, id)
	if e != nil {
		return pp.Candidate{}, e
	}
	if !ok {
		return pp.Candidate{}, pp.ErrDenied
	}
	v, e := currentPlaceSemanticVersion(ctx, tx, placeID)
	if e != nil {
		return pp.Candidate{}, e
	}
	if v != in.ExpectedVersion {
		return pp.Candidate{}, pp.ErrConflict
	}
	status := "approved"
	if in.Decision == "reject" {
		status = "rejected"
	}
	c, e = scanPlaceSemanticCandidate(tx.QueryRow(ctx, `UPDATE place_semantic_candidates SET status=$2,reviewed_by=$3,reviewed_at=clock_timestamp(),review_note=$4 WHERE id=$1 RETURNING `+placeSemanticCandidateColumns, id, status, b.actor, in.Note))
	if e != nil {
		return pp.Candidate{}, placeSemanticError(e)
	}
	if status == "approved" {
		_, e = tx.Exec(ctx, `INSERT INTO place_semantic_profiles(place_id,city_id,version,source_candidate_id,state) VALUES($1,$2,$3,$4,'published') ON CONFLICT(place_id) DO UPDATE SET version=EXCLUDED.version,source_candidate_id=EXCLUDED.source_candidate_id,state='published',updated_at=clock_timestamp(),withdrawn_by=NULL,withdraw_note=NULL`, placeID, cityID, v+1, id)
		if e != nil {
			return pp.Candidate{}, placeSemanticError(e)
		}
	}
	if e = placeSemanticAudit(ctx, tx, b.actor, "place_semantic_"+status, "place_semantic_candidate", id); e != nil {
		return pp.Candidate{}, e
	}
	final, e := s.finishPlaceSemantic(ctx, tx, a, b, placeID, id)
	if e != nil {
		return pp.Candidate{}, e
	}
	if !c.Source.ExpiresAt.After(final) {
		return pp.Candidate{}, pp.ErrDenied
	}
	if e = tx.Commit(ctx); e != nil {
		return pp.Candidate{}, pp.ErrUnavailable
	}
	return c, nil
}
func (s *Store) WithdrawPlaceSemanticProfile(ctx context.Context, a pp.Access, id string, in pp.WithdrawInput) (pp.Revision, error) {
	if ctx == nil || s == nil || s.pool == nil {
		return pp.Revision{}, pp.ErrUnavailable
	}
	if !pp.ValidID(id) || pp.ValidateWithdraw(in) != nil {
		return pp.Revision{}, pp.ErrInvalid
	}
	var city string
	if e := s.pool.QueryRow(ctx, `SELECT city_id FROM places WHERE id=$1`, id).Scan(&city); e != nil {
		return pp.Revision{}, placeSemanticError(e)
	}
	tx, b, e := s.beginPlaceSemantic(ctx, a, city, true)
	if e != nil {
		return pp.Revision{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = lockPlaceSemantic(ctx, tx, city, id); e != nil {
		return pp.Revision{}, e
	}
	if e = placeSemanticSlot(ctx, tx, id); e != nil {
		return pp.Revision{}, e
	}
	var out pp.Revision
	e = tx.QueryRow(ctx, `UPDATE place_semantic_profiles SET state='withdrawn',version=version+1,updated_at=clock_timestamp(),withdrawn_by=$2,withdraw_note=$3 WHERE place_id=$1 AND version=$4 AND state='published' RETURNING place_id,version,state`, id, b.actor, in.Note, in.ExpectedVersion).Scan(&out.PlaceID, &out.Version, &out.State)
	if errors.Is(e, pgx.ErrNoRows) {
		return pp.Revision{}, pp.ErrConflict
	}
	if e != nil {
		return pp.Revision{}, placeSemanticError(e)
	}
	if e = placeSemanticAudit(ctx, tx, b.actor, "place_semantic_withdrawn", "place", id); e != nil {
		return pp.Revision{}, e
	}
	if _, e = s.finishPlaceSemantic(ctx, tx, a, b, id); e != nil {
		return pp.Revision{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return pp.Revision{}, pp.ErrUnavailable
	}
	return out, nil
}
func readPlaceSemanticPublic(ctx context.Context, tx pgx.Tx, id string) (pp.Public, error) {
	var p pp.Public
	var raw []byte
	p.SchemaVersion = pp.SchemaVersion
	e := tx.QueryRow(ctx, `SELECT pr.place_id,pr.city_id,pr.version,c.facts,c.source_label,c.source_url,c.observed_at,c.reviewed_at,c.expires_at,c.confidence_kind,c.confidence_level FROM place_semantic_profiles pr JOIN place_semantic_candidates c ON c.id=pr.source_candidate_id AND c.place_id=pr.place_id AND c.city_id=pr.city_id JOIN places pl ON pl.id=pr.place_id AND pl.city_id=pr.city_id JOIN cities city ON city.id=pr.city_id JOIN accounts ac ON ac.id=c.submitted_by JOIN city_editor_memberships m ON m.city_id=c.city_id AND m.account_id=ac.id WHERE pr.place_id=$1 AND pr.state='published' AND c.status='approved' AND c.expires_at>clock_timestamp() AND c.observed_at<=c.reviewed_at AND c.reviewed_at<=clock_timestamp() AND pl.publication_status='published' AND pl.xmin::text=c.place_generation AND (pl.expires_at IS NULL OR pl.expires_at>clock_timestamp()) AND city.publication_status='published' AND city.xmin::text=c.city_generation AND (city.expires_at IS NULL OR city.expires_at>clock_timestamp()) AND ac.status='active' AND ac.xmin::text=c.submitter_generation AND m.state='active' AND m.xmin::text=c.editor_generation FOR SHARE OF pr,c,pl,city`, id).Scan(&p.PlaceID, &p.CityID, &p.Version, &raw, &p.Source.Label, &p.Source.URL, &p.Source.ObservedAt, &p.Source.ReviewedAt, &p.Source.ExpiresAt, &p.Confidence.Kind, &p.Confidence.Level)
	if errors.Is(e, pgx.ErrNoRows) {
		return pp.Public{}, pp.ErrNotFound
	}
	if e != nil || json.Unmarshal(raw, &p.Facts) != nil {
		return pp.Public{}, pp.ErrUnavailable
	}
	return p, nil
}
func (s *Store) GetPublicPlaceSemanticProfile(ctx context.Context, id string) (pp.Public, error) {
	if ctx == nil || s == nil || s.pool == nil || ctx.Err() != nil {
		return pp.Public{}, pp.ErrUnavailable
	}
	if !pp.ValidID(id) {
		return pp.Public{}, pp.ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return pp.Public{}, pp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	p, e := readPlaceSemanticPublic(ctx, tx, id)
	if e != nil {
		return pp.Public{}, e
	}
	var candidate string
	e = tx.QueryRow(ctx, `SELECT source_candidate_id FROM place_semantic_profiles WHERE place_id=$1`, id).Scan(&candidate)
	if e != nil {
		return pp.Public{}, pp.ErrUnavailable
	}
	now, ok, e := placeSemanticOriginAt(ctx, tx, candidate)
	if e != nil {
		return pp.Public{}, e
	}
	if !ok || pp.ValidatePublic(p, now) != nil {
		return pp.Public{}, pp.ErrNotFound
	}
	p.CheckedAt = now.UTC()
	if ctx.Err() != nil {
		return pp.Public{}, pp.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return pp.Public{}, pp.ErrUnavailable
	}
	return p, nil
}
