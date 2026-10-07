package agentsocialpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
)

type SourceKind string

const (
	EducationDeclaration  SourceKind = "EDUCATION_DECLARATION_PAIR"
	CommunityMembership   SourceKind = "COMMUNITY_MEMBERSHIP_PAIR"
	ActivityParticipation SourceKind = "ACTIVITY_PARTICIPATION_PAIR"
	PersonTie             SourceKind = "PERSON_TIE"
)

// SourceReference is a synthetic metadata contract, not a new native resource
// or proof of university enrollment/attendance/membership. Version is the
// fingerprint of current native row(s); there is no invented Revision=1.
type SourceReference struct {
	Kind         SourceKind
	ResourceID   string
	Owner        actorref.PrincipalRef
	Counterparty actorref.PrincipalRef
	Version      agentevent.SourceVersion
}
type Relation struct {
	Category Category
	Source   SourceReference
}

type Purpose string

const EvaluateSocialPolicy Purpose = "SOCIAL_POLICY_PREFERENCE_EVALUATION"

// Request is metadata only. Categories are claims until a native resolver
// verifies them. No university string, name, private field, message or score.
// Counterparty identifies accounts.id, never organizations.id/businesses.id.
type Request struct {
	Purpose      Purpose
	Actor        actorref.ActorRef
	Agent        agentcognitive.AgentReference
	Counterparty actorref.PrincipalRef
	OperationID  string
	Relations    []Relation
	RequestedAt  time.Time
	ExpiresAt    time.Time
}

func (Request) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (r *Request) UnmarshalJSON([]byte) error { *r = Request{}; return ErrServerOnly }

type BoundaryState string

const (
	BoundaryAllowed     BoundaryState = "ALLOWED"
	BoundaryDenied      BoundaryState = "DENIED"
	BoundaryUnavailable BoundaryState = "UNAVAILABLE"
)

type BlockState string

const (
	BlockClear   BlockState = "CLEAR"
	BlockPresent BlockState = "BLOCKED"
)

// OfflineBoundary contains synthetic test facts ONLY. Service has no input
// accepting this type, a Verified assertion, or an arbitrary fake resolver.
type OfflineBoundary struct {
	OwnerConsentPurpose                Purpose
	CounterpartyConsentPurpose         Purpose
	State                              BoundaryState
	OwnerActive                        bool
	CounterpartyActive                 bool
	OwnerBlock                         BlockState
	CounterpartyBlock                  BlockState
	CurrentPolicyRevision              uint64
	CurrentRequestDigest               string
	CurrentSources                     []SourceReference
	OwnerConsentRevision               uint64
	CurrentOwnerConsentRevision        uint64
	CounterpartyConsentRevision        uint64
	CurrentCounterpartyConsentRevision uint64
	Revoked                            bool
	SourceWithdrawn                    bool
	CheckedAt                          time.Time
	ExpiresAt                          time.Time
}

func (OfflineBoundary) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (b *OfflineBoundary) UnmarshalJSON([]byte) error { *b = OfflineBoundary{}; return ErrServerOnly }

type Decision struct {
	Preference     Preference
	Reason         string
	PolicyRevision uint64
	Mode           string
}

func disabled(reason string, p Policy) Decision {
	return Decision{Disabled, reason, p.revision, "OFFLINE_CONTRACT"}
}

func sourceKind(category Category) SourceKind {
	switch category {
	case SameUniversity:
		return EducationDeclaration
	case SharedCommunity:
		return CommunityMembership
	case SharedActivity:
		return ActivityParticipation
	case ExistingConnection:
		return PersonTie
	}
	return ""
}
func validVersion(v agentevent.SourceVersion) bool {
	if v.Kind != agentevent.UpdatedAtDigestVersion || v.Revision != 0 || len(v.Token) != 64 || strings.ToLower(v.Token) != v.Token {
		return false
	}
	b, err := hex.DecodeString(v.Token)
	return err == nil && len(b) == sha256.Size
}
func validateRequest(p Policy, r Request, now time.Time) error {
	if r.Purpose != EvaluateSocialPolicy || !validTime(now) || !validAgent(p.agent) || p.revision == 0 || validateSpec(p.spec) != nil ||
		!validAgent(r.Agent) || !validPrincipal(r.Counterparty) || !validID(r.OperationID) ||
		r.Agent != p.agent || r.Actor.Type != actorref.Person || r.Actor.ID != p.agent.Principal.ID ||
		r.Counterparty == p.agent.Principal || len(r.Relations) == 0 || len(r.Relations) > len(categories) {
		return ErrInvalid
	}
	if p.revoked {
		return ErrDenied
	}
	if now.Before(p.spec.ValidFrom) || !now.Before(p.spec.ExpiresAt) || !validTime(r.RequestedAt) || !validTime(r.ExpiresAt) ||
		r.RequestedAt.After(now) || !r.ExpiresAt.Equal(r.RequestedAt.Add(MaxRequestTTL)) || !r.ExpiresAt.After(now) {
		return ErrExpired
	}
	seen := map[Category]bool{}
	for _, relation := range r.Relations {
		c := relation.Category
		if !knownCategory(c) || seen[c] {
			return ErrInvalid
		}
		seen[c] = true
		if r.Counterparty.Type == actorref.Business {
			if c != Business || len(r.Relations) != 1 {
				return ErrInvalid
			}
		} else if r.Counterparty.Type == actorref.Organization {
			if c != Organization || len(r.Relations) != 1 {
				return ErrInvalid
			}
		} else if c == Business || c == Organization || (c == UnknownPerson && len(r.Relations) != 1) {
			return ErrInvalid
		}
		kind := sourceKind(c)
		if kind == "" {
			if relation.Source != (SourceReference{}) {
				return ErrInvalid
			}
			continue
		}
		s := relation.Source
		if s.Kind != kind || !validID(s.ResourceID) || s.Owner != p.agent.Principal || s.Counterparty != r.Counterparty || !validVersion(s.Version) {
			return ErrInvalid
		}
	}
	return nil
}

// RequestDigest binds exact metadata, both typed principals and native source
// fingerprints. It is neither source authority, an effect key nor an approval.
func RequestDigest(r Request) string {
	r.Relations = append([]Relation(nil), r.Relations...)
	sort.Slice(r.Relations, func(i, j int) bool { return r.Relations[i].Category < r.Relations[j].Category })
	r.RequestedAt, r.ExpiresAt = r.RequestedAt.UTC(), r.ExpiresAt.UTC()
	type wire Request
	data, _ := json.Marshal(wire(r))
	digest := sha256.Sum256(append([]byte("birdtie.social-policy.offline.v1\x00"), data...))
	return hex.EncodeToString(digest[:])
}

func currentSources(r Request, current []SourceReference) bool {
	var expected []SourceReference
	for _, relation := range r.Relations {
		if sourceKind(relation.Category) != "" {
			expected = append(expected, relation.Source)
		}
	}
	if len(expected) != len(current) {
		return false
	}
	used := make([]bool, len(current))
	for _, source := range expected {
		found := false
		for i, other := range current {
			if !used[i] && other == source {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// EvaluateOffline classifies local preferences with synthetic contract facts.
// It does not verify a relationship/education or grant messaging/disclosure.
// Security denial outranks preferences; any DISABLED category vetoes REVIEW.
func EvaluateOffline(p Policy, r Request, b OfflineBoundary, now time.Time) (Decision, error) {
	if err := validateRequest(p, r, now); err != nil {
		return disabled("invalid_or_inactive_request", p), err
	}
	if b.State != BoundaryAllowed {
		if b.State == BoundaryUnavailable {
			return disabled("purpose_authorization_unavailable", p), ErrUnavailable
		}
		return disabled("purpose_authorization_denied_or_unknown", p), ErrDenied
	}
	if b.OwnerConsentPurpose != EvaluateSocialPolicy || b.CounterpartyConsentPurpose != EvaluateSocialPolicy ||
		!b.OwnerActive || !b.CounterpartyActive || b.OwnerBlock != BlockClear || b.CounterpartyBlock != BlockClear ||
		b.Revoked || b.SourceWithdrawn || b.CurrentPolicyRevision != p.revision || b.CurrentRequestDigest != RequestDigest(r) ||
		b.OwnerConsentRevision == 0 || b.CounterpartyConsentRevision == 0 || b.OwnerConsentRevision != b.CurrentOwnerConsentRevision ||
		b.CounterpartyConsentRevision != b.CurrentCounterpartyConsentRevision || !currentSources(r, b.CurrentSources) {
		return disabled("current_identity_source_or_permission_denied", p), ErrDenied
	}
	if !validTime(b.CheckedAt) || !validTime(b.ExpiresAt) || !b.CheckedAt.Equal(now) || b.CheckedAt.Before(r.RequestedAt) ||
		!b.ExpiresAt.After(b.CheckedAt) || !b.ExpiresAt.After(now) {
		return disabled("boundary_expired_or_stale", p), ErrExpired
	}
	if r.Counterparty.Type == actorref.Business {
		return disabled("business_runtime_unavailable", p), ErrUnavailable
	}
	for _, relation := range r.Relations {
		preference, _ := p.Preference(relation.Category)
		if preference == Disabled {
			return disabled("category_disabled", p), nil
		}
	}
	return Decision{ReviewRequired, "review_preference_only", p.revision, "OFFLINE_CONTRACT"}, nil
}
