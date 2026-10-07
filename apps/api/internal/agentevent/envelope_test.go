package agentevent

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

const ownID = "72000000-0000-4000-8000-000000000001"
const otherID = "72000000-0000-4000-8000-000000000002"
const agentID = "72000000-0000-4000-8000-000000000003"
const sourceID = "72000000-0000-4000-8000-000000000004"
const operationID = "72000000-0000-4000-8000-000000000005"
const traceID = "72000000-0000-4000-8000-000000000006"

func fixedNow() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }

func validEnvelope(kind Type) Envelope {
	d, _ := Lookup(kind)
	now := fixedNow()
	principal := actorref.PrincipalRef{Type: actorref.Person, ID: ownID}
	version := SourceVersion{Kind: RevisionVersion, Revision: 1}
	if kind == UserQuery {
		version, _ = QueryVersion(now.Add(-time.Minute), []byte(`{"query":"private fixture"}`))
	}
	e := Envelope{SchemaVersion: SchemaVersion, EventType: kind, Tenant: principal, Subject: principal,
		Actor: actorref.ActorRef{Type: actorref.Person, ID: ownID}, AgentID: agentID,
		LogicalOperationID: operationID, OccurredAt: now.Add(-time.Minute), ReceivedAt: now,
		ExpiresAt: now.Add(-time.Minute).Add(MaxEventTTL),
		Source:    SourceReference{Type: d.Source, ID: sourceID, Owner: principal, Version: version},
		Purpose:   d.Purpose, RootTraceID: traceID, PayloadRef: PayloadReference{Type: d.Source, ID: sourceID},
		ProcessingStatus: Unavailable}
	e.EventID = StableEventID(e)
	return e
}

func TestEventCatalogAndSchemaPositive(t *testing.T) {
	for _, kind := range []Type{MomentCreated, UserQuery} {
		t.Run(string(kind), func(t *testing.T) {
			e := validEnvelope(kind)
			if err := Validate(e, fixedNow()); err != nil {
				t.Fatal(err)
			}
			data, err := Encode(e, fixedNow())
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Decode(data, fixedNow())
			if err != nil || restored.Source != e.Source || restored.EventID != e.EventID || restored.ProcessingStatus != Unavailable {
				t.Fatal("schema roundtrip failed", err)
			}
			for _, forbidden := range []string{"private fixture", "\"query\":", "\"conversation\":", "consent_epoch", "permission_override", "\"body\":", "token_sha256", "latitude"} {
				if bytes.Contains(data, []byte(forbidden)) {
					t.Fatal("event contains payload or invented authority", forbidden)
				}
			}
		})
	}
	if d, ok := Lookup(Type("UnknownFutureType")); ok || d != (Descriptor{}) {
		t.Fatal("unknown event registered")
	}
	c := Catalog()
	c[0].Purpose = "override"
	if Catalog()[0].Purpose != MemoryCandidate || len(Catalog()) != 13 {
		t.Fatal("catalog can be mutated")
	}
}

func TestEventSchemaRejectsInvalidFields(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Envelope)
	}{
		{"unknown_type", func(e *Envelope) { e.EventType = "Unknown" }},
		{"unknown_schema", func(e *Envelope) { e.SchemaVersion = "air.event.v2" }},
		{"wrong_purpose", func(e *Envelope) { e.Purpose = ActivityQuery }},
		{"status_available", func(e *Envelope) { e.ProcessingStatus = "AVAILABLE" }},
		{"tenant_other", func(e *Envelope) { e.Tenant.ID = otherID }},
		{"tenant_org", func(e *Envelope) { e.Tenant.Type = actorref.Organization }},
		{"subject_other", func(e *Envelope) { e.Subject.ID = otherID }},
		{"subject_org", func(e *Envelope) { e.Subject.Type = actorref.Organization }},
		{"actor_other", func(e *Envelope) { e.Actor.ID = otherID }},
		{"actor_business", func(e *Envelope) { e.Actor.Type = actorref.Business }},
		{"source_owner", func(e *Envelope) { e.Source.Owner.ID = otherID }},
		{"source_owner_org", func(e *Envelope) { e.Source.Owner.Type = actorref.Organization }},
		{"wrong_source_type", func(e *Envelope) { e.Source.Type = QuerySource }},
		{"zero_revision", func(e *Envelope) { e.Source.Version.Revision = 0 }},
		{"negative_revision", func(e *Envelope) { e.Source.Version.Revision = -1 }},
		{"extra_token", func(e *Envelope) { e.Source.Version.Token = strings.Repeat("a", 64) }},
		{"wrong_version_kind", func(e *Envelope) { e.Source.Version.Kind = UpdatedAtDigestVersion }},
		{"payload_url", func(e *Envelope) { e.PayloadRef.ID = "https://example.invalid/private" }},
		{"payload_other", func(e *Envelope) { e.PayloadRef.ID = otherID }},
		{"payload_type", func(e *Envelope) { e.PayloadRef.Type = QuerySource }},
		{"occurred_future", func(e *Envelope) { e.OccurredAt = fixedNow().Add(time.Minute) }},
		{"received_future", func(e *Envelope) { e.ReceivedAt = fixedNow().Add(time.Minute) }},
		{"zero_occurred", func(e *Envelope) { e.OccurredAt = time.Time{} }},
		{"zero_received", func(e *Envelope) { e.ReceivedAt = time.Time{} }},
		{"zero_expires", func(e *Envelope) { e.ExpiresAt = time.Time{} }},
		{"extend_expiry", func(e *Envelope) { e.ExpiresAt = e.ExpiresAt.Add(time.Second) }},
		{"source_url", func(e *Envelope) { e.Source.ID = "file:///secret" }},
		{"invalid_agent", func(e *Envelope) { e.AgentID = "agent_name" }},
		{"zero_agent", func(e *Envelope) { e.AgentID = "00000000-0000-0000-0000-000000000000" }},
		{"trace_secret", func(e *Envelope) { e.RootTraceID = "Bearer private" }},
		{"operation_blank", func(e *Envelope) { e.LogicalOperationID = "" }},
		{"operation_whitespace", func(e *Envelope) { e.LogicalOperationID = " " + operationID }},
		{"invalid_causation", func(e *Envelope) { v := "secret"; e.CausationID = &v }},
		{"wrong_event_id", func(e *Envelope) { e.EventID = otherID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := validEnvelope(MomentCreated)
			tc.change(&e)
			if tc.name != "wrong_event_id" {
				e.EventID = StableEventID(e)
			}
			if !errors.Is(Validate(e, fixedNow()), ErrInvalid) {
				t.Fatal("invalid event accepted")
			}
			data, _ := json.Marshal(e)
			if got, err := Decode(data, fixedNow()); !errors.Is(err, ErrInvalid) || got != (Envelope{}) {
				t.Fatal("invalid decode emitted payload", err)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		version SourceVersion
	}{
		{"missing", SourceVersion{Kind: UpdatedAtDigestVersion}},
		{"not_hex", SourceVersion{Kind: UpdatedAtDigestVersion, Token: strings.Repeat("z", 64)}},
		{"uppercase", SourceVersion{Kind: UpdatedAtDigestVersion, Token: strings.Repeat("A", 64)}},
		{"counter_present", SourceVersion{Kind: UpdatedAtDigestVersion, Token: strings.Repeat("a", 64), Revision: 1}},
		{"revision_kind", SourceVersion{Kind: RevisionVersion, Revision: 1}},
	} {
		t.Run("opaque_"+tc.name, func(t *testing.T) {
			e := validEnvelope(UserQuery)
			e.Source.Version = tc.version
			e.EventID = StableEventID(e)
			if !errors.Is(Validate(e, fixedNow()), ErrInvalid) {
				t.Fatal("invalid opaque version accepted")
			}
		})
	}
}

func TestEventStrictJSONAndBounds(t *testing.T) {
	e := validEnvelope(MomentCreated)
	data, _ := Encode(e, fixedNow())
	cases := map[string][]byte{
		"case_alias_top":      bytes.Replace(data, []byte(`"subject":`), []byte(`"Subject":`), 1),
		"case_alias_nested":   bytes.Replace(data, []byte(`"revision":1`), []byte(`"Revision":1`), 1),
		"case_duplicate":      bytes.Replace(data, []byte(`"revision":1`), []byte(`"revision":999,"Revision":1`), 1),
		"wrong_variant_null":  bytes.Replace(data, []byte(`"revision":1`), []byte(`"revision":1,"token":null`), 1),
		"wrong_variant_empty": bytes.Replace(data, []byte(`"revision":1`), []byte(`"revision":1,"token":""`), 1),
		"wrong_variant_zero":  bytes.Replace(data, []byte(`"revision":1`), []byte(`"revision":1,"token":0`), 1),
		"unknown_top":         bytes.Replace(data, []byte(`"schema_version":`), []byte(`"confirmed":true,"schema_version":`), 1),
		"unknown_nested":      bytes.Replace(data, []byte(`"revision":1`), []byte(`"revision":1,"verified":true`), 1),
		"duplicate_top":       bytes.Replace(data, []byte(`"schema_version":"air.event.v1"`), []byte(`"schema_version":"evil","schema_version":"air.event.v1"`), 1),
		"duplicate_nested":    bytes.Replace(data, []byte(`"revision":1`), []byte(`"revision":999,"revision":1`), 1),
		"escaped_duplicate":   bytes.Replace(data, []byte(`"revision":1`), []byte(`"revision":999,"revis\u0069on":1`), 1),
		"trailing":            append(append([]byte{}, data...), []byte(`{}`)...),
		"null":                []byte(`null`), "array": []byte(`[]`), "empty": nil, "malformed": []byte(`{`),
		"oversize": append(append([]byte{}, data...), bytes.Repeat([]byte(" "), MaxEnvelopeBytes)...),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Decode(value, fixedNow())
			if !errors.Is(err, ErrInvalid) || got != (Envelope{}) {
				t.Fatal("unsafe JSON accepted", err)
			}
			// The standard library checks syntax before invoking UnmarshalJSON,
			// and strips outer whitespace. The explicit Decode API owns total body
			// bounds. The receiver starts empty for a malformed JSON syntax case.
			got = Envelope{}
			if name != "oversize" {
				if err := json.Unmarshal(value, &got); err == nil || got != (Envelope{}) {
					t.Fatal("standard decoder bypassed schema")
				}
			}
		})
	}
	padding := append(append([]byte{}, data...), bytes.Repeat([]byte(" "), MaxEnvelopeBytes-len(data))...)
	if _, err := Decode(padding, fixedNow()); err != nil {
		t.Fatal("exact byte bound rejected", err)
	}
	if got, err := Decode(data, e.ExpiresAt); !errors.Is(err, ErrExpired) || got != (Envelope{}) {
		t.Fatal("expired accepted", err)
	}
}

func TestEventStableIdentityAndOpaqueNativeVersion(t *testing.T) {
	e := validEnvelope(UserQuery)
	first := e.EventID
	e.ReceivedAt = e.ReceivedAt.Add(time.Second)
	if StableEventID(e) != first {
		t.Fatal("received/retry time changed stable event")
	}
	e.RootTraceID = otherID
	if StableEventID(e) != first {
		t.Fatal("technical trace replaced logical operation")
	}
	e.LogicalOperationID = otherID
	if StableEventID(e) == first {
		t.Fatal("deliberately repeated request permanently deduplicated")
	}
	a, _ := QueryVersion(fixedNow(), []byte("same stored content"))
	b, _ := QueryVersion(fixedNow(), []byte("same stored content"))
	c, _ := QueryVersion(fixedNow().Add(time.Microsecond), []byte("same stored content"))
	d, _ := QueryVersion(fixedNow(), []byte("changed stored content"))
	if a != b || a == c || a == d {
		t.Fatal("native fingerprint not bound to exact time/content")
	}
	for _, tc := range []struct {
		name string
		at   time.Time
		body []byte
	}{
		{"zero_time", time.Time{}, []byte("x")}, {"empty", fixedNow(), nil}, {"oversized", fixedNow(), bytes.Repeat([]byte("x"), 1024*1024+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := QueryVersion(tc.at, tc.body); !errors.Is(err, ErrInvalid) || got != (SourceVersion{}) {
				t.Fatal("invalid query source version accepted")
			}
		})
	}
}
