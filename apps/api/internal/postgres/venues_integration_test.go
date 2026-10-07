package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"testing"
	"time"
)

func bookingVenueNative(t *testing.T) *placeMemoryFixture {
	t.Helper()
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	b.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role,state) VALUES($1,$2,'contributor','active'),($1,$3,'reviewer','active')`, f.city, b.person.ID, b.other.ID)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM booking_external_events WHERE place_id=$1`, `DELETE FROM venues WHERE place_id=$1`, `DELETE FROM venue_candidates WHERE place_id=$1`} {
			b.exec(q, f.place)
		}
		b.exec(`DELETE FROM city_editor_memberships WHERE city_id=$1`, f.city)
	})
	u := "https://example.org/booking"
	c, e := b.store.SubmitVenueCandidate(b.ctx, b.person.ID, f.city, f.place, venue.SubmitInput{Facts: venue.Facts{ReservationSupport: "external_url", ReservationURL: &u, Suitability: []string{}, Amenities: []string{}}, SourceURL: "https://example.org/public-source", RightsNote: "LOCAL_SYNTHETIC only, no real provider permission", ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if e != nil {
		t.Fatal("real original submit", e)
	}
	if _, e = b.store.ReviewVenueCandidate(b.ctx, b.other.ID, c.ID, venue.ReviewInput{Decision: "approve", Note: "LOCAL_SYNTHETIC independent local reviewer"}); e != nil {
		t.Fatal("real original review", e)
	}
	return f
}

func TestBookingVenueNativeExpiredCityCannotExposeBooking(t *testing.T) {
	f := bookingVenueNative(t)
	b := f.private.base
	if _, e := b.store.GetPublicVenue(b.ctx, f.place); e != nil {
		t.Fatal("original positive", e)
	}
	b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.city)
	if _, e := b.store.GetPublicVenue(b.ctx, f.place); !errors.Is(e, venue.ErrNotFound) {
		t.Fatal("expired public City exposed old booking link", e)
	}
}

// Keep context imported for the upcoming true native wait/cleanup cases.
var _ = context.Background
