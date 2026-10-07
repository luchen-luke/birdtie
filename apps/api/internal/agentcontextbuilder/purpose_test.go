package agentcontextbuilder

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
)

func purposePureSelection() PurposeSelection {
	return PurposeSelection{AgentID: "11111111-1111-1111-1111-111111111111", TaskID: "22222222-2222-2222-2222-222222222222", CityID: "aberdeen-gb", CurrentQuery: "帮我找公开羽毛球活动", TaskUpdatedAt: time.Date(2026, 10, 3, 1, 2, 3, 0, time.UTC), DeadlineAt: time.Date(2026, 10, 3, 1, 12, 3, 0, time.UTC), ProfileFields: []string{"availability"}}
}
func TestContextPurposeSelectionCanonicalAndNoQueryCopy(t *testing.T) {
	s := purposePureSelection()
	s.ProfileFields = []string{"agentNotes", "availability"}
	normalized, e := NormalizePurposeSelection(s)
	if e != nil || normalized.QueryDigest != PurposeQueryDigest(s.CurrentQuery) {
		t.Fatal(e)
	}
	raw, e := PurposeSelectionBytes(s)
	if e != nil || strings.Contains(string(raw), s.CurrentQuery) || strings.Contains(string(raw), "currentQuery") {
		t.Fatal("original Task query copied into permission", e)
	}
	var persisted PurposeSelection
	if json.Unmarshal(raw, &persisted) != nil || !SamePurposeSelection(s, persisted) {
		t.Fatal("stored reference cannot roundtrip")
	}
	s.ProfileFields = []string{"availability", "agentNotes"}
	if !SamePurposeSelection(s, persisted) {
		t.Fatal("selector order altered authority")
	}
	s.ProfileFields = []string{"personalPreferences"}
	if SamePurposeSelection(s, persisted) {
		t.Fatal("changed selector reused authority")
	}
}
func TestContextPurposeSelectionRejectsInvalid(t *testing.T) {
	cases := map[string]func(*PurposeSelection){
		"agent": func(s *PurposeSelection) { s.AgentID = "" }, "task": func(s *PurposeSelection) { s.TaskID = "" }, "city": func(s *PurposeSelection) { s.CityID = "" }, "query": func(s *PurposeSelection) { s.CurrentQuery = "\nquery" }, "digest": func(s *PurposeSelection) { s.QueryDigest = strings.Repeat("0", 64) }, "missing_query_and_digest": func(s *PurposeSelection) { s.CurrentQuery = "" }, "empty": func(s *PurposeSelection) { s.ProfileFields = nil }, "unknown_field": func(s *PurposeSelection) { s.ProfileFields = []string{"allPrivateData"} }, "duplicate_field": func(s *PurposeSelection) { s.ProfileFields = []string{"availability", "availability"} }, "four_fields": func(s *PurposeSelection) {
			s.ProfileFields = []string{"availability", "agentNotes", "privateCityHistory", "personalPreferences"}
		}, "bad_memory": func(s *PurposeSelection) { s.MemoryIDs = []string{"memory"} }, "duplicate_memory": func(s *PurposeSelection) { s.MemoryIDs = []string{s.AgentID, s.AgentID} }, "many_places": func(s *PurposeSelection) {
			s.PlaceIDs = []string{s.AgentID, s.AgentID, s.AgentID, s.AgentID, s.AgentID, s.AgentID}
		}, "many_activities": func(s *PurposeSelection) {
			s.ActivityIDs = []string{s.AgentID, s.AgentID, s.AgentID, s.AgentID, s.AgentID, s.AgentID}
		}, "many_ties": func(s *PurposeSelection) { s.RelationshipTieIDs = []string{s.AgentID, s.AgentID, s.AgentID, s.AgentID} }, "policy": func(s *PurposeSelection) { s.PolicyFamilies = []agentpolicysettings.Family{"MODEL_EGRESS"} }, "two_policies": func(s *PurposeSelection) {
			s.PolicyFamilies = []agentpolicysettings.Family{agentpolicysettings.Social, agentpolicysettings.Autonomy}
		}, "nano_deadline": func(s *PurposeSelection) { s.DeadlineAt = s.DeadlineAt.Add(time.Nanosecond) }, "future_zero_time": func(s *PurposeSelection) { s.TaskUpdatedAt = time.Time{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := purposePureSelection()
			mutate(&s)
			if _, e := NormalizePurposeSelection(s); e == nil {
				t.Fatal("invalid permission selector accepted")
			}
		})
	}
}
func TestContextPurposeControlObjectsRejectWire(t *testing.T) {
	for _, v := range []any{PurposeCapture{}, PurposeResolution{}} {
		if _, e := json.Marshal(v); !errors.Is(e, ErrServerOnly) {
			t.Fatal("control object serializable", e)
		}
	}
	var c PurposeCapture
	var r PurposeResolution
	if json.Unmarshal([]byte(`{"Authority":"verified"}`), &c) != ErrServerOnly || json.Unmarshal([]byte(`{"GrantID":"approved"}`), &r) != ErrServerOnly {
		t.Fatal("wire manufactured authority")
	}
}
