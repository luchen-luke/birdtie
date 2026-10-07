package agentmemory

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const evidenceReferenceBody = `{"expectedMemoryVersion":1,"sourceType":"MOMENT","sourceId":"6d6250eb-e19f-4f84-a42d-4338cf792beb"}`

func TestEvidenceWireStrictReferenceAndDetach(t *testing.T) {
	for _, kind := range []string{"MOMENT", "ACTIVITY_PARTICIPATION", "SAVED_PLACE"} {
		t.Run(kind, func(t *testing.T) {
			body := strings.Replace(evidenceReferenceBody, "MOMENT", kind, 1)
			got, err := DecodeEvidenceReferenceInput([]byte(body))
			if err != nil || got.SourceID != evidenceSourceTestID || got.ExpectedMemoryVersion != 1 || string(got.SourceType) != kind {
				t.Fatal("valid three-field reference failed")
			}
			var standard EvidenceReferenceInput
			if json.Unmarshal([]byte(body), &standard) != nil || standard != got {
				t.Fatal("standard JSON path bypasses or differs from strict reference decoder")
			}
		})
	}
	if got, err := DecodeEvidenceDetachInput([]byte(`{"expectedVersion":1}`)); err != nil || got.ExpectedVersion != 1 {
		t.Fatal("valid one-field detach failed")
	}
	var detached EvidenceDetachInput
	if json.Unmarshal([]byte(`{"expectedVersion":2}`), &detached) != nil || detached.ExpectedVersion != 2 {
		t.Fatal("standard detach JSON differs")
	}
	var referenceNil *EvidenceReferenceInput
	var detachNil *EvidenceDetachInput
	if !errors.Is(referenceNil.UnmarshalJSON([]byte(evidenceReferenceBody)), ErrInvalid) || !errors.Is(detachNil.UnmarshalJSON([]byte(`{"expectedVersion":1}`)), ErrInvalid) {
		t.Fatal("nil decoder receiver did not reject")
	}
}

func TestEvidenceWireRejectsCallerAuthorityAndPrivacyFields(t *testing.T) {
	for _, key := range []string{"owner", "ownerType", "ownerId", "agentId", "memoryId", "memoryVersion", "id", "version", "sourceVersion", "source", "signalType", "weight", "observedAt", "eventTime", "createdAt", "status", "confirmed", "verified", "consent", "grants", "analysisPurpose", "modelUse", "sourceBody", "body", "title", "location", "mediaUrl", "recipient", "principal", "relationship", "privateFields"} {
		t.Run(key, func(t *testing.T) {
			body := strings.TrimSuffix(evidenceReferenceBody, "}") + `,"` + key + `":{"private":"synthetic-canary"}}`
			got, err := DecodeEvidenceReferenceInput([]byte(body))
			if !errors.Is(err, ErrInvalid) || got != (EvidenceReferenceInput{}) || err.Error() != ErrInvalid.Error() {
				t.Fatal("wire authority/private field was accepted or exposed")
			}
			standard := EvidenceReferenceInput{ExpectedMemoryVersion: 9, SourceID: evidenceOtherTestID}
			if json.Unmarshal([]byte(body), &standard) == nil || standard != (EvidenceReferenceInput{}) {
				t.Fatal("standard JSON accepted authority or retained previous input")
			}
		})
	}
}

func TestEvidenceWireRejectsMalformedReference(t *testing.T) {
	for _, item := range []struct{ name, body string }{
		{"null", `null`}, {"array", `[]`}, {"empty", `{}`}, {"truncated", `{"expectedMemoryVersion":1`},
		{"trailing_json", evidenceReferenceBody + `{}`}, {"trailing_nonjson", evidenceReferenceBody + `private`},
		{"missing_expected", `{"sourceType":"MOMENT","sourceId":"` + evidenceSourceTestID + `"}`},
		{"missing_kind", `{"expectedMemoryVersion":1,"sourceId":"` + evidenceSourceTestID + `"}`},
		{"missing_id", `{"expectedMemoryVersion":1,"sourceType":"MOMENT"}`},
		{"version_zero", strings.Replace(evidenceReferenceBody, `:1,`, `:0,`, 1)},
		{"version_negative", strings.Replace(evidenceReferenceBody, `:1,`, `:-1,`, 1)},
		{"version_float", strings.Replace(evidenceReferenceBody, `:1,`, `:1.0,`, 1)},
		{"version_exponent", strings.Replace(evidenceReferenceBody, `:1,`, `:1e0,`, 1)},
		{"version_string", strings.Replace(evidenceReferenceBody, `:1,`, `:"1",`, 1)},
		{"version_null", strings.Replace(evidenceReferenceBody, `:1,`, `:null,`, 1)},
		{"version_bool", strings.Replace(evidenceReferenceBody, `:1,`, `:true,`, 1)},
		{"version_overflow", strings.Replace(evidenceReferenceBody, `:1,`, `:9223372036854775808,`, 1)},
		{"type_null", strings.Replace(evidenceReferenceBody, `"MOMENT"`, `null`, 1)},
		{"type_array", strings.Replace(evidenceReferenceBody, `"MOMENT"`, `[]`, 1)},
		{"type_lower", strings.Replace(evidenceReferenceBody, `"MOMENT"`, `"moment"`, 1)},
		{"type_public_activity", strings.Replace(evidenceReferenceBody, `"MOMENT"`, `"ACTIVITY"`, 1)},
		{"type_visited", strings.Replace(evidenceReferenceBody, `"MOMENT"`, `"PLACE_VISIT"`, 1)},
		{"id_null", strings.Replace(evidenceReferenceBody, `"`+evidenceSourceTestID+`"`, `null`, 1)},
		{"id_bad", strings.Replace(evidenceReferenceBody, evidenceSourceTestID, "private-canary", 1)},
		{"id_zero", strings.Replace(evidenceReferenceBody, evidenceSourceTestID, "00000000-0000-0000-0000-000000000000", 1)},
		{"id_padded", strings.Replace(evidenceReferenceBody, evidenceSourceTestID, " "+evidenceSourceTestID, 1)},
		{"duplicate_expected", strings.TrimSuffix(evidenceReferenceBody, "}") + `,"expectedMemoryVersion":1}`},
		{"duplicate_type", strings.TrimSuffix(evidenceReferenceBody, "}") + `,"sourceType":"MOMENT"}`},
		{"duplicate_id", strings.TrimSuffix(evidenceReferenceBody, "}") + `,"sourceId":"` + evidenceSourceTestID + `"}`},
		{"duplicate_escaped_key", strings.TrimSuffix(evidenceReferenceBody, "}") + `,"source\u0049d":"` + evidenceSourceTestID + `"}`},
		{"key_case", strings.Replace(evidenceReferenceBody, "expectedMemoryVersion", "ExpectedMemoryVersion", 1)},
		{"unknown_replaces_required", strings.Replace(evidenceReferenceBody, "sourceId", "ownerId", 1)},
		{"nested_fake_source", strings.Replace(evidenceReferenceBody, `"MOMENT"`, `{"owner":"PERSON","verified":true}`, 1)},
		{"invalid_utf8", string([]byte{0xff})},
	} {
		t.Run(item.name, func(t *testing.T) {
			got, err := DecodeEvidenceReferenceInput([]byte(item.body))
			if !errors.Is(err, ErrInvalid) || got != (EvidenceReferenceInput{}) || err.Error() != ErrInvalid.Error() {
				t.Fatal("malformed reference accepted or returned input")
			}
			var standard EvidenceReferenceInput
			if json.Unmarshal([]byte(item.body), &standard) == nil || standard != (EvidenceReferenceInput{}) {
				t.Fatal("standard malformed reference path bypassed strict decoder")
			}
		})
	}
	for _, key := range []string{"expectedMemoryVersion", "sourceType", "sourceId"} {
		t.Run("null_"+key, func(t *testing.T) {
			var object map[string]any
			if json.Unmarshal([]byte(evidenceReferenceBody), &object) != nil {
				t.Fatal("fixture")
			}
			object[key] = nil
			body, _ := json.Marshal(object)
			got, err := DecodeEvidenceReferenceInput(body)
			if !errors.Is(err, ErrInvalid) || got != (EvidenceReferenceInput{}) {
				t.Fatal("null required field accepted")
			}
		})
	}
}

func TestEvidenceWireDetachRejectsExtrasAndVersions(t *testing.T) {
	for _, item := range []struct{ name, body string }{
		{"null", `null`}, {"array", `[]`}, {"empty", `{}`},
		{"zero", `{"expectedVersion":0}`}, {"negative", `{"expectedVersion":-1}`},
		{"float", `{"expectedVersion":1.0}`}, {"exponent", `{"expectedVersion":1e0}`},
		{"string", `{"expectedVersion":"1"}`}, {"version_null", `{"expectedVersion":null}`},
		{"boolean", `{"expectedVersion":true}`}, {"overflow", `{"expectedVersion":9223372036854775808}`},
		{"case_key", `{"ExpectedVersion":1}`}, {"confirmed", `{"expectedVersion":1,"confirmed":true}`},
		{"source_claim", `{"expectedVersion":1,"sourceId":"` + evidenceSourceTestID + `"}`},
		{"duplicate", `{"expectedVersion":1,"expectedVersion":1}`},
		{"escaped_duplicate", `{"expectedVersion":1,"expected\u0056ersion":1}`},
		{"trailing_json", `{"expectedVersion":1}{}`}, {"trailing_nonjson", `{"expectedVersion":1}private`},
	} {
		t.Run(item.name, func(t *testing.T) {
			got, err := DecodeEvidenceDetachInput([]byte(item.body))
			if !errors.Is(err, ErrInvalid) || got != (EvidenceDetachInput{}) {
				t.Fatal("detach accepted malformed/authority fields")
			}
			// json.Unmarshal rejects malformed outer JSON before invoking a
			// custom receiver. A fresh receiver cannot retain another request.
			var standard EvidenceDetachInput
			if json.Unmarshal([]byte(item.body), &standard) == nil || standard != (EvidenceDetachInput{}) {
				t.Fatal("standard detach accepted unauthorized version")
			}
			direct := EvidenceDetachInput{ExpectedVersion: 2}
			if !errors.Is(direct.UnmarshalJSON([]byte(item.body)), ErrInvalid) || direct != (EvidenceDetachInput{}) {
				t.Fatal("strict decoder method retained an earlier input")
			}
		})
	}
}

func TestEvidenceWireRawBodyBoundsRemainAtDecoder(t *testing.T) {
	for _, item := range []struct {
		name, body string
		decode     func([]byte) error
	}{
		{"reference", evidenceReferenceBody, func(raw []byte) error { _, err := DecodeEvidenceReferenceInput(raw); return err }},
		{"detach", `{"expectedVersion":1}`, func(raw []byte) error { _, err := DecodeEvidenceDetachInput(raw); return err }},
	} {
		t.Run(item.name, func(t *testing.T) {
			exact := item.body + strings.Repeat(" ", MaxBodyBytes-len(item.body))
			if err := item.decode([]byte(exact)); err != nil {
				t.Fatal("exact raw body bound rejected")
			}
			if !errors.Is(item.decode([]byte(exact+" ")), ErrInvalid) {
				t.Fatal("raw body bound bypassed by whitespace")
			}
		})
	}
	input := EvidenceReferenceInput{ExpectedMemoryVersion: 2, SourceID: evidenceOtherTestID}
	if json.Unmarshal([]byte(`null`), &input) == nil || !reflect.DeepEqual(input, EvidenceReferenceInput{}) {
		t.Fatal("null unmarshal did not clear previous reference")
	}
}
