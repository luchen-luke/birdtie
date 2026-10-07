package agentruntime

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

// These are synthetic policy evidence, not live coordination grants, delivery
// or user interest. AGA-001 has deliberately not added a transport or resolver.
func coordinationPolicyFixture() (time.Time, CoordinationRequest, CoordinationFacts) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	request := CoordinationRequest{
		Version:   CoordinationVersion,
		RequestID: "66666666-6666-4666-8666-666666666666", TaskID: "55555555-5555-4555-8555-555555555555",
		Sender:    CoordinationAgentRef{AgentID: "33333333-3333-4333-8333-333333333333", Principal: actorref.PrincipalRef{Type: actorref.Person, ID: "11111111-1111-4111-8111-111111111111"}},
		Recipient: CoordinationAgentRef{AgentID: "44444444-4444-4444-8444-444444444444", Principal: actorref.PrincipalRef{Type: actorref.Person, ID: "22222222-2222-4222-8222-222222222222"}},
		Purpose:   CoordinationPurposeAskActivityInterest, Scope: Connection,
		Resource: CoordinationResourceRef{Type: "ACTIVITY", ID: "77777777-7777-4777-8777-777777777777"},
		Fields:   []string{CoordinationFieldNextStep}, ExpiresAt: now.Add(10 * time.Minute),
	}
	grant := CoordinationGrant{
		Verified: true, RequestID: request.RequestID, TaskID: request.TaskID,
		SenderAgentID: request.Sender.AgentID, RecipientAgentID: request.Recipient.AgentID,
		SenderPrincipal: request.Sender.Principal, RecipientPrincipal: request.Recipient.Principal,
		Purpose: request.Purpose, Scope: request.Scope, Resource: request.Resource,
		Fields: []string{CoordinationFieldNextStep}, HumanConfirmed: true,
		ConfirmedAt: now.Add(-time.Minute), ExpiresAt: now.Add(15 * time.Minute), Revision: 1, CurrentRevision: 1,
	}
	facts := CoordinationFacts{
		Session:     CoordinationSessionFacts{Verified: true, Active: true, ActingUserID: request.Sender.Principal.ID},
		Sender:      CoordinationAgentFacts{Verified: true, Active: true, OwnerActive: true, Role: PersonalAgent, AgentID: request.Sender.AgentID, Principal: request.Sender.Principal},
		Recipient:   CoordinationAgentFacts{Verified: true, Active: true, OwnerActive: true, Role: PersonalAgent, AgentID: request.Recipient.AgentID, Principal: request.Recipient.Principal},
		Task:        CoordinationTaskFacts{Verified: true, ID: request.TaskID, OwnerPrincipal: request.Sender.Principal, AgentID: request.Sender.AgentID, Status: "COMPLETED", Intent: "FIND_ACTIVITY", ResourceAuthorized: true},
		Resource:    CoordinationResourceFacts{Verified: true, Reference: request.Resource, Published: true, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), SenderVisible: true, RecipientVisible: true},
		Tie:         CoordinationTieFacts{Verified: true, ID: "88888888-8888-4888-8888-888888888888", RequestID: "99999999-9999-4999-8999-999999999999", RequestSenderID: request.Sender.Principal.ID, RequestRecipientID: request.Recipient.Principal.ID, PersonAID: request.Sender.Principal.ID, PersonBID: request.Recipient.Principal.ID, Active: true, RequestScope: "friend", RequestState: "accepted"},
		SenderGrant: grant, RecipientGrant: grant,
	}
	facts.SenderGrant.ID, facts.SenderGrant.OwnerPrincipal = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", request.Sender.Principal
	facts.RecipientGrant.ID, facts.RecipientGrant.OwnerPrincipal = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", request.Recipient.Principal
	return now, request, facts
}

func coordinationTime(value time.Time) *time.Time { return &value }

func TestCoordinationPolicyValidMinimumResponse(t *testing.T) {
	now, request, facts := coordinationPolicyFixture()
	if decision := DecideCoordination(now, request, facts); !decision.Allowed || decision.Reason != "allowed" {
		t.Fatalf("valid synthetic evidence rejected: %+v", decision)
	}
	response, err := BuildCoordinationResponse(now, request, facts)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"version":"agent-coordination-v1","requestId":"66666666-6666-4666-8666-666666666666","nextStep":"ASK_USER"}`
	if string(raw) != expected {
		t.Fatalf("response exceeded minimal contract: %s", raw)
	}
	for _, secret := range []string{request.TaskID, request.Sender.AgentID, request.Recipient.AgentID, request.Resource.ID, facts.Tie.ID, facts.SenderGrant.ID, "purpose", "scope", "fields", "profile", "interest", "message", "schedule", "location", "revision"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("response disclosed unnecessary evidence %q: %s", secret, raw)
		}
	}
	decoded, err := DecodeCoordinationResponse(raw, request.RequestID)
	if err != nil || decoded != response {
		t.Fatalf("minimal response cannot be consumed: %+v %v", decoded, err)
	}
	// Direct callers also get canonical UUIDs without broadening enum semantics.
	request.RequestID = strings.ToUpper(facts.RecipientGrant.ID)
	facts.SenderGrant.RequestID, facts.RecipientGrant.RequestID = request.RequestID, strings.ToLower(request.RequestID)
	response, err = BuildCoordinationResponse(now, request, facts)
	if err != nil || response.RequestID != strings.ToLower(request.RequestID) {
		t.Fatalf("UUID case canonicalization failed: %+v %v", response, err)
	}
}

func TestCoordinationPolicyRequestAndAuthorityMatrix(t *testing.T) {
	type mutation func(*CoordinationRequest, *CoordinationFacts, time.Time)
	cases := []struct {
		name string
		edit mutation
	}{
		{"unknown version", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Version = "v2" }},
		{"missing request", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.RequestID = "" }},
		{"invalid task reference", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.TaskID = "task" }},
		{"unknown purpose", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Purpose = "READ_MEMORY" }},
		{"profile purpose cannot substitute", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Purpose = "profile_view" }},
		{"public scope is separate purpose", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Scope = Public }},
		{"private scope", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Scope = Private }},
		{"workspace private scope", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Scope = WorkspacePrivate }},
		{"close scope has no live model", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Scope = Close }},
		{"scope casing strict", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Scope = "connection" }},
		{"private resource", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Resource.Type = "MEMORY" }},
		{"zero resource", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Resource.ID = "00000000-0000-0000-0000-000000000000"
		}},
		{"no requested fields", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Fields = nil }},
		{"private requested field", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Fields = []string{"PRIVATE_SCHEDULE"}
		}},
		{"duplicate requested fields", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Fields = []string{CoordinationFieldNextStep, CoordinationFieldNextStep}
		}},
		{"zero expiry", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.ExpiresAt = time.Time{} }},
		{"expiry at current instant", func(r *CoordinationRequest, _ *CoordinationFacts, now time.Time) { r.ExpiresAt = now }},
		{"past expiry", func(r *CoordinationRequest, _ *CoordinationFacts, now time.Time) {
			r.ExpiresAt = now.Add(-time.Nanosecond)
		}},
		{"over maximum TTL", func(r *CoordinationRequest, _ *CoordinationFacts, now time.Time) {
			r.ExpiresAt = now.Add(CoordinationMaxTTL + time.Nanosecond)
		}},
		{"missing sender agent", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Sender.AgentID = "" }},
		{"zero recipient agent", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Recipient.AgentID = "00000000-0000-0000-0000-000000000000"
		}},
		{"same agent", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Recipient.AgentID = r.Sender.AgentID
		}},
		{"same Person", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Recipient.Principal = r.Sender.Principal
		}},
		{"sender organization", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Sender.Principal.Type = actorref.Organization
		}},
		{"recipient organization", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Recipient.Principal.Type = actorref.Organization
		}},
		{"recipient Business", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Recipient.Principal.Type = actorref.Business
		}},
		{"recipient Community", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) {
			r.Recipient.Principal.Type = actorref.Community
		}},
		{"recipient City", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Recipient.Principal.Type = "CITY" }},
		{"principal type casing strict", func(r *CoordinationRequest, _ *CoordinationFacts, _ time.Time) { r.Sender.Principal.Type = "person" }},
		{"anonymous session", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Session = CoordinationSessionFacts{}
		}},
		{"session proof missing", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Session.Verified = false }},
		{"session inactive", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Session.Active = false }},
		{"acting user mismatch", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Session.ActingUserID = f.Recipient.Principal.ID
		}},
		{"sender ownership not verified", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Sender.Verified = false }},
		{"recipient ownership not verified", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Recipient.Verified = false }},
		{"sender Agent stopped", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Sender.Active = false }},
		{"recipient Agent stopped", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Recipient.Active = false }},
		{"sender owner stopped", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Sender.OwnerActive = false }},
		{"recipient owner stopped", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Recipient.OwnerActive = false }},
		{"sender role replaced", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Sender.Role = OrganizationAgent }},
		{"recipient role replaced", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Recipient.Role = BusinessAgent }},
		{"sender Agent record mismatch", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Sender.AgentID = f.Recipient.AgentID
		}},
		{"recipient principal record mismatch", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Recipient.Principal = f.Sender.Principal
		}},
		{"source task proof missing", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Task.Verified = false }},
		{"source task ID mismatch", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Task.ID = f.Tie.ID }},
		{"source task ownership mismatch", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Task.OwnerPrincipal = f.Recipient.Principal
		}},
		{"source task Agent mismatch", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Task.AgentID = f.Recipient.AgentID }},
		{"task active not completed", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Task.Status = "ACTIVE" }},
		{"task failed", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Task.Status = "FAILED" }},
		{"task unrelated intent", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Task.Intent = "FIND_NEW_PEOPLE" }},
		{"task resource revoked", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Task.ResourceAuthorized = false }},
		{"source resource proof missing", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Resource.Verified = false }},
		{"source resource mismatch", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Resource.Reference.ID = f.Task.ID }},
		{"unpublished Activity", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Resource.Published = false }},
		{"cancelled Activity", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Resource.Cancelled = true }},
		{"missing Activity start", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Resource.StartsAt = time.Time{} }},
		{"invalid Activity range", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Resource.EndsAt = f.Resource.StartsAt
		}},
		{"ended Activity", func(_ *CoordinationRequest, f *CoordinationFacts, now time.Time) {
			f.Resource.StartsAt, f.Resource.EndsAt = now.Add(-time.Hour), now
		}},
		{"expired Activity", func(_ *CoordinationRequest, f *CoordinationFacts, now time.Time) {
			f.Resource.ExpiresAt = coordinationTime(now)
		}},
		{"sender cannot read Activity", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Resource.SenderVisible = false }},
		{"recipient cannot read Activity", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Resource.RecipientVisible = false }},
		{"Tie proof missing", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.Verified = false }},
		{"Tie ID missing", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.ID = "" }},
		{"Tie request reference missing", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.RequestID = "" }},
		{"accepted request sender from another pair", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.RequestSenderID = f.Task.ID }},
		{"accepted request recipient from another pair", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.RequestRecipientID = f.Task.ID }},
		{"accepted request has missing pair", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.RequestSenderID = "" }},
		{"accepted request pair same Person", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Tie.RequestRecipientID = f.Tie.RequestSenderID
		}},
		{"Tie removed", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.Active = false }},
		{"chat is not Tie", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.RequestScope = "conversation" }},
		{"Follow is not Tie", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.RequestScope = "follow" }},
		{"shared Activity is not Tie", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Tie.RequestScope = "shared_activity"
		}},
		{"request not accepted", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.RequestState = "pending" }},
		{"request acceptance revoked", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.RequestState = "declined" }},
		{"pair unrelated", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.PersonBID = f.Task.ID }},
		{"pair order corrupted", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.Tie.PersonAID, f.Tie.PersonBID = f.Tie.PersonBID, f.Tie.PersonAID
		}},
		{"pair blocked", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) { f.Tie.Blocked = true }},
		{"independent grant reused", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.RecipientGrant.ID = f.SenderGrant.ID
		}},
		{"both grants same owner", func(_ *CoordinationRequest, f *CoordinationFacts, _ time.Time) {
			f.RecipientGrant.OwnerPrincipal = f.SenderGrant.OwnerPrincipal
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, request, facts := coordinationPolicyFixture()
			tc.edit(&request, &facts, now)
			if decision := DecideCoordination(now, request, facts); decision.Allowed || decision.Reason == "" {
				t.Fatalf("unsafe request authorized: %+v", decision)
			}
			response, err := BuildCoordinationResponse(now, request, facts)
			if !errors.Is(err, ErrCoordinationForbidden) || response != (CoordinationResponse{}) {
				t.Fatalf("denial disclosed a response: %+v %v", response, err)
			}
		})
	}
}

func TestCoordinationIndependentGrantMatrix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*CoordinationGrant, CoordinationRequest, time.Time)
	}{
		{"absent grant", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { *g = CoordinationGrant{} }},
		{"proof missing", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Verified = false }},
		{"invalid grant ID", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.ID = "profile-grant" }},
		{"missing owner", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.OwnerPrincipal.ID = "" }},
		{"organization owner", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) {
			g.OwnerPrincipal.Type = actorref.Organization
		}},
		{"request replay", func(g *CoordinationGrant, r CoordinationRequest, _ time.Time) { g.RequestID = r.TaskID }},
		{"task replay", func(g *CoordinationGrant, r CoordinationRequest, _ time.Time) { g.TaskID = r.RequestID }},
		{"sender Agent replay", func(g *CoordinationGrant, r CoordinationRequest, _ time.Time) { g.SenderAgentID = r.Recipient.AgentID }},
		{"recipient Agent replay", func(g *CoordinationGrant, r CoordinationRequest, _ time.Time) { g.RecipientAgentID = r.Sender.AgentID }},
		{"sender principal replay", func(g *CoordinationGrant, r CoordinationRequest, _ time.Time) {
			g.SenderPrincipal = r.Recipient.Principal
		}},
		{"recipient principal replay", func(g *CoordinationGrant, r CoordinationRequest, _ time.Time) {
			g.RecipientPrincipal = r.Sender.Principal
		}},
		{"profile_view purpose substitution", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Purpose = "profile_view" }},
		{"051 own context purpose substitution", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) {
			g.Purpose = "agent_relationship_context"
		}},
		{"052 new people purpose substitution", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Purpose = "new_people" }},
		{"resource scope escalation", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Scope = Private }},
		{"resource type replay", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Resource.Type = "MOMENT" }},
		{"Activity replay", func(g *CoordinationGrant, r CoordinationRequest, _ time.Time) { g.Resource.ID = r.TaskID }},
		{"no fields", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Fields = nil }},
		{"profile_read field substitution", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Fields = []string{"read"} }},
		{"extra field escalation", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) {
			g.Fields = []string{CoordinationFieldNextStep, "SCHEDULE"}
		}},
		{"no human confirmation", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.HumanConfirmed = false }},
		{"missing confirmation time", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.ConfirmedAt = time.Time{} }},
		{"future confirmation time", func(g *CoordinationGrant, _ CoordinationRequest, now time.Time) {
			g.ConfirmedAt = now.Add(time.Nanosecond)
		}},
		{"missing expiry", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.ExpiresAt = time.Time{} }},
		{"expired now", func(g *CoordinationGrant, _ CoordinationRequest, now time.Time) { g.ExpiresAt = now }},
		{"expiry does not cover request", func(g *CoordinationGrant, r CoordinationRequest, _ time.Time) {
			g.ExpiresAt = r.ExpiresAt.Add(-time.Nanosecond)
		}},
		{"explicitly revoked", func(g *CoordinationGrant, _ CoordinationRequest, now time.Time) {
			g.RevokedAt = coordinationTime(now.Add(-time.Second))
		}},
		{"revocation scheduled or zero still not current", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) {
			g.RevokedAt = coordinationTime(time.Time{})
		}},
		{"missing revision", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Revision = 0 }},
		{"missing current revision", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.CurrentRevision = 0 }},
		{"negative revisions", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.Revision, g.CurrentRevision = -1, -1 }},
		{"stale revision", func(g *CoordinationGrant, _ CoordinationRequest, _ time.Time) { g.CurrentRevision++ }},
	}
	for _, side := range []string{"sender", "recipient"} {
		for _, tc := range cases {
			t.Run(side+"/"+tc.name, func(t *testing.T) {
				now, request, facts := coordinationPolicyFixture()
				grant := &facts.SenderGrant
				if side == "recipient" {
					grant = &facts.RecipientGrant
				}
				tc.edit(grant, request, now)
				if decision := DecideCoordination(now, request, facts); decision.Allowed || decision.Reason != "coordination_independent_consent_required" {
					t.Fatalf("invalid independent consent authorized: %+v", decision)
				}
				response, err := BuildCoordinationResponse(now, request, facts)
				if !errors.Is(err, ErrCoordinationForbidden) || response != (CoordinationResponse{}) {
					t.Fatalf("denial disclosed response: %+v %v", response, err)
				}
			})
		}
	}
}

func TestCoordinationExpiryRevalidationAndOngoingActivity(t *testing.T) {
	now, request, facts := coordinationPolicyFixture()
	// The accepted friend request can have been sent in either direction.
	facts.Tie.RequestSenderID, facts.Tie.RequestRecipientID = facts.Tie.RequestRecipientID, facts.Tie.RequestSenderID
	if !DecideCoordination(now, request, facts).Allowed {
		t.Fatal("valid reverse-direction accepted friend request rejected")
	}
	request.ExpiresAt = now.Add(CoordinationMaxTTL)
	facts.SenderGrant.ExpiresAt, facts.RecipientGrant.ExpiresAt = request.ExpiresAt, request.ExpiresAt
	if !DecideCoordination(now, request, facts).Allowed {
		t.Fatal("inclusive 15 minute maximum rejected")
	}
	if DecideCoordination(time.Time{}, request, facts).Allowed {
		t.Fatal("unknown server clock authorized")
	}
	// A started Activity can remain eligible; no availability is inferred.
	facts.Resource.StartsAt = now.Add(-time.Minute)
	if !DecideCoordination(now, request, facts).Allowed {
		t.Fatal("ongoing, authorized and unfinished Activity rejected")
	}
	facts.Resource.ExpiresAt = coordinationTime(now.Add(time.Minute))
	if !DecideCoordination(now, request, facts).Allowed {
		t.Fatal("currently valid Activity rejected")
	}
	if response, err := BuildCoordinationResponse(now.Add(time.Minute), request, facts); !errors.Is(err, ErrCoordinationForbidden) || response != (CoordinationResponse{}) {
		t.Fatalf("cached allowed outcome survived Activity expiry: %+v %v", response, err)
	}
	facts.Resource.ExpiresAt = nil
	facts.RecipientGrant.RevokedAt = coordinationTime(now)
	if response, err := BuildCoordinationResponse(now, request, facts); !errors.Is(err, ErrCoordinationForbidden) || response != (CoordinationResponse{}) {
		t.Fatalf("cached allowed outcome survived one-side revocation: %+v %v", response, err)
	}
}

func TestCoordinationDefaultsAndNoMutation(t *testing.T) {
	now, request, facts := coordinationPolicyFixture()
	for _, input := range []struct {
		request CoordinationRequest
		facts   CoordinationFacts
	}{{request, CoordinationFacts{}}, {CoordinationRequest{}, facts}, {CoordinationRequest{}, CoordinationFacts{}}} {
		if DecideCoordination(now, input.request, input.facts).Allowed {
			t.Fatal("zero or partial server evidence authorized")
		}
	}
	originalRequest, originalFacts := request, facts
	for i := 0; i < 2; i++ {
		response, err := BuildCoordinationResponse(now, request, facts)
		if err != nil || response.NextStep != CoordinationNextStepAskUser {
			t.Fatalf("pure permission outcome: %+v %v", response, err)
		}
	}
	if !reflect.DeepEqual(request, originalRequest) || !reflect.DeepEqual(facts, originalFacts) {
		t.Fatal("pure policy altered source request, consent or relationship")
	}
	// The contract carries no durable replay counter. Repeated pure calls cannot
	// demonstrate delivery/idempotency; that requires AGA-002's live adapter.
}
