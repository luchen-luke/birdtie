package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentcontextrelevance"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Exact self-only local Task reads. These routes never accept an owner, session,
// workspace, source body, model tool or boolean purporting to grant authority.
func (s *server) contextPurposeAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, acb.PurposeStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		contextBuilderHTTPFailure(w, acb.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return agentprofile.PrivateAccess{}, nil, false
	}
	port, ok := s.catalog.(acb.PurposeStore)
	if !ok || port == nil || s.access == nil {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return agentprofile.PrivateAccess{}, nil, false
	}
	if e != nil {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	principal, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if e != nil || principal.Type != actorref.Person || agentprofile.ValidatePrivateAccess(access) != nil {
		contextBuilderHTTPFailure(w, acb.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	return access, port, true
}

func contextPurposeObject(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	if len(raw) > 8192 {
		return nil, acb.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, acb.ErrInvalid
	}
	values := map[string]json.RawMessage{}
	for d.More() {
		token, e = d.Token()
		key, ok := token.(string)
		if e != nil || !ok {
			return nil, acb.ErrInvalid
		}
		allowed := false
		for _, k := range keys {
			if key == k {
				allowed = true
			}
		}
		if _, dup := values[key]; dup || !allowed {
			return nil, acb.ErrInvalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, acb.ErrInvalid
		}
		values[key] = value
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') || len(values) != len(keys) {
		return nil, acb.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, acb.ErrInvalid
	}
	return values, nil
}
func contextPurposeBody(w http.ResponseWriter, r *http.Request, keys ...string) ([]byte, bool) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !strings.EqualFold(media, "application/json") {
		respondError(w, 415, "json_required")
		return nil, false
	}
	if r.Body == nil {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if e != nil {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return nil, false
	}
	if _, e = contextPurposeObject(raw, keys...); e != nil {
		contextBuilderHTTPFailure(w, e)
		return nil, false
	}
	return raw, true
}
func contextPurposeNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) > 0 {
			contextBuilderHTTPFailure(w, acb.ErrInvalid)
			return false
		}
	}
	return true
}
func (s *server) previewContextPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.contextPurposeAccess(w, r)
	if !ok {
		return
	}
	raw, ok := contextPurposeBody(w, r, "agentId", "taskId", "cityId", "currentQuery", "taskUpdatedAt", "profileFields", "memoryIds", "placeIds", "activityIds", "relationshipTieIds", "policyFamilies", "deadlineAt")
	if !ok {
		return
	}
	var selection acb.PurposeSelection
	if json.Unmarshal(raw, &selection) != nil {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return
	}
	preview, e := port.PreviewOwnContextPurpose(r.Context(), a, selection)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": preview})
}
func (s *server) approveContextPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.contextPurposeAccess(w, r)
	if !ok {
		return
	}
	raw, ok := contextPurposeBody(w, r, "previewId")
	if !ok {
		return
	}
	var in struct {
		PreviewID string `json:"previewId"`
	}
	if json.Unmarshal(raw, &in) != nil || !uuidPath.MatchString(in.PreviewID) {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return
	}
	grant, e := port.ApproveOwnContextPurpose(r.Context(), a, in.PreviewID)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": grant})
}
func (s *server) readContextPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.contextPurposeAccess(w, r)
	if !ok || !contextPurposeNoBody(w, r) {
		return
	}
	id := r.PathValue("grantID")
	if !uuidPath.MatchString(id) {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return
	}
	grant, e := port.ReadOwnContextPurpose(r.Context(), a, id)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": grant})
}
func (s *server) revokeContextPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.contextPurposeAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("grantID")
	if !uuidPath.MatchString(id) {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return
	}
	raw, ok := contextPurposeBody(w, r, "expectedRevision")
	if !ok {
		return
	}
	var in struct {
		Revision int64 `json:"expectedRevision"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Revision <= 0 {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return
	}
	grant, e := port.RevokeOwnContextPurpose(r.Context(), a, id, in.Revision)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": grant})
}

// RuntimeContextResponse is an ephemeral local read-only result, not a new
// durable knowledge ledger, export ticket, inference or action approval.
type runtimeContextFact struct {
	Kind             string          `json:"kind"`
	SourceID         string          `json:"sourceId"`
	Field            string          `json:"field,omitempty"`
	Text             string          `json:"text"`
	DeclaredSettings json.RawMessage `json:"declaredSettings,omitempty"`
}
type runtimeContextResponse struct {
	SchemaVersion          string               `json:"schemaVersion"`
	TaskID                 string               `json:"taskId"`
	Answer                 string               `json:"answer"`
	City                   acb.ContextCity      `json:"city"`
	Facts                  []runtimeContextFact `json:"facts"`
	Places                 []acb.PublicPlace    `json:"places"`
	Activities             []acb.PublicActivity `json:"activities"`
	Relationships          []acb.ContextTie     `json:"relationships"`
	Sources                []acb.Source         `json:"sources"`
	ExpiresAt              time.Time            `json:"expiresAt"`
	ModelAccess            string               `json:"modelAccess"`
	MemoryPromotionAllowed bool                 `json:"memoryPromotionAllowed"`
}

func consumeLocalTaskContext(b acb.Bundle) runtimeContextResponse {
	out := runtimeContextResponse{SchemaVersion: "agent-task-context-response-v1", TaskID: b.TaskID, City: *b.City, Places: b.Places, Activities: b.Activities, Relationships: b.Relationships,
		Sources: append([]acb.Source(nil), b.Sources...), ExpiresAt: b.ExpiresAt, ModelAccess: "UNAVAILABLE", Facts: []runtimeContextFact{}}
	labels := map[string]string{"personalPreferences": "你填写的个人偏好", "socialPreferences": "你填写的社交偏好", "availability": "你填写的可用时间", "preferredActivityTypes": "你填写的活动类型", "travelPreferences": "你填写的出行偏好", "interactionPreferences": "你填写的互动偏好", "privateCityHistory": "你填写的城市记录", "languagePreferences": "你填写的语言偏好", "agentNotes": "你填写的补充说明"}
	for _, key := range acb.PrivateFieldKeys() {
		if value, ok := b.Profile[key]; ok {
			var text string
			if json.Unmarshal(value, &text) != nil {
				var list []string
				if json.Unmarshal(value, &list) == nil {
					text = strings.Join(list, "、")
				}
			}
			if text == "" {
				text = "尚未填写"
			}
			out.Facts = append(out.Facts, runtimeContextFact{Kind: "DECLARED_PROFILE", SourceID: b.Agent.AgentID, Field: key, Text: labels[key] + "：" + text})
		}
	}
	for _, memory := range b.Memories {
		out.Facts = append(out.Facts, runtimeContextFact{Kind: "EXPLICIT_MEMORY", SourceID: memory.ID, Text: "你明确记录的内容：" + memory.Summary})
	}
	for _, policy := range b.Policies {
		label := map[string]string{"ATTENTION": "注意范围", "SOCIAL": "社交偏好", "AUTONOMY": "自主操作边界"}[string(policy.Family)]
		text := "本轮读取你当前的" + label + "设置；任何提交仍需单独授权"
		if string(policy.Family) == "AUTONOMY" {
			var value struct {
				Level string `json:"level"`
			}
			_ = json.Unmarshal(policy.Settings, &value)
			level := map[string]string{"LEVEL_0_OBSERVE": "仅观察", "LEVEL_1_ASSIST": "提供建议", "LEVEL_2_PREPARE": "准备待确认内容", "LEVEL_3_DELEGATE": "委托范围"}[value.Level]
			text = "你当前的自主操作边界为「" + level + "」；本轮只整理信息，不执行或提交操作"
		}
		out.Facts = append(out.Facts, runtimeContextFact{Kind: "DECLARED_POLICY", SourceID: b.Agent.AgentID + ":" + string(policy.Family), Text: text, DeclaredSettings: append(json.RawMessage(nil), policy.Settings...)})
	}
	for i := range out.Sources {
		out.Sources[i].RowToken = ""
	}
	out.Answer = fmt.Sprintf("本轮任务是「%s」。已按你批准的具体范围整理 %d 项资料、%d 条明确记录、%d 个地点和 %d 个活动；%d 条关系仅表示目前的好友状态。城市时区为 %s。这些信息只用于当前任务，不代表到访、出席或新的承诺。", b.CurrentQuery, len(b.Profile), len(b.Memories), len(b.Places), len(b.Activities), len(b.Relationships), b.City.TimeZone)
	return out
}
func (s *server) runtimeContextPurpose(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.contextPurposeAccess(w, r)
	if !ok {
		return
	}
	raw, ok := contextPurposeBody(w, r, "grantId")
	if !ok {
		return
	}
	var in struct {
		GrantID string `json:"grantId"`
	}
	if json.Unmarshal(raw, &in) != nil || !uuidPath.MatchString(in.GrantID) {
		contextBuilderHTTPFailure(w, acb.ErrInvalid)
		return
	}
	grant, e := port.ReadOwnContextPurpose(r.Context(), a, in.GrantID)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	store, ok := s.catalog.(acb.Store)
	if !ok {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	service, e := acb.NewService(store)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	agent, e := service.Resolve(r.Context(), a)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	sel := grant.Selection
	deadline := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond)
	if sel.DeadlineAt.Before(deadline) {
		deadline = sel.DeadlineAt
	}
	request := acb.Request{Access: a, Agent: agent, TaskID: sel.TaskID, CityID: sel.CityID, CurrentQuery: sel.CurrentQuery, TaskUpdatedAt: sel.TaskUpdatedAt,
		RequestID: w.Header().Get("X-Request-ID"), Mode: acb.MachineTaskContext, Selection: acb.ExactTaskContext, ProfileFields: sel.ProfileFields, MemoryIDs: sel.MemoryIDs,
		PlaceIDs: sel.PlaceIDs, ActivityIDs: sel.ActivityIDs, RelationshipTieIDs: sel.RelationshipTieIDs, PolicyFamilies: sel.PolicyFamilies, PurposeGrantID: grant.ID, PurposeDeadlineAt: sel.DeadlineAt, DeadlineAt: deadline}
	if request.RequestID == "" {
		request.RequestID = "local-task-context:" + sel.TaskID
	}
	relevance, e := agentcontextrelevance.NewService(store)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	built, e := relevance.Retrieve(r.Context(), request)
	if e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	result, e := consumeRelatedBudgetedTaskContext(built.ProjectionBundle(), agentcontextadapter.DefaultBudget(), built.ExcludedCounts())
	if e != nil {
		contextBuilderHTTPFailure(w, acb.ErrUnavailable)
		return
	}
	// The result is composed before the final current native read; a changed
	// source/session/grant while composing cannot expose its previous contents.
	if e = relevance.Revalidate(r.Context(), a, built); e != nil {
		contextBuilderHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": result})
}
