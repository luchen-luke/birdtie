package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) humanEntityShareAccess(w http.ResponseWriter, r *http.Request) (connection.EntityShareAccess, connection.HumanEntityShareStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 || !uuidPath.MatchString(r.PathValue("conversationID")) {
		respondError(w, 400, "invalid_entity_share")
		return connection.EntityShareAccess{}, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if e != nil || actor.AccountType != "person" {
		respondError(w, 401, "unauthorized")
		return connection.EntityShareAccess{}, nil, false
	}
	store, ok := s.connections.(connection.HumanEntityShareStore)
	if !ok {
		respond(w, 503, map[string]any{"error": map[string]string{"code": "entity_share_unavailable", "message": "分享暂不可用，请稍后重试"}})
		return connection.EntityShareAccess{}, nil, false
	}
	return connection.EntityShareAccess{Actor: actor, SessionDigest: digest}, store, true
}
func entityShareFailure(w http.ResponseWriter, e error) bool {
	if errors.Is(e, identity.ErrUnauthorized) {
		respondError(w, 401, "unauthorized")
		return true
	}
	return connectionError(w, e)
}
func (s *server) shareHumanEntity(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.humanEntityShareAccess(w, r)
	if !ok {
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 2049))
	if e != nil {
		respondError(w, 400, "invalid_entity_share")
		return
	}
	in, e := connection.DecodeEntityShare(raw)
	if e != nil {
		respondError(w, 400, "invalid_entity_share")
		return
	}
	out, e := store.ShareHumanEntity(r.Context(), a, r.PathValue("conversationID"), in)
	if entityShareFailure(w, e) {
		return
	}
	if out.OperationID != in.OperationID || out.Message.ConversationID != r.PathValue("conversationID") || out.Message.SenderID != a.Actor.ID || out.Message.ID == "" {
		respondError(w, 503, "entity_share_unavailable")
		return
	}
	respond(w, 201, map[string]any{"data": out})
}
func (s *server) getHumanEntityShare(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.humanEntityShareAccess(w, r)
	if !ok {
		return
	}
	if !uuidPath.MatchString(r.PathValue("operationID")) {
		respondError(w, 400, "invalid_entity_share")
		return
	}
	if r.Body != nil {
		body, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(body) > 0 {
			respondError(w, 400, "invalid_entity_share")
			return
		}
	}
	out, e := store.GetHumanEntityShare(r.Context(), a, r.PathValue("conversationID"), r.PathValue("operationID"))
	if entityShareFailure(w, e) {
		return
	}
	if out.OperationID != r.PathValue("operationID") || out.Message.ConversationID != r.PathValue("conversationID") || out.Message.SenderID != a.Actor.ID || out.Message.ID == "" {
		respondError(w, 503, "entity_share_unavailable")
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
