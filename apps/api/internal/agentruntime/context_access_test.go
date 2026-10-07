package agentruntime

import (
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func TestAgentContextAccessRedTeamFixtures(t *testing.T) {
	owner := "11111111-1111-4111-8111-111111111111"
	viewer := "22222222-2222-4222-8222-222222222222"
	org := "33333333-3333-4333-8333-333333333333"
	personal := ForType(actorref.Person)
	organization := ForType(actorref.Organization)
	base := AccessRequest{Scope: Public, OwnerID: owner, ViewerID: viewer,
		PrincipalID: viewer, AuthorityVerified: true, Released: true}
	cases := []struct {
		name   string
		policy Policy
		input  AccessRequest
		want   bool
	}{
		{"published public", personal, base, true},
		{"draft public", personal, modify(base, func(a *AccessRequest) { a.Released = false }), false},
		{"blocked public", personal, modify(base, func(a *AccessRequest) { a.Blocked = true }), false},
		{"forged personal principal", personal, modify(base, func(a *AccessRequest) { a.PrincipalID = owner }), false},
		{"unverified authority", personal, modify(base, func(a *AccessRequest) { a.AuthorityVerified = false }), false},
		{"owner private memory", personal, AccessRequest{Scope: Private, OwnerID: owner,
			ViewerID: owner, PrincipalID: owner, AuthorityVerified: true}, true},
		{"other Personal Agent private memory", personal, modify(base, func(a *AccessRequest) {
			a.Scope = Private
			a.ExplicitGrant = true
		}), false},
		{"organization admin private memory", organization, AccessRequest{Scope: Private,
			OwnerID: owner, ViewerID: owner, PrincipalID: org, AuthorityVerified: true,
			ExplicitGrant: true}, false},
		{"organization reads published public", organization, modify(base, func(a *AccessRequest) {
			a.PrincipalID = org
		}), true},
		{"organization member tie does not transfer", organization, modify(base, func(a *AccessRequest) {
			a.Scope = Connection
			a.PrincipalID = org
			a.Relationship = VerifiedClose
		}), false},
		{"accepted chat alone is not a Tie", personal, modify(base, func(a *AccessRequest) {
			a.Scope = Connection
			a.Relationship = NoRelationship
		}), false},
		{"verified connection released", personal, modify(base, func(a *AccessRequest) {
			a.Scope = Connection
			a.Relationship = VerifiedConnection
		}), true},
		{"relationship needs real owner", personal, modify(base, func(a *AccessRequest) {
			a.Scope = Connection
			a.Relationship = VerifiedConnection
			a.OwnerID = ""
		}), false},
		{"close without resource consent", personal, modify(base, func(a *AccessRequest) {
			a.Scope = Close
			a.Relationship = VerifiedClose
		}), false},
		{"close with resource consent", personal, modify(base, func(a *AccessRequest) {
			a.Scope = Close
			a.Relationship = VerifiedClose
			a.ExplicitGrant = true
		}), true},
		{"organization own workspace history", organization, AccessRequest{Scope: WorkspacePrivate,
			OwnerID: org, ViewerID: viewer, PrincipalID: org, AuthorityVerified: true}, true},
		{"organization reads person workspace history", organization, AccessRequest{Scope: WorkspacePrivate,
			OwnerID: owner, ViewerID: owner, PrincipalID: org, AuthorityVerified: true}, false},
		{"business role remains disabled", ForType(actorref.Business), base, false},
		{"unknown scope", personal, modify(base, func(a *AccessRequest) { a.Scope = "EVERYONE" }), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.DecideContext(tc.input); got.Allowed != tc.want {
				t.Fatalf("decision=%+v want allowed=%v", got, tc.want)
			}
		})
	}
}

func modify(input AccessRequest, edit func(*AccessRequest)) AccessRequest {
	edit(&input)
	return input
}
