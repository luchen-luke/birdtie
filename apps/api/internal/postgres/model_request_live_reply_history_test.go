package postgres

import (
	"context"
	"errors"
	"testing"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

// Pure contract checks only: no SQL, provider, credentials or device evidence.
func liveReplyHistoryUnitTask() agentworkspace.Task {
	return agentworkspace.Task{ID: "11111111-1111-4111-8111-111111111111", PrincipalType: "person", PrincipalID: "22222222-2222-4222-8222-222222222222", ActingUserID: "22222222-2222-4222-8222-222222222222", CityID: "aberdeen-gb", ContextType: "CITY", Status: agentworkspace.TaskActive, Intent: agentworkspace.FindPlace, Query: "找地点", Filters: map[string]string{"currentQuery": "找地点", "searchTerm": "Museum"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "找地点"}}}
}

func TestModelLiveReplyHistoryCurrentNativeQueryGuard(t *testing.T) {
	for _, mode := range []string{"place", "activity", "organization", "tailMismatch", "assistantTail", "completed", "pending", "malformedBounds"} {
		t.Run(mode, func(t *testing.T) {
			task := liveReplyHistoryUnitTask()
			allowed := true
			switch mode {
			case "activity":
				task.Intent = agentworkspace.FindActivity
			case "organization":
				task.Intent = agentworkspace.FindOrganization
			case "tailMismatch":
				task.Conversation[0].Text = "another question"
				allowed = false
			case "assistantTail":
				task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: "old reply"})
				allowed = false
			case "completed":
				task.Status = agentworkspace.TaskCompleted
				allowed = false
			case "pending":
				task.Intent = "PENDING"
				allowed = false
			case "malformedBounds":
				task.Filters["mapWest"] = "-2.2"
				allowed = false
			}
			q, err := replyCaptureQuery(task)
			if allowed {
				if err != nil || !q.Valid() || q.CityID != task.CityID || q.SearchTerm != task.Filters["searchTerm"] {
					t.Fatal("current native query was not preserved", err)
				}
			} else if !errors.Is(err, arp.ErrDenied) {
				t.Fatal("invalid current native query accepted", err)
			}
		})
	}
}

func TestModelLiveReplyHistoryTaskGetterCopiesMembership(t *testing.T) {
	task := liveReplyHistoryUnitTask()
	task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "assistant", Text: "UNIT actual validated model text"})
	ref := arp.Ref{Type: "place", ID: "33333333-3333-4333-8333-333333333333"}
	member, err := agentworkspace.NewReplyMembership(task, 1, "place", []arp.Ref{ref})
	if err != nil {
		t.Fatal(err)
	}
	task.Conversation[1].ResultMembership = member
	reply := &nativeLiveSourceReply{task: task}
	copy := reply.Task()
	copy.Conversation[1].ResultMembership.Refs[0].ID = "44444444-4444-4444-8444-444444444444"
	copy.Conversation[1].ResultMembership.ResultSetID = "forged"
	if got := reply.Task(); got.Conversation[1].ResultMembership.Refs[0] != ref || got.Conversation[1].ResultMembership.ResultSetID != member.ResultSetID || agentworkspace.ValidateReplyMembership(got, 1) != nil {
		t.Fatal("presentation getter exposed retained native membership")
	}
}

func TestModelLiveReplyHistoryPrivatePersistenceRejectsNoTransaction(t *testing.T) {
	var store *Store
	if _, handle, err := store.persistReplyMembershipTx(context.Background(), nil, arp.Access{}, liveReplyHistoryUnitTask(), nil, "place", arp.Receipt{}, agentworkspace.Message{Role: "assistant", Text: "not authority"}); !errors.Is(err, arp.ErrDenied) || handle != nil {
		t.Fatal("ordinary data persisted without original native transaction", err)
	}
}
