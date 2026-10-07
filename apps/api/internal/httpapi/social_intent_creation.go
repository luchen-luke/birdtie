package httpapi

import (
	"errors"
	si "github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"net/http"
	"strings"
)

func (s *server) creationAccess(w http.ResponseWriter, r *http.Request) (si.CreationAccess, si.CreationGateway, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "person_workspace_required")
		return si.CreationAccess{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, 400, "invalid_social_intent_creation")
		return si.CreationAccess{}, nil, false
	}
	actor, digest, e := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, e) {
		return si.CreationAccess{}, nil, false
	}
	a := si.CreationAccess{Actor: actor, SessionDigest: digest}
	if !si.ValidCreationAccess(a) {
		respondError(w, 403, "person_workspace_required")
		return a, nil, false
	}
	g, ok := s.socialIntents.(si.CreationGateway)
	if !ok {
		respondError(w, 503, "social_intent_creations_unavailable")
		return a, nil, false
	}
	return a, g, true
}
func creationFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, si.ErrCreationInvalid):
		respondError(w, 400, "invalid_social_intent_creation")
	case errors.Is(e, si.ErrCreationDenied):
		respondError(w, 403, "social_intent_creation_denied")
	case errors.Is(e, si.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(e, si.ErrCreationConflict):
		respondError(w, 409, "social_intent_operation_changed")
	default:
		respondError(w, 503, "social_intent_creations_unavailable")
	}
}
func (s *server) createKeyedPrivateIntent(w http.ResponseWriter, r *http.Request, source string, input si.DraftInput) {
	a, g, ok := s.creationAccess(w, r)
	if !ok {
		return
	}
	n, e := si.NormalizeCreation(input, source)
	if e != nil {
		creationFailure(w, e)
		return
	}
	receipt, created, e := g.CreatePrivateDraft(r.Context(), a, source, n)
	if e != nil {
		creationFailure(w, e)
		return
	}
	expected, e := si.CreationDigest(a.Actor.ID, source, n)
	if e != nil || si.ValidateCreationReceipt(receipt, a.Actor.ID, n.OperationID) != nil || receipt.RequestDigest != expected || receipt.SourceTaskID != source {
		creationFailure(w, si.ErrCreationUnavailable)
		return
	}
	if created {
		item := receipt.Intent
		echo := n
		if item == nil || item.Audience != "PRIVATE" || item.Status != "DRAFT" {
			creationFailure(w, si.ErrCreationUnavailable)
			return
		}
		echo.Type, echo.Title, echo.Audience, echo.Modality, echo.Constraints, echo.ExpiresAt = item.Type, item.Title, item.Audience, item.Modality, item.Constraints, item.ExpiresAt
		echo.ContextID = ""
		if item.ContextID != nil {
			echo.ContextID = *item.ContextID
		}
		actual, err := si.CreationDigest(a.Actor.ID, source, echo)
		if err != nil || actual != expected {
			creationFailure(w, si.ErrCreationUnavailable)
			return
		}
	}
	code := 200
	if created {
		code = 201
	}
	if receipt.Status == "NO_EFFECT" && receipt.Reason == "SOURCE_ALREADY_EXISTS" {
		code = 409
	}
	respond(w, code, map[string]any{"data": receipt})
}
func (s *server) getOwnSocialIntentCreation(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.creationAccess(w, r)
	if !ok {
		return
	}
	operation := strings.ToLower(r.PathValue("operationID"))
	if !uuidPath.MatchString(operation) {
		creationFailure(w, si.ErrCreationInvalid)
		return
	}
	receipt, e := g.ReadCreation(r.Context(), a, operation)
	if e != nil {
		creationFailure(w, e)
		return
	}
	if si.ValidateCreationReceipt(receipt, a.Actor.ID, operation) != nil {
		creationFailure(w, si.ErrCreationUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": receipt})
}
func (s *server) getOwnTaskSocialIntentDraft(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.creationAccess(w, r)
	if !ok {
		return
	}
	source := strings.ToLower(r.PathValue("taskID"))
	if !uuidPath.MatchString(source) {
		creationFailure(w, si.ErrCreationInvalid)
		return
	}
	v, e := g.ReadTaskDraft(r.Context(), a, source)
	if e != nil {
		creationFailure(w, e)
		return
	}
	if v.SchemaVersion != si.CreationSchema || v.OwnerID != a.Actor.ID || v.SourceTaskID != source || !uuidPath.MatchString(v.IntentID) || v.Intent.ID != v.IntentID || v.Intent.CreatorID != a.Actor.ID {
		creationFailure(w, si.ErrCreationUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
