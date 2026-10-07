package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/agentbusiness"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func businessKnowledgeFailure(w http.ResponseWriter, e error) {
	status, code, message := 503, "business_knowledge_unavailable", "暂时无法核实商家资料，请重新读取。商家 Agent 和模型尚不可用。"
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "请重新登录。"
		w.Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(e, businessconsole.ErrForbidden):
		status, code, message = 403, "business_knowledge_forbidden", "当前身份不能读取这家商家的管理资料。"
	case errors.Is(e, businessconsole.ErrNotFound):
		status, code, message = 404, "business_knowledge_not_found", "未找到当前可管理的商家。"
	case errors.Is(e, agentbusiness.ErrChanged):
		status, code, message = 409, "business_knowledge_changed", "资料或权限已变化，请重新提问。"
	case errors.Is(e, agentbusiness.ErrInvalid):
		status, code, message = 400, "invalid_business_knowledge_query", "请检查问题和场地。"
	}
	respond(w, status, map[string]any{"error": code, "message": message})
}

// Root registers only a /me management route, never public /agent invocation.
func (s *server) askOwnBusinessKnowledge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 || len(r.Header.Values("X-Birdtie-Business-Workspace")) > 0 || !businessconsole.ValidID(r.PathValue("businessID")) {
		businessKnowledgeFailure(w, agentbusiness.ErrInvalid)
		return
	}
	store, ok := s.catalog.(agentbusiness.Store)
	if !ok {
		businessKnowledgeFailure(w, agentbusiness.ErrUnavailable)
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		respond(w, 415, map[string]any{"error": "json_required", "message": "请提交JSON格式。"})
		return
	}
	if r.Body == nil {
		businessKnowledgeFailure(w, agentbusiness.ErrInvalid)
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 4097))
	if e != nil {
		businessKnowledgeFailure(w, agentbusiness.ErrInvalid)
		return
	}
	q, e := agentbusiness.DecodeQuery(body)
	if e != nil {
		businessKnowledgeFailure(w, e)
		return
	}
	actor, digest, e := s.organizationAgentActor(r)
	if e != nil {
		businessKnowledgeFailure(w, e)
		return
	}
	if digest == ([32]byte{}) {
		businessKnowledgeFailure(w, identity.ErrUnauthorized)
		return
	}
	if actor.AccountType != "person" {
		businessKnowledgeFailure(w, businessconsole.ErrForbidden)
		return
	}
	a := businessconsole.Access{SessionDigest: digest, ActingPersonID: actor.ID, BusinessID: r.PathValue("businessID")}
	answer, e := agentbusiness.NewService(store).Answer(r.Context(), a, q)
	if e != nil {
		businessKnowledgeFailure(w, e)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": answer})
	if e != nil {
		businessKnowledgeFailure(w, agentbusiness.ErrUnavailable)
		return
	}
	// Exact native source materialization, then current non-refreshing session.
	if e = store.ValidateOwnBusinessKnowledge(r.Context(), a, answer.SourceVersion); e != nil {
		businessKnowledgeFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		businessKnowledgeFailure(w, agentbusiness.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(encoded, '\n'))
}
