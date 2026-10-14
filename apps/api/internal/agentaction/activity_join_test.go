package agentaction

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"strings"
	"testing"
	"time"
)

func joinUnitBinding() (ActivityJoinBinding, ActivityJoinInput) {
	id := "25000000-0000-4000-8000-000000000001"
	p := ActivityJoinInput{TaskID: id, LogicalOperationID: id, ActionID: id, ActivityID: "25000000-0000-4000-8000-000000000002", ExpectedTaskUpdatedAt: "2026-10-13T00:00:00Z", ReplyTurnDigest: strings.Repeat("a", 64)}
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	b := ActivityJoinBinding{Binding: Binding{Schema: ActivityJoinSchema, ApprovalID: id, TenantID: id, ActorID: id, SubjectType: "PERSON", SubjectID: id, AgentID: id, SessionID: id, TaskID: id, LogicalOperationID: id, ActionID: id, Tool: ActivityJoinTool, ToolVersion: ActivityJoinToolVersion, TargetID: p.ActivityID, PayloadDigest: ActivityJoinPayloadDigest(p), SourceVersion: strings.Repeat("a", 64), SourceGeneration: "123", AuthorityVersion: strings.Repeat("b", 64), PolicyVersion: strings.Repeat("c", 64), ConsentPurpose: ActivityJoinPurpose, GrantID: id, GrantRevision: 1, MembershipVersion: "NONE_PERSON_ONLY", ObservedAt: at, ExpiresAt: at.Add(30 * time.Second)}, TargetVersion: strings.Repeat("d", 64), TargetTailVersion: strings.Repeat("e", 64), ParticipationVersion: strings.Repeat("f", 64)}
	b.SourceContract = ActivityJoinReplySource
	b.ReplyIndex = "1"
	b.ReplyTurnDigest = p.ReplyTurnDigest
	b.ReplyResultSetID = "reply:" + b.TaskID + ":" + b.ReplyTurnDigest[:24]
	b.ReviewDigest = strings.Repeat("a", 64)
	return b, p
}
func TestAgentActivityJoinCanonicalClosedPurposeAndStableEffect(t *testing.T) {
	b, p := joinUnitBinding()
	if !ValidActivityJoinBinding(b, p) {
		t.Fatal("valid native binding rejected")
	}
	raw, d, e := ActivityJoinCanonical(b, p)
	if e != nil || d != ActivityJoinBindingDigest(raw) {
		t.Fatal(e)
	}
	key := ActivityJoinEffectKey(b)
	for _, mutate := range []func(*ActivityJoinBinding){func(b *ActivityJoinBinding) { b.ToolVersion = "new-handler" }, func(b *ActivityJoinBinding) { b.PayloadDigest = strings.Repeat("0", 64) }, func(b *ActivityJoinBinding) { b.GrantID = p.ActivityID }} {
		c := b
		mutate(&c)
		if ActivityJoinEffectKey(c) != key {
			t.Fatal("technical version changed logical effect")
		}
	}
	c := b
	c.ActionID = p.ActivityID
	if ActivityJoinEffectKey(c) == key {
		t.Fatal("deliberate new action merged")
	}
	for _, mutate := range []func(*ActivityJoinBinding){func(b *ActivityJoinBinding) { b.ConsentPurpose = Purpose }, func(b *ActivityJoinBinding) { b.SubjectType = "ORGANIZATION" }, func(b *ActivityJoinBinding) { b.ExpiresAt = b.ObservedAt.Add(31 * time.Second) }, func(b *ActivityJoinBinding) { b.TargetID = b.ActorID }} {
		c = b
		mutate(&c)
		if ValidActivityJoinBinding(c, p) {
			t.Fatal("invalid scope allowed")
		}
	}
}
func TestAgentActivityJoinWireRejectsInventedApprovalAndCurrentGoing(t *testing.T) {
	b, p := joinUnitBinding()
	raw, _ := json.Marshal(p)
	if _, e := DecodeActivityJoinInput(raw); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{`{}`, string(raw[:len(raw)-1]) + `,"confirmed":true}`, string(raw[:len(raw)-1]) + `,"taskId":"x"}`, string(raw) + `{}`, `null`} {
		if _, e := DecodeActivityJoinInput([]byte(s)); e == nil {
			t.Fatal("accepted loose input")
		}
	}
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: b.ActorID}
	r := ActivityJoinReceipt{SchemaVersion: ActivityJoinSchema, Owner: owner, AgentID: b.AgentID, ApprovalID: b.ApprovalID, DispatchID: b.ApprovalID, Status: Succeeded, CommittedAt: b.ObservedAt}
	if ValidateActivityJoinReceipt(r, owner, b.ApprovalID) == nil {
		t.Fatal("current going or success label fabricated effect")
	}
	r.Status = Unknown
	if ValidateActivityJoinReceipt(r, owner, b.ApprovalID) != nil {
		t.Fatal("honest unknown rejected")
	}
}

func TestAgentActivityJoinReplyBindingAndReviewClosedUnknowns(t *testing.T) {
	b, p := joinUnitBinding()
	raw, _, e := ActivityJoinCanonical(b, p)
	if e != nil {
		t.Fatal(e)
	}
	var keys map[string]any
	if json.Unmarshal([]byte(raw), &keys) != nil || len(keys) != 33 {
		t.Fatal("new source not closed33")
	}
	for _, mutate := range []func(*ActivityJoinBinding){func(q *ActivityJoinBinding) { q.ReplyIndex = "01" }, func(q *ActivityJoinBinding) { q.ReplyTurnDigest = strings.Repeat("0", 64) }, func(q *ActivityJoinBinding) { q.SourceContract = "ACTIVE_OR_ANY_TASK" }, func(q *ActivityJoinBinding) { q.ReviewDigest = "" }} {
		q := b
		mutate(&q)
		if ValidActivityJoinBinding(q, p) {
			t.Fatal("invented reply source accepted")
		}
	}
	legacy := b
	legacy.SourceContract = ""
	legacy.ReplyIndex = ""
	legacy.ReplyTurnDigest = ""
	legacy.ReplyResultSetID = ""
	legacy.ReviewDigest = ""
	oldInput := p
	oldInput.ExpectedTaskUpdatedAt = ""
	oldInput.ReplyTurnDigest = ""
	legacy.PayloadDigest = ActivityJoinPayloadDigest(oldInput)
	if !ValidActivityJoinBinding(legacy, oldInput) || legacy.CurrentReplySource() {
		t.Fatal("legacy history lost or elevated")
	}
	oldRaw, _ := json.Marshal(oldInput)
	if _, e = DecodeActivityJoinInput(oldRaw); e == nil {
		t.Fatal("old4selector minted fresh preview")
	}
	timezone := b.ObservedAt
	r := ActivityJoinReview{SchemaVersion: ActivityJoinReviewSchema, ActivityID: p.ActivityID, TargetVersion: b.TargetVersion, Title: "完整标题", Summary: "条件摘要", Description: "完整资格与报名规则", Organizer: ActivityJoinOrganizer{Type: "PERSON", ID: b.ActorID}, StartsAt: timezone, EndsAt: timezone.Add(time.Hour), TimeZone: "Europe/London", Modality: "unspecified", Visibility: "public"}
	if ValidateActivityJoinReview(&r, p.ActivityID) != nil {
		t.Fatal("explicit unknown rejected")
	}
	bytes, _ := json.Marshal(r)
	var review map[string]any
	json.Unmarshal(bytes, &review)
	if len(review) != 20 || review["priceMinor"] != nil || review["capacity"] != nil || review["hostLabel"] != nil {
		t.Fatal("unknown fabricated", review)
	}
	d := ActivityJoinReviewDigest(r)
	r.Description += "新增规则"
	if ActivityJoinReviewDigest(r) == d {
		t.Fatal("rules not version bound")
	}
	r.Description = strings.Repeat("界", 6000)
	if ValidateActivityJoinReview(&r, p.ActivityID) == nil {
		t.Fatal("overbound rules approved")
	}
}
