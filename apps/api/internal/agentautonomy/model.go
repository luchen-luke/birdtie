package agentautonomy

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const SchemaVersion = "agent-autonomy-limits-v1"
const MaxSettingsTTL = 30 * 24 * time.Hour
const MaxRequestTTL = 15 * time.Minute
const MaxSources = 16

var (
	ErrInvalid     = errors.New("自主性设置或请求无效")
	ErrDenied      = errors.New("当前主体或自主性范围不允许此请求")
	ErrExpired     = errors.New("自主性设置、请求或来源已过期")
	ErrConflict    = errors.New("自主性设置已更新，请重新读取")
	ErrUnavailable = errors.New("当前来源、用途或人类批准服务不可用")
	ErrServerOnly  = errors.New("自主性控制对象仅供服务端使用")
)

func validTime(at time.Time) bool {
	return !at.IsZero() && at.UTC().Year() >= 1 && at.UTC().Year() <= 9999
}
func validID(id string) bool {
	ref, err := actorref.ParsePrincipal("PERSON", id)
	return err == nil && ref.ID == id && id == strings.ToLower(id) && id != "00000000-0000-0000-0000-000000000000"
}
func validAgent(agent agentcognitive.AgentReference) bool {
	return agentcognitive.ValidateAgentReference(agent) == nil && agent.Principal.Type == actorref.Person &&
		agent.Role == agentruntime.PersonalAgent && validID(agent.AgentID) && validID(agent.Principal.ID)
}

// Specification is trusted process configuration, not a self-profile edit or
// ownership/session proof. Even LevelObserve grants no private-source access.
type Specification struct {
	Level     Level
	ValidFrom time.Time
	ExpiresAt time.Time
}

func (Specification) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (s *Specification) UnmarshalJSON([]byte) error {
	if s != nil {
		*s = Specification{}
	}
	return ErrServerOnly
}
func validateSpecification(spec Specification, now time.Time) error {
	if _, ok := levelRank(spec.Level); !ok || !validTime(now) || !validTime(spec.ValidFrom) || !validTime(spec.ExpiresAt) ||
		spec.ValidFrom.After(now) || !spec.ExpiresAt.After(spec.ValidFrom) || !spec.ExpiresAt.After(now) ||
		spec.ExpiresAt.Sub(spec.ValidFrom) > MaxSettingsTTL {
		return ErrInvalid
	}
	if spec.Level == LevelDelegate && !agentfeature.Limits().AutonomousAction {
		return ErrUnavailable
	}
	return nil
}

// Snapshot's revision is only this process setting version, never a source
// revision, feature epoch, grant or concrete draft approval. The origin binds
// it to one Store instance, including when two stores name the same Agent.
type Snapshot struct {
	origin   *Store
	agent    agentcognitive.AgentReference
	revision uint64
	spec     Specification
	revoked  bool
}

func (Snapshot) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (s *Snapshot) UnmarshalJSON([]byte) error {
	if s != nil {
		*s = Snapshot{}
	}
	return ErrServerOnly
}
func (s Snapshot) Agent() agentcognitive.AgentReference { return s.agent }
func (s Snapshot) Revision() uint64                     { return s.revision }
func (s Snapshot) Specification() Specification         { return s.spec }
func (s Snapshot) Revoked() bool                        { return s.revoked }

// Store holds one exact Personal Agent's finite settings in memory. It has no
// persistence, HTTP, subject resolver or executor. Constructor now is trusted
// server input, not a current session proof. Restart does not restore settings.
type Store struct {
	mu      sync.RWMutex
	current Snapshot
}

func (*Store) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (*Store) UnmarshalJSON([]byte) error   { return ErrServerOnly }
func NewStore(agent agentcognitive.AgentReference, now time.Time) (*Store, error) {
	if !validAgent(agent) || !validTime(now) {
		return nil, ErrInvalid
	}
	now = now.UTC()
	spec := Specification{LevelObserve, now, now.Add(MaxSettingsTTL)}
	if err := validateSpecification(spec, now); err != nil {
		return nil, err
	}
	store := &Store{}
	store.current = Snapshot{origin: store, agent: agent, revision: 1, spec: spec}
	return store, nil
}
func (s *Store) Snapshot() (Snapshot, error) {
	if s == nil {
		return Snapshot{}, ErrUnavailable
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current, nil
}
func (s *Store) Current(snapshot Snapshot, now time.Time) bool {
	if s == nil || !validTime(now) {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return snapshot.origin == s && snapshot.agent == s.current.agent && snapshot.revision != 0 &&
		snapshot.revision == s.current.revision && snapshot.spec == s.current.spec && !snapshot.revoked && !s.current.revoked &&
		!now.Before(s.current.spec.ValidFrom) && now.Before(s.current.spec.ExpiresAt)
}
func (s *Store) Replace(expectedRevision uint64, spec Specification, now time.Time) error {
	if s == nil {
		return ErrUnavailable
	}
	if err := validateSpecification(spec, now); err != nil {
		return err
	}
	spec.ValidFrom, spec.ExpiresAt = spec.ValidFrom.UTC(), spec.ExpiresAt.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision == 0 || expectedRevision != s.current.revision || s.current.revision == ^uint64(0) {
		return ErrConflict
	}
	s.current.spec, s.current.revoked = spec, false
	s.current.revision++
	return nil
}
func (s *Store) Revoke(expectedRevision uint64) error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if expectedRevision == 0 || expectedRevision != s.current.revision || s.current.revision == ^uint64(0) {
		return ErrConflict
	}
	s.current.revoked = true
	s.current.revision++
	return nil
}
