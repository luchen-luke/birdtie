package httpapi

import (
	"encoding/json"
	ba "github.com/birdtie/birdtie/apps/api/internal/bookinganalytics"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBookingHTTPExactBodyRejectsAliasesAndAuthority(t *testing.T) {
	body, _ := json.Marshal(ba.Input{EventID: "84000000-0000-4000-8000-000000000001", EventType: ba.EventType, Outcome: ba.Outcome, SourceVersion: strings.Repeat("a", 64), ValidUntil: time.Now().UTC()})
	keys := []string{"eventId", "eventType", "outcome", "sourceVersion", "validUntil"}
	if _, e := contextPurposeObject(body, keys...); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(string(body), "eventId", "EventID", 1), string(body[:len(body)-1]) + `,"eventId":"x"}`, string(body[:len(body)-1]) + `,"url":"https://example.org/private"}`, `null`} {
		if _, e := contextPurposeObject([]byte(bad), keys...); e == nil {
			t.Fatal("unknown/duplicate body accepted")
		}
	}
}
func TestBookingVenueHTTPMissingPortUnavailable(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/places/84000000-0000-4000-8000-000000000001/venue", nil)
	r.SetPathValue("placeID", "84000000-0000-4000-8000-000000000001")
	w := httptest.NewRecorder()
	s := &server{}
	s.getPublicVenue(w, r)
	if w.Code != 503 {
		t.Fatal("missing public Venue port", w.Code, w.Body.String())
	}
}
