package agentmemory

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
)

const evidenceTestID = "57de4ad6-8b25-484f-b3b9-d0aedc0e1e14"
const evidenceSourceTestID = "6d6250eb-e19f-4f84-a42d-4338cf792beb"
const evidenceOtherTestID = "f3a92b5e-2a98-40e9-bb3a-4103a39b8b7a"

func evidenceTestNow() time.Time { return memoryTestNow().Add(time.Hour) }

func evidenceFixture(t *testing.T, sourceType agentevent.SourceType) Evidence {
	t.Helper()
	eventTime := memoryTestNow().Add(30 * time.Minute)
	version := agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 3}
	if sourceType != agentevent.MomentSource {
		kind := agentevent.UpdatedAtDigestVersion
		if sourceType == agentevent.SavedPlaceSource {
			kind = agentevent.CreatedAtDigestVersion
		}
		var err error
		version, err = agentevent.SnapshotVersion(sourceType, kind, eventTime, []byte(`{"fixture":"shape-only-current-native-metadata"}`))
		if err != nil {
			t.Fatal("cannot construct synthetic source-version shape")
		}
	}
	return Evidence{SchemaVersion: EvidenceSchemaV1, ID: evidenceTestID, MemoryID: memoryTestID,
		MemoryVersion: 1, AgentID: memoryTestAgentID, OwnerType: actorref.Person, OwnerID: memoryTestOwnerID, Version: 1,
		Source: &agentevent.SourceReference{Type: sourceType, ID: evidenceSourceTestID,
			Owner: actorref.PrincipalRef{Type: actorref.Person, ID: memoryTestOwnerID}, Version: version},
		SignalType: SignalManualReference, Weight: 1, ObservedAt: memoryTestNow().Add(45 * time.Minute),
		EventTime: &eventTime, CreatedAt: memoryTestNow().Add(45 * time.Minute), Status: EvidenceCurrent}
}

func evidenceRemovedFixture(t *testing.T) Evidence {
	record := evidenceFixture(t, agentevent.MomentSource)
	record.Version = 2
	record.Status = EvidenceRemoved
	record.Source, record.EventTime = nil, nil
	record.SignalType, record.Weight = "", 0
	return record
}

func TestEvidenceReferenceShapeUsesOnlyNativeOwnedSourceKinds(t *testing.T) {
	for _, kind := range []agentevent.SourceType{agentevent.MomentSource, agentevent.ParticipationSource, agentevent.SavedPlaceSource} {
		t.Run(string(kind), func(t *testing.T) {
			input := EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: kind, SourceID: strings.ToUpper(evidenceSourceTestID)}
			got, err := NormalizeReference(input)
			if err != nil || got.SourceID != evidenceSourceTestID || got.ExpectedMemoryVersion != 1 || got.SourceType != kind {
				t.Fatal("source address shape failed to normalize independently of authority")
			}
			record := evidenceFixture(t, kind)
			if ValidateEvidence(record) != nil {
				t.Fatal("valid synthetic native-version shape was rejected")
			}
		})
	}
	for _, kind := range []agentevent.SourceType{"", "moment", "ACTIVITY", "PLACE", agentevent.QuerySource, agentevent.ProfileSource, agentevent.PrivatePreferenceSource, agentevent.CommunityMembershipSource, agentevent.CompletionSource, agentevent.VisitSource} {
		t.Run("reject_"+string(kind), func(t *testing.T) {
			got, err := NormalizeReference(EvidenceReferenceInput{ExpectedMemoryVersion: 1, SourceType: kind, SourceID: evidenceSourceTestID})
			if !errors.Is(err, ErrInvalid) || got != (EvidenceReferenceInput{}) {
				t.Fatal("unknown, public-target or unsupported source accepted")
			}
		})
	}
	for _, version := range []int64{0, -1} {
		got, err := NormalizeReference(EvidenceReferenceInput{ExpectedMemoryVersion: version, SourceType: agentevent.MomentSource, SourceID: evidenceSourceTestID})
		if !errors.Is(err, ErrInvalid) || got != (EvidenceReferenceInput{}) {
			t.Fatal("non-positive expected Memory version accepted")
		}
	}
	for _, id := range []string{"", "bad", "00000000-0000-0000-0000-000000000000", " " + evidenceSourceTestID} {
		got, err := NormalizeEvidenceID(id)
		if !errors.Is(err, ErrInvalid) || got != "" {
			t.Fatal("invalid evidence address accepted")
		}
	}
	if id, err := NormalizeEvidenceID(strings.ToUpper(evidenceTestID)); err != nil || id != evidenceTestID {
		t.Fatal("valid uppercase path address failed to canonicalize")
	}
	for _, version := range []int64{0, -1} {
		got, err := NormalizeEvidenceDetach(EvidenceDetachInput{ExpectedVersion: version})
		if !errors.Is(err, ErrInvalid) || got != (EvidenceDetachInput{}) {
			t.Fatal("nonpositive removal version accepted")
		}
	}
	if got, err := NormalizeEvidenceDetach(EvidenceDetachInput{ExpectedVersion: math.MaxInt64}); err != nil || got.ExpectedVersion != math.MaxInt64 {
		t.Fatal("positive shape was incorrectly confused with a service CAS authorization")
	}
}

func TestEvidenceRecordRejectsAuthorityAndPrivateSourceShapeConfusion(t *testing.T) {
	for _, item := range []struct {
		name  string
		edit  func(*Evidence)
		valid bool
	}{
		{"valid", func(*Evidence) {}, true},
		{"max_evidence_version", func(r *Evidence) { r.Version = math.MaxInt64 }, true},
		{"max_memory_version_shape", func(r *Evidence) { r.MemoryVersion = math.MaxInt64 }, true},
		{"schema_missing", func(r *Evidence) { r.SchemaVersion = "" }, false},
		{"schema_unknown", func(r *Evidence) { r.SchemaVersion = "agent-memory-evidence-v2" }, false},
		{"id_zero", func(r *Evidence) { r.ID = "00000000-0000-0000-0000-000000000000" }, false},
		{"id_padded", func(r *Evidence) { r.ID = " " + r.ID }, false},
		{"id_upper_record", func(r *Evidence) { r.ID = strings.ToUpper(r.ID) }, false},
		{"memory_id_bad", func(r *Evidence) { r.MemoryID = "private memory" }, false},
		{"agent_id_bad", func(r *Evidence) { r.AgentID = "bad" }, false},
		{"owner_zero", func(r *Evidence) { r.OwnerID = "00000000-0000-0000-0000-000000000000" }, false},
		{"owner_organization", func(r *Evidence) { r.OwnerType = actorref.Organization }, false},
		{"owner_business", func(r *Evidence) { r.OwnerType = actorref.Business }, false},
		{"owner_community", func(r *Evidence) { r.OwnerType = actorref.Community }, false},
		{"version_zero", func(r *Evidence) { r.Version = 0 }, false},
		{"version_negative", func(r *Evidence) { r.Version = -1 }, false},
		{"memory_version_zero", func(r *Evidence) { r.MemoryVersion = 0 }, false},
		{"memory_version_negative", func(r *Evidence) { r.MemoryVersion = -1 }, false},
		{"source_missing", func(r *Evidence) { r.Source = nil }, false},
		{"source_id_bad", func(r *Evidence) { r.Source.ID = "bad" }, false},
		{"source_id_upper", func(r *Evidence) { r.Source.ID = strings.ToUpper(r.Source.ID) }, false},
		{"source_owner_foreign", func(r *Evidence) { r.Source.Owner.ID = evidenceOtherTestID }, false},
		{"source_owner_organization", func(r *Evidence) { r.Source.Owner.Type = actorref.Organization }, false},
		{"source_activity_target", func(r *Evidence) { r.Source.Type = "ACTIVITY" }, false},
		{"source_place_target", func(r *Evidence) { r.Source.Type = "PLACE" }, false},
		{"source_query", func(r *Evidence) { r.Source.Type = agentevent.QuerySource }, false},
		{"source_body_profile", func(r *Evidence) { r.Source.Type = agentevent.ProfileSource }, false},
		{"source_visit", func(r *Evidence) { r.Source.Type = agentevent.VisitSource }, false},
		{"source_completion", func(r *Evidence) { r.Source.Type = agentevent.CompletionSource }, false},
		{"moment_no_revision", func(r *Evidence) { r.Source.Version.Revision = 0 }, false},
		{"moment_negative_revision", func(r *Evidence) { r.Source.Version.Revision = -1 }, false},
		{"moment_fake_digest", func(r *Evidence) { r.Source.Version.Kind = agentevent.UpdatedAtDigestVersion }, false},
		{"moment_extra_token", func(r *Evidence) { r.Source.Version.Token = strings.Repeat("a", 64) }, false},
		{"signal_inference", func(r *Evidence) { r.SignalType = "MODEL_INFERENCE" }, false},
		{"signal_missing", func(r *Evidence) { r.SignalType = "" }, false},
		{"signal_confirmed", func(r *Evidence) { r.SignalType = "CONFIRMED" }, false},
		{"weight_zero", func(r *Evidence) { r.Weight = 0 }, false},
		{"weight_uncalibrated", func(r *Evidence) { r.Weight = 0.8 }, false},
		{"weight_large", func(r *Evidence) { r.Weight = 2 }, false},
		{"weight_nan", func(r *Evidence) { r.Weight = math.NaN() }, false},
		{"weight_inf", func(r *Evidence) { r.Weight = math.Inf(1) }, false},
		{"observed_zero", func(r *Evidence) { r.ObservedAt = time.Time{} }, false},
		{"observed_outside_json", func(r *Evidence) { r.ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, false},
		{"created_zero", func(r *Evidence) { r.CreatedAt = time.Time{} }, false},
		{"created_year_zero", func(r *Evidence) { r.CreatedAt = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC) }, false},
		{"event_missing", func(r *Evidence) { r.EventTime = nil }, false},
		{"event_zero", func(r *Evidence) { value := time.Time{}; r.EventTime = &value }, false},
		{"event_outside_json", func(r *Evidence) { value := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC); r.EventTime = &value }, false},
		{"event_after_observation", func(r *Evidence) { value := r.ObservedAt.Add(time.Nanosecond); r.EventTime = &value }, false},
		{"status_unknown", func(r *Evidence) { r.Status = "VERIFIED" }, false},
		{"status_removed_with_payload", func(r *Evidence) { r.Status = EvidenceRemoved }, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			record := evidenceFixture(t, agentevent.MomentSource)
			item.edit(&record)
			err := ValidateEvidence(record)
			if (err == nil) != item.valid || (!item.valid && !errors.Is(err, ErrInvalid)) {
				t.Fatalf("record shape result differs: valid=%t err=%v", item.valid, err)
			}
		})
	}
	for _, sourceType := range []agentevent.SourceType{agentevent.ParticipationSource, agentevent.SavedPlaceSource} {
		for _, item := range []struct {
			name string
			edit func(*agentevent.SourceVersion)
		}{
			{"fake_native_revision", func(v *agentevent.SourceVersion) { v.Revision = 1 }},
			{"token_missing", func(v *agentevent.SourceVersion) { v.Token = "" }},
			{"token_short", func(v *agentevent.SourceVersion) { v.Token = strings.Repeat("a", 63) }},
			{"token_long", func(v *agentevent.SourceVersion) { v.Token = strings.Repeat("a", 65) }},
			{"token_uppercase", func(v *agentevent.SourceVersion) { v.Token = strings.Repeat("A", 64) }},
			{"token_nonhex", func(v *agentevent.SourceVersion) { v.Token = strings.Repeat("z", 64) }},
			{"wrong_revision_kind", func(v *agentevent.SourceVersion) { v.Kind = agentevent.RevisionVersion; v.Revision = 1; v.Token = "" }},
			{"unknown_kind", func(v *agentevent.SourceVersion) { v.Kind = "VERIFIED" }},
		} {
			t.Run(string(sourceType)+"_"+item.name, func(t *testing.T) {
				record := evidenceFixture(t, sourceType)
				item.edit(&record.Source.Version)
				if !errors.Is(ValidateEvidence(record), ErrInvalid) {
					t.Fatal("unsupported source version shape accepted")
				}
			})
		}
	}
}

func TestEvidenceRemovedHasNoSourcePayload(t *testing.T) {
	for _, item := range []struct {
		name  string
		edit  func(*Evidence)
		valid bool
	}{
		{"cleared", func(*Evidence) {}, true},
		{"source", func(r *Evidence) { r.Source = evidenceFixture(t, agentevent.MomentSource).Source }, false},
		{"signal", func(r *Evidence) { r.SignalType = SignalManualReference }, false},
		{"weight", func(r *Evidence) { r.Weight = 1 }, false},
		{"weight_nan", func(r *Evidence) { r.Weight = math.NaN() }, false},
		{"event_time", func(r *Evidence) { r.EventTime = evidenceFixture(t, agentevent.MomentSource).EventTime }, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			record := evidenceRemovedFixture(t)
			item.edit(&record)
			if (ValidateEvidence(record) == nil) != item.valid {
				t.Fatal("removed evidence source-clear boundary differs")
			}
		})
	}
	encoded, err := json.Marshal(evidenceRemovedFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"source"`, `"signalType"`, `"weight"`, `"eventTime"`, evidenceSourceTestID} {
		if strings.Contains(string(encoded), key) {
			t.Fatal("removed response retained source payload or source address")
		}
	}
}

func TestEvidenceCreationFollowsObservationAtExactTimePrecision(t *testing.T) {
	for _, item := range []struct {
		name    string
		removed bool
		edit    func(*Evidence)
		valid   bool
	}{
		{"same_instant", false, func(*Evidence) {}, true},
		{"one_nanosecond_after", false, func(e *Evidence) { e.CreatedAt = e.ObservedAt.Add(time.Nanosecond) }, true},
		{"one_nanosecond_before", false, func(e *Evidence) { e.CreatedAt = e.ObservedAt.Add(-time.Nanosecond) }, false},
		{"one_microsecond_after", false, func(e *Evidence) { e.CreatedAt = e.ObservedAt.Add(time.Microsecond) }, true},
		{"one_microsecond_before", false, func(e *Evidence) { e.CreatedAt = e.ObservedAt.Add(-time.Microsecond) }, false},
		{"same_instant_different_offset", false, func(e *Evidence) {
			e.ObservedAt = e.ObservedAt.In(time.FixedZone("test-west", -7*60*60))
			e.CreatedAt = e.CreatedAt.In(time.FixedZone("test-east", 8*60*60))
		}, true},
		{"removed_same_instant", true, func(*Evidence) {}, true},
		{"removed_one_nanosecond_after", true, func(e *Evidence) { e.CreatedAt = e.ObservedAt.Add(time.Nanosecond) }, true},
		{"removed_one_nanosecond_before", true, func(e *Evidence) { e.CreatedAt = e.ObservedAt.Add(-time.Nanosecond) }, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			e := evidenceFixture(t, agentevent.MomentSource)
			if item.removed {
				e = evidenceRemovedFixture(t)
			}
			item.edit(&e)
			err := ValidateEvidence(e)
			if (err == nil) != item.valid || (!item.valid && !errors.Is(err, ErrInvalid)) {
				t.Fatal("creation/observation order compared truncated times or timezone labels")
			}
		})
	}

	t.Run("preserve_native_fractional_source_time", func(t *testing.T) {
		e := evidenceFixture(t, agentevent.MomentSource)
		*e.EventTime = e.EventTime.Add(123456789 * time.Nanosecond)
		e.ObservedAt = e.ObservedAt.Add(987654321 * time.Nanosecond)
		e.CreatedAt = e.ObservedAt.Add(time.Nanosecond)
		if ValidateEvidence(e) != nil {
			t.Fatal("valid precise native metadata rejected")
		}
		encoded, err := json.Marshal(e)
		if err != nil {
			t.Fatal("valid metadata could not be encoded")
		}
		var decoded Evidence
		if json.Unmarshal(encoded, &decoded) != nil || ValidateEvidence(decoded) != nil ||
			!decoded.EventTime.Equal(*e.EventTime) || !decoded.ObservedAt.Equal(e.ObservedAt) || !decoded.CreatedAt.Equal(e.CreatedAt) {
			t.Fatal("metadata JSON changed native timestamp precision")
		}
		out, err := BuildOwnProvenance(memoryTestRecord(t), []Evidence{e}, evidenceTestNow())
		if err != nil || len(out.Evidence) != 1 || !out.Evidence[0].EventTime.Equal(*e.EventTime) ||
			!out.Evidence[0].ObservedAt.Equal(e.ObservedAt) || !out.Evidence[0].CreatedAt.Equal(e.CreatedAt) {
			t.Fatal("owner explanation changed native timestamp precision")
		}
	})
}

func TestEvidenceProvenanceExplainsManualAssociationWithoutInference(t *testing.T) {
	memory := memoryTestRecord(t)
	for _, visibility := range []Visibility{VisibilityPrivate, VisibilityAgentOnly} {
		t.Run(string(visibility), func(t *testing.T) {
			memory.Visibility = visibility
			empty, err := BuildOwnProvenance(memory, nil, evidenceTestNow())
			if err != nil || empty.Declaration != "本人明确填写" || empty.Explanation != "本人明确填写" || empty.Evidence == nil || len(empty.Evidence) != 0 {
				t.Fatal("bare explicit declaration fabricated sources or lost bounded Chinese explanation")
			}
		})
	}
	records := []Evidence{}
	for i, kind := range []agentevent.SourceType{agentevent.MomentSource, agentevent.ParticipationSource, agentevent.SavedPlaceSource} {
		record := evidenceFixture(t, kind)
		record.ID = []string{evidenceTestID, evidenceOtherTestID, evidenceSourceTestID}[i]
		records = append(records, record)
	}
	out, err := BuildOwnProvenance(memory, records, evidenceTestNow())
	if err != nil || out.Explanation != "本人关联了1条私人记录、1次报名记录、1条收藏记录" || out.Declaration != "本人明确填写" || len(out.Evidence) != 3 {
		t.Fatal("manual provenance counts or Chinese wording differs")
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{memory.Summary, `"summary"`, `"structuredValue"`, `"body"`, `"title"`, "因为", "亲历", "去过", "参加过", "喜欢", "probability", "consent", "grant"} {
		if strings.Contains(string(body), term) {
			t.Fatal("provenance copied content, asserted preference/experience or created an authority claim")
		}
	}
	firstSource := *out.Evidence[0].Source
	firstEventTime := *out.Evidence[0].EventTime
	records[1].Source.ID = evidenceOtherTestID
	*records[1].EventTime = time.Time{}
	if !reflect.DeepEqual(firstSource, *out.Evidence[0].Source) || !firstEventTime.Equal(*out.Evidence[0].EventTime) {
		t.Fatal("provenance source/time pointers alias caller memory")
	}
}

func TestEvidenceProvenanceRejectsInvalidBindingAndStaleMemory(t *testing.T) {
	for _, item := range []struct {
		name     string
		edit     func(*Record, *Evidence, *time.Time)
		expected error
	}{
		{"foreign_memory", func(_ *Record, e *Evidence, _ *time.Time) { e.MemoryID = evidenceOtherTestID }, ErrForbidden},
		{"foreign_agent", func(_ *Record, e *Evidence, _ *time.Time) { e.AgentID = evidenceOtherTestID }, ErrForbidden},
		{"foreign_owner", func(_ *Record, e *Evidence, _ *time.Time) {
			e.OwnerID = evidenceOtherTestID
			e.Source.Owner.ID = evidenceOtherTestID
		}, ErrForbidden},
		{"stale_memory_version", func(_ *Record, e *Evidence, _ *time.Time) { e.MemoryVersion = 2 }, ErrConflict},
		{"invalid_source", func(_ *Record, e *Evidence, _ *time.Time) { e.Source.Type = agentevent.QuerySource }, ErrInvalid},
		{"future_observed", func(_ *Record, e *Evidence, n *time.Time) { e.ObservedAt = n.Add(time.Nanosecond) }, ErrInvalid},
		{"future_created", func(_ *Record, e *Evidence, n *time.Time) { e.CreatedAt = n.Add(time.Nanosecond) }, ErrInvalid},
		{"invalid_now", func(_ *Record, _ *Evidence, n *time.Time) { *n = time.Time{} }, ErrInvalid},
		{"invalid_memory", func(m *Record, _ *Evidence, _ *time.Time) { m.SchemaVersion = "UNKNOWN" }, ErrInvalid},
		{"inferred_unavailable", func(m *Record, _ *Evidence, _ *time.Time) {
			m.SourceType = SourceInferred
			m.Confidence = 0.4
			m.Status = StatusPendingReview
		}, ErrUnavailable},
		{"expired_status", func(m *Record, _ *Evidence, _ *time.Time) { m.Status = StatusExpired }, ErrNotFound},
		{"deleted", func(m *Record, _ *Evidence, _ *time.Time) {
			m.Status = StatusDeleted
			m.Summary = ""
			m.StructuredValue = json.RawMessage(`{}`)
		}, ErrNotFound},
		{"exact_deadline", func(m *Record, _ *Evidence, n *time.Time) { *n = m.ValidUntil }, ErrNotFound},
		{"after_deadline", func(m *Record, _ *Evidence, n *time.Time) { *n = m.ValidUntil.Add(time.Nanosecond) }, ErrNotFound},
		{"before_validity", func(m *Record, _ *Evidence, n *time.Time) { *n = m.ValidFrom.Add(-time.Nanosecond) }, ErrNotFound},
	} {
		t.Run(item.name, func(t *testing.T) {
			m := memoryTestRecord(t)
			e := evidenceFixture(t, agentevent.MomentSource)
			now := evidenceTestNow()
			item.edit(&m, &e, &now)
			out, err := BuildOwnProvenance(m, []Evidence{e}, now)
			if !errors.Is(err, item.expected) || !reflect.DeepEqual(out, Provenance{}) {
				t.Fatalf("provenance rejection differs: error=%v expected=%v", err, item.expected)
			}
		})
	}
}

func TestEvidenceProvenanceDeduplicatesSourcesAndClearedRecords(t *testing.T) {
	memory := memoryTestRecord(t)
	first := evidenceFixture(t, agentevent.MomentSource)
	second := evidenceFixture(t, agentevent.MomentSource)
	second.ID = evidenceOtherTestID
	out, err := BuildOwnProvenance(memory, []Evidence{first, second}, evidenceTestNow())
	if err != nil || len(out.Evidence) != 1 || out.Explanation != "本人关联了1条私人记录" {
		t.Fatal("same source repeated across Evidence IDs became several independent signals")
	}
	reversed, err := BuildOwnProvenance(memory, []Evidence{second, first}, evidenceTestNow())
	if err != nil || !reflect.DeepEqual(out, reversed) {
		t.Fatal("equivalent repeated references depend on input order")
	}
	second.Source.ID = evidenceOtherTestID
	out, err = BuildOwnProvenance(memory, []Evidence{first, second}, evidenceTestNow())
	if err != nil || len(out.Evidence) != 2 || out.Explanation != "本人关联了2条私人记录" {
		t.Fatal("two distinct owned references were not counted separately")
	}
	removed := evidenceRemovedFixture(t)
	removed.ID = evidenceSourceTestID
	removed.MemoryVersion = 7 // historical cleared control rows never counted.
	out, err = BuildOwnProvenance(memory, []Evidence{first, removed}, evidenceTestNow())
	if err != nil || len(out.Evidence) != 1 {
		t.Fatal("removed source/control record entered current provenance")
	}
	out, err = BuildOwnProvenance(memory, []Evidence{first, first}, evidenceTestNow())
	if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(out, Provenance{}) {
		t.Fatal("duplicate Evidence address was accepted")
	}
	left, err := BuildOwnProvenance(memory, []Evidence{first, second}, evidenceTestNow())
	if err != nil {
		t.Fatal(err)
	}
	right, err := BuildOwnProvenance(memory, []Evidence{second, first}, evidenceTestNow())
	if err != nil || !reflect.DeepEqual(left, right) {
		t.Fatal("provenance order depends on input iteration")
	}
}

func TestEvidenceProvenanceRejectsConflictingSnapshotsOfSameSource(t *testing.T) {
	for _, item := range []struct {
		name string
		kind agentevent.SourceType
		edit func(*Evidence)
	}{
		{"moment_revision", agentevent.MomentSource, func(e *Evidence) { e.Source.Version.Revision++ }},
		{"moment_source_time", agentevent.MomentSource, func(e *Evidence) { *e.EventTime = e.EventTime.Add(-time.Second) }},
		{"participation_digest", agentevent.ParticipationSource, func(e *Evidence) { e.Source.Version.Token = strings.Repeat("a", 64) }},
		{"bookmark_source_time", agentevent.SavedPlaceSource, func(e *Evidence) { *e.EventTime = e.EventTime.Add(-time.Second) }},
	} {
		t.Run(item.name, func(t *testing.T) {
			first := evidenceFixture(t, item.kind)
			second := evidenceFixture(t, item.kind)
			second.ID = evidenceOtherTestID
			item.edit(&second)
			if ValidateEvidence(first) != nil || ValidateEvidence(second) != nil {
				t.Fatal("conflicting snapshots must each have valid metadata shape")
			}
			for _, records := range [][]Evidence{{first, second}, {second, first}} {
				out, err := BuildOwnProvenance(memoryTestRecord(t), records, evidenceTestNow())
				if !errors.Is(err, ErrConflict) || !reflect.DeepEqual(out, Provenance{}) {
					t.Fatal("same source with conflicting version/time selected an arbitrary snapshot")
				}
			}
		})
	}
}

func TestEvidenceProvenanceResponseMetadataAndDisplayValidation(t *testing.T) {
	fixture := func(t *testing.T) Provenance {
		t.Helper()
		p, err := BuildOwnProvenance(memoryTestRecord(t), []Evidence{evidenceFixture(t, agentevent.MomentSource)}, evidenceTestNow())
		if err != nil {
			t.Fatal("valid response fixture failed")
		}
		return p
	}
	for _, item := range []struct {
		name  string
		edit  func(*Provenance)
		valid bool
	}{
		{"valid", func(*Provenance) {}, true},
		{"schema", func(p *Provenance) { p.SchemaVersion = "unknown" }, false},
		{"invalid_memory_id", func(p *Provenance) { p.MemoryID = "bad" }, false},
		{"uppercase_memory_id", func(p *Provenance) { p.MemoryID = strings.ToUpper(p.MemoryID) }, false},
		{"nonpositive_version", func(p *Provenance) { p.MemoryVersion = 0 }, false},
		{"declaration_inference", func(p *Provenance) { p.Declaration = "模型已证实偏好" }, false},
		{"private_explanation", func(p *Provenance) { p.Explanation = "private-canary-body" }, false},
		{"invented_experience", func(p *Provenance) { p.Explanation = "本人参加过活动" }, false},
		{"wrong_count", func(p *Provenance) { p.Explanation = "本人关联了2条私人记录" }, false},
		{"wrong_signal_word", func(p *Provenance) { p.Explanation = "本人关联了1次亲历记录" }, false},
		{"nil_list", func(p *Provenance) { p.Evidence = nil; p.Explanation = "本人明确填写" }, false},
		{"empty_list", func(p *Provenance) { p.Evidence = []Evidence{}; p.Explanation = "本人明确填写" }, true},
		{"empty_wrong_count", func(p *Provenance) { p.Evidence = []Evidence{} }, false},
		{"evidence_invalid", func(p *Provenance) { p.Evidence[0].Version = 0 }, false},
		{"evidence_memory_mismatch", func(p *Provenance) { p.Evidence[0].MemoryID = evidenceOtherTestID }, false},
		{"evidence_version_mismatch", func(p *Provenance) { p.Evidence[0].MemoryVersion++ }, false},
		{"removed", func(p *Provenance) { p.Evidence[0] = evidenceRemovedFixture(t) }, false},
		{"duplicate_id", func(p *Provenance) {
			p.Evidence = append(p.Evidence, p.Evidence[0])
			p.Explanation = "本人关联了2条私人记录"
		}, false},
		{"duplicate_source", func(p *Provenance) {
			e := evidenceFixture(t, agentevent.MomentSource)
			e.ID = evidenceOtherTestID
			p.Evidence = append(p.Evidence, e)
			p.Explanation = "本人关联了2条私人记录"
		}, false},
		{"mixed_owners", func(p *Provenance) {
			e := evidenceFixture(t, agentevent.MomentSource)
			e.ID = evidenceOtherTestID
			e.Source.ID = evidenceOtherTestID
			e.OwnerID = evidenceOtherTestID
			e.Source.Owner.ID = evidenceOtherTestID
			p.Evidence = append(p.Evidence, e)
			p.Explanation = "本人关联了2条私人记录"
		}, false},
		{"mixed_agents", func(p *Provenance) {
			e := evidenceFixture(t, agentevent.MomentSource)
			e.ID = evidenceOtherTestID
			e.Source.ID = evidenceOtherTestID
			e.AgentID = evidenceOtherTestID
			p.Evidence = append(p.Evidence, e)
			p.Explanation = "本人关联了2条私人记录"
		}, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			p := fixture(t)
			item.edit(&p)
			err := ValidateProvenance(p)
			if (err == nil) != item.valid || (!item.valid && !errors.Is(err, ErrInvalid)) {
				t.Fatal("response metadata/display validator accepted invalid claims or rejected valid shape")
			}
		})
	}
	for _, total := range []int{MaxProvenanceEvidence, MaxProvenanceEvidence + 1} {
		t.Run(fmt.Sprintf("bounded_%d", total), func(t *testing.T) {
			p := fixture(t)
			p.Evidence = []Evidence{}
			for i := 1; i <= total; i++ {
				e := evidenceFixture(t, agentevent.MomentSource)
				e.ID = fmt.Sprintf("81000000-0000-4000-8000-%012d", i)
				e.Source.ID = fmt.Sprintf("82000000-0000-4000-8000-%012d", i)
				p.Evidence = append(p.Evidence, e)
			}
			p.Explanation = fmt.Sprintf("本人关联了%d条私人记录", total)
			if (ValidateProvenance(p) == nil) != (total == MaxProvenanceEvidence) {
				t.Fatal("response list bound differs")
			}
			out, err := BuildOwnProvenance(memoryTestRecord(t), p.Evidence, evidenceTestNow())
			if total == MaxProvenanceEvidence {
				if err != nil || ValidateProvenance(out) != nil {
					t.Fatal("builder and validator disagree at exact bound")
				}
			} else if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(out, Provenance{}) {
				t.Fatal("builder returned over-bound provenance")
			}
		})
	}
	t.Run("three_native_counts", func(t *testing.T) {
		records := []Evidence{}
		for i, kind := range []agentevent.SourceType{agentevent.MomentSource, agentevent.ParticipationSource, agentevent.SavedPlaceSource} {
			e := evidenceFixture(t, kind)
			e.ID = []string{evidenceTestID, evidenceOtherTestID, evidenceSourceTestID}[i]
			records = append(records, e)
		}
		p, err := BuildOwnProvenance(memoryTestRecord(t), records, evidenceTestNow())
		if err != nil || ValidateProvenance(p) != nil || p.Explanation != "本人关联了1条私人记录、1次报名记录、1条收藏记录" {
			t.Fatal("renderer was not shared across typed native categories")
		}
	})
}
