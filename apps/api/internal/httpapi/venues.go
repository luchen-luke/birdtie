package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/venue"
)

var venueCode = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

func validHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && len(raw) <= 1000
}

func normalizeVenueInput(in *venue.SubmitInput) bool {
	in.SourceURL = strings.TrimSpace(in.SourceURL)
	in.RightsNote = strings.TrimSpace(in.RightsNote)
	in.ReservationSupport = strings.TrimSpace(in.ReservationSupport)
	if in.ReservationSupport == "" {
		in.ReservationSupport = "unknown"
	}
	if in.ReservationURL != nil {
		s := strings.TrimSpace(*in.ReservationURL)
		in.ReservationURL = &s
	}
	if !validHTTPS(in.SourceURL) || len(in.RightsNote) < 10 || len(in.RightsNote) > 1000 ||
		in.ExpiresAt.Before(time.Now().Add(time.Hour)) || in.ExpiresAt.After(time.Now().Add(365*24*time.Hour)) ||
		(in.Capacity != nil && (*in.Capacity < 1 || *in.Capacity > 100000)) {
		return false
	}
	switch in.ReservationSupport {
	case "unknown", "none", "contact":
		if in.ReservationURL != nil {
			return false
		}
	case "external_url":
		if in.ReservationURL == nil || !validHTTPS(*in.ReservationURL) {
			return false
		}
	default:
		return false
	}
	if len(in.Suitability) > 20 || len(in.Amenities) > 20 {
		return false
	}
	for _, group := range [][]string{in.Suitability, in.Amenities} {
		seen := map[string]bool{}
		for _, v := range group {
			if !venueCode.MatchString(v) || seen[v] {
				return false
			}
			seen[v] = true
		}
	}
	if in.Capacity == nil && in.ReservationSupport == "unknown" && len(in.Suitability) == 0 && len(in.Amenities) == 0 {
		return false
	}
	if in.OperatorOrganizationID != nil && !uuidPath.MatchString(*in.OperatorOrganizationID) {
		return false
	}
	return true
}

func (s *server) getPublicVenue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("placeID")
	if !uuidPath.MatchString(id) {
		respondError(w, 400, "invalid_place_id")
		return
	}
	if s.venues == nil {
		bookingFailure(w, venue.ErrUnavailable)
		return
	}
	if native, ok := s.venues.(venue.CurrentStore); ok {
		if !bookingWireValid(r) || r.ContentLength != 0 || r.TransferEncoding != nil {
			bookingFailure(w, venue.ErrChanged)
			return
		}
		a, ok := s.bookingAccess(w, r)
		if !ok {
			return
		}
		receipt, e := native.ReadCurrentPublicVenue(r.Context(), a, id)
		if e != nil {
			bookingFailure(w, e)
			return
		}
		writeCurrentBooking(w, receipt.View, native, r, a, receipt)
		return
	}
	v, err := s.venues.GetPublicVenue(r.Context(), id)
	if errors.Is(err, venue.ErrNotFound) {
		respondError(w, 404, "not_found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}

func (s *server) submitVenueCandidate(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	cityID, placeID := r.PathValue("cityID"), r.PathValue("placeID")
	if len(cityID) == 0 || len(cityID) > 80 || !uuidPath.MatchString(placeID) {
		respondError(w, 400, "invalid_place_id")
		return
	}
	var in venue.SubmitInput
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	if !normalizeVenueInput(&in) {
		respondError(w, 400, "invalid_venue_candidate")
		return
	}
	c, err := s.venues.SubmitVenueCandidate(r.Context(), actor.ID, cityID, placeID, in)
	switch {
	case errors.Is(err, venue.ErrForbidden):
		respondError(w, 403, "city_editor_required")
	case errors.Is(err, venue.ErrConflict):
		respondError(w, 409, "venue_candidate_conflict")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, 201, map[string]any{"data": c})
	}
}

func (s *server) listVenueCandidates(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	items, err := s.venues.ListVenueCandidates(r.Context(), actor.ID, r.PathValue("cityID"))
	switch {
	case errors.Is(err, venue.ErrForbidden):
		respondError(w, 403, "reviewer_required")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, 200, map[string]any{"data": items})
	}
}

func (s *server) reviewVenueCandidate(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("candidateID")
	if !uuidPath.MatchString(id) {
		respondError(w, 400, "invalid_candidate_id")
		return
	}
	var in venue.ReviewInput
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if len(in.Note) < 10 || len(in.Note) > 1000 || (in.Decision != "approve" && in.Decision != "reject") {
		respondError(w, 400, "invalid_review")
		return
	}
	c, err := s.venues.ReviewVenueCandidate(r.Context(), actor.ID, id, in)
	switch {
	case errors.Is(err, venue.ErrForbidden):
		respondError(w, 403, "reviewer_required")
	case errors.Is(err, venue.ErrConflict):
		respondError(w, 409, "review_conflict")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, 200, map[string]any{"data": c})
	}
}
