package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"net/http"
)

func placeHistoryFailure(w http.ResponseWriter, e error) {
	status, code, msg := 503, "place_history_unavailable", "地点公开摘要暂不可用，请稍后重试"
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, msg = 401, "unauthorized", "登录已失效，请重新登录"
	case errors.Is(e, ph.ErrNotFound):
		status, code, msg = 404, "not_found", "地点当前不可见"
	case errors.Is(e, ph.ErrInvalid):
		status, code, msg = 400, "invalid_place_history", "地点公开摘要参数无效"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func (s *server) getPublicPlaceSocialHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		placeHistoryFailure(w, ph.ErrInvalid)
		return
	}
	id := r.PathValue("placeID")
	if !uuidPath.MatchString(id) {
		placeHistoryFailure(w, ph.ErrInvalid)
		return
	}
	var a ph.Access
	if len(r.Header.Values("Authorization")) > 0 {
		if s.access == nil {
			placeHistoryFailure(w, ph.ErrUnavailable)
			return
		}
		actor, digest, e := s.actor(r, true)
		if e != nil {
			placeHistoryFailure(w, e)
			return
		}
		a = ph.Access{Actor: actor, SessionDigest: digest}
	}
	store, ok := s.catalog.(ph.Store)
	if !ok || store == nil {
		placeHistoryFailure(w, ph.ErrUnavailable)
		return
	}
	out, e := store.GetPublicPlaceSocialHistory(r.Context(), a, id)
	if e != nil {
		placeHistoryFailure(w, e)
		return
	}
	if out.PlaceID != id || ph.ValidateSummary(out) != nil {
		placeHistoryFailure(w, ph.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
