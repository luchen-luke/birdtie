package connection

import (
	"strings"
	"testing"
	"time"
)

const messageOwner = "11111111-1111-4111-8111-111111111111"
const messageConv = "33333333-3333-4333-8333-333333333333"
const messageOp = "55555555-5555-4555-8555-555555555555"

func TestHumanMessageOperationBinding(t *testing.T) {
	m := Message{ID: "66666666-6666-4666-8666-666666666666", ConversationID: messageConv, SenderID: messageOwner, Body: "本地消息", CreatedAt: time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC)}
	r := NewMessageOperationReceipt(messageOwner, messageOp, m)
	if !ValidMessageOperationReceipt(r, messageOwner, messageConv, messageOp) {
		t.Fatal("original effect rejected")
	}
	if HumanMessageEffectKey(messageOwner, messageOp) == HumanMessageEffectKey(messageConv, messageOp) {
		t.Fatal("owner is required")
	}
	if HumanMessagePayloadDigest(messageConv, m.Body) == HumanMessagePayloadDigest(messageOwner, m.Body) {
		t.Fatal("target must bind payload")
	}
	r.PayloadDigest = "z" + r.PayloadDigest[1:]
	if ValidMessageOperationReceipt(r, messageOwner, messageConv, messageOp) {
		t.Fatal("bad digest accepted")
	}
}
func TestHumanMessageOperationClosedInput(t *testing.T) {
	for _, raw := range []string{`{"operationId":"` + messageOp + `","body":"明确信息"}`, `{"operationId":"` + messageOp + `","body":"a","ownerId":"` + messageOwner + `"}`, `{"operationId":"` + messageOp + `","body":"a","body":"b"}`, `{"OperationId":"` + messageOp + `","body":"a"}`, `{"operationId":"` + messageOp + `","body":null}`, `{"operationId":"` + messageOp + `","body":" a "}`} {
		in, e := DecodeMessageOperation([]byte(raw))
		valid := strings.Contains(raw, "明确信息")
		if (e == nil) != valid || (valid && !ValidMessageOperation(in)) {
			t.Fatalf("closed input %s err=%v", raw, e)
		}
	}
	if ValidMessageOperation(MessageOperationInput{messageOp, strings.Repeat("汉", 667)}) {
		t.Fatal("UTF8 byte bound lost")
	}
}

func TestHumanMessageOperationUppercaseRejectedBeforeAddress(t *testing.T) {
	lower := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	upper := strings.ToUpper(lower)
	if !ValidMessageOperation(MessageOperationInput{lower, "明确发送"}) || ValidMessageOperation(MessageOperationInput{upper, "明确发送"}) {
		t.Fatal("operation canonical closure lost")
	}
	if _, e := DecodeMessageOperation([]byte(`{"operationId":"` + upper + `","body":"明确发送"}`)); e == nil {
		t.Fatal("uppercase must not create another effect address")
	}
}
