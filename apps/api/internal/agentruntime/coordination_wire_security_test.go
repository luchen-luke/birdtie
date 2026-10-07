package agentruntime

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

const coordinationWireSecurityRequestID = "a1111111-1111-4111-8111-111111111111"

func coordinationWireSecurityRequest() map[string]any {
	return map[string]any{
		"version":   "agent-coordination-v1",
		"requestId": coordinationWireSecurityRequestID,
		"taskId":    "b2222222-2222-4222-8222-222222222222",
		"sender": map[string]any{
			"agentId":   "c3333333-3333-4333-8333-333333333333",
			"principal": map[string]any{"type": "PERSON", "id": "d4444444-4444-4444-8444-444444444444"},
		},
		"recipient": map[string]any{
			"agentId":   "e5555555-5555-4555-8555-555555555555",
			"principal": map[string]any{"type": "PERSON", "id": "f6666666-6666-4666-8666-666666666666"},
		},
		"purpose":  "ASK_ACTIVITY_INTEREST",
		"scope":    "CONNECTION",
		"resource": map[string]any{"type": "ACTIVITY", "id": "77777777-7777-4777-8777-777777777777"},
		"fields":   []any{"NEXT_STEP"},
		// Parsing is structural. It must preserve this explicit timestamp;
		// server-clock expiry/TTL authorization belongs to DecideCoordination.
		"expiresAt": "2000-01-01T00:05:00Z",
	}
}

func coordinationWireSecurityEncode(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func coordinationWireSecurityObject(root map[string]any, path ...string) map[string]any {
	object := root
	for _, key := range path {
		object = object[key].(map[string]any)
	}
	return object
}

func coordinationWireSecurityRejectRequest(t *testing.T, raw []byte) {
	t.Helper()
	got, err := DecodeCoordinationRequest(raw)
	if err == nil {
		t.Fatalf("untrusted wire request accepted: %s", raw)
	}
	if !reflect.DeepEqual(got, CoordinationRequest{}) {
		t.Fatal("decoder returned partly decoded authority or payload after rejection", got)
	}
	if strings.Contains(err.Error(), "PRIVATE_WIRE_CANARY") {
		t.Fatal("error disclosed private request text")
	}
}

func TestCoordinationWireSecurityValidRequestAndUUIDNormalization(t *testing.T) {
	body := coordinationWireSecurityRequest()
	for _, path := range [][]string{
		{"requestId"}, {"taskId"}, {"sender", "agentId"}, {"sender", "principal", "id"},
		{"recipient", "agentId"}, {"recipient", "principal", "id"}, {"resource", "id"},
	} {
		object := coordinationWireSecurityObject(body, path[:len(path)-1]...)
		key := path[len(path)-1]
		object[key] = strings.ToUpper(object[key].(string))
	}
	got, err := DecodeCoordinationRequest(coordinationWireSecurityEncode(t, body))
	if err != nil {
		t.Fatal("valid structural request rejected", err)
	}
	if got.RequestID != coordinationWireSecurityRequestID || got.Sender.Principal.Type != actorref.Person ||
		got.Sender.Principal.ID != "d4444444-4444-4444-8444-444444444444" ||
		got.Recipient.AgentID != "e5555555-5555-4555-8555-555555555555" ||
		!got.ExpiresAt.Equal(time.Date(2000, 1, 1, 0, 5, 0, 0, time.UTC)) {
		t.Fatal("UUID normalization or explicit timestamp changed", got)
	}
	if decision := DecideCoordination(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), got, CoordinationFacts{}); decision.Allowed {
		t.Fatal("parsed declarations granted authority without trusted facts")
	}
}

func TestCoordinationWireSecurityRejectsUnknownPrivateAndAuthorityFields(t *testing.T) {
	for _, field := range []string{
		"facts", "consent", "grant", "explicitGrant", "authorityVerified", "released", "relationship",
		"role", "permissions", "ownerId", "actorUserId", "principalId", "confirmed", "calendar",
		"privateMemory", "chatBody", "location", "latitude", "longitude", "contacts", "interests",
		"profile", "availability", "accessToken", "payload", "action", "message", "RequestId", "Scope",
	} {
		t.Run("top/"+field, func(t *testing.T) {
			body := coordinationWireSecurityRequest()
			body[field] = "PRIVATE_WIRE_CANARY"
			coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
		})
	}
	for _, path := range [][]string{{"sender"}, {"recipient"}, {"sender", "principal"}, {"recipient", "principal"}, {"resource"}} {
		for _, field := range []string{"facts", "consent", "relationship", "role", "permissions", "privateMemory", "AgentId", "Type", "ID"} {
			t.Run(strings.Join(path, "/")+"/"+field, func(t *testing.T) {
				body := coordinationWireSecurityRequest()
				coordinationWireSecurityObject(body, path...)[field] = "PRIVATE_WIRE_CANARY"
				coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
			})
		}
	}
}

func TestCoordinationWireSecurityRejectsMissingWrongTypesAndReferences(t *testing.T) {
	for _, field := range []string{"version", "requestId", "taskId", "sender", "recipient", "purpose", "scope", "resource", "fields", "expiresAt"} {
		t.Run("missing/"+field, func(t *testing.T) {
			body := coordinationWireSecurityRequest()
			delete(body, field)
			coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
		})
		for _, invalid := range []any{nil, false, 1, map[string]any{}} {
			t.Run("invalid-type/"+field+"/"+string(coordinationWireSecurityEncode(t, invalid)), func(t *testing.T) {
				body := coordinationWireSecurityRequest()
				body[field] = invalid
				coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
			})
		}
	}
	for _, path := range [][]string{
		{"requestId"}, {"taskId"}, {"sender", "agentId"}, {"sender", "principal", "id"},
		{"recipient", "agentId"}, {"recipient", "principal", "id"}, {"resource", "id"},
	} {
		for _, invalid := range []any{"", "not-a-uuid", "11111111-1111-4111-8111", "00000000-0000-0000-0000-000000000000", true, 10, nil} {
			t.Run("reference/"+strings.Join(path, "/")+"/"+string(coordinationWireSecurityEncode(t, invalid)), func(t *testing.T) {
				body := coordinationWireSecurityRequest()
				coordinationWireSecurityObject(body, path[:len(path)-1]...)[path[len(path)-1]] = invalid
				coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
			})
		}
	}
	for _, path := range [][]string{{"sender", "principal"}, {"recipient", "principal"}} {
		for _, kind := range []string{"", "person", "ORGANIZATION", "BUSINESS", "COMMUNITY", "CITY", "UNKNOWN"} {
			t.Run("kind/"+strings.Join(path, "/")+"/"+kind, func(t *testing.T) {
				body := coordinationWireSecurityRequest()
				coordinationWireSecurityObject(body, path...)["type"] = kind
				coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
			})
		}
	}
	for _, path := range [][]string{{"sender"}, {"recipient"}, {"sender", "principal"}, {"recipient", "principal"}, {"resource"}} {
		for key := range coordinationWireSecurityObject(coordinationWireSecurityRequest(), path...) {
			t.Run("nested-missing/"+strings.Join(path, "/")+"/"+key, func(t *testing.T) {
				body := coordinationWireSecurityRequest()
				delete(coordinationWireSecurityObject(body, path...), key)
				coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
			})
		}
	}
}

func TestCoordinationWireSecurityEnumsAndBodyLimits(t *testing.T) {
	for field, invalids := range map[string][]any{
		"version":   {"", "agent-coordination-v2", "AGENT-COORDINATION-V1"},
		"purpose":   {"", "ASK_AVAILABILITY", "ask_activity_interest", "RESERVE_VENUE", "READ_MEMORY"},
		"scope":     {"", "PUBLIC", "PRIVATE", "CLOSE", "WORKSPACE_PRIVATE", "connection"},
		"fields":    {[]any{}, []any{"PRIVATE_MEMORY"}, []any{"NEXT_STEP", "NEXT_STEP"}, []any{"NEXT_STEP", "CALENDAR"}, []any{1}, "NEXT_STEP"},
		"expiresAt": {"", "not-a-date", "0001-01-01T00:00:00Z"},
	} {
		for _, invalid := range invalids {
			t.Run(field+"/"+string(coordinationWireSecurityEncode(t, invalid)), func(t *testing.T) {
				body := coordinationWireSecurityRequest()
				body[field] = invalid
				coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
			})
		}
	}
	for _, kind := range []string{"", "activity", "PLACE", "PERSON", "INTENT", "PRIVATE_MEMORY"} {
		t.Run("resource-type/"+kind, func(t *testing.T) {
			body := coordinationWireSecurityRequest()
			coordinationWireSecurityObject(body, "resource")["type"] = kind
			coordinationWireSecurityRejectRequest(t, coordinationWireSecurityEncode(t, body))
		})
	}
	valid := coordinationWireSecurityEncode(t, coordinationWireSecurityRequest())
	for _, raw := range [][]byte{nil, []byte("null"), []byte("[]"), []byte(`"text"`), []byte(`{"version":`),
		append(append([]byte{}, valid...), []byte(` {}`)...), append(append([]byte{}, valid...), []byte(` null`)...),
		append(bytes.Repeat([]byte(" "), 2048), valid...)} {
		coordinationWireSecurityRejectRequest(t, raw)
	}
}

func TestCoordinationWireSecurityRejectsDuplicateKeysAtEveryDepth(t *testing.T) {
	valid := string(coordinationWireSecurityEncode(t, coordinationWireSecurityRequest()))
	for _, needle := range []string{
		`"version":"agent-coordination-v1"`, `"requestId":"` + coordinationWireSecurityRequestID + `"`,
		`"scope":"CONNECTION"`, `"purpose":"ASK_ACTIVITY_INTEREST"`, `"fields":["NEXT_STEP"]`,
		`"agentId":"c3333333-3333-4333-8333-333333333333"`,
		`"agentId":"e5555555-5555-4555-8555-555555555555"`,
		`"id":"d4444444-4444-4444-8444-444444444444"`,
		`"id":"f6666666-6666-4666-8666-666666666666"`,
		`"type":"PERSON"`, `"type":"ACTIVITY"`,
	} {
		t.Run(needle, func(t *testing.T) {
			requirePresent := strings.Contains(valid, needle)
			if !requirePresent {
				t.Fatalf("duplicate fixture key absent: %s", needle)
			}
			coordinationWireSecurityRejectRequest(t, []byte(strings.Replace(valid, needle, needle+","+needle, 1)))
		})
	}
	// Different values cannot authorize via last-key-wins either.
	coordinationWireSecurityRejectRequest(t, []byte(strings.Replace(valid, `"scope":"CONNECTION"`, `"scope":"PRIVATE","scope":"CONNECTION"`, 1)))
}

func TestCoordinationWireSecurityResponseIsOnlyBoundAskUser(t *testing.T) {
	valid := map[string]any{"version": "agent-coordination-v1", "requestId": coordinationWireSecurityRequestID, "nextStep": "ASK_USER"}
	raw := coordinationWireSecurityEncode(t, valid)
	got, err := DecodeCoordinationResponse(raw, coordinationWireSecurityRequestID)
	if err != nil || got.NextStep != "ASK_USER" || got.RequestID != coordinationWireSecurityRequestID {
		t.Fatal("valid minimum response rejected", got, err)
	}
	encoded := coordinationWireSecurityEncode(t, got)
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil || !reflect.DeepEqual(object, valid) {
		t.Fatal("response expanded beyond minimal contract", string(encoded), err)
	}
	reject := func(body []byte, expected string) {
		t.Helper()
		got, err := DecodeCoordinationResponse(body, expected)
		if err == nil || !reflect.DeepEqual(got, CoordinationResponse{}) {
			t.Fatal("untrusted response accepted or returned partial payload", string(body), got, err)
		}
		if strings.Contains(err.Error(), "PRIVATE_WIRE_CANARY") {
			t.Fatal("private response text included in error")
		}
	}
	for _, field := range []string{"status", "interested", "availability", "calendar", "message", "reason", "privateMemory", "chat", "facts", "consent", "relationship", "grant", "profile", "location", "latitude", "longitude", "contacts", "responseId", "RequestId", "action", "payload"} {
		t.Run("extra/"+field, func(t *testing.T) {
			body := map[string]any{"version": "agent-coordination-v1", "requestId": coordinationWireSecurityRequestID, "nextStep": "ASK_USER", field: map[string]string{"text": "PRIVATE_WIRE_CANARY"}}
			reject(coordinationWireSecurityEncode(t, body), coordinationWireSecurityRequestID)
		})
	}
	for field, invalids := range map[string][]any{
		"version":   {"agent-coordination-v2", "", nil, 1},
		"requestId": {"", "not-a-uuid", "b2222222-2222-4222-8222-222222222222", nil, true},
		"nextStep":  {"", "INTERESTED", "HAS_AVAILABILITY", "SEND_INVITATION", "ask_user", nil, []string{"ASK_USER"}},
	} {
		for _, invalid := range invalids {
			t.Run("invalid/"+field+"/"+string(coordinationWireSecurityEncode(t, invalid)), func(t *testing.T) {
				body := map[string]any{"version": "agent-coordination-v1", "requestId": coordinationWireSecurityRequestID, "nextStep": "ASK_USER"}
				body[field] = invalid
				reject(coordinationWireSecurityEncode(t, body), coordinationWireSecurityRequestID)
			})
		}
	}
	for field := range valid {
		body := map[string]any{"version": "agent-coordination-v1", "requestId": coordinationWireSecurityRequestID, "nextStep": "ASK_USER"}
		delete(body, field)
		reject(coordinationWireSecurityEncode(t, body), coordinationWireSecurityRequestID)
	}
	for _, needle := range []string{`"version":"agent-coordination-v1"`, `"requestId":"` + coordinationWireSecurityRequestID + `"`, `"nextStep":"ASK_USER"`} {
		reject([]byte(strings.Replace(string(raw), needle, needle+","+needle, 1)), coordinationWireSecurityRequestID)
	}
	for _, malformed := range [][]byte{nil, []byte("null"), []byte("[]"), append(append([]byte{}, raw...), []byte(` {}`)...), append(bytes.Repeat([]byte(" "), 512), raw...)} {
		reject(malformed, coordinationWireSecurityRequestID)
	}
	for _, expected := range []string{"", "not-a-uuid", "b2222222-2222-4222-8222-222222222222"} {
		reject(raw, expected)
	}
}

func TestCoordinationWireSecurityFactsCannotBeWireAuthority(t *testing.T) {
	facts := CoordinationFacts{}
	for _, value := range []any{facts, &facts, map[string]any{"facts": facts}} {
		if raw, err := json.Marshal(value); err == nil {
			t.Fatal("server facts serialized into wire authority", string(raw))
		}
	}
	for _, raw := range []string{`{}`, `null`, `{"AuthorityVerified":true}`, `{"consent":true,"relationship":"CLOSE","privateMemory":"PRIVATE_WIRE_CANARY"}`} {
		if err := json.Unmarshal([]byte(raw), &facts); err == nil || strings.Contains(err.Error(), "PRIVATE_WIRE_CANARY") {
			t.Fatal("JSON created facts or leaked private input", raw, err)
		}
		if !reflect.DeepEqual(facts, CoordinationFacts{}) {
			t.Fatal("failed decode mutated server facts", facts)
		}
	}
}

func TestCoordinationWireSecurityDoesNotEnableRuntimeCapability(t *testing.T) {
	for _, kind := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business, actorref.Community} {
		policy := ForType(kind)
		for _, permission := range policy.PermissionStrings() {
			if strings.Contains(strings.ToLower(permission), "coordination") || strings.Contains(strings.ToLower(permission), "agent-to-agent") {
				t.Fatal("contract enabled a callable runtime capability", kind, permission)
			}
		}
		for _, forbidden := range []Capability{"agent_coordination.send", "agent_coordination.respond", "coordination.send", "agent_to_agent.request"} {
			if policy.Allows(forbidden) {
				t.Fatal("contract activated A2A transport", kind, forbidden)
			}
		}
		if (kind == actorref.Business || kind == actorref.Community) && policy.Available {
			t.Fatal("reserved/non-Agent role became callable", kind)
		}
	}
}
