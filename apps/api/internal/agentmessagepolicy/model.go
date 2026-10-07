// Package agentmessagepolicy describes restrictive human contact routing.
// Its Decisions, settings and stored metadata never authorize Agent actions.
package agentmessagepolicy

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"time"
)

const Schema = "agent-message-request-policy-v1"
const MaxBodyBytes = 4096
const Authority = "ROUTING_ONLY_NO_AGENT_PERMISSION"

type Disposition string

const (
	Allow   Disposition = "ALLOW"
	Request Disposition = "REQUEST"
	Screen  Disposition = "SCREEN"
	Block   Disposition = "BLOCK"
)

var (
	ErrInvalid     = errors.New("消息请求策略内容无效")
	ErrDenied      = errors.New("当前无权使用该消息请求策略")
	ErrChanged     = errors.New("消息请求条件已变化，请重新查看")
	ErrUnavailable = errors.New("消息请求策略暂不可用")
)

func ValidIncoming(d Disposition) bool { return d == Request || d == Screen || d == Block }

// RoutingFacts are supplied only by the current native domain transaction.
// This reducer is not an authentication or authorization API.
type RoutingFacts struct {
	Blocked, AcceptedTie, PublicPerson, Configured, CurrentBinding bool
	Incoming                                                       Disposition
	ValidFrom, ExpiresAt                                           time.Time
}

func Route(f RoutingFacts, now time.Time) Disposition {
	if now.IsZero() || f.Blocked {
		return Block
	}
	if f.AcceptedTie {
		return Allow
	}
	if !f.PublicPerson || !f.CurrentBinding {
		return Block
	}
	if !f.Configured {
		return Request
	}
	if !ValidIncoming(f.Incoming) || now.Before(f.ValidFrom) || !now.Before(f.ExpiresAt) {
		return Block
	}
	return f.Incoming
}
func ValidSourceVersion(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func ValidRecord(r Record, owner string) bool {
	if r.Schema != Schema || r.OwnerID != owner || !ValidIncoming(r.IncomingRequests) || r.ObservedAt.IsZero() || r.AgentID == "" {
		return false
	}
	if !r.Configured {
		return r.NativeRevision == 0 && r.Status == "UNCONFIGURED" && r.IncomingRequests == Request && r.ValidFrom == nil && r.ExpiresAt == nil
	}
	if r.NativeRevision < 1 || r.ValidFrom == nil || r.ExpiresAt == nil || !r.ExpiresAt.After(*r.ValidFrom) {
		return false
	}
	active := !r.ObservedAt.Before(*r.ValidFrom) && r.ObservedAt.Before(*r.ExpiresAt)
	return active && r.Status == "ACTIVE" || !active && r.Status == "EXPIRED"
}
func ValidDecision(d Decision) bool {
	if d.Schema != Schema || d.Authority != Authority || d.ScreeningAvailable || d.AutomaticAcceptance || d.ObservedAt.IsZero() || d.ValidUntil.Before(d.ObservedAt) {
		return false
	}
	if d.Disposition == Block {
		return d.SourceVersion == ""
	}
	return (d.Disposition == Allow || d.Disposition == Request || d.Disposition == Screen) && ValidSourceVersion(d.SourceVersion) && d.ValidUntil.After(d.ObservedAt)
}

type PutInput struct {
	ExpectedVersion  int64       `json:"expectedVersion"`
	IncomingRequests Disposition `json:"incomingRequests"`
	ExpiresAt        time.Time   `json:"expiresAt"`
}

func ValidInput(in PutInput) bool {
	return in.ExpectedVersion >= 0 && in.ExpectedVersion < 9223372036854775807 && ValidIncoming(in.IncomingRequests) && !in.ExpiresAt.IsZero() && in.ExpiresAt.Year() >= 1 && in.ExpiresAt.Year() <= 9999 && in.ExpiresAt.Nanosecond()%1000 == 0
}

type Record struct {
	Schema           string      `json:"schemaVersion"`
	OwnerID          string      `json:"ownerId"`
	AgentID          string      `json:"agentId"`
	Configured       bool        `json:"configured"`
	NativeRevision   int64       `json:"nativeRevision"`
	Status           string      `json:"status"`
	IncomingRequests Disposition `json:"incomingRequests"`
	ObservedAt       time.Time   `json:"observedAt"`
	ValidFrom        *time.Time  `json:"validFrom,omitempty"`
	ExpiresAt        *time.Time  `json:"expiresAt,omitempty"`
}
type Decision struct {
	Schema              string      `json:"schemaVersion"`
	Disposition         Disposition `json:"disposition"`
	SourceVersion       string      `json:"sourceVersion"`
	ObservedAt          time.Time   `json:"observedAt"`
	ValidUntil          time.Time   `json:"validUntil"`
	ScreeningAvailable  bool        `json:"screeningAvailable"`
	AutomaticAcceptance bool        `json:"automaticAcceptance"`
	Authority           string      `json:"authority"`
}
type Store interface {
	GetOwnMessagePolicy(context.Context, agentprofile.PrivateAccess) (Record, error)
	PutOwnMessagePolicy(context.Context, agentprofile.PrivateAccess, PutInput) (Record, error)
	ReadMessagePolicyDecision(context.Context, agentprofile.PrivateAccess, string) (Decision, error)
}
