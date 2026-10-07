package agentcontextbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func builderPureRequest() Request {
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: "33000000-0000-4000-8000-000000000001"}
	at := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	return Request{Access: agentprofile.PrivateAccess{SessionDigest: [32]byte{1}, WorkspacePrincipal: owner}, Agent: agentcognitive.AgentReference{AgentID: "33000000-0000-4000-8000-000000000002", Principal: owner, Role: agentruntime.PersonalAgent}, TaskID: "33000000-0000-4000-8000-000000000003", TaskUpdatedAt: at, RequestID: "pure-contract-synthetic", CityID: "synthetic-city", CurrentQuery: "今晚附近活动", Selection: ActivitySearch, Mode: RulesPublicQuery, DeadlineAt: at.Add(time.Minute)}
}

func TestContextBudgetBuilderConfidenceCloneAndCompleteSeal(t *testing.T) {
	a := agentconfidence.NewDirectDeclaration() // Pure shape/seal fixture, not native authority.
	b := BuiltContext{Bundle: Bundle{Memories: []ReviewMemory{{ID: "synthetic", Confidence: &a}}}}
	svc, _ := NewService(&builderPureStore{})
	before := svc.signature(b)
	copy := cloneBuilt(b)
	*copy.Bundle.Memories[0].Confidence.Value = 0
	if *b.Bundle.Memories[0].Confidence.Value != 1 || reflect.DeepEqual(before, svc.signature(copy)) {
		t.Fatal("confidence not deep-copied/sealed")
	}
	copy = cloneBuilt(b)
	copy.Bundle.Memories[0].Confidence = nil
	if reflect.DeepEqual(before, svc.signature(copy)) {
		t.Fatal("removed confidence escaped seal")
	}
	if !validMemoryConfidence(nil) || !validMemoryConfidence(&a) {
		t.Fatal("provided/missing confidence")
	}
	for _, value := range []float64{0, 0.5, math.NaN(), math.Inf(1)} {
		bad := agentconfidence.Assessment{Semantics: agentconfidence.DirectDeclaration, Value: &value}
		if validMemoryConfidence(&bad) {
			t.Fatal("bad confidence accepted", value)
		}
	}
	bad, _ := agentconfidence.NewUncalibratedScore(0.4)
	if validMemoryConfidence(&bad) {
		t.Fatal("inferred confidence became explicit")
	}
}

// Shape/port defenses only. These synthetic values are not native grant proof.
func machinePureRequest() Request {
	r := builderPureRequest()
	r.Mode = MachineTaskContext
	r.Selection = ExactTaskContext
	r.PurposeGrantID = "33000000-0000-4000-8000-000000000008"
	r.PurposeDeadlineAt = r.DeadlineAt
	r.ProfileFields = []string{"availability"}
	return r
}
func TestContextBuilderMachineRequiresClosedTaskPurposeShape(t *testing.T) {
	r := machinePureRequest()
	if ValidateAt(r, r.TaskUpdatedAt) != nil {
		t.Fatal("bounded local shape rejected")
	}
	for name, change := range map[string]func(*Request){
		"missingGrant": func(r *Request) { r.PurposeGrantID = "" }, "noPurposeDeadline": func(r *Request) { r.PurposeDeadlineAt = time.Time{} },
		"readExtendsGrant": func(r *Request) { r.DeadlineAt = r.PurposeDeadlineAt.Add(time.Second) }, "badTask": func(r *Request) { r.TaskID = "not-native" },
		"implicitAllRelationships": func(r *Request) { r.Relationships = true }, "duplicateTie": func(r *Request) { r.RelationshipTieIDs = []string{r.TaskID, r.TaskID} },
		"allPrivateFields": func(r *Request) { r.ProfileFields = PrivateFieldKeys() }, "modelMode": func(r *Request) { r.Mode = "MODEL_CONTEXT_EGRESS" },
		"humanInheritance": func(r *Request) { r.Mode = HumanSelfReview; r.Selection = ExactSelfReview }, "publicInheritance": func(r *Request) { r.Mode = RulesPublicQuery; r.Selection = ActivitySearch },
		"wrongRole": func(r *Request) { r.Agent.Role = agentruntime.OrganizationAgent }, "unboundedPlaces": func(r *Request) {
			for i := 0; i < 6; i++ {
				r.PlaceIDs = append(r.PlaceIDs, fmt.Sprintf("33000000-0000-4000-8000-%012d", i+30))
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneRequest(r)
			change(&candidate)
			if ValidateAt(candidate, r.TaskUpdatedAt) == nil {
				t.Fatal("accepted invalid machine context")
			}
		})
	}
	copy := cloneRequest(r)
	copy.ProfileFields[0] = "agentNotes"
	if r.ProfileFields[0] != "availability" {
		t.Fatal("mutable selection alias")
	}
}
func TestContextBuilderMachinePortRejectsMissingSourcesAndBroaderPayload(t *testing.T) {
	r := machinePureRequest()
	b := BuiltContext{Authority: strings.Repeat("a", 64), Bundle: Bundle{SchemaVersion: SchemaVersion, Agent: r.Agent, Mode: r.Mode, RequestID: r.RequestID, TaskID: r.TaskID, CityID: r.CityID, CurrentQuery: r.CurrentQuery,
		ObservedAt: r.TaskUpdatedAt, ExpiresAt: r.DeadlineAt, ModelAccess: "UNAVAILABLE", Profile: map[string]json.RawMessage{"availability": json.RawMessage(`"周末上午"`)},
		City: &ContextCity{ID: r.CityID, TimeZone: "Europe/London"}, Task: &ContextTask{ID: r.TaskID, Query: r.CurrentQuery, UpdatedAt: r.TaskUpdatedAt},
		Sections: Sections{Profile: "AVAILABLE", Memories: "NOT_REQUESTED", Policies: "NOT_REQUESTED", Places: "NOT_REQUESTED", Activities: "NOT_REQUESTED", Relationships: "NOT_REQUESTED"},
		Sources: []Source{{Kind: "PUBLIC_CITY", ID: r.CityID, NativeTime: r.TaskUpdatedAt, Version: agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: strings.Repeat("b", 64)}},
			{Kind: "CURRENT_TASK_REQUEST", ID: r.TaskID, NativeTime: r.TaskUpdatedAt, Version: agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: strings.Repeat("c", 64)}},
			{Kind: "PURPOSE_PRIVATE_PROFILE", ID: r.Agent.AgentID, NativeTime: r.TaskUpdatedAt, RowToken: "native-row-fixture", Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 2}}}}}
	if e := ValidateBuilt(r, b); e != nil {
		t.Fatal("valid closed synthetic port result", e)
	}
	for name, change := range map[string]func(*BuiltContext){
		"humanSource": func(b *BuiltContext) { b.Bundle.Sources[2].Kind = "HUMAN_PRIVATE_PROFILE" }, "missingSource": func(b *BuiltContext) { b.Bundle.Sources = b.Bundle.Sources[:2] },
		"extraPrivateField": func(b *BuiltContext) { b.Bundle.Profile["agentNotes"] = json.RawMessage(`"unrequested"`) }, "modelGranted": func(b *BuiltContext) { b.Bundle.ModelAccess = "ALLOWED" },
		"memoryPromotion": func(b *BuiltContext) { b.Bundle.MemoryPromotionAllowed = true }, "changedTask": func(b *BuiltContext) { b.Bundle.Task.Query = "不同请求" },
		"cityMissing": func(b *BuiltContext) { b.Bundle.City = nil }, "defaultPolicy": func(b *BuiltContext) {
			b.Bundle.Policies = []agentpolicysettings.Record{agentpolicysettings.DefaultRecord(agentpolicysettings.Autonomy)}
		},
		"peerMetadata": func(b *BuiltContext) {
			b.Bundle.Relationships = []ContextTie{{ID: r.TaskID, PeerAccountID: r.Agent.Principal.ID, State: "ACCEPTED"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneBuilt(b)
			change(&candidate)
			if ValidateBuilt(r, candidate) == nil {
				t.Fatal("unsafe machine payload accepted")
			}
		})
	}
}
func TestContextBuilderClosedRequestBoundaries(t *testing.T) {
	good := builderPureRequest()
	if ValidateAt(good, good.TaskUpdatedAt) != nil {
		t.Fatal("valid explicit public request")
	}
	cases := map[string]func(*Request){
		"unknownMode": func(r *Request) { r.Mode = "MODEL_CONTEXT_EGRESS" }, "privateMemoryInRuntime": func(r *Request) { r.MemoryIDs = []string{r.TaskID} }, "privateProfileInRuntime": func(r *Request) { r.ProfileFields = []string{"agentNotes"} }, "privatePolicyInRuntime": func(r *Request) { r.PolicyFamilies = []agentpolicysettings.Family{agentpolicysettings.Social} },
		"relationship": func(r *Request) { r.Relationships = true }, "peerOwner": func(r *Request) { r.Access.WorkspacePrincipal.ID = r.TaskID }, "wrongRole": func(r *Request) { r.Agent.Role = agentruntime.OrganizationAgent }, "unknownSelection": func(r *Request) { r.Selection = "ALL_HISTORY" }, "missingAgent": func(r *Request) { r.Agent.AgentID = "" }, "missingSession": func(r *Request) { r.Access.SessionDigest = [32]byte{} }, "foreignType": func(r *Request) { r.Agent.Principal.Type = actorref.Organization }, "unknownIDs": func(r *Request) { r.ActivityIDs = []string{"123"} }, "duplicateIDs": func(r *Request) { r.ActivityIDs = []string{r.TaskID, r.TaskID} }, "tooManyIDs": func(r *Request) {
			for i := 0; i < 31; i++ {
				r.ActivityIDs = append(r.ActivityIDs, fmt.Sprintf("33000000-0000-4000-8000-%012d", i+10))
			}
		}, "mixedSources": func(r *Request) { r.PlaceIDs = []string{r.TaskID} }, "hugeQuery": func(r *Request) { r.CurrentQuery = strings.Repeat("x", 241) }, "badUTF8": func(r *Request) { r.CurrentQuery = string([]byte{0xff}) }, "newline": func(r *Request) { r.CurrentQuery = "查询\n" }, "blankRequest": func(r *Request) { r.RequestID = "" }, "missingTaskClock": func(r *Request) { r.TaskUpdatedAt = time.Time{} }, "expired": func(r *Request) { r.DeadlineAt = r.TaskUpdatedAt }, "tooLongDeadline": func(r *Request) { r.DeadlineAt = r.TaskUpdatedAt.Add(MaxDeadline + time.Second) },
	}
	// A concrete family selector is private even when its enum is legitimate.
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := cloneRequest(good)
			mutate(&r)
			if e := ValidateAt(r, good.TaskUpdatedAt); e == nil {
				t.Fatal("accepted invalid request")
			}
		})
	}
}
func TestContextBuilderHumanReviewBoundsAndNoRuntimeInheritance(t *testing.T) {
	r := builderPureRequest()
	r.Mode = HumanSelfReview
	r.Selection = ExactSelfReview
	r.TaskID = ""
	r.TaskUpdatedAt = time.Time{}
	r.CityID = ""
	r.CurrentQuery = ""
	r.ProfileFields = []string{"availability"}
	if ValidateAt(r, builderPureRequest().TaskUpdatedAt) != nil {
		t.Fatal("selected human review")
	}
	for name, mutate := range map[string]func(*Request){"unknownKey": func(r *Request) { r.ProfileFields = []string{"password"} }, "caseKey": func(r *Request) { r.ProfileFields = []string{"Availability"} }, "duplicate": func(r *Request) { r.ProfileFields = []string{"availability", "availability"} }, "allProfile": func(r *Request) { r.ProfileFields = PrivateFieldKeys() }, "allMemory": func(r *Request) {
		r.MemoryIDs = []string{builderPureRequest().TaskID, builderPureRequest().Agent.AgentID, builderPureRequest().Agent.Principal.ID, "33000000-0000-4000-8000-000000000004"}
	}, "privateRuntimeReuse": func(r *Request) { r.Mode = RulesPublicQuery }, "relationship": func(r *Request) { r.Relationships = true }, "publicMixed": func(r *Request) { r.ActivityIDs = []string{builderPureRequest().TaskID} }} {
		t.Run(name, func(t *testing.T) {
			v := cloneRequest(r)
			mutate(&v)
			if ValidateShape(v) == nil {
				t.Fatal("accepted invalid human selector")
			}
		})
	}
}

type builderPureStore struct {
	now     time.Time
	calls   int
	malform func(*BuiltContext)
}

func (s *builderPureStore) ResolveOwnContextAgent(context.Context, agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	return builderPureRequest().Agent, nil
}
func (s *builderPureStore) BuildOwnAgentContext(_ context.Context, r Request) (BuiltContext, error) {
	s.calls++
	if e := ValidateAt(r, s.now); e != nil {
		return BuiltContext{}, e
	}
	out := BuiltContext{Authority: strings.Repeat("a", 64), Bundle: Bundle{SchemaVersion: SchemaVersion, Agent: r.Agent, Mode: r.Mode, RequestID: r.RequestID, TaskID: r.TaskID, CityID: r.CityID, CurrentQuery: r.CurrentQuery, ObservedAt: s.now, ExpiresAt: r.DeadlineAt, ModelAccess: "UNAVAILABLE", Sections: Sections{Profile: "NOT_REQUESTED", Memories: "NOT_REQUESTED", Policies: "NOT_REQUESTED", Relationships: "UNAVAILABLE", Activities: "AVAILABLE", Places: "NOT_REQUESTED"}}}
	for _, f := range []struct{ kind, id string }{{"PUBLIC_CITY", r.CityID}, {"CURRENT_TASK_REQUEST", r.TaskID}} {
		v, _ := PublicVersion(f.kind, builderPureRequest().TaskUpdatedAt, []byte(`{"synthetic":true}`))
		out.Bundle.Sources = append(out.Bundle.Sources, Source{Kind: f.kind, ID: f.id, Version: v, NativeTime: builderPureRequest().TaskUpdatedAt})
	}
	if s.malform != nil {
		s.malform(&out)
	}
	return out, nil
}
func TestContextBuilderPureServiceFailClosedAndEphemeralSeal(t *testing.T) {
	r := builderPureRequest()
	port := &builderPureStore{now: r.TaskUpdatedAt}
	svc, e := NewService(port)
	if e != nil {
		t.Fatal(e)
	}
	got, e := svc.Build(context.Background(), r)
	if e != nil {
		t.Fatal("pure synthetic contract", e)
	}
	port.now = port.now.Add(time.Second)
	again, e := svc.RevalidateOwn(context.Background(), r.Access, got)
	if e != nil || !again.Bundle.ExpiresAt.Equal(got.Bundle.ExpiresAt) {
		t.Fatal("revalidation renewed lease", e)
	}
	t.Run("privateZeroPortReads", func(t *testing.T) {
		bad := cloneRequest(r)
		bad.MemoryIDs = []string{r.TaskID}
		before := port.calls
		out, e := svc.Build(context.Background(), bad)
		if !errors.Is(e, ErrUnavailable) || !reflect.DeepEqual(out, BuiltContext{}) || port.calls != before {
			t.Fatal("private runtime request read a port")
		}
	})
	t.Run("tamperedSource", func(t *testing.T) {
		bad := cloneBuilt(got)
		bad.Bundle.Sources[0].Version.Token = strings.Repeat("b", 64)
		if out, e := svc.RevalidateOwn(context.Background(), r.Access, bad); e == nil || !reflect.DeepEqual(out, BuiltContext{}) {
			t.Fatal("tamper accepted")
		}
	})
	t.Run("otherService", func(t *testing.T) {
		other, _ := NewService(port)
		if out, e := other.RevalidateOwn(context.Background(), r.Access, got); e == nil || !reflect.DeepEqual(out, BuiltContext{}) {
			t.Fatal("seal crossed a service")
		}
	})
	t.Run("databaseClockRollback", func(t *testing.T) {
		port.now = r.TaskUpdatedAt.Add(-time.Second)
		if out, e := svc.RevalidateOwn(context.Background(), r.Access, got); e == nil || !reflect.DeepEqual(out, BuiltContext{}) {
			t.Fatal("future observation accepted")
		}
	})
	t.Run("expiredBySourceClock", func(t *testing.T) {
		port.now = r.DeadlineAt
		if out, e := svc.RevalidateOwn(context.Background(), r.Access, got); e == nil || !reflect.DeepEqual(out, BuiltContext{}) {
			t.Fatal("expired source accepted")
		}
	})
	for _, name := range []string{"request", "built"} {
		t.Run("controlJSON"+name, func(t *testing.T) {
			var value any = r
			if name == "built" {
				value = got
			}
			if _, e := json.Marshal(value); !errors.Is(e, ErrServerOnly) {
				t.Fatal("serialized control")
			}
		})
	}
}
func TestContextBuilderPurePortOutputIsNotAuthority(t *testing.T) {
	for name, mutate := range map[string]func(*BuiltContext){"wrongOwner": func(b *BuiltContext) { b.Bundle.Agent.Principal.ID = builderPureRequest().TaskID }, "modelEnabled": func(b *BuiltContext) { b.Bundle.ModelAccess = "ALLOW" }, "memoryPromotion": func(b *BuiltContext) { b.Bundle.MemoryPromotionAllowed = true }, "unknownSource": func(b *BuiltContext) { b.Bundle.Sources[0].Kind = "PRIVATE_PROFILE" }, "futureSource": func(b *BuiltContext) { b.Bundle.Sources[0].NativeTime = b.Bundle.ObservedAt.Add(time.Hour) }, "missingSource": func(b *BuiltContext) { b.Bundle.Sources = nil }, "fakeRevision": func(b *BuiltContext) { b.Bundle.Sources[0].Version.Revision = 1 }, "invalidToken": func(b *BuiltContext) { b.Bundle.Sources[0].Version.Token = strings.Repeat("z", 64) }, "privateContent": func(b *BuiltContext) {
		b.Bundle.Profile = map[string]json.RawMessage{"agentNotes": json.RawMessage(`"private"`)}
	}, "longLease": func(b *BuiltContext) { b.Bundle.ExpiresAt = b.Bundle.ObservedAt.Add(MaxLease + time.Second) }} {
		t.Run(name, func(t *testing.T) {
			r := builderPureRequest()
			port := &builderPureStore{now: r.TaskUpdatedAt, malform: mutate}
			svc, _ := NewService(port)
			out, e := svc.Build(context.Background(), r)
			if e == nil || !reflect.DeepEqual(out, BuiltContext{}) {
				t.Fatal("malformed result released")
			}
		})
	}
}
func TestContextBuilderPublicSourceVersionsAreOpaqueAndClosed(t *testing.T) {
	at := builderPureRequest().TaskUpdatedAt
	a, e := PublicVersion("PUBLIC_PLACE", at, []byte(`{"native":"fixture"}`))
	if e != nil || a.Kind != "UPDATED_AT_DIGEST" || a.Revision != 0 {
		t.Fatal(e)
	}
	zone, _ := time.LoadLocation("Asia/Shanghai")
	b, _ := PublicVersion("PUBLIC_PLACE", at.In(zone), []byte(`{"native":"fixture"}`))
	if a != b {
		t.Fatal("same instant changed")
	}
	c, _ := PublicVersion("PUBLIC_PLACE", at, []byte(`{"native":"changed"}`))
	if a == c {
		t.Fatal("same timestamp changed source ignored")
	}
	for _, kind := range []string{"MomentCreated", "PROFILE", "MODEL", ""} {
		if _, e := PublicVersion(kind, at, []byte(`{}`)); e == nil {
			t.Fatal("unknown context source")
		}
	}
}

func TestContextBuilderPureHumanSourceMappingIsExact(t *testing.T) {
	r := builderPureRequest()
	id := r.TaskID
	r.Mode = HumanSelfReview
	r.Selection = ExactSelfReview
	r.TaskID = ""
	r.TaskUpdatedAt = time.Time{}
	r.CityID = ""
	r.CurrentQuery = ""
	r.ProfileFields = []string{"availability"}
	r.MemoryIDs = []string{id}
	r.PolicyFamilies = []agentpolicysettings.Family{agentpolicysettings.Autonomy}
	b := BuiltContext{Authority: strings.Repeat("a", 64), Bundle: Bundle{SchemaVersion: SchemaVersion, Agent: r.Agent, Mode: r.Mode, RequestID: r.RequestID, ObservedAt: builderPureRequest().TaskUpdatedAt, ExpiresAt: r.DeadlineAt, ModelAccess: "UNAVAILABLE", Sections: Sections{Profile: "AVAILABLE", Memories: "AVAILABLE", Policies: "UNCONFIGURED", Activities: "NOT_REQUESTED", Places: "NOT_REQUESTED", Relationships: "UNAVAILABLE"}, Profile: map[string]json.RawMessage{"availability": json.RawMessage(`"纯合同合成自述"`)}, Memories: []ReviewMemory{{ID: id, Version: 5, StructuredValue: json.RawMessage(`{"synthetic":true}`), ValidUntil: r.DeadlineAt}}, Policies: []agentpolicysettings.Record{agentpolicysettings.DefaultRecord(agentpolicysettings.Autonomy)}}}
	b.Bundle.Sources = []Source{{Kind: "HUMAN_PRIVATE_PROFILE", ID: r.Agent.AgentID, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 8}, NativeTime: b.Bundle.ObservedAt, RowToken: "100"}, {Kind: "HUMAN_EXPLICIT_MEMORY", ID: id, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 5}, NativeTime: b.Bundle.ObservedAt, RowToken: "101"}}
	if ValidateBuilt(r, b) != nil {
		t.Fatal("valid pure human output shape; no native authority claimed")
	}
	for name, mutate := range map[string]func(*BuiltContext){
		"missingMemorySource":         func(o *BuiltContext) { o.Bundle.Sources = o.Bundle.Sources[:1] },
		"missingProfileSource":        func(o *BuiltContext) { o.Bundle.Sources = o.Bundle.Sources[1:] },
		"wrongMemoryVersion":          func(o *BuiltContext) { o.Bundle.Memories[0].Version++ },
		"wrongMemoryID":               func(o *BuiltContext) { o.Bundle.Memories[0].ID = r.Agent.AgentID },
		"profileUnexpectedField":      func(o *BuiltContext) { o.Bundle.Profile["agentNotes"] = json.RawMessage(`"私密"`) },
		"relationshipAvailable":       func(o *BuiltContext) { o.Bundle.Sections.Relationships = "AVAILABLE" },
		"humanActivities":             func(o *BuiltContext) { o.Bundle.Sections.Activities = "AVAILABLE" },
		"missingProfileContent":       func(o *BuiltContext) { o.Bundle.Profile = nil },
		"unconfiguredPolicyAvailable": func(o *BuiltContext) { o.Bundle.Sections.Policies = "AVAILABLE" },
		"invalidAuthorityHex":         func(o *BuiltContext) { o.Authority = strings.Repeat("z", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			o := cloneBuilt(b)
			mutate(&o)
			if ValidateBuilt(r, o) == nil {
				t.Fatal("malformed human source map accepted")
			}
		})
	}
}

func TestContextBuilderPureNonJSONDomainResultRejected(t *testing.T) {
	r := builderPureRequest()
	r.ActivityIDs = []string{r.TaskID}
	p := &builderPureStore{now: r.TaskUpdatedAt, malform: func(b *BuiltContext) {
		v, _ := PublicVersion("PUBLIC_ACTIVITY", r.TaskUpdatedAt, []byte(`{"offline":true}`))
		b.Bundle.Sources = append(b.Bundle.Sources, Source{Kind: "PUBLIC_ACTIVITY", ID: r.TaskID, Version: v, NativeTime: r.TaskUpdatedAt})
		b.Bundle.Activities = []PublicActivity{{ID: r.TaskID, Title: "Synthetic"}}
		lat := math.NaN()
		b.Activities = []foundation.Activity{{ID: r.TaskID, CityID: r.CityID, Title: "Synthetic", Visibility: "public", Location: &foundation.Location{Precision: "point", CoordinateSystem: "wgs84", Latitude: &lat}}}
	}}
	s, _ := NewService(p)
	out, e := s.Build(context.Background(), r)
	if e == nil || !reflect.DeepEqual(out, BuiltContext{}) {
		t.Fatal("non-JSON port result produced nil error or broken clone")
	}
}
