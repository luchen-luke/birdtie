package agentaction

import (
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"strings"
	"testing"
	"time"
)

func zeroAccess() agentevent.Access { return agentevent.Access{} }
func canonicalFixture() (Binding, agenttool.SandboxProposal) {
	id := "11111111-1111-4111-8111-111111111111"
	p := agenttool.SandboxProposal{ActionID: id, LogicalOperationID: id, TargetID: id, Value: "本人沙箱", ResourceVersion: strings.Repeat("1", 64)}
	b := Binding{Schema: Schema, ApprovalID: id, TenantID: id, ActorID: id, SubjectType: "PERSON", SubjectID: id, AgentID: id, SessionID: id, TaskID: id, LogicalOperationID: id, ActionID: id, Tool: agenttool.SandboxWrite, ToolVersion: ToolVersion, TargetID: id, PayloadDigest: agenttool.Digest(p), SourceVersion: p.ResourceVersion, SourceGeneration: "123", AuthorityVersion: strings.Repeat("2", 64), PolicyVersion: strings.Repeat("3", 64), ConsentPurpose: Purpose, GrantID: id, GrantRevision: 1, MembershipVersion: "NONE_PERSON_ONLY", ObservedAt: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 10, 6, 0, 0, 10, 0, time.UTC)}
	return b, p
}
func TestActionCanonicalBindsFullVersionAndIndependentEffectKey(t *testing.T) {
	b, p := canonicalFixture()
	c, d, e := Canonical(b, p)
	if e != nil || d != Digest(c) {
		t.Fatal(e)
	}
	key := EffectKey(b)
	next := b
	next.PolicyVersion = strings.Repeat("4", 64)
	_, d2, e := Canonical(next, p)
	if e != nil || d == d2 || EffectKey(next) != key {
		t.Fatal("policy not bound or mistaken idempotency", e)
	}
	next.LogicalOperationID = "22222222-2222-4222-8222-222222222222"
	if EffectKey(next) == key {
		t.Fatal("independent deliberate operation collapsed")
	}
}
func TestActionCanonicalRejectsChangedTenantActorSourceToolAndContent(t *testing.T) {
	for _, kind := range []string{"tenant", "actor", "subject", "org", "source", "tool", "value", "expiry", "membership", "grant"} {
		t.Run(kind, func(t *testing.T) {
			b, p := canonicalFixture()
			switch kind {
			case "tenant":
				b.TenantID = "22222222-2222-4222-8222-222222222222"
			case "actor":
				b.ActorID = "22222222-2222-4222-8222-222222222222"
			case "subject":
				b.SubjectID = "22222222-2222-4222-8222-222222222222"
			case "org":
				b.SubjectType = "ORG"
			case "source":
				b.SourceVersion = "unknown"
			case "tool":
				b.Tool = "message.send"
			case "value":
				p.Value = "修改"
			case "expiry":
				b.ExpiresAt = b.ObservedAt.Add(time.Minute)
			case "membership":
				b.MembershipVersion = "1"
			case "grant":
				b.GrantRevision = 2
			}
			if _, _, e := Canonical(b, p); e == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
}
