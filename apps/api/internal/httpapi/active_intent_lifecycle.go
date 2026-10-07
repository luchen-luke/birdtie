package httpapi

import (
	"errors"
	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"net/http"
)

func (s *server) activeIntentAccess(w http.ResponseWriter, r *http.Request) (ai.Access, bool) {
	actor, digest, e := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, e) {
		return ai.Access{}, false
	}
	a := ai.Access{Actor: actor, SessionDigest: digest}
	if !ai.ValidAccess(a) || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "person_workspace_required")
		return a, false
	}
	if s.humanActiveIntents == nil {
		respondError(w, 503, "active_social_intents_unavailable")
		return a, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return a, true
}
func activeIntentFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, ai.ErrInvalid):
		respondError(w, 400, "invalid_active_social_intent")
	case errors.Is(e, ai.ErrDenied):
		respondError(w, 403, "active_social_intent_denied")
	case errors.Is(e, ai.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(e, ai.ErrConflict):
		respondError(w, 409, "active_social_intent_version_changed")
	default:
		respondError(w, 503, "active_social_intents_unavailable")
	}
}
func validActiveDetail(v ai.Detail, a ai.Access, id string) bool {
	return ai.ValidateEnvelope(v.Envelope, a.Actor.ID) == nil && v.Item.Intent.ID == id && ai.ValidateItem(v.Item, a.Actor.ID) == nil
}
func (s *server) listOwnActiveSocialIntents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.activeIntentAccess(w, r)
	if !ok {
		return
	}
	v, e := s.humanActiveIntents.ListOwn(r.Context(), a)
	if e != nil {
		activeIntentFailure(w, e)
		return
	}
	seen := map[string]bool{}
	valid := ai.ValidateEnvelope(v.Envelope, a.Actor.ID) == nil && v.Limit == 100 && len(v.Items) <= 100 && v.Items != nil
	for _, i := range v.Items {
		valid = valid && !seen[i.Intent.ID] && ai.ValidateItem(i, a.Actor.ID) == nil
		seen[i.Intent.ID] = true
	}
	if !valid {
		activeIntentFailure(w, ai.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) getOwnActiveSocialIntent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.activeIntentAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("intentID")
	if !ai.UUID(id) {
		activeIntentFailure(w, ai.ErrInvalid)
		return
	}
	v, e := s.humanActiveIntents.ReadOwn(r.Context(), a, id)
	if e != nil {
		activeIntentFailure(w, e)
		return
	}
	if !validActiveDetail(v, a, id) {
		activeIntentFailure(w, ai.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) getActiveSocialIntentOptions(w http.ResponseWriter, r *http.Request) {
	a, ok := s.activeIntentAccess(w, r)
	if !ok {
		return
	}
	v, e := s.humanActiveIntents.OptionsOwn(r.Context(), a)
	if e != nil {
		activeIntentFailure(w, e)
		return
	}
	valid := ai.ValidateEnvelope(v.Envelope, a.Actor.ID) == nil && v.Limit == 100
	for n, items := range [][]ai.Option{v.Cities, v.Places, v.Communities, v.Invitees} {
		valid = valid && items != nil && len(items) <= 100
		seen := map[string]bool{}
		for _, item := range items {
			valid = valid && item.ID != "" && item.Label != "" && !seen[item.ID] && (n == 0 || ai.UUID(item.ID))
			seen[item.ID] = true
		}
	}
	if !valid {
		activeIntentFailure(w, ai.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) previewOwnActiveSocialIntent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.activeIntentAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("intentID")
	var in ai.Input
	if !ai.UUID(id) {
		activeIntentFailure(w, ai.ErrInvalid)
		return
	}
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	if ai.ValidateInput(in) != nil {
		activeIntentFailure(w, ai.ErrInvalid)
		return
	}
	v, e := s.humanActiveIntents.PreviewOwn(r.Context(), a, id, in)
	if e != nil {
		activeIntentFailure(w, e)
		return
	}
	valid := ai.ValidateEnvelope(v.Envelope, a.Actor.ID) == nil && v.Operation == in.Operation && v.Before.Intent.ID == id && v.After.Intent.ID == id && v.Before.Version == in.ExpectedVersion && ai.ValidateItem(v.Before, a.Actor.ID) == nil && ai.ValidateItem(v.After, a.Actor.ID) == nil && v.PreviewID != "" && len(v.PreviewID) <= 20000 && v.ExpiresAt.After(v.ObservedAt) && v.ExpiresAt.Sub(v.ObservedAt) <= ai.PreviewTTL && v.Explanation != ""
	if !valid {
		activeIntentFailure(w, ai.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) approveOwnActiveSocialIntent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.activeIntentAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("intentID")
	var in struct {
		PreviewID string `json:"previewId"`
	}
	if !ai.UUID(id) {
		activeIntentFailure(w, ai.ErrInvalid)
		return
	}
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	if in.PreviewID == "" || len(in.PreviewID) > 20000 {
		activeIntentFailure(w, ai.ErrInvalid)
		return
	}
	v, e := s.humanActiveIntents.ApproveOwn(r.Context(), a, id, in.PreviewID)
	if e != nil {
		activeIntentFailure(w, e)
		return
	}
	valid := ai.ValidateEnvelope(v.Envelope, a.Actor.ID) == nil && v.Item.Intent.ID == id && ai.ValidateItem(v.Item, a.Actor.ID) == nil && v.Committed && v.Explanation != "" && (v.Operation == "EDIT" || v.Operation == "CANCEL" || v.Operation == "ACTIVATE")
	if !valid {
		activeIntentFailure(w, ai.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
