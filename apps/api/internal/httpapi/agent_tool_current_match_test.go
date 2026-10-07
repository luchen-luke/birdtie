package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"net/http"
	"strings"
	"testing"
	"time"
)

type currentReadonlyMatchHTTPSpy struct {
	newpeople.Store
	reads, late, legacy int
	in                  agenttool.CurrentMatch
	err                 error
	lateErr             error
	candidate           bool
}
type currentReadonlyMatchHTTPCatalog struct {
	foundation.PublicCatalog
	*currentReadonlyMatchHTTPSpy
}

func currentReadonlyMatchSource(source string) newpeople.HumanReceipt {
	return newpeople.HumanReceipt{Response: newpeople.Response{Source: "RULE_BASED", RuleVersion: newpeople.RuleVersion, SourceIntentID: source, Candidates: []newpeople.Candidate{}}, Proof: strings.Repeat("a", 64), Seal: strings.Repeat("b", 64), ExpiresAt: time.Now().UTC().Add(20 * time.Second)}
}
func (s *currentReadonlyMatchHTTPSpy) ReadHumanNewPeople(_ context.Context, a identity.Actor, d [32]byte, source string) (newpeople.HumanReceipt, error) {
	s.legacy++
	return currentReadonlyMatchSource(source), nil
}
func (s *currentReadonlyMatchHTTPSpy) RevalidateHumanNewPeople(context.Context, identity.Actor, [32]byte, newpeople.HumanReceipt) error {
	return nil
}
func (s *currentReadonlyMatchHTTPSpy) ReadOwnCurrentMatch(_ context.Context, q agenttool.CurrentMatch) (agenttool.CurrentMatchReceipt, error) {
	s.reads++
	s.in = q
	if s.err != nil {
		return agenttool.CurrentMatchReceipt{}, s.err
	}
	src := currentReadonlyMatchSource(q.SourceIntentID)
	if s.candidate {
		src.Response.Candidates = []newpeople.Candidate{{SourceIntentID: q.SourceIntentID, CandidateIntentID: privateProfileHTTPForeign, AccountID: privateProfileHTTPForeign, DisplayName: "当前候选", Category: "badminton", Modality: "ONLINE", ReasonCodes: []string{"CATEGORY_EQUAL"}, Reasons: []string{"当前明确声明相容"}}}
	}
	at := src.ExpiresAt.Add(-20 * time.Second)
	meta, _ := agenttool.Lookup(agenttool.PersonMatch)
	d := agenttool.Decision{SchemaVersion: agenttool.Schema, DecisionID: privateProfileHTTPAgent, ActionID: privateProfileHTTPAgent, LogicalOperationID: q.SourceIntentID, Disposition: agenttool.Allow, ReasonCodes: []string{"CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY"}, Tool: agenttool.PersonMatch, ToolVersion: meta.Version, ActorID: q.Actor.ID, AgentID: privateProfileHTTPAgent, SubjectType: "PERSON", SubjectID: q.Actor.ID, PolicyVersion: strings.Repeat("c", 64), ResourceVersion: src.Proof, ArgumentsDigest: agenttool.CurrentMatchDigest(q), Purpose: meta.Purpose, DataDestinations: []string{"LOCAL_OWNER"}, ObservedAt: at, ExpiresAt: src.ExpiresAt}
	return agenttool.CurrentMatchReceipt{Decision: d, Source: src, ObservedAt: at, Seal: strings.Repeat("d", 64)}, nil
}
func (s *currentReadonlyMatchHTTPSpy) RevalidateOwnCurrentMatch(_ context.Context, q agenttool.CurrentMatch, r agenttool.CurrentMatchReceipt) error {
	s.late++
	if !r.Valid(q) {
		return agenttool.ErrDenied
	}
	return s.lateErr
}
func TestCurrentReadonlyRegisteredMatchGETUsesOriginalOptInPort(t *testing.T) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	f := &currentReadonlyMatchHTTPSpy{}
	h := New(currentReadonlyMatchHTTPCatalog{currentReadonlyMatchHTTPSpy: f}, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w := messagePolicyUnitRequest(h, http.MethodGet, "/v1/me/new-people/candidates?sourceIntentId="+privateProfileHTTPAgent, "", token, nil)
	t.Logf("SYNTHETIC_REGISTERED_MATCH_EMPTY_WIRE %s", w.Body.String())
	if w.Code != 200 {
		t.Fatal("original GET", w.Code, w.Body.String())
	}
	if f.reads != 1 || f.late != 1 || f.legacy != 0 {
		t.Fatalf("registered Match GET bypassed current adapter reads=%d final=%d legacy=%d wire=%s", f.reads, f.late, f.legacy, w.Body.String())
	}
}

func TestCurrentReadonlyRegisteredMatchKeepsOriginalCandidateContractNoInvitation(t *testing.T) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	f := &currentReadonlyMatchHTTPSpy{candidate: true}
	h := New(currentReadonlyMatchHTTPCatalog{currentReadonlyMatchHTTPSpy: f}, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/new-people/candidates?sourceIntentId="+privateProfileHTTPAgent, "", token, nil)
	t.Logf("SYNTHETIC_REGISTERED_MATCH_CANDIDATE_WIRE %s", w.Body.String())
	var wire struct{ Data newpeople.Response }
	json.Unmarshal(w.Body.Bytes(), &wire)
	if w.Code != 200 || f.reads != 1 || f.late != 1 || f.in.Actor != a.actor || f.in.SessionDigest != a.digest || len(wire.Data.Candidates) != 1 || wire.Data.Candidates[0].AccountID != privateProfileHTTPForeign {
		t.Fatal("actual candidate transport changed", w.Code, w.Body.String())
	}
	for _, field := range []string{"constraints", "latitude", "longitude", "onlinePlatform", "approved", "accepted", "permission_override"} {
		if strings.Contains(w.Body.String(), `"`+field+`"`) {
			t.Fatal("private scope or authority leaked", field)
		}
	}
}
func TestCurrentReadonlyRegisteredMatchScopeAndLateFailures(t *testing.T) {
	for _, name := range []string{"owner_override", "organization", "bad_source", "withdrawn_source", "disabled_optin", "policy_aba", "late_session", "unknown"} {
		t.Run(name, func(t *testing.T) {
			_, _, a, token := messagePolicyUnitHTTP(t)
			f := &currentReadonlyMatchHTTPSpy{candidate: true}
			h := New(currentReadonlyMatchHTTPCatalog{currentReadonlyMatchHTTPSpy: f}, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
			path := "/v1/me/new-people/candidates?sourceIntentId=" + privateProfileHTTPAgent
			var change func(*http.Request)
			switch name {
			case "owner_override":
				path += "&ownerId=foreign"
			case "organization":
				change = func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign) }
			case "bad_source":
				path = "/v1/me/new-people/candidates?sourceIntentId=guess"
			case "withdrawn_source":
				f.err = newpeople.ErrNotFound
			case "disabled_optin":
				f.err = newpeople.ErrForbidden
			case "policy_aba":
				f.lateErr = agenttool.ErrChanged
			case "late_session":
				f.lateErr = identity.ErrUnauthorized
			case "unknown":
				f.lateErr = errors.New("PRIVATE_NATIVE_FAILURE")
			}
			w := messagePolicyUnitRequest(h, "GET", path, "", token, change)
			if w.Code == 200 || strings.Contains(w.Body.String(), "当前候选") || strings.Contains(w.Body.String(), "PRIVATE_NATIVE_FAILURE") || strings.Contains(w.Body.String(), "\"candidates\"") {
				t.Fatal("current match denied/unknown became empty", w.Code, w.Body.String())
			}
			if f.legacy != 0 {
				t.Fatal("current failure fell back to legacy")
			}
		})
	}
}
