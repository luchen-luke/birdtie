package agentcognitive

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

func cognitiveFixture(kind actorref.Type) (time.Time, ReadRequest, BoundaryFacts) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	principal := actorref.PrincipalRef{Type: kind, ID: "11111111-1111-4111-8111-111111111111"}
	agent := AgentReference{AgentID: "33333333-3333-4333-8333-333333333333", Principal: principal, Role: agentruntime.ForType(kind).Role}
	scope := agentruntime.Private
	actor := principal
	if kind != actorref.Person {
		scope, actor = agentruntime.WorkspacePrivate, actorref.PrincipalRef{Type: actorref.Person, ID: "22222222-2222-4222-8222-222222222222"}
	}
	source := SourceReference{Type: Memory, ID: "77777777-7777-4777-8777-777777777777", Owner: principal, Version: 1}
	req := ReadRequest{Version: ContractVersion, RequestID: "66666666-6666-4666-8666-666666666666",
		TaskID: "55555555-5555-4555-8555-555555555555", Agent: agent, Purpose: ReadMemory, Scope: scope,
		Source: source, ExpiresAt: now.Add(10 * time.Minute)}
	facts := BoundaryFacts{SessionVerified: true, SessionActive: true, ActingUser: actor, AgentVerified: true,
		AgentActive: true, PrincipalActive: true, ResolvedAgent: agent, MembershipVerified: true, MembershipActive: true,
		MembershipID: "88888888-8888-4888-8888-888888888888", MembershipActor: actor, MembershipPrincipal: principal,
		MembershipRevision: 1, CurrentMembershipRevision: 1,
		Source: SourceState{Verified: true, Authorized: true, Reference: source, CurrentVersion: 1}}
	return now, req, facts
}

func TestCognitiveNativeIdentityAndRolePolicy(t *testing.T) {
	for _, kind := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business} {
		t.Run(string(kind), func(t *testing.T) {
			_, req, _ := cognitiveFixture(kind)
			if err := ValidateAgentReference(req.Agent); err != nil {
				t.Fatal(err)
			}
			policy, eligibility := DescribeRolePolicy(req.Agent)
			expected := agentruntime.ForType(kind)
			if policy.Role != expected.Role || !reflect.DeepEqual(policy.PermissionStrings(), expected.PermissionStrings()) {
				t.Fatalf("native runtime duplicated or broadened: %+v", policy)
			}
			if kind == actorref.Business {
				if eligibility.Status != Unavailable || policy.Available {
					t.Fatal("reserved Business runtime opened")
				}
			} else if eligibility.Status != Available || !policy.Available {
				t.Fatal("native role description unavailable")
			}
			for _, permission := range []agentruntime.Capability{"memory.write", "provider.send", "inference.share", "coordination.send", "actions.execute"} {
				if policy.Allows(permission) {
					t.Fatalf("future permission opened: %s", permission)
				}
			}
		})
	}
	for _, kind := range []actorref.Type{actorref.Community, "CITY", "PLACE", "PERSONA", "UNKNOWN", "person"} {
		t.Run("no Agent "+string(kind), func(t *testing.T) {
			_, req, _ := cognitiveFixture(actorref.Person)
			req.Agent.Principal.Type = kind
			if ValidateAgentReference(req.Agent) == nil {
				t.Fatal("invented Agent principal")
			}
			policy, eligibility := DescribeRolePolicy(req.Agent)
			if policy.Available || eligibility.Status != Denied {
				t.Fatal("invented principal exposed runtime")
			}
		})
	}
	_, req, _ := cognitiveFixture(actorref.Person)
	req.Agent.Role = agentruntime.OrganizationAgent
	if ValidateAgentReference(req.Agent) == nil {
		t.Fatal("PERSON role reused as Organization")
	}
	for _, invalid := range []string{"", "agent", "00000000-0000-0000-0000-000000000000", " " + req.Agent.AgentID} {
		req.Agent.AgentID = invalid
		if ValidateAgentReference(req.Agent) == nil {
			t.Fatal("invalid stable agent reference")
		}
	}
}

func TestCognitiveModelDoesNotOwnIdentity(t *testing.T) {
	_, req, _ := cognitiveFixture(actorref.Person)
	before := req.Agent
	for _, model := range []string{"provider-a/model-one", "provider-b/model-two", "offline"} {
		// Runtime selection is deliberately outside the stable typed reference.
		configuration := struct {
			Model string
			Agent AgentReference
		}{model, before}
		if !sameAgent(before, configuration.Agent) {
			t.Fatal("model switch replaced Agent identity")
		}
	}
	typ := reflect.TypeOf(AgentReference{})
	if typ.NumField() != 3 {
		t.Fatal("identity contract has unexpected authority/provider fields")
	}
	for i := 0; i < typ.NumField(); i++ {
		if strings.Contains(strings.ToLower(typ.Field(i).Name), "model") || strings.Contains(strings.ToLower(typ.Field(i).Name), "provider") {
			t.Fatal("model became identity")
		}
	}
	// Same textual UUID in a different typed namespace never identifies the
	// same principal. Organization public actor ID is not its account principal.
	org := before
	org.Principal.Type, org.Role = actorref.Organization, agentruntime.OrganizationAgent
	if sameAgent(before, org) {
		t.Fatal("typed namespace collision")
	}
}

func TestCognitiveDefaultPortsNeverPretendToRun(t *testing.T) {
	ports := UnavailableCognitivePorts{}
	for _, kind := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business} {
		t.Run(string(kind), func(t *testing.T) {
			now, req, facts := cognitiveFixture(kind)
			result := DecideEligibility(now, req, facts)
			if result.Status != Unavailable {
				t.Fatalf("future cognition exposed: %+v", result)
			}
			view, err := ports.ReadMemory(context.Background(), req)
			if !errors.Is(err, ErrUnavailable) || !reflect.ValueOf(view).IsZero() {
				t.Fatal("fabricated Memory view")
			}
			receipt, err := ports.SubmitMemoryCandidate(context.Background(), CandidateSubmission{Request: req, Sources: []SourceReference{req.Source}, PayloadDigest: "model says confirmed"})
			if !errors.Is(err, ErrUnavailable) || receipt.Status != Unavailable {
				t.Fatal("candidate accepted without service")
			}
		})
	}
	for _, purpose := range []Purpose{ReadAgentProfile, SubmitMemoryCandidate, ModelContextEgress, AgentCoordination, ExecuteAction} {
		t.Run(string(purpose), func(t *testing.T) {
			now, req, facts := cognitiveFixture(actorref.Person)
			req.Purpose = purpose
			switch purpose {
			case ReadAgentProfile:
				req.Source.Type = AgentProfile
			case SubmitMemoryCandidate:
				req.Source.Type = MemoryEvidence
			case AgentCoordination, ExecuteAction:
				req.Source.Type = AgentPolicy
			}
			facts.Source.Reference = req.Source
			if decision := DecideEligibility(now, req, facts); decision.Status != Unavailable {
				t.Fatalf("future port opened: %+v", decision)
			}
		})
	}
}

func TestCognitiveBoundaryAuthorityAndInvalidationMatrix(t *testing.T) {
	type mutation func(*ReadRequest, *BoundaryFacts, time.Time)
	other := "22222222-2222-4222-8222-222222222222"
	cases := []struct {
		name string
		edit mutation
	}{
		{"unknown version", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Version = "v2" }},
		{"no request", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.RequestID = "" }},
		{"bad task", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.TaskID = "task" }},
		{"unknown purpose", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Purpose = "SHARE_PROFILE_048" }},
		{"source purpose mismatch", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Purpose = ReadAgentProfile }},
		{"expires now", func(r *ReadRequest, _ *BoundaryFacts, n time.Time) { r.ExpiresAt = n }},
		{"over TTL", func(r *ReadRequest, _ *BoundaryFacts, n time.Time) {
			r.ExpiresAt = n.Add(MaxRequestTTL + time.Nanosecond)
		}},
		{"source zero version", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Source.Version = 0 }},
		{"source invalid ID", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Source.ID = "url://raw" }},
		{"source unknown kind", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Source.Type = "CHAT_BODY" }},
		{"source other owner", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Source.Owner.ID = other }},
		{"source organization namespace", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Source.Owner.Type = actorref.Organization }},
		{"unverified session", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.SessionVerified = false }},
		{"inactive session", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.SessionActive = false }},
		{"anonymous actor", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.ActingUser = actorref.PrincipalRef{} }},
		{"other actor", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.ActingUser.ID = other }},
		{"org actor is not a human session", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.ActingUser.Type = actorref.Organization }},
		{"agent proof unavailable", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.AgentVerified = false }},
		{"inactive Agent", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.AgentActive = false }},
		{"inactive principal", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.PrincipalActive = false }},
		{"other resolved Agent", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.ResolvedAgent.AgentID = other }},
		{"other resolved owner", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.ResolvedAgent.Principal.ID = other }},
		{"cross namespace Agent", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) {
			f.ResolvedAgent.Principal.Type, f.ResolvedAgent.Role = actorref.Organization, agentruntime.OrganizationAgent
		}},
		{"public Memory", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Scope = agentruntime.Public }},
		{"connection Memory", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Scope = agentruntime.Connection }},
		{"organization workspace from person", func(r *ReadRequest, _ *BoundaryFacts, _ time.Time) { r.Scope = agentruntime.WorkspacePrivate }},
		{"source not verified", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.Source.Verified = false }},
		{"source unauthorized", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.Source.Authorized = false }},
		{"source version changed", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.Source.CurrentVersion++ }},
		{"source version zero", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.Source.CurrentVersion = 0 }},
		{"source record changed", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.Source.Reference.ID = other }},
		{"source proof owner changed", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.Source.Reference.Owner.ID = other }},
		{"source proof kind changed", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.Source.Reference.Type = UserProfile }},
		{"source deleted", func(_ *ReadRequest, f *BoundaryFacts, _ time.Time) { f.Source.Deleted = true }},
		{"source revoked", func(_ *ReadRequest, f *BoundaryFacts, n time.Time) { f.Source.RevokedAt = &n }},
		{"source expired", func(_ *ReadRequest, f *BoundaryFacts, n time.Time) { f.Source.ExpiresAt = &n }},
		{"source too short lived", func(_ *ReadRequest, f *BoundaryFacts, n time.Time) {
			expiry := n.Add(time.Minute)
			f.Source.ExpiresAt = &expiry
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, req, facts := cognitiveFixture(actorref.Person)
			tc.edit(&req, &facts, now)
			if result := DecideEligibility(now, req, facts); result.Status != Denied {
				t.Fatalf("invalid trusted input not rejected: %+v", result)
			}
		})
	}
	now, req, facts := cognitiveFixture(actorref.Person)
	if DecideEligibility(now, req, BoundaryFacts{}).Status != Denied {
		t.Fatal("default facts granted authority")
	}
	if DecideEligibility(time.Time{}, req, facts).Status != Denied {
		t.Fatal("missing clock granted authority")
	}
	req.ExpiresAt = now.Add(MaxRequestTTL)
	facts.Source.ExpiresAt = &req.ExpiresAt
	if DecideEligibility(now, req, facts).Status != Unavailable {
		t.Fatal("exact TTL contract boundary invalid")
	}
}

func TestCognitiveOrganizationCannotInheritPersonalData(t *testing.T) {
	cases := []struct {
		name string
		edit func(*ReadRequest, *BoundaryFacts)
	}{
		{"member removed", func(_ *ReadRequest, f *BoundaryFacts) { f.MembershipActive = false }},
		{"membership not verified", func(_ *ReadRequest, f *BoundaryFacts) { f.MembershipVerified = false }},
		{"missing membership record", func(_ *ReadRequest, f *BoundaryFacts) { f.MembershipID = "" }},
		{"membership of another actor", func(_ *ReadRequest, f *BoundaryFacts) { f.MembershipActor.ID = "44444444-4444-4444-8444-444444444444" }},
		{"membership of another Organization", func(_ *ReadRequest, f *BoundaryFacts) {
			f.MembershipPrincipal.ID = "44444444-4444-4444-8444-444444444444"
		}},
		{"membership stale", func(_ *ReadRequest, f *BoundaryFacts) { f.CurrentMembershipRevision++ }},
		{"membership zero version", func(_ *ReadRequest, f *BoundaryFacts) { f.MembershipRevision, f.CurrentMembershipRevision = 0, 0 }},
		{"personal scope", func(r *ReadRequest, _ *BoundaryFacts) { r.Scope = agentruntime.Private }},
		{"person source", func(r *ReadRequest, f *BoundaryFacts) { r.Source.Owner = f.ActingUser; f.Source.Reference = r.Source }},
		{"UserProfile namespace spoof", func(r *ReadRequest, f *BoundaryFacts) {
			r.Purpose, r.Source.Type = ModelContextEgress, UserProfile
			f.Source.Reference = r.Source
		}},
		{"PersonContext namespace spoof", func(r *ReadRequest, f *BoundaryFacts) {
			r.Purpose, r.Source.Type = ModelContextEgress, PersonContext
			f.Source.Reference = r.Source
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, req, facts := cognitiveFixture(actorref.Organization)
			tc.edit(&req, &facts)
			if result := DecideEligibility(now, req, facts); result.Status != Denied {
				t.Fatalf("organization inherited personal authority: %+v", result)
			}
		})
	}
	// A valid organization membership is still no live Memory/AI/A2A grant.
	now, req, facts := cognitiveFixture(actorref.Organization)
	if DecideEligibility(now, req, facts).Status != Unavailable {
		t.Fatal("membership opened cognition")
	}
}

func TestCognitiveAuthorityNotSerializable(t *testing.T) {
	_, req, facts := cognitiveFixture(actorref.Person)
	values := []any{facts, facts.Source, SessionAccess{Digest: [32]byte{1}, Workspace: req.Agent.Principal}}
	for _, value := range values {
		t.Run(reflect.TypeOf(value).Name(), func(t *testing.T) {
			body, err := json.Marshal(value)
			if len(body) != 0 || !errors.Is(err, ErrAuthorityJSON) {
				t.Fatal("authority encoded as JSON")
			}
			pointer := reflect.New(reflect.TypeOf(value))
			pointer.Elem().Set(reflect.ValueOf(value))
			err = json.Unmarshal([]byte(`{"SessionVerified":true,"Authorized":true,"confirmed":true,"secret":"SECRET_CANARY"}`), pointer.Interface())
			if !errors.Is(err, ErrAuthorityJSON) || strings.Contains(err.Error(), "SECRET_CANARY") || !pointer.Elem().IsZero() {
				t.Fatal("wire proof retained or manufactured authority")
			}
		})
	}
}
