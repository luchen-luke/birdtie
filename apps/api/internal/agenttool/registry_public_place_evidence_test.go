package agenttool

import (
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Metadata field/type/closed-bound coverage of the actual public producer;
// this is not an invocation decoder, authorization check or DB fixture.
func checkRegistryPublicPresentationWire(t *testing.T, schema map[string]any, value any, path string) {
	t.Helper()
	if c, ok := schema["const"]; ok && !reflect.DeepEqual(c, value) {
		t.Fatal("actual constant differs", path, value)
	}
	if choices, ok := schema["enum"].([]any); ok {
		found := false
		for _, c := range choices {
			found = found || reflect.DeepEqual(c, value)
		}
		if !found {
			t.Fatal("actual enum differs", path, value)
		}
	}
	switch v := value.(type) {
	case map[string]any:
		if schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Fatal("actual object not closed", path)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatal("actual object fields missing", path)
		}
		for key, child := range v {
			s, ok := properties[key].(map[string]any)
			if !ok {
				t.Fatal("actual public field undeclared", path, key)
			}
			checkRegistryPublicPresentationWire(t, s, child, path+"."+key)
		}
		for _, key := range schema["required"].([]any) {
			if _, ok := v[key.(string)]; !ok {
				t.Fatal("required actual field missing", path, key)
			}
		}
	case []any:
		if schema["type"] != "array" {
			t.Fatal("actual array differs", path)
		}
		if max, ok := schema["maxItems"].(float64); ok && len(v) > int(max) {
			t.Fatal("actual array exceeds public bound", path)
		}
		for _, child := range v {
			checkRegistryPublicPresentationWire(t, schema["items"].(map[string]any), child, path+"[]")
		}
	case string:
		if _, ok := schema["const"]; !ok && schema["enum"] == nil && schema["type"] != "string" {
			t.Fatal("actual string metadata missing", path)
		}
		if pattern, ok := schema["pattern"].(string); ok && !regexp.MustCompile(pattern).MatchString(v) {
			t.Fatal("actual public string outside closed pattern", path)
		}
	case float64:
		if _, ok := schema["const"]; !ok && schema["type"] != "integer" && schema["type"] != "number" {
			t.Fatal("actual public number metadata missing", path)
		}
		if schema["type"] == "integer" && math.Trunc(v) != v {
			t.Fatal("actual counter not integer", path)
		}
		if max, ok := schema["maximum"].(float64); ok && v > max {
			t.Fatal("actual counter exceeds bound", path)
		}
		if min, ok := schema["minimum"].(float64); ok && v < min {
			t.Fatal("actual counter below bound", path)
		}
	case bool:
		if _, ok := schema["const"]; !ok && schema["type"] != "boolean" {
			t.Fatal("actual boolean metadata missing", path)
		}
	default:
		t.Fatal("unexpected actual public presentation type", path)
	}
}

func TestCurrentReadonlyRegistryDescribesOriginalPublicPlaceEvidenceProducer(t *testing.T) {
	for _, mode := range []string{"current", "source_expiry", "unknown_source_time", "no_public_ref"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			task := agentworkspace.Task{ID: currentReadTask, PrincipalType: "PERSON", PrincipalID: currentReadOwner, ActingUserID: currentReadOwner,
				CityID: "city_fixture", ContextType: "CITY", ContextID: "city_fixture", Intent: agentworkspace.FindPlace,
				Status: agentworkspace.TaskCompleted, Query: "原地点问题", UpdatedAt: now.Add(-time.Second), Filters: map[string]string{}, Conversation: []agentworkspace.Message{}}
			rawTask, _ := json.Marshal(task)
			ref := arp.Ref{Type: "place", ID: currentReadEntity}
			r := arp.Receipt{Places: []foundation.Place{{ID: ref.ID, Name: "原公开地点", CategoryCode: "culture", Source: foundation.Source{UpdatedAt: now.Add(-time.Second)}}},
				Items: []arp.Item{{Entity: ref, Title: "原公开地点", Summary: "原公开记录", Scope: arp.AuthorizedView, Detail: &ref}}, PublicCommercialRefs: []arp.Ref{ref},
				ObservedAt: now, ValidUntil: now.Add(20 * time.Second), Proof: strings.Repeat("a", 64), Seal: strings.Repeat("b", 64)}
			switch mode {
			case "source_expiry":
				expires := now.Add(time.Minute)
				r.Places[0].Source.ExpiresAt = &expires
			case "unknown_source_time":
				r.Places[0].Source.UpdatedAt = time.Time{}
			case "no_public_ref":
				r.PublicCommercialRefs = []arp.Ref{}
			}
			p, e := arp.BuildPublicFieldEvidence(arp.Access{Actor: identity.Actor{ID: currentReadOwner, AccountType: "person"}, SessionDigest: [32]byte{1},
				TaskID: task.ID, ExpectedTask: rawTask}, arp.Query{Kind: "place", CityID: task.CityID}, r)
			if e != nil || p == nil {
				t.Fatal("actual public evidence producer", e)
			}
			result := agentworkspace.WithContract(agentworkspace.Results{PrincipalType: "PERSON", PrincipalID: currentReadOwner,
				NativeProjection: true, ProjectionItems: r.Items, PublicCommercialRefs: r.PublicCommercialRefs, PublicFieldEvidence: p,
				Places: r.Places, Query: task.Query, CityID: task.CityID}, task, "request_fixture")
			if result.ResultSet.PublicFieldEvidence == nil {
				t.Fatal("actual original same-task producer omitted public metadata")
			}
			d, _ := Lookup(PlaceSearch)
			var schema map[string]any
			var wire any
			json.Unmarshal(d.OutputSchema, &schema)
			raw, e := json.Marshal(result.ResultSet)
			if e != nil || json.Unmarshal(raw, &wire) != nil {
				t.Fatal(e)
			}
			checkRegistryPublicPresentationWire(t, schema, wire, "ResultSet")
			if p.ModelAccess != "UNAVAILABLE" || p.MediaAccess != "UNAVAILABLE" || p.GrantsAuthority {
				t.Fatal("public presentation evidence granted authority")
			}
		})
	}
}

func TestCurrentReadonlyRegistryPublicPlaceMetadataExcludesPrivateShapes(t *testing.T) {
	d, _ := Lookup(PlaceSearch)
	for _, forbidden := range []string{`"rowToken"`, `"latitudePrecise"`, `"confidence"`, `"capturedAt"`, `"claimantId"`, `"privateCityHistory"`, `"profile"`, `"authorization_generation"`, `"approved"`, `"executed"`, `"permission_override"`} {
		if strings.Contains(string(d.OutputSchema), forbidden) {
			t.Fatal("private or authority shape described by public Place metadata", forbidden)
		}
	}
	person, _ := Lookup(PersonSearch)
	if strings.Contains(string(person.OutputSchema), "publicFieldEvidence") {
		t.Fatal("Place public field evidence broadened Person output")
	}
}
