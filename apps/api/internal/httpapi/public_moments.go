package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func publicMomentFailure(w http.ResponseWriter, err error) {
	status, code, message := 503, "public_moment_unavailable", "公开动态暂不可用，请稍后重新读取"
	switch {
	case errors.Is(err, content.ErrInvalid):
		status, code, message = 400, "invalid_public_moment", "公开动态请求无效"
	case errors.Is(err, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "登录已失效，请重新登录"
	case errors.Is(err, content.ErrNotFound):
		status, code, message = 404, "not_found", "动态当前不可查看"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *server) getPublicMoment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 || !uuidPath.MatchString(r.PathValue("momentID")) {
		publicMomentFailure(w, content.ErrInvalid)
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			publicMomentFailure(w, content.ErrInvalid)
			return
		}
	}
	var access content.PublicMomentAccess
	if len(r.Header.Values("Authorization")) > 0 {
		actor, digest, err := s.actor(r, true)
		if err != nil || actor.AccountType != "person" {
			publicMomentFailure(w, identity.ErrUnauthorized)
			return
		}
		access = content.PublicMomentAccess{Actor: actor, SessionDigest: digest}
	}
	store, ok := s.catalog.(content.PublicMomentStore)
	if !ok {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	id := r.PathValue("momentID")
	initial, err := store.GetPublicMoment(r.Context(), access, id)
	if err != nil || content.ValidatePublicMoment(initial, id) != nil {
		if err == nil {
			err = content.ErrUnavailable
		}
		publicMomentFailure(w, err)
		return
	}
	if access.Actor.ID != "" {
		actor, digest, err := s.actor(r, true)
		if err != nil {
			publicMomentFailure(w, err)
			return
		}
		if actor.ID != access.Actor.ID || actor.AccountType != access.Actor.AccountType || digest != access.SessionDigest {
			publicMomentFailure(w, content.ErrUnavailable)
			return
		}
	}
	// Authenticate can wait on Session UPDATE. Re-read after that wait with no
	// later authentication/resource lock before emitting the public payload.
	current, err := store.GetPublicMoment(r.Context(), access, id)
	if err != nil {
		publicMomentFailure(w, err)
		return
	}
	if !reflect.DeepEqual(initial, current) || r.Context().Err() != nil {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	encoded, err := json.Marshal(map[string]any{"data": current})
	if err != nil {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}
