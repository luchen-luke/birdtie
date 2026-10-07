package agentcandidatepipeline

import (
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"strings"
	"testing"
	"time"
)

const ownID = "86000000-0000-4000-8000-000000000001"
const grantID = "86000000-0000-4000-8000-000000000002"
const agentID = "86000000-0000-4000-8000-000000000003"

func receiptFixture() Receipt {
	return Receipt{SchemaVersion: Schema, State: "CANDIDATE_STAGED", RetentionGrantID: grantID, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: ownID}, AgentID: agentID, EventID: "86000000-0000-4000-8000-000000000004", LogicalOperationID: "86000000-0000-4000-8000-000000000005", EffectKey: strings.Repeat("a", 64), HandlerVersion: LocalV1, CandidateID: "86000000-0000-4000-8000-000000000006", ObservedAt: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), Committed: true, Explanation: "原生事务回执"}
}
func TestCandidatePipelineReceiptClosedShape(t *testing.T) {
	r := receiptFixture()
	if ValidateReceipt(r) != nil {
		t.Fatal("valid")
	}
	for _, name := range []string{"owner", "model", "promotion", "key", "handler", "state", "committed", "time", "candidateID"} {
		t.Run(name, func(t *testing.T) {
			b := r
			switch name {
			case "owner":
				b.Owner.Type = actorref.Organization
			case "model":
				b.ModelAccess = true
			case "promotion":
				b.MemoryPromotionAllowed = true
			case "key":
				b.EffectKey = strings.Repeat("A", 64)
			case "handler":
				b.HandlerVersion = "mom-control-v1"
			case "state":
				b.State = "ACTIVE"
			case "committed":
				b.Committed = false
			case "time":
				b.ObservedAt = time.Time{}
			case "candidateID":
				b.CandidateID = "fake"
			}
			if ValidateReceipt(b) == nil {
				t.Fatal("accepted", name)
			}
		})
	}
	empty := Receipt{SchemaVersion: Schema, State: "NOT_STAGED", RetentionGrantID: grantID, Owner: r.Owner, AgentID: agentID, ObservedAt: r.ObservedAt, Explanation: "未找到原生回执"}
	if ValidateReceipt(empty) != nil {
		t.Fatal(empty)
	}
	empty.EffectKey = r.EffectKey
	if ValidateReceipt(empty) == nil {
		t.Fatal("partial false success")
	}
}
