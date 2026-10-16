package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
)

type inboxPersonTargetStore interface {
	GetInboxPersonTarget(context.Context, [32]byte, identity.Actor, string) (map[string]any, error)
}

func (s *server) readInboxPersonTarget(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "itemID") || !humanControlNoBody(w, r) {
		return
	}
	if r.URL.RawQuery != "" {
		momentControlFailure(w, identity.ErrControlInvalid)
		return
	}
	p, ok := s.inbox.(inboxPersonTargetStore)
	if !ok {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	first, e := p.GetInboxPersonTarget(r.Context(), d, a, r.PathValue("itemID"))
	if e != nil {
		if errors.Is(e, inbox.ErrNotFound) {
			respondError(w, http.StatusNotFound, "not_found")
		} else {
			momentControlFailure(w, e)
		}
		return
	}
	if !validPersonFollowTarget(first) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	proof := inboxPersonTargetProof{p, d, a, r.PathValue("itemID"), first["accountId"].(string)}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": first}, proof, true)
}

func validPersonFollowTarget(v map[string]any) bool {
	id, ok := v["accountId"].(string)
	return len(v) == 1 && ok && identity.HumanControlUUID(id)
}

type inboxPersonTargetProof struct {
	store        inboxPersonTargetStore
	d            [32]byte
	a            identity.Actor
	id, expected string
}

func (p inboxPersonTargetProof) RevalidateHumanControl(ctx context.Context) error {
	v, e := p.store.GetInboxPersonTarget(ctx, p.d, p.a, p.id)
	if errors.Is(e, inbox.ErrNotFound) {
		return identity.ErrControlChanged
	}
	if e != nil {
		return e
	}
	if !validPersonFollowTarget(v) || v["accountId"] != p.expected {
		return identity.ErrControlChanged
	}
	return nil
}
