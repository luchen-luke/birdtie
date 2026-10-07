package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"strings"
	"testing"
	"time"
)

// These are transport-only spies over the actual registered Task GET. No DB,
// verified people, production supply or model provider is used by this test.
type currentReadonlySearchHTTPSpy struct {
	*httpActionSafetyTaskStore
	reads, late, legacy int
	in                  agenttool.CurrentSearch
	err                 error
	lateErr             error
	empty               bool
	legacyFinal         int
}

func currentReadonlySearchSource(kind string) arp.Receipt {
	now := time.Now().UTC()
	ref := arp.Ref{Type: kind, ID: httpActionSafetyActivity}
	r := arp.Receipt{Items: []arp.Item{{Entity: ref, Title: "同一当前对象", Summary: "合成获准来源", Scope: arp.AuthorizedView, Detail: &ref, SourceVersion: "actual-source-version"}}, PublicCommercialRefs: []arp.Ref{}, ObservedAt: now, ValidUntil: now.Add(20 * time.Second), Proof: strings.Repeat("a", 64), Seal: strings.Repeat("b", 64)}
	return r
}
func (s *currentReadonlySearchHTTPSpy) ReadAgentResultProjection(_ context.Context, a arp.Access, q arp.Query) (arp.Receipt, error) {
	s.legacy++
	src := currentReadonlySearchSource(q.Kind)
	if q.Kind == "activity" {
		src.Activities = []foundation.Activity{{ID: httpActionSafetyActivity, Visibility: "invite_only", Title: "原领域允许的邀请活动"}}
	}
	return src, nil
}
func (s *currentReadonlySearchHTTPSpy) RevalidateAgentResultProjection(context.Context, arp.Access, arp.Query, arp.Receipt) error {
	s.legacyFinal++
	return s.lateErr
}
func (s *currentReadonlySearchHTTPSpy) ReadOwnCurrentSearch(_ context.Context, q agenttool.CurrentSearch) (agenttool.CurrentSearchReceipt, error) {
	s.reads++
	s.in = q
	if s.err != nil {
		return agenttool.CurrentSearchReceipt{}, s.err
	}
	src := currentReadonlySearchSource(q.Query.Kind)
	if s.empty {
		src.Items = []arp.Item{}
	}
	desc, _ := agenttool.Lookup(agenttool.CurrentSearchTool(q.Query.Kind))
	d := agenttool.Decision{SchemaVersion: agenttool.Schema, DecisionID: httpActionSafetyTaskID, ActionID: httpActionSafetyTaskID, LogicalOperationID: q.Access.TaskID, Disposition: agenttool.Allow, ReasonCodes: []string{"CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY"}, Tool: agenttool.CurrentSearchTool(q.Query.Kind), ToolVersion: desc.Version, ActorID: q.Access.Actor.ID, AgentID: httpActionSafetyOrgID, SubjectType: "PERSON", SubjectID: q.Access.Actor.ID, PolicyVersion: strings.Repeat("c", 64), ResourceVersion: src.Proof, ArgumentsDigest: agenttool.CurrentSearchDigest(q), Purpose: desc.Purpose, DataDestinations: []string{"LOCAL_OWNER"}, ObservedAt: src.ObservedAt, ExpiresAt: src.ValidUntil}
	return agenttool.CurrentSearchReceipt{Decision: d, Source: src, Seal: strings.Repeat("d", 64)}, nil
}
func (s *currentReadonlySearchHTTPSpy) RevalidateOwnCurrentSearch(_ context.Context, q agenttool.CurrentSearch, r agenttool.CurrentSearchReceipt) error {
	s.late++
	if q.Access.Actor.ID != s.in.Access.Actor.ID || q.Access.SessionDigest != s.in.Access.SessionDigest || !r.Valid(q) {
		return agenttool.ErrDenied
	}
	return s.lateErr
}

func TestCurrentReadonlyRegisteredTaskGETUsesActualToolPort(t *testing.T) {
	s, h, tasks, token := httpActionSafetyServer(t, nil)
	tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalID: httpActionSafetyOwner, PrincipalType: "person", ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", Intent: agentworkspace.FindPerson, Status: agentworkspace.TaskCompleted, Query: "找公开成员", Filters: map[string]string{}, Conversation: []agentworkspace.Message{}}
	f := &currentReadonlySearchHTTPSpy{httpActionSafetyTaskStore: tasks}
	s.agent = f
	code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	if code != 200 {
		t.Fatalf("original registered GET failed: %d %s", code, raw)
	}
	t.Logf("SYNTHETIC_REGISTERED_TASK_GET_WIRE %s", raw)
	if f.reads != 1 || f.late != 1 || f.legacy != 0 {
		t.Fatalf("registered GET bypassed current READ adapter reads=%d final=%d legacy=%d wire=%s", f.reads, f.late, f.legacy, raw)
	}
	var wire struct{ Data agentworkspace.Results }
	if json.Unmarshal(raw, &wire) != nil || len(wire.Data.ResultSet.Items) != 1 || wire.Data.ResultSet.Items[0].Entity.Type != "person" {
		t.Fatal("original person ref lost", string(raw))
	}
}

func TestCurrentReadonlyRegisteredSearchPOSTGETAndTrueEmpty(t *testing.T) {
	for _, tc := range []struct {
		operation, query, kind, method string
		empty                          bool
	}{{agentworkspace.FindPerson, "找公开成员", "person", "POST", false}, {agentworkspace.FindPlace, "找地点", "place", "GET", false}, {agentworkspace.FindPerson, "找公开成员", "person", "GET", true}} {
		t.Run(tc.kind+tc.method, func(t *testing.T) {
			s, h, tasks, token := httpActionSafetyServer(t, nil)
			s.catalog = anonymousRecoveryPublicCatalog{}
			tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalID: httpActionSafetyOwner, PrincipalType: "person", ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", Intent: tc.operation, Status: agentworkspace.TaskCompleted, Query: tc.query, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}}
			f := &currentReadonlySearchHTTPSpy{httpActionSafetyTaskStore: tasks, empty: tc.empty}
			s.agent = f
			path, body := "/v1/me/agent-tasks/"+httpActionSafetyTaskID, ""
			if tc.method == "POST" {
				path = "/v1/cities/aberdeen-gb/agent/tasks"
				body = `{"query":"` + tc.query + `"}`
			}
			code, raw := httpActionSafetyCall(t, h, token, "", tc.method, path, body)
			t.Logf("SYNTHETIC_REGISTERED_%s_%s_EMPTY_%t_WIRE %s", tc.method, tc.kind, tc.empty, raw)
			if code != 200 || f.reads != 1 || f.late != 1 || f.legacy != 0 {
				t.Fatalf("actual consumer counts %d %s %d/%d/%d", code, raw, f.reads, f.late, f.legacy)
			}
			if f.in.Access.Actor.ID != httpActionSafetyOwner || f.in.Access.SessionDigest == ([32]byte{}) || f.in.Access.TaskID != httpActionSafetyTaskID || f.in.Query.Kind != tc.kind {
				t.Fatal("real request binding lost")
			}
			var wire struct{ Data agentworkspace.Results }
			json.Unmarshal(raw, &wire)
			status := map[bool]string{true: "empty", false: "ready"}[tc.empty]
			if wire.Data.ResultSet.Status != status || strings.Contains(string(raw), "PRIVATE_FILTER_CANARY") {
				t.Fatal("fake empty or private input leak", string(raw))
			}
		})
	}
}
func TestCurrentReadonlyRegisteredActivityKeepsOriginalAuthorizedProjection(t *testing.T) {
	s, h, tasks, token := httpActionSafetyServer(t, nil)
	tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalID: httpActionSafetyOwner, PrincipalType: "person", ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskCompleted, Query: "找活动", Filters: map[string]string{}, Conversation: []agentworkspace.Message{}}
	f := &currentReadonlySearchHTTPSpy{httpActionSafetyTaskStore: tasks}
	s.agent = f
	code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	t.Logf("SYNTHETIC_ORIGINAL_AUTHORIZED_ACTIVITY_WIRE %s", raw)
	var wire struct{ Data agentworkspace.Results }
	json.Unmarshal(raw, &wire)
	if code != 200 || f.reads != 0 || f.late != 0 || f.legacy != 1 || f.legacyFinal != 1 || len(wire.Data.Activities) != 1 || wire.Data.Activities[0].Visibility != "invite_only" {
		t.Fatal("ordinary granted activity confused with model PUBLIC tool", code, string(raw), f)
	}
	f.lateErr = arp.ErrChanged
	code, raw = httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	if code != 409 || strings.Contains(string(raw), "原领域允许的邀请活动") {
		t.Fatal("original final revalidation lost", code, string(raw))
	}
}
func TestCurrentReadonlyRegisteredSearchErrorsAndLateRevocationNeverBecomeEmpty(t *testing.T) {
	for _, late := range []bool{false, true} {
		for name, e := range map[string]error{"sourceABA": agenttool.ErrChanged, "policyWithdrawn": agenttool.ErrDenied, "sessionExpired": identity.ErrUnauthorized, "unavailable": agenttool.ErrUnavailable, "unknown": errors.New("PRIVATE_ERROR_CANARY")} {
			t.Run(name+map[bool]string{true: "late", false: "early"}[late], func(t *testing.T) {
				s, h, tasks, token := httpActionSafetyServer(t, nil)
				tasks.task = agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalID: httpActionSafetyOwner, PrincipalType: "person", ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", Intent: agentworkspace.FindPerson, Status: agentworkspace.TaskCompleted, Query: "找公开成员", Filters: map[string]string{}, Conversation: []agentworkspace.Message{}}
				f := &currentReadonlySearchHTTPSpy{httpActionSafetyTaskStore: tasks}
				if late {
					f.lateErr = e
				} else {
					f.err = e
				}
				s.agent = f
				code, raw := httpActionSafetyCall(t, h, token, "", "GET", "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
				if code == 200 || strings.Contains(string(raw), "同一当前对象") || strings.Contains(string(raw), "PRIVATE_ERROR_CANARY") || strings.Contains(string(raw), "\"items\"") {
					t.Fatal("failed current read released data", code, string(raw))
				}
				if f.legacy != 0 || f.reads != 1 || late && f.late != 1 {
					t.Fatal("fallback or final bypass", f)
				}
			})
		}
	}
}
