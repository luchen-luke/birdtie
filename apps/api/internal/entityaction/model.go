// Package entityaction projects existing domain actions. A descriptor is a
// human proposal, never a capability, approval, or general-purpose executor.
package entityaction

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const Schema = "entity-actions-v1"
const (
	Connect     = "CONNECT"
	Message     = "MESSAGE"
	Share       = "SHARE"
	Join        = "JOIN"
	Save        = "SAVE"
	Navigate    = "NAVIGATE"
	Available   = "AVAILABLE"
	Unavailable = "UNAVAILABLE"
)

var (
	ErrInvalid     = errors.New("invalid entity action request")
	ErrNotFound    = errors.New("entity action source unavailable")
	ErrChanged     = errors.New("entity action source changed")
	ErrUnavailable = errors.New("entity action service unavailable")
)
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Ref struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func (r Ref) Valid() bool {
	switch r.Type {
	case "person", "activity", "place", "community", "organization", "business":
	default:
		return false
	}
	return uuid.MatchString(r.ID) && r.ID != "00000000-0000-0000-0000-000000000000"
}

type Descriptor struct {
	Kind              string   `json:"kind"`
	Operation         string   `json:"operation"`
	AllowedOperations []string `json:"allowedOperations"`
	Target            Ref      `json:"targetRef"`
	State             string   `json:"state"`
	Label             string   `json:"label"`
	Reason            string   `json:"reason"`
	// A human must still inspect the original current domain target, recipient
	// and consequences. This flag does not attest that approval has occurred.
	RequiresConfirmation bool `json:"requiresConfirmation"`
}
type View struct {
	Schema        string       `json:"schema"`
	Title         string       `json:"title"`
	SourceVersion string       `json:"sourceVersion"`
	Entity        Ref          `json:"entityRef"`
	Actions       []Descriptor `json:"actions"`
	ObservedAt    time.Time    `json:"observedAt"`
	ValidUntil    time.Time    `json:"validUntil"`
}
type Access struct {
	Public        bool
	Actor         identity.Actor
	SessionDigest [32]byte
}

func (a Access) Valid() bool {
	if a.Public {
		return a.Actor == (identity.Actor{}) && a.SessionDigest == ([32]byte{})
	}
	return a.Actor.AccountType == "person" && uuid.MatchString(a.Actor.ID) && a.SessionDigest != ([32]byte{})
}

// Receipt remains server-only; its proof binds the exact native source and
// current actor/session. Wire View has no claim or write authority.
type Receipt struct {
	View  View
	Proof string
	Seal  string
}
type Store interface {
	ReadEntityActions(context.Context, Access, Ref) (Receipt, error)
	RevalidateEntityActions(context.Context, Access, Ref, Receipt) error
}

// BoundCondition is an optimistic condition on a reviewed proposal. It is
// never a permission: the original domain writer authorizes in its own tx.
type BoundCondition struct {
	SourceVersion string
	ValidUntil    time.Time
	Kind          string
	Operation     string
}

func (b BoundCondition) Valid(ref Ref) bool {
	if !ref.Valid() || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(b.SourceVersion) || b.ValidUntil.IsZero() {
		return false
	}
	return ValidOperation(b.Kind, b.Operation, ref.Type)
}

// Facts is server-resolved current state, not a client DTO. Unresolved facts
// remain false, preventing fallback to buttons inferred solely from entity type.
type Facts struct {
	CanConnect, CanMessage, CanShare, CanJoin, CanSave, CanNavigate bool
	CanExport, CanRequestConversation                               bool
	Connected, Pending, Joined, JoinPending, Saved                  bool
	RequestJoin                                                     bool
	Invited                                                         bool
}

func Project(ref Ref, f Facts, observed, deadline time.Time, title, version string) View {
	v := View{Title: title, SourceVersion: version, Schema: Schema, Entity: ref, ObservedAt: observed.UTC(), ValidUntil: deadline.UTC(), Actions: []Descriptor{}}
	labels := []string{"申请连接", "打开私信", "发给好友", "报名参加", "收藏", "导航"}
	operations := []string{"REQUEST_FRIEND", "OPEN_CHAT", "CHOOSE_RECIPIENT", "JOIN", "SAVE", "NAVIGATE"}
	kinds := []string{Connect, Message, Share, Join, Save, Navigate}
	allowed := []bool{f.CanConnect, f.CanMessage, f.CanShare, f.CanJoin, f.CanSave, f.CanNavigate}
	for i, k := range kinds {
		a := Descriptor{Kind: k, Operation: operations[i], Target: ref, State: Unavailable, Label: labels[i], Reason: "当前对象或身份不支持此操作。", RequiresConfirmation: k != Message}
		if allowed[i] {
			a.State = Available
			a.Reason = "提交前仍需检查当前领域权限和状态。"
		}
		switch k {
		case Share:
			if f.CanExport && !f.CanShare {
				a.State = Available
				a.Operation = "EXPORT_PUBLIC"
				a.Label = "打开系统分享"
				a.Reason = "检查当前公开内容后打开系统分享面板，由你选择应用或收件人；不表示已送达。"
			}
		case Connect:
			if f.CanRequestConversation && !f.CanConnect {
				a.State = Available
				a.Operation = "REQUEST_CONVERSATION"
				a.Label = "申请私信"
				a.Reason = "检查这次私信申请与留言，对方可以接受或拒绝；不会建立持续好友关系或自动发送消息。"
			}
			if f.Connected && a.State == Unavailable {
				a.Reason = "已经是好友。"
			}
			if f.Pending && a.State == Unavailable {
				a.Reason = "已有待处理的连接申请。"
			}
		case Join:
			if ref.Type == "community" {
				a.Label = "加入社群"
			}
			if f.RequestJoin {
				a.Label = "申请加入"
				a.Operation = "REQUEST_JOIN"
			}
			if f.Invited && ref.Type == "community" {
				a.Label = "接受邀请"
				a.Operation = "ACCEPT_INVITATION"
			}
			if f.Joined {
				if ref.Type == "community" {
					a.Label = "退出社群"
					a.Operation = "LEAVE"
				} else {
					a.Label = "取消报名"
					a.Operation = "CANCEL_RSVP"
				}
			}
			if f.JoinPending {
				if ref.Type == "community" {
					a.Label = "撤回申请"
					a.Operation = "LEAVE"
				} else {
					a.Label = "取消报名"
					a.Operation = "CANCEL_RSVP"
				}
			}
		case Save:
			if f.Saved {
				a.Label = "取消收藏"
				a.Operation = "UNSAVE"
			}
		}
		a.AllowedOperations = []string{a.Operation}
		if k == Join && ref.Type == "community" && a.Operation == "ACCEPT_INVITATION" && a.State == Available {
			a.AllowedOperations = append(a.AllowedOperations, "DECLINE_INVITATION")
		}
		if k == Share && f.CanExport && a.Operation == "CHOOSE_RECIPIENT" && a.State == Available {
			a.AllowedOperations = append(a.AllowedOperations, "EXPORT_PUBLIC")
		}
		if k == Connect && f.CanRequestConversation && a.State == Available && a.Operation == "REQUEST_FRIEND" {
			a.AllowedOperations = append(a.AllowedOperations, "REQUEST_CONVERSATION")
		}
		v.Actions = append(v.Actions, a)
	}
	return v
}
func (v View) Valid() bool {
	if v.Title == "" || len(v.Title) > 1200 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(v.SourceVersion) || v.Schema != Schema || !v.Entity.Valid() || len(v.Actions) != 6 || v.ObservedAt.IsZero() || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.After(v.ObservedAt.Add(30*time.Second)) {
		return false
	}
	seen := map[string]bool{}
	for _, a := range v.Actions {
		if seen[a.Kind] || a.Target != v.Entity || a.Label == "" || a.Reason == "" || (a.State != Available && a.State != Unavailable) {
			return false
		}
		switch a.Kind {
		case Connect, Message, Share, Join, Save, Navigate:
		default:
			return false
		}
		seen[a.Kind] = true
		if !ValidOperation(a.Kind, a.Operation, v.Entity.Type) || (a.Kind != Message && !a.RequiresConfirmation) {
			return false
		}
		if len(a.AllowedOperations) == 0 || len(a.AllowedOperations) > 2 || a.AllowedOperations[0] != a.Operation {
			return false
		}
		ops := map[string]bool{}
		for _, op := range a.AllowedOperations {
			if ops[op] || !ValidOperation(a.Kind, op, v.Entity.Type) {
				return false
			}
			ops[op] = true
		}
		if len(ops) > 1 && !(v.Entity.Type == "person" && a.Kind == Connect && a.Operation == "REQUEST_FRIEND" && ops["REQUEST_CONVERSATION"]) && !(v.Entity.Type == "community" && a.Kind == Join && a.Operation == "ACCEPT_INVITATION" && ops["DECLINE_INVITATION"]) && !((v.Entity.Type == "place" || v.Entity.Type == "activity") && a.Kind == Share && a.Operation == "CHOOSE_RECIPIENT" && ops["EXPORT_PUBLIC"]) {
			return false
		}
		if a.State == Available {
			switch a.Kind {
			case Connect, Message:
				if v.Entity.Type != "person" {
					return false
				}
			case Join:
				if v.Entity.Type != "activity" && v.Entity.Type != "community" {
					return false
				}
			case Save:
				if v.Entity.Type != "activity" && v.Entity.Type != "place" && v.Entity.Type != "community" {
					return false
				}
			case Navigate:
				if v.Entity.Type != "activity" && v.Entity.Type != "place" {
					return false
				}
			}
		}
	}
	return true
}

func ValidOperation(kind, op, entity string) bool {
	switch kind {
	case Connect:
		return op == "REQUEST_FRIEND" || (entity == "person" && op == "REQUEST_CONVERSATION")
	case Message:
		return op == "OPEN_CHAT"
	case Share:
		return op == "CHOOSE_RECIPIENT" || ((entity == "place" || entity == "activity") && op == "EXPORT_PUBLIC")
	case Save:
		return op == "SAVE" || op == "UNSAVE"
	case Navigate:
		return op == "NAVIGATE"
	case Join:
		return op == "JOIN" || (entity == "activity" && op == "CANCEL_RSVP") || (entity == "community" && (op == "LEAVE" || op == "REQUEST_JOIN" || op == "ACCEPT_INVITATION" || op == "DECLINE_INVITATION"))
	}
	return false
}
