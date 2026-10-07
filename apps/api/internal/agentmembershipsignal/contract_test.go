package agentmembershipsignal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"testing"
	"time"
)

func shapeRequest() Request {
	a := agentprofile.PrivateAccess{SessionDigest: [32]byte{1}, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: "11111111-1111-4111-8111-111111111111"}}
	return Request{a, agentcognitive.AgentReference{AgentID: "22222222-2222-4222-8222-222222222222", Principal: a.WorkspacePrincipal, Role: agentruntime.PersonalAgent}, Community, "33333333-3333-4333-8333-333333333333", time.Now().UTC().Add(10 * time.Minute)}
}
func shapeState(r Request) nativeState {
	now := time.Now().UTC()
	v, _ := membershipVersion(r.Kind, now.Add(-time.Second), []byte(`{"offlineShapeOnly":true}`))
	principal := ""
	if r.Kind == Organization {
		principal = "55555555-5555-4555-8555-555555555555"
	}
	return nativeState{now: now, sessionLimit: now.Add(time.Hour), authority: "OFFLINE_SHAPE_ONLY", fact: Evidence{r.Kind, r.MembershipID, "44444444-4444-4444-8444-444444444444", principal, "合成摄影社群", "member", "active", v, now.Add(-time.Second)}}
}
func TestOfflineContractRolesAreShapeOnly(t *testing.T) {
	for _, k := range []Kind{Community, Organization} {
		for _, role := range []string{"member", "moderator", "admin", "owner", "unknown"} {
			t.Run(string(k)+role, func(t *testing.T) {
				r := shapeRequest()
				r.Kind = k
				s := shapeState(r)
				s.fact.Role = role
				out, e := assembleContext(r, s)
				want := validRole(k, role)
				if want != (e == nil) {
					t.Fatal(e)
				}
				if want && (len(out.Evidence) != 1 || out.InterestInferred || out.IdentityVerified || out.FriendEstablished || out.MemoryPromotionAllowed || out.ModelAccess != "UNAVAILABLE") {
					t.Fatal("shape introduced inference")
				}
			})
		}
	}
}
func TestOfflineContractRejectsUntrustedInput(t *testing.T) {
	cases := map[string]func(*Request){"unknown": func(r *Request) { r.Kind = "UNKNOWN" }, "zeroID": func(r *Request) { r.MembershipID = "00000000-0000-0000-0000-000000000000" }, "crossPrincipal": func(r *Request) { r.Agent.Principal.ID = r.MembershipID }, "orgActor": func(r *Request) {
		r.Access.WorkspacePrincipal.Type = actorref.Organization
		r.Agent.Principal = r.Access.WorkspacePrincipal
		r.Agent.Role = agentruntime.OrganizationAgent
	}, "business": func(r *Request) { r.Access.WorkspacePrincipal.Type = actorref.Business }, "missingSession": func(r *Request) { r.Access.SessionDigest = [32]byte{} }, "oldDeadline": func(r *Request) { r.DeadlineAt = time.Now().Add(-time.Second) }, "tooLong": func(r *Request) { r.DeadlineAt = time.Now().Add(time.Hour) }, "uppercase": func(r *Request) { r.MembershipID = "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA" }, "badAgent": func(r *Request) { r.Agent.AgentID = "agent" }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := shapeRequest()
			mutate(&r)
			if validateRequest(r, time.Now().UTC()) == nil {
				t.Fatal("untrusted shape accepted")
			}
		})
	}
}
func TestOfflineContractLeaseAndShape(t *testing.T) {
	for _, name := range []string{"lease", "session", "source", "pending", "invited", "left", "removed", "label", "future", "orgNamespace"} {
		t.Run(name, func(t *testing.T) {
			r := shapeRequest()
			s := shapeState(r)
			ok := false
			switch name {
			case "lease":
				ok = true
			case "session":
				s.sessionLimit = s.now.Add(time.Second)
				ok = true
			case "source":
				limit := s.now.Add(2 * time.Second)
				s.sourceExpires = &limit
				ok = true
			case "label":
				s.fact.Label = "\x00"
			case "future":
				s.fact.NativeTime = s.now.Add(time.Hour)
			case "orgNamespace":
				r.Kind = Organization
				s = shapeState(r)
				s.fact.PrincipalAccountID = s.fact.ResourceID
			default:
				s.fact.Status = name
			}
			out, e := assembleContext(r, s)
			if ok != (e == nil) {
				t.Fatal(e)
			}
			if ok && (out.ExpiresAt.Sub(out.ObservedAt) > MaxLease || !out.ExpiresAt.After(out.ObservedAt)) {
				t.Fatal("unbounded lease")
			}
		})
	}
}
func TestContractVersionsAndWire(t *testing.T) {
	r := shapeRequest()
	s := shapeState(r)
	out, e := assembleContext(r, s)
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []Kind{Community, Organization} {
		a, _ := membershipVersion(kind, s.fact.NativeTime, []byte(`{"native":"a"}`))
		b, _ := membershipVersion(kind, s.fact.NativeTime, []byte(`{"native":"b"}`))
		if a == b || a.Token == "" {
			t.Fatal("source mutation invisible")
		}
	}
	if _, e = json.Marshal(r); !errors.Is(e, ErrServerOnly) {
		t.Fatal("request wire")
	}
	var decoded Snapshot
	if e = json.Unmarshal([]byte(`{"confirmed":true}`), &decoded); !errors.Is(e, ErrServerOnly) {
		t.Fatal("snapshot restored")
	}
	wire, e := json.Marshal(out)
	if e != nil || len(wire) == 0 {
		t.Fatal(e)
	}
	c := cloneSnapshot(out)
	c.Evidence[0].Label = "mutation"
	if out.Evidence[0].Label == c.Evidence[0].Label {
		t.Fatal("alias")
	}
}
func TestServiceUnavailableWithoutRealAuthority(t *testing.T) {
	controller, _ := agentfeature.NewController(agentfeature.DefaultConfig())
	if _, e := NewService(nil, controller, false); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	var service *Service
	out, e := service.ReadOwnMembershipContext(context.Background(), shapeRequest())
	if !errors.Is(e, ErrUnavailable) || out.Evidence != nil {
		t.Fatal(e)
	}
	_, e = service.ReadOwnMembershipContext(nil, shapeRequest())
	if !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	_, e = service.ReadForCognition(context.Background(), agentcognitive.ReadRequest{})
	if !errors.Is(e, agentcognitive.ErrUnavailable) {
		t.Fatal("cognition opened")
	}
}

func TestOfflineClockBoundaryStrictNoTolerance(t *testing.T) {
	now := time.Now().UTC()
	for _, name := range []string{"current", "future", "expiredEqual", "past", "invalidClock", "invalidObservation", "overlong"} {
		t.Run(name, func(t *testing.T) {
			s := Snapshot{ObservedAt: now, ExpiresAt: now.Add(MaxLease)}
			checked := now
			want := false
			switch name {
			case "current":
				want = true
			case "future":
				s.ObservedAt = now.Add(time.Nanosecond)
			case "expiredEqual":
				s.ExpiresAt = now
			case "past":
				checked = s.ExpiresAt.Add(time.Nanosecond)
			case "invalidClock":
				checked = time.Time{}
			case "invalidObservation":
				s.ObservedAt = time.Time{}
			case "overlong":
				s.ExpiresAt = s.ExpiresAt.Add(time.Nanosecond)
			}
			if (snapshotTimeAt(s, checked) == nil) != want {
				t.Fatal("strict DB-time shape boundary")
			}
		})
	}
}
