package agentmessagepolicy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMessagePolicyUnitStrictPutContract(t *testing.T) {
	good := `{"expectedVersion":0,"incomingRequests":"SCREEN","expiresAt":"2026-10-07T01:02:03.123456+08:00"}`
	in, e := DecodePut([]byte(good))
	if e != nil || in.IncomingRequests != Screen || in.ExpiresAt.Location() != time.UTC {
		t.Fatal("known wire must normalize UTC without guessing")
	}
	bad := []string{"", `null`, `[]`, `{}`, good + good, good + " true", strings.Replace(good, "\"expectedVersion\":0", "\"expectedVersion\":-1", 1), strings.Replace(good, "\"expectedVersion\":0", "\"expectedVersion\":1.2", 1), strings.Replace(good, "\"expectedVersion\":0", "\"expectedVersion\":\"0\"", 1), strings.Replace(good, "SCREEN", "ALLOW", 1), strings.Replace(good, "SCREEN", "screen", 1), strings.Replace(good, ".123456", ".1234567", 1), strings.Replace(good, "0,", "null,", 1), strings.Replace(good, "0,", "false,", 1), strings.Replace(good, "0,", "0,\"expectedVersion\":1,", 1), strings.Replace(good, "0,", "0,\"expected\\u0056ersion\":1,", 1), strings.Replace(good, "0,", "0,\"ownerId\":\"foreign\",", 1), strings.Replace(good, "0,", "0,\"confirmed\":true,", 1), good[:len(good)-1]}
	for i, raw := range bad {
		if _, e := DecodePut([]byte(raw)); e != ErrInvalid {
			t.Fatalf("invalid wire %d accepted", i)
		}
	}
	if _, e := DecodePut(append([]byte(good), 0xff)); e != ErrInvalid {
		t.Fatal("invalid UTF8")
	}
	if _, e := DecodePut([]byte(strings.Repeat(" ", MaxBodyBytes) + good)); e != ErrInvalid {
		t.Fatal("oversize")
	}
	b, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	back, e := DecodePut(b)
	if e != nil || back != in {
		t.Fatal("normalized roundtrip")
	}
}
