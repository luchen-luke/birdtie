package postgres

import (
	"context"
	"errors"
	ba "github.com/birdtie/birdtie/apps/api/internal/bookinganalytics"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

func bookingInput(t *testing.T, f *placeMemoryFixture, r venue.CurrentReceipt) ba.Input {
	t.Helper()
	return ba.Input{EventID: agentMemoryID(t, f.private), EventType: ba.EventType, Outcome: ba.Outcome, SourceVersion: r.View.BookingSourceVersion, ValidUntil: r.View.BookingValidUntil}
}
func TestBookingAnalyticsNativeReportIdempotentAndNoConfirmedTransaction(t *testing.T) {
	f := bookingVenueNative(t)
	b := f.private.base
	r, e := b.store.ReadCurrentPublicVenue(b.ctx, venue.PublicAccess{}, f.place)
	if e != nil {
		t.Fatal(e)
	}
	in := bookingInput(t, f, r)
	one, e := b.store.RecordExternalBookingEvent(b.ctx, venue.PublicAccess{}, f.place, in)
	if e != nil {
		t.Fatal("first telemetry", e)
	}
	two, e := b.store.RecordExternalBookingEvent(b.ctx, venue.PublicAccess{}, f.place, in)
	if e != nil || one != two || one.ConfirmedCapability != "UNAVAILABLE" || one.ConfirmedBooking != "UNKNOWN" {
		t.Fatal("idempotency or fake provider fact", e)
	}
	var count int
	b.pool.QueryRow(b.ctx, `SELECT count(*) FROM booking_external_events WHERE place_id=$1`, f.place).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate telemetry", count)
	}
	in.EventType = "CONFIRMED_BOOKING"
	if _, e = b.store.RecordExternalBookingEvent(b.ctx, venue.PublicAccess{}, f.place, in); !errors.Is(e, ba.ErrInvalid) {
		t.Fatal("confirmed client input accepted", e)
	}
}
func TestBookingAnalyticsNativeVersionAndACL(t *testing.T) {
	for _, kind := range []string{"hidden", "cityExpiry", "candidateRevoked", "sourceABA", "noURL", "operatorHidden"} {
		t.Run(kind, func(t *testing.T) {
			f := bookingVenueNative(t)
			b := f.private.base
			r, e := b.store.ReadCurrentPublicVenue(b.ctx, venue.PublicAccess{}, f.place)
			if e != nil {
				t.Fatal(e)
			}
			in := bookingInput(t, f, r)
			switch kind {
			case "hidden":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
			case "cityExpiry":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.city)
			case "candidateRevoked":
				b.exec(`UPDATE venue_candidates SET status='rejected' WHERE place_id=$1`, f.place)
			case "sourceABA":
				b.exec(`UPDATE venues SET reservation_url='https://example.org/changed' WHERE place_id=$1`, f.place)
				b.exec(`UPDATE venues SET reservation_url='https://example.org/booking' WHERE place_id=$1`, f.place)
			case "noURL":
				b.exec(`UPDATE venues SET reservation_support='none',reservation_url=NULL WHERE place_id=$1`, f.place)
			case "operatorHidden":
				b.exec(`UPDATE venues SET operator_organization_id=$2 WHERE place_id=$1`, f.place, b.orgID)
				b.exec(`UPDATE organizations SET status='suspended' WHERE id=$1`, b.orgID)
			}
			_, e = b.store.RecordExternalBookingEvent(b.ctx, venue.PublicAccess{}, f.place, in)
			if !errors.Is(e, venue.ErrChanged) && !errors.Is(e, venue.ErrNotFound) {
				t.Fatal("stale source accepted or SQL failure", kind, e)
			}
			var count int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM booking_external_events WHERE place_id=$1`, f.place).Scan(&count); e != nil || count != 0 {
				t.Fatal("invalid source telemetry", count, e)
			}
		})
	}
}
func TestBookingVenueNativeSealAndIdleRefresh(t *testing.T) {
	f := bookingVenueNative(t)
	b := f.private.base
	a := venue.PublicAccess{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.private.owner.SessionDigest}
	r, e := b.store.ReadCurrentPublicVenue(b.ctx, a, f.place)
	if e != nil {
		t.Fatal(e)
	}
	b.exec(`UPDATE sessions SET idle_expires_at=LEAST(expires_at,clock_timestamp()+interval '1 hour') WHERE token_sha256=$1`, a.SessionDigest[:])
	if e = b.store.RevalidateCurrentPublicVenue(b.ctx, a, f.place, r); e != nil {
		t.Fatal("normal idle refresh invalidated approved public source", e)
	}
	corrupt := r
	corrupt.View.ReservationSupport = "none"
	if e = b.store.RevalidateCurrentPublicVenue(b.ctx, a, f.place, corrupt); !errors.Is(e, venue.ErrChanged) {
		t.Fatal("unsealed payload accepted", e)
	}
	other := a
	other.Actor.ID = b.other.ID
	if e = b.store.RevalidateCurrentPublicVenue(b.ctx, other, f.place, r); !errors.Is(e, venue.ErrChanged) {
		t.Fatal("old authority accepted", e)
	}
}
func TestBookingVenueNativeFinalTableWaitChecksSourceAndNaturalExpiry(t *testing.T) {
	for _, kind := range []string{"sourceABA", "naturalExpiry", "sessionRevoke"} {
		t.Run(kind, func(t *testing.T) {
			f := bookingVenueNative(t)
			b := f.private.base
			a := venue.PublicAccess{}
			if kind == "naturalExpiry" {
				b.exec(`UPDATE venues SET expires_at=clock_timestamp()+interval '800 milliseconds' WHERE place_id=$1`, f.place)
			}
			if kind == "sessionRevoke" {
				a = venue.PublicAccess{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.private.owner.SessionDigest}
			}
			r, e := b.store.ReadCurrentPublicVenue(b.ctx, a, f.place)
			if e != nil {
				t.Fatal(e)
			}
			hold, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Rollback(context.Background())
			if _, e = hold.Exec(b.ctx, `LOCK TABLE venues IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			cfg := b.pool.Config().Copy()
			name := "booking-final-" + f.place
			cfg.ConnConfig.RuntimeParams["application_name"] = name
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			done := make(chan error, 1)
			go func() { done <- store.RevalidateCurrentPublicVenue(b.ctx, a, f.place, r) }()
			wait := false
			for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0)`, name).Scan(&wait); e != nil {
					t.Fatal(e)
				}
				if wait {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !wait {
				t.Fatal("no actual source table wait")
			}
			switch kind {
			case "sourceABA":
				if _, e = hold.Exec(b.ctx, `UPDATE venues SET source_url='https://example.org/x' WHERE place_id=$1`, f.place); e != nil {
					t.Fatal(e)
				}
				if _, e = hold.Exec(b.ctx, `UPDATE venues SET source_url='https://example.org/public-source' WHERE place_id=$1`, f.place); e != nil {
					t.Fatal(e)
				}
			case "naturalExpiry":
				time.Sleep(time.Until(r.View.BookingValidUntil) + 50*time.Millisecond)
			case "sessionRevoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			if e = hold.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				want := venue.ErrChanged
				if kind == "naturalExpiry" {
					want = venue.ErrNotFound
				}
				if kind == "sessionRevoke" {
					want = identity.ErrUnauthorized
				}
				if !errors.Is(e, want) {
					t.Fatal("late source check", kind, e, want)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("native wait never completed")
			}
		})
	}
}

func TestBookingAnalyticsNativeInsertForeignKeyWaitRollsBackStaleTelemetry(t *testing.T) {
	for _, kind := range []string{"hidden", "sourceABA", "naturalExpiry", "sessionExpiry", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			f := bookingVenueNative(t)
			b := f.private.base
			a := venue.PublicAccess{}
			if kind == "sessionExpiry" {
				a = venue.PublicAccess{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.private.owner.SessionDigest}
				b.exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp()+interval '900 milliseconds' AS at) UPDATE sessions SET expires_at=stamp.at,idle_expires_at=stamp.at FROM stamp WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			if kind == "naturalExpiry" {
				b.exec(`UPDATE venues SET expires_at=clock_timestamp()+interval '900 milliseconds' WHERE place_id=$1`, f.place)
			}
			r, e := b.store.ReadCurrentPublicVenue(b.ctx, a, f.place)
			if e != nil {
				t.Fatal(e)
			}
			in := bookingInput(t, f, r)
			hold, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Rollback(context.Background())
			if _, e = hold.Exec(b.ctx, `SELECT id FROM places WHERE id=$1 FOR UPDATE`, f.place); e != nil {
				t.Fatal(e)
			}
			cfg := b.pool.Config().Copy()
			name := "booking-fk-" + f.place
			cfg.ConnConfig.RuntimeParams["application_name"] = name
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			done := make(chan error, 1)
			callctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			go func() { _, e := store.RecordExternalBookingEvent(callctx, a, f.place, in); done <- e }()
			waited := false
			for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE 'INSERT INTO booking_external_events%')`, name).Scan(&waited); e != nil {
					t.Fatal(e)
				}
				if waited {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waited {
				t.Fatal("telemetry never waited on actual Place FK")
			}
			switch kind {
			case "hidden":
				if _, e = hold.Exec(b.ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place); e != nil {
					t.Fatal(e)
				}
			case "sourceABA":
				for _, name := range []string{"合成改变", "合成027地点"} {
					if _, e = hold.Exec(b.ctx, `UPDATE places SET name=$2 WHERE id=$1`, f.place, name); e != nil {
						t.Fatal(e)
					}
				}
			case "naturalExpiry", "sessionExpiry":
				time.Sleep(time.Until(in.ValidUntil) + 50*time.Millisecond)
			case "cancelled":
				cancel()
			}
			if e = hold.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				want := venue.ErrNotFound
				if kind == "sourceABA" {
					want = venue.ErrChanged
				}
				if kind == "sessionExpiry" {
					want = identity.ErrUnauthorized
				}
				if kind == "cancelled" {
					want = venue.ErrUnavailable
				}
				if !errors.Is(e, want) {
					t.Fatal("wrong final error", kind, e, want)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("telemetry FK never finished")
			}
			var n int
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM booking_external_events WHERE place_id=$1`, f.place).Scan(&n); e != nil || n != 0 {
				t.Fatal("failed statement leaked telemetry", n, e)
			}
		})
	}
}

func TestBookingAnalyticsNativeThirtyDayPruneIsInternalAndBounded(t *testing.T) {
	f := bookingVenueNative(t)
	b := f.private.base
	b.exec(`INSERT INTO booking_external_events(event_id,place_id,event_type,outcome,recorded_at) SELECT gen_random_uuid(),$1,'EXTERNAL_BOOKING_CLICK','CLIENT_REPORTED_EXTERNAL_OPEN',clock_timestamp()-interval '31 days' FROM generate_series(1,1001)`, f.place)
	b.exec(`INSERT INTO booking_external_events(event_id,place_id,event_type,outcome) VALUES(gen_random_uuid(),$1,'EXTERNAL_BOOKING_CLICK','CLIENT_REPORTED_EXTERNAL_OPEN')`, f.place)
	n, e := b.store.PruneExternalBookingEvents(b.ctx)
	if e != nil || n != 1000 {
		t.Fatal("unbounded/failed retention", n, e)
	}
	n, e = b.store.PruneExternalBookingEvents(b.ctx)
	if e != nil || n != 1 {
		t.Fatal("remaining old event", n, e)
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM booking_external_events WHERE place_id=$1`, f.place).Scan(&count); e != nil || count != 1 {
		t.Fatal("retention removed current event", count, e)
	}
}

func TestBookingVenueNativeOriginalRevokeAndSessionReplacement(t *testing.T) {
	f := bookingVenueNative(t)
	b := f.private.base
	a := venue.PublicAccess{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.private.owner.SessionDigest}
	r, e := b.store.ReadCurrentPublicVenue(b.ctx, a, f.place)
	if e != nil {
		t.Fatal(e)
	}
	in := bookingInput(t, f, r)
	if e = b.store.RevokeSession(b.ctx, a.SessionDigest); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.Authenticate(b.ctx, a.SessionDigest); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("original revoke revived by idle Authenticate", e)
	}
	if e = b.store.RevalidateCurrentPublicVenue(b.ctx, a, f.place, r); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("old revoked read accepted", e)
	}
	if _, e = b.store.RecordExternalBookingEvent(b.ctx, a, f.place, in); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("old revoked report accepted", e)
	}
	// Replacement is test-only, not an API to revive revoked approvals.
	b.exec(`DELETE FROM sessions WHERE token_sha256=$1`, a.SessionDigest[:])
	b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'oidc',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, a.Actor.ID, a.SessionDigest[:])
	if _, e = b.store.Authenticate(b.ctx, a.SessionDigest); e != nil {
		t.Fatal("fresh replacement session", e)
	}
	if e = b.store.RevalidateCurrentPublicVenue(b.ctx, a, f.place, r); !errors.Is(e, venue.ErrChanged) {
		t.Fatal("replacement revived old receipt", e)
	}
	if _, e = b.store.RecordExternalBookingEvent(b.ctx, a, f.place, in); !errors.Is(e, venue.ErrChanged) {
		t.Fatal("replacement revived old proposal", e)
	}
	var n int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM booking_external_events WHERE place_id=$1`, f.place).Scan(&n); e != nil || n != 0 {
		t.Fatal("old frame telemetry", n, e)
	}
}

func TestBookingAnalyticsNativeClientCannotExtendDeadline(t *testing.T) {
	f := bookingVenueNative(t)
	b := f.private.base
	r, e := b.store.ReadCurrentPublicVenue(b.ctx, venue.PublicAccess{}, f.place)
	if e != nil {
		t.Fatal(e)
	}
	in := bookingInput(t, f, r)
	in.ValidUntil = in.ValidUntil.Add(time.Millisecond)
	if _, e = b.store.RecordExternalBookingEvent(b.ctx, venue.PublicAccess{}, f.place, in); !errors.Is(e, venue.ErrChanged) {
		t.Fatal("client substituted original deadline", e)
	}
}

func TestBookingVenueNativeConnectionTimeZoneDoesNotChangeRevision(t *testing.T) {
	f := bookingVenueNative(t)
	b := f.private.base
	a := venue.PublicAccess{}
	r, e := b.store.ReadCurrentPublicVenue(b.ctx, a, f.place)
	if e != nil {
		t.Fatal(e)
	}
	for _, zone := range []string{"Asia/Tokyo", "America/New_York"} {
		cfg := b.pool.Config().Copy()
		cfg.ConnConfig.RuntimeParams["TimeZone"] = zone
		pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
		if e != nil {
			t.Fatal(e)
		}
		store := New(pool, false)
		current, e := store.ReadCurrentPublicVenue(b.ctx, a, f.place)
		if e != nil || current.Proof != r.Proof {
			pool.Close()
			t.Fatal("session timezone altered source", zone, e)
		}
		e = store.RevalidateCurrentPublicVenue(b.ctx, a, f.place, r)
		pool.Close()
		if e != nil {
			t.Fatal("time-zone revalidation", zone, e)
		}
	}
}

func TestBookingVenueNativeDoesNotReadPrivateConsoleBookingTable(t *testing.T) {
	f := bookingVenueNative(t)
	b := f.private.base
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(b.ctx, `LOCK TABLE business_console_venue_facts IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(b.ctx, time.Second)
	defer cancel()
	r, e := b.store.ReadCurrentPublicVenue(ctx, venue.PublicAccess{}, f.place)
	if e != nil || r.View.ReservationURL == nil || *r.View.ReservationURL != "https://example.org/booking" {
		t.Fatal("public source depended on private Console", e)
	}
	if e = b.store.RevalidateCurrentPublicVenue(ctx, venue.PublicAccess{}, f.place, r); e != nil {
		t.Fatal("public tail consulted private Console", e)
	}
}
