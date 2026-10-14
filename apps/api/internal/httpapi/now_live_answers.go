package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// This original server option is a trusted dependency, never request input.
// With no configured native boundary the existing ordinary paths are retained.
func WithNowLiveAnswers(answers agentworkspace.LiveAnswers) Option {
	return func(s *server) { s.liveAnswers = answers }
}

type nowLiveReplyKey struct{}
type nowHumanReplyKey struct{}
type nowLocalReplyKey struct{}

var errLocalPlaceAnswerUnavailable = errors.New("local place facts unavailable without model eligibility")

func (s *server) finishCurrentNowQuery(r *http.Request, digest [32]byte, task agentworkspace.Task, message string) (agentworkspace.Task, *http.Request, error) {
	if task.Status != agentworkspace.TaskFailed {
		task.Status = agentworkspace.TaskActive
	}
	if port, ok := s.agent.(agentworkspace.LocalPlaceReplyPort); ok && r.Method == http.MethodPost && digest != ([32]byte{}) && task.PrincipalType == "person" && task.PrincipalID != "" && task.ActingUserID == task.PrincipalID && task.ContextType == "CITY" && task.Status != agentworkspace.TaskFailed && r.Header.Get("X-Birdtie-Organization-Workspace") == "" {
		if task.Intent == agentworkspace.FindPlace {
			current, e := s.agent.UpdateTask(r.Context(), task)
			if e != nil {
				return task, r, e
			}
			raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(current))
			if e != nil {
				return task, r, e
			}
			reply, handled, e := port.TryOwnLocalPlaceReply(r.Context(), arp.Access{Actor: identity.Actor{ID: current.PrincipalID, AccountType: "person"}, SessionDigest: digest, TaskID: current.ID, ExpectedTask: raw})
			if e != nil {
				return current, r, e
			}
			if handled {
				if reply == nil {
					return current, r, agentworkspace.ErrSourcedAnswer
				}
				r = r.WithContext(context.WithValue(r.Context(), nowHumanReplyKey{}, true))
				return reply.Task(), r.WithContext(context.WithValue(r.Context(), nowLocalReplyKey{}, reply)), nil
			}
			task = current
		}
	}
	// A closed explicit refinement with a genuinely empty native member set is
	// a human rules reply. Nonempty/insufficient licensed facts still reach the
	// original model path and keep its errors; no provider failure is caught.
	var emptyHandled bool
	var emptyErr error
	task, r, emptyHandled, emptyErr = s.tryEmptyPlaceRefinement(r, digest, task)
	if emptyErr != nil || emptyHandled {
		return task, r, emptyErr
	}
	if s.liveAnswers != nil && r.Method == http.MethodPost && digest != ([32]byte{}) && task.PrincipalType == "person" && task.PrincipalID != "" && task.ActingUserID == task.PrincipalID && task.Status != agentworkspace.TaskFailed && r.Header.Get("X-Birdtie-Organization-Workspace") == "" && s.liveAnswers.Eligible(r.Context(), digest, task) {
		// Final filters/results are persisted while ACTIVE. The same native Run
		// writes its validated answer and final status after the authorized provider calls.
		current, e := s.agent.UpdateTask(r.Context(), task)
		if e != nil {
			return task, r, e
		}
		reply, e := s.liveAnswers.Execute(r.Context(), digest, current)
		if e != nil {
			return current, r, e
		}
		if reply == nil {
			return current, r, agentworkspace.ErrSourcedAnswer
		}
		return reply.Task(), r.WithContext(context.WithValue(r.Context(), nowLiveReplyKey{}, reply)), nil
	}
	if r.Method == http.MethodPost && digest != ([32]byte{}) && task.Intent == agentworkspace.FindPlace &&
		task.PrincipalType == "person" && task.PrincipalID != "" && task.ActingUserID == task.PrincipalID &&
		task.ContextType == "CITY" && task.Status != agentworkspace.TaskFailed && r.Header.Get("X-Birdtie-Organization-Workspace") == "" &&
		isBoundedLocalPlaceFactQuestion(task.Filters["currentQuery"]) {
		return task, r, errLocalPlaceAnswerUnavailable
	}
	if port, ok := s.agent.(agentworkspace.HumanReplyResultsPort); ok && r.Method == http.MethodPost && digest != ([32]byte{}) && task.Status != agentworkspace.TaskFailed && task.PrincipalType == "person" && task.ActingUserID == task.PrincipalID && task.ContextType == "CITY" && r.Header.Get("X-Birdtie-Organization-Workspace") == "" {
		kind := nativeResultKind(task.Intent)
		if kind == "activity" || kind == "place" || kind == "organization" {
			// Persist the final filters (including comparison resultIDs) before
			// capturing the exact ACTIVE Task; no ExpectedTask guard is bypassed.
			current, e := s.agent.UpdateTask(r.Context(), task)
			if e != nil {
				return task, r, e
			}
			raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(current))
			if e != nil {
				return current, r, e
			}
			current, e = port.CaptureOwnHumanReply(r.Context(), arp.Access{Actor: identity.Actor{ID: current.PrincipalID, AccountType: "person"}, SessionDigest: digest, TaskID: current.ID, ExpectedTask: raw})
			if e != nil {
				return task, r, e
			}
			return current, r.WithContext(context.WithValue(r.Context(), nowHumanReplyKey{}, true)), nil
		}
	}
	if task.Status != agentworkspace.TaskFailed {
		task.Status = agentworkspace.TaskCompleted
	}
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: message})
	if task.PrincipalID != "" {
		current, e := s.agent.UpdateTask(r.Context(), task)
		return current, r, e
	}
	return task, r, nil
}

func decorateCurrentNowReply(r *http.Request, result agentworkspace.Results) (agentworkspace.Results, error) {
	if result.OnlinePlaceSearch != nil {
		_, continuation := r.Context().Value(onlineRestaurantFollowupKey{}).(bool)
		if r.Method != http.MethodGet && !continuation {
			result.Message = onlineRestaurantMessage(result.OnlinePlaceSearch, len(result.Places))
		}
		if r.Method == http.MethodGet && result.Task != nil && len(result.Task.Conversation) > 0 {
			result.Message = result.Task.Conversation[len(result.Task.Conversation)-1].Text
		}
		result.Mode = "online_places"
		result.Note = "OpenStreetMap 公开地点资料已保存；评分与营业状态尚未核验。"
		return result, nil
	}
	if _, ok := r.Context().Value(nowEmptyPlaceReplyKey{}).(agentworkspace.EmptyPlaceReplyRead); ok {
		if result.Task == nil || len(result.ResultSet.Items) != 0 || len(result.Places) != 0 || len(result.Activities) != 0 || result.ResultSet.AnswerBinding != nil || len(result.ResultSet.Sources) != 0 {
			return result, arp.ErrChanged
		}
		result.Mode = "rules"
		result.Note = "当前授权的站内地点筛选为空；本轮未调用联网搜索或模型。"
		return result, nil
	}
	if reply, ok := r.Context().Value(nowLocalReplyKey{}).(agentworkspace.LiveReply); ok {
		if result.Task == nil {
			return result, agentworkspace.ErrSourcedAnswer
		}
		result.ResultSet.GeneratedAt = result.Task.UpdatedAt
		answer, e := reply.Answer(result.RequestID)
		if e != nil {
			return result, e
		}
		result, e = agentworkspace.ApplySourcedAnswer(result, *result.Task, result.RequestID, answer, time.Now())
		if e == nil {
			result.Mode = "rules"
			result.Note = "使用许可仍有效的站内地点事实；本轮未调用联网搜索或模型。"
		}
		return result, e
	}
	reply, ok := r.Context().Value(nowLiveReplyKey{}).(agentworkspace.LiveReply)
	if !ok {
		return result, nil
	}
	if result.Task == nil {
		return result, agentworkspace.ErrSourcedAnswer
	}
	answer, e := reply.Answer(result.RequestID)
	if e != nil {
		return result, e
	}
	result, e = agentworkspace.ApplySourcedAnswer(result, *result.Task, result.RequestID, answer, time.Now())
	if e == nil {
		result.Mode = "live"
		result.Note = "回答依据联网来源；地图与卡片展示同一批站内实体。"
		for _, message := range result.Task.Conversation {
			if message.LicensedPlaceModelEvidence != nil && result.ResultSet.AnswerBinding != nil && message.LicensedPlaceModelEvidence.BindingID == result.ResultSet.AnswerBinding.RunID {
				result.Note = "回答依据许可有效的站内公共地点事实；本轮仅调用模型，未调用联网搜索。"
				break
			}
		}
	}
	return result, e
}

func revalidateCurrentNowReply(r *http.Request) error {
	if read, ok := r.Context().Value(nowEmptyPlaceReplyKey{}).(agentworkspace.EmptyPlaceReplyRead); ok {
		return read.Revalidate(r.Context())
	}
	if reply, ok := r.Context().Value(nowLocalReplyKey{}).(agentworkspace.LiveReply); ok {
		return reply.Revalidate(r.Context())
	}
	if reply, ok := r.Context().Value(nowLiveReplyKey{}).(agentworkspace.LiveReply); ok {
		return reply.Revalidate(r.Context())
	}
	return nil
}
