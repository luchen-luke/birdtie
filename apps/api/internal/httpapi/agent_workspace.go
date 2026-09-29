package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
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
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	var input struct {
		Query string `json:"query"`
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
	result, err := s.agent.Search(r.Context(), cityID, actor.ID, agentworkspace.Terms(query))
	if err != nil {
		serverError(w, err)
		return
	}
	result.Query = query
	if actor.ID != "" {
		task, err := s.agent.SaveTask(r.Context(), actor.ID, cityID, query)
		if err != nil {
			serverError(w, err)
			return
		}
		result.TaskID = task.ID
	}
	respond(w, http.StatusOK, map[string]any{"data": result})
}

func (s *server) listAgentTasks(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	tasks, err := s.agent.ListTasks(r.Context(), actor.ID)
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
	task, err := s.agent.GetTask(r.Context(), actor.ID, id)
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
	result, err := s.agent.Search(r.Context(), task.CityID, actor.ID, agentworkspace.Terms(task.Query))
	if err != nil {
		serverError(w, err)
		return
	}
	result.Query = task.Query
	result.TaskID = task.ID
	respond(w, http.StatusOK, map[string]any{"data": result})
}
