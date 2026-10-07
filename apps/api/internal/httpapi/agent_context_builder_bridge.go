package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

func contextBuilderHTTPFailure(w http.ResponseWriter, err error) {
	status, code, message := 503, "agent_context_unavailable", "当前结果暂不可用，请稍后重试"
	switch {
	case errors.Is(err, acb.ErrDenied):
		status, code, message = 403, "agent_context_denied", "当前账号或结果权限已变化，请重新查询"
	case errors.Is(err, acb.ErrInvalid):
		status, code, message = 400, "invalid_agent_context", "当前查询内容无效，请检查后重试"
	case errors.Is(err, acb.ErrExpired):
		status, code, message = 409, "agent_context_expired", "当前结果已过期，请重新查询"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// Existing catalog carries the actual native *postgres.Store. No Server field,
// route, provider, fake authority or owner-only fallback is installed here.
func (s *server) buildRulesContext(ctx context.Context, digest [32]byte, task agentworkspace.Task, query, requestID string, selection acb.Selection, activityIDs, placeIDs []string) (acb.BuiltContext, error) {
	if s == nil || ctx == nil || ctx.Err() != nil {
		return acb.BuiltContext{}, acb.ErrUnavailable
	}
	port, ok := s.catalog.(acb.Store)
	if !ok || port == nil {
		return acb.BuiltContext{}, acb.ErrUnavailable
	}
	principal, e := actorref.ParsePrincipal("PERSON", task.PrincipalID)
	if e != nil || task.PrincipalType != "person" || task.ActingUserID != task.PrincipalID {
		return acb.BuiltContext{}, acb.ErrDenied
	}
	access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	service, e := acb.NewService(port)
	if e != nil {
		return acb.BuiltContext{}, e
	}
	agent, e := service.Resolve(ctx, access)
	if e != nil {
		return acb.BuiltContext{}, e
	}
	if requestID == "" {
		requestID = fmt.Sprintf("context:%s:%d", task.ID, time.Now().UnixNano())
	}
	request := acb.Request{Access: access, Agent: agent, TaskID: task.ID, RequestID: requestID, CityID: task.CityID, CurrentQuery: query, TaskUpdatedAt: task.UpdatedAt, Selection: selection, Mode: acb.RulesPublicQuery, ActivityIDs: activityIDs, PlaceIDs: placeIDs, DeadlineAt: time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond)}
	return service.Build(ctx, request)
}
func (s *server) currentRulesActivities(ctx context.Context, digest [32]byte, task agentworkspace.Task, query, requestID string, items []foundation.Activity) ([]foundation.Activity, error) {
	ids := []string{}
	for _, item := range items {
		if item.Visibility == "public" {
			ids = append(ids, item.ID)
		}
	}
	built, e := s.buildRulesContext(ctx, digest, task, query, requestID, acb.ActivitySearch, ids, nil)
	if e != nil {
		return nil, e
	}
	if built.Bundle.CurrentQuery != query || built.Bundle.Sections.Activities != "AVAILABLE" || len(built.Activities) != len(ids) || len(built.Bundle.Activities) != len(ids) {
		return nil, acb.ErrUnavailable
	}
	byID := map[string]foundation.Activity{}
	for i, item := range built.Activities {
		if item.Visibility != "public" || item.ID != ids[i] || built.Bundle.Activities[i].ID != item.ID || item.CityID != task.CityID {
			return nil, acb.ErrUnavailable
		}
		byID[item.ID] = item
	}
	current := make([]foundation.Activity, 0, len(items))
	for _, old := range items {
		if old.Visibility != "public" { // Retains the existing purpose-limited human domain output only.
			current = append(current, old)
			continue
		}
		item, ok := byID[old.ID]
		if !ok {
			return nil, acb.ErrUnavailable
		}
		current = append(current, item)
	}
	return current, nil
}
func (s *server) currentRulesPlaces(ctx context.Context, digest [32]byte, task agentworkspace.Task, query, requestID string, items []foundation.Place) ([]foundation.Place, error) {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	built, e := s.buildRulesContext(ctx, digest, task, query, requestID, acb.PlaceSearch, nil, ids)
	if e != nil {
		return nil, e
	}
	if built.Bundle.CurrentQuery != query || built.Bundle.Sections.Places != "AVAILABLE" || len(built.Places) != len(ids) || len(built.Bundle.Places) != len(ids) {
		return nil, acb.ErrUnavailable
	}
	current := make([]foundation.Place, 0, len(items))
	for i, item := range built.Places {
		if item.ID != ids[i] || built.Bundle.Places[i].ID != item.ID || item.CityID != task.CityID {
			return nil, acb.ErrUnavailable
		}
		current = append(current, item)
	}
	return current, nil
}
