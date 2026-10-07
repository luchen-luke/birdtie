package agentprofilecompletion

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const testOwner = "94000000-0000-4000-8000-000000000001"
const testAgent = "94000000-0000-4000-8000-000000000002"
const testMemory = "94000000-0000-4000-8000-000000000003"
const testPreview = "94000000-0000-4000-8000-000000000004"

var testAt = time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)

func sourceFixture(category string) agentmemory.Record {
	value, _ := json.Marshal(map[string]string{"activityCategory": category, "nature": "human-declaration"})
	return agentmemory.Record{SchemaVersion: agentmemory.SchemaV1, ID: testMemory, AgentID: testAgent, OwnerType: actorref.Person, OwnerID: testOwner, Version: 1, MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:" + category, Summary: agentmemorycandidate.Statement(category), StructuredValue: value, Confidence: 1, SourceType: agentmemory.SourceExplicit, Visibility: agentmemory.VisibilityPrivate, Status: agentmemory.StatusActive, ValidFrom: testAt.Add(-time.Minute), ValidUntil: testAt.Add(time.Hour), CreatedAt: testAt.Add(-time.Minute), UpdatedAt: testAt.Add(-time.Minute)}
}
func privateFixture(t *testing.T) agentprofile.PrivateRecord {
	t.Helper()
	m, e := agentprofile.New(testAgent, actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, testAt.Add(-time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	f := agentprofile.PrivateFields{PersonalPreferences: []string{"PRIVATE_PERSONAL_CANARY"}, SocialPreferences: []string{"PRIVATE_SOCIAL_CANARY"}, Availability: "PRIVATE_AVAILABILITY_CANARY", TravelPreferences: []string{"PRIVATE_TRAVEL_CANARY"}, InteractionPreferences: []string{"PRIVATE_INTERACTION_CANARY"}, PrivateCityHistory: "PRIVATE_CITY_CANARY", LanguagePreferences: []string{"PRIVATE_LANGUAGE_CANARY"}, AgentNotes: "PRIVATE_NOTES_CANARY"}
	r, e := agentprofile.NewPrivateRecord(m, f, true)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func previewFixture(t *testing.T) Preview {
	t.Helper()
	s, e := EligibleSource(sourceFixture("hiking"), testOwner, testAgent, testAt)
	if e != nil {
		t.Fatal(e)
	}
	return Preview{SchemaVersion: Schema, ID: testPreview, Purpose: Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, AgentID: testAgent, Source: s, ExpectedProfileVersion: 1, TargetField: Field, Before: []string{}, After: []string{s.Value}, PlanDigest: strings.Repeat("a", 64), ObservedAt: testAt, ExpiresAt: testAt.Add(PreviewTTL), Explanation: "来源有效期与独立保存后果明确展示"}
}
func TestProfileMemoryCompletionClosedSourceMatrix(t *testing.T) {
	for _, c := range []string{"badminton", "basketball", "football", "sports", "culture", "hiking"} {
		t.Run(c, func(t *testing.T) {
			s, e := EligibleSource(sourceFixture(c), testOwner, testAgent, testAt)
			if e != nil || s.Category != c || s.Value != agentmemorycandidate.Statement(c) {
				t.Fatal("canonical source", e)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*agentmemory.Record)
	}{
		{"source_inferred", func(r *agentmemory.Record) { r.SourceType = agentmemory.SourceInferred }},
		{"agent_only", func(r *agentmemory.Record) { r.Visibility = agentmemory.VisibilityAgentOnly }},
		{"pending_review", func(r *agentmemory.Record) { r.Status = agentmemory.StatusPendingReview }},
		{"native_expired", func(r *agentmemory.Record) { r.Status = agentmemory.StatusExpired }},
		{"deleted", func(r *agentmemory.Record) { r.Status = agentmemory.StatusDeleted }},
		{"identity_type", func(r *agentmemory.Record) { r.MemoryType = agentmemory.TypeIdentity }},
		{"wrong_owner", func(r *agentmemory.Record) { r.OwnerID = testMemory }},
		{"wrong_agent", func(r *agentmemory.Record) { r.AgentID = testMemory }},
		{"wrong_schema", func(r *agentmemory.Record) { r.SchemaVersion = "unknown" }},
		{"zero_version", func(r *agentmemory.Record) { r.Version = 0 }},
		{"confidence_not_declaration", func(r *agentmemory.Record) { r.Confidence = .9 }},
		{"future_from", func(r *agentmemory.Record) { r.ValidFrom = testAt.Add(time.Second) }},
		{"future_updated", func(r *agentmemory.Record) { r.UpdatedAt = testAt.Add(time.Second) }},
		{"expiry_exact_now", func(r *agentmemory.Record) { r.ValidUntil = testAt }},
		{"key_category_mismatch", func(r *agentmemory.Record) { r.MemoryKey = "activity_category:football" }},
		{"arbitrary_summary", func(r *agentmemory.Record) { r.Summary = "PRIVATE_ARBITRARY_BODY" }},
		{"extra_sensitive_field", func(r *agentmemory.Record) {
			r.StructuredValue = json.RawMessage(`{"activityCategory":"hiking","nature":"human-declaration","health":"PRIVATE"}`)
		}},
		{"unknown_category", func(r *agentmemory.Record) {
			r.StructuredValue = json.RawMessage(`{"activityCategory":"health","nature":"human-declaration"}`)
		}},
		{"inferred_nature", func(r *agentmemory.Record) {
			r.StructuredValue = json.RawMessage(`{"activityCategory":"hiking","nature":"inferred"}`)
		}},
		{"duplicate_key", func(r *agentmemory.Record) {
			r.StructuredValue = json.RawMessage(`{"activityCategory":"hiking","activityCategory":"hiking","nature":"human-declaration"}`)
		}},
		{"nested_body", func(r *agentmemory.Record) {
			r.StructuredValue = json.RawMessage(`{"activityCategory":"hiking","nature":{"body":"PRIVATE"}}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := sourceFixture("hiking")
			tc.mutate(&r)
			s, e := EligibleSource(r, testOwner, testAgent, testAt)
			if e == nil || s != (Suggestion{}) {
				t.Fatal("nonclosed source returned payload")
			}
		})
	}
}
func TestProfileMemoryCompletionReplacementPreservesEightFields(t *testing.T) {
	r := privateFixture(t)
	s, _ := EligibleSource(sourceFixture("hiking"), testOwner, testAgent, testAt)
	next, e := Replacement(r, s)
	if e != nil {
		t.Fatal(e)
	}
	want := r.Fields
	want.PreferredActivityTypes = []string{agentmemorycandidate.Statement("hiking")}
	if next.ExpectedVersion != r.Profile.ProfileVersion || !reflect.DeepEqual(next.Fields, want) || len(r.Fields.PreferredActivityTypes) != 0 {
		t.Fatal("changed an unselected field/source")
	}
	r.Fields.PreferredActivityTypes = []string{"本人已有文本"}
	if _, e = Replacement(r, s); e == nil {
		t.Fatal("nonempty target overwritten")
	}
	r = privateFixture(t)
	r.Profile.ProfileVersion = math.MaxInt64
	if _, e = Replacement(r, s); e == nil {
		t.Fatal("overflow version")
	}
	s.Value = "猜测徒步"
	if _, e = Replacement(privateFixture(t), s); e == nil {
		t.Fatal("noncanonical value")
	}
}
func TestProfileMemoryCompletionPreviewReceiptShapeMatrix(t *testing.T) {
	p := previewFixture(t)
	if ValidatePreview(p) != nil {
		t.Fatal("valid preview")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Preview)
	}{
		{"max_version", func(p *Preview) { p.ExpectedProfileVersion = math.MaxInt64 }},
		{"nonfinite_source_deadline", func(p *Preview) { p.Source.MemoryValidUntil = time.Time{} }},
		{"unbounded_source_deadline", func(p *Preview) { p.Source.MemoryValidUntil = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"long_preview", func(p *Preview) { p.ExpiresAt = p.ObservedAt.Add(PreviewTTL + time.Second) }},
		{"source_shorter", func(p *Preview) { p.Source.MemoryValidUntil = p.ExpiresAt.Add(-time.Second) }},
		{"model_true", func(p *Preview) { p.ModelAccess = true }},
		{"nonempty_before", func(p *Preview) { p.Before = []string{"old"} }},
		{"two_after", func(p *Preview) { p.After = append(p.After, "new") }},
		{"wrong_after", func(p *Preview) { p.After = []string{"body"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := previewFixture(t)
			tc.mutate(&p)
			if ValidatePreview(p) == nil {
				t.Fatal("invalid preview shape")
			}
		})
	}
	version := int64(2)
	at := testAt.Add(time.Second)
	r := Receipt{SchemaVersion: Schema, ID: testPreview, Purpose: Purpose, Owner: p.Owner, AgentID: testAgent, MemoryID: testMemory, MemoryVersion: 1, ExpectedProfileVersion: 1, PlanDigest: p.PlanDigest, State: "COMMITTED", ResultProfileVersion: &version, CommittedAt: &at, ObservedAt: at, ExpiresAt: p.ExpiresAt, CurrentProfileMatches: true}
	if ValidateReceipt(r) != nil {
		t.Fatal("valid receipt")
	}
	r.ExpectedProfileVersion = math.MaxInt64
	bad := int64(math.MinInt64)
	r.ResultProfileVersion = &bad
	if ValidateReceipt(r) == nil {
		t.Fatal("overflow receipt falsely validated")
	}
}
func TestProfileMemoryCompletionSuggestionsShapeMatrix(t *testing.T) {
	source, _ := EligibleSource(sourceFixture("hiking"), testOwner, testAgent, testAt)
	original := Suggestions{SchemaVersion: Schema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, AgentID: testAgent, ProfileVersion: 1, TargetField: Field, State: "FIELD_EMPTY", Sources: []Suggestion{source}, ObservedAt: testAt}
	if ValidateSuggestions(original) != nil {
		t.Fatal("valid suggestions")
	}
	for _, mode := range []string{"duplicate", "nil", "already_set_with_source", "expired", "unknown_state", "max_version", "model", "bad_category"} {
		t.Run(mode, func(t *testing.T) {
			s := original
			s.Sources = append([]Suggestion{}, original.Sources...)
			switch mode {
			case "duplicate":
				s.Sources = append(s.Sources, source)
			case "nil":
				s.Sources = nil
			case "already_set_with_source":
				s.State = "FIELD_ALREADY_SET"
			case "expired":
				s.Sources[0].MemoryValidUntil = testAt
			case "unknown_state":
				s.State = "ACTIVE"
			case "max_version":
				s.ProfileVersion = math.MaxInt64
			case "model":
				s.ModelAccess = true
			case "bad_category":
				s.Sources[0].Category = "health"
			}
			if ValidateSuggestions(s) == nil {
				t.Fatal("invalid suggestions")
			}
		})
	}
}
