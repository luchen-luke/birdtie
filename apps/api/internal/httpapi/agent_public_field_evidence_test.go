package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// SYNTHETIC_TRANSPORT_ONLY: actual registered Task GET, not PostgreSQL source
// authorization, real activities, native login or production supply evidence.
type publicFieldEvidenceHTTPSpy struct {
	*httpActionSafetyTaskStore
	source           arp.Receipt
	readErr, lateErr error
	reads, late      int
}

func (s *publicFieldEvidenceHTTPSpy) ReadAgentResultProjection(context.Context, arp.Access, arp.Query) (arp.Receipt, error) {
	s.reads++
	return s.source, s.readErr
}
func (s *publicFieldEvidenceHTTPSpy) RevalidateAgentResultProjection(context.Context, arp.Access, arp.Query, arp.Receipt) error {
	s.late++
	return s.lateErr
}
func publicFieldEvidenceHTTPSource() arp.Receipt {
	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)
	ref := arp.Ref{Type: "activity", ID: httpActionSafetyActivity}
	return arp.Receipt{Items: []arp.Item{{Entity: ref, Title: "同一公开活动", Scope: arp.AuthorizedView, Detail: &ref}},
		Activities:           []foundation.Activity{{ID: ref.ID, CityID: "aberdeen-gb", Visibility: "public", Title: "同一公开活动", CategoryCode: "badminton", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), Organizer: foundation.ActivityOrganizer{Type: "person", ID: httpActionSafetyOwner}, Source: foundation.Source{UpdatedAt: now.Add(-time.Hour), ExpiresAt: &expires}}},
		PublicCommercialRefs: []arp.Ref{ref}, ObservedAt: now, ValidUntil: now.Add(20 * time.Second), Proof: strings.Repeat("a", 64), Seal: strings.Repeat("b", 64)}
}
func publicFieldEvidenceHTTPTask() agentworkspace.Task {
	return agentworkspace.Task{ID: httpActionSafetyTaskID, PrincipalID: httpActionSafetyOwner, PrincipalType: "person", ActingUserID: httpActionSafetyOwner, CityID: "aberdeen-gb", Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskCompleted, Query: "找活动", Filters: map[string]string{}, Conversation: []agentworkspace.Message{}, UpdatedAt: time.Now().UTC()}
}
func TestPublicFieldEvidenceHTTPUnitRegisteredGETHasCurrentFieldEvidence(t *testing.T) {
	s, h, tasks, token := httpActionSafetyServer(t, nil)
	tasks.task = publicFieldEvidenceHTTPTask()
	f := &publicFieldEvidenceHTTPSpy{httpActionSafetyTaskStore: tasks, source: publicFieldEvidenceHTTPSource()}
	s.agent = f
	code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
	t.Logf("SYNTHETIC_REGISTERED_PUBLIC_TASK_GET_WIRE %s", raw)
	if code != 200 || f.reads != 1 || f.late != 1 {
		t.Fatalf("original route/final fence changed: code=%d read=%d late=%d wire=%s", code, f.reads, f.late, raw)
	}
	var wire struct {
		Data struct{ ResultSet map[string]json.RawMessage }
	}
	if json.Unmarshal(raw, &wire) != nil || !strings.Contains(string(raw), "同一公开活动") {
		t.Fatal("original authorized entity lost")
	}
	if len(wire.Data.ResultSet["publicFieldEvidence"]) == 0 {
		t.Fatal("registered GET omitted current public field evidence")
	}
	var data struct{ Data agentworkspace.Results }
	if json.Unmarshal(raw, &data) != nil || data.Data.ResultSet.PublicFieldEvidence == nil || len(data.Data.ResultSet.PublicFieldEvidence.FieldEvidenceSet.Claims) != 6 || data.Data.PrincipalType != "PERSON" {
		t.Fatal("actual PERSON response failed current evidence binding")
	}
}

func TestPublicFieldEvidenceHTTPUnitOptionalUnavailableKeepsLegalOriginalResults(t *testing.T) {
	for _, mode := range []string{"invited", "missing_public_ref", "unknown_updated", "expired_source", "unknown_expiry", "empty", "person"} {
		t.Run(mode, func(t *testing.T) {
			s, h, tasks, token := httpActionSafetyServer(t, nil)
			tasks.task = publicFieldEvidenceHTTPTask()
			r := publicFieldEvidenceHTTPSource()
			switch mode {
			case "invited":
				r.Activities[0].Visibility = "invite_only"
				r.PublicCommercialRefs = []arp.Ref{}
			case "missing_public_ref":
				r.PublicCommercialRefs = []arp.Ref{}
			case "unknown_updated":
				r.Activities[0].Source.UpdatedAt = time.Time{}
			case "expired_source":
				v := r.ObservedAt.Add(-time.Second)
				r.Activities[0].Source.ExpiresAt = &v
			case "unknown_expiry":
				r.Activities[0].Source.ExpiresAt = nil
			case "empty":
				r.Items = []arp.Item{}
				r.PublicCommercialRefs = []arp.Ref{}
				r.Activities = []foundation.Activity{}
			case "person":
				tasks.task.Intent = agentworkspace.FindPerson
				r.Items[0].Entity.Type = "person"
				r.Items[0].Detail = &r.Items[0].Entity
				r.PublicCommercialRefs = []arp.Ref{}
				r.Activities = nil
			}
			f := &publicFieldEvidenceHTTPSpy{httpActionSafetyTaskStore: tasks, source: r}
			s.agent = f
			code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
			t.Logf("SYNTHETIC_REGISTERED_PUBLIC_METADATA_%s_WIRE %s", mode, raw)
			var wire struct{ Data agentworkspace.Results }
			json.Unmarshal(raw, &wire)
			if code != 200 || f.reads != 1 || f.late != 1 || len(wire.Data.ResultSet.Items) != len(r.Items) || len(wire.Data.Activities) != len(r.Activities) {
				t.Fatalf("metadata changed old domain read %d %s", code, raw)
			}
			p := wire.Data.ResultSet.PublicFieldEvidence
			if mode == "person" {
				if p != nil {
					t.Fatal("person result borrowed PUBLIC evidence")
				}
				return
			}
			if p == nil {
				t.Fatal("missing explicit metadata status")
			}
			want := 0
			if mode == "unknown_expiry" {
				want = 6
			}
			if len(p.FieldEvidenceSet.Claims) != want {
				t.Fatal("unknown/invited/empty fabricated fields")
			}
			if mode == "expired_source" && (p.Status != "UNAVAILABLE" || p.Budget.Omitted["EXPIRED_SOURCE"] != 1) {
				t.Fatal("expired metadata renewed or original response denied")
			}
			if mode == "empty" && (wire.Data.ResultSet.Status != "empty" || p.Status != "NO_PUBLIC_FIELDS") {
				t.Fatal("real empty mislabeled")
			}
		})
	}
}
func TestPublicFieldEvidenceHTTPUnitOriginalFinalFenceStillRejectsWholeResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"revoked", arp.ErrDenied, 403}, {"source_aba", arp.ErrChanged, 409}, {"session", identity.ErrUnauthorized, 401}, {"unavailable", arp.ErrUnavailable, 503}, {"cancelled", context.Canceled, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			s, h, tasks, token := httpActionSafetyServer(t, nil)
			tasks.task = publicFieldEvidenceHTTPTask()
			f := &publicFieldEvidenceHTTPSpy{httpActionSafetyTaskStore: tasks, source: publicFieldEvidenceHTTPSource(), lateErr: tc.err}
			s.agent = f
			code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
			t.Logf("SYNTHETIC_REGISTERED_PUBLIC_FINAL_%s_WIRE %s", tc.name, raw)
			if code != tc.status || f.reads != 1 || f.late != 1 || strings.Contains(string(raw), "同一公开活动") || strings.Contains(string(raw), "publicFieldEvidence") || strings.Contains(string(raw), `"data"`) {
				t.Fatalf("optional metadata swallowed final refusal %d %s", code, raw)
			}
		})
	}
}
func TestPublicFieldEvidenceHTTPUnitOriginalReadFailureStillFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"denied", arp.ErrDenied, 403}, {"changed", arp.ErrChanged, 409}, {"expired", identity.ErrUnauthorized, 401}, {"unknown", errors.New("RAW_PRIVATE_ERROR_CANARY"), 503}} {
		t.Run(tc.name, func(t *testing.T) {
			s, h, tasks, token := httpActionSafetyServer(t, nil)
			tasks.task = publicFieldEvidenceHTTPTask()
			f := &publicFieldEvidenceHTTPSpy{httpActionSafetyTaskStore: tasks, readErr: tc.err}
			s.agent = f
			code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
			if code != tc.status || f.reads != 1 || f.late != 0 || strings.Contains(string(raw), "RAW_PRIVATE_ERROR_CANARY") || strings.Contains(string(raw), `"data"`) {
				t.Fatalf("read error became metadata unavailability %d %s", code, raw)
			}
		})
	}
}

type publicFieldEvidenceCurrentHTTPSpy struct {
	*publicFieldEvidenceHTTPSpy
	input               agenttool.CurrentSearch
	toolReads, toolLate int
}

func (s *publicFieldEvidenceCurrentHTTPSpy) ReadOwnCurrentSearch(_ context.Context, in agenttool.CurrentSearch) (agenttool.CurrentSearchReceipt, error) {
	s.toolReads++
	s.input = in
	desc, _ := agenttool.Lookup(agenttool.CurrentSearchTool(in.Query.Kind))
	d := agenttool.Decision{SchemaVersion: agenttool.Schema, DecisionID: httpActionSafetyTaskID, ActionID: httpActionSafetyTaskID, LogicalOperationID: in.Access.TaskID, Disposition: agenttool.Allow, ReasonCodes: []string{"CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY"}, Tool: agenttool.CurrentSearchTool(in.Query.Kind), ToolVersion: desc.Version, ActorID: in.Access.Actor.ID, AgentID: httpActionSafetyOrgID, SubjectType: "PERSON", SubjectID: in.Access.Actor.ID, PolicyVersion: strings.Repeat("c", 64), ResourceVersion: s.source.Proof, ArgumentsDigest: agenttool.CurrentSearchDigest(in), Purpose: desc.Purpose, DataDestinations: []string{"LOCAL_OWNER"}, ObservedAt: s.source.ObservedAt, ExpiresAt: s.source.ValidUntil}
	return agenttool.CurrentSearchReceipt{Decision: d, Source: s.source, Seal: strings.Repeat("d", 64)}, nil
}
func (s *publicFieldEvidenceCurrentHTTPSpy) RevalidateOwnCurrentSearch(_ context.Context, in agenttool.CurrentSearch, r agenttool.CurrentSearchReceipt) error {
	s.toolLate++
	if in.Access.Actor.ID != s.input.Access.Actor.ID || in.Access.SessionDigest != s.input.Access.SessionDigest || !r.Valid(in) {
		return agenttool.ErrDenied
	}
	return s.lateErr
}
func TestPublicFieldEvidenceHTTPUnitPlaceUsesOriginalCurrentToolAndFinalPolicy(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "policy_changed"}[late], func(t *testing.T) {
			s, h, tasks, token := httpActionSafetyServer(t, nil)
			s.catalog = anonymousRecoveryPublicCatalog{}
			tasks.task = publicFieldEvidenceHTTPTask()
			tasks.task.Intent = agentworkspace.FindPlace
			r := publicFieldEvidenceHTTPSource()
			source := r.Activities[0].Source
			r.Activities = []foundation.Activity{}
			r.Items[0].Entity.Type = "place"
			ref := r.Items[0].Entity
			r.Items[0].Detail = &ref
			r.PublicCommercialRefs = []arp.Ref{ref}
			r.Places = []foundation.Place{{ID: ref.ID, Name: "同一公开地点", CategoryCode: "sports", Source: source}}
			f := &publicFieldEvidenceCurrentHTTPSpy{publicFieldEvidenceHTTPSpy: &publicFieldEvidenceHTTPSpy{httpActionSafetyTaskStore: tasks, source: r}}
			if late {
				f.lateErr = agenttool.ErrChanged
			}
			s.agent = f
			code, raw := httpActionSafetyCall(t, h, token, "", http.MethodGet, "/v1/me/agent-tasks/"+httpActionSafetyTaskID, "")
			t.Logf("SYNTHETIC_REGISTERED_CURRENT_PLACE_LATE_%t_WIRE %s", late, raw)
			if f.toolReads != 1 || f.toolLate != 1 || f.reads != 0 || f.late != 0 {
				t.Fatal("place bypassed original currentTool policy/final boundary")
			}
			if late {
				if code != 409 || strings.Contains(string(raw), "publicFieldEvidence") {
					t.Fatalf("late policy did not refuse %d %s", code, raw)
				}
				return
			}
			var wire struct{ Data agentworkspace.Results }
			json.Unmarshal(raw, &wire)
			if code != 200 || wire.Data.ResultSet.PublicFieldEvidence == nil || wire.Data.ResultSet.PublicFieldEvidence.QueryKind != "place" || len(wire.Data.ResultSet.PublicFieldEvidence.FieldEvidenceSet.Claims) != 2 {
				t.Fatalf("actual place port did not attach same source evidence %d %s", code, raw)
			}
		})
	}
}
