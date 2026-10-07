package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
var languageCode = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)

func validManagedActivity(input *activitypublish.Input) string {
	input.CityID = strings.TrimSpace(input.CityID)
	input.PlaceID = strings.TrimSpace(input.PlaceID)
	input.Modality = strings.TrimSpace(input.Modality)
	input.PhysicalPlaceStatus = strings.TrimSpace(input.PhysicalPlaceStatus)
	input.VenuePlaceID = strings.TrimSpace(input.VenuePlaceID)
	if input.Modality == "" && input.PhysicalPlaceStatus == "" {
		if input.PlaceID != "" {
			input.Modality, input.PhysicalPlaceStatus = "in_person", "confirmed"
		} else {
			input.Modality, input.PhysicalPlaceStatus = "unspecified", "unknown"
		}
	}
	switch {
	case input.Modality == "unspecified" && input.PhysicalPlaceStatus == "unknown" && input.PlaceID == "" && input.VenuePlaceID == "":
	case (input.Modality == "in_person" || input.Modality == "hybrid") && input.PhysicalPlaceStatus == "confirmed" && input.PlaceID != "" && (input.VenuePlaceID == "" || input.VenuePlaceID == input.PlaceID):
	case (input.Modality == "in_person" || input.Modality == "hybrid") && input.PhysicalPlaceStatus == "tbd" && input.PlaceID == "" && input.VenuePlaceID == "":
	case input.Modality == "online" && input.PhysicalPlaceStatus == "not_applicable" && input.PlaceID == "" && input.VenuePlaceID == "":
	default:
		return "activity_location_invalid"
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Description = strings.TrimSpace(input.Description)
	input.TimeZone = strings.TrimSpace(input.TimeZone)
	input.CategoryCode = strings.TrimSpace(input.CategoryCode)
	input.Currency = strings.TrimSpace(input.Currency)
	input.Eligibility = strings.TrimSpace(input.Eligibility)
	input.LanguageCode = strings.TrimSpace(input.LanguageCode)
	input.Visibility = strings.TrimSpace(input.Visibility)
	if input.CityID == "" {
		return "cityId_required"
	}
	if len([]rune(input.Title)) < 2 || len([]rune(input.Title)) > 160 {
		return "title_length_invalid"
	}
	if len([]rune(input.Summary)) > 3000 || len([]rune(input.Description)) > 10000 {
		return "description_too_long"
	}
	if input.StartsAt.IsZero() || input.EndsAt.IsZero() || !input.EndsAt.After(input.StartsAt) {
		return "schedule_invalid"
	}
	if input.EndsAt.Sub(input.StartsAt) > 7*24*time.Hour {
		return "schedule_too_long"
	}
	if _, err := time.LoadLocation(input.TimeZone); err != nil {
		return "timeZone_invalid"
	}
	if input.PlaceID != "" && !uuidPath.MatchString(input.PlaceID) {
		return "placeId_invalid"
	}
	if input.VenuePlaceID != "" && !uuidPath.MatchString(input.VenuePlaceID) {
		return "venuePlaceId_invalid"
	}
	if input.CategoryCode != "" && !categoryCode.MatchString(input.CategoryCode) {
		return "categoryCode_invalid"
	}
	if input.Capacity != nil && (*input.Capacity < 1 || *input.Capacity > 100000) {
		return "capacity_invalid"
	}
	if input.PriceMinor < 0 || input.PriceMinor > 2147483647 {
		return "priceMinor_invalid"
	}
	if input.PriceMinor > 0 && !currencyCode.MatchString(input.Currency) {
		return "currency_required"
	}
	if input.Currency != "" && !currencyCode.MatchString(input.Currency) {
		return "currency_invalid"
	}
	if len([]rune(input.Eligibility)) > 500 {
		return "eligibility_too_long"
	}
	if input.LanguageCode != "" && !languageCode.MatchString(input.LanguageCode) {
		return "languageCode_invalid"
	}
	if input.Visibility == "" {
		input.Visibility = "public"
	}
	if input.Visibility != "public" && input.Visibility != "unlisted" && input.Visibility != "private" &&
		input.Visibility != "organizer_members" && input.Visibility != "invite_only" {
		return "visibility_invalid"
	}
	return ""
}

func (s *server) managedActivityActor(w http.ResponseWriter, r *http.Request) (identity.Actor, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return identity.Actor{}, false
	}
	if actor.AccountType != "person" || s.activityPublish == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return identity.Actor{}, false
	}
	if !uuidPath.MatchString(r.PathValue("organizationID")) {
		respondError(w, http.StatusBadRequest, "invalid_organization_id")
		return identity.Actor{}, false
	}
	return actor, true
}

func managedActivityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, activitypublish.ErrForbidden):
		respondError(w, http.StatusForbidden, "organization_admin_required")
	case errors.Is(err, activitypublish.ErrConflict):
		respondError(w, http.StatusConflict, "activity_state_conflict")
	default:
		serverError(w, err)
	}
}

func (s *server) decodeActivityInput(w http.ResponseWriter, r *http.Request) (activitypublish.Input, bool) {
	var input activitypublish.Input
	if !decodeStrictJSON(w, r, &input) {
		return input, false
	}
	if code := validManagedActivity(&input); code != "" {
		respondError(w, http.StatusBadRequest, code)
		return input, false
	}
	city, err := s.catalog.GetCity(r.Context(), input.CityID)
	if handleReadError(w, err) {
		return input, false
	}
	if input.TimeZone != city.TimeZone {
		respondError(w, http.StatusBadRequest, "timeZone_city_mismatch")
		return input, false
	}
	if input.PlaceID != "" {
		place, err := s.catalog.GetPlace(r.Context(), input.PlaceID)
		if handleReadError(w, err) {
			return input, false
		}
		if place.CityID != input.CityID {
			respondError(w, http.StatusBadRequest, "place_city_mismatch")
			return input, false
		}
		if place.Source.ExpiresAt != nil && !place.Source.ExpiresAt.After(time.Now()) {
			respondError(w, http.StatusBadRequest, "place_unavailable")
			return input, false
		}
	}
	if input.VenuePlaceID != "" {
		if s.venues == nil {
			respondError(w, http.StatusBadRequest, "venue_unavailable")
			return input, false
		}
		v, err := s.venues.GetPublicVenue(r.Context(), input.VenuePlaceID)
		if err != nil || v.CityID != input.CityID {
			respondError(w, http.StatusBadRequest, "venue_unavailable")
			return input, false
		}
	}
	return input, true
}

func (s *server) createActivityDraft(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.managedActivityActor(w, r)
	if !ok {
		return
	}
	input, ok := s.decodeActivityInput(w, r)
	if !ok {
		return
	}
	item, err := s.activityPublish.CreateDraft(r.Context(), actor.ID, r.PathValue("organizationID"), input)
	if err != nil {
		managedActivityError(w, err)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"data": item})
}

func (s *server) updateManagedActivity(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.managedActivityActor(w, r)
	if !ok {
		return
	}
	if !uuidPath.MatchString(r.PathValue("activityID")) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return
	}
	input, ok := s.decodeActivityInput(w, r)
	if !ok {
		return
	}
	item, err := s.activityPublish.UpdateActivity(r.Context(), actor.ID, r.PathValue("organizationID"), r.PathValue("activityID"), input)
	if err != nil {
		managedActivityError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": item})
}

func (s *server) publishManagedActivity(w http.ResponseWriter, r *http.Request) {
	s.changeManagedActivityState(w, r, true)
}

func (s *server) cancelManagedActivity(w http.ResponseWriter, r *http.Request) {
	s.changeManagedActivityState(w, r, false)
}

func (s *server) changeManagedActivityState(w http.ResponseWriter, r *http.Request, publish bool) {
	actor, ok := s.managedActivityActor(w, r)
	if !ok {
		return
	}
	if !uuidPath.MatchString(r.PathValue("activityID")) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return
	}
	var item activitypublish.Activity
	var err error
	if publish {
		item, err = s.activityPublish.PublishActivity(r.Context(), actor.ID, r.PathValue("organizationID"), r.PathValue("activityID"))
	} else {
		item, err = s.activityPublish.CancelActivity(r.Context(), actor.ID, r.PathValue("organizationID"), r.PathValue("activityID"))
	}
	if err != nil {
		managedActivityError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": item})
}

func (s *server) listManagedActivities(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.managedActivityActor(w, r)
	if !ok {
		return
	}
	items, err := s.activityPublish.ListManagedActivities(r.Context(), actor.ID, r.PathValue("organizationID"))
	if err != nil {
		managedActivityError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}
