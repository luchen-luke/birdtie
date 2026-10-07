package agenttool

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
)

type Descriptor struct {
	Tool                string                  `json:"tool"`
	Version             string                  `json:"version"`
	InputSchema         json.RawMessage         `json:"input_schema"`
	OutputSchema        json.RawMessage         `json:"output_schema"`
	Kind                string                  `json:"kind"`
	ResourceScope       string                  `json:"resource_scope"`
	Risk                string                  `json:"risk"`
	RequiredPermissions []string                `json:"required_permissions"`
	Operation           agentautonomy.Operation `json:"autonomy_operation"`
	Purpose             string                  `json:"purpose"`
	Idempotency         string                  `json:"idempotency"`
	Reconciliation      string                  `json:"reconciliation"`
}

var catalogue = []Descriptor{
	{agentplanner.ActivitySearch, "activity.search.v1", json.RawMessage(`{"type":"object","additionalProperties":false,"required":["query","city_id"],"properties":{"query":{"type":"string","maxLength":240},"city_id":{"type":"string","maxLength":160}}}`), json.RawMessage(`{"type":"object","additionalProperties":false,"required":["schema_version","decision_id","action_id","tool","activities","observed_at","valid_until"],"properties":{"schema_version":{"const":"air.readonly_tool_result.v1"},"decision_id":{"type":"string"},"action_id":{"type":"string"},"tool":{"const":"activity.search"},"activities":{"type":"array","maxItems":200,"items":{"type":"object","required":["id","visibility"],"properties":{"id":{"type":"string","format":"uuid"},"visibility":{"const":"public"}}}},"observed_at":{"type":"string","format":"date-time"},"valid_until":{"type":"string","format":"date-time"}}}`), "READ", "CURRENT_PUBLIC_ACTIVITY", "LOW", []string{"CURRENT_PERSONAL_TASK_OWNER", "CURRENT_SOURCE", "ACTIVITY_PUBLIC_ACL"}, agentautonomy.Observe, "READ_CURRENT_PUBLIC_ACTIVITY", "READ_ONLY_CURRENT_RECHECK", "NO_EFFECT"},
	{agentplanner.ActivityDetail, "activity.detail.v1", json.RawMessage(`{"type":"object","additionalProperties":false,"required":["activity_id"],"properties":{"activity_id":{"type":"string","format":"uuid"}}}`), json.RawMessage(`{"type":"object","additionalProperties":false,"required":["schema_version","decision_id","action_id","tool","activities","observed_at","valid_until"],"properties":{"schema_version":{"const":"air.readonly_tool_result.v1"},"decision_id":{"type":"string"},"action_id":{"type":"string"},"tool":{"const":"activity.detail"},"activities":{"type":"array","minItems":1,"maxItems":1,"items":{"type":"object","required":["id","visibility"],"properties":{"id":{"type":"string","format":"uuid"},"visibility":{"const":"public"}}}},"observed_at":{"type":"string","format":"date-time"},"valid_until":{"type":"string","format":"date-time"}}}`), "READ", "CURRENT_SELECTED_PUBLIC_ACTIVITY", "LOW", []string{"CURRENT_PERSONAL_TASK_OWNER", "CURRENT_SOURCE", "ACTIVITY_PUBLIC_ACL"}, agentautonomy.Observe, "READ_CURRENT_PUBLIC_ACTIVITY", "READ_ONLY_CURRENT_RECHECK", "NO_EFFECT"},
	{SandboxWrite, "sandbox.write.v1", json.RawMessage(`{"type":"object","additionalProperties":false,"required":["action_id","logical_operation_id","target_id","value","resource_version"],"properties":{"action_id":{"type":"string","format":"uuid"},"logical_operation_id":{"type":"string","format":"uuid"},"target_id":{"type":"string","format":"uuid"},"value":{"type":"string","minLength":1,"maxLength":240},"resource_version":{"type":"string","minLength":1,"maxLength":240}}}`), json.RawMessage(`{"type":"object","additionalProperties":false,"required":["schema_version","decision_id","decision","reason_codes","tool","tool_version","action_id","logical_operation_id","actor_id","agent_id","subject_type","subject_id","policy_version","resource_version","arguments_digest","purpose","data_destinations","observed_at","expires_at"],"properties":{"schema_version":{"const":"air.tool_decision.v1"},"decision_id":{"type":"string"},"decision":{"enum":["DENY","CONFIRM"]},"reason_codes":{"type":"array","items":{"type":"string"}},"tool":{"type":"string"},"tool_version":{"type":"string"},"action_id":{"type":"string"},"logical_operation_id":{"type":"string"},"actor_id":{"type":"string"},"agent_id":{"type":"string"},"subject_type":{"type":"string"},"subject_id":{"type":"string"},"policy_version":{"type":"string"},"resource_version":{"type":"string"},"arguments_digest":{"type":"string"},"purpose":{"type":"string"},"data_destinations":{"type":"array","items":{"const":"LOCAL_OWNER"}},"observed_at":{"type":"string","format":"date-time"},"expires_at":{"type":"string","format":"date-time"}}}`), "WRITE", "OWN_LOCAL_SANDBOX_ONLY", "CONFIRMATION", []string{"CURRENT_PERSONAL_TASK_OWNER", "CURRENT_SOURCE", "OWN_SANDBOX_TARGET", "EXACT_VERSION_HUMAN_APPROVAL"}, agentautonomy.PrepareRegistration, "PREPARE_OWN_SANDBOX_APPROVAL", "NATIVE_OWNER_LOGICAL_OPERATION_ACTION_EFFECT", "OWN_NATIVE_SANDBOX_FENCE_AND_EFFECT_ROW"},
}

func init() {
	searchInput := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["city_id","search_term"],"properties":{"city_id":{"type":"string","minLength":1,"maxLength":160},"search_term":{"type":"string","maxLength":240},"category":{"type":"string","maxLength":80},"time_preference":{"enum":["","anytime","today","tomorrow","tonight","weekend"]},"closer":{"type":"boolean"},"bounds":{"type":"object","additionalProperties":false,"required":["west","south","east","north"],"properties":{"west":{"type":"number"},"south":{"type":"number"},"east":{"type":"number"},"north":{"type":"number"}}}}}`)
	// The existing typed ResultSet is the transport projection. The internal
	// compound receipt/Decision are not deserializable execution credentials.

	for _, d := range []struct{ tool, scope, purpose string }{
		{PlaceSearch, "CURRENT_TASK_PUBLIC_PLACE", "HUMAN_READ_CURRENT_PUBLIC_PLACE"},
		{PersonSearch, "CURRENT_TASK_EXPLICIT_PUBLIC_PERSON", "HUMAN_READ_CURRENT_PUBLIC_PERSON"},
	} {
		catalogue = append(catalogue, Descriptor{d.tool, d.tool + ".v1", searchInput, currentSearchOutputSchema(d.tool), "READ", d.scope, "LOW", []string{"CURRENT_PERSONAL_TASK_OWNER", "CURRENT_SESSION", "CURRENT_SOURCE", "NATIVE_DOMAIN_FIELD_ACL"}, agentautonomy.Observe, d.purpose, "READ_ONLY_CURRENT_RECHECK", "NO_EFFECT"})
	}
	catalogue = append(catalogue, Descriptor{PersonMatch, "person.match.v1", json.RawMessage(`{"type":"object","additionalProperties":false,"required":["source_intent_id"],"properties":{"source_intent_id":{"type":"string","format":"uuid"}}}`), json.RawMessage(`{"type":"object","additionalProperties":false,"required":["source","ruleVersion","sourceIntentId","candidates","truncated"],"properties":{"source":{"const":"RULE_BASED"},"ruleVersion":{"const":"v1"},"sourceIntentId":{"type":"string","format":"uuid"},"truncated":{"type":"boolean"},"candidates":{"type":"array","maxItems":50,"items":{"type":"object","additionalProperties":false,"required":["sourceIntentId","candidateIntentId","accountId","displayName","category","modality","reasonCodes","reasons"],"properties":{"sourceIntentId":{"type":"string","format":"uuid"},"candidateIntentId":{"type":"string","format":"uuid"},"accountId":{"type":"string","format":"uuid"},"displayName":{"type":"string"},"category":{"type":"string"},"modality":{"enum":["ONLINE","IN_PERSON","HYBRID"]},"reasonCodes":{"type":"array","items":{"type":"string"}},"reasons":{"type":"array","items":{"type":"string"}}}}}}}`), "READ", "EXPLICIT_OWN_INTENT_BILATERAL_OPT_IN", "LOW", []string{"CURRENT_PERSONAL_SOURCE_OWNER", "CURRENT_SESSION", "BILATERAL_NEW_PEOPLE_OPT_IN", "MUTUAL_CURRENT_AUDIENCE", "BLOCK_CLEAR"}, agentautonomy.Observe, "HUMAN_READ_CURRENT_NEW_PEOPLE", "READ_ONLY_CURRENT_RECHECK", "NO_EFFECT"})
}

// Metadata describes the ORIGINAL ResultSet and entity-action DTOs. This is
// not a model output schema, an invocation decoder or a permission resolver.
func currentSearchOutputSchema(tool string) json.RawMessage {
	kind := "person"
	if tool == PlaceSearch {
		kind = "place"
	}
	str := map[string]any{"type": "string"}
	ref := currentReadObject([]string{"type", "id"}, map[string]any{"type": map[string]any{"const": kind}, "id": map[string]any{"type": "string", "format": "uuid"}})
	action := currentReadObject([]string{"kind", "operation", "allowedOperations", "targetRef", "state", "label", "reason", "requiresConfirmation"}, map[string]any{
		"kind": map[string]any{"enum": []string{"CONNECT", "MESSAGE", "SHARE", "JOIN", "SAVE", "NAVIGATE"}}, "operation": str,
		"allowedOperations": map[string]any{"type": "array", "minItems": 1, "maxItems": 2, "items": str}, "targetRef": ref,
		"state": map[string]any{"enum": []string{"AVAILABLE", "UNAVAILABLE"}}, "label": str, "reason": str, "requiresConfirmation": map[string]any{"type": "boolean"},
	})
	precision := "area"
	zone := []string{"city_centre", "north", "south", "east", "west"}
	if kind == "place" {
		precision = "point"
		zone = []string{""}
	}
	anchor := currentReadObject([]string{"coordinateSystem", "precision", "latitude", "longitude"}, map[string]any{
		"coordinateSystem": map[string]any{"const": "wgs84"}, "precision": map[string]any{"const": precision},
		"latitude": map[string]any{"type": "number", "minimum": -90, "maximum": 90}, "longitude": map[string]any{"type": "number", "minimum": -180, "maximum": 180},
		"publicZone": map[string]any{"enum": zone}, "placeId": map[string]any{"type": "string", "format": "uuid"},
	})
	if kind == "person" {
		anchor["required"] = []string{"coordinateSystem", "precision", "latitude", "longitude", "publicZone"}
		delete(anchor["properties"].(map[string]any), "placeId")
	}
	item := currentReadObject([]string{"entityRef", "title", "summary", "scope"}, map[string]any{
		"entityRef": ref, "title": str, "summary": str, "scope": map[string]any{"const": "AUTHORIZED_VIEW"}, "detailRef": ref, "shareRef": ref, "anchor": anchor, "sourceVersion": str,
		"actions": map[string]any{"type": "array", "maxItems": 6, "items": action}, "actionsSourceVersion": str, "actionsValidUntil": map[string]any{"type": "string", "format": "date-time"},
	})
	filters := currentReadObject([]string{}, map[string]any{
		"category":       map[string]any{"enum": []string{"", "badminton", "basketball", "football", "sports", "culture"}},
		"timePreference": map[string]any{"enum": []string{"", "anytime", "today", "tonight", "tomorrow", "weekend"}}, "distancePreference": map[string]any{"enum": []string{"", "closer"}}, "locationPreference": map[string]any{"enum": []string{"", "city", "viewport"}},
		"targetIntent": str, "currentQuery": str, "searchTerm": str, "mapWest": str, "mapSouth": str, "mapEast": str, "mapNorth": str,
	})
	// The human ResultSet always includes sources, even when this native read
	// has none. A validated Now reply may also carry its presentation binding.
	// These describe output data only: neither is a model schema or a grant.
	source := currentReadObject([]string{"id", "title", "url"}, map[string]any{
		"id":          map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
		"title":       map[string]any{"type": "string", "minLength": 1, "maxLength": 1024},
		"url":         map[string]any{"type": "string", "minLength": 1, "maxLength": 2048, "format": "uri", "pattern": `^https?://`},
		"site":        map[string]any{"type": "string", "minLength": 1, "maxLength": 512},
		"publishedAt": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
	})
	identifier := map[string]any{"type": "string", "minLength": 1, "maxLength": 180}
	digest := map[string]any{"type": "string", "pattern": `^[0-9a-f]{64}$`}
	date := map[string]any{"type": "string", "format": "date-time"}
	answerBinding := currentReadObject([]string{"taskId", "requestId", "currentQueryDigest", "taskSnapshotDigest", "sourceEvidenceDigest", "runId", "generatedAt", "validUntil"}, map[string]any{
		"taskId": identifier, "requestId": identifier, "currentQueryDigest": digest,
		"taskSnapshotDigest": digest, "sourceEvidenceDigest": digest, "runId": identifier,
		"generatedAt": date, "validUntil": date,
	})
	schema := currentReadObject([]string{"schema", "id", "query", "cityId", "entities", "items", "filters", "generatedAt", "status", "sources"}, map[string]any{
		"schema": map[string]any{"const": "typed-agent-results-v1"}, "id": str, "taskId": str, "query": str, "cityId": str,
		"entities": map[string]any{"type": "array", "maxItems": 200, "items": ref}, "items": map[string]any{"type": "array", "maxItems": 200, "items": item}, "filters": filters, "generatedAt": map[string]any{"type": "string", "format": "date-time"}, "status": map[string]any{"enum": []string{"ready", "empty", "unsupported"}},
		"sources": map[string]any{"type": "array", "maxItems": 10, "items": source}, "answerBinding": answerBinding,
	})
	if kind == "place" {
		schema["properties"].(map[string]any)["publicFieldEvidence"] = currentReadPublicPlaceEvidenceSchema()
	}
	raw, _ := json.Marshal(schema) // Static JSON-compatible fields only.
	return raw
}
func currentReadObject(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

func copyDescriptor(d Descriptor) Descriptor {
	d.InputSchema = append(json.RawMessage{}, d.InputSchema...)
	d.OutputSchema = append(json.RawMessage{}, d.OutputSchema...)
	d.RequiredPermissions = append([]string{}, d.RequiredPermissions...)
	return d
}
func Catalogue() []Descriptor {
	out := make([]Descriptor, len(catalogue))
	for i, d := range catalogue {
		out[i] = copyDescriptor(d)
	}
	return out
}
func Lookup(tool string) (Descriptor, bool) {
	for _, d := range catalogue {
		if d.Tool == tool {
			return copyDescriptor(d), true
		}
	}
	return Descriptor{}, false
}
