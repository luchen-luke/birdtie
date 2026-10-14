package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"net/http"
)

func inboxReviewFailure(w http.ResponseWriter, e error) {
	if errors.Is(e, identity.ErrControlChanged) {
		e = inbox.ErrSourceChanged
	}
	if errors.Is(e, identity.ErrControlUnavailable) {
		e = socialnow.ErrUnavailable
	}
	inboxPageFailure(w, e)
}

func (s *server) readInboxReviewResult(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !socialInboxWire(w, r) || !messagePolicyNoBody(w, r) {
		return
	}
	a, d, e := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, e) {
		return
	}
	if a.AccountType != "person" {
		respondError(w, 403, "personal_inbox_required")
		return
	}
	id := r.PathValue("itemID")
	if !inbox.ValidPageID(id) {
		inboxPageFailure(w, inbox.ErrNotFound)
		return
	}
	st, ok := s.inbox.(inbox.HumanReviewResultStore)
	access, authOK := s.access.(socialnow.HumanSessionStore)
	if !ok || !authOK || nilConnectionRequestReader(st) || nilConnectionRequestReader(access) {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	v, e := st.ReadHumanInboxReviewResult(r.Context(), d, a, id)
	if e != nil {
		inboxReviewFailure(w, e)
		return
	}
	if !v.Valid(id) {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	if e = access.ValidateHumanSocialResponse(r.Context(), d, a); e == nil {
		e = st.ValidateHumanInboxReviewResultResponse(r.Context(), d, a, v)
	}
	if e != nil {
		inboxReviewFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
