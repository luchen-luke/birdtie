package agentmulticandidate

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentlocalcandidate"
	"reflect"
	"testing"
	"time"
)

func TestMultiCandidatePureSelectionClosed(t *testing.T) {
	a := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	b := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	until := time.Now().UTC().Truncate(time.Microsecond).Add(time.Minute)
	x, e := Normalize(Selection{[]string{b, a}, until})
	if e != nil {
		t.Fatal(e)
	}
	y, e := Normalize(Selection{[]string{a, b}, until})
	if e != nil || !reflect.DeepEqual(x, y) {
		t.Fatal("ordering")
	}
	for _, ids := range [][]string{nil, {a}, {a, a}, {a, "bad"}, {a, b, a, b, a, b}} {
		if _, e := Normalize(Selection{ids, until}); e == nil {
			t.Fatal("accepted invalid selection")
		}
	}
	var r Resolution
	if e = json.Unmarshal([]byte(`{}`), &r); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	if _, e = json.Marshal(r); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
}
func TestMultiCandidatePureDoesNotClaimProbabilityOrSensitiveClass(t *testing.T) {
	for _, v := range []string{"nightlife", "我不喜欢羽毛球", "可能参加羽毛球或者足球"} {
		if _, e := agentlocalcandidate.Extract(map[string]string{"body": v}); e == nil {
			t.Fatal("unsafe category or ambiguous", v)
		}
	}
	p, e := agentlocalcandidate.Extract(map[string]string{"body": "羽毛球记录"})
	if e != nil || p.Assessment.Level != "LOW" || p.Assessment.Value != nil {
		t.Fatal(p, e)
	}
}

func TestMultiPreviewReceiptPureOriginalRecordNotPermission(t *testing.T) {
	at := time.Now().UTC()
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	r := PreviewReceipt{SchemaVersion: "agent-multi-candidate-preview-receipt-v1", Purpose: Purpose, PreviewID: id, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: id}, AgentID: id, State: "APPROVAL_RECORDED", ConsumedGrantID: id, PreviewObservedAt: at.Add(-2 * time.Minute), PreviewExpiresAt: at.Add(-time.Minute), ObservedAt: at, ValidUntil: at.Add(time.Second)}
	if ValidatePreviewReceipt(r) != nil {
		t.Fatal("historical record must not require current approval expiry")
	}
	r.MemoryPromotionAllowed = true
	if ValidatePreviewReceipt(r) == nil {
		t.Fatal("receipt became permission")
	}
}
