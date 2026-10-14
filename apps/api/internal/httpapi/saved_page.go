package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/saved"
)

func savedPageFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(err, saved.ErrInvalidPage):
		respondError(w, 400, "invalid_saved_page")
	case errors.Is(err, saved.ErrPageChanged):
		respondError(w, 409, "saved_page_changed")
	default:
		respondError(w, 503, "saved_page_unavailable")
	}
}

func (s *server) listSavedPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, digest, err := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "personal_saved_required")
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || (r.URL.RawQuery != "" && len(q) == 0) || len(q) > 4 {
		savedPageFailure(w, saved.ErrInvalidPage)
		return
	}
	for key, values := range q {
		if len(values) != 1 || key != "cursor" && key != "q" && key != "kind" && key != "cityId" || key == "cursor" && values[0] == "" {
			savedPageFailure(w, saved.ErrInvalidPage)
			return
		}
	}
	query, err := saved.NormalizeQuery(saved.Query{Text: q.Get("q"), Kind: q.Get("kind"), CityID: q.Get("cityId")})
	if err != nil || query.Filtered() && !saved.ValidSearchCursor(q.Get("cursor")) || !query.Filtered() && !saved.ValidPageCursor(q.Get("cursor")) {
		savedPageFailure(w, saved.ErrInvalidPage)
		return
	}
	store, ok := s.saved.(saved.HumanPageStore)
	if !ok || nilConnectionRequestReader(store) {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	search, currentSearch := s.saved.(saved.HumanSearchPageStore)
	if query.Filtered() && (!currentSearch || nilConnectionRequestReader(search)) {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	var page saved.Page
	if currentSearch && !nilConnectionRequestReader(search) {
		page, err = search.SearchHumanSavedPage(r.Context(), digest, actor, query, q.Get("cursor"))
	} else {
		page, err = store.ListHumanSavedPage(r.Context(), digest, actor, q.Get("cursor"))
	}
	if err != nil {
		savedPageFailure(w, err)
		return
	}
	if page.Items == nil {
		page.Items = []saved.Item{}
	}
	if query.Filtered() {
		err = saved.ValidateSearchPage(page, query, q.Get("cursor"))
	} else {
		err = saved.ValidatePage(page, q.Get("cursor"))
	}
	if err != nil {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	payload := map[string]any{"data": page}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > 128*1024 {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	if !currentSearch {
		s.respondHumanSocialNow(w, r, digest, actor, payload)
		return
	}
	if err = search.ValidateHumanSavedSearchPage(r.Context(), digest, actor, query, q.Get("cursor"), page); err != nil {
		savedPageFailure(w, err)
		return
	}
	gate, ok := s.access.(interface {
		ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error
	})
	if !ok || nilConnectionRequestReader(gate) {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	if err = gate.ValidateHumanSocialResponse(r.Context(), digest, actor); err != nil {
		savedPageFailure(w, err)
		return
	}
	if r.Context().Err() != nil {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}

func (s *server) getSavedTarget(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.savedHumanActor(w, r)
	if !ok {
		return
	}
	kind, target := r.PathValue("kind"), strings.ToLower(r.PathValue("targetID"))
	if (kind != "place" && kind != "activity" && kind != "group") || target == "" || !saved.ValidPageCursor(target) || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		savedPageFailure(w, saved.ErrInvalidPage)
		return
	}
	store, ok := s.saved.(saved.HumanTargetStore)
	if !ok || nilConnectionRequestReader(store) {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	item, err := store.GetHumanSavedTarget(r.Context(), d, a, kind, target)
	if err != nil {
		savedPageFailure(w, err)
		return
	}
	if item != nil && (item.Kind != kind || item.TargetID != target || saved.ValidatePage(saved.Page{Items: []saved.Item{*item}}, "") != nil) {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	payload := map[string]any{"data": item}
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > 128*1024 {
		savedPageFailure(w, saved.ErrPageUnavailable)
		return
	}
	s.respondHumanSocialNow(w, r, d, a, payload)
}
