package agentattention

import (
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
)

type BoundaryState string

const (
	BoundaryAllowed     BoundaryState = "ALLOWED"
	BoundaryDenied      BoundaryState = "DENIED"
	BoundaryUnavailable BoundaryState = "UNAVAILABLE"
)

// OfflineBoundary represents synthetic contract facts ONLY. It cannot be
// accepted by Service, decoded from JSON, or used as a notification grant.
// CurrentSource must be the exact native source version, not policy revision.
type OfflineBoundary struct {
	State                  BoundaryState
	CurrentSource          agentevent.SourceReference
	PolicyRevision         uint64
	ConsentRevision        uint64
	CurrentConsentRevision uint64
	CheckedAt              time.Time
	ExpiresAt              time.Time
	Revoked                bool
	Deleted                bool
}

func (OfflineBoundary) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (b *OfflineBoundary) UnmarshalJSON([]byte) error { *b = OfflineBoundary{}; return ErrServerOnly }

type Decision struct {
	Route          Route
	Reason         string
	PolicyRevision uint64
	EventID        string
	Mode           string
}

func blocked(reason string, policy Policy, event agentevent.Envelope) Decision {
	return Decision{Block, reason, policy.revision, event.EventID, "OFFLINE_CONTRACT"}
}

func validateRequest(policy Policy, event agentevent.Envelope, now time.Time) error {
	if !validTime(now) || !validAgent(policy.agent) || policy.revision == 0 || validateSpec(policy.spec) != nil {
		return ErrInvalid
	}
	if err := agentevent.Validate(event, now); err != nil {
		if err == agentevent.ErrExpired {
			return ErrExpired
		}
		return ErrInvalid
	}
	if event.AgentID != policy.agent.AgentID || event.Subject != policy.agent.Principal ||
		event.Tenant != policy.agent.Principal || event.Actor.Type != policy.agent.Principal.Type ||
		event.Actor.ID != policy.agent.Principal.ID || policy.revoked {
		return ErrDenied
	}
	if now.Before(policy.spec.ValidFrom) || !now.Before(policy.spec.ExpiresAt) {
		return ErrExpired
	}
	return nil
}

// EvaluateOffline executes deterministic routing with synthetic facts. An
// IMMEDIATE result is a preference classification, not delivery/permission.
// Security > explicit BLOCK > active pause SILENT > exact rule > default.
// Rule order has no effect; duplicates and unknown types are invalid.
func EvaluateOffline(policy Policy, event agentevent.Envelope, boundary OfflineBoundary, now time.Time) (Decision, error) {
	if err := validateRequest(policy, event, now); err != nil {
		return blocked("invalid_or_inactive_request", policy, event), err
	}
	if boundary.State != BoundaryAllowed {
		if boundary.State == BoundaryUnavailable {
			return blocked("authorization_unavailable", policy, event), ErrUnavailable
		}
		return blocked("authorization_denied_or_unknown", policy, event), ErrDenied
	}
	if boundary.Revoked || boundary.Deleted || boundary.ConsentRevision == 0 ||
		boundary.ConsentRevision != boundary.CurrentConsentRevision ||
		boundary.PolicyRevision != policy.revision || boundary.CurrentSource != event.Source {
		return blocked("source_or_permission_changed", policy, event), ErrDenied
	}
	if !validTime(boundary.CheckedAt) || !validTime(boundary.ExpiresAt) ||
		!boundary.CheckedAt.Equal(now) || boundary.CheckedAt.Before(event.ReceivedAt) ||
		!boundary.ExpiresAt.After(boundary.CheckedAt) || !boundary.ExpiresAt.After(now) {
		return blocked("boundary_expired_or_invalid", policy, event), ErrExpired
	}
	route, reason := policy.spec.DefaultRoute, "default_route"
	for _, rule := range policy.spec.Rules {
		if rule.EventType == event.EventType {
			route, reason = rule.Route, "event_rule"
			break
		}
	}
	// A default BLOCK applies only to unmatched events; an explicit event rule
	// is the precise user configuration. Matched BLOCK always outranks pause.
	if route == Block {
		return Decision{Block, reason, policy.revision, event.EventID, "OFFLINE_CONTRACT"}, nil
	}
	if !policy.spec.PauseUntil.IsZero() && now.Before(policy.spec.PauseUntil) {
		route, reason = Silent, "attention_paused"
	}
	return Decision{route, reason, policy.revision, event.EventID, "OFFLINE_CONTRACT"}, nil
}
