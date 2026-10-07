// Package agentnotification describes explicit human notification preferences.
// Preference classification never authorizes a source, analysis, model, Memory,
// A2A, delivery or a Business Agent. Native stores check current source ACL first.
package agentnotification

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentattention"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const (
	SchemaVersion            = "agent-notification-policy-v1"
	MaxBodyBytes             = 4096
	MaxRules                 = 8
	MaxVersion        uint64 = math.MaxInt64
	MaxPolicyDuration        = 30 * 24 * time.Hour
)

type Category string

const (
	CategoryMessage      Category = "MESSAGE"
	CategoryActivity     Category = "ACTIVITY"
	CategoryCommunity    Category = "COMMUNITY"
	CategoryOrganization Category = "ORGANIZATION"
	CategoryBusiness     Category = "BUSINESS"
	CategorySystem       Category = "SYSTEM"
	CategoryAgent        Category = "AGENT"
	CategorySocial       Category = "SOCIAL"
)

type Route = agentattention.Route

const (
	Immediate = agentattention.Immediate
	Normal    = agentattention.Normal
	Digest    = agentattention.Digest
	Silent    = agentattention.Silent
	Block     = agentattention.Block
)

var (
	ErrInvalid     = errors.New("通知偏好无效")
	ErrForbidden   = errors.New("当前无权管理此通知偏好")
	ErrNotFound    = errors.New("通知偏好主体不存在")
	ErrConflict    = errors.New("通知偏好已更新，请重新读取")
	ErrUnavailable = errors.New("通知偏好暂不可用")
	ErrServerOnly  = errors.New("通知来源控制仅供服务端使用")
)

type Rule struct {
	Category Category `json:"category"`
	Route    Route    `json:"route"`
}

// Policy is a human self-management response, not a processing capability. V0
// means no configuration. Disabled restores ordinary human NORMAL delivery;
// SILENT or BLOCK are the explicit ways to suppress ordinary notifications.
type Policy struct {
	SchemaVersion string     `json:"schemaVersion"`
	Version       uint64     `json:"version"`
	AgentID       string     `json:"agentId"`
	Enabled       bool       `json:"enabled"`
	DefaultRoute  Route      `json:"defaultRoute"`
	Rules         []Rule     `json:"rules"`
	PauseUntil    *time.Time `json:"pauseUntil,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
}

type PutInput struct {
	ExpectedVersion uint64     `json:"expectedVersion"`
	Enabled         bool       `json:"enabled"`
	DefaultRoute    Route      `json:"defaultRoute"`
	Rules           []Rule     `json:"rules"`
	PauseUntil      *time.Time `json:"pauseUntil,omitempty"`
	ExpiresAt       time.Time  `json:"expiresAt"`
}

type Store interface {
	GetOwnNotificationPolicy(context.Context, agentprofile.PrivateAccess) (Policy, error)
	PutOwnNotificationPolicy(context.Context, agentprofile.PrivateAccess, PutInput) (Policy, error)
}

func ValidCategory(category Category) bool {
	switch category {
	case CategoryMessage, CategoryActivity, CategoryCommunity, CategoryOrganization, CategoryBusiness, CategorySystem, CategoryAgent, CategorySocial:
		return true
	default:
		return false
	}
}

// RoutePriority is an ordering weight, never a probability or grant.
func RoutePriority(route Route) (int, error) {
	switch route {
	case Immediate:
		return 100, nil
	case Normal:
		return 50, nil
	case Digest:
		return 10, nil
	case Silent, Block:
		return 0, nil
	default:
		return 0, ErrInvalid
	}
}

func validTime(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}

func validID(id string) bool {
	p, err := actorref.ParsePrincipal("PERSON", id)
	return err == nil && p.ID == id && id != "00000000-0000-0000-0000-000000000000"
}

func NormalizeRules(rules []Rule) ([]Rule, error) {
	if len(rules) > MaxRules {
		return nil, ErrInvalid
	}
	out := make([]Rule, 0, len(rules))
	seen := map[Category]bool{}
	for _, rule := range rules {
		if !ValidCategory(rule.Category) || seen[rule.Category] {
			return nil, ErrInvalid
		}
		if _, err := RoutePriority(rule.Route); err != nil {
			return nil, ErrInvalid
		}
		seen[rule.Category] = true
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Category < out[j].Category })
	return out, nil
}

// NormalizePutInput must receive the store's current database clock, refreshed
// after locks. This validator alone does not authenticate any person or source.
func NormalizePutInput(input PutInput, now time.Time) (PutInput, error) {
	if input.ExpectedVersion > MaxVersion || !validTime(now) || !validTime(input.ExpiresAt) {
		return PutInput{}, ErrInvalid
	}
	if _, err := RoutePriority(input.DefaultRoute); err != nil {
		return PutInput{}, ErrInvalid
	}
	rules, err := NormalizeRules(input.Rules)
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	expires := input.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !expires.After(now) || expires.After(now.Add(MaxPolicyDuration)) {
		return PutInput{}, ErrInvalid
	}
	out := input
	out.Rules = rules
	out.ExpiresAt = expires
	if input.PauseUntil != nil {
		if !validTime(*input.PauseUntil) {
			return PutInput{}, ErrInvalid
		}
		pause := input.PauseUntil.UTC().Truncate(time.Microsecond)
		if pause.After(expires) {
			return PutInput{}, ErrInvalid
		}
		out.PauseUntil = &pause
	}
	return out, nil
}

func DefaultPolicy(agentID string) (Policy, error) {
	policy := Policy{SchemaVersion: SchemaVersion, AgentID: agentID, DefaultRoute: Normal, Rules: []Rule{}}
	if ValidatePolicy(policy) != nil {
		return Policy{}, ErrInvalid
	}
	return policy, nil
}

// ValidatePolicy checks response shape; AgentID ownership is resolved by the
// real store. Existing expired records remain readable for owner review.
func ValidatePolicy(policy Policy) error {
	if policy.SchemaVersion != SchemaVersion || !validID(policy.AgentID) || policy.Version > MaxVersion || policy.Rules == nil {
		return ErrInvalid
	}
	if _, err := RoutePriority(policy.DefaultRoute); err != nil {
		return ErrInvalid
	}
	rules, err := NormalizeRules(policy.Rules)
	if err != nil || !reflect.DeepEqual(rules, policy.Rules) {
		return ErrInvalid
	}
	if policy.Version == 0 {
		if policy.Enabled || policy.DefaultRoute != Normal || len(policy.Rules) != 0 || policy.PauseUntil != nil || policy.ExpiresAt != nil || policy.UpdatedAt != nil {
			return ErrInvalid
		}
		return nil
	}
	if policy.ExpiresAt == nil || policy.UpdatedAt == nil || !validTime(*policy.ExpiresAt) || !validTime(*policy.UpdatedAt) || !policy.ExpiresAt.After(*policy.UpdatedAt) || policy.ExpiresAt.After(policy.UpdatedAt.Add(MaxPolicyDuration)) {
		return ErrInvalid
	}
	if policy.PauseUntil != nil && (!validTime(*policy.PauseUntil) || policy.PauseUntil.After(*policy.ExpiresAt)) {
		return ErrInvalid
	}
	return nil
}

type Decision struct {
	Route    Route
	Priority int
	Reason   string
}

// ChooseRoute is deterministic preference classification only. The native
// transaction must independently resolve source ACL, recipient, current policy
// version and clock. Even IMMEDIATE does not send, grant, analyze or execute.
func ChooseRoute(policy Policy, category Category, now time.Time) (Decision, error) {
	if ValidatePolicy(policy) != nil || !ValidCategory(category) || !validTime(now) {
		return Decision{}, ErrInvalid
	}
	if policy.Version != 0 && now.Before(*policy.UpdatedAt) {
		return Decision{}, ErrInvalid
	}
	route, reason := Normal, "not_configured"
	if policy.Version != 0 {
		switch {
		case !policy.Enabled:
			reason = "disabled"
		case !now.Before(*policy.ExpiresAt):
			reason = "expired"
		default:
			route, reason = policy.DefaultRoute, "default_rule"
			for _, rule := range policy.Rules {
				if rule.Category == category {
					route, reason = rule.Route, "exact_rule"
					break
				}
			}
			if route != Block && policy.PauseUntil != nil && now.Before(*policy.PauseUntil) {
				route, reason = Silent, "attention_paused"
			}
		}
	}
	priority, _ := RoutePriority(route)
	return Decision{Route: route, Priority: priority, Reason: reason}, nil
}
