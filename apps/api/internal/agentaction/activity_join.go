package agentaction

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const ActivityJoinSchema = "air.activity_join.v1"
const ActivityJoinPurpose = "OWN_ACTIVITY_JOIN_ACTION"
const ActivityJoinTool = "activity.join"
const ActivityJoinToolVersion = "activity.join.v1"
const ActivityJoinEffectKind = "ACTIVITY_JOIN"
const ActivityJoinMaxBytes = 32768
const ActivityJoinReplySource = "LATEST_ACTIVITY_REPLY_V1"
const ActivityJoinReviewSchema = "activity-join-review-v1"

// Selectors identify an explicit proposal. None selects its owner or Agent,
// proves a source, carries a human approval, or invokes a model.
type ActivityJoinInput struct {
	TaskID                string `json:"taskId"`
	LogicalOperationID    string `json:"logicalOperationId"`
	ActionID              string `json:"actionId"`
	ActivityID            string `json:"activityId"`
	ExpectedTaskUpdatedAt string `json:"expectedTaskUpdatedAt,omitempty"`
	ReplyTurnDigest       string `json:"replyTurnDigest,omitempty"`
}

func (p ActivityJoinInput) Valid() bool {
	return agentplanner.ValidID(p.TaskID) && agentplanner.ValidID(p.LogicalOperationID) && agentplanner.ValidID(p.ActionID) && agentplanner.ValidID(p.ActivityID) && ((p.ExpectedTaskUpdatedAt == "" && p.ReplyTurnDigest == "") || p.CurrentSourceValid())
}
func (p ActivityJoinInput) CurrentSourceValid() bool {
	t, e := time.Parse(time.RFC3339Nano, p.ExpectedTaskUpdatedAt)
	return e == nil && recoveryTime(t) && t.Location() == time.UTC && p.ExpectedTaskUpdatedAt == t.UTC().Format(time.RFC3339Nano) && hex64(p.ReplyTurnDigest)
}

// The common original approval fields retain their meanings. The extra
// fingerprints bind the original domain's target and expected transition.
type ActivityJoinBinding struct {
	Binding
	TargetVersion        string `json:"target_version"`
	TargetTailVersion    string `json:"target_tail_version"`
	ParticipationVersion string `json:"participation_version"`
	SourceContract       string `json:"source_contract,omitempty"`
	ReplyIndex           string `json:"reply_index,omitempty"`
	ReplyTurnDigest      string `json:"reply_turn_digest,omitempty"`
	ReplyResultSetID     string `json:"reply_result_set_id,omitempty"`
	ReviewDigest         string `json:"review_digest,omitempty"`
}

func ValidActivityJoinBinding(b ActivityJoinBinding, p ActivityJoinInput) bool {
	// The original 28-key variant remains readable as immutable history. Only
	// the reply-source variant can obtain a new native execution capability.
	if b.SourceContract == "" {
		if b.ReplyIndex != "" || b.ReplyTurnDigest != "" || b.ReplyResultSetID != "" || b.ReviewDigest != "" || p.ExpectedTaskUpdatedAt != "" || p.ReplyTurnDigest != "" {
			return false
		}
	} else if !b.CurrentReplySource() || !p.CurrentSourceValid() || b.ReplyTurnDigest != p.ReplyTurnDigest {
		return false
	}
	for _, id := range []string{b.ApprovalID, b.TenantID, b.ActorID, b.SubjectID, b.AgentID, b.SessionID, b.GrantID, b.TaskID} {
		if !agentplanner.ValidID(id) {
			return false
		}
	}
	return p.Valid() && b.Schema == ActivityJoinSchema && b.TenantID == b.ActorID && b.ActorID == b.SubjectID && b.SubjectType == "PERSON" && b.TaskID == p.TaskID && b.LogicalOperationID == p.LogicalOperationID && b.ActionID == p.ActionID && b.TargetID == p.ActivityID && b.Tool == ActivityJoinTool && b.ToolVersion == ActivityJoinToolVersion && b.PayloadDigest == ActivityJoinPayloadDigest(p) && hex64(b.SourceVersion) && b.SourceGeneration != "" && hex64(b.AuthorityVersion) && hex64(b.PolicyVersion) && hex64(b.TargetVersion) && hex64(b.TargetTailVersion) && hex64(b.ParticipationVersion) && b.ConsentPurpose == ActivityJoinPurpose && b.GrantRevision == 1 && b.MembershipVersion == "NONE_PERSON_ONLY" && recoveryTime(b.ObservedAt) && recoveryTime(b.ExpiresAt) && b.ExpiresAt.After(b.ObservedAt) && b.ExpiresAt.Sub(b.ObservedAt) <= 30*time.Second
}
func (b ActivityJoinBinding) CurrentReplySource() bool {
	i, e := strconv.Atoi(b.ReplyIndex)
	return b.SourceContract == ActivityJoinReplySource && e == nil && i >= 1 && i <= 9999 && strconv.Itoa(i) == b.ReplyIndex && hex64(b.ReplyTurnDigest) && hex64(b.ReviewDigest) && b.ReplyResultSetID == "reply:"+b.TaskID+":"+b.ReplyTurnDigest[:24]
}
func activityJoinHash(prefix string, value any) string {
	raw, _ := json.Marshal(value)
	h := sha256.Sum256(append([]byte(prefix), raw...))
	return hex.EncodeToString(h[:])
}
func ActivityJoinPayloadDigest(p ActivityJoinInput) string {
	return activityJoinHash("birdtie.activity-join-payload.v1:", p)
}
func ActivityJoinCanonical(b ActivityJoinBinding, p ActivityJoinInput) (string, string, error) {
	if !ValidActivityJoinBinding(b, p) {
		return "", "", ErrInvalid
	}
	raw, e := json.Marshal(b)
	if e != nil {
		return "", "", ErrInvalid
	}
	return string(raw), ActivityJoinBindingDigest(string(raw)), nil
}
func ActivityJoinBindingDigest(raw string) string {
	h := sha256.Sum256([]byte("birdtie.activity-join-approval.v1:" + raw))
	return hex.EncodeToString(h[:])
}
func ActivityJoinEffectKey(b ActivityJoinBinding) string {
	return activityJoinHash("birdtie.activity-join-effect.v1:", []string{b.TenantID, b.LogicalOperationID, b.ActionID, ActivityJoinEffectKind})
}

type ActivityJoinPreview struct {
	SchemaVersion      string                `json:"schemaVersion"`
	Owner              actorref.PrincipalRef `json:"owner"`
	AgentID            string                `json:"agentId"`
	TaskID             string                `json:"taskId"`
	ApprovalID         string                `json:"approvalId"`
	LogicalOperationID string                `json:"logicalOperationId"`
	ActionID           string                `json:"actionId"`
	ActivityID         string                `json:"activityId"`
	BindingDigest      string                `json:"bindingDigest"`
	State              string                `json:"state"`
	ObservedAt         time.Time             `json:"observedAt"`
	ExpiresAt          time.Time             `json:"expiresAt"`
	DispatchCommitted  bool                  `json:"dispatchCommitted"`
	Review             *ActivityJoinReview   `json:"review"`
	Proof              any                   `json:"-"`
}

type ActivityJoinOrganizer struct {
	Type string  `json:"type"`
	ID   string  `json:"id"`
	Name *string `json:"name"`
}

// These are original current domain facts, not a model summary or client
// details. Unknowns are explicit null. Oversized facts prevent an approval;
// truncation must never hide a condition that the eventual action will use.
type ActivityJoinReview struct {
	SchemaVersion       string                `json:"schemaVersion"`
	ActivityID          string                `json:"activityId"`
	TargetVersion       string                `json:"targetVersion"`
	Title               string                `json:"title"`
	Summary             string                `json:"summary"`
	Description         string                `json:"description"`
	HostLabel           *string               `json:"hostLabel"`
	Organizer           ActivityJoinOrganizer `json:"organizer"`
	StartsAt            time.Time             `json:"startsAt"`
	EndsAt              time.Time             `json:"endsAt"`
	TimeZone            string                `json:"timeZone"`
	Modality            string                `json:"modality"`
	LocationLabel       *string               `json:"locationLabel"`
	PhysicalPlaceStatus *string               `json:"physicalPlaceStatus"`
	PriceMinor          *int                  `json:"priceMinor"`
	Currency            *string               `json:"currency"`
	Eligibility         *string               `json:"eligibility"`
	Visibility          string                `json:"visibility"`
	Capacity            *int                  `json:"capacity"`
	ParticipantCount    *int                  `json:"participantCount"`
}

func reviewText(s string, max int, required bool) bool {
	return utf8.ValidString(s) && len(s) <= max && (!required || strings.TrimSpace(s) != "") && !strings.ContainsRune(s, '\x00')
}
func reviewOptional(s *string, max int) bool { return s == nil || reviewText(*s, max, true) }
func ActivityJoinCurrencyValid(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, v := range s {
		if v < 'A' || v > 'Z' {
			return false
		}
	}
	return true
}
func ValidateActivityJoinReview(r *ActivityJoinReview, activity string) error {
	if r == nil || r.SchemaVersion != ActivityJoinReviewSchema || r.ActivityID != activity || !agentplanner.ValidID(activity) || !hex64(r.TargetVersion) || !reviewText(r.Title, 1024, true) || !reviewText(r.Summary, 2048, false) || !reviewText(r.Description, 16384, false) || !reviewOptional(r.HostLabel, 512) || !reviewOptional(r.Organizer.Name, 512) || !agentplanner.ValidID(r.Organizer.ID) || !reviewText(r.TimeZone, 128, true) || !reviewOptional(r.LocationLabel, 1024) || !reviewOptional(r.Eligibility, 2048) || r.Visibility != "public" || !recoveryTime(r.StartsAt) || !recoveryTime(r.EndsAt) || !r.EndsAt.After(r.StartsAt) {
		return ErrUnavailable
	}
	switch r.Organizer.Type {
	case "PERSON", "COMMUNITY", "BUSINESS", "ORGANIZATION":
	default:
		return ErrUnavailable
	}
	switch r.Modality {
	case "unspecified", "in_person", "online", "hybrid":
	default:
		return ErrUnavailable
	}
	if r.PhysicalPlaceStatus != nil {
		switch *r.PhysicalPlaceStatus {
		case "unknown", "confirmed", "tbd", "not_applicable":
		default:
			return ErrUnavailable
		}
	}
	if (r.PriceMinor == nil) != (r.Currency == nil) || (r.PriceMinor != nil && *r.PriceMinor < 0) || (r.Currency != nil && !ActivityJoinCurrencyValid(*r.Currency)) || (r.Capacity != nil && *r.Capacity <= 0) || (r.ParticipantCount != nil && *r.ParticipantCount < 0) {
		return ErrUnavailable
	}
	if _, e := time.LoadLocation(r.TimeZone); e != nil {
		return ErrUnavailable
	}
	return nil
}
func ActivityJoinReviewDigest(r ActivityJoinReview) string {
	return activityJoinHash("birdtie.activity-join-review.v1:", r)
}

// Immutable causal outcome. A participation currently marked going is not a
// result for this operation. No session, grant, source proof or body is public.
type ActivityJoinReceipt struct {
	SchemaVersion   string                `json:"schemaVersion"`
	Owner           actorref.PrincipalRef `json:"owner"`
	AgentID         string                `json:"agentId"`
	ApprovalID      string                `json:"approvalId"`
	DispatchID      string                `json:"dispatchId"`
	Status          string                `json:"status"`
	CommittedAt     time.Time             `json:"committedAt"`
	EffectID        *string               `json:"effectId,omitempty"`
	ParticipationID *string               `json:"participationId,omitempty"`
	AppliedAt       *time.Time            `json:"appliedAt,omitempty"`
	Proof           any                   `json:"-"`
}

func ValidateActivityJoinPreview(v ActivityJoinPreview, owner actorref.PrincipalRef) error {
	if owner.Type != actorref.Person || v.Owner != owner || v.SchemaVersion != ActivityJoinSchema || !agentplanner.ValidID(owner.ID) || !agentplanner.ValidID(v.AgentID) || !agentplanner.ValidID(v.TaskID) || !agentplanner.ValidID(v.ApprovalID) || !agentplanner.ValidID(v.LogicalOperationID) || !agentplanner.ValidID(v.ActionID) || !agentplanner.ValidID(v.ActivityID) || !hex64(v.BindingDigest) || !recoveryTime(v.ObservedAt) || !recoveryTime(v.ExpiresAt) || !v.ExpiresAt.After(v.ObservedAt) || v.ExpiresAt.Sub(v.ObservedAt) > 30*time.Second {
		return ErrUnavailable
	}
	switch v.State {
	case Pending, Approved:
		if v.DispatchCommitted || ValidateActivityJoinReview(v.Review, v.ActivityID) != nil {
			return ErrUnavailable
		}
		return nil
	case Consumed:
		if !v.DispatchCommitted {
			return ErrUnavailable
		}
		if v.Review != nil && ValidateActivityJoinReview(v.Review, v.ActivityID) != nil {
			return ErrUnavailable
		}
		return nil
	case Cancelled:
		if v.Review != nil {
			return ErrUnavailable
		}
		return nil
	}
	return ErrUnavailable
}
func ValidateActivityJoinReceipt(v ActivityJoinReceipt, owner actorref.PrincipalRef, approval string) error {
	if owner.Type != actorref.Person || v.Owner != owner || v.SchemaVersion != ActivityJoinSchema || !agentplanner.ValidID(owner.ID) || !agentplanner.ValidID(v.AgentID) || !agentplanner.ValidID(approval) || v.ApprovalID != approval || !agentplanner.ValidID(v.DispatchID) || !recoveryTime(v.CommittedAt) {
		return ErrUnavailable
	}
	switch v.Status {
	case Committed, InFlight, Unknown, NoEffect:
		if v.EffectID != nil || v.ParticipationID != nil || v.AppliedAt != nil {
			return ErrUnavailable
		}
	case Succeeded:
		if v.EffectID == nil || v.ParticipationID == nil || v.AppliedAt == nil || !agentplanner.ValidID(*v.EffectID) || !agentplanner.ValidID(*v.ParticipationID) || !recoveryTime(*v.AppliedAt) || v.AppliedAt.Before(v.CommittedAt) {
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	return nil
}

type ActivityJoinHumanGateway interface {
	PreviewOwn(context.Context, agentprofile.PrivateAccess, ActivityJoinInput) (ActivityJoinPreview, error)
	ConfirmOwn(context.Context, agentprofile.PrivateAccess, string, string) (ActivityJoinPreview, error)
	ExecuteOwn(context.Context, agentprofile.PrivateAccess, string, string) (ActivityJoinReceipt, error)
	ReconcileOwn(context.Context, agentprofile.PrivateAccess, string) (ActivityJoinReceipt, error)
	RevokeOwn(context.Context, agentprofile.PrivateAccess, string) (ActivityJoinPreview, error)
	ValidatePreviewOwn(context.Context, agentprofile.PrivateAccess, ActivityJoinPreview) error
	ValidateReceiptOwn(context.Context, agentprofile.PrivateAccess, ActivityJoinReceipt) error
}

// These opaque native capabilities exist only after an actual acknowledged
// dispatch commit. Recovery of JSON/IDs never returns them.
type ActivityJoinCommitment interface {
	Begin(context.Context, agentprofile.PrivateAccess) (ActivityJoinReceipt, ActivityJoinClaim, error)
}
type ActivityJoinClaim interface {
	Execute(context.Context, agentprofile.PrivateAccess) (ActivityJoinReceipt, error)
}

func decodeActivityJoinObject(raw []byte, keys ...string) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, ErrInvalid
	}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	v := map[string]string{}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok || !allowed[k] {
			return nil, ErrInvalid
		}
		if _, ok = v[k]; ok {
			return nil, ErrInvalid
		}
		var s string
		if d.Decode(&s) != nil {
			return nil, ErrInvalid
		}
		v[k] = s
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(v) != len(keys) {
		return nil, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	return v, nil
}
func DecodeActivityJoinInput(raw []byte) (ActivityJoinInput, error) {
	v, e := decodeActivityJoinObject(raw, "taskId", "logicalOperationId", "actionId", "activityId", "expectedTaskUpdatedAt", "replyTurnDigest")
	p := ActivityJoinInput{TaskID: v["taskId"], LogicalOperationID: v["logicalOperationId"], ActionID: v["actionId"], ActivityID: v["activityId"], ExpectedTaskUpdatedAt: v["expectedTaskUpdatedAt"], ReplyTurnDigest: v["replyTurnDigest"]}
	if at, err := time.Parse(time.RFC3339Nano, p.ExpectedTaskUpdatedAt); err == nil && recoveryTime(at) {
		p.ExpectedTaskUpdatedAt = at.UTC().Format(time.RFC3339Nano)
	}
	if e != nil || !p.Valid() || !p.CurrentSourceValid() {
		return ActivityJoinInput{}, ErrInvalid
	}
	return p, nil
}
func DecodeActivityJoinDigest(raw []byte) (string, error) {
	v, e := decodeActivityJoinObject(raw, "bindingDigest")
	if e != nil || !hex64(v["bindingDigest"]) {
		return "", ErrInvalid
	}
	return v["bindingDigest"], nil
}
