package identity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestHumanControlOriginalFrameCannotRebind(t *testing.T) {
	a := Actor{ID: "11111111-1111-1111-1111-111111111111", AccountType: "person"}
	d := [32]byte{1}
	f := HumanControlFrame{ID: "22222222-2222-2222-2222-222222222222", Generation: 1}
	ctx := WithHumanControlFrame(context.Background(), d, a, f)
	if _, v, p := OriginalHumanControlFrame(ctx, d, a); !v || !p {
		t.Fatal("original absent")
	}
	ctx = WithHumanControlFrame(ctx, d, a, HumanControlFrame{ID: f.ID, Generation: 2})
	if _, v, p := OriginalHumanControlFrame(ctx, d, a); v || !p {
		t.Fatal("new generation replaced original")
	}
	ctx = WithHumanControlFrame(ctx, d, a, f)
	if _, v, _ := OriginalHumanControlFrame(ctx, d, a); v {
		t.Fatal("invalid binding revived")
	}
	raw, _ := json.Marshal(HumanControlReceipt{OperationID: a.ID, Family: "BLOCK", Status: "STOPPED"})
	if string(raw) != `{"operationId":"`+a.ID+`","family":"BLOCK","status":"STOPPED","appliedVersion":0}` {
		t.Fatal("stop leaked mutable selector", string(raw))
	}
}
func TestHumanControlGrantAndPairClosedMaxTerminal(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	grant := `{"operationId":"` + id + `","action":"REVOKE","grantId":"` + id + `","expectedVersion":8999999999999999}`
	if _, e := DecodeGrantRevokeControl([]byte(grant), false); e != nil {
		t.Fatal("terminal revoke", e)
	}
	block := `{"operationId":"` + id + `","action":"UNBLOCK","accountId":"` + id + `","expectedVersion":8999999999999999}`
	if _, e := DecodeBlockControl([]byte(block), false); e != nil {
		t.Fatal("terminal unblock", e)
	}
	if _, e := DecodeBlockControl([]byte(strings.Replace(block, "UNBLOCK", "BLOCK", 1)), false); e == nil {
		t.Fatal("nonterminal overflow")
	}
	for n, b := range map[string]string{"missingVersion": strings.Replace(grant, `,"expectedVersion":8999999999999999`, "", 1), "unknownActor": strings.Replace(grant, `"action":"REVOKE"`, `"action":"REVOKE","actorId":"`+id+`"`, 1), "null": strings.Replace(grant, `"grantId":"`+id+`"`, `"grantId":null`, 1), "duplicate": strings.Replace(grant, `"action":"REVOKE"`, `"action":"REVOKE","action":"REVOKE"`, 1)} {
		t.Run(n, func(t *testing.T) {
			if _, e := DecodeGrantRevokeControl([]byte(b), false); e == nil {
				t.Fatal("unsafe body accepted")
			}
		})
	}
	if HumanControlNextVersion(HumanControlMaxVersion) != HumanControlMaxVersion {
		t.Fatal("terminal cap exceeded")
	}
}
