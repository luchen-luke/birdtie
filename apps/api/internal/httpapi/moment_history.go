package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) registerMomentHistoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/moments/history", s.getOwnMomentHistory)
	mux.HandleFunc("GET /v1/moments/history", s.getPublicMomentHistory)
}

func momentHistoryQuery(r *http.Request, own bool) (content.MomentHistoryQuery, error) {
	q := content.MomentHistoryQuery{}
	if r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		return q, content.ErrInvalid
	}
	v, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return q, content.ErrInvalid
	}
	for k, values := range v {
		allowed := k == "cursor" || own && k == "status" || !own && (k == "scope" || k == "cityId" || k == "authorId" || k == "from" || k == "to")
		if !allowed || len(values) != 1 || values[0] == "" {
			return q, content.ErrInvalid
		}
	}
	q.Cursor = v.Get("cursor")
	if own {
		q.Status = v.Get("status")
		if q.Status == "" {
			q.Status = "active"
		}
	} else {
		q.Scope = v.Get("scope")
		q.CityID = v.Get("cityId")
		q.AuthorID = v.Get("authorId")
		q.From = v.Get("from")
		q.To = v.Get("to")
	}
	if content.ValidateMomentHistoryQuery(q, own) != nil {
		return q, content.ErrInvalid
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			return q, content.ErrInvalid
		}
	}
	return q, nil
}
func momentHistoryFailure(w http.ResponseWriter, e error) {
	if errors.Is(e, content.ErrMomentHistoryChanged) {
		respond(w, 409, map[string]any{"error": map[string]string{"code": "moment_history_changed", "message": "动态列表或权限已更新，请重新读取。"}})
		return
	}
	if errors.Is(e, identity.ErrControlChanged) || errors.Is(e, identity.ErrControlInvalid) || errors.Is(e, identity.ErrControlUnavailable) {
		humanControlFailure(w, e)
		return
	}
	publicMomentFailure(w, e)
}
func (s *server) getOwnMomentHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, e := momentHistoryQuery(r, true)
	if e != nil {
		momentHistoryFailure(w, e)
		return
	}
	r, actor, digest, ok := s.humanControlActor(w, r)
	if !ok {
		return
	}
	store, ok := s.content.(content.MomentHistoryStore)
	if !ok || store == nil {
		momentHistoryFailure(w, content.ErrUnavailable)
		return
	}
	p, e := store.ReadOwnMomentHistory(r.Context(), digest, actor, q)
	if e != nil {
		momentHistoryFailure(w, e)
		return
	}
	if content.ValidateOwnMomentHistoryPage(p, q, actor.ID) != nil {
		momentHistoryFailure(w, content.ErrUnavailable)
		return
	}
	b, e := json.Marshal(map[string]any{"data": p})
	if e != nil {
		momentHistoryFailure(w, content.ErrUnavailable)
		return
	}
	if e = store.RevalidateOwnMomentHistory(r.Context(), digest, actor, q, p); e != nil || r.Context().Err() != nil {
		if e == nil {
			e = content.ErrUnavailable
		}
		momentHistoryFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(b, '\n'))
}
func (s *server) getPublicMomentHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, e := momentHistoryQuery(r, false)
	if e != nil {
		momentHistoryFailure(w, e)
		return
	}
	var a content.PublicMomentAccess
	if len(r.Header.Values("Authorization")) > 0 {
		if s.access == nil {
			momentHistoryFailure(w, content.ErrUnavailable)
			return
		}
		actor, digest, e := s.actor(r, true)
		if e != nil {
			momentHistoryFailure(w, e)
			return
		}
		if actor.AccountType != "person" {
			momentHistoryFailure(w, identity.ErrUnauthorized)
			return
		}
		a = content.PublicMomentAccess{Actor: actor, SessionDigest: digest}
	}
	if (q.Scope == "following" || q.Scope == "friends") && a.Actor.ID == "" {
		momentHistoryFailure(w, identity.ErrUnauthorized)
		return
	}
	store, ok := s.catalog.(content.MomentHistoryStore)
	if !ok || store == nil {
		momentHistoryFailure(w, content.ErrUnavailable)
		return
	}
	p, e := store.ReadPublicMomentHistory(r.Context(), a, q)
	if e != nil {
		momentHistoryFailure(w, e)
		return
	}
	if content.ValidatePublicMomentHistoryPage(p, q) != nil {
		momentHistoryFailure(w, content.ErrUnavailable)
		return
	}
	b, e := json.Marshal(map[string]any{"data": p})
	if e != nil {
		momentHistoryFailure(w, e)
		return
	}
	if e = store.RevalidatePublicMomentHistory(r.Context(), a, q, p); e != nil || r.Context().Err() != nil {
		if e == nil {
			e = content.ErrUnavailable
		}
		momentHistoryFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(b, '\n'))
}
