package agentconfidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const confidenceTestID = "81000000-0000-4000-8000-000000000001"
const confidenceTestAgent = "81000000-0000-4000-8000-000000000002"
const confidenceTestOwner = "81000000-0000-4000-8000-000000000003"

func scorePtr(v float64) *float64 { return &v }
func confidenceView() OwnMemoryView {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	a := NewDirectDeclaration()
	description, _ := Description(a)
	return OwnMemoryView{SchemaVersion: SchemaV1, MemoryID: confidenceTestID, MemoryVersion: 1, AgentID: confidenceTestAgent, OwnerType: actorref.Person, OwnerID: confidenceTestOwner, SourceType: agentmemory.SourceExplicit, Assessment: a, Explanation: description, ValidFrom: now, ValidUntil: now.Add(time.Hour), ReadAt: now}
}
func TestConfidenceNumericRangeIsFiniteAndNonBoolean(t *testing.T) {
	for _, value := range []float64{0, math.SmallestNonzeroFloat64, 0.25, 0.86, 1} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			a, err := NewUncalibratedScore(value)
			if err != nil || a.Value == nil || *a.Value != value || a.Semantics != UncalibratedScore || a.Level != "" {
				t.Fatal("valid finite score rejected")
			}
			description, err := Description(a)
			if err != nil || description != "未校准评分，不代表正确概率。" {
				t.Fatal("uncalibrated score claimed correctness probability")
			}
		})
	}
	for _, value := range []float64{-math.SmallestNonzeroFloat64, -0.01, math.Nextafter(1, 2), 2, math.NaN(), math.Inf(1), math.Inf(-1)} {
		t.Run("reject_"+fmt.Sprint(value), func(t *testing.T) {
			a, err := NewUncalibratedScore(value)
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(a, Assessment{}) {
				t.Fatal("invalid score retained")
			}
		})
	}
	var a Assessment
	for _, body := range []string{`{"semantics":"UNCALIBRATED_SCORE","value":true}`, `{"semantics":"UNCALIBRATED_SCORE","value":false}`, `{"semantics":"UNCALIBRATED_SCORE","value":"0.86"}`} {
		if json.Unmarshal([]byte(body), &a) == nil {
			t.Fatal("boolean/string substituted for numeric confidence")
		}
	}
	source := 0.86
	normalized, err := NormalizeAssessment(Assessment{Semantics: UncalibratedScore, Value: &source})
	source = 1
	if err != nil || *normalized.Value != 0.86 {
		t.Fatal("normalized score aliases caller variable")
	}
	negativeZero := math.Copysign(0, -1)
	normalized, err = NewUncalibratedScore(negativeZero)
	if err != nil || math.Signbit(*normalized.Value) {
		t.Fatal("negative zero was not canonicalized")
	}
}
func TestConfidenceSemanticTypesNeverAuthorizeProbability(t *testing.T) {
	for _, item := range []struct {
		name string
		a    Assessment
		want error
	}{
		{"direct", NewDirectDeclaration(), nil}, {"direct_wrong", Assessment{Semantics: DirectDeclaration, Value: scorePtr(.86)}, ErrInvalid},
		{"direct_missing", Assessment{Semantics: DirectDeclaration}, ErrInvalid}, {"direct_level", Assessment{Semantics: DirectDeclaration, Value: scorePtr(1), Level: High}, ErrInvalid},
		{"score_missing", Assessment{Semantics: UncalibratedScore}, ErrInvalid}, {"score_level", Assessment{Semantics: UncalibratedScore, Value: scorePtr(.5), Level: High}, ErrInvalid},
		{"ordinal_low", Assessment{Semantics: Ordinal, Level: Low}, nil}, {"ordinal_medium", Assessment{Semantics: Ordinal, Level: Medium}, nil}, {"ordinal_high", Assessment{Semantics: Ordinal, Level: High}, nil},
		{"ordinal_missing", Assessment{Semantics: Ordinal}, ErrInvalid}, {"ordinal_unknown", Assessment{Semantics: Ordinal, Level: "VERIFIED"}, ErrInvalid}, {"ordinal_lowercase", Assessment{Semantics: Ordinal, Level: "high"}, ErrInvalid},
		{"ordinal_number", Assessment{Semantics: Ordinal, Level: High, Value: scorePtr(.86)}, ErrInvalid}, {"unknown_semantics", Assessment{Semantics: "APPROVED", Value: scorePtr(1)}, ErrInvalid},
		{"unproven_calibration", Assessment{Semantics: CalibratedProbability, Value: scorePtr(.86)}, ErrUnavailable}, {"calibrated_one", Assessment{Semantics: CalibratedProbability, Value: scorePtr(1)}, ErrUnavailable},
		{"calibration_missing", Assessment{Semantics: CalibratedProbability}, ErrInvalid}, {"calibration_nonfinite", Assessment{Semantics: CalibratedProbability, Value: scorePtr(math.NaN())}, ErrInvalid},
	} {
		t.Run(item.name, func(t *testing.T) {
			a, err := NormalizeAssessment(item.a)
			if !errors.Is(err, item.want) || item.want != nil && !reflect.DeepEqual(a, Assessment{}) {
				t.Fatal("confidence semantics accepted invalid shape or self-proven calibration")
			}
		})
	}
	for _, level := range []OrdinalLevel{Low, Medium, High} {
		a, err := NewOrdinal(level)
		if err != nil || a.Value != nil {
			t.Fatal("ordinal generated numeric probability")
		}
	}
	if a, err := NewCalibratedProbability(.86); !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(a, Assessment{}) {
		t.Fatal("calibrated capability became available")
	}
	if text, err := Description(Assessment{Semantics: CalibratedProbability, Value: scorePtr(.86)}); !errors.Is(err, ErrUnavailable) || text != "" {
		t.Fatal("unproven calibration produced display text")
	}
}
func TestConfidenceOwnerProjectionContainsOnlyMetadata(t *testing.T) {
	v := confidenceView()
	if ValidateOwnMemoryView(v) != nil {
		t.Fatal("valid direct metadata rejected")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"summary"`, `"structuredValue"`, `"body"`, `"evidence"`, `"consent"`, `"confirmed"`, `"approved"`, `"calibrationId"`} {
		if strings.Contains(string(raw), key) {
			t.Fatal("confidence projection exposed content or authorization claims")
		}
	}
	if !strings.Contains(v.Explanation, "不代表正确概率") {
		t.Fatal("direct declaration presented as probability")
	}
	for _, item := range []struct {
		name string
		edit func(*OwnMemoryView)
	}{
		{"schema", func(v *OwnMemoryView) { v.SchemaVersion = "unknown" }}, {"memory", func(v *OwnMemoryView) { v.MemoryID = "bad" }}, {"agent", func(v *OwnMemoryView) { v.AgentID = "bad" }}, {"owner", func(v *OwnMemoryView) { v.OwnerID = "bad" }},
		{"organization", func(v *OwnMemoryView) { v.OwnerType = actorref.Organization }}, {"version", func(v *OwnMemoryView) { v.MemoryVersion = 0 }}, {"inferred", func(v *OwnMemoryView) { v.SourceType = agentmemory.SourceInferred }},
		{"score_not_declaration", func(v *OwnMemoryView) { v.Assessment.Semantics = UncalibratedScore }}, {"private_explanation", func(v *OwnMemoryView) { v.Explanation = "private-canary" }},
		{"read_before_source", func(v *OwnMemoryView) { v.ReadAt = v.ValidFrom.Add(-time.Nanosecond) }}, {"exact_expiry", func(v *OwnMemoryView) { v.ReadAt = v.ValidUntil }}, {"negative_validity", func(v *OwnMemoryView) { v.ValidUntil = v.ValidFrom }},
		{"validity_over_year", func(v *OwnMemoryView) { v.ValidUntil = v.ValidFrom.Add(agentmemory.MaxValidity + time.Nanosecond) }}, {"invalid_time", func(v *OwnMemoryView) { v.ReadAt = time.Time{} }},
	} {
		t.Run(item.name, func(t *testing.T) {
			v := confidenceView()
			item.edit(&v)
			if !errors.Is(ValidateOwnMemoryView(v), ErrInvalid) {
				t.Fatal("invalid current owner metadata accepted")
			}
		})
	}
}
func TestConfidenceDoesNotInstallMachinePortsOrAcceptCallerScores(t *testing.T) {
	ports := agentcognitive.UnavailableCognitivePorts{}
	view, err := ports.ReadMemory(context.Background(), agentcognitive.ReadRequest{})
	if !errors.Is(err, agentcognitive.ErrUnavailable) || !reflect.ValueOf(view).IsZero() {
		t.Fatal("confidence domain enabled cognitive reading")
	}
	if reflect.TypeOf(NewHumanReader(nil, false)).Implements(reflect.TypeOf((*agentcognitive.MemoryReader)(nil)).Elem()) {
		t.Fatal("human reader became a machine memory adapter")
	}
	input := `{"expectedVersion":0,"memoryType":"PREFERENCE","memoryKey":"test","summary":"明确声明","structuredValue":{},"visibility":"PRIVATE","validUntil":"2026-10-04T00:00:00Z","confidence":1}`
	if _, err := agentmemory.DecodePutInput([]byte(input)); !errors.Is(err, agentmemory.ErrInvalid) {
		t.Fatal("confidence field bypassed current native write wire")
	}
	a := agentprofile.PrivateAccess{SessionDigest: [32]byte{1}, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: confidenceTestOwner}}
	for _, item := range []struct {
		name    string
		access  agentprofile.PrivateAccess
		id      string
		version int64
		want    error
	}{
		{"unavailable", a, confidenceTestID, 1, ErrUnavailable}, {"anonymous", agentprofile.PrivateAccess{}, confidenceTestID, 1, ErrForbidden}, {"invalid_id", a, "bad", 1, ErrInvalid}, {"missing_version", a, confidenceTestID, 0, ErrInvalid}, {"negative_version", a, confidenceTestID, -1, ErrInvalid},
	} {
		t.Run(item.name, func(t *testing.T) {
			v, err := NewHumanReader(nil, false).ReadOwnMemoryConfidence(context.Background(), item.access, item.id, item.version)
			if !errors.Is(err, item.want) || !reflect.DeepEqual(v, OwnMemoryView{}) {
				t.Fatal("unavailable/invalid reader returned data")
			}
		})
	}
}

func TestConfidenceReaderRejectsAbsentAndCancelledContext(t *testing.T) {
	access := agentprofile.PrivateAccess{SessionDigest: [32]byte{1}, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: confidenceTestOwner}}
	v, err := NewHumanReader(nil, false).ReadOwnMemoryConfidence(nil, access, confidenceTestID, 1)
	if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(v, OwnMemoryView{}) {
		t.Fatal("nil context was not rejected with an empty view")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v, err = NewHumanReader(nil, false).ReadOwnMemoryConfidence(ctx, access, confidenceTestID, 1)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(v, OwnMemoryView{}) {
		t.Fatal("cancelled context proceeded to data access")
	}
}
