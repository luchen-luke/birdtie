package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	ic "github.com/birdtie/birdtie/apps/api/internal/intentconversion"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"net/http"
	"time"
)

func (s *server) conversionAccess(w http.ResponseWriter, r *http.Request) (ic.Access, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		conversionFailure(w, ic.ErrInvalid)
		return ic.Access{}, false
	}
	actor, digest, e := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, e) {
		return ic.Access{}, false
	}
	a := ic.Access{Actor: actor, SessionDigest: digest}
	w.Header().Set("Cache-Control", "no-store")
	if !ai.ValidAccess(a) || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "person_workspace_required")
		return a, false
	}
	if s.humanIntentConversions == nil {
		respondError(w, 503, "intent_conversion_unavailable")
		return a, false
	}
	return a, true
}
func conversionFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, ic.ErrInvalid):
		respondError(w, 400, "invalid_intent_conversion")
	case errors.Is(e, ic.ErrDenied):
		respondError(w, 403, "intent_conversion_denied")
	case errors.Is(e, ic.ErrConflict):
		respondError(w, 409, "intent_conversion_source_changed")
	default:
		respondError(w, 503, "intent_conversion_unavailable")
	}
}
func conversionEnvelopeValid(v ic.Envelope, a ic.Access) bool {
	return v.SchemaVersion == ic.Schema && v.OwnerID == a.Actor.ID && ai.UUID(v.AgentID) && !v.ObservedAt.IsZero() && !v.ModelAccess && !v.SendAllowed
}
func conversionIntentValid(v socialintent.Record, owner, id string, observed time.Time) bool {
	if v.ID != id || v.CreatorID != owner || v.Title == "" || v.CreatedAt.IsZero() || v.UpdatedAt.Before(v.CreatedAt) || v.ExpiresAt.IsZero() {
		return false
	}
	if _, _, e := socialintent.ParseConstraints(v.Constraints, v.Modality); e != nil {
		return false
	}
	switch v.Status {
	case socialintent.Draft, socialintent.Active, socialintent.Matched, socialintent.Converted, socialintent.Expired, socialintent.Cancelled:
	default:
		return false
	}
	if (v.ConvertedActivityID == nil) != (v.ConvertedParticipationID == nil) || (v.ConvertedActivityID == nil) != (v.ConvertedAt == nil) {
		return false
	}
	if v.ConvertedAt != nil && (v.Status != socialintent.Converted || !ai.UUID(*v.ConvertedActivityID) || !ai.UUID(*v.ConvertedParticipationID) || v.ConvertedAt.Before(v.CreatedAt) || v.ConvertedAt.After(observed)) {
		return false
	}
	return true
}
func conversionChoiceValid(v ic.Choice, observed time.Time) bool {
	a := v.Activity
	if !ai.UUID(a.ActivityID) || a.ID != a.ActivityID || !ai.UUID(v.ParticipationID) || !a.Available || a.CreatedAt.IsZero() || a.CreatedAt.After(observed) || a.Title == "" || a.CityID == "" || a.StartsAt == nil || a.EndsAt == nil || !a.EndsAt.After(*a.StartsAt) || !a.EndsAt.After(observed) || (a.Status != "upcoming" && a.Status != "ongoing") {
		return false
	}
	if a.PlaceID != "" && !ai.UUID(a.PlaceID) || a.VenuePlaceID != "" && !ai.UUID(a.VenuePlaceID) || a.PlaceName != "" && a.PlaceID == "" {
		return false
	}
	switch a.Modality {
	case "online":
		return a.PhysicalPlaceStatus == "not_applicable" && a.PlaceID == "" && a.PlaceName == ""
	case "in_person", "hybrid":
		return a.PhysicalPlaceStatus == "confirmed" || a.PhysicalPlaceStatus == "tbd"
	default:
		return false
	}
}
func conversionRespond(w http.ResponseWriter, r *http.Request, v any, check func(context.Context) error) {
	if check == nil {
		conversionFailure(w, ic.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": v})
	if e == nil {
		e = check(r.Context())
	}
	if e == nil {
		e = r.Context().Err()
	}
	if e != nil {
		conversionFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
func (s *server) getOwnIntentActivityConversion(w http.ResponseWriter, r *http.Request) {
	a, ok := s.conversionAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("intentID")
	if !ai.UUID(id) {
		conversionFailure(w, ic.ErrInvalid)
		return
	}
	v, e := s.humanIntentConversions.ListOwn(r.Context(), a, id)
	if e != nil {
		conversionFailure(w, e)
		return
	}
	valid := conversionEnvelopeValid(v.Envelope, a) && conversionIntentValid(v.Intent, a.Actor.ID, id, v.ObservedAt) && v.Limit == 100 && v.Choices != nil && len(v.Choices) <= 100 && len(v.Version) == 64
	seen := map[string]bool{}
	for _, c := range v.Choices {
		valid = valid && conversionChoiceValid(c, v.ObservedAt) && !seen[c.Activity.ActivityID]
		seen[c.Activity.ActivityID] = true
	}
	if !valid {
		conversionFailure(w, ic.ErrUnavailable)
		return
	}
	conversionRespond(w, r, v, v.Revalidate)
}
func (s *server) previewOwnIntentActivityConversion(w http.ResponseWriter, r *http.Request) {
	a, ok := s.conversionAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("intentID")
	var in ic.Input
	if !ai.UUID(id) {
		conversionFailure(w, ic.ErrInvalid)
		return
	}
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	v, e := s.humanIntentConversions.PreviewOwn(r.Context(), a, id, in)
	if e != nil {
		conversionFailure(w, e)
		return
	}
	if !conversionEnvelopeValid(v.Envelope, a) || !conversionIntentValid(v.Intent, a.Actor.ID, id, v.ObservedAt) || v.Intent.Type != "FIND_ACTIVITY" || (v.Intent.Status != socialintent.Active && v.Intent.Status != socialintent.Matched) || !v.Intent.ExpiresAt.After(v.ObservedAt) || !conversionChoiceValid(v.Choice, v.ObservedAt) || v.Version != in.ExpectedVersion || v.Choice.Activity.ActivityID != in.ActivityID || v.Choice.Activity.ID != in.ActivityID || !ai.UUID(v.Choice.ParticipationID) || v.PreviewID == "" || !v.ExpiresAt.After(v.ObservedAt) || v.ExpiresAt.Sub(v.ObservedAt) > ic.PreviewTTL {
		conversionFailure(w, ic.ErrUnavailable)
		return
	}
	conversionRespond(w, r, v, v.Revalidate)
}
func (s *server) approveOwnIntentActivityConversion(w http.ResponseWriter, r *http.Request) {
	a, ok := s.conversionAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("intentID")
	var in ic.Approval
	if !ai.UUID(id) {
		conversionFailure(w, ic.ErrInvalid)
		return
	}
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	v, e := s.humanIntentConversions.ApproveOwn(r.Context(), a, id, in.PreviewID)
	if e != nil {
		conversionFailure(w, e)
		return
	}
	if !conversionEnvelopeValid(v.Envelope, a) || !conversionIntentValid(v.Intent, a.Actor.ID, id, v.ObservedAt) || !v.Committed || v.Intent.Status != "CONVERTED" || v.Intent.ConvertedActivityID == nil || *v.Intent.ConvertedActivityID != v.ActivityID || v.Intent.ConvertedParticipationID == nil || *v.Intent.ConvertedParticipationID != v.ParticipationID || !ai.UUID(v.ActivityID) || !ai.UUID(v.ParticipationID) || v.Intent.ConvertedAt == nil {
		conversionFailure(w, ic.ErrUnavailable)
		return
	}
	conversionRespond(w, r, v, v.Revalidate)
}
