package httpapi

import (
	"context"
	"encoding/json"
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

func (s *server) finishCurrentNowQuery(r *http.Request, digest [32]byte, task agentworkspace.Task, message string) (agentworkspace.Task, *http.Request, error) {
	if task.Status != agentworkspace.TaskFailed {
		task.Status = agentworkspace.TaskActive
	}
	if s.liveAnswers != nil && r.Method == http.MethodPost && digest != ([32]byte{}) && task.PrincipalType == "person" && task.PrincipalID != "" && task.ActingUserID == task.PrincipalID && task.Status != agentworkspace.TaskFailed && r.Header.Get("X-Birdtie-Organization-Workspace") == "" && s.liveAnswers.Eligible(r.Context(), digest, task) {
		// Final filters/results are persisted while ACTIVE. The same native Run
		// writes its validated answer and final status after the two API calls.
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
	}
	return result, e
}

func revalidateCurrentNowReply(r *http.Request) error {
	if reply, ok := r.Context().Value(nowLiveReplyKey{}).(agentworkspace.LiveReply); ok {
		return reply.Revalidate(r.Context())
	}
	return nil
}
