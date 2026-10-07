package agentruntime

// Scope describes the owner's intended audience for one context resource.
// WorkspacePrivate is for an organization's own Agent task history; it never
// grants access to a member's Personal Agent memory.
type Scope string

const (
	Public           Scope = "PUBLIC"
	Connection       Scope = "CONNECTION"
	Close            Scope = "CLOSE"
	Private          Scope = "PRIVATE"
	WorkspacePrivate Scope = "WORKSPACE_PRIVATE"
)

type Relationship string

const (
	NoRelationship     Relationship = "NONE"
	VerifiedConnection Relationship = "CONNECTION"
	VerifiedClose      Relationship = "CLOSE"
)

// AccessRequest must be built from server-resolved session, membership, block,
// grant and relationship records. Client JSON and generated Agent text are not
// authority evidence. A profile-view grant is not a private-memory grant.
type AccessRequest struct {
	Scope             Scope
	OwnerID           string // Person account ID, or organization account ID for workspace history.
	ViewerID          string // Signed-in Person account ID; empty only for public guest reads.
	PrincipalID       string // Active Agent's account principal ID; empty for public guest reads.
	AuthorityVerified bool
	Released          bool
	Blocked           bool
	Relationship      Relationship
	ExplicitGrant     bool // Resource-specific consent for close-scope data, not a profile-view grant.
}

type AccessDecision struct {
	Allowed bool
	Reason  string
}

func allow() AccessDecision             { return AccessDecision{Allowed: true, Reason: "allowed"} }
func deny(reason string) AccessDecision { return AccessDecision{Reason: reason} }

func (p Policy) DecideContext(request AccessRequest) AccessDecision {
	if !p.Available || !request.AuthorityVerified {
		return deny("agent_authority_required")
	}
	if p.Role == PersonalAgent {
		if request.ViewerID != request.PrincipalID {
			return deny("personal_principal_mismatch")
		}
	} else if p.Role == OrganizationAgent {
		if request.ViewerID == "" || request.PrincipalID == "" {
			return deny("organization_principal_required")
		}
	} else {
		return deny("agent_role_unavailable")
	}
	self := p.Role == PersonalAgent && request.OwnerID != "" &&
		request.OwnerID == request.ViewerID && request.PrincipalID == request.OwnerID
	if request.Blocked && !self {
		return deny("blocked")
	}
	switch request.Scope {
	case Public:
		if request.Released {
			return allow()
		}
		return deny("not_published")
	case WorkspacePrivate:
		if request.OwnerID != "" && request.OwnerID == request.PrincipalID &&
			((p.Role == PersonalAgent && self) || p.Role == OrganizationAgent) {
			return allow()
		}
		return deny("workspace_owner_required")
	case Private:
		if self {
			return allow()
		}
		return deny("private_memory_owner_required")
	case Connection:
		if request.OwnerID == "" || request.ViewerID == "" {
			return deny("relationship_subject_required")
		}
		if self || (p.Role == PersonalAgent && request.Released &&
			(request.Relationship == VerifiedConnection || request.Relationship == VerifiedClose)) {
			return allow()
		}
		return deny("verified_connection_required")
	case Close:
		if request.OwnerID == "" || request.ViewerID == "" {
			return deny("relationship_subject_required")
		}
		if self || (p.Role == PersonalAgent && request.Released &&
			request.Relationship == VerifiedClose && request.ExplicitGrant) {
			return allow()
		}
		return deny("verified_close_grant_required")
	default:
		return deny("unknown_scope")
	}
}
