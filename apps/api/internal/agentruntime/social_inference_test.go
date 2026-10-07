package agentruntime

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

// These synthetic facts prove a permission contract. They do not establish a
// learner, live consent, a real preference, public sharing or an action executor.
func socialInferenceFixture(provenance string) (time.Time, SocialInferenceRequest, SocialInferenceFacts) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: "11111111-1111-4111-8111-111111111111"}
	req := SocialInferenceRequest{
		Version: SocialInferenceVersion, RequestID: "66666666-6666-4666-8666-666666666666",
		TaskID: "55555555-5555-4555-8555-555555555555", AgentID: "33333333-3333-4333-8333-333333333333",
		OwnerPrincipal: owner, ResourceID: "77777777-7777-4777-8777-777777777777",
		Action: SocialActionRead, Purpose: SocialPreferenceReviewPurpose, Scope: Private,
		ExpiresAt: now.Add(10 * time.Minute),
	}
	source := SocialPreferenceSource{
		Verified: true, ID: "88888888-8888-4888-8888-888888888888", Type: "USER_STATEMENT",
		Kind: "EXPLICIT_DECLARATION", OwnerPrincipal: owner, Category: "badminton", Sensitivity: SocialPreferenceOrdinary,
		Revision: 1, CurrentRevision: 1, ClusterID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		IndependentClusterVerified: true, ObservedAt: now.Add(-time.Hour), CurrentlyAuthorized: true, UserConfirmed: true,
	}
	claim := SocialPreferenceClaim{
		Verified: true, ID: req.ResourceID, OwnerPrincipal: owner, Key: SocialPreferenceActivityCategory,
		Value: "badminton", Provenance: provenance, Sensitivity: SocialPreferenceOrdinary,
		Revision: 1, CurrentRevision: 1, ExpiresAt: now.Add(15 * time.Minute), Sources: []SocialPreferenceSource{source},
	}
	switch provenance {
	case SocialPreferenceRuleFact:
		claim.Sources[0].Type, claim.Sources[0].Kind = "SOCIAL_INTENT", "EXPLICIT_RULE"
	case SocialPreferenceInferred:
		claim.AssessmentKind, claim.Score = SocialPreferenceUncalibratedScore, 0.7
		claim.Sources[0].Type, claim.Sources[0].Kind = "ACTIVITY_PARTICIPATION", "AUTHORIZED_OBSERVATION"
		claim.Sources[0].AnalysisAuthorized, claim.Sources[0].AnalysisPurpose = true, SocialPreferenceAnalysisPurpose
		claim.Sources[0].AnalysisScope, claim.Sources[0].AnalysisRevision, claim.Sources[0].CurrentAnalysisRevision = Private, 1, 1
		claim.Sources[0].AnalysisExpiresAt = now.Add(15 * time.Minute)
		second := claim.Sources[0]
		second.ID, second.Type, second.ClusterID = "99999999-9999-4999-8999-999999999999", "SAVED_PLACE", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
		claim.Sources = append(claim.Sources, second)
	}
	facts := SocialInferenceFacts{
		Session: SocialInferenceSession{Verified: true, Active: true, ID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", ActingUserID: owner.ID, ExpiresAt: now.Add(time.Hour)},
		Agent:   SocialInferenceAgent{Verified: true, Active: true, OwnerActive: true, Role: PersonalAgent, ID: req.AgentID, OwnerPrincipal: owner},
		Task:    SocialInferenceTask{Verified: true, ID: req.TaskID, ActingUserID: owner.ID, AgentID: req.AgentID, OwnerPrincipal: owner, Status: "COMPLETED", Revision: 1, CurrentRevision: 1},
		Claim:   claim,
		Grant: SocialPreferenceGrant{
			Verified: true, ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", SessionID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", ActorID: owner.ID,
			OwnerPrincipal: owner, AgentID: req.AgentID, RequestID: req.RequestID, TaskID: req.TaskID, TaskRevision: 1,
			ResourceID: req.ResourceID, ResourceRevision: 1, Action: req.Action, Purpose: req.Purpose, Scope: req.Scope,
			Revision: 1, CurrentRevision: 1, HumanConfirmed: true, ConfirmedAt: now.Add(-time.Minute), ExpiresAt: now.Add(15 * time.Minute),
		},
	}
	socialInferenceBindTask(&req, &facts)
	facts.Grant.PayloadDigest = facts.Task.PayloadDigest
	return now, req, facts
}

func socialInferenceBindTask(req *SocialInferenceRequest, facts *SocialInferenceFacts) {
	digest, err := SocialPreferencePayloadDigest(facts.Claim)
	if err != nil {
		panic(err)
	}
	facts.Task.Action, facts.Task.Purpose, facts.Task.Scope = req.Action, req.Purpose, req.Scope
	facts.Task.ResourceID, facts.Task.ResourceRevision, facts.Task.PayloadDigest = req.ResourceID, facts.Claim.CurrentRevision, digest
}

func socialInferenceTime(v time.Time) *time.Time { return &v }

func socialInferenceRequireAllowed(t *testing.T, now time.Time, req SocialInferenceRequest, facts SocialInferenceFacts) {
	t.Helper()
	if result := DecideSocialInference(now, req, facts); !result.Allowed || result.Reason != "allowed" {
		t.Fatalf("synthetic contract rejected: %+v", result)
	}
}

func socialInferenceRequireDenied(t *testing.T, now time.Time, req SocialInferenceRequest, facts SocialInferenceFacts) {
	t.Helper()
	if result := DecideSocialInference(now, req, facts); result.Allowed || result.Reason == "" || result.Reason == "allowed" {
		t.Fatalf("invalid authority allowed: %+v", result)
	}
}

func TestSocialInferenceSyntheticProvenanceAndActionContract(t *testing.T) {
	for _, provenance := range []string{SocialPreferenceExplicit, SocialPreferenceRuleFact, SocialPreferenceInferred} {
		t.Run(provenance, func(t *testing.T) {
			now, req, facts := socialInferenceFixture(provenance)
			socialInferenceRequireAllowed(t, now, req, facts)
			req.Action = SocialActionNavigate
			socialInferenceBindTask(&req, &facts)
			facts.Grant.Action = req.Action
			socialInferenceRequireAllowed(t, now, req, facts)
		})
	}
	for _, category := range []string{"badminton", "basketball", "football", "sports", "culture"} {
		t.Run("ordinary category "+category, func(t *testing.T) {
			now, req, facts := socialInferenceFixture(SocialPreferenceExplicit)
			facts.Claim.Value, facts.Claim.Sources[0].Category = category, category
			socialInferenceBindTask(&req, &facts)
			facts.Grant.PayloadDigest = facts.Task.PayloadDigest
			socialInferenceRequireAllowed(t, now, req, facts)
		})
	}
}

func TestSocialInferencePublicRequiresIndependentVersionConsent(t *testing.T) {
	now, req, facts := socialInferenceFixture(SocialPreferenceInferred)
	req.Action, req.Purpose, req.Scope = SocialActionPublish, SocialPreferencePublishPurpose, Public
	socialInferenceBindTask(&req, &facts)
	// A private review grant cannot become a public release through an action.
	socialInferenceRequireDenied(t, now, req, facts)
	facts.Grant = SocialPreferenceGrant{}
	profile := ForType(actorref.Person).DecideContext(AccessRequest{Scope: Public, OwnerID: req.OwnerPrincipal.ID,
		ViewerID: req.OwnerPrincipal.ID, PrincipalID: req.OwnerPrincipal.ID, AuthorityVerified: true, Released: true})
	if !profile.Allowed {
		t.Fatal("fixture should independently allow an ordinary released profile")
	}
	// Even a released profile and valid sources cannot stand in for this grant.
	socialInferenceRequireDenied(t, now, req, facts)
	_, _, facts = socialInferenceFixture(SocialPreferenceInferred)
	socialInferenceBindTask(&req, &facts)
	facts.Grant.ID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	facts.Grant.Action, facts.Grant.Purpose, facts.Grant.Scope = req.Action, req.Purpose, req.Scope
	facts.Grant.ConfirmedAt = now
	socialInferenceRequireAllowed(t, now, req, facts)
	// This is only a proof fixture: the current runtime has no public capability.
	for _, actor := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business, actorref.Community} {
		policy := ForType(actor)
		for _, capability := range []Capability{"social_inference.read", "social_inference.publish", "social_inference.execute", "memory.infer", "coordination.send"} {
			if policy.Allows(capability) {
				t.Fatalf("unimplemented live capability exposed for %s: %s", actor, capability)
			}
		}
	}
}

func TestSocialInferenceHighImpactAlwaysDenied(t *testing.T) {
	for _, provenance := range []string{SocialPreferenceExplicit, SocialPreferenceRuleFact, SocialPreferenceInferred} {
		t.Run(provenance, func(t *testing.T) {
			now, req, facts := socialInferenceFixture(provenance)
			req.Action = SocialActionHighImpact
			socialInferenceBindTask(&req, &facts)
			facts.Grant.Action = req.Action
			result := DecideSocialInference(now, req, facts)
			if result.Allowed || result.Reason != "agent_effects_unavailable" {
				t.Fatalf("confirmed candidate became an external effect: %+v", result)
			}
		})
	}
}

func TestSocialInferenceRequestSessionTaskAuthorityMatrix(t *testing.T) {
	type mutation func(*SocialInferenceRequest, *SocialInferenceFacts, time.Time)
	otherID := "22222222-2222-4222-8222-222222222222"
	cases := []struct {
		name string
		edit mutation
	}{
		{"wrong version", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Version = "v2" }},
		{"missing request ID", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.RequestID = "" }},
		{"zero request ID", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) {
			r.RequestID = "00000000-0000-0000-0000-000000000000"
		}},
		{"padded task ID", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.TaskID = " " + r.TaskID }},
		{"invalid agent ID", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.AgentID = "agent" }},
		{"missing resource ID", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.ResourceID = "" }},
		{"anonymous owner", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) {
			r.OwnerPrincipal = actorref.PrincipalRef{}
		}},
		{"other owner", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.OwnerPrincipal.ID = otherID }},
		{"organization owner", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) {
			r.OwnerPrincipal.Type = actorref.Organization
		}},
		{"business owner", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) {
			r.OwnerPrincipal.Type = actorref.Business
		}},
		{"community owner", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) {
			r.OwnerPrincipal.Type = actorref.Community
		}},
		{"city owner", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.OwnerPrincipal.Type = "CITY" }},
		{"missing expiry", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.ExpiresAt = time.Time{} }},
		{"expired at now", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, n time.Time) { r.ExpiresAt = n }},
		{"past expiry", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, n time.Time) {
			r.ExpiresAt = n.Add(-time.Nanosecond)
		}},
		{"over maximum TTL", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, n time.Time) {
			r.ExpiresAt = n.Add(SocialInferenceMaxTTL + time.Nanosecond)
		}},
		{"unknown action", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Action = "AUTO_RSVP" }},
		{"blank action", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Action = "" }},
		{"lowercase action", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Action = "read" }},
		{"unknown review purpose", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Purpose = "READ_PROFILE" }},
		{"public read", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Scope = Public }},
		{"connection read", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Scope = Connection }},
		{"close read", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Scope = Close }},
		{"workspace read", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Scope = WorkspacePrivate }},
		{"private publish", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) { r.Action = SocialActionPublish }},
		{"unknown publish purpose", func(r *SocialInferenceRequest, _ *SocialInferenceFacts, _ time.Time) {
			r.Action, r.Scope, r.Purpose = SocialActionPublish, Public, "profile_release"
		}},
		{"unverified session", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Session.Verified = false }},
		{"inactive session", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Session.Active = false }},
		{"invalid session ID", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Session.ID = "session" }},
		{"expired session", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, n time.Time) { f.Session.ExpiresAt = n }},
		{"session expiry before request", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, n time.Time) {
			f.Session.ExpiresAt = n.Add(time.Minute)
		}},
		{"anonymous session", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Session.ActingUserID = "" }},
		{"other session actor", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Session.ActingUserID = otherID
		}},
		{"session replaced old approval", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Session.ID = otherID }},
		{"unverified agent", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Agent.Verified = false }},
		{"inactive agent", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Agent.Active = false }},
		{"inactive owner", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Agent.OwnerActive = false }},
		{"organization agent", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Agent.Role = OrganizationAgent
		}},
		{"business agent", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Agent.Role = BusinessAgent }},
		{"other agent", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Agent.ID = otherID }},
		{"other agent owner", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Agent.OwnerPrincipal.ID = otherID
		}},
		{"unverified task", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.Verified = false }},
		{"other task", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.ID = otherID }},
		{"other task actor", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.ActingUserID = otherID }},
		{"other task agent", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.AgentID = otherID }},
		{"other task owner", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Task.OwnerPrincipal.ID = otherID
		}},
		{"organization task owner", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Task.OwnerPrincipal.Type = actorref.Organization
		}},
		{"pending task", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.Status = "RUNNING" }},
		{"cancelled task", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.Status = "CANCELLED" }},
		{"zero task revision", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Task.Revision, f.Task.CurrentRevision = 0, 0
		}},
		{"stale task revision", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.CurrentRevision++ }},
		{"changed task version old approval", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Task.Revision++
			f.Task.CurrentRevision++
		}},
		{"task wrong action", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Task.Action = SocialActionNavigate
		}},
		{"task wrong purpose", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Task.Purpose = SocialPreferencePublishPurpose
		}},
		{"task wrong scope", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.Scope = Public }},
		{"task wrong resource", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.ResourceID = otherID }},
		{"task wrong resource version", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) { f.Task.ResourceRevision++ }},
		{"task wrong payload", func(_ *SocialInferenceRequest, f *SocialInferenceFacts, _ time.Time) {
			f.Task.PayloadDigest = strings.Repeat("0", 64)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, req, facts := socialInferenceFixture(SocialPreferenceInferred)
			tc.edit(&req, &facts, now)
			socialInferenceRequireDenied(t, now, req, facts)
		})
	}
	now, req, facts := socialInferenceFixture(SocialPreferenceExplicit)
	socialInferenceRequireDenied(t, time.Time{}, req, facts)
	socialInferenceRequireDenied(t, now, req, SocialInferenceFacts{})
	socialInferenceRequireDenied(t, now, SocialInferenceRequest{}, facts)
	req.ExpiresAt = now.Add(SocialInferenceMaxTTL)
	socialInferenceRequireAllowed(t, now, req, facts)
	facts.Session.ExpiresAt, facts.Claim.ExpiresAt, facts.Grant.ExpiresAt = req.ExpiresAt, req.ExpiresAt, req.ExpiresAt
	socialInferenceRequireAllowed(t, now, req, facts)
}

func TestSocialInferenceClaimAndSourceMatrix(t *testing.T) {
	type mutation func(*SocialPreferenceClaim, time.Time)
	otherID := "22222222-2222-4222-8222-222222222222"
	cases := []struct {
		name string
		edit mutation
	}{
		{"unverified claim", func(c *SocialPreferenceClaim, _ time.Time) { c.Verified = false }},
		{"other claim", func(c *SocialPreferenceClaim, _ time.Time) { c.ID = otherID }},
		{"other claim owner", func(c *SocialPreferenceClaim, _ time.Time) { c.OwnerPrincipal.ID = otherID }},
		{"organization claim owner", func(c *SocialPreferenceClaim, _ time.Time) { c.OwnerPrincipal.Type = actorref.Organization }},
		{"missing provenance", func(c *SocialPreferenceClaim, _ time.Time) { c.Provenance = "" }},
		{"unknown provenance", func(c *SocialPreferenceClaim, _ time.Time) { c.Provenance = "GENERATED" }},
		{"provenance casing", func(c *SocialPreferenceClaim, _ time.Time) { c.Provenance = "inferred" }},
		{"undeclared sensitive candidate", func(c *SocialPreferenceClaim, _ time.Time) { c.Sensitivity = "" }},
		{"sensitive candidate", func(c *SocialPreferenceClaim, _ time.Time) { c.Sensitivity = "SENSITIVE" }},
		{"private health attribute", func(c *SocialPreferenceClaim, _ time.Time) { c.Key = "HEALTH_STATUS" }},
		{"relationship score", func(c *SocialPreferenceClaim, _ time.Time) { c.Key = "RELATIONSHIP_STRENGTH" }},
		{"personality attribute", func(c *SocialPreferenceClaim, _ time.Time) { c.Key = "PERSONALITY" }},
		{"geo attribute", func(c *SocialPreferenceClaim, _ time.Time) { c.Key = "HOME_LOCATION" }},
		{"ethnicity category", func(c *SocialPreferenceClaim, _ time.Time) {
			c.Value, c.Sources[0].Category, c.Sources[1].Category = "ethnicity", "ethnicity", "ethnicity"
		}},
		{"free text category", func(c *SocialPreferenceClaim, _ time.Time) { c.Value = "probably lonely" }},
		{"uncanonical category", func(c *SocialPreferenceClaim, _ time.Time) { c.Value = "Badminton" }},
		{"zero revision", func(c *SocialPreferenceClaim, _ time.Time) { c.Revision, c.CurrentRevision = 0, 0 }},
		{"stale claim", func(c *SocialPreferenceClaim, _ time.Time) { c.CurrentRevision++ }},
		{"new claim old approval", func(c *SocialPreferenceClaim, _ time.Time) { c.Revision++; c.CurrentRevision++ }},
		{"claim deleted", func(c *SocialPreferenceClaim, _ time.Time) { c.Deleted = true }},
		{"claim revoked", func(c *SocialPreferenceClaim, n time.Time) { c.RevokedAt = socialInferenceTime(n) }},
		{"claim future revocation scheduled", func(c *SocialPreferenceClaim, n time.Time) { c.RevokedAt = socialInferenceTime(n.Add(time.Hour)) }},
		{"claim expired", func(c *SocialPreferenceClaim, n time.Time) { c.ExpiresAt = n }},
		{"claim expires before operation", func(c *SocialPreferenceClaim, n time.Time) { c.ExpiresAt = n.Add(time.Minute) }},
		{"no source", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources = nil }},
		{"single weak inference source", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources = c.Sources[:1] }},
		{"duplicate source", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[1].ID = c.Sources[0].ID }},
		{"zero source ID", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].ID = "00000000-0000-0000-0000-000000000000" }},
		{"unverified source", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Verified = false }},
		{"other source owner", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].OwnerPrincipal.ID = otherID }},
		{"organization source owner", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].OwnerPrincipal.Type = actorref.Organization }},
		{"different source category", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Category = "football" }},
		{"source missing sensitivity", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Sensitivity = "" }},
		{"sensitive source", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Sensitivity = "SENSITIVE" }},
		{"zero source version", func(c *SocialPreferenceClaim, _ time.Time) {
			c.Sources[0].Revision, c.Sources[0].CurrentRevision = 0, 0
		}},
		{"stale source version", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].CurrentRevision++ }},
		{"new source old approval", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Revision++; c.Sources[0].CurrentRevision++ }},
		{"missing cluster", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].ClusterID = "" }},
		{"correlated sources", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[1].ClusterID = c.Sources[0].ClusterID }},
		{"unverified independent source", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].IndependentClusterVerified = false }},
		{"source lacks current authorization", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].CurrentlyAuthorized = false }},
		{"source deleted", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Deleted = true }},
		{"source revoked", func(c *SocialPreferenceClaim, n time.Time) { c.Sources[0].RevokedAt = socialInferenceTime(n) }},
		{"source expired", func(c *SocialPreferenceClaim, n time.Time) { c.Sources[0].ExpiresAt = socialInferenceTime(n) }},
		{"source expires before operation", func(c *SocialPreferenceClaim, n time.Time) {
			c.Sources[0].ExpiresAt = socialInferenceTime(n.Add(time.Minute))
		}},
		{"missing observation time", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].ObservedAt = time.Time{} }},
		{"future observation", func(c *SocialPreferenceClaim, n time.Time) { c.Sources[0].ObservedAt = n.Add(time.Nanosecond) }},
		{"unknown source type", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Type = "CHAT_BODY" }},
		{"geo inference source", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Type = "PERSON_LOCATION" }},
		{"profile visibility source", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Type = "PUBLIC_PROFILE" }},
		{"unknown source kind", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Kind = "GENERATED" }},
		{"rule cannot become observation", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].Kind = "EXPLICIT_RULE" }},
		{"missing analysis consent", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].AnalysisAuthorized = false }},
		{"analysis wrong purpose", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].AnalysisPurpose = "PROFILE_READ" }},
		{"analysis public scope", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].AnalysisScope = Public }},
		{"analysis connection scope", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].AnalysisScope = Connection }},
		{"analysis zero version", func(c *SocialPreferenceClaim, _ time.Time) {
			c.Sources[0].AnalysisRevision, c.Sources[0].CurrentAnalysisRevision = 0, 0
		}},
		{"analysis stale version", func(c *SocialPreferenceClaim, _ time.Time) { c.Sources[0].CurrentAnalysisRevision++ }},
		{"new analysis old approval", func(c *SocialPreferenceClaim, _ time.Time) {
			c.Sources[0].AnalysisRevision++
			c.Sources[0].CurrentAnalysisRevision++
		}},
		{"analysis expired", func(c *SocialPreferenceClaim, n time.Time) { c.Sources[0].AnalysisExpiresAt = n }},
		{"analysis expires before request", func(c *SocialPreferenceClaim, n time.Time) { c.Sources[0].AnalysisExpiresAt = n.Add(time.Minute) }},
		{"missing assessment kind", func(c *SocialPreferenceClaim, _ time.Time) { c.AssessmentKind = "" }},
		{"false probability assessment", func(c *SocialPreferenceClaim, _ time.Time) { c.AssessmentKind = "CALIBRATED_PROBABILITY" }},
		{"NaN assessment", func(c *SocialPreferenceClaim, _ time.Time) { c.Score = math.NaN() }},
		{"positive infinity assessment", func(c *SocialPreferenceClaim, _ time.Time) { c.Score = math.Inf(1) }},
		{"negative infinity assessment", func(c *SocialPreferenceClaim, _ time.Time) { c.Score = math.Inf(-1) }},
		{"negative assessment", func(c *SocialPreferenceClaim, _ time.Time) { c.Score = -0.1 }},
		{"zero assessment", func(c *SocialPreferenceClaim, _ time.Time) { c.Score = 0 }},
		{"out of range assessment", func(c *SocialPreferenceClaim, _ time.Time) { c.Score = 1.01 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, req, facts := socialInferenceFixture(SocialPreferenceInferred)
			tc.edit(&facts.Claim, now)
			socialInferenceRequireDenied(t, now, req, facts)
		})
	}
	// Current valid facts still need exactly the approval payload previously shown.
	now, req, facts := socialInferenceFixture(SocialPreferenceInferred)
	facts.Claim.Sources[0].ExpiresAt = socialInferenceTime(req.ExpiresAt)
	socialInferenceBindTask(&req, &facts)
	facts.Grant.PayloadDigest = facts.Task.PayloadDigest
	socialInferenceRequireAllowed(t, now, req, facts)
}

func TestSocialInferenceProvenanceCannotBeSilentlyPromoted(t *testing.T) {
	for _, provenance := range []string{SocialPreferenceExplicit, SocialPreferenceRuleFact} {
		t.Run(provenance, func(t *testing.T) {
			now, req, facts := socialInferenceFixture(provenance)
			facts.Claim.AssessmentKind, facts.Claim.Score = SocialPreferenceUncalibratedScore, 0.7
			socialInferenceRequireDenied(t, now, req, facts)
			_, _, facts = socialInferenceFixture(provenance)
			facts.Claim.Sources[0].UserConfirmed = false
			if provenance == SocialPreferenceExplicit {
				socialInferenceRequireDenied(t, now, req, facts)
			}
			_, _, facts = socialInferenceFixture(provenance)
			second := facts.Claim.Sources[0]
			second.ID, second.ClusterID = "99999999-9999-4999-8999-999999999999", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
			facts.Claim.Sources = append(facts.Claim.Sources, second)
			socialInferenceRequireDenied(t, now, req, facts)
			facts.Claim.Provenance, facts.Claim.AssessmentKind, facts.Claim.Score = SocialPreferenceInferred, SocialPreferenceUncalibratedScore, 0.7
			// Neither repeated explicit input nor repeated query facts authorize analysis.
			socialInferenceRequireDenied(t, now, req, facts)
		})
	}
	for _, sourceType := range []string{"ACTIVITY_PARTICIPATION", "SAVED_PLACE", "MOMENT_CONTEXT"} {
		t.Run("bounded observation "+sourceType, func(t *testing.T) {
			now, req, facts := socialInferenceFixture(SocialPreferenceInferred)
			facts.Claim.Sources[0].Type = sourceType
			socialInferenceBindTask(&req, &facts)
			facts.Grant.PayloadDigest = facts.Task.PayloadDigest
			socialInferenceRequireAllowed(t, now, req, facts)
		})
	}
}

func TestSocialInferenceIndependentHumanGrantMatrix(t *testing.T) {
	type mutation func(*SocialPreferenceGrant, time.Time)
	otherID := "22222222-2222-4222-8222-222222222222"
	cases := []struct {
		name string
		edit mutation
	}{
		{"missing server verification", func(g *SocialPreferenceGrant, _ time.Time) { g.Verified = false }},
		{"missing ID", func(g *SocialPreferenceGrant, _ time.Time) { g.ID = "" }},
		{"all zero ID", func(g *SocialPreferenceGrant, _ time.Time) { g.ID = "00000000-0000-0000-0000-000000000000" }},
		{"wrong session", func(g *SocialPreferenceGrant, _ time.Time) { g.SessionID = otherID }},
		{"wrong actor", func(g *SocialPreferenceGrant, _ time.Time) { g.ActorID = otherID }},
		{"wrong owner", func(g *SocialPreferenceGrant, _ time.Time) { g.OwnerPrincipal.ID = otherID }},
		{"organization grant", func(g *SocialPreferenceGrant, _ time.Time) { g.OwnerPrincipal.Type = actorref.Organization }},
		{"wrong agent", func(g *SocialPreferenceGrant, _ time.Time) { g.AgentID = otherID }},
		{"wrong request", func(g *SocialPreferenceGrant, _ time.Time) { g.RequestID = otherID }},
		{"wrong task", func(g *SocialPreferenceGrant, _ time.Time) { g.TaskID = otherID }},
		{"wrong task revision", func(g *SocialPreferenceGrant, _ time.Time) { g.TaskRevision++ }},
		{"wrong resource", func(g *SocialPreferenceGrant, _ time.Time) { g.ResourceID = otherID }},
		{"wrong resource revision", func(g *SocialPreferenceGrant, _ time.Time) { g.ResourceRevision++ }},
		{"different action", func(g *SocialPreferenceGrant, _ time.Time) { g.Action = SocialActionNavigate }},
		{"different purpose", func(g *SocialPreferenceGrant, _ time.Time) { g.Purpose = SocialPreferencePublishPurpose }},
		{"old context switch purpose", func(g *SocialPreferenceGrant, _ time.Time) { g.Purpose = "SHARED_GOING_048" }},
		{"old relationship consent purpose", func(g *SocialPreferenceGrant, _ time.Time) { g.Purpose = "RELATIONSHIP_CONTEXT_051" }},
		{"old discovery consent purpose", func(g *SocialPreferenceGrant, _ time.Time) { g.Purpose = "NEW_PEOPLE_052" }},
		{"coordination grant purpose", func(g *SocialPreferenceGrant, _ time.Time) { g.Purpose = CoordinationPurposeAskActivityInterest }},
		{"wrong scope", func(g *SocialPreferenceGrant, _ time.Time) { g.Scope = Public }},
		{"missing digest", func(g *SocialPreferenceGrant, _ time.Time) { g.PayloadDigest = "" }},
		{"wrong digest", func(g *SocialPreferenceGrant, _ time.Time) { g.PayloadDigest = strings.Repeat("0", 64) }},
		{"uppercase digest is not approved bytes", func(g *SocialPreferenceGrant, _ time.Time) { g.PayloadDigest = strings.ToUpper(g.PayloadDigest) }},
		{"missing human confirmation", func(g *SocialPreferenceGrant, _ time.Time) { g.HumanConfirmed = false }},
		{"zero grant revision", func(g *SocialPreferenceGrant, _ time.Time) { g.Revision, g.CurrentRevision = 0, 0 }},
		{"stale grant revision", func(g *SocialPreferenceGrant, _ time.Time) { g.CurrentRevision++ }},
		{"missing confirmation time", func(g *SocialPreferenceGrant, _ time.Time) { g.ConfirmedAt = time.Time{} }},
		{"future confirmation", func(g *SocialPreferenceGrant, n time.Time) { g.ConfirmedAt = n.Add(time.Nanosecond) }},
		{"expired grant", func(g *SocialPreferenceGrant, n time.Time) { g.ExpiresAt = n }},
		{"grant expires before operation", func(g *SocialPreferenceGrant, n time.Time) { g.ExpiresAt = n.Add(time.Minute) }},
		{"revoked grant", func(g *SocialPreferenceGrant, n time.Time) { g.RevokedAt = socialInferenceTime(n) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, req, facts := socialInferenceFixture(SocialPreferenceInferred)
			tc.edit(&facts.Grant, now)
			decision := DecideSocialInference(now, req, facts)
			if decision.Allowed || decision.Reason != "independent_bound_human_consent_required" {
				t.Fatalf("grant not strictly checked: %+v", decision)
			}
		})
	}
}

func TestSocialInferenceDigestBindsContentAndSourceVersions(t *testing.T) {
	_, _, facts := socialInferenceFixture(SocialPreferenceInferred)
	digest, err := SocialPreferencePayloadDigest(facts.Claim)
	if err != nil || len(digest) != 64 {
		t.Fatalf("missing canonical approval binding: %s %v", digest, err)
	}
	claim := facts.Claim
	claim.Sources = []SocialPreferenceSource{facts.Claim.Sources[1], facts.Claim.Sources[0]}
	claim.ID, claim.OwnerPrincipal.ID = strings.ToUpper(claim.ID), strings.ToUpper(claim.OwnerPrincipal.ID)
	for i := range claim.Sources {
		claim.Sources[i].ID, claim.Sources[i].OwnerPrincipal.ID, claim.Sources[i].ClusterID =
			strings.ToUpper(claim.Sources[i].ID), strings.ToUpper(claim.Sources[i].OwnerPrincipal.ID), strings.ToUpper(claim.Sources[i].ClusterID)
	}
	canonical, err := SocialPreferencePayloadDigest(claim)
	if err != nil || canonical != digest {
		t.Fatalf("source ordering or UUID case changed semantic payload: %s %v", canonical, err)
	}
	cases := []struct {
		name string
		edit func(*SocialPreferenceClaim)
	}{
		{"category", func(c *SocialPreferenceClaim) {
			c.Value = "football"
			for i := range c.Sources {
				c.Sources[i].Category = c.Value
			}
		}},
		{"score", func(c *SocialPreferenceClaim) { c.Score = 0.8 }},
		{"claim version", func(c *SocialPreferenceClaim) { c.Revision++; c.CurrentRevision++ }},
		{"claim expiry", func(c *SocialPreferenceClaim) { c.ExpiresAt = c.ExpiresAt.Add(time.Minute) }},
		{"source ID", func(c *SocialPreferenceClaim) { c.Sources[0].ID = "22222222-2222-4222-8222-222222222222" }},
		{"source version", func(c *SocialPreferenceClaim) { c.Sources[0].Revision++; c.Sources[0].CurrentRevision++ }},
		{"analysis version", func(c *SocialPreferenceClaim) {
			c.Sources[0].AnalysisRevision++
			c.Sources[0].CurrentAnalysisRevision++
		}},
		{"source type", func(c *SocialPreferenceClaim) { c.Sources[0].Type = "MOMENT_CONTEXT" }},
		{"source cluster", func(c *SocialPreferenceClaim) { c.Sources[0].ClusterID = "22222222-2222-4222-8222-222222222222" }},
		{"source observation", func(c *SocialPreferenceClaim) { c.Sources[0].ObservedAt = c.Sources[0].ObservedAt.Add(-time.Minute) }},
		{"source expiry", func(c *SocialPreferenceClaim) { c.Sources[0].ExpiresAt = socialInferenceTime(c.ExpiresAt) }},
		{"analysis expiry", func(c *SocialPreferenceClaim) {
			c.Sources[0].AnalysisExpiresAt = c.Sources[0].AnalysisExpiresAt.Add(time.Minute)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, local := socialInferenceFixture(SocialPreferenceInferred)
			tc.edit(&local.Claim)
			changed, err := SocialPreferencePayloadDigest(local.Claim)
			if err != nil || changed == digest {
				t.Fatalf("old approval still binds changed content: %s %v", changed, err)
			}
		})
	}
	// Converting a candidate into an explicit assertion requires a distinct shape
	// and a new approval digest; the policy never performs this transformation.
	_, _, explicit := socialInferenceFixture(SocialPreferenceExplicit)
	explicitDigest, err := SocialPreferencePayloadDigest(explicit.Claim)
	if err != nil || explicitDigest == digest {
		t.Fatalf("provenance was omitted from binding: %s %v", explicitDigest, err)
	}
	_, _, rule := socialInferenceFixture(SocialPreferenceRuleFact)
	ruleDigest, err := SocialPreferencePayloadDigest(rule.Claim)
	if err != nil || ruleDigest == explicitDigest || ruleDigest == digest {
		t.Fatalf("rule source was confused with declaration/inference: %s %v", ruleDigest, err)
	}
	if _, err := SocialPreferencePayloadDigest(SocialPreferenceClaim{}); !errors.Is(err, ErrInvalidSocialPreference) {
		t.Fatalf("invalid source-less payload acquired approval key: %v", err)
	}
}

func TestSocialInferenceAuthorityFactsCannotCrossJSONBoundary(t *testing.T) {
	_, _, facts := socialInferenceFixture(SocialPreferenceInferred)
	canary := "SECRET_CANARY_CHAT_BODY_AND_LOCATION"
	cases := []struct {
		name  string
		value any
	}{
		{"all facts", facts}, {"session", facts.Session}, {"agent", facts.Agent}, {"task", facts.Task},
		{"claim", facts.Claim}, {"source", facts.Claim.Sources[0]}, {"grant", facts.Grant},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.value)
			if len(body) != 0 || !errors.Is(err, ErrSocialInferenceFactsJSON) || strings.Contains(err.Error(), canary) {
				t.Fatalf("server authority serializable: %s %v", body, err)
			}
			pointer := reflect.New(reflect.TypeOf(tc.value))
			pointer.Elem().Set(reflect.ValueOf(tc.value))
			err = json.Unmarshal([]byte(`{"Verified":true,"HumanConfirmed":true,"secret":"`+canary+`"}`), pointer.Interface())
			if !errors.Is(err, ErrSocialInferenceFactsJSON) || strings.Contains(err.Error(), canary) || !pointer.Elem().IsZero() {
				t.Fatalf("client proof accepted or retained previous trusted facts: %v zero=%v", err, pointer.Elem().IsZero())
			}
		})
	}
	// A denied decision never includes source IDs, score or a candidate text.
	now, req, facts := socialInferenceFixture(SocialPreferenceInferred)
	facts.Claim.Key, facts.Claim.Value = canary, canary
	decision := DecideSocialInference(now, req, facts)
	body, err := json.Marshal(decision)
	if err != nil || decision.Allowed || strings.Contains(string(body), canary) || strings.Contains(string(body), facts.Claim.Sources[0].ID) {
		t.Fatalf("denial leaked sensitive material: %s %v", body, err)
	}
}

func TestSocialInferenceBoundedSchemaAndExpiry(t *testing.T) {
	now, req, facts := socialInferenceFixture(SocialPreferenceInferred)
	// A schema-size limit rejects excessive observations before any source is
	// summarized; there is no free-text body for a learner to silently consume.
	for len(facts.Claim.Sources) <= 20 {
		facts.Claim.Sources = append(facts.Claim.Sources, facts.Claim.Sources[0])
	}
	socialInferenceRequireDenied(t, now, req, facts)
	_, _, facts = socialInferenceFixture(SocialPreferenceInferred)
	facts.Claim.Score = math.SmallestNonzeroFloat64
	socialInferenceBindTask(&req, &facts)
	facts.Grant.PayloadDigest = facts.Task.PayloadDigest
	// This is an explicitly uncalibrated assessment, not a truth/probability
	// threshold. Authorization comes from current evidence and human consent.
	socialInferenceRequireAllowed(t, now, req, facts)
	facts.Claim.Score = 1
	socialInferenceBindTask(&req, &facts)
	facts.Grant.PayloadDigest = facts.Task.PayloadDigest
	socialInferenceRequireAllowed(t, now, req, facts)
	socialInferenceRequireDenied(t, req.ExpiresAt, req, facts)
}
