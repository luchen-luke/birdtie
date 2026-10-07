package httpapi

import (
	"context"
	"encoding/json"
	ba "github.com/birdtie/birdtie/apps/api/internal/bookinganalytics"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"net/http"
	"strings"
	"testing"
	"time"
)

func bookingHTTPNative(t *testing.T) *contextBuilderHTTPFixture {
	t.Helper()
	f := contextBuilderHTTPNative(t)
	f.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role,state) VALUES($1,$2,'contributor','active'),($1,$3,'reviewer','active')`, f.city, f.accountIDs[0], f.accountIDs[1])
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM booking_external_events WHERE place_id=$1`, `DELETE FROM venues WHERE place_id=$1`, `DELETE FROM venue_candidates WHERE place_id=$1`} {
			f.exec(q, f.place)
		}
		f.exec(`DELETE FROM city_editor_memberships WHERE city_id=$1`, f.city)
	})
	u := "https://example.org/booking"
	c, e := f.store.SubmitVenueCandidate(f.ctx, f.accountIDs[0], f.city, f.place, venue.SubmitInput{Facts: venue.Facts{ReservationSupport: "external_url", ReservationURL: &u, Suitability: []string{}, Amenities: []string{}}, SourceURL: "https://example.org/public", RightsNote: "PRIVATE_RIGHTS_CANARY not a real supplier", ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.ReviewVenueCandidate(f.ctx, f.accountIDs[1], c.ID, venue.ReviewInput{Decision: "approve", Note: "LOCAL_SYNTHETIC reviewer"}); e != nil {
		t.Fatal(e)
	}
	return f
}
func TestBookingHTTPRegisteredClosedTelemetryAndCurrentPublicSource(t *testing.T) {
	f := bookingHTTPNative(t)
	path := "/v1/places/" + f.place + "/venue"
	for _, token := range []string{"", f.tokens[0]} {
		w := f.request(t, f.handler, "GET", path, "", token, 200, nil)
		for _, hidden := range []string{"PRIVATE_RIGHTS_CANARY", "rightsNote", "reviewedBy", "Proof", "Seal", "SessionDigest"} {
			if strings.Contains(w.Body.String(), hidden) {
				t.Fatal("private/internal field escaped", hidden)
			}
		}
		var envelope struct {
			Data venue.Public `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Data.BookingSourceVersion == "" {
			t.Fatal("missing native public binding", w.Body.String())
		}
		var id string
		if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		in := ba.Input{EventID: id, EventType: ba.EventType, Outcome: ba.Outcome, SourceVersion: envelope.Data.BookingSourceVersion, ValidUntil: envelope.Data.BookingValidUntil}
		body, _ := json.Marshal(in)
		p := "/v1/places/" + f.place + "/booking-events"
		w = f.request(t, f.handler, "POST", p, string(body), token, 200, nil)
		var report struct {
			Data ba.Result `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &report) != nil || report.Data.ConfirmedBooking != "UNKNOWN" || report.Data.ConfirmedCapability != "UNAVAILABLE" {
			t.Fatal(w.Body.String())
		}
		f.request(t, f.handler, "POST", p, string(body), token, 200, nil)
		for _, bad := range []string{`{"eventId":"` + id + `"}`, `null`, string(body[:len(body)-1]) + `,"ownerId":"` + f.accountIDs[0] + `"}`, string(body[:len(body)-1]) + `,"eventId":"` + id + `"}`} {
			f.request(t, f.handler, "POST", p, bad, token, 400, nil)
		}
		in.EventType = "CONFIRMED_BOOKING"
		raw, _ := json.Marshal(in)
		f.request(t, f.handler, "POST", p, string(raw), token, 400, nil)
		f.request(t, f.handler, "POST", p+"?source=x", string(body), token, 400, nil)
	}
	f.request(t, f.handler, "GET", path, "", "bad-token", 401, nil)
	f.request(t, f.handler, "POST", "/v1/places/"+f.place+"/booking-events", `{}`, "bad-token", 401, nil)
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM booking_external_events WHERE place_id=$1`, f.place).Scan(&n); e != nil || n != 2 {
		t.Fatal("telemetry not idempotent", n, e)
	}
}

type bookingHTTPBeforeTail struct {
	*postgres.Store
	before func()
}

func (c *bookingHTTPBeforeTail) ReadCurrentPublicVenue(ctx context.Context, a venue.PublicAccess, id string) (venue.CurrentReceipt, error) {
	r, e := c.Store.ReadCurrentPublicVenue(ctx, a, id)
	if e == nil && c.before != nil {
		c.before()
	}
	return r, e
}
func TestBookingVenueHTTPFinalEncodingThenNativeSourceRevalidation(t *testing.T) {
	f := bookingHTTPNative(t)
	catalog := &bookingHTTPBeforeTail{Store: f.store, before: func() {
		f.exec(`UPDATE venues SET reservation_url='https://example.org/changed' WHERE place_id=$1`, f.place)
		f.exec(`UPDATE venues SET reservation_url='https://example.org/booking' WHERE place_id=$1`, f.place)
	}}
	handler := contextBuilderHTTPNew(catalog, f.store, f.store)
	f.request(t, handler, http.MethodGet, "/v1/places/"+f.place+"/venue", "", "", 409, nil)
}
