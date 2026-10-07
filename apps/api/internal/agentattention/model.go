// Package agentattention defines deterministic Attention Policy foundation.
// Offline decisions are contracts, never permission to notify or analyze.
package agentattention

import (
	"errors"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const SchemaVersion = "agent-attention-policy-v1"

type Route string

const (
	Immediate Route = "IMMEDIATE"
	Normal    Route = "NORMAL"
	Digest    Route = "DIGEST"
	Silent    Route = "SILENT"
	Block     Route = "BLOCK"
)

var (
	ErrInvalid     = errors.New("注意力策略无效")
	ErrDenied      = errors.New("当前无权使用注意力策略")
	ErrExpired     = errors.New("注意力策略或来源已过期")
	ErrConflict    = errors.New("注意力策略已更新，请重新读取")
	ErrUnavailable = errors.New("注意力策略授权服务当前不可用")
	ErrServerOnly  = errors.New("注意力策略控制对象仅供服务端使用")
)

func validRoute(route Route) bool {
	switch route {
	case Immediate, Normal, Digest, Silent, Block:
		return true
	default:
		return false
	}
}

// Rule matches one registered event exactly. There is no wildcard, free text,
// category inference, payload inspection or model score.
type Rule struct {
	EventType agentevent.Type
	Route     Route
}

// Specification is trusted server configuration, not an HTTP editing API.
// PauseUntil is an absolute half-open pause, not a timezone/recurring schedule.
type Specification struct {
	DefaultRoute Route
	Rules        []Rule
	ValidFrom    time.Time
	ExpiresAt    time.Time
	PauseUntil   time.Time
}

func (Specification) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (s *Specification) UnmarshalJSON([]byte) error {
	*s = Specification{}
	return ErrServerOnly
}

// Policy is an immutable process-local snapshot with an independent revision.
// Its revision does not describe source, consent, Profile or feature flags.
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

func cloneSpec(spec Specification) Specification {
	spec.Rules = append([]Rule(nil), spec.Rules...)
	return spec
}

func validTime(t time.Time) bool {
	return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999
}

func validateSpec(spec Specification) error {
	if !validRoute(spec.DefaultRoute) || !validTime(spec.ValidFrom) ||
		!validTime(spec.ExpiresAt) || !spec.ExpiresAt.After(spec.ValidFrom) ||
		len(spec.Rules) > len(agentevent.Catalog()) {
		return ErrInvalid
	}
	if !spec.PauseUntil.IsZero() && (!validTime(spec.PauseUntil) ||
		spec.PauseUntil.Before(spec.ValidFrom) || spec.PauseUntil.After(spec.ExpiresAt)) {
		return ErrInvalid
	}
	seen := make(map[agentevent.Type]bool, len(spec.Rules))
	for _, rule := range spec.Rules {
		_, known := agentevent.Lookup(rule.EventType)
		if !known || !validRoute(rule.Route) || seen[rule.EventType] {
			return ErrInvalid
		}
		seen[rule.EventType] = true
	}
	return nil
}

func validAgent(agent agentcognitive.AgentReference) bool {
	if agentcognitive.ValidateAgentReference(agent) != nil || agent.Role != agentruntime.PersonalAgent ||
		agent.Principal.Type != actorref.Person {
		return false
	}
	principal, err := actorref.ParsePrincipal(string(agent.Principal.Type), agent.Principal.ID)
	ref, idErr := actorref.ParsePrincipal("PERSON", agent.AgentID)
	return err == nil && idErr == nil && principal == agent.Principal && ref.ID == agent.AgentID
}

// Store controls one exact Personal Agent's policy in memory. It is a backend
// primitive, not a persistent policy service or an ownership/session resolver.
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
	if !spec.PauseUntil.IsZero() {
		spec.PauseUntil = spec.PauseUntil.UTC()
	}
	return &Store{policy: Policy{agent: agent, revision: 1, spec: spec}}, nil
}

func (s *Store) Snapshot() (Policy, error) {
	if s == nil {
		return Policy{}, ErrUnavailable
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	copy := s.policy
	copy.spec = cloneSpec(copy.spec)
	return copy, nil
}

// Replace CAS advances only this policy's revision. Replacing revoked settings
// expresses a server configuration change; it grants no runtime authorization.
func (s *Store) Replace(expectedRevision uint64, spec Specification) error {
	if s == nil {
		return ErrUnavailable
	}
	if validateSpec(spec) != nil {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision == 0 || expectedRevision != s.policy.revision || s.policy.revision == ^uint64(0) {
		return ErrConflict
	}
	spec = cloneSpec(spec)
	spec.ValidFrom, spec.ExpiresAt = spec.ValidFrom.UTC(), spec.ExpiresAt.UTC()
	if !spec.PauseUntil.IsZero() {
		spec.PauseUntil = spec.PauseUntil.UTC()
	}
	s.policy.spec, s.policy.revoked = spec, false
	s.policy.revision++
	return nil
}

func (s *Store) Revoke(expectedRevision uint64) error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision == 0 || expectedRevision != s.policy.revision || s.policy.revision == ^uint64(0) {
		return ErrConflict
	}
	s.policy.revoked = true
	s.policy.revision++
	return nil
}

func (s *Store) current(policy Policy) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policy.agent == policy.agent && s.policy.revision == policy.revision && !s.policy.revoked
}
