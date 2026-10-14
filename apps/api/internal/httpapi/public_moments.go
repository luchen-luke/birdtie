package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mphoto "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
)

// The original bare endpoint retains its closed v1 wire. Only this explicit
// singleton opt-in can select v2; unknown/duplicate selectors grant nothing.
func publicAuthorOptIn(r *http.Request) (bool, error) {
	if r.URL.ForceQuery {
		return false, content.ErrInvalid
	}
	if r.URL.RawQuery == "" {
		return false, nil
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query) != 1 || len(query["include"]) != 1 || query["include"][0] != "public-author" {
		return false, content.ErrInvalid
	}
	return true, nil
}

func publicMomentFailure(w http.ResponseWriter, err error) {
	status, code, message := 503, "public_moment_unavailable", "公开动态暂不可用，请稍后重新读取"
	switch {
	case errors.Is(err, content.ErrInvalid):
		status, code, message = 400, "invalid_public_moment", "公开动态请求无效"
	case errors.Is(err, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "登录已失效，请重新登录"
	case errors.Is(err, content.ErrNotFound), errors.Is(err, mphoto.ErrNotFound), errors.Is(err, mphoto.ErrForbidden):
		status, code, message = 404, "not_found", "动态当前不可查看"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *server) getPublicMoment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "X-Birdtie-Public-Moment-Media")
	media := false
	if fields := r.Header.Values("X-Birdtie-Public-Moment-Media"); len(fields) != 0 {
		if len(fields) != 1 || fields[0] != "photo-v1" {
			publicMomentFailure(w, content.ErrInvalid)
			return
		}
		media = true
	}
	withAuthor, err := publicAuthorOptIn(r)
	if err != nil || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 || !uuidPath.MatchString(r.PathValue("momentID")) {
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
		if s.access == nil {
			publicMomentFailure(w, content.ErrUnavailable)
			return
		}
		actor, digest, err := s.actor(r, true)
		if err != nil || actor.AccountType != "person" {
			publicMomentFailure(w, identity.ErrUnauthorized)
			return
		}
		access = content.PublicMomentAccess{Actor: actor, SessionDigest: digest}
	}
	id := r.PathValue("momentID")
	if media {
		s.respondPublicMomentWithPhoto(w, r, access, id, withAuthor)
		return
	}
	read := func() (any, error) {
		if withAuthor {
			store, ok := s.catalog.(content.PublicMomentWithAuthorStore)
			if !ok {
				return nil, content.ErrUnavailable
			}
			out, err := store.GetPublicMomentWithAuthor(r.Context(), access, id)
			if err == nil && content.ValidatePublicMomentWithAuthor(out, id) != nil {
				err = content.ErrUnavailable
			}
			return out, err
		}
		store, ok := s.catalog.(content.PublicMomentStore)
		if !ok {
			return nil, content.ErrUnavailable
		}
		out, err := store.GetPublicMoment(r.Context(), access, id)
		if err == nil && content.ValidatePublicMoment(out, id) != nil {
			err = content.ErrUnavailable
		}
		return out, err
	}
	initial, err := read()
	if err != nil {
		publicMomentFailure(w, err)
		return
	}
	// Freeze the actual initial wire before the authentication wait. A Store
	// retaining mutable slices/pointers must not rewrite the comparison input.
	initialWire, err := json.Marshal(map[string]any{"data": initial})
	if err != nil {
		publicMomentFailure(w, content.ErrUnavailable)
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
	current, err := read()
	if err != nil {
		publicMomentFailure(w, err)
		return
	}
	if !reflect.DeepEqual(initial, current) || r.Context().Err() != nil {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	encoded, err := json.Marshal(map[string]any{"data": current})
	if err != nil || !bytes.Equal(initialWire, encoded) {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}

func (s *server) respondPublicMomentWithPhoto(w http.ResponseWriter, r *http.Request, access content.PublicMomentAccess, id string, withAuthor bool) {
	store, ok := s.catalog.(content.PublicMomentWithPhotoStore)
	if !ok {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	receipt, err := store.CapturePublicMomentWithPhoto(r.Context(), access, id, withAuthor)
	if err != nil {
		publicMomentFailure(w, err)
		return
	}
	if receipt.IncludeAuthor != withAuthor || content.ValidatePublicMomentWithPhoto(receipt.View, id) != nil || (!withAuthor && (receipt.View.AuthorID != nil || receipt.View.AuthorPublicProfile != nil)) {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	// Freeze all optional pointer values before any further authentication wait.
	if p := receipt.View.Photo; p != nil {
		copy := *p
		receipt.View.Photo = &copy
	}
	if p := receipt.View.AuthorID; p != nil {
		copy := *p
		receipt.View.AuthorID = &copy
	}
	if p := receipt.View.AuthorPublicProfile; p != nil {
		copy := *p
		receipt.View.AuthorPublicProfile = &copy
	}
	if access.Actor.ID != "" {
		actor, digest, e := s.actor(r, true)
		if e != nil {
			publicMomentFailure(w, e)
			return
		}
		if actor.ID != access.Actor.ID || actor.AccountType != access.Actor.AccountType || digest != access.SessionDigest {
			publicMomentFailure(w, content.ErrUnavailable)
			return
		}
	}
	// The native finalizer retains the actual original Session frame, base proof
	// and lease. Optional retirement never borrows a newer photograph or lease.
	current, err := store.FinalizePublicMomentWithPhoto(r.Context(), access, receipt)
	if err != nil {
		publicMomentFailure(w, err)
		return
	}
	if content.ValidatePublicMomentWithPhoto(current, id) != nil || r.Context().Err() != nil || !current.ValidUntil.After(time.Now().UTC()) {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	before, after := receipt.View, current
	before.Photo, after.Photo = nil, nil
	if !reflect.DeepEqual(before, after) {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	if current.Photo != nil && (receipt.View.Photo == nil || *current.Photo != *receipt.View.Photo) {
		publicMomentFailure(w, content.ErrUnavailable)
		return
	}
	if current.Photo != nil && !current.Photo.ExpiresAt.After(time.Now().UTC()) {
		current.Photo = nil
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
