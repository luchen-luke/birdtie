// Package agentsocialpolicy implements local SocialInteractionPolicy foundation.
// Preferences restrict future suggestions; they never authorize access/actions.
package agentsocialpolicy

import (
	"errors"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const SchemaVersion = "agent-social-interaction-policy-v1"
const MaxRequestTTL = 15 * time.Minute

type Category string

const (
	SameUniversity     Category = "SAME_UNIVERSITY"
	SharedCommunity    Category = "SHARED_COMMUNITY"
	SharedActivity     Category = "SHARED_ACTIVITY"
	ExistingConnection Category = "EXISTING_CONNECTION"
	UnknownPerson      Category = "UNKNOWN_PERSON"
	Business           Category = "BUSINESS"
	Organization       Category = "ORGANIZATION"
)

var categories = [...]Category{SameUniversity, SharedCommunity, SharedActivity, ExistingConnection, UnknownPerson, Business, Organization}

func Categories() []Category { return append([]Category(nil), categories[:]...) }
func knownCategory(c Category) bool {
	for _, known := range categories {
		if c == known {
			return true
		}
	}
	return false
}

type Preference string

const (
	Disabled       Preference = "DISABLED"
	ReviewRequired Preference = "REVIEW_REQUIRED"
)

var (
	ErrInvalid     = errors.New("社交互动策略无效")
	ErrDenied      = errors.New("当前无权使用社交互动策略")
	ErrExpired     = errors.New("社交互动策略或来源已过期")
	ErrConflict    = errors.New("社交互动策略已更新，请重新读取")
	ErrUnavailable = errors.New("社交互动策略授权服务当前不可用")
	ErrServerOnly  = errors.New("社交互动策略控制对象仅供服务端使用")
)

// Rule is a preference, not AGE042 message-request ALLOW/REQUEST/SCREEN/BLOCK.
type Rule struct {
	Category   Category
	Preference Preference
}
type Specification struct {
	Rules     []Rule
	ValidFrom time.Time
	ExpiresAt time.Time
}

func (Specification) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (s *Specification) UnmarshalJSON([]byte) error { *s = Specification{}; return ErrServerOnly }
func validTime(t time.Time) bool                    { return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 }
func cloneSpec(s Specification) Specification       { s.Rules = append([]Rule(nil), s.Rules...); return s }
func validateSpec(s Specification) error {
	if !validTime(s.ValidFrom) || !validTime(s.ExpiresAt) || !s.ExpiresAt.After(s.ValidFrom) || len(s.Rules) > len(categories) {
		return ErrInvalid
	}
	seen := map[Category]bool{}
	for _, r := range s.Rules {
		if !knownCategory(r.Category) || seen[r.Category] || (r.Preference != Disabled && r.Preference != ReviewRequired) {
			return ErrInvalid
		}
		seen[r.Category] = true
	}
	return nil
}
func validID(id string) bool {
	ref, err := actorref.ParsePrincipal("PERSON", id)
	return err == nil && ref.ID == id && id != "00000000-0000-0000-0000-000000000000"
}
func validPrincipal(p actorref.PrincipalRef) bool {
	if p.Type != actorref.Person && p.Type != actorref.Organization && p.Type != actorref.Business {
		return false
	}
	ref, err := actorref.ParsePrincipal(string(p.Type), p.ID)
	return err == nil && ref == p && validID(p.ID)
}
func validAgent(a agentcognitive.AgentReference) bool {
	return agentcognitive.ValidateAgentReference(a) == nil && validID(a.AgentID) && validPrincipal(a.Principal) && a.Principal.Type == actorref.Person && a.Role == agentruntime.PersonalAgent
}

// Policy is an immutable snapshot. Its revision is neither a Profile/source
// revision, feature epoch, independent consent nor an action approval.
type Policy struct {
	agent    agentcognitive.AgentReference
	revision uint64
	spec     Specification
	revoked  bool
}

func (Policy) MarshalJSON() ([]byte, error)           { return nil, ErrServerOnly }
func (p *Policy) UnmarshalJSON([]byte) error          { *p = Policy{}; return ErrServerOnly }
func (p Policy) Agent() agentcognitive.AgentReference { return p.agent }
func (p Policy) Revision() uint64                     { return p.revision }
func (p Policy) Revoked() bool                        { return p.revoked }
func (p Policy) Specification() Specification         { return cloneSpec(p.spec) }
func (p Policy) Preference(category Category) (Preference, error) {
	if !knownCategory(category) {
		return Disabled, ErrInvalid
	}
	for _, r := range p.spec.Rules {
		if r.Category == category {
			return r.Preference, nil
		}
	}
	return Disabled, nil
}

// Store owns one Personal Agent's process-local settings only. It has no HTTP,
// DB persistence, session/ownership resolver or activation mechanism.
type Store struct {
	mu     sync.RWMutex
	policy Policy
}

func NewStore(agent agentcognitive.AgentReference, spec Specification) (*Store, error) {
	if !validAgent(agent) || validateSpec(spec) != nil {
		return nil, ErrInvalid
	}
	spec = cloneSpec(spec)
	spec.ValidFrom, spec.ExpiresAt = spec.ValidFrom.UTC(), spec.ExpiresAt.UTC()
	return &Store{policy: Policy{agent: agent, revision: 1, spec: spec}}, nil
}
func (s *Store) Snapshot() (Policy, error) {
	if s == nil {
		return Policy{}, ErrUnavailable
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.policy
	p.spec = cloneSpec(p.spec)
	return p, nil
}
func (s *Store) Replace(expected uint64, spec Specification) error {
	if s == nil {
		return ErrUnavailable
	}
	if validateSpec(spec) != nil {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected == 0 || expected != s.policy.revision || s.policy.revision == ^uint64(0) {
		return ErrConflict
	}
	spec = cloneSpec(spec)
	spec.ValidFrom, spec.ExpiresAt = spec.ValidFrom.UTC(), spec.ExpiresAt.UTC()
	s.policy.spec, s.policy.revoked = spec, false
	s.policy.revision++
	return nil
}
func (s *Store) Revoke(expected uint64) error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if expected == 0 || expected != s.policy.revision || s.policy.revision == ^uint64(0) {
		return ErrConflict
	}
	s.policy.revoked = true
	s.policy.revision++
	return nil
}
func (s *Store) current(p Policy) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy.agent == p.agent && s.policy.revision == p.revision && !s.policy.revoked
}
