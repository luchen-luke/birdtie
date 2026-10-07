package agentruntime

import (
	"reflect"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func TestAgentRoleCapabilityIsolation(t *testing.T) {
	personal := ForType(actorref.Person)
	organization := ForType(actorref.Organization)
	business := ForType(actorref.Business)
	community := ForType(actorref.Community)
	if personal.Role != PersonalAgent || !personal.Available ||
		!reflect.DeepEqual(personal.PermissionStrings(), []string{"city_context.read", "relationship_context.read"}) ||
		!personal.Allows(RelationshipContextRead) ||
		personal.Allows(OrganizationContextRead) || personal.Allows(BusinessContextRead) {
		t.Fatalf("personal pack escalated: %+v", personal)
	}
	if organization.Role != OrganizationAgent || !organization.Available ||
		!reflect.DeepEqual(organization.PermissionStrings(), []string{"city_context.read", "organization_context.read"}) ||
		organization.Allows(BusinessContextRead) || organization.Allows(RelationshipContextRead) {
		t.Fatalf("organization pack incorrect: %+v", organization)
	}
	if business.Role != BusinessAgent || business.Available || business.Allows(CityContextRead) ||
		len(business.PermissionStrings()) != 0 {
		t.Fatalf("business runtime was enabled without ownership checks: %+v", business)
	}
	if community.Available || community.Role != "" || len(community.PermissionStrings()) != 0 {
		t.Fatalf("community acquired an agent: %+v", community)
	}
	permissions := organization.PermissionStrings()
	permissions[0] = "business_context.read"
	if !reflect.DeepEqual(organization.PermissionStrings(), []string{"city_context.read", "organization_context.read"}) {
		t.Fatal("caller mutated capability policy")
	}
}
