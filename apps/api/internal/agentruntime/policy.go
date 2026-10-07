// Package agentruntime defines role-specific capability packs for the shared
// Agent request path. A pack is not an authorization decision: handlers must
// first resolve the session and workspace against live server-side records.
package agentruntime

import "github.com/birdtie/birdtie/apps/api/internal/actorref"

type Role string

const (
	PersonalAgent     Role = "PERSONAL"
	OrganizationAgent Role = "ORGANIZATION"
	BusinessAgent     Role = "BUSINESS"
)

type Capability string

const (
	CityContextRead         Capability = "city_context.read"
	OrganizationContextRead Capability = "organization_context.read"
	BusinessContextRead     Capability = "business_context.read"
	RelationshipContextRead Capability = "relationship_context.read"
)

type Policy struct {
	Role         Role
	ActorType    actorref.Type
	Available    bool
	capabilities []Capability
}

// ForType describes what the shared runtime may expose after the caller's
// authority has been checked. Business is reserved until claim and membership
// checks exist; Community has no Agent. Neither can be invoked today.
func ForType(kind actorref.Type) Policy {
	switch kind {
	case actorref.Person:
		return Policy{Role: PersonalAgent, ActorType: kind, Available: true,
			capabilities: []Capability{CityContextRead, RelationshipContextRead}}
	case actorref.Organization:
		return Policy{Role: OrganizationAgent, ActorType: kind, Available: true,
			capabilities: []Capability{CityContextRead, OrganizationContextRead}}
	case actorref.Business:
		return Policy{Role: BusinessAgent, ActorType: kind}
	default:
		return Policy{}
	}
}

func (p Policy) Allows(capability Capability) bool {
	if !p.Available {
		return false
	}
	for _, allowed := range p.capabilities {
		if allowed == capability {
			return true
		}
	}
	return false
}

func (p Policy) PermissionStrings() []string {
	if !p.Available {
		return nil
	}
	out := make([]string, len(p.capabilities))
	for i, capability := range p.capabilities {
		out[i] = string(capability)
	}
	return out
}
