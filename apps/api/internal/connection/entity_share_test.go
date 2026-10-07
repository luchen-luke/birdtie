package connection

import "testing"

func TestEntityShareClosedInput(t *testing.T) {
	valid := `{"operationId":"00000000-0000-4000-8000-000000000001","entity":{"type":"moment","id":"00000000-0000-4000-8000-000000000002"}}`
	if _, e := DecodeEntityShare([]byte(valid)); e != nil {
		t.Fatal("closed share input", e)
	}
	for _, raw := range []string{`null`, `{}`, valid + `{}`, `{"OperationId":"00000000-0000-4000-8000-000000000001","entity":{"type":"moment","id":"00000000-0000-4000-8000-000000000002"}}`, `{"operationId":"00000000-0000-4000-8000-000000000001","operationId":"00000000-0000-4000-8000-000000000001","entity":{"type":"moment","id":"00000000-0000-4000-8000-000000000002"}}`, `{"operationId":null,"entity":{"type":"moment","id":"00000000-0000-4000-8000-000000000002"}}`, `{"operationId":"00000000-0000-4000-8000-000000000001","entity":{"type":"moment","id":"00000000-0000-4000-8000-000000000002","confirmed":true}}`} {
		if _, e := DecodeEntityShare([]byte(raw)); e == nil {
			t.Fatal("invalid share shape accepted", raw)
		}
	}
}
