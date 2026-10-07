package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func nativeResultKind(operation string) string {
	switch operation {
	case agentworkspace.FindActivity, agentworkspace.AreaDiscovery, agentworkspace.RefineResults, agentworkspace.CompareResults:
		return "activity"
	case agentworkspace.FindPlace:
		return "place"
	case agentworkspace.FindOrganization:
		return "organization"
	case agentworkspace.FindPerson:
		return "person"
	case agentworkspace.FindCommunity:
		return "community"
	case agentworkspace.FindBusiness:
		return "business"
	case agentworkspace.FindOpportunity:
		return "opportunity"
	default:
		return "none"
	}
}
func newTypedOperation(operation string) bool {
	switch operation {
	case agentworkspace.FindPerson, agentworkspace.FindCommunity, agentworkspace.FindBusiness, agentworkspace.FindOpportunity:
		return true
	}
	return false
}

func resultProjectionFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(e, arp.ErrInvalid):
		respondError(w, http.StatusBadRequest, "invalid_query")
	case errors.Is(e, arp.ErrDenied):
		respondError(w, http.StatusForbidden, "result_source_denied")
	case errors.Is(e, arp.ErrChanged):
		respondError(w, http.StatusConflict, "result_source_changed")
	case errors.Is(e, agentworkspace.ErrNotFound):
		respondError(w, http.StatusNotFound, "not_found")
	default:
		respondError(w, http.StatusServiceUnavailable, "result_source_unavailable")
	}
}

// The human is explicitly asking for current public native results or their own
// computed opportunities. This is NOT a machine-purpose context grant/model
// request. No Memory/Profile personalization, invitations or other writes run.
func (s *server) prepareNativeAgentResults(r *http.Request, result agentworkspace.Results, digest [32]byte, actor identity.Actor) (agentworkspace.Results, arp.NativeStore, arp.Access, arp.Query, arp.Receipt, error) {
	var a arp.Access
	var q arp.Query
	var receipt arp.Receipt
	native, ok := s.agent.(arp.NativeStore)
	if result.Task == nil || actor.AccountType != "person" || r.Header.Get("X-Birdtie-Organization-Workspace") != "" || result.Task.ContextType == "ONLINE" {
		return result, nil, a, q, receipt, nil // Separate existing native Online/Org contract.
	}
	if !ok {
		if newTypedOperation(result.Task.Intent) {
			return result, nil, a, q, receipt, arp.ErrUnavailable
		}
		// Original non-native interface adapters preserve their original legacy
		// contract only. They are not a native seven-source completion path.
		return result, nil, a, q, receipt, nil
	}
	task := *result.Task
	if task.Status == agentworkspace.TaskFailed || nativeResultKind(task.Intent) == "none" {
		return result, nil, a, q, receipt, nil // Preserve each existing action/private proof.
	}
	q = arp.Query{CityID: task.CityID, Kind: nativeResultKind(task.Intent), SearchTerm: task.Filters["searchTerm"], Category: task.Filters["category"], TimePreference: task.Filters["timePreference"], Closer: task.Filters["distancePreference"] == "closer", CompareIDs: []string{}}
	b, e := agentworkspace.BoundsFromFilters(task.Filters)
	if e != nil {
		return result, nil, a, q, receipt, arp.ErrInvalid
	}
	if b != nil {
		q.Bounds = &arp.Bounds{West: b.West, South: b.South, East: b.East, North: b.North}
	}
	if task.Intent == agentworkspace.CompareResults {
		q.Comparison = true
		for _, v := range result.Activities {
			if len(q.CompareIDs) < 2 {
				q.CompareIDs = append(q.CompareIDs, v.ID)
			}
		}
	}
	raw, e := json.Marshal(task)
	if e != nil {
		return result, nil, a, q, receipt, arp.ErrUnavailable
	}
	a = arp.Access{Actor: actor, SessionDigest: digest, TaskID: task.ID, ExpectedTask: raw}
	if current, yes := s.agent.(agenttool.CurrentSearchPort); yes && agenttool.CurrentSearchTool(q.Kind) != "" {
		input := agenttool.CurrentSearch{Access: a, Query: q}
		var toolReceipt agenttool.CurrentSearchReceipt
		toolReceipt, e = agenttool.ReadCurrentSearch(r.Context(), current, input)
		if e == nil {
			receipt = toolReceipt.Source
			native = &currentToolSearchResponseStore{port: current, input: input, receipt: toolReceipt}
		} else {
			e = currentToolReadError(e)
		}
	} else {
		receipt, e = native.ReadAgentResultProjection(r.Context(), a, q)
	}
	if e != nil {
		return result, nil, a, q, receipt, e
	}
	// Typed Items alone drive this native result. No duplicate legacy ref may
	// choose a different title/ACL or get deleted as an ambiguous projection.
	if q.Kind != "none" {
		result.Activities = receipt.Activities
		result.People = []agentworkspace.Person{}
		result.Groups = []agentworkspace.Group{}
		result.Organizations = []agentworkspace.Organization{}
		result.Places = receipt.Places
		result.NativeProjection = true
		result.ProjectionItems = receipt.Items
		result.PublicCommercialRefs = receipt.PublicCommercialRefs
		result.PublicFieldEvidence, e = arp.BuildPublicFieldEvidence(a, q, receipt)
		if e != nil {
			return result, nil, a, q, receipt, e
		}
		names := map[string]string{"activity": "当前可见活动", "place": "公开地点", "person": "公开成员", "community": "公开社区", "organization": "公开组织", "business": "公开商家", "opportunity": "本人社交机会"}
		result.Message = fmt.Sprintf("已按当前可见来源整理，找到 %d 个%s。", len(receipt.Items), names[q.Kind])
		result.Note = "结果来自当前原生来源；点击卡片查看同一对象，不会自动报名、联系或发送邀请。"
		if q.Kind == "opportunity" {
			result.Note = "根据本人明确的社交意图与当前可见活动按规则整理；仅本人可见，打开或分享的是原活动。"
		}
		result = agentworkspace.WithContract(result, task, result.RequestID)
	}
	return result, native, a, q, receipt, nil
}

// Used in tests as well as the actual registered ordinary POST/special/GET.
// Encoding and any commercial reads complete BEFORE this final native check.
func copyAgentResponseBuffer(w http.ResponseWriter, b *taskResponseBuffer) {
	for key, values := range b.header {
		w.Header()[key] = values
	}
	if b.status == 0 {
		b.status = http.StatusInternalServerError
	}
	w.WriteHeader(b.status)
	_, _ = w.Write(b.Bytes())
}
