package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func emptyAgentResults(cityID, query string) agentworkspace.Results {
	return agentworkspace.Results{CityID: cityID, Query: query, Mode: "rules",
		Activities: []foundation.Activity{}, People: []agentworkspace.Person{},
		Groups: []agentworkspace.Group{}, Organizations: []agentworkspace.Organization{},
		Places: []foundation.Place{}, FollowUps: []string{}}
}

func (s *server) executeSpecialAgentIntent(w http.ResponseWriter, r *http.Request,
	task agentworkspace.Task, intent agentworkspace.MVPIntent, query, cityID, actorID,
	principalType, workspace, role string, permissions []string, bounds *agentworkspace.MapBounds, digest [32]byte) {
	usesBuilder := actorID != "" && principalType == "person" && intent.Supported && (intent.Operation == agentworkspace.FindPlace || intent.Operation == agentworkspace.CompareResults)
	if usesBuilder {
		if task.Filters == nil {
			task.Filters = map[string]string{}
		}
		task.Filters["currentQuery"] = query
		task.Status = agentworkspace.TaskActive
		var err error
		task, err = s.agent.UpdateTask(r.Context(), task)
		if err != nil {
			serverError(w, err)
			return
		}
	}
	result, message, err := s.specialAgentResults(r.Context(), task, intent, query, cityID, actorID, bounds)
	if err != nil {
		serverError(w, err)
		return
	}
	if usesBuilder {
		if intent.Operation == agentworkspace.FindPlace {
			result.Places, err = s.currentRulesPlaces(r.Context(), digest, task, query, w.Header().Get("X-Request-ID"), result.Places)
			if err == nil {
				message = fmt.Sprintf("找到 %d 个已发布地点。", len(result.Places))
			}
		} else {
			result.Activities, err = s.currentRulesActivities(r.Context(), digest, task, query, w.Header().Get("X-Request-ID"), result.Activities)
			if err == nil && len(result.Activities) >= 2 {
				message = fmt.Sprintf("可比较这两个活动：%s（%s）和 %s（%s）。", result.Activities[0].Title, result.Activities[0].Schedule, result.Activities[1].Title, result.Activities[1].Schedule)
			}
		}
		if err != nil {
			contextBuilderHTTPFailure(w, err)
			return
		}
	}
	task.Intent = intent.Operation
	task.Status = agentworkspace.TaskActive
	if !intent.Supported {
		task.Status = agentworkspace.TaskFailed
	}
	if task.Filters == nil {
		task.Filters = map[string]string{}
	}
	task.Filters["targetIntent"] = intent.Target
	task.Filters["searchTerm"] = intent.SearchTerm
	task.Filters["locationPreference"] = intent.LocationPreference
	task.Filters["currentQuery"] = query
	task.Filters["reason"] = intent.Reason
	if currentPublicPlaceFollowup(r) {
		task.Filters["timePreference"] = intent.TimePreference
	}
	if bounds != nil {
		for key, value := range bounds.Filters() {
			task.Filters[key] = value
		}
	}
	task, r, err = s.finishCurrentNowQuery(r, digest, task, message)
	if err != nil {
		serverError(w, err)
		return
	}
	result.Message = message
	if intent.Operation == agentworkspace.PersonalRelationshipContext {
		w.Header().Set("Cache-Control", "no-store")
	}
	result = workspaceEnvelope(result, task, actorID != "", principalType, workspace, role, permissions, w.Header().Get("X-Request-ID"))
	// The workspace's person policy is not an authenticated identity. Preserve
	// the optional-auth zero Actor for guests; signed callers retain the exact
	// existing current Task, Session and workspace response checks.
	responseActor := identity.Actor{}
	if actorID != "" {
		responseActor = identity.Actor{ID: actorID, AccountType: principalType}
	}
	s.respondCurrentAgentTask(w, r, result, digest, responseActor)
}

func (s *server) specialAgentResults(ctx context.Context, task agentworkspace.Task,
	intent agentworkspace.MVPIntent, query, cityID, actorID string,
	bounds *agentworkspace.MapBounds) (agentworkspace.Results, string, error) {
	result := emptyAgentResults(cityID, query)
	if !intent.Supported {
		switch intent.Reason {
		case "map_bounds_required":
			return result, "请先移动地图并选定搜索范围。", nil
		case "previous_results_required":
			return result, "请先搜索活动，再继续筛选或比较结果。", nil
		case "opportunity_filters_unavailable":
			return result, "社交机会暂不支持额外筛选。请先输入“我的社交机会”，再查看原活动详情。", nil
		default:
			return result, "暂时无法处理这类请求。你可以搜索活动、组织或地点。", nil
		}
	}
	switch intent.Operation {
	case agentworkspace.FindPerson, agentworkspace.FindCommunity, agentworkspace.FindBusiness, agentworkspace.FindOpportunity:
		if task.PrincipalType != "person" || actorID == "" || task.PrincipalID != actorID {
			return result, "请登录本人账号后查询当前可见的来源。", nil
		}
		if _, ok := s.agent.(arp.NativeStore); !ok {
			return result, "", arp.ErrUnavailable
		}
		return result, "正在按当前原生公开来源整理结果。", nil
	case agentworkspace.FindNewPeople:
		if task.PrincipalType != "person" || actorID == "" || task.PrincipalID != actorID {
			return result, "登录本人账号后，可以自主开启找伙伴匹配并查看候选。", nil
		}
		result.Actions = []agentworkspace.Action{{Type: "OPEN_NEW_PEOPLE", Label: "找新朋友"}}
		result.Note = "按双方明确意图与授权匹配；不读取私聊、私密记忆或定位，不自动发送邀请。"
		return result, "先选择公开找伙伴意图，再查看同样选择参与匹配的候选。你确认留言后，才会发送好友申请。", nil
	case agentworkspace.PersonalRelationshipContext:
		if task.PrincipalType != "person" || actorID == "" || task.PrincipalID != actorID {
			return result, "关系信号仅供登录后的本人 Personal Agent 使用。", nil
		}
		policy := agentruntime.ForType(actorref.Person)
		if !policy.Allows(agentruntime.RelationshipContextRead) || !policy.DecideContext(agentruntime.AccessRequest{Scope: agentruntime.Private, OwnerID: actorID, ViewerID: actorID, PrincipalID: task.PrincipalID, AuthorityVerified: true}).Allowed {
			return result, "关系信号暂不可访问。", nil
		}
		if s.relationshipContext == nil {
			return result, "", errors.New("relationship context store unavailable")
		}
		context, err := s.relationshipContext.OwnRelationshipContext(ctx, actorID)
		if err != nil {
			return result, "", err
		}
		result.RelationshipContext = &context
		result.Actions = []agentworkspace.Action{{Type: "OPEN_RELATIONSHIP_CONTEXT", Label: "查看关系信号"}}
		result.Note = "来自当前好友关系、近 30 天本人发送记录和双方授权的公开活动报名；不读取私聊正文，不推断亲密程度。"
		if !context.Enabled {
			return result, "关系信号使用尚未开启。你可以先查看说明，再决定是否允许本人的 Personal Agent 使用。", nil
		}
		// Persist only this general explanation; names/metrics are live owner-only
		// tool output and must never be copied into task conversation or filters.
		return result, "已读取当前授权的关系信号。点击“查看关系信号”可核对好友的互动记录；记录频次不代表亲密程度。", nil
	case agentworkspace.FindPlace:
		places, err := s.catalog.ListPlaces(ctx, cityID, intent.SearchTerm)
		if err != nil {
			return result, "", err
		}
		for _, place := range places {
			if bounds != nil && !pointInBounds(place.Location, bounds) {
				continue
			}
			result.Places = append(result.Places, place)
		}
		result.Note = "结果来自当前城市已发布的真实地点。"
		return result, fmt.Sprintf("找到 %d 个已发布地点。", len(result.Places)), nil
	case agentworkspace.FindOrganization:
		organizations, err := s.agent.SearchOrganizations(ctx, cityID, intent.SearchTerm)
		if err != nil {
			return result, "", err
		}
		result.Organizations = organizations
		result.Note = "仅显示在当前城市有可发现活动的公开组织。"
		return result, fmt.Sprintf("找到 %d 个公开组织。", len(organizations)), nil
	case agentworkspace.CompareResults:
		ids := strings.Split(task.Filters["resultIDs"], ",")
		all, err := s.agent.SearchActivities(ctx, cityID, actorID,
			task.Filters["category"], task.Filters["timePreference"], false, bounds)
		if err != nil {
			return result, "", err
		}
		byID := make(map[string]foundation.Activity, len(all))
		for _, item := range all {
			byID[item.ID] = item
		}
		for _, id := range ids {
			if item, ok := byID[id]; ok && len(result.Activities) < 2 {
				result.Activities = append(result.Activities, item)
			}
		}
		if len(result.Activities) < 2 {
			return result, "当前公开结果不足两个，暂时无法比较。", nil
		}
		result.Note = "仅比较当前仍可发现的真实活动，未生成推测性排名。"
		return result, fmt.Sprintf("可比较这两个活动：%s（%s）和 %s（%s）。", result.Activities[0].Title,
			result.Activities[0].Schedule, result.Activities[1].Title, result.Activities[1].Schedule), nil
	case agentworkspace.CreateActivity:
		if actorID == "" || s.organizations == nil {
			return result, "发布活动需要登录，并在组织工作台拥有管理员权限。", nil
		}
		organizations, err := s.organizations.ListOrganizations(ctx, actorID)
		if err != nil {
			return result, "", err
		}
		for _, item := range organizations {
			if item.Role == "owner" || item.Role == "admin" {
				if task.PrincipalType == "organization" && task.PrincipalID != item.AccountID {
					continue
				}
				result.Note = "已找到可用的组织工作台。Agent 不会代替你发布活动。"
				result.Actions = []agentworkspace.Action{agentworkspace.AuthorizedOrganizationConsoleAction(actorID, item.ID, item.AccountID)}
				return result, "可以进入组织工作台创建活动草稿，预览后再发布。", nil
			}
		}
		return result, "发布活动需要组织工作台的所有者或管理员权限。", nil
	default:
		return result, "", errors.New("unsupported special agent operation")
	}
}

func pointInBounds(location foundation.Location, bounds *agentworkspace.MapBounds) bool {
	return location.Precision == "point" && location.CoordinateSystem == "wgs84" &&
		location.Latitude != nil && location.Longitude != nil &&
		*location.Latitude >= bounds.South && *location.Latitude <= bounds.North &&
		*location.Longitude >= bounds.West && *location.Longitude <= bounds.East
}
