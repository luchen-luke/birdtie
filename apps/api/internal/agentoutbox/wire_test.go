package agentoutbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func envelopeJSON(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(outboxEvent(MomentCreated))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func TestAgentOutboxMetadataWireRoundTrip(t *testing.T) {
	for _, kind := range []EventType{MomentCreated, MomentUpdated, MomentWithdrawn} {
		t.Run(string(kind), func(t *testing.T) {
			e := outboxEvent(kind)
			raw, _ := json.Marshal(e)
			decoded, err := DecodeEnvelope(raw, outboxTime())
			if err != nil || decoded.EventID != e.EventID || decoded.Source != e.Source {
				t.Fatal("metadata wire round trip", err)
			}
			var standard Envelope
			if json.Unmarshal(raw, &standard) != nil || standard.EventID != e.EventID {
				t.Fatal("standard unmarshal bypass")
			}
		})
	}
	for _, state := range []State{Pending, Leased, Unavailable, Invalidated, Expired, DeadLetter} {
		t.Run("record_"+string(state), func(t *testing.T) {
			r := outboxRecord()
			if state == Leased {
				r = outboxLease()
			} else {
				r.State = state
			}
			raw, _ := json.Marshal(r)
			decoded, err := DecodeRecord(raw)
			if err != nil || decoded.Event.EventID != r.Event.EventID || decoded.State != r.State {
				t.Fatal("record wire", err)
			}
			var standard Record
			if json.Unmarshal(raw, &standard) != nil {
				t.Fatal("standard record parser")
			}
		})
	}
}

func TestAgentOutboxEnvelopeWireRejectsPrivateAuthorityAndAmbiguity(t *testing.T) {
	valid := envelopeJSON(t)
	sourceJSON, err := json.Marshal(outboxEvent(MomentCreated).Source)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"body", "query", "media", "location", "session", "token", "consent", "confirmed", "authority", "purpose", "candidate_id", "effect_key", "processing_status", "handler_version", "lease", "source_owner"} {
		t.Run("unknown_"+key, func(t *testing.T) {
			raw := strings.TrimSuffix(valid, "}") + `,"` + key + `":true}`
			e, err := DecodeEnvelope([]byte(raw), outboxTime())
			if err != ErrInvalid || e != (Envelope{}) {
				t.Fatal("private/authority key accepted")
			}
		})
	}
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"duplicate_top", strings.Replace(valid, `"schema_version":`, `"schema_version":"agent-outbox-v1","schema_version":`, 1)},
		{"case_top", strings.Replace(valid, `"schema_version":`, `"Schema_Version":`, 1)},
		{"duplicate_subject", strings.Replace(valid, `"subject":{"type":`, `"subject":{"type":"PERSON","type":`, 1)},
		{"duplicate_source", strings.Replace(valid, `"source":{"type":`, `"source":{"type":"MOMENT","type":`, 1)},
		{"source_owner_override", strings.Replace(valid, `"source":{"type":`, `"source":{"ownerId":"`+testOther+`","type":`, 1)},
		{"nested_privacy", strings.Replace(valid, `"source":{"type":`, `"source":{"body":"PRIVATE","type":`, 1)},
		{"revision_null", strings.Replace(valid, `"revision":1`, `"revision":null`, 1)},
		{"revision_fraction", strings.Replace(valid, `"revision":1`, `"revision":1.5`, 1)},
		{"revision_overflow", strings.Replace(valid, `"revision":1`, `"revision":9223372036854775808`, 1)},
		{"source_array", strings.Replace(valid, `"source":`+string(sourceJSON), `"source":[`+string(sourceJSON)+`]`, 1)},
		{"tenant_null", strings.Replace(valid, `"tenant":{"type":"PERSON","id":"`+testOwner+`"}`, `"tenant":null`, 1)},
		{"fingerprint_null", strings.Replace(valid, `"fingerprint":"`+strings.Repeat("a", 64)+`"`, `"fingerprint":null`, 1)},
		{"missing_trace", strings.Replace(valid, `,"root_trace_id":"`+testTrace+`"`, "", 1)},
		{"causation_boolean", strings.Replace(valid, `"causation_id":null`, `"causation_id":true`, 1)},
		{"multiple_json", valid + valid},
		{"trailing", valid + " trailing"},
		{"null", "null"}, {"array", "[]"}, {"empty", ""},
		{"oversize", valid + strings.Repeat(" ", MaxEnvelopeBytes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := DecodeEnvelope([]byte(tc.raw), outboxTime())
			if err != ErrInvalid || e != (Envelope{}) {
				t.Fatal("ambiguous metadata accepted", err)
			}
			before := outboxEvent(MomentCreated)
			if before.UnmarshalJSON([]byte(tc.raw)) == nil || before != (Envelope{}) {
				t.Fatal("custom JSON method failed to clear rejected metadata")
			}
			// encoding/json validates syntax before invoking UnmarshalJSON, and
			// strips surrounding whitespace. Raw byte limits belong to the
			// explicit decoder; a syntax error cannot promise to clear a prior
			// receiver when the custom method never ran.
			before = outboxEvent(MomentCreated)
			standardErr := json.Unmarshal([]byte(tc.raw), &before)
			if json.Valid([]byte(tc.raw)) && len(tc.raw) <= MaxEnvelopeBytes {
				if standardErr == nil || before != (Envelope{}) {
					t.Fatal("standard JSON bypassed strict schema decoder")
				}
			} else if !json.Valid([]byte(tc.raw)) && standardErr == nil {
				t.Fatal("standard JSON accepted invalid syntax")
			} else if json.Valid([]byte(tc.raw)) && len(tc.raw) > MaxEnvelopeBytes {
				if standardErr != nil || before != outboxEvent(MomentCreated) {
					t.Fatal("surrounding whitespace normalization changed metadata")
				}
			}
		})
	}
	t.Run("current_expiry", func(t *testing.T) {
		e, err := DecodeEnvelope([]byte(valid), outboxTime().Add(15*time.Minute))
		if err != ErrExpired || e != (Envelope{}) {
			t.Fatal("decode granted historical expiry")
		}
	})
	t.Run("nil_receiver", func(t *testing.T) {
		if (*Envelope)(nil).UnmarshalJSON(nil) != ErrInvalid {
			t.Fatal("nil receiver")
		}
	})
}

func TestAgentOutboxRecordWireRejectsControlOverrides(t *testing.T) {
	raw, _ := json.Marshal(outboxRecord())
	valid := string(raw)
	for _, key := range []string{"confirmed", "analysisEnabled", "sessionDigest", "effect", "candidate", "handlerVersion", "owner", "source", "fencingToken"} {
		t.Run("unknown_"+key, func(t *testing.T) {
			bad := strings.TrimSuffix(valid, "}") + `,"` + key + `":true}`
			r, err := DecodeRecord([]byte(bad))
			if err != ErrInvalid || r != (Record{}) {
				t.Fatal("control injection accepted")
			}
		})
	}
	for _, tc := range []struct{ name, raw string }{
		{"null_attempt", strings.Replace(valid, `"attempt":0`, `"attempt":null`, 1)},
		{"null_fence", strings.Replace(valid, `"fence":0`, `"fence":null`, 1)},
		{"missing_attempt", strings.Replace(valid, `,"attempt":0`, "", 1)},
		{"missing_fence", strings.Replace(valid, `,"fence":0`, "", 1)},
		{"duplicate_fence", strings.Replace(valid, `"fence":0`, `"fence":0,"fence":1`, 1)},
		{"case_fence", strings.Replace(valid, `"fence":0`, `"Fence":0`, 1)},
		{"unknown_nested", strings.Replace(valid, `"event":{`, `"event":{"confirmed":true,`, 1)},
		{"null_lease", strings.TrimSuffix(valid, "}") + `,"lease_until":null}`},
		{"null_owner", strings.TrimSuffix(valid, "}") + `,"lease_owner":null}`},
		{"unknown_success", strings.Replace(valid, `"state":"PENDING"`, `"state":"SUCCEEDED"`, 1)},
		{"multiple", valid + valid}, {"oversize", valid + strings.Repeat(" ", MaxRecordBytes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := DecodeRecord([]byte(tc.raw))
			if err != ErrInvalid || r != (Record{}) {
				t.Fatal("bad control shape accepted", err)
			}
			before := outboxRecord()
			if before.UnmarshalJSON([]byte(tc.raw)) == nil || before != (Record{}) {
				t.Fatal("custom JSON method failed to clear rejected control")
			}
			before = outboxRecord()
			standardErr := json.Unmarshal([]byte(tc.raw), &before)
			if json.Valid([]byte(tc.raw)) && len(tc.raw) <= MaxRecordBytes {
				if standardErr == nil || before != (Record{}) {
					t.Fatal("standard record schema decoder bypass")
				}
			} else if !json.Valid([]byte(tc.raw)) && standardErr == nil {
				t.Fatal("standard JSON accepted invalid control syntax")
			} else if json.Valid([]byte(tc.raw)) && len(tc.raw) > MaxRecordBytes {
				if standardErr != nil || before != outboxRecord() {
					t.Fatal("surrounding whitespace normalization changed controls")
				}
			}
		})
	}
	t.Run("nil_receiver", func(t *testing.T) {
		if (*Record)(nil).UnmarshalJSON(nil) != ErrInvalid {
			t.Fatal("nil receiver")
		}
	})
}
