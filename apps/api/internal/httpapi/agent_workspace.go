package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

func (s *server) createAgentTask(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	cityID := r.PathValue("cityID")
	if _, err := s.catalog.GetCity(r.Context(), cityID); handleReadError(w, err) {
		return
	}
	principalID, role, organizationWorkspace := s.workspacePrincipal(w, r, actor)
	if principalID == "" {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	var input struct {
		Query  string `json:"query"`
		TaskID string `json:"taskId"`
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
	principalType, workspace, permissions := "person", "PERSONAL", []string{"city_context.read"}
	if organizationWorkspace {
		principalType, workspace = "organization", "ORGANIZATION"
		permissions = []string{"city_context.read", "organization_context.read"}
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
			serverError(w, err)
			return
		}
		if task.CityID != cityID {
			respondError(w, http.StatusConflict, "task_city_mismatch")
			return
		}
		if task.PrincipalType != principalType || task.PrincipalID != principalID {
			respondError(w, http.StatusNotFound, "not_found")
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
				serverError(w, err)
				return
			}
		}
	}
	intent := agentworkspace.ResolveIntent(query, taskContext(task, input.TaskID != ""))
	if !intent.Supported {
		if input.TaskID == "" {
			task.Intent = agentworkspace.UnsupportedIntent
			task.Status = agentworkspace.TaskFailed
		} else {
			task.Status = agentworkspace.TaskCompleted
		}
		if actor.ID != "" {
			task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: "I can currently help you find nearby activities. Try: Find badminton this weekend."})
			task, err = s.agent.UpdateTask(r.Context(), task)
			if err != nil {
				serverError(w, err)
				return
			}
		}
		result := workspaceEnvelope(agentworkspace.Results{
			CityID: cityID, Query: query, Mode: "rules", Activities: []foundation.Activity{},
		}, task, actor.ID != "", principalType, workspace, role, permissions)
		result.Note = "I can currently help you find nearby activities. Try: Find badminton this weekend."
		respond(w, http.StatusOK, map[string]any{"data": result})
		return
	}
	if input.TaskID == "" || task.Intent != intent.Intent || task.Filters["category"] == "" {
		task.Intent = intent.Intent
		task.Filters = map[string]string{
			"category":       intent.Category,
			"timePreference": intent.TimePreference,
		}
		if intent.DistancePreference != "" {
			task.Filters["distancePreference"] = intent.DistancePreference
		}
		if actor.ID != "" {
			task, err = s.agent.UpdateTask(r.Context(), task)
			if err != nil {
				serverError(w, err)
				return
			}
		}
	}
	if intent.DistancePreference != "" {
		task.Filters["distancePreference"] = intent.DistancePreference
	} else {
		delete(task.Filters, "distancePreference")
	}
	if actor.ID != "" {
		task, err = s.agent.UpdateTask(r.Context(), task)
		if err != nil {
			serverError(w, err)
			return
		}
	}
	activities, err := s.agent.SearchActivities(r.Context(), cityID, actor.ID,
		intent.Category, intent.TimePreference, intent.DistancePreference == "closer")
	if err != nil {
		task.Status = agentworkspace.TaskFailed
		if actor.ID != "" {
			_, _ = s.agent.UpdateTask(r.Context(), task)
		}
		serverError(w, err)
		return
	}
	task.Status = agentworkspace.TaskCompleted
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: fmt.Sprintf("Found %d matching badminton activities.", len(activities))})
	if actor.ID != "" {
		task, err = s.agent.UpdateTask(r.Context(), task)
		if err != nil {
			serverError(w, err)
			return
		}
	}
	result := activityResults(cityID, query, intent, activities)
	places, searchErr := s.agent.SearchPlaces(r.Context(), cityID, []string{intent.Category})
	if searchErr != nil {
		serverError(w, searchErr)
		return
	}
	result.Places = places
	result = workspaceEnvelope(result, task, actor.ID != "", principalType, workspace, role, permissions)
	if intent.DistancePreference == "closer" {
		result.Note = "Showing matching badminton activities ordered by distance from the city centre; device location is not used."
	}
	respond(w, http.StatusOK, map[string]any{"data": result})
}

func (s *server) listAgentTasks(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	principalID, _, _ := s.workspacePrincipal(w, r, actor)
	if principalID == "" {
		return
	}
	tasks, err := s.agent.ListTasks(r.Context(), principalID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": tasks})
}

func (s *server) getAgentTask(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
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
	task, err := s.agent.GetTask(r.Context(), principalID, id)
	if errors.Is(err, agentworkspace.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if _, err := s.catalog.GetCity(r.Context(), task.CityID); handleReadError(w, err) {
		return
	}
	intent := agentworkspace.IntentContext{Intent: task.Intent, Category: task.Filters["category"], TimePreference: task.Filters["timePreference"], DistancePreference: task.Filters["distancePreference"], Supported: task.Intent == agentworkspace.FindActivity}
	activities := []foundation.Activity{}
	if intent.Supported {
		activities, err = s.agent.SearchActivities(r.Context(), task.CityID, actor.ID, intent.Category, intent.TimePreference, intent.DistancePreference == "closer")
		if err != nil {
			serverError(w, err)
			return
		}
	}
	result := activityResults(task.CityID, task.Query, intent, activities)
	if intent.Supported {
		full, searchErr := s.agent.Search(r.Context(), task.CityID, actor.ID, []string{intent.Category})
		if searchErr != nil {
			serverError(w, searchErr)
			return
		}
		result.People = full.People
		result.Groups = full.Groups
		result.Places = full.Places
	}
	if !intent.Supported {
		result.Note = "I can currently help you find nearby activities. Try: Find badminton this weekend."
	}
	principalType, workspace, permissions := "person", "PERSONAL", []string{"city_context.read"}
	if organizationWorkspace {
		principalType, workspace = "organization", "ORGANIZATION"
		permissions = []string{"city_context.read", "organization_context.read"}
	}
	result = workspaceEnvelope(result, task, true, principalType, workspace, role, permissions)
	respond(w, http.StatusOK, map[string]any{"data": result})
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
		results.Note = "Activities are ordered by distance from the city centre; device location is not used."
	} else if len(activities) == 0 {
		results.Note = "No matching published activities are available for this time."
	} else {
		results.Note = "Published Birdtie activities · rule-based intent resolution"
	}
	return results
}

func workspaceEnvelope(result agentworkspace.Results, task agentworkspace.Task, persisted bool, principalType, workspace, role string, permissions []string) agentworkspace.Results {
	result.PrincipalID = task.PrincipalID
	result.PrincipalType = strings.ToUpper(principalType)
	result.Workspace = workspace
	result.Permissions = permissions
	if workspace == "ORGANIZATION" {
		result.Role = strings.ToUpper(role)
	}
	if persisted {
		result.TaskID = task.ID
		result.Task = &task
	}
	return result
}
