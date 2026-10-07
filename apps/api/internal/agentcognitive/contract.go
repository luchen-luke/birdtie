// Package agentcognitive defines AGE/AIR's shared native boundary. It does not
// implement AgentProfile, Memory, ContextAssembler, a provider or an executor.
// Current-domain reads remain owned by identity/contextgraph; cognition ports
// explicitly return Unavailable until their own implementations and gates exist.
package agentcognitive

import (
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const ContractVersion = "agent-cognitive-boundary-v1"
const MaxRequestTTL = 15 * time.Minute

var (
	ErrInvalid       = errors.New("invalid cognitive boundary")
	ErrDenied        = errors.New("cognitive boundary denied")
	ErrUnavailable   = errors.New("cognitive capability unavailable")
	ErrAuthorityJSON = errors.New("cognitive authority is server-only")
)

type Availability string

const (
	Available   Availability = "AVAILABLE"
	Denied      Availability = "DENIED"
	Unavailable Availability = "UNAVAILABLE"
)

type Eligibility struct {
	Status Availability
	Reason string
}

// AgentReference preserves the existing stable agents.id and account principal
// namespaces. Model/provider configuration is deliberately absent. A reference
// is not proof of ownership, membership or a live Agent's status.
type AgentReference struct {
	AgentID   string
	Principal actorref.PrincipalRef
	Role      agentruntime.Role
}

func validID(id string) bool {
	if id != strings.TrimSpace(id) || id == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	_, err := actorref.ParsePrincipal("PERSON", id)
	return err == nil
}

func validPrincipal(ref actorref.PrincipalRef) bool {
	return (ref.Type == actorref.Person || ref.Type == actorref.Organization || ref.Type == actorref.Business) && validID(ref.ID)
}

func ValidateAgentReference(ref AgentReference) error {
	if !validID(ref.AgentID) || !validPrincipal(ref.Principal) {
		return ErrInvalid
	}
	policy := agentruntime.ForType(ref.Principal.Type)
	if policy.Role != ref.Role {
		return ErrInvalid
	}
	return nil
}

// DescribeRolePolicy reuses the runtime's existing capability pack. It is a
// role description, not permission to invoke a tool. Business is reserved and
// unavailable; Community/City/Place are not Agent principals.
func DescribeRolePolicy(ref AgentReference) (agentruntime.Policy, Eligibility) {
	if err := ValidateAgentReference(ref); err != nil {
		return agentruntime.Policy{}, Eligibility{Denied, "invalid_agent_reference"}
	}
	policy := agentruntime.ForType(ref.Principal.Type)
	if !policy.Available {
		return policy, Eligibility{Unavailable, "role_runtime_unavailable"}
	}
	return policy, Eligibility{Available, "role_description_only"}
}

type SourceType string

const (
	UserProfile    SourceType = "USER_PROFILE"
	PersonContext  SourceType = "PERSON_CONTEXT_DECLARATION"
	AgentPolicy    SourceType = "AGENT_POLICY"
	AgentProfile   SourceType = "AGENT_PROFILE"
	Memory         SourceType = "MEMORY"
	MemoryEvidence SourceType = "MEMORY_EVIDENCE"
)

// SourceReference identifies an authoritative domain version, not a snapshot
// body or arbitrary URL. Existing profile/declaration services lack versioned
// cognition views: adapters must not invent Version=1 to satisfy this type.
type SourceReference struct {
	Type    SourceType
	ID      string
	Owner   actorref.PrincipalRef
	Version int64
}

type Purpose string

const (
	ReadAgentProfile      Purpose = "READ_AGENT_PROFILE"
	ReadMemory            Purpose = "READ_MEMORY"
	SubmitMemoryCandidate Purpose = "SUBMIT_MEMORY_CANDIDATE"
	ModelContextEgress    Purpose = "MODEL_CONTEXT_EGRESS"
	AgentCoordination     Purpose = "AGENT_COORDINATION"
	ExecuteAction         Purpose = "EXECUTE_ACTION"
)

// ReadRequest is only an operation reference. Its fields and model-generated
// confirmation cannot manufacture BoundaryFacts or a domain authorization.
type ReadRequest struct {
	Version   string
	RequestID string
	TaskID    string
	Agent     AgentReference
	Purpose   Purpose
	Scope     agentruntime.Scope
	Source    SourceReference
	ExpiresAt time.Time
}

type SourceState struct {
	Verified       bool
	Authorized     bool
	Reference      SourceReference
	CurrentVersion int64
	ExpiresAt      *time.Time
	Deleted        bool
	RevokedAt      *time.Time
}

// BoundaryFacts are hypothetical trusted, current resolver outputs. No current
// resolver proves a specific agentID/source version for these new cognition
// ports. HasActiveAgent's bool cannot populate AgentVerified or source proof.
type BoundaryFacts struct {
	SessionVerified           bool
	SessionActive             bool
	ActingUser                actorref.PrincipalRef
	AgentVerified             bool
	AgentActive               bool
	PrincipalActive           bool
	ResolvedAgent             AgentReference
	MembershipVerified        bool
	MembershipActive          bool
	MembershipID              string
	MembershipActor           actorref.PrincipalRef
	MembershipPrincipal       actorref.PrincipalRef
	MembershipRevision        int64
	CurrentMembershipRevision int64
	Source                    SourceState
}

func (BoundaryFacts) MarshalJSON() ([]byte, error)  { return nil, ErrAuthorityJSON }
func (v *BoundaryFacts) UnmarshalJSON([]byte) error { *v = BoundaryFacts{}; return ErrAuthorityJSON }
func (SourceState) MarshalJSON() ([]byte, error)    { return nil, ErrAuthorityJSON }
func (v *SourceState) UnmarshalJSON([]byte) error   { *v = SourceState{}; return ErrAuthorityJSON }

func sameAgent(a, b AgentReference) bool {
	return ValidateAgentReference(a) == nil && ValidateAgentReference(b) == nil &&
		strings.EqualFold(a.AgentID, b.AgentID) && a.Principal.Equal(b.Principal) && a.Role == b.Role
}

func validSource(ref SourceReference) bool {
	if !validID(ref.ID) || !validPrincipal(ref.Owner) || ref.Version <= 0 {
		return false
	}
	switch ref.Type {
	case UserProfile, PersonContext, AgentPolicy, AgentProfile, Memory, MemoryEvidence:
		return true
	default:
		return false
	}
}

func purposeMatchesSource(purpose Purpose, source SourceType) bool {
	switch purpose {
	case ReadAgentProfile:
		return source == AgentProfile
	case ReadMemory:
		return source == Memory
	case SubmitMemoryCandidate:
		return source == MemoryEvidence
	case ModelContextEgress:
		return source == UserProfile || source == PersonContext || source == AgentPolicy || source == AgentProfile || source == Memory || source == MemoryEvidence
	case AgentCoordination, ExecuteAction:
		return source == AgentPolicy
	default:
		return false
	}
}

// DecideEligibility checks isolation/invalidation, then fails closed. Passing
// this pure contract is not a read/write grant. Phase 0 has no new cognition
// service, provider egress, live approval resolver, communication or executor.
// Future implementations must recheck sources and consent at their boundary;
// approval payload digests never replace logical-operation/effect identities.
func DecideEligibility(now time.Time, req ReadRequest, facts BoundaryFacts) Eligibility {
	deny := func(reason string) Eligibility { return Eligibility{Denied, reason} }
	if now.IsZero() || req.Version != ContractVersion || !validID(req.RequestID) || !validID(req.TaskID) ||
		ValidateAgentReference(req.Agent) != nil || !validSource(req.Source) || !purposeMatchesSource(req.Purpose, req.Source.Type) ||
		!req.ExpiresAt.After(now) || req.ExpiresAt.After(now.Add(MaxRequestTTL)) {
		return deny("invalid_request")
	}
	if !facts.SessionVerified || !facts.SessionActive || facts.ActingUser.Type != actorref.Person || !validID(facts.ActingUser.ID) ||
		!facts.AgentVerified || !facts.AgentActive || !facts.PrincipalActive || !sameAgent(req.Agent, facts.ResolvedAgent) {
		return deny("current_authority_required")
	}
	if !req.Source.Owner.Equal(req.Agent.Principal) {
		return deny("source_principal_mismatch")
	}
	switch req.Agent.Principal.Type {
	case actorref.Person:
		if !facts.ActingUser.Equal(req.Agent.Principal) || req.Scope != agentruntime.Private {
			return deny("personal_private_owner_required")
		}
	case actorref.Organization:
		if req.Scope != agentruntime.WorkspacePrivate || !facts.MembershipVerified || !facts.MembershipActive ||
			!validID(facts.MembershipID) || !facts.MembershipActor.Equal(facts.ActingUser) ||
			!facts.MembershipPrincipal.Equal(req.Agent.Principal) ||
			facts.MembershipRevision <= 0 || facts.MembershipRevision != facts.CurrentMembershipRevision ||
			req.Source.Type == UserProfile || req.Source.Type == PersonContext {
			return deny("organization_workspace_required")
		}
	case actorref.Business:
		return Eligibility{Unavailable, "business_runtime_unavailable"}
	default:
		return deny("agent_principal_unavailable")
	}
	source := facts.Source
	if !source.Verified || !source.Authorized || !validSource(source.Reference) ||
		source.Reference.Type != req.Source.Type || !strings.EqualFold(source.Reference.ID, req.Source.ID) ||
		!source.Reference.Owner.Equal(req.Source.Owner) || source.Reference.Version != req.Source.Version ||
		source.CurrentVersion != req.Source.Version || source.Deleted || source.RevokedAt != nil ||
		(source.ExpiresAt != nil && (!source.ExpiresAt.After(now) || source.ExpiresAt.Before(req.ExpiresAt))) {
		return deny("current_source_required")
	}
	return Eligibility{Unavailable, "cognitive_ports_unavailable"}
}
