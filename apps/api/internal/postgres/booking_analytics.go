package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	ba "github.com/birdtie/birdtie/apps/api/internal/bookinganalytics"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

var _ venue.CurrentStore = (*Store)(nil)
var _ ba.Store = (*Store)(nil)

// Trusted internal maintenance only, never a public route. Bounded deletion
// enforces the declared 30-day policy when invoked by an operator/scheduler.
// No deployed scheduler is asserted by this repository implementation.
func (s *Store) PruneExternalBookingEvents(ctx context.Context) (int64, error) {
	tag, e := s.pool.Exec(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at),old AS(SELECT event_id FROM booking_external_events,stamp WHERE recorded_at<stamp.at-interval '30 days' ORDER BY recorded_at,event_id LIMIT 1000 FOR UPDATE SKIP LOCKED) DELETE FROM booking_external_events WHERE event_id IN(SELECT event_id FROM old)`)
	if e != nil {
		return 0, venue.ErrUnavailable
	}
	return tag.RowsAffected(), nil
}

var bookingReceiptKey = func() []byte {
	v := make([]byte, 32)
	if _, e := rand.Read(v); e != nil {
		panic("booking receipt key unavailable")
	}
	return v
}()

const bookingRelations = `LOCK TABLE venues,venue_candidates,places,cities,organizations,business_venue_relations,businesses,accounts,sessions IN ACCESS SHARE MODE`

// Exactly the original public 040/041 projection, with stricter native current
// City/Candidate expiry. 070 private management URLs are never queried. One
// statement captures payload, all selected source row versions, session and PG
// wall-clock after table/session waits. Source proof is server-only.
const bookingCurrentSQL = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at),
 bs AS MATERIALIZED(SELECT b.id,b.name,b.xmin::text bx,r.place_id rid,r.xmin::text rx,a.id aid,a.xmin::text ax
 FROM business_venue_relations r JOIN businesses b ON b.id=r.business_id AND b.status='active' AND b.claim_status='verified'
 JOIN accounts a ON a.id=b.account_id AND a.account_type='business' AND a.status='active'
 WHERE r.place_id=$1 AND r.status='verified' AND birdtie_verified_business_venue(b.id,$1) ORDER BY b.name,b.id LIMIT 20),
 src AS MATERIALIZED(SELECT jsonb_build_object('placeId',v.place_id,'cityId',v.city_id,'capacity',v.capacity,
 'reservationSupport',v.reservation_support,'reservationUrl',v.reservation_url,'suitability',v.suitability,'amenities',v.amenities,
 'operatorOrganizationId',CASE WHEN o.visibility='public' THEN o.id END,'operatorOrganizationName',CASE WHEN o.visibility='public' THEN o.name ELSE '' END,
 'businesses',(SELECT coalesce(jsonb_agg(jsonb_build_object('id',id,'name',name) ORDER BY name,id),'[]') FROM bs),
 'sourceUrl',v.source_url,'reviewedAt',v.reviewed_at,'expiresAt',v.expires_at) payload,
 jsonb_build_object('venue',v.xmin::text,'candidate',vc.xmin::text,'place',p.xmin::text,'city',c.xmin::text,'operator',o.xmin::text,
 'businesses',(SELECT coalesce(jsonb_agg(jsonb_build_array(id,bx,rid,rx,aid,ax) ORDER BY id,rid),'[]') FROM bs)) versions,
 LEAST(v.expires_at,vc.expires_at,p.expires_at,c.expires_at) source_until
 FROM venues v JOIN venue_candidates vc ON vc.id=v.source_candidate_id
 JOIN places p ON p.id=v.place_id AND p.city_id=v.city_id JOIN cities c ON c.id=v.city_id
 LEFT JOIN organizations o ON o.id=v.operator_organization_id CROSS JOIN cl
 WHERE v.place_id=$1 AND v.expires_at>cl.at AND vc.status='approved' AND vc.expires_at>cl.at
 AND vc.place_id=v.place_id AND vc.city_id=v.city_id AND vc.reviewed_by=v.reviewed_by
 AND v.reviewed_at<=cl.at AND vc.reviewed_at<=cl.at
 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>cl.at)
 AND c.publication_status='published' AND (c.expires_at IS NULL OR c.expires_at>cl.at)
 AND (v.operator_organization_id IS NULL OR o.status='active')),
 auth AS MATERIALIZED(SELECT s.expires_at,s.idle_expires_at,
 jsonb_build_object('session',s.id,'created',s.created_at AT TIME ZONE 'UTC','absolute',s.expires_at AT TIME ZONE 'UTC',
 'method',s.authentication_method,'account',a.id,'accountVersion',a.xmin::text) binding
 FROM sessions s JOIN accounts a ON a.id=s.account_id CROSS JOIN cl
 WHERE a.id=$2 AND a.account_type=$5 AND a.status='active' AND s.token_sha256=$3 AND s.revoked_at IS NULL
 AND s.expires_at>cl.at AND s.idle_expires_at>cl.at AND ($4::boolean OR s.authentication_method<>'dev_phone'))
 SELECT cl.at,($2::uuid IS NULL OR EXISTS(SELECT 1 FROM auth)),(SELECT payload FROM src),
 (SELECT encode(sha256(convert_to((payload||versions||jsonb_build_object('authority',(SELECT binding FROM auth)))::text,'UTF8')),'hex') FROM src),
 LEAST(cl.at+interval '30 seconds',(SELECT source_until FROM src),(SELECT expires_at FROM auth),(SELECT idle_expires_at FROM auth)) FROM cl`

func bookingAccessValid(a venue.PublicAccess) bool {
	if a.Actor.ID == "" {
		return a.Actor.AccountType == "" && a.SessionDigest == ([32]byte{})
	}
	return validHumanMomentID(a.Actor.ID) && a.SessionDigest != ([32]byte{}) && (a.Actor.AccountType == "person" || a.Actor.AccountType == "business" || a.Actor.AccountType == "organization")
}
func (s *Store) beginBookingTx(ctx context.Context, a venue.PublicAccess) (pgx.Tx, error) {
	if !bookingAccessValid(a) {
		return nil, identity.ErrUnauthorized
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, venue.ErrUnavailable
	}
	fail := func(err error) (pgx.Tx, error) { _ = tx.Rollback(context.Background()); return nil, err }
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(venue.ErrUnavailable)
	}
	if _, e = tx.Exec(ctx, bookingRelations); e != nil {
		return fail(venue.ErrUnavailable)
	}
	if a.Actor.ID != "" {
		var id string
		if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type=$2 AND status='active' FOR SHARE`, a.Actor.ID, a.Actor.AccountType).Scan(&id); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return fail(identity.ErrUnauthorized)
			}
			return fail(venue.ErrUnavailable)
		}
		if e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2 FOR SHARE`, a.SessionDigest[:], a.Actor.ID).Scan(&id); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return fail(identity.ErrUnauthorized)
			}
			return fail(venue.ErrUnavailable)
		}
	}
	return tx, nil
}
func (s *Store) captureBooking(ctx context.Context, tx pgx.Tx, a venue.PublicAccess, id string) (venue.CurrentReceipt, error) {
	var r venue.CurrentReceipt
	var owner any
	if a.Actor.ID != "" {
		owner = a.Actor.ID
	}
	var live bool
	var raw []byte
	var proof *string
	e := tx.QueryRow(ctx, bookingCurrentSQL, id, owner, a.SessionDigest[:], s.devPhoneEnabled, a.Actor.AccountType).Scan(&r.View.BookingObservedAt, &live, &raw, &proof, &r.View.BookingValidUntil)
	if e != nil || ctx.Err() != nil {
		return r, venue.ErrUnavailable
	}
	if !live {
		return venue.CurrentReceipt{}, identity.ErrUnauthorized
	}
	if raw == nil || proof == nil {
		return venue.CurrentReceipt{}, venue.ErrNotFound
	}
	now, until := r.View.BookingObservedAt, r.View.BookingValidUntil
	if json.Unmarshal(raw, &r.View) != nil {
		return venue.CurrentReceipt{}, venue.ErrUnavailable
	}
	r.View.BookingObservedAt = now.UTC()
	r.View.BookingValidUntil = until.UTC()
	r.View.BookingSourceRevision = *proof
	r.View.BookingSourceVersion = bookingVersion(a, id, *proof, until)
	r.Proof = *proof
	r.View.ReviewedAt = r.View.ReviewedAt.UTC()
	r.View.ExpiresAt = r.View.ExpiresAt.UTC()
	r.Seal = bookingSeal(a, id, r)
	return r, nil
}
func bookingSeal(a venue.PublicAccess, id string, r venue.CurrentReceipt) string {
	r.Seal = ""
	data, _ := json.Marshal(struct {
		Domain  string
		Access  venue.PublicAccess
		ID      string
		Receipt venue.CurrentReceipt
	}{"public-venue-booking-v1", a, id, r})
	h := hmac.New(sha256.New, bookingReceiptKey)
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Store) ReadCurrentPublicVenue(ctx context.Context, a venue.PublicAccess, id string) (venue.CurrentReceipt, error) {
	if !validHumanMomentID(id) {
		return venue.CurrentReceipt{}, venue.ErrNotFound
	}
	tx, e := s.beginBookingTx(ctx, a)
	if e != nil {
		return venue.CurrentReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := s.captureBooking(ctx, tx, a, id)
	if e != nil {
		return venue.CurrentReceipt{}, e
	}
	if tx.Commit(ctx) != nil {
		return venue.CurrentReceipt{}, venue.ErrUnavailable
	}
	return r, nil
}
func (s *Store) RevalidateCurrentPublicVenue(ctx context.Context, a venue.PublicAccess, id string, r venue.CurrentReceipt) error {
	if !bookingAccessValid(a) || r.View.PlaceID != id || r.Proof == "" || r.Seal == "" || !hmac.Equal([]byte(r.Seal), []byte(bookingSeal(a, id, r))) {
		return venue.ErrChanged
	}
	tx, e := s.beginBookingTx(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	current, e := s.captureBooking(ctx, tx, a, id)
	if e != nil {
		return e
	}
	if current.Proof != r.Proof || !r.View.BookingValidUntil.After(current.View.BookingObservedAt) {
		return venue.ErrChanged
	}
	if tx.Commit(ctx) != nil {
		return venue.ErrUnavailable
	}
	return nil
}
func (s *Store) RecordExternalBookingEvent(ctx context.Context, a venue.PublicAccess, id string, in ba.Input) (ba.Result, error) {
	if !in.Valid(id) {
		return ba.Result{}, ba.ErrInvalid
	}
	tx, e := s.beginBookingTx(ctx, a)
	if e != nil {
		return ba.Result{}, e
	}
	defer tx.Rollback(context.Background())
	check := func() (venue.CurrentReceipt, error) {
		r, e := s.captureBooking(ctx, tx, a, id)
		if e != nil {
			return r, e
		}
		if r.View.ReservationSupport != "external_url" || r.View.ReservationURL == nil || !hmac.Equal([]byte(bookingVersion(a, id, r.Proof, in.ValidUntil)), []byte(in.SourceVersion)) || !in.ValidUntil.After(r.View.BookingObservedAt) || in.ValidUntil.After(r.View.BookingObservedAt.Add(30*time.Second)) || in.ValidUntil.After(r.View.BookingValidUntil) {
			return venue.CurrentReceipt{}, venue.ErrChanged
		}
		return r, nil
	}
	if _, e = check(); e != nil {
		return ba.Result{}, e
	}
	var at time.Time
	e = tx.QueryRow(ctx, `INSERT INTO booking_external_events(event_id,place_id,event_type,outcome) VALUES($1,$2,$3,$4) ON CONFLICT(event_id) DO NOTHING RETURNING recorded_at`, in.EventID, id, in.EventType, in.Outcome).Scan(&at)
	if errors.Is(e, pgx.ErrNoRows) {
		var place, kind, outcome string
		e = tx.QueryRow(ctx, `SELECT place_id,event_type,outcome,recorded_at FROM booking_external_events WHERE event_id=$1`, in.EventID).Scan(&place, &kind, &outcome, &at)
		if e == nil && !reflect.DeepEqual([]string{place, kind, outcome}, []string{id, in.EventType, in.Outcome}) {
			return ba.Result{}, venue.ErrChanged
		}
	}
	if e != nil {
		return ba.Result{}, venue.ErrUnavailable
	}
	// Includes ON CONFLICT/FK and current-session waits. No source/auth read or
	// SQL side effect follows the final same-clock authority/source comparison.
	if _, e = check(); e != nil {
		return ba.Result{}, e
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return ba.Result{}, venue.ErrUnavailable
	}
	return ba.Report(in.EventID, id, at), nil
}

// Stateless concurrency binding, not a permission. Every use resolves native
// current source and actor; the exact original deadline cannot be substituted.
func bookingVersion(a venue.PublicAccess, id, revision string, until time.Time) string {
	data, _ := json.Marshal(struct {
		Domain       string
		Access       venue.PublicAccess
		ID, Revision string
		Until        time.Time
	}{"public-booking-deadline-v1", a, id, revision, until.UTC()})
	h := hmac.New(sha256.New, bookingReceiptKey)
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
