package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
)

var categoryCode = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, output any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return false
	}
	return true
}

func validCandidate(input *cityseed.SubmitInput) bool {
	input.Name = strings.TrimSpace(input.Name)
	input.CategoryCode = strings.TrimSpace(input.CategoryCode)
	input.Summary = strings.TrimSpace(input.Summary)
	input.AddressLabel = strings.TrimSpace(input.AddressLabel)
	input.SourceLabel = strings.TrimSpace(input.SourceLabel)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	input.RightsNote = strings.TrimSpace(input.RightsNote)
	input.ProviderCode = strings.ToLower(strings.TrimSpace(input.ProviderCode))
	input.ProviderPlaceID = strings.TrimSpace(input.ProviderPlaceID)
	input.Attribution = strings.TrimSpace(input.Attribution)
	source, err := url.Parse(input.SourceURL)
	if err != nil || source.Scheme != "https" || source.Host == "" || source.User != nil {
		return false
	}
	now := time.Now()
	if len(input.Name) == 0 || len(input.Name) > 150 ||
		!categoryCode.MatchString(input.CategoryCode) ||
		len(input.Summary) > 1000 || len(input.SourceLabel) == 0 ||
		len(input.SourceLabel) > 120 || len(input.SourceURL) > 1000 ||
		len(input.RightsNote) < 10 || len(input.RightsNote) > 1000 ||
		len([]rune(input.AddressLabel)) > 240 ||
		(input.AddressLabel != "" && input.LocationPrecision != "point") ||
		input.ExpiresAt.Before(now.Add(time.Hour)) ||
		input.ExpiresAt.After(now.Add(365*24*time.Hour)) {
		return false
	}
	hasProvider := input.ProviderCode != "" || input.ProviderPlaceID != "" || input.Attribution != ""
	if hasProvider && (input.ProviderCode == "" || input.ProviderPlaceID == "" ||
		input.Attribution == "" || !categoryCode.MatchString(input.ProviderCode) ||
		len(input.ProviderPlaceID) > 200 || len(input.Attribution) > 500) {
		return false
	}
	coordinates := input.Latitude != nil && input.Longitude != nil
	if (input.Latitude == nil) != (input.Longitude == nil) {
		return false
	}
	if coordinates && (math.IsNaN(*input.Latitude) || math.IsInf(*input.Latitude, 0) ||
		math.IsNaN(*input.Longitude) || math.IsInf(*input.Longitude, 0) ||
		*input.Latitude < -90 || *input.Latitude > 90 ||
		*input.Longitude < -180 || *input.Longitude > 180) {
		return false
	}
	switch input.LocationPrecision {
	case "none":
		return !coordinates
	case "city", "area", "point":
		return coordinates
	default:
		return false
	}
}

func (s *server) submitPlaceCandidate(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	if len(cityID) == 0 || len(cityID) > 80 {
		respondError(w, http.StatusBadRequest, "invalid_city_id")
		return
	}
	var input cityseed.SubmitInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !validCandidate(&input) {
		respondError(w, http.StatusBadRequest, "invalid_candidate")
		return
	}
	candidate, err := s.seed.Submit(r.Context(), actor.ID, cityID, input)
	if errors.Is(err, cityseed.ErrForbidden) {
		respondError(w, http.StatusForbidden, "city_editor_required")
	} else if errors.Is(err, cityseed.ErrDuplicate) {
		respondError(w, http.StatusConflict, "duplicate_provider_candidate")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": candidate})
	}
}

func (s *server) listPlaceCandidates(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	candidates, err := s.seed.List(r.Context(), actor.ID, r.PathValue("cityID"))
	if errors.Is(err, cityseed.ErrForbidden) {
		respondError(w, http.StatusForbidden, "city_editor_required")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": candidates})
	}
}

func (s *server) reviewPlaceCandidate(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	candidateID := r.PathValue("candidateID")
	if !uuidPath.MatchString(candidateID) {
		respondError(w, http.StatusBadRequest, "invalid_candidate_id")
		return
	}
	var input cityseed.ReviewInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.Note = strings.TrimSpace(input.Note)
	if len(input.Note) < 10 || len(input.Note) > 1000 ||
		(input.Decision != "publish" && input.Decision != "link_existing" && input.Decision != "reject") ||
		(input.Decision == "link_existing" && !uuidPath.MatchString(input.TargetPlaceID)) ||
		(input.Decision != "link_existing" && input.TargetPlaceID != "") {
		respondError(w, http.StatusBadRequest, "invalid_review")
		return
	}
	candidate, err := s.seed.Review(r.Context(), actor.ID, candidateID, input)
	switch {
	case errors.Is(err, cityseed.ErrForbidden):
		respondError(w, http.StatusForbidden, "reviewer_required")
	case errors.Is(err, cityseed.ErrConflict):
		respondError(w, http.StatusConflict, "review_conflict")
	case errors.Is(err, cityseed.ErrDuplicate):
		respondError(w, http.StatusConflict, "provider_reference_conflict")
	case err != nil:
		serverError(w, err)
	default:
		respond(w, http.StatusOK, map[string]any{"data": candidate})
	}
}
