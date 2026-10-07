package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"strings"
)

func orgMemoryFailure(w http.ResponseWriter, e error) {
	status, code, message := http.StatusServiceUnavailable, "organization_memory_unavailable", "暂时无法处理组织记忆，请重新读取检查结果。"
	switch {
	case errors.Is(e, agentorganizationmemory.ErrLimit):
		status, code, message = 409, "organization_memory_limit", "组织记忆已达到 128 条，请先删除不再需要的记录。"
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "请重新登录。"
		w.Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(e, agentmemory.ErrForbidden):
		status, code, message = 403, "organization_memory_forbidden", "当前身份无权管理该组织记忆。"
	case errors.Is(e, agentmemory.ErrInvalid):
		status, code, message = 400, "invalid_organization_memory", "请检查类别、内容和有效期限。"
	case errors.Is(e, agentmemory.ErrNotFound):
		status, code, message = 404, "organization_memory_not_found", "未找到当前组织的记忆。"
	case errors.Is(e, agentmemory.ErrConflict):
		status, code, message = 409, "organization_memory_version_conflict", "内容已发生变化，请重新读取并确认。"
	}
	respond(w, status, map[string]any{"error": code, "message": message})
}
func (s *server) organizationMemoryAccess(w http.ResponseWriter, r *http.Request) (agentorganizationmemory.Access, agentorganizationmemory.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	var a agentorganizationmemory.Access
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		orgMemoryFailure(w, agentmemory.ErrInvalid)
		return a, nil, false
	}
	id, e := agentmemory.NormalizeMemoryID(r.PathValue("organizationID"))
	if e != nil || id != r.PathValue("organizationID") {
		orgMemoryFailure(w, agentmemory.ErrInvalid)
		return a, nil, false
	}
	store, ok := s.catalog.(agentorganizationmemory.Store)
	if !ok {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return a, nil, false
	}
	actor, digest, e := s.organizationAgentActor(r)
	if e == nil && digest == ([32]byte{}) {
		e = identity.ErrUnauthorized
	}
	if e != nil {
		orgMemoryFailure(w, e)
		return a, nil, false
	}
	if actor.AccountType != "person" {
		orgMemoryFailure(w, agentmemory.ErrForbidden)
		return a, nil, false
	}
	a = agentorganizationmemory.Access{SessionDigest: digest, ActingPersonID: actor.ID, OrganizationID: id}
	if agentorganizationmemory.ValidateAccess(a) != nil {
		orgMemoryFailure(w, agentmemory.ErrForbidden)
		return a, nil, false
	}
	return a, store, true
}
func orgMemoryBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		respond(w, 415, map[string]any{"error": "json_required", "message": "请提交 JSON 格式。"})
		return nil, false
	}
	if r.Body == nil {
		orgMemoryFailure(w, agentmemory.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, agentmemory.MaxBodyBytes+1))
	if e != nil || len(raw) > agentmemory.MaxBodyBytes {
		orgMemoryFailure(w, agentmemory.ErrInvalid)
		return nil, false
	}
	return raw, true
}
func (s *server) listOrganizationAgentMemories(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.organizationMemoryAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) > 0 {
			orgMemoryFailure(w, agentmemory.ErrInvalid)
			return
		}
	}
	views, e := store.ListOrganizationMemories(r.Context(), a)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	now, e := store.ValidateOrganizationMemoryAccess(r.Context(), a)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if views == nil {
		views = []agentorganizationmemory.View{}
	}
	if len(views) > agentorganizationmemory.MaxRecords {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	ids := map[string]bool{}
	var agent, principal string
	for i := range views {
		v := &views[i]
		if ids[v.Memory.ID] {
			orgMemoryFailure(w, agentmemory.ErrUnavailable)
			return
		}
		ids[v.Memory.ID] = true
		if agentorganizationmemory.ValidateView(*v, a.OrganizationID) != nil || v.Memory.Status == agentmemory.StatusDeleted || (agent != "" && (agent != v.Memory.AgentID || principal != v.Memory.OwnerID)) {
			orgMemoryFailure(w, agentmemory.ErrUnavailable)
			return
		}
		agent, principal = v.Memory.AgentID, v.Memory.OwnerID
		if !v.Memory.ValidUntil.After(now) {
			v.Memory.Status = agentmemory.StatusExpired
		}
	}
	if r.Context().Err() != nil {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": views})
}
func (s *server) putOrganizationAgentMemory(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.organizationMemoryAccess(w, r)
	if !ok {
		return
	}
	id, e := agentmemory.NormalizeMemoryID(r.PathValue("memoryID"))
	if e != nil || id != r.PathValue("memoryID") {
		orgMemoryFailure(w, agentmemory.ErrInvalid)
		return
	}
	raw, ok := orgMemoryBody(w, r)
	if !ok {
		return
	}
	input, e := agentorganizationmemory.DecodePut(raw)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	view, e := store.PutOrganizationMemory(r.Context(), a, id, input)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	now, e := store.ValidateOrganizationMemoryAccess(r.Context(), a)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if agentorganizationmemory.ValidateView(view, a.OrganizationID) != nil || view.Memory.ID != id || view.Memory.Status != agentmemory.StatusActive || !view.Memory.ValidUntil.After(now) || r.Context().Err() != nil {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": view})
}
func (s *server) deleteOrganizationAgentMemory(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.organizationMemoryAccess(w, r)
	if !ok {
		return
	}
	id, e := agentmemory.NormalizeMemoryID(r.PathValue("memoryID"))
	if e != nil || id != r.PathValue("memoryID") {
		orgMemoryFailure(w, agentmemory.ErrInvalid)
		return
	}
	raw, ok := orgMemoryBody(w, r)
	if !ok {
		return
	}
	input, e := agentmemory.DecodeDeleteInput(raw)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	view, e := store.DeleteOrganizationMemory(r.Context(), a, id, input.ExpectedVersion)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if _, e = store.ValidateOrganizationMemoryAccess(r.Context(), a); e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if agentorganizationmemory.ValidateView(view, a.OrganizationID) != nil || view.Memory.ID != id || view.Memory.Status != agentmemory.StatusDeleted || strings.TrimSpace(view.Memory.Summary) != "" || r.Context().Err() != nil {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": view})
}
