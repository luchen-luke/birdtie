package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"strings"
)

func publicationFailure(w http.ResponseWriter, e error) {
	status, code, msg := 503, "publication_unavailable", "公开记录服务暂不可用，请稍后重试"
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, msg = 401, "unauthorized", "登录已失效，请重新登录"
	case errors.Is(e, content.ErrNotFound):
		status, code, msg = 404, "not_found", "当前记录不可用"
	case errors.Is(e, content.ErrConflict):
		status, code, msg = 409, "publication_conflict", "记录、地点或登录状态已变化，请重新查看并确认"
	case errors.Is(e, content.ErrInvalid):
		status, code, msg = 400, "invalid_publication", "请检查公开确认内容与格式"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func (s *server) publicationActor(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if s.access == nil {
		publicationFailure(w, content.ErrUnavailable)
		return identity.Actor{}, [32]byte{}, false
	}
	actor, digest, e := s.actor(r, true)
	if e != nil {
		publicationFailure(w, e)
		return identity.Actor{}, [32]byte{}, false
	}
	if actor.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		respond(w, 403, map[string]any{"error": map[string]string{"code": "person_account_required", "message": "请使用本人个人账号管理记录"}})
		return identity.Actor{}, [32]byte{}, false
	}
	return actor, digest, true
}
func (s *server) publicationGateway(w http.ResponseWriter) (content.HumanMomentPublicationStore, bool) {
	store, ok := s.content.(content.HumanMomentPublicationStore)
	if !ok || store == nil {
		publicationFailure(w, content.ErrUnavailable)
		return nil, false
	}
	return store, true
}
func (s *server) previewMomentPublication(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.publicationActor(w, r)
	if !ok {
		return
	}
	store, ok := s.publicationGateway(w)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	id := r.PathValue("momentID")
	if !uuidPath.MatchString(id) {
		publicationFailure(w, content.ErrInvalid)
		return
	}
	p, e := store.PreviewHumanMomentPublication(r.Context(), digest, actor, id)
	if e != nil {
		publicationFailure(w, e)
		return
	}
	if content.ValidateMomentPublicationPreview(p, id) != nil {
		publicationFailure(w, content.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": p})
}
func (s *server) publishMomentPublication(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.publicationActor(w, r)
	if !ok {
		return
	}
	store, ok := s.publicationGateway(w)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	id := r.PathValue("momentID")
	if !uuidPath.MatchString(id) || r.Body == nil {
		publicationFailure(w, content.ErrInvalid)
		return
	}
	hs := r.Header.Values("Content-Type")
	if len(hs) != 1 {
		publicationFailure(w, content.ErrInvalid)
		return
	}
	media, params, e := mime.ParseMediaType(hs[0])
	if e != nil || !strings.EqualFold(media, "application/json") {
		publicationFailure(w, content.ErrInvalid)
		return
	}
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			publicationFailure(w, content.ErrInvalid)
			return
		}
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if e != nil {
		publicationFailure(w, content.ErrInvalid)
		return
	}
	in, e := content.DecodeMomentPublication(raw)
	if e != nil {
		publicationFailure(w, e)
		return
	}
	receipt, e := store.PublishHumanMoment(r.Context(), digest, actor, id, in)
	if e != nil {
		publicationFailure(w, e)
		return
	}
	if receipt.MomentID != id || !uuidPath.MatchString(receipt.PlaceID) || receipt.Revision != in.Revision+1 || receipt.Status != "published" || receipt.PublishedAt.IsZero() {
		publicationFailure(w, content.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": receipt})
}
