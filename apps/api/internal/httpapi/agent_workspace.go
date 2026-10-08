package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

type organizationTaskContextStore interface {
	CaptureOrganizationTaskAuthority(context.Context, [32]byte, identity.Actor, string, string, string) (agentruntime.ContextSnapshot, error)
	BindOrganizationTaskQuerySources(context.Context, agentruntime.ContextSnapshot, string) (agentruntime.ContextSnapshot, error)
	SaveOrganizationTaskContext(context.Context, agentruntime.ContextSnapshot, agentworkspace.Task) (agentworkspace.Task, agentruntime.ContextSnapshot, error)
	UpdateOrganizationTaskContext(context.Context, agentruntime.ContextSnapshot, agentworkspace.Task) (agentworkspace.Task, agentruntime.ContextSnapshot, error)
	ReadOrganizationTaskContext(context.Context, agentruntime.ContextSnapshot, string) (agentworkspace.Task, agentruntime.ContextSnapshot, error)
	ReadOrganizationTasksContext(context.Context, agentruntime.ContextSnapshot) ([]agentworkspace.Task, agentruntime.ContextSnapshot, error)
	RevalidateOrganizationTaskContext(context.Context, agentruntime.ContextSnapshot, []agentworkspace.Task) error
}
type organizationTaskSnapshotKey struct{}

// Request-local facade: the server holds only dependency interfaces and scalar
// startup options, no mutex/state is copied. Each original special-intent path
// uses this same facade; no shared server field is mutated.
type organizationTaskContextAgent struct {
	agentworkspace.Store
	organizationTaskContextStore
	current   agentruntime.ContextSnapshot
	principal string
}

func (a *organizationTaskContextAgent) GetTask(ctx context.Context, principal, id string) (agentworkspace.Task, error) {
	if principal != a.principal {
		return agentworkspace.Task{}, organization.ErrForbidden
	}
	task, next, err := a.organizationTaskContextStore.ReadOrganizationTaskContext(ctx, a.current, id)
	if err == nil {
		a.current = next
	}
	return task, err
}
func (a *organizationTaskContextAgent) SaveTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	task, next, err := a.organizationTaskContextStore.SaveOrganizationTaskContext(ctx, a.current, task)
	if err == nil {
		a.current = next
	}
	return task, err
}
func (a *organizationTaskContextAgent) UpdateTask(ctx context.Context, task agentworkspace.Task) (agentworkspace.Task, error) {
	task, next, err := a.organizationTaskContextStore.UpdateOrganizationTaskContext(ctx, a.current, task)
	if err == nil {
		a.current = next
	}
	return task, err
}
func (a *organizationTaskContextAgent) ReadOrganizationTaskContext(ctx context.Context, _ agentruntime.ContextSnapshot, id string) (agentworkspace.Task, agentruntime.ContextSnapshot, error) {
	return a.organizationTaskContextStore.ReadOrganizationTaskContext(ctx, a.current, id)
}
func (a *organizationTaskContextAgent) SearchActivities(ctx context.Context, city, viewer, category, when string, closer bool, bounds *agentworkspace.MapBounds) ([]foundation.Activity, error) {
	// A member's Personal invitation/host access is not an Organization City
	// purpose. The original domain SQL applies PUBLIC ACL before materializing.
	return a.Store.SearchActivities(ctx, city, "", category, when, closer, bounds)
}
func (a *organizationTaskContextAgent) Search(ctx context.Context, city, viewer string, terms []string) (agentworkspace.Results, error) {
	return a.Store.Search(ctx, city, "", terms)
}
func agentTaskWriteFailure(w http.ResponseWriter, err error, org bool) {
	if org {
		organizationTaskContextFailure(w, err)
	} else {
		serverError(w, err)
	}
}

func organizationTaskContextFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, organization.ErrForbidden) {
		respondError(w, http.StatusForbidden, "organization_workspace_forbidden")
		return
	}
	resultProjectionFailure(w, err)
}
func (s *server) captureOrganizationTaskContext(w http.ResponseWriter, r *http.Request, digest [32]byte, actor identity.Actor, principal, role string) (*http.Request, bool) {
	if r.Header.Get("X-Birdtie-Organization-Workspace") == "" {
		return r, true
	}
	native, ok := s.agent.(organizationTaskContextStore)
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "organization_context_unavailable")
		return r, false
	}
	snapshot, err := native.CaptureOrganizationTaskAuthority(r.Context(), digest, actor, r.Header.Get("X-Birdtie-Organization-Workspace"), principal, role)
	if err != nil {
		organizationTaskContextFailure(w, err)
		return r, false
	}
	return r.WithContext(context.WithValue(r.Context(), organizationTaskSnapshotKey{}, snapshot)), true
}

func (s *server) createAgentTask(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	if _, err := s.catalog.GetCity(r.Context(), cityID); handleReadError(w, err) {
		return
	}
	principalID, role, organizationWorkspace := s.workspacePrincipal(w, r, actor)
	if principalID == "" && r.Header.Get("X-Birdtie-Organization-Workspace") != "" {
		return
	}
	var captured bool
	r, captured = s.captureOrganizationTaskContext(w, r, digest, actor, principalID, role)
	if !captured {
		return
	}
	if organizationWorkspace {
		store := s.agent.(organizationTaskContextStore)
		snapshot := r.Context().Value(organizationTaskSnapshotKey{}).(agentruntime.ContextSnapshot)
		snapshot, err = store.BindOrganizationTaskQuerySources(r.Context(), snapshot, cityID)
		if err != nil {
			organizationTaskContextFailure(w, err)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), organizationTaskSnapshotKey{}, snapshot))
		local := *s
		local.agent = &organizationTaskContextAgent{Store: s.agent, organizationTaskContextStore: store, current: snapshot, principal: principalID}
		s = &local
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	var input struct {
		Query     string                    `json:"query"`
		TaskID    string                    `json:"taskId"`
		MapBounds *agentworkspace.MapBounds `json:"mapBounds"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len(query) > 240 {
		respondError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	if input.MapBounds != nil && !input.MapBounds.Valid() {
		respondError(w, http.StatusBadRequest, "invalid_map_bounds")
		return
	}
	principalType, policy := workspaceAgentPolicy(organizationWorkspace)
	if !policy.Available || !policy.Allows(agentruntime.CityContextRead) ||
		(organizationWorkspace && !policy.Allows(agentruntime.OrganizationContextRead)) {
		respondError(w, http.StatusForbidden, "agent_role_unavailable")
		return
	}
	workspace, permissions := string(policy.Role), policy.PermissionStrings()
	if principalID != "" {
		if _, err := actorref.ParsePrincipal(principalType, principalID); err != nil {
			respondError(w, http.StatusForbidden, "invalid_workspace_principal")
			return
		}
	}
	if !s.requireActiveWorkspaceAgent(w, r, principalType, principalID) {
		return
	}
	if input.TaskID != "" && actor.ID == "" {
		respondError(w, http.StatusBadRequest, "invalid_task_id")
		return
	}
	access := agentruntime.AccessRequest{Scope: agentruntime.Public,
		ViewerID: actor.ID, PrincipalID: principalID,
		AuthorityVerified: true, Released: true}
	if input.TaskID != "" {
		access.Scope, access.OwnerID, access.Released =
			agentruntime.WorkspacePrivate, principalID, false
	}
	if !policy.DecideContext(access).Allowed {
		respondError(w, http.StatusForbidden, "agent_context_forbidden")
		return
	}
	var task agentworkspace.Task
	if input.TaskID != "" {
		if actor.ID == "" || !uuidPath.MatchString(input.TaskID) {
			respondError(w, http.StatusBadRequest, "invalid_task_id")
			return
		}
		task, err = s.agent.GetTask(r.Context(), principalID, input.TaskID)
		if errors.Is(err, agentworkspace.ErrNotFound) {
			respondError(w, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			agentTaskWriteFailure(w, err, organizationWorkspace)
			return
		}
		if !workspaceTaskMatches(task, principalType, principalID) {
			respondError(w, http.StatusNotFound, "not_found")
			return
		}
		if task.CityID != cityID {
			respondError(w, http.StatusConflict, "task_city_mismatch")
			return
		}
		task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: query})
		task.Status = agentworkspace.TaskActive
	} else {
		task = agentworkspace.Task{
			PrincipalType: principalType, PrincipalID: principalID, ActingUserID: actor.ID,
			CityID: cityID, Query: query, Intent: "PENDING", Status: agentworkspace.TaskActive,
			Filters:      map[string]string{},
			Conversation: []agentworkspace.Message{{Role: "user", Text: query}},
		}
		if actor.ID != "" {
			task, err = s.agent.SaveTask(r.Context(), task)
			if err != nil {
				agentTaskWriteFailure(w, err, organizationWorkspace)
				return
			}
		}
	}
	mapBounds := input.MapBounds
	if mapBounds == nil && input.TaskID != "" {
		mapBounds, err = agentworkspace.BoundsFromFilters(task.Filters)
		if err != nil {
			respondError(w, http.StatusConflict, "invalid_task_context")
			return
		}
	}
	mvp := agentworkspace.ParseMVPIntent(query, taskContext(task, input.TaskID != ""))
	if !mvp.Supported && input.MapBounds != nil && mvp.Operation == agentworkspace.UnsupportedIntent {
		mvp = agentworkspace.MVPIntent{Operation: agentworkspace.AreaDiscovery, Target: agentworkspace.FindActivity,
			TimePreference: "anytime", LocationPreference: "viewport", Supported: true}
	}
	if mvp.Operation == agentworkspace.AreaDiscovery && mapBounds == nil {
		mvp.Supported, mvp.Reason = false, "map_bounds_required"
	}
	if newTypedOperation(mvp.Operation) || mvp.Operation == agentworkspace.FindOrganization || mvp.Operation == agentworkspace.FindPlace ||
		mvp.Operation == agentworkspace.CompareResults || mvp.Operation == agentworkspace.CreateActivity || mvp.Operation == agentworkspace.PersonalRelationshipContext || mvp.Operation == agentworkspace.FindNewPeople || !mvp.Supported {
		s.executeSpecialAgentIntent(w, r, task, mvp, query, cityID, actor.ID,
			principalType, workspace, role, permissions, mapBounds, digest)
		return
	}
	intent := agentworkspace.IntentContext{Intent: mvp.Operation, Category: mvp.Category,
		TimePreference: mvp.TimePreference, DistancePreference: mvp.DistancePreference, Supported: mvp.Supported}
	if input.TaskID == "" || task.Intent != intent.Intent || task.Filters["category"] == "" {
		task.Intent = intent.Intent
		task.Filters = map[string]string{
			"category":           intent.Category,
			"timePreference":     intent.TimePreference,
			"targetIntent":       mvp.Target,
			"locationPreference": mvp.LocationPreference,
			"currentQuery":       query,
		}
		if intent.DistancePreference != "" {
			task.Filters["distancePreference"] = intent.DistancePreference
		}
		if actor.ID != "" {
			task, err = s.agent.UpdateTask(r.Context(), task)
			if err != nil {
				agentTaskWriteFailure(w, err, organizationWorkspace)
				return
			}
		}
	}
	if intent.DistancePreference != "" {
		task.Filters["distancePreference"] = intent.DistancePreference
	} else if input.TaskID != "" {
		delete(task.Filters, "distancePreference")
	}
	task.Filters["currentQuery"] = query
	if mapBounds != nil {
		for key, value := range mapBounds.Filters() {
			task.Filters[key] = value
		}
	} else {
		for _, key := range []string{"mapWest", "mapSouth", "mapEast", "mapNorth"} {
			delete(task.Filters, key)
		}
	}
	if actor.ID != "" {
		task, err = s.agent.UpdateTask(r.Context(), task)
		if err != nil {
			agentTaskWriteFailure(w, err, organizationWorkspace)
			return
		}
	}
	activities, err := s.agent.SearchActivities(r.Context(), cityID, actor.ID,
		intent.Category, intent.TimePreference, intent.DistancePreference == "closer", mapBounds)
	if err != nil {
		task.Status = agentworkspace.TaskFailed
		if actor.ID != "" {
			_, _ = s.agent.UpdateTask(r.Context(), task)
		}
		agentTaskWriteFailure(w, err, organizationWorkspace)
		return
	}
	if actor.ID != "" && !organizationWorkspace {
		activities, err = s.currentRulesActivities(r.Context(), digest, task, query, w.Header().Get("X-Request-ID"), activities)
		if err != nil {
			contextBuilderHTTPFailure(w, err)
			return
		}
	}
	task.Status = agentworkspace.TaskActive
	ids := make([]string, 0, len(activities))
	for _, activity := range activities {
		ids = append(ids, activity.ID)
	}
	task.Filters["resultIDs"] = strings.Join(ids, ",")
	responseText := fmt.Sprintf("找到 %d 个符合条件的公开活动。", len(activities))
	if input.MapBounds != nil {
		responseText = fmt.Sprintf("在所选地图范围内找到 %d 个已发布活动。", len(activities))
	} else if intent.Category == "" {
		responseText = fmt.Sprintf("找到 %d 个当前城市的公开活动。", len(activities))
	}
	if len(activities) == 0 {
		if input.MapBounds != nil {
			responseText = "所选地图范围内暂时没有找到符合条件的公开活动。"
		} else if intent.Category == "" {
			responseText = "当前城市暂时没有找到已发布的公开活动。"
		} else {
			responseText = "暂时没有找到符合条件的公开活动。"
		}
	}
	if input.MapBounds == nil && intent.DistancePreference == "closer" {
		subject := "公开活动"
		if intent.Category == "badminton" {
			subject = "羽毛球活动"
		}
		if intent.TimePreference == "weekend" {
			subject = "周末的" + subject
		}
		responseText = fmt.Sprintf("继续查找%s：按与城市中心的距离排序，找到 %d 个（未使用设备定位）。", subject, len(activities))
	}
	task, r, err = s.finishCurrentNowQuery(r, digest, task, responseText)
	if err != nil {
		agentTaskWriteFailure(w, err, organizationWorkspace)
		return
	}
	result := activityResults(cityID, query, intent, activities)
	result = workspaceEnvelope(result, task, actor.ID != "", principalType, workspace, role, permissions, w.Header().Get("X-Request-ID"))
	result.Message = responseText
	if mapBounds != nil {
		result.Note = "以下为所选地图范围内当前的公开活动。"
	} else if intent.DistancePreference == "closer" {
		result.Note = "以下公开活动按与城市中心的距离排序，未使用设备定位。"
	}
	s.respondCurrentAgentTask(w, r, result, digest, actor)
}

func (s *server) listAgentTasks(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	principalID, role, organizationWorkspace := s.workspacePrincipal(w, r, actor)
	if principalID == "" {
		return
	}
	var captured bool
	r, captured = s.captureOrganizationTaskContext(w, r, digest, actor, principalID, role)
	if !captured {
		return
	}
	_, policy := workspaceAgentPolicy(organizationWorkspace)
	if !policy.Available || !policy.Allows(agentruntime.CityContextRead) {
		respondError(w, http.StatusForbidden, "agent_role_unavailable")
		return
	}
	kind, _ := workspaceAgentPolicy(organizationWorkspace)
	if !s.requireActiveWorkspaceAgent(w, r, kind, principalID) {
		return
	}
	if !policy.DecideContext(agentruntime.AccessRequest{
		Scope: agentruntime.WorkspacePrivate, OwnerID: principalID,
		ViewerID: actor.ID, PrincipalID: principalID, AuthorityVerified: true,
	}).Allowed {
		respondError(w, http.StatusForbidden, "agent_context_forbidden")
		return
	}
	var tasks []agentworkspace.Task
	var snapshot agentruntime.ContextSnapshot
	if organizationWorkspace {
		snapshot = r.Context().Value(organizationTaskSnapshotKey{}).(agentruntime.ContextSnapshot)
		tasks, snapshot, err = s.agent.(organizationTaskContextStore).ReadOrganizationTasksContext(r.Context(), snapshot)
	} else {
		tasks, err = s.agent.ListTasks(r.Context(), principalID)
	}
	if err != nil {
		if organizationWorkspace {
			organizationTaskContextFailure(w, err)
		} else {
			serverError(w, err)
		}
		return
	}
	filtered := make([]agentworkspace.Task, 0, len(tasks))
	for _, task := range tasks {
		if workspaceTaskMatches(task, kind, principalID) {
			filtered = append(filtered, agentworkspace.SanitizeTaskForResponse(task))
		}
	}
	if organizationWorkspace {
		buffer := &taskResponseBuffer{header: w.Header().Clone()}
		respond(buffer, http.StatusOK, map[string]any{"data": filtered})
		if err = s.agent.(organizationTaskContextStore).RevalidateOrganizationTaskContext(r.Context(), snapshot, tasks); err != nil {
			organizationTaskContextFailure(w, err)
			return
		}
		copyAgentResponseBuffer(w, buffer)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": filtered})
}

func (s *server) getAgentTask(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("taskID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_task_id")
		return
	}
	principalID, role, organizationWorkspace := s.workspacePrincipal(w, r, actor)
	if principalID == "" {
		return
	}
	var captured bool
	r, captured = s.captureOrganizationTaskContext(w, r, digest, actor, principalID, role)
	if !captured {
		return
	}
	if organizationWorkspace {
		local := *s
		local.agent = &organizationTaskContextAgent{Store: s.agent, organizationTaskContextStore: s.agent.(organizationTaskContextStore), current: r.Context().Value(organizationTaskSnapshotKey{}).(agentruntime.ContextSnapshot), principal: principalID}
		s = &local
	}
	_, policy := workspaceAgentPolicy(organizationWorkspace)
	if !policy.Available || !policy.Allows(agentruntime.CityContextRead) {
		respondError(w, http.StatusForbidden, "agent_role_unavailable")
		return
	}
	kind, _ := workspaceAgentPolicy(organizationWorkspace)
	if !s.requireActiveWorkspaceAgent(w, r, kind, principalID) {
		return
	}
	if !policy.DecideContext(agentruntime.AccessRequest{
		Scope: agentruntime.WorkspacePrivate, OwnerID: principalID,
		ViewerID: actor.ID, PrincipalID: principalID, AuthorityVerified: true,
	}).Allowed {
		respondError(w, http.StatusForbidden, "agent_context_forbidden")
		return
	}
	var task agentworkspace.Task
	if organizationWorkspace {
		snapshot := r.Context().Value(organizationTaskSnapshotKey{}).(agentruntime.ContextSnapshot)
		task, snapshot, err = s.agent.(organizationTaskContextStore).ReadOrganizationTaskContext(r.Context(), snapshot, id)
		r = r.WithContext(context.WithValue(r.Context(), organizationTaskSnapshotKey{}, snapshot))
	} else {
		task, err = s.agent.GetTask(r.Context(), principalID, id)
	}
	if errors.Is(err, agentworkspace.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		if organizationWorkspace {
			organizationTaskContextFailure(w, err)
		} else {
			serverError(w, err)
		}
		return
	}
	if !workspaceTaskMatches(task, kind, principalID) {
		respondError(w, http.StatusNotFound, "not_found")
		return
	}
	if task.ContextType == "ONLINE" {
		s.getOwnNowOnlineTask(w, r)
		return
	}
	if _, err := s.catalog.GetCity(r.Context(), task.CityID); handleReadError(w, err) {
		return
	}
	currentQuery := task.Filters["currentQuery"]
	if currentQuery == "" {
		currentQuery = task.Query
	}
	intent := agentworkspace.IntentContext{Intent: task.Intent, Category: task.Filters["category"], TimePreference: task.Filters["timePreference"], DistancePreference: task.Filters["distancePreference"], Supported: task.Intent == agentworkspace.FindActivity || task.Intent == agentworkspace.RefineResults || task.Intent == agentworkspace.AreaDiscovery}
	mapBounds, err := agentworkspace.BoundsFromFilters(task.Filters)
	if err != nil {
		respondError(w, http.StatusConflict, "invalid_task_context")
		return
	}
	if task.Status == agentworkspace.TaskFailed {
		result := emptyAgentResults(task.CityID, currentQuery)
		for i := len(task.Conversation) - 1; i >= 0; i-- {
			if task.Conversation[i].Role == "assistant" {
				result.Message = task.Conversation[i].Text
				break
			}
		}
		principalType, policy := workspaceAgentPolicy(organizationWorkspace)
		workspace, permissions := string(policy.Role), policy.PermissionStrings()
		result = workspaceEnvelope(result, task, true, principalType, workspace, role, permissions, w.Header().Get("X-Request-ID"))
		s.respondCurrentAgentTask(w, r, result, digest, actor)
		return
	}
	if newTypedOperation(task.Intent) || task.Intent == agentworkspace.FindOrganization || task.Intent == agentworkspace.FindPlace ||
		task.Intent == agentworkspace.CompareResults || task.Intent == agentworkspace.CreateActivity ||
		task.Intent == agentworkspace.UnsupportedIntent || task.Intent == agentworkspace.PersonalRelationshipContext || task.Intent == agentworkspace.FindNewPeople {
		mvp := agentworkspace.MVPIntent{Operation: task.Intent, Target: task.Filters["targetIntent"],
			SearchTerm: task.Filters["searchTerm"], Reason: task.Filters["reason"], Supported: task.Status != agentworkspace.TaskFailed}
		result, liveMessage, err := s.specialAgentResults(r.Context(), task, mvp, currentQuery, task.CityID, actor.ID, mapBounds)
		if err != nil {
			serverError(w, err)
			return
		}
		result.Message = liveMessage
		if task.Intent == agentworkspace.PersonalRelationshipContext {
			w.Header().Set("Cache-Control", "no-store")
		}
		for i := len(task.Conversation) - 1; task.Intent != agentworkspace.PersonalRelationshipContext && i >= 0; i-- {
			if task.Conversation[i].Role == "assistant" {
				result.Message = task.Conversation[i].Text
				break
			}
		}
		principalType, policy := workspaceAgentPolicy(organizationWorkspace)
		workspace, permissions := string(policy.Role), policy.PermissionStrings()
		result = workspaceEnvelope(result, task, true, principalType, workspace, role, permissions, w.Header().Get("X-Request-ID"))
		s.respondCurrentAgentTask(w, r, result, digest, actor)
		return
	}
	activities := []foundation.Activity{}
	if intent.Supported {
		activities, err = s.agent.SearchActivities(r.Context(), task.CityID, actor.ID, intent.Category, intent.TimePreference, intent.DistancePreference == "closer", mapBounds)
		if err != nil {
			serverError(w, err)
			return
		}
	}
	result := activityResults(task.CityID, currentQuery, intent, activities)
	if !intent.Supported {
		result.Note = "目前的公开数据暂不支持此类请求，可以试试搜索活动。"
		result.Message = result.Note
	} else if len(task.Conversation) > 0 {
		for i := len(task.Conversation) - 1; i >= 0; i-- {
			if task.Conversation[i].Role == "assistant" {
				result.Message = task.Conversation[i].Text
				break
			}
		}
	}
	principalType, policy := workspaceAgentPolicy(organizationWorkspace)
	workspace, permissions := string(policy.Role), policy.PermissionStrings()
	result = workspaceEnvelope(result, task, true, principalType, workspace, role, permissions, w.Header().Get("X-Request-ID"))
	s.respondCurrentAgentTask(w, r, result, digest, actor)
}

type taskResponseBuffer struct {
	header http.Header
	status int
	bytes.Buffer
}

func (b *taskResponseBuffer) Header() http.Header { return b.header }
func (b *taskResponseBuffer) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}
func (b *taskResponseBuffer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.Buffer.Write(p)
}

func (s *server) respondCurrentAgentTask(w http.ResponseWriter, r *http.Request, result agentworkspace.Results, digest [32]byte, actor identity.Actor) {
	if r.Header.Get("X-Birdtie-Organization-Workspace") != "" {
		store, ok := s.agent.(organizationTaskContextStore)
		snapshot, valid := r.Context().Value(organizationTaskSnapshotKey{}).(agentruntime.ContextSnapshot)
		if !ok || !valid || result.Task == nil {
			respondError(w, http.StatusServiceUnavailable, "organization_context_unavailable")
			return
		}
		// POST creates/updates its original Task. Obtain the current exact Task
		// receipt without replacing the original captured authority/deadline.
		if r.Method == http.MethodPost {
			current, next, err := store.ReadOrganizationTaskContext(r.Context(), snapshot, result.Task.ID)
			if err != nil {
				organizationTaskContextFailure(w, err)
				return
			}
			left, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(current))
			right, _ := json.Marshal(agentworkspace.SanitizeTaskForResponse(*result.Task))
			if !bytes.Equal(left, right) {
				respondError(w, http.StatusConflict, "result_source_changed")
				return
			}
			snapshot = next
		}
		buffer := &taskResponseBuffer{header: w.Header().Clone()}
		s.respondAgentWithCommercial(buffer, r, result)
		if buffer.status == http.StatusOK {
			if err := store.RevalidateOrganizationTaskContext(r.Context(), snapshot, []agentworkspace.Task{*result.Task}); err != nil {
				organizationTaskContextFailure(w, err)
				return
			}
		}
		if r.Context().Err() != nil {
			respondError(w, http.StatusServiceUnavailable, "organization_context_unavailable")
			return
		}
		copyAgentResponseBuffer(w, buffer)
		return
	}
	result, history, historyPrimary, err := s.prepareAgentMessageResults(r, result, digest, actor)
	if err != nil {
		resultProjectionFailure(w, err)
		return
	}
	var native arp.NativeStore
	var access arp.Access
	var query arp.Query
	var receipt arp.Receipt
	if !historyPrimary {
		result, native, access, query, receipt, err = s.prepareNativeAgentResults(r, result, digest, actor)
	}
	if err != nil {
		resultProjectionFailure(w, err)
		return
	}
	result, err = decorateCurrentNowReply(r, result)
	if err != nil {
		resultProjectionFailure(w, err)
		return
	}
	// Authenticated recovery displays the original message and its citations.
	// Reading history neither dispatches again nor revives an old egress grant.
	if r.Method == http.MethodGet && result.Task != nil {
		for i := len(result.Task.Conversation) - 1; i >= 0; i-- {
			message := result.Task.Conversation[i]
			if message.Role == "assistant" && message.SourceRunID != "" && message.SourceEvidenceDigest != "" && len(message.Sources) > 0 {
				result.Message = message.Text
				result.ResultSet.Sources = append([]agentworkspace.AnswerSource(nil), message.Sources...)
				break
			}
			if message.Role == "user" {
				break
			}
		}
	}
	buffer := &taskResponseBuffer{header: w.Header().Clone()}
	s.respondAgentWithCommercial(buffer, r, result)
	if native != nil {
		if buffer.status == http.StatusOK {
			if err = native.RevalidateAgentResultProjection(r.Context(), access, query, receipt); err != nil {
				resultProjectionFailure(w, err)
				return
			}
		}
		if r.Context().Err() != nil {
			respondError(w, http.StatusServiceUnavailable, "result_source_unavailable")
			return
		}
		if err = revalidateCurrentNowReply(r); err != nil {
			resultProjectionFailure(w, err)
			return
		}
		if history != nil && buffer.status == http.StatusOK {
			if err = history.Revalidate(r.Context()); err != nil {
				resultProjectionFailure(w, currentToolReadError(err))
				return
			}
		}
		copyAgentResponseBuffer(w, buffer)
		return
	}
	if buffer.status == http.StatusOK && actor.AccountType == "person" && r.Header.Get("X-Birdtie-Organization-Workspace") == "" {
		if result.Task == nil {
			respondError(w, http.StatusServiceUnavailable, "task_unavailable")
			return
		}
		if native, ok := s.agent.(interface {
			ValidateHumanAgentTask(context.Context, [32]byte, identity.Actor, agentworkspace.Task) error
		}); ok {
			if err := native.ValidateHumanAgentTask(r.Context(), digest, actor, *result.Task); err != nil {
				if errors.Is(err, identity.ErrUnauthorized) && authFailed(w, err) {
					return
				}
				if errors.Is(err, agentworkspace.ErrNotFound) {
					respondError(w, http.StatusNotFound, "not_found")
				} else {
					serverError(w, err)
				}
				return
			}
		} else {
			// Existing non-native domain adapters retain their original interfaces,
			// but must recheck current identity and owned Agent before writing.
			current, err := s.access.Authenticate(r.Context(), digest)
			if authFailed(w, err) {
				return
			}
			if current != actor {
				respondError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if !s.requireActiveWorkspaceAgent(w, r, "person", actor.ID) {
				return
			}
		}
	}
	// The history source/absolute lifetime check is the final guarded read,
	// after any original current-task/native/commercial encoding or lock waits.
	if history != nil && buffer.status == http.StatusOK {
		if err = history.Revalidate(r.Context()); err != nil {
			resultProjectionFailure(w, currentToolReadError(err))
			return
		}
	}
	if r.Context().Err() != nil {
		respondError(w, http.StatusServiceUnavailable, "task_unavailable")
		return
	}
	for key, values := range buffer.header {
		w.Header()[key] = values
	}
	if buffer.status == 0 {
		buffer.status = http.StatusInternalServerError
	}
	w.WriteHeader(buffer.status)
	_, _ = w.Write(buffer.Bytes())
}

func workspaceTaskMatches(task agentworkspace.Task, kind, id string) bool {
	actual, err := task.PrincipalRef()
	if err != nil {
		return false
	}
	expected, err := actorref.ParsePrincipal(kind, id)
	return err == nil && actual.Equal(expected)
}

func workspaceAgentPolicy(organizationWorkspace bool) (string, agentruntime.Policy) {
	if organizationWorkspace {
		return "organization", agentruntime.ForType(actorref.Organization)
	}
	return "person", agentruntime.ForType(actorref.Person)
}

func (s *server) requireActiveWorkspaceAgent(w http.ResponseWriter, r *http.Request, kind, principalID string) bool {
	// Anonymous city discovery has no persisted Agent or principal. Authenticated
	// Personal and Organization invocations require the active owned Agent.
	if principalID == "" {
		return true
	}
	active, err := s.agent.HasActiveAgent(r.Context(), kind, principalID)
	if err != nil {
		serverError(w, err)
		return false
	}
	if !active {
		respondError(w, http.StatusForbidden, "agent_unavailable")
		return false
	}
	return true
}

func taskContext(task agentworkspace.Task, hasCurrent bool) *agentworkspace.Task {
	if !hasCurrent {
		return nil
	}
	return &task
}

func activityResults(cityID, query string, intent agentworkspace.IntentContext, activities []foundation.Activity) agentworkspace.Results {
	results := agentworkspace.Results{CityID: cityID, Query: query, Mode: "rules", Activities: activities,
		People: []agentworkspace.Person{}, Groups: []agentworkspace.Group{}, Places: []foundation.Place{}}
	if intent.DistancePreference == "closer" {
		results.Note = "活动按与城市中心的距离排序，未使用设备定位。"
	} else if len(activities) == 0 {
		results.Note = "这个时间段暂无符合条件的已发布活动。"
		results.Message = "暂时没有找到符合条件的公开活动。"
	} else {
		results.Note = "Birdtie 已发布活动 · 按规则匹配需求"
		results.Message = fmt.Sprintf("找到 %d 个符合条件的公开活动。", len(activities))
		if intent.DistancePreference != "closer" {
			results.FollowUps = []string{"按市中心距离排序"}
		}
	}
	return results
}

func workspaceEnvelope(result agentworkspace.Results, task agentworkspace.Task, persisted bool, principalType, workspace, role string, permissions []string, requestID string) agentworkspace.Results {
	result.PrincipalID = task.PrincipalID
	result.PrincipalType = strings.ToUpper(principalType)
	result.ContextType = task.ContextType
	result.ContextID = task.ContextID
	result.Workspace = workspace
	result.Permissions = permissions
	if workspace == "ORGANIZATION" {
		result.Role = strings.ToUpper(role)
	}
	if persisted {
		result.TaskID = task.ID
		result.Task = &task
	}
	return agentworkspace.WithContract(result, task, requestID)
}
