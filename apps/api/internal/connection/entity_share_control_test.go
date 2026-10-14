package connection

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEntityShareControlClosedSevenKindsAndRecipientBinding(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	op := "22222222-2222-2222-2222-222222222222"
	target := EntityShareRecipient{Type: "CONVERSATION", ID: id}
	body := `{"operationId":"` + op + `","entity":{"type":"place","id":"` + id + `"}}`
	for _, kind := range []string{"activity", "place", "person", "community", "organization", "business", "moment"} {
		t.Run(kind, func(t *testing.T) {
			v, e := DecodeEntityShareControl([]byte(strings.Replace(body, `"place"`, `"`+kind+`"`, 1)), target, false)
			if e != nil || v.Entity.Type != kind {
				t.Fatal(v, e)
			}
		})
	}
	v, e := DecodeEntityShareControl([]byte(body), target, false)
	if e != nil {
		t.Fatal(e)
	}
	digest := EntityShareControlDigest(target, v)
	if digest == EntityShareControlDigest(EntityShareRecipient{Type: "FRIEND_TIE", ID: id}, v) || digest == EntityShareControlDigest(EntityShareRecipient{Type: "CONVERSATION", ID: op}, v) {
		t.Fatal("recipient namespace absent")
	}
	for n, b := range map[string]string{"snapshotTitle": strings.Replace(body, `"type":"place"`, `"type":"place","title":"旧标题"`, 1), "URL": strings.Replace(body, `"type":"place"`, `"type":"place","url":"https://example.invalid"`, 1), "actor": strings.Replace(body, `"operationId"`, `"actorId":"`+id+`","operationId"`, 1), "duplicate": strings.Replace(body, `"type":"place"`, `"type":"place","type":"person"`, 1), "unsupported": strings.Replace(body, `"place"`, `"journey"`, 1), "null": strings.Replace(body, `"entity":{"type":"place","id":"`+id+`"}`, `"entity":null`, 1)} {
		t.Run(n, func(t *testing.T) {
			if _, e := DecodeEntityShareControl([]byte(b), target, false); e == nil {
				t.Fatal("borrowed payload accepted")
			}
		})
	}
	v.SourceVersion = strings.Repeat("a", 64)
	v.Approval = "specific"
	raw, _ := json.Marshal(v)
	if _, e = DecodeEntityShareControl(raw, target, true); e != nil {
		t.Fatal("closed commit", e)
	}
	v.SourceVersion = strings.Repeat("A", 64)
	if _, e = NormalizeEntityShareControl(target, v, true); e == nil {
		t.Fatal("noncanonical source")
	}
	target.Type = "ACCOUNT"
	if ValidEntityShareRecipient(target) {
		t.Fatal("arbitrary account used as relationship")
	}
}
func TestEntityShareControlTinyStoppedAndOriginalSuccess(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	target := EntityShareRecipient{Type: "FRIEND_TIE", ID: id}
	v := EntityShareControlReceipt{OperationID: id, Status: "STOPPED", Recipient: target}
	if !ValidEntityShareControlReceipt(v, target, id) {
		t.Fatal("stop invalid")
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "message") || strings.Contains(string(raw), "entity") || strings.Contains(string(raw), "title") {
		t.Fatal("stop contains snapshot", string(raw))
	}
	v.MessageID = id
	if ValidEntityShareControlReceipt(v, target, id) {
		t.Fatal("stop claims actual effect")
	}
	if _, e := DecodeEntityShareControl([]byte(`{"operationId":"`+id+`","entity":{"type":"place","id":"`+id+`"},"approval":"forged"}`), target, false); e == nil {
		t.Fatal("caller approved preview")
	}
}
