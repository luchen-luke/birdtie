package agentmemory

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const memoryTestID = "a1848c64-7225-4b33-8c89-dda8fa432e0b"
const memoryTestAgentID = "f9543c3d-bf5d-481a-8327-95c8429bbf94"
const memoryTestOwnerID = "d2575259-e104-4e31-b68c-711fc398d076"

func memoryTestNow() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }

func memoryTestPut() PutInput {
	return PutInput{ExpectedVersion: 0, MemoryType: TypePreference,
		MemoryKey: "activity.hiking", Summary: "我喜欢徒步，这是本人主动保存的声明。",
		StructuredValue: json.RawMessage(`{"activity":"hiking","preferred":true}`),
		Visibility:      VisibilityPrivate, ValidUntil: memoryTestNow().Add(24 * time.Hour)}
}

func memoryTestRecord(t *testing.T) Record {
	t.Helper()
	record, err := NewExplicit(memoryTestID, memoryTestAgentID,
		actorref.PrincipalRef{Type: actorref.Person, ID: memoryTestOwnerID}, 1,
		memoryTestPut(), memoryTestNow(), memoryTestNow())
	if err != nil {
		t.Fatal("valid synthetic declaration construction failed")
	}
	return record
}

func TestMemoryNormalizationValidExplicit(t *testing.T) {
	for _, memoryType := range MemoryTypes() {
		t.Run(string(memoryType), func(t *testing.T) {
			for _, visibility := range []Visibility{VisibilityPrivate, VisibilityAgentOnly} {
				t.Run(string(visibility), func(t *testing.T) {
					input := memoryTestPut()
					input.MemoryType, input.Visibility = memoryType, visibility
					input.Summary = " \t我喜欢徒步\r\n明确声明\n "
					input.StructuredValue = json.RawMessage(`{ "preferred": true, "activity": "hiking" }`)
					normalized, err := NormalizePutInput(input, memoryTestNow())
					if err != nil || normalized.Summary != "我喜欢徒步\n明确声明" ||
						string(normalized.StructuredValue) != `{"activity":"hiking","preferred":true}` {
						t.Fatal("explicit contents did not normalize independently of memory category")
					}
					input.StructuredValue[0] = '['
					if normalized.StructuredValue[0] != '{' {
						t.Fatal("normalized structured value aliased caller bytes")
					}
				})
			}
		})
	}
}

func TestMemoryPutBoundaries(t *testing.T) {
	cases := []struct {
		name string
		edit func(*PutInput)
		ok   bool
	}{
		{"zero_create_version", func(v *PutInput) { v.ExpectedVersion = 0 }, true},
		{"positive_update_version", func(v *PutInput) { v.ExpectedVersion = 7 }, true},
		{"max_version_shape_not_cas_permission", func(v *PutInput) { v.ExpectedVersion = math.MaxInt64 }, true},
		{"negative_version", func(v *PutInput) { v.ExpectedVersion = -1 }, false},
		{"unknown_type", func(v *PutInput) { v.MemoryType = "TRAIT" }, false},
		{"lowercase_type", func(v *PutInput) { v.MemoryType = "preference" }, false},
		{"blank_key", func(v *PutInput) { v.MemoryKey = "" }, false},
		{"uppercase_key", func(v *PutInput) { v.MemoryKey = "Hiking" }, false},
		{"unicode_key", func(v *PutInput) { v.MemoryKey = "喜欢徒步" }, false},
		{"padded_key", func(v *PutInput) { v.MemoryKey = " hiking" }, false},
		{"key_100", func(v *PutInput) { v.MemoryKey = strings.Repeat("a", 100) }, true},
		{"key_101", func(v *PutInput) { v.MemoryKey = strings.Repeat("a", 101) }, false},
		{"key_control", func(v *PutInput) { v.MemoryKey = "a\nb" }, false},
		{"empty_summary", func(v *PutInput) { v.Summary = " \t\n " }, false},
		{"summary_exact_bytes", func(v *PutInput) { v.Summary = strings.Repeat("中", 400) }, true},
		{"summary_over_bytes", func(v *PutInput) { v.Summary = strings.Repeat("中", 401) }, false},
		{"summary_null_control", func(v *PutInput) { v.Summary = "a\x00b" }, false},
		{"summary_invalid_utf8", func(v *PutInput) { v.Summary = string([]byte{0xff}) }, false},
		{"nil_value", func(v *PutInput) { v.StructuredValue = nil }, false},
		{"null_value", func(v *PutInput) { v.StructuredValue = json.RawMessage(`null`) }, false},
		{"array_value", func(v *PutInput) { v.StructuredValue = json.RawMessage(`[]`) }, false},
		{"empty_object", func(v *PutInput) { v.StructuredValue = json.RawMessage(`{}`) }, true},
		{"public_visibility", func(v *PutInput) { v.Visibility = "PUBLIC" }, false},
		{"connections_visibility", func(v *PutInput) { v.Visibility = "CONNECTIONS" }, false},
		{"workspace_visibility", func(v *PutInput) { v.Visibility = "WORKSPACE_PRIVATE" }, false},
		{"blank_visibility", func(v *PutInput) { v.Visibility = "" }, false},
		{"zero_until", func(v *PutInput) { v.ValidUntil = time.Time{} }, false},
		{"expiry_now", func(v *PutInput) { v.ValidUntil = memoryTestNow() }, false},
		{"expiry_past", func(v *PutInput) { v.ValidUntil = memoryTestNow().Add(-time.Nanosecond) }, false},
		{"expiry_one_nanosecond_future", func(v *PutInput) { v.ValidUntil = memoryTestNow().Add(time.Nanosecond) }, true},
		{"expiry_365days", func(v *PutInput) { v.ValidUntil = memoryTestNow().Add(MaxValidity) }, true},
		{"expiry_beyond_365days", func(v *PutInput) { v.ValidUntil = memoryTestNow().Add(MaxValidity + time.Nanosecond) }, false},
		{"expiry_json_unrepresentable", func(v *PutInput) { v.ValidUntil = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			input := memoryTestPut()
			item.edit(&input)
			actual, err := NormalizePutInput(input, memoryTestNow())
			if item.ok {
				if err != nil {
					t.Fatal("bounded input rejected")
				}
			} else if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(actual, PutInput{}) {
				t.Fatal("invalid input returned data or an unexpected error")
			}
		})
	}
	t.Run("zero_server_clock", func(t *testing.T) {
		if _, err := NormalizePutInput(memoryTestPut(), time.Time{}); !errors.Is(err, ErrInvalid) {
			t.Fatal("missing real server write time accepted")
		}
	})
}

func TestMemoryIDNamespacesAndOwner(t *testing.T) {
	for _, id := range []string{"", "bad", "00000000-0000-0000-0000-000000000000", " " + memoryTestID, memoryTestID + " ", "community-aberdeen"} {
		t.Run(id, func(t *testing.T) {
			if normalized, err := NormalizeMemoryID(id); !errors.Is(err, ErrInvalid) || normalized != "" {
				t.Fatal("invalid memory UUID accepted")
			}
		})
	}
	t.Run("uuid_case_normalizes", func(t *testing.T) {
		id, err := NormalizeMemoryID(strings.ToUpper(memoryTestID))
		if err != nil || id != memoryTestID {
			t.Fatal("stable UUID case normalization failed")
		}
	})
	for _, principalType := range []actorref.Type{actorref.Organization, actorref.Business, "COMMUNITY", "PLACE", "CITY"} {
		t.Run(string(principalType), func(t *testing.T) {
			actual, err := NewExplicit(memoryTestID, memoryTestAgentID,
				actorref.PrincipalRef{Type: principalType, ID: memoryTestOwnerID}, 1,
				memoryTestPut(), memoryTestNow(), memoryTestNow())
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(actual, Record{}) {
				t.Fatal("reserved non-Person shape became a Person declaration")
			}
		})
	}
}

func TestMemoryExplicitConstruction(t *testing.T) {
	record := memoryTestRecord(t)
	if record.SchemaVersion != SchemaV1 || record.Version != 1 || record.ID != memoryTestID ||
		record.AgentID != memoryTestAgentID || record.OwnerID != memoryTestOwnerID ||
		record.SourceType != SourceExplicit || record.Status != StatusActive || record.Confidence != 1 ||
		record.LastReinforcedAt != nil || ValidateRecord(record) != nil {
		t.Fatal("explicit declaration contract is incorrect")
	}
	owner, err := record.OwnerRef()
	if err != nil || owner.Type != actorref.Person || owner.ID != memoryTestOwnerID {
		t.Fatal("typed owner reference lost stable identity")
	}
	t.Run("update_exact_current_version", func(t *testing.T) {
		input := memoryTestPut()
		input.ExpectedVersion = 8
		updated, err := NewExplicit(memoryTestID, memoryTestAgentID, owner, 9, input,
			memoryTestNow().Add(time.Minute), memoryTestNow())
		if err != nil || updated.Version != 9 || !updated.CreatedAt.Equal(record.CreatedAt) ||
			!updated.UpdatedAt.After(record.CreatedAt) {
			t.Fatal("update shape failed independent version semantics")
		}
	})
	for _, item := range []struct {
		name      string
		id, agent string
		version   int64
		expected  int64
		created   time.Time
	}{
		{"bad_memory", "bad", memoryTestAgentID, 1, 0, memoryTestNow()},
		{"bad_agent", memoryTestID, "bad", 1, 0, memoryTestNow()},
		{"zero_version", memoryTestID, memoryTestAgentID, 0, 0, memoryTestNow()},
		{"wrong_next_version", memoryTestID, memoryTestAgentID, 3, 0, memoryTestNow()},
		{"overflow", memoryTestID, memoryTestAgentID, math.MinInt64, math.MaxInt64, memoryTestNow()},
		{"missing_created", memoryTestID, memoryTestAgentID, 1, 0, time.Time{}},
		{"future_created", memoryTestID, memoryTestAgentID, 1, 0, memoryTestNow().Add(time.Minute)},
	} {
		t.Run(item.name, func(t *testing.T) {
			input := memoryTestPut()
			input.ExpectedVersion = item.expected
			actual, err := NewExplicit(item.id, item.agent, owner, item.version, input, memoryTestNow(), item.created)
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(actual, Record{}) {
				t.Fatal("invalid server record construction returned contents")
			}
		})
	}
}

func TestMemoryRecordSourceAndLifecycleShapes(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Record)
		ok   bool
	}{
		{"explicit_active", func(*Record) {}, true},
		{"explicit_expired", func(v *Record) { v.Status = StatusExpired }, true},
		{"explicit_pending_not_inference", func(v *Record) { v.Status = StatusPendingReview }, false},
		{"explicit_score_not_probability", func(v *Record) { v.Confidence = 0.9 }, false},
		{"explicit_no_fake_reinforcement", func(v *Record) { at := v.CreatedAt; v.LastReinforcedAt = &at }, false},
		{"inferred_pending_shape_only", func(v *Record) { v.SourceType = SourceInferred; v.Status = StatusPendingReview; v.Confidence = 0.4 }, true},
		{"inferred_expired_shape_only", func(v *Record) { v.SourceType = SourceInferred; v.Status = StatusExpired; v.Confidence = 0.4 }, true},
		{"inferred_active_forbidden", func(v *Record) { v.SourceType = SourceInferred }, false},
		{"unknown_source", func(v *Record) { v.SourceType = "QUERY" }, false},
		{"unknown_status", func(v *Record) { v.Status = "VERIFIED" }, false},
		{"nan", func(v *Record) { v.Confidence = math.NaN() }, false},
		{"inf", func(v *Record) { v.Confidence = math.Inf(1) }, false},
		{"below_zero", func(v *Record) { v.Confidence = -0.01 }, false},
		{"above_one", func(v *Record) { v.Confidence = 1.01 }, false},
		{"wrong_schema", func(v *Record) { v.SchemaVersion = "memory-v0" }, false},
		{"bad_uuid", func(v *Record) { v.ID = "bad" }, false},
		{"bad_agent", func(v *Record) { v.AgentID = "bad" }, false},
		{"bad_owner", func(v *Record) { v.OwnerID = "bad" }, false},
		{"org_owner_not_enabled", func(v *Record) { v.OwnerType = actorref.Organization }, false},
		{"business_owner_not_enabled", func(v *Record) { v.OwnerType = actorref.Business }, false},
		{"zero_version", func(v *Record) { v.Version = 0 }, false},
		{"max_positive_version", func(v *Record) { v.Version = math.MaxInt64 }, true},
		{"noncanonical_summary", func(v *Record) { v.Summary = " " + v.Summary }, false},
		{"empty_active_summary", func(v *Record) { v.Summary = "" }, false},
		{"expired_at_same_start", func(v *Record) { v.ValidUntil = v.ValidFrom }, false},
		{"validity_exact365days", func(v *Record) { v.ValidUntil = v.ValidFrom.Add(MaxValidity) }, true},
		{"validity_beyond365days", func(v *Record) { v.ValidUntil = v.ValidFrom.Add(MaxValidity + time.Nanosecond) }, false},
		{"zero_from", func(v *Record) { v.ValidFrom = time.Time{} }, false},
		{"zero_created", func(v *Record) { v.CreatedAt = time.Time{} }, false},
		{"zero_updated", func(v *Record) { v.UpdatedAt = time.Time{} }, false},
		{"updated_before_created", func(v *Record) { v.UpdatedAt = v.CreatedAt.Add(-time.Nanosecond) }, false},
		{"deleted_with_contents", func(v *Record) { v.Status = StatusDeleted }, false},
		{"deleted_with_value", func(v *Record) { v.Status = StatusDeleted; v.Summary = "" }, false},
		{"deleted_tombstone", func(v *Record) { v.Status = StatusDeleted; v.Summary = ""; v.StructuredValue = json.RawMessage(`{}`) }, true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			record := memoryTestRecord(t)
			item.edit(&record)
			err := ValidateRecord(record)
			if (err == nil) != item.ok || (err != nil && !errors.Is(err, ErrInvalid)) {
				t.Fatal("source/lifecycle shape did not enforce its explicit boundary")
			}
		})
	}
}

func TestMemoryExplicitOrderingNotScorePromotion(t *testing.T) {
	explicit := memoryTestRecord(t)
	inferred := explicit
	inferred.ID, inferred.SourceType, inferred.Status = memoryTestAgentID, SourceInferred, StatusPendingReview
	inferred.UpdatedAt, inferred.Confidence = explicit.UpdatedAt.Add(time.Hour), 1
	input := []Record{inferred, explicit}
	ordered := SortRecords(input)
	if ordered[0].SourceType != SourceExplicit || ordered[1].SourceType != SourceInferred || input[0].SourceType != SourceInferred {
		t.Fatal("newer high-score inference displaced the explicit declaration or mutated caller ordering")
	}
	if SortRecords(nil) == nil {
		t.Fatal("empty owner list must have a non-nil JSON array shape")
	}
}

func TestMemoryAccessRemainsServerOnly(t *testing.T) {
	access := Access{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: memoryTestOwnerID}}
	access.SessionDigest[0] = 1
	if agentprofile.ValidatePrivateAccess(access) != nil {
		t.Fatal("valid credential shape rejected")
	}
	if _, err := json.Marshal(access); err == nil {
		t.Fatal("server session credential serialized")
	}
	if err := json.Unmarshal([]byte(`{"confirmed":true,"ownerId":"`+memoryTestOwnerID+`"}`), &access); err == nil || !reflect.DeepEqual(access, Access{}) {
		t.Fatal("wire declaration created an access credential")
	}
}
