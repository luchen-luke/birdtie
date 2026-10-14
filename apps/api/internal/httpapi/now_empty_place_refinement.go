package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type nowEmptyPlaceReplyKey struct{}

func (s *server) tryEmptyPlaceRefinement(r *http.Request, digest [32]byte, task agentworkspace.Task) (agentworkspace.Task, *http.Request, bool, error) {
	port, ok := s.agent.(agentworkspace.EmptyPlaceReplyResultsPort)
	if !ok || r.Method != http.MethodPost || digest == ([32]byte{}) || r.Header.Get("X-Birdtie-Organization-Workspace") != "" || !agentworkspace.IsExplicitPlaceRefinementTask(task) {
		return task, r, false, nil
	}
	current, e := s.agent.UpdateTask(r.Context(), task)
	if e != nil {
		return task, r, false, e
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(current))
	if e != nil {
		return current, r, false, e
	}
	reply, read, handled, e := port.TryOwnEmptyPlaceRefinementReply(r.Context(), arp.Access{Actor: identity.Actor{ID: current.PrincipalID, AccountType: "person"}, SessionDigest: digest, TaskID: current.ID, ExpectedTask: raw})
	if e != nil || !handled {
		return current, r, false, e
	}
	i := len(reply.Conversation) - 1
	if read == nil || reply.ID != current.ID || reply.PrincipalType != current.PrincipalType || reply.PrincipalID != current.PrincipalID || reply.ActingUserID != current.ActingUserID || reply.CityID != current.CityID || reply.ContextType != current.ContextType || reply.Intent != agentworkspace.FindPlace || reply.Status != agentworkspace.TaskCompleted || i != len(current.Conversation) || agentworkspace.ValidateReplyMembership(reply, i) != nil {
		return current, r, false, arp.ErrChanged
	}
	if reply.Query != current.Query || reply.ContextID != current.ContextID || !reflect.DeepEqual(reply.Filters, current.Filters) || !reflect.DeepEqual(reply.Conversation[:i], current.Conversation) {
		return current, r, false, arp.ErrChanged
	}
	m := reply.Conversation[i]
	if m.ResultMembership.Kind != "place" || len(m.ResultMembership.Refs) != 0 || m.LocalReplyEvidence != nil || m.LicensedPlaceModelEvidence != nil || m.SourceRunID != "" || m.SourceEvidenceDigest != "" || len(m.Sources) != 0 {
		return current, r, false, arp.ErrChanged
	}
	r = r.WithContext(context.WithValue(r.Context(), nowHumanReplyKey{}, true))
	r = r.WithContext(context.WithValue(r.Context(), nowEmptyPlaceReplyKey{}, read))
	return reply, r, true, nil
}
