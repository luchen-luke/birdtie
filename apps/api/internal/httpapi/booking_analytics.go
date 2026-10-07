package httpapi

import (
	"encoding/json"
	"errors"
	ba "github.com/birdtie/birdtie/apps/api/internal/bookinganalytics"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"net/http"
)

func bookingFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(e, ba.ErrInvalid):
		respondError(w, 400, "invalid_booking_event")
	case errors.Is(e, venue.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(e, venue.ErrChanged):
		respondError(w, 409, "booking_source_changed")
	default:
		respondError(w, 503, "booking_sources_unavailable")
	}
}
func (s *server) bookingAccess(w http.ResponseWriter, r *http.Request) (venue.PublicAccess, bool) {
	a, d, e := s.humanSocialActor(r, false)
	if e != nil {
		bookingFailure(w, e)
		return venue.PublicAccess{}, false
	}
	return venue.PublicAccess{Actor: a, SessionDigest: d}, true
}
func bookingWireValid(r *http.Request) bool {
	return r.URL.RawQuery == "" && !r.URL.ForceQuery && len(r.Header.Values("X-Birdtie-Organization-Workspace")) == 0
}
func (s *server) postExternalBookingEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("placeID")
	if !uuidPath.MatchString(id) || !bookingWireValid(r) {
		bookingFailure(w, ba.ErrInvalid)
		return
	}
	a, ok := s.bookingAccess(w, r)
	if !ok {
		return
	}
	raw, ok := contextPurposeBody(w, r, "eventId", "eventType", "outcome", "sourceVersion", "validUntil")
	if !ok {
		return
	}
	var in ba.Input
	if json.Unmarshal(raw, &in) != nil {
		bookingFailure(w, ba.ErrInvalid)
		return
	}
	if !in.Valid(id) {
		bookingFailure(w, ba.ErrInvalid)
		return
	}
	native, ok := s.catalog.(ba.Store)
	if !ok {
		bookingFailure(w, venue.ErrUnavailable)
		return
	}
	out, e := native.RecordExternalBookingEvent(r.Context(), a, id, in)
	if e != nil {
		bookingFailure(w, e)
		return
	}
	// Only aggregate telemetry fields. Provider transaction status stays UNKNOWN.
	respond(w, 200, map[string]any{"data": out})
}
func writeCurrentBooking(w http.ResponseWriter, v venue.Public, native venue.CurrentStore, r *http.Request, a venue.PublicAccess, receipt venue.CurrentReceipt) {
	body, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		bookingFailure(w, venue.ErrUnavailable)
		return
	}
	if e = native.RevalidateCurrentPublicVenue(r.Context(), a, v.PlaceID, receipt); e != nil {
		bookingFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}
