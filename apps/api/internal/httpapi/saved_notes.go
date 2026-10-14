package httpapi

import (
	"errors"
	"net/http"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
)

func (s *server) savedHumanActor(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if s.access == nil {
		respondError(w, 503, "saved_note_unavailable")
		return identity.Actor{}, [32]byte{}, false
	}
	a, d, e := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, e) {
		return a, d, false
	}
	if a.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "person_account_required")
		return a, d, false
	}
	return a, d, true
}

func savedNoteFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		authFailed(w, e)
	case errors.Is(e, saved.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(e, saved.ErrNoteChanged):
		respondError(w, 409, "saved_note_changed")
	case errors.Is(e, saved.ErrInvalidNote):
		respondError(w, 400, "invalid_saved_note")
	default:
		respondError(w, 503, "saved_note_unavailable")
	}
}

func savedNotePath(w http.ResponseWriter, r *http.Request, operation bool) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || !saved.ValidNoteID(r.PathValue("savedID")) || (operation && !saved.ValidNoteID(r.PathValue("operationID"))) {
		respondError(w, 400, "invalid_saved_note")
		return false
	}
	return true
}

func (s *server) savedNoteGateway(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, saved.HumanNoteStore, bool) {
	a, d, ok := s.savedHumanActor(w, r)
	if !ok {
		return a, d, nil, false
	}
	port, ok := s.saved.(saved.HumanNoteStore)
	if !ok || port == nil || (reflect.ValueOf(port).Kind() == reflect.Pointer && reflect.ValueOf(port).IsNil()) {
		respondError(w, 503, "saved_note_unavailable")
		return a, d, nil, false
	}
	return a, d, port, true
}

func (s *server) getSavedNote(w http.ResponseWriter, r *http.Request) {
	a, d, port, ok := s.savedNoteGateway(w, r)
	if !ok || !savedNotePath(w, r, false) {
		return
	}
	v, e := port.GetHumanSavedNote(r.Context(), d, a, r.PathValue("savedID"))
	if e != nil {
		savedNoteFailure(w, e)
		return
	}
	s.respondHumanSocialNow(w, r, d, a, map[string]any{"data": v})
}

func (s *server) updateSavedNote(w http.ResponseWriter, r *http.Request) {
	a, d, port, ok := s.savedNoteGateway(w, r)
	if !ok || !savedNotePath(w, r, false) {
		return
	}
	var input struct {
		Note             *string `json:"note"`
		ExpectedRevision *int64  `json:"expectedRevision"`
		OperationID      string  `json:"operationId"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if input.Note == nil || input.ExpectedRevision == nil {
		respondError(w, 400, "invalid_saved_note")
		return
	}
	in, e := saved.NormalizeNoteInput(saved.NoteInput{Note: *input.Note, ExpectedRevision: *input.ExpectedRevision, OperationID: input.OperationID})
	if e != nil {
		savedNoteFailure(w, e)
		return
	}
	v, e := port.UpdateHumanSavedNote(r.Context(), d, a, r.PathValue("savedID"), in)
	if e != nil {
		savedNoteFailure(w, e)
		return
	}
	s.respondHumanSocialNow(w, r, d, a, map[string]any{"data": v})
}

func (s *server) getSavedNoteOperation(w http.ResponseWriter, r *http.Request) {
	a, d, port, ok := s.savedNoteGateway(w, r)
	if !ok || !savedNotePath(w, r, true) {
		return
	}
	v, e := port.GetHumanSavedNoteOperation(r.Context(), d, a, r.PathValue("savedID"), r.PathValue("operationID"))
	if e != nil {
		savedNoteFailure(w, e)
		return
	}
	s.respondHumanSocialNow(w, r, d, a, map[string]any{"data": v})
}

func (s *server) registerSavedNoteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/saved/{savedID}/note", s.getSavedNote)
	mux.HandleFunc("PUT /v1/me/saved/{savedID}/note", s.updateSavedNote)
	mux.HandleFunc("GET /v1/me/saved/{savedID}/note/operations/{operationID}", s.getSavedNoteOperation)
}
