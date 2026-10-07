package agenttool

// This describes only BuildPublicFieldEvidence's public Place branch. Its
// source version token is an opaque public projection fingerprint; no Session,
// bearer token, native row token, permission or model input is described here.
func currentReadPublicPlaceEvidenceSchema() map[string]any {
	text := map[string]any{"type": "string"}
	date := map[string]any{"type": "string", "format": "date-time"}
	id := map[string]any{"type": "string", "format": "uuid"}
	owner := currentReadObject([]string{"type", "id"}, map[string]any{"type": map[string]any{"const": "PERSON"}, "id": id})
	version := currentReadObject([]string{"kind", "token"}, map[string]any{
		"kind": map[string]any{"const": "UPDATED_AT_DIGEST"}, "token": map[string]any{"type": "string", "pattern": `^[0-9a-f]{64}$`},
	})
	source := currentReadObject([]string{"kind", "id", "version", "nativeTime"}, map[string]any{
		"kind": map[string]any{"const": "PUBLIC_PLACE"}, "id": id, "version": version, "nativeTime": date,
	})
	claim := currentReadObject([]string{"id", "itemKind", "itemId", "subjectKind", "subjectId", "field", "claimantKind", "source", "nature", "use", "observedAt", "collectedAt", "collectionEvent", "sourceUpdatedAt", "captureTimeStatus", "validityStatus"}, map[string]any{
		"id": text, "itemKind": map[string]any{"const": "places"}, "itemId": id,
		"subjectKind": map[string]any{"const": "PLACE"}, "subjectId": id,
		"field": map[string]any{"enum": []string{"place.name", "place.category"}}, "claimantKind": map[string]any{"const": "NATIVE_DOMAIN_RECORD"},
		"source": source, "nature": map[string]any{"const": "NATIVE_RECORD"}, "use": map[string]any{"const": "CURRENT_DOMAIN_RECORD_ONLY"},
		"observedAt": date, "collectedAt": date, "collectionEvent": map[string]any{"const": "CURRENT_NATIVE_READ"},
		"sourceUpdatedAt": date, "validUntil": date, "captureTimeStatus": map[string]any{"const": "UNKNOWN_NOT_COLLECTED"},
		"validityStatus": map[string]any{"enum": []string{"SOURCE_INTERVAL_UNKNOWN", "VALID_UNTIL_KNOWN"}},
	})
	unavailable := map[string]any{"const": "UNAVAILABLE"}
	noAuthority := map[string]any{"const": false}
	fieldSet := currentReadObject([]string{"schemaVersion", "owner", "observedAt", "expiresAt", "scope", "claims", "conflicts", "modelAccess", "mediaAccess", "grantsAuthority"}, map[string]any{
		"schemaVersion": map[string]any{"const": "agent-field-evidence-v1"}, "owner": owner, "observedAt": date, "expiresAt": date,
		"scope":       map[string]any{"enum": []string{"FILTERED_CONTEXT", "BUDGETED_CONTEXT"}},
		"claims":      map[string]any{"type": "array", "maxItems": 100, "items": claim},
		"conflicts":   map[string]any{"type": "array", "maxItems": 0, "items": currentReadObject([]string{}, map[string]any{})},
		"modelAccess": unavailable, "mediaAccess": unavailable, "grantsAuthority": noAuthority,
	})
	count := map[string]any{"type": "integer", "minimum": 0, "maximum": 200}
	omissions := currentReadObject([]string{"OMITTED_BUDGET", "UNKNOWN_SOURCE_TIME", "EXPIRED_SOURCE", "SOURCE_METADATA_UNAVAILABLE"}, map[string]any{
		"OMITTED_BUDGET": count, "UNKNOWN_SOURCE_TIME": count, "EXPIRED_SOURCE": count, "SOURCE_METADATA_UNAVAILABLE": count,
	})
	budget := currentReadObject([]string{"unit", "limit", "used", "omitted"}, map[string]any{
		"unit": map[string]any{"const": "UTF8_JSON_BYTES_V1"}, "limit": map[string]any{"const": 16384},
		"used": map[string]any{"type": "integer", "minimum": 0, "maximum": 16384}, "omitted": omissions,
	})
	return currentReadObject([]string{"schemaVersion", "taskId", "queryKind", "owner", "observedAt", "validUntil", "status", "fieldEvidenceSet", "budget", "modelAccess", "mediaAccess", "grantsAuthority"}, map[string]any{
		"schemaVersion": map[string]any{"const": "public-query-field-evidence-v1"}, "taskId": id, "queryKind": map[string]any{"const": "place"}, "owner": owner,
		"observedAt": date, "validUntil": date, "status": map[string]any{"enum": []string{"NO_PUBLIC_FIELDS", "UNAVAILABLE", "PARTIAL", "AVAILABLE"}},
		"fieldEvidenceSet": fieldSet, "budget": budget, "modelAccess": unavailable, "mediaAccess": unavailable, "grantsAuthority": noAuthority,
	})
}
