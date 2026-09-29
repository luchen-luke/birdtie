package httpapi

import (
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/inbox"
)

func (s *server) listInbox(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	items, err := s.inbox.ListInbox(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) markInboxRead(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("itemID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_item_id")
		return
	}
	item, err := s.inbox.ReadInboxItem(r.Context(), actor.ID, id)
	if errors.Is(err, inbox.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": item})
	}
}
