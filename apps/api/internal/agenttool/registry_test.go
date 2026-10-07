package agenttool

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"strings"
	"testing"
	"time"
)

func TestAgentToolSandboxSchemaDescribesCompleteProposalAndDecisionWire(t *testing.T) {
	d, _ := Lookup(SandboxWrite)
	now := time.Now().UTC()
	p := SandboxProposal{ActionID: "11111111-1111-4111-8111-111111111111", LogicalOperationID: "22222222-2222-4222-8222-222222222222", TargetID: "33333333-3333-4333-8333-333333333333", Value: "待本人确认的本地沙箱版本", ResourceVersion: "original-native-source"}
	if !p.Valid() {
		t.Fatal("closed proposal fixture invalid")
	}
	decision := Decision{SchemaVersion: Schema, DecisionID: p.ActionID, Disposition: Confirm, ReasonCodes: []string{"EXACT_VERSION_APPROVAL_REQUIRED_NO_EFFECT"}, Tool: SandboxWrite, ToolVersion: d.Version, ActionID: p.ActionID, LogicalOperationID: p.LogicalOperationID, ActorID: p.TargetID, AgentID: p.ActionID, SubjectType: "PERSON", SubjectID: p.TargetID, PolicyVersion: "native-policy", ResourceVersion: p.ResourceVersion, ArgumentsDigest: Digest(p), Purpose: d.Purpose, DataDestinations: []string{"LOCAL_OWNER"}, ObservedAt: now, ExpiresAt: now.Add(time.Second)}
	for _, item := range []struct {
		value  any
		schema json.RawMessage
	}{{p, d.InputSchema}, {decision, d.OutputSchema}, {Decision{SchemaVersion: Schema, Disposition: Deny, ReasonCodes: []string{"DENIED"}}, d.OutputSchema}} {
		var s struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
			Additional bool                       `json:"additionalProperties"`
		}
		if json.Unmarshal(item.schema, &s) != nil || s.Additional {
			t.Fatal("schema must close unknown fields")
		}
		raw, _ := json.Marshal(item.value)
		var wire map[string]json.RawMessage
		if json.Unmarshal(raw, &wire) != nil {
			t.Fatal("invalid actual Go wire")
		}
		for key := range wire {
			if _, ok := s.Properties[key]; !ok {
				t.Fatal("actual field not declared", key)
			}
		}
		for _, key := range s.Required {
			if _, ok := wire[key]; !ok {
				t.Fatal("required field missing", key)
			}
		}
		for _, forbidden := range []string{"approved", "executed", "effect_key", "permission_override"} {
			if _, ok := s.Properties[forbidden]; ok {
				t.Fatal("schema grants authority", forbidden)
			}
		}
	}
}

// Metadata/wire field coverage ONLY, not native authority or JSON-schema engine.
func checkCurrentReadDeclaredWire(t *testing.T, s map[string]any, v any, path string) {
	t.Helper()
	if c, ok := s["const"]; ok && c != v {
		t.Fatalf("%s const differs: %v/%v", path, c, v)
	}
	if enum, ok := s["enum"].([]any); ok {
		found := false
		for _, c := range enum {
			if c == v {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s enum differs: %v", path, v)
		}
	}
	switch value := v.(type) {
	case map[string]any:
		if s["additionalProperties"] != false {
			t.Fatal("nested object is not closed", path)
		}
		props, ok := s["properties"].(map[string]any)
		if !ok {
			t.Fatal("properties missing", path)
		}
		for k, v := range value {
			p, ok := props[k].(map[string]any)
			if !ok {
				t.Fatal("actual wire field undeclared", path, k)
			}
			checkCurrentReadDeclaredWire(t, p, v, path+"."+k)
		}
		for _, k := range s["required"].([]any) {
			if _, ok := value[k.(string)]; !ok {
				t.Fatal("actual required wire field missing", path, k)
			}
		}
		for _, k := range []string{"approved", "executed", "permission_override", "actor_id", "token", "latitudePrecise"} {
			if _, ok := props[k]; ok {
				t.Fatal("extra authority/private field", path, k)
			}
		}
	case []any:
		item, ok := s["items"].(map[string]any)
		if !ok {
			t.Fatal("array item schema missing", path)
		}
		for _, v := range value {
			checkCurrentReadDeclaredWire(t, item, v, path+"[]")
		}
	}
}

func TestCurrentReadonlyRegistryDescribesOriginalNestedWireWithoutModelSchemaChange(t *testing.T) {
	now := time.Now().UTC()
	for _, kind := range []string{"person", "place"} {
		t.Run(kind, func(t *testing.T) {
			ref := arp.Ref{Type: kind, ID: currentReadEntity}
			item := arp.Item{Entity: ref, Title: "原对象", Summary: "原领域明确可见", Scope: arp.AuthorizedView, Detail: &ref, Share: &ref, SourceVersion: strings.Repeat("a", 64), ActionsSourceVersion: strings.Repeat("a", 64), ActionsValidUntil: &now}
			item.Actions = ea.Project(ea.Ref{Type: kind, ID: ref.ID}, ea.Facts{}, now, now.Add(time.Second), item.Title, item.SourceVersion).Actions
			if kind == "place" {
				item.Anchor = &arp.Anchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 57, Longitude: -2, PlaceID: ref.ID}
			}
			task := agentworkspace.Task{ID: currentReadTask, Filters: map[string]string{"category": "badminton", "timePreference": "weekend", "distancePreference": "closer", "locationPreference": "viewport", "targetIntent": agentworkspace.FindPlace, "currentQuery": "原输入", "searchTerm": "原输入", "mapWest": "-3", "mapSouth": "56", "mapEast": "-1", "mapNorth": "58", "PRIVATE_CANARY": "do not expose"}, UpdatedAt: now}
			out := agentworkspace.WithContract(agentworkspace.Results{Query: "原输入", CityID: "city_fixture", NativeProjection: true, ProjectionItems: []arp.Item{item}}, task, "request_fixture")
			if _, ok := out.ResultSet.Filters["PRIVATE_CANARY"]; ok {
				t.Fatal("original field sanitizer lost")
			}
			d, _ := Lookup(CurrentSearchTool(kind))
			var schema map[string]any
			var wire any
			json.Unmarshal(d.OutputSchema, &schema)
			raw, _ := json.Marshal(out.ResultSet)
			json.Unmarshal(raw, &wire)
			checkCurrentReadDeclaredWire(t, schema, wire, "ResultSet")
			if d.Operation != "OBSERVE" || d.ResourceScope == "CURRENT_PUBLIC_ACTIVITY" {
				t.Fatal("new read descriptor acquired different permission", d)
			}
		})
	}
	r := newpeople.BuildResponse(newpeople.Signal{IntentID: currentReadTask, AccountID: currentReadOwner, Category: "badminton", Modality: "ONLINE"}, []newpeople.Signal{{IntentID: currentReadEntity, AccountID: currentReadEntity, Category: "badminton", Modality: "ONLINE", DisplayName: "声明相容"}})
	d, _ := Lookup(PersonMatch)
	var schema map[string]any
	var wire any
	json.Unmarshal(d.OutputSchema, &schema)
	raw, _ := json.Marshal(r)
	json.Unmarshal(raw, &wire)
	checkCurrentReadDeclaredWire(t, schema, wire, "NewPeople")
	for _, tool := range []string{agentplanner.ActivitySearch, agentplanner.ActivityDetail} {
		d, _ := Lookup(tool)
		if strings.Contains(string(d.OutputSchema), "person") || strings.Contains(string(d.OutputSchema), "place") || !strings.Contains(string(d.OutputSchema), `"visibility":{"const":"public"}`) {
			t.Fatal("original model public activity schema broadened")
		}
	}
}

func TestAgentToolClosedRegistryCompleteAndImmutable(t *testing.T) {
	values := Catalogue()
	if len(values) != 6 {
		t.Fatal("closed catalogue changed")
	}
	writes := 0
	for _, d := range values {
		if d.Tool == "" || d.Version == "" || !json.Valid(d.InputSchema) || !json.Valid(d.OutputSchema) || d.ResourceScope == "" || d.Risk == "" || len(d.RequiredPermissions) == 0 || d.Purpose == "" || d.Idempotency == "" || d.Reconciliation == "" {
			t.Fatal("incomplete metadata", d)
		}
		if d.Kind == "WRITE" {
			writes++
			if d.Tool != SandboxWrite {
				t.Fatal("real write registered")
			}
		}
		d.InputSchema[0] = 'x'
		d.RequiredPermissions[0] = "ALLOW_ALL"
		next, _ := Lookup(d.Tool)
		if !json.Valid(next.InputSchema) || next.RequiredPermissions[0] == "ALLOW_ALL" {
			t.Fatal("mutable registry")
		}
	}
	if writes != 1 {
		t.Fatal("sandbox must be only write descriptor")
	}
	for _, tool := range []string{"message.send", "profile.update", "shell.execute", "http.get", "memory_candidate.accept", "activity.rsvp", "Activity.Search", "", "activity.search "} {
		if _, ok := Lookup(tool); ok {
			t.Fatal("unknown tool registered", tool)
		}
	}
}
func TestAgentToolProposalUntrustedAuthorityRejected(t *testing.T) {
	p := agentplanner.Proposal("11111111-1111-4111-8111-111111111111", 1, agentplanner.ActivitySearch, agentplanner.Arguments{Query: "找羽毛球", CityID: "city_fixture"}, "native-source", "只读")
	raw, _ := json.Marshal(p)
	decoded, e := DecodeProposal(raw)
	if e != nil || decoded != p {
		t.Fatal("valid closed proposal rejected", e)
	}
	for _, field := range []string{"confirmed", "requires_confirmation", "permission_override", "actor_id", "OCR", "image", "third_party_message", "code"} {
		t.Run(field, func(t *testing.T) {
			for _, suffix := range []string{`,"` + field + `":true}`, `,"arguments":{"query":"找羽毛球","city_id":"city_fixture","` + field + `":"allow"}}`} {
				polluted := strings.TrimSuffix(string(raw), "}") + suffix
				if _, e := DecodeProposal([]byte(polluted)); e == nil {
					t.Fatal("pollution accepted", field)
				}
			}
		})
	}
	for _, raw := range []string{`{}`, `null`, string(raw) + `{}`, strings.Replace(string(raw), `"tool":`, `"tool":"shell.execute","tool":`, 1)} {
		if _, e := DecodeProposal([]byte(raw)); e == nil {
			t.Fatal("invalid or duplicate keys accepted")
		}
	}
	if Digest(p) == Digest(agentplanner.Proposal(p.LogicalOperationID, 1, p.Tool, agentplanner.Arguments{Query: "换一个查询", CityID: "city_fixture"}, p.ResourceVersion, p.ReasonSummary)) {
		t.Fatal("changed arguments share digest")
	}
}
