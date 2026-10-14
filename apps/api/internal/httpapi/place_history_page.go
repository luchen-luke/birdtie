package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
)

func historyPageQuery(r *http.Request) (ph.PageQuery, error) {
	q := ph.PageQuery{PlaceID: r.PathValue("placeID")}
	if !uuidPath.MatchString(q.PlaceID) || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		return q, ph.ErrInvalid
	}
	v, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return q, ph.ErrInvalid
	}
	for k, values := range v {
		if (k != "include" && k != "cursor") || len(values) != 1 || values[0] == "" {
			return q, ph.ErrInvalid
		}
	}
	if v.Has("include") {
		if v.Get("include") != "public-author" {
			return q, ph.ErrInvalid
		}
		q.WithAuthor = true
	}
	if v.Has("cursor") {
		c, e := ph.DecodePageCursor(v.Get("cursor"), q.PlaceID, q.WithAuthor)
		if e != nil {
			return q, e
		}
		q.Cursor = &c
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			return q, ph.ErrInvalid
		}
	}
	return q, nil
}
func historyPageFailure(w http.ResponseWriter, e error) {
	if errors.Is(e, ph.ErrChanged) {
		respond(w, 409, map[string]any{"error": map[string]string{"code": "place_history_changed", "message": "公开分享已更新，请重新读取。"}})
		return
	}
	placeHistoryFailure(w, e)
}
func (s *server) getPublicPlaceHistoryMomentsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, e := historyPageQuery(r)
	if e != nil {
		historyPageFailure(w, e)
		return
	}
	var a ph.Access
	if len(r.Header.Values("Authorization")) > 0 {
		if s.access == nil {
			historyPageFailure(w, ph.ErrUnavailable)
			return
		}
		actor, digest, e := s.actor(r, true)
		if e != nil {
			historyPageFailure(w, e)
			return
		}
		a = ph.Access{Actor: actor, SessionDigest: digest}
	}
	store, ok := s.catalog.(ph.PageStore)
	if !ok || store == nil {
		historyPageFailure(w, ph.ErrUnavailable)
		return
	}
	out, e := store.ReadPublicPlaceHistoryPage(r.Context(), a, q)
	if e != nil {
		historyPageFailure(w, e)
		return
	}
	if ph.ValidatePage(out, q) != nil {
		historyPageFailure(w, ph.ErrUnavailable)
		return
	}
	// Encode first, then validate the exact already encoded native proof. No
	// Authenticate/idle renewal, model or mutating request follows this fence.
	encoded, e := json.Marshal(map[string]any{"data": out})
	if e != nil {
		historyPageFailure(w, ph.ErrUnavailable)
		return
	}
	if e = store.RevalidatePublicPlaceHistoryPage(r.Context(), a, q, out); e != nil || r.Context().Err() != nil {
		if e == nil {
			e = ph.ErrUnavailable
		}
		historyPageFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}
