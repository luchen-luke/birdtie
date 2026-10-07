package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

func (s *server) listInbox(w http.ResponseWriter, r *http.Request) {
	if !socialInboxWire(w, r) {
		return
	}
	actor, digest, err := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "personal_inbox_required")
		return
	}
	store, ok := s.inbox.(interface {
		ListHumanInbox(context.Context, [32]byte, identity.Actor) ([]inbox.Item, error)
	})
	if !ok {
		socialNowReadError(w, socialnow.ErrUnavailable)
		return
	}
	items, err := store.ListHumanInbox(r.Context(), digest, actor)
	if err != nil {
		socialNowReadError(w, err)
		return
	}
	s.respondHumanSocialNow(w, r, digest, actor, map[string]any{"data": items})
}

func (s *server) markInboxRead(w http.ResponseWriter, r *http.Request) {
	if !socialInboxWire(w, r) {
		return
	}
	actor, digest, err := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, err) {
		return
	}
	id := r.PathValue("itemID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_item_id")
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "personal_inbox_required")
		return
	}
	store, ok := s.inbox.(interface {
		ReadHumanInboxItem(context.Context, [32]byte, identity.Actor, string) (inbox.Item, error)
	})
	if !ok {
		socialNowReadError(w, socialnow.ErrUnavailable)
		return
	}
	item, err := store.ReadHumanInboxItem(r.Context(), digest, actor, id)
	if errors.Is(err, inbox.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		socialNowReadError(w, err)
	} else {
		s.respondHumanSocialNow(w, r, digest, actor, map[string]any{"data": item})
	}
}

func socialInboxWire(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusForbidden, "personal_inbox_required")
		return false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, http.StatusBadRequest, "inbox_query_not_supported")
		return false
	}
	return true
}
