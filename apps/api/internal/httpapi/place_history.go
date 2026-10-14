package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	ph "github.com/birdtie/birdtie/apps/api/internal/placehistory"
	"net/http"
	"time"
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
	withAuthor, e := publicAuthorOptIn(r)
	if e != nil || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
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
	if withAuthor {
		s.getPublicPlaceSocialHistoryWithAuthor(w, r, a, id)
		return
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

func publicHistoryAuthorSnapshot(s ph.SummaryWithAuthor) ([]byte, error) {
	s.CheckedAt, s.WindowStart = time.Time{}, time.Time{}
	if s.Suitability != nil {
		profile := *s.Suitability
		profile.CheckedAt = time.Time{}
		s.Suitability = &profile
	}
	return json.Marshal(struct {
		SourceBinding string
		Summary       ph.SummaryWithAuthor
	}{s.SourceBinding, s})
}

func (s *server) getPublicPlaceSocialHistoryWithAuthor(w http.ResponseWriter, r *http.Request, access ph.Access, id string) {
	store, ok := s.catalog.(ph.StoreWithAuthor)
	if !ok {
		placeHistoryFailure(w, ph.ErrUnavailable)
		return
	}
	initial, err := store.GetPublicPlaceSocialHistoryWithAuthor(r.Context(), access, id)
	if err != nil || initial.PlaceID != id || ph.ValidateSummaryWithAuthor(initial) != nil {
		if err == nil {
			err = ph.ErrUnavailable
		}
		placeHistoryFailure(w, err)
		return
	}
	initialSnapshot, err := publicHistoryAuthorSnapshot(initial)
	if err != nil {
		placeHistoryFailure(w, ph.ErrUnavailable)
		return
	}
	if access.Actor.ID != "" {
		actor, digest, err := s.actor(r, true)
		if err != nil {
			placeHistoryFailure(w, err)
			return
		}
		if actor.ID != access.Actor.ID || actor.AccountType != access.Actor.AccountType || digest != access.SessionDigest {
			placeHistoryFailure(w, ph.ErrUnavailable)
			return
		}
	}
	// No authentication or mutating idle renewal after this current native read.
	current, err := store.GetPublicPlaceSocialHistoryWithAuthor(r.Context(), access, id)
	if err != nil || current.PlaceID != id || ph.ValidateSummaryWithAuthor(current) != nil || r.Context().Err() != nil {
		if err == nil {
			err = ph.ErrUnavailable
		}
		placeHistoryFailure(w, err)
		return
	}
	currentSnapshot, err := publicHistoryAuthorSnapshot(current)
	if err != nil || !bytes.Equal(initialSnapshot, currentSnapshot) {
		placeHistoryFailure(w, ph.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": current})
}
