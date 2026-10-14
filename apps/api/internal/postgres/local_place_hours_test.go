package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// All rows below are synthetic pure-function inputs, not reviewed candidates.
// No database or provider is opened by these tests.
func nowLocalHoursTestFacts() (foundation.Place, []cityseed.PlaceFieldEvidence, time.Time) {
	p, fields := nowLocalPlaceTestFacts()
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	reviewer := "synthetic-independent-reviewer"
	reviewed := now.Add(-7 * time.Minute)
	hours := cityseed.PlaceFieldEvidence{
		ID: "synthetic-hours-evidence", PlaceID: p.ID, SourceCandidateID: "synthetic-candidate",
		FieldKey: agentworkspace.PlaceOpeningHours, Value: json.RawMessage(`"Mon-Fri 10:00-17:00; Sat 12:00-16:00; Sun closed"`),
		ObservedAt: now.Add(-10 * time.Minute), VerifiedAt: now.Add(-5 * time.Minute),
		ValidFrom: now.Add(-15 * time.Minute), ValidTo: now.Add(time.Hour), RefreshDue: now.Add(30 * time.Minute),
		PermissionID: "synthetic-permission", PermissionVersion: 4, Version: 2, ConflictState: "NONE", RowToken: "synthetic-field-xmin:permission-xmin",
		Permission: cityseed.SourcePermission{
			ID: "synthetic-permission", CandidateID: "synthetic-candidate", Version: 4,
			SourcePermissionInput: cityseed.SourcePermissionInput{
				State: cityseed.PermissionConfirmed, Acquisition: "DIRECT_AUTHORIZATION", OriginProvider: "DIRECT_SOURCE",
				LicenseURL: "https://example.invalid/test-only-grant", LicenseVersion: "TEST_ONLY",
				Attribution: "TEST_ONLY hours source", AcquiredAt: now.Add(-24 * time.Hour), RetentionUntil: now.Add(2 * time.Hour),
			},
			SourceURL: "https://example.invalid/synthetic-hours", DeclaredBy: "synthetic-declarer", ReviewedBy: &reviewer, ReviewedAt: &reviewed,
		},
	}
	return p, append(fields, hours), now
}

func TestNowLocalPlaceHoursLiteralTextKeepsEvidenceDatesAndNoOpenNowClaim(t *testing.T) {
	p, fields, now := nowLocalHoursTestFacts()
	if _, ok := localFieldsForPlace(p, fields); !ok {
		t.Fatal("synthetic canonical facts do not match")
	}
	text, err := localPlaceHoursText(p, fields, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		p.Name + "的营业时间记录", "Mon-Fri 10:00-17:00; Sat 12:00-16:00; Sun closed",
		"2026-10-09T07:55:00Z", "2026-10-09T08:30:00Z", "不代表当前开门状态",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("literal/source freshness missing %q in %q", required, text)
		}
	}
	for _, invented := range []string{"官方", "现在开门", "现在营业", "今天17:00关门", "联网搜索"} {
		if strings.Contains(text, invented) {
			t.Fatalf("hours record turned into an unsupported claim: %s", text)
		}
	}
}

func TestNowLocalPlaceHoursMissingStaleConflictingOrUnpermittedIsSpecificUnavailable(t *testing.T) {
	mutations := []struct {
		name   string
		change func(*cityseed.PlaceFieldEvidence, time.Time)
	}{
		{"null", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Value = json.RawMessage(`null`) }},
		{"number", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Value = json.RawMessage(`10`) }},
		{"object", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Value = json.RawMessage(`{"open":"now"}`) }},
		{"empty", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Value = json.RawMessage(`""`) }},
		{"whitespace", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Value = json.RawMessage(`"  "`) }},
		{"newline", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Value = json.RawMessage(`"Mon\nTue"`) }},
		{"foreign place", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.PlaceID = "other-synthetic-place" }},
		{"expired field", func(f *cityseed.PlaceFieldEvidence, now time.Time) { f.ValidTo = now }},
		{"refresh due", func(f *cityseed.PlaceFieldEvidence, now time.Time) { f.RefreshDue = now }},
		{"future field", func(f *cityseed.PlaceFieldEvidence, now time.Time) { f.ValidFrom = now.Add(time.Second) }},
		{"missing verification", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.VerifiedAt = time.Time{} }},
		{"missing observation", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.ObservedAt = time.Time{} }},
		{"future observation", func(f *cityseed.PlaceFieldEvidence, now time.Time) { f.ObservedAt = now.Add(time.Second) }},
		{"future verification", func(f *cityseed.PlaceFieldEvidence, now time.Time) { f.VerifiedAt = now.Add(time.Second) }},
		{"verification before observation", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.VerifiedAt = f.ObservedAt.Add(-time.Second) }},
		{"missing evidence version", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Version = 0 }},
		{"missing evidence ID", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.ID = "" }},
		{"missing row token", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.RowToken = "" }},
		{"pending permission", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Permission.State = cityseed.PermissionPending }},
		{"prohibited permission", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Permission.State = cityseed.PermissionProhibited }},
		{"expired retention", func(f *cityseed.PlaceFieldEvidence, now time.Time) { f.Permission.RetentionUntil = now }},
		{"permission version changed", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Permission.Version++ }},
		{"wrong permission ID", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Permission.ID = "other-permission" }},
		{"wrong candidate", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Permission.CandidateID = "other-candidate" }},
		{"review missing", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.Permission.ReviewedBy = nil }},
		{"review not independent", func(f *cityseed.PlaceFieldEvidence, _ time.Time) {
			reviewer := f.Permission.DeclaredBy
			f.Permission.ReviewedBy = &reviewer
		}},
		{"revoked permission", func(f *cityseed.PlaceFieldEvidence, now time.Time) { f.Permission.RevokedAt = &now }},
		{"conflict unresolved", func(f *cityseed.PlaceFieldEvidence, _ time.Time) { f.ConflictState = "UNRESOLVED" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			p, fields, now := nowLocalHoursTestFacts()
			mutation.change(&fields[len(fields)-1], now)
			if text, err := localPlaceHoursText(p, fields, now); text != "" || !errors.Is(err, agentworkspace.ErrLocalPlaceHoursUnavailable) {
				t.Fatalf("invalid hours acquired a fact: %q %v", text, err)
			}
		})
	}
	p, fields, now := nowLocalHoursTestFacts()
	if text, err := localPlaceHoursText(p, fields[:len(fields)-1], now); text != "" || !errors.Is(err, agentworkspace.ErrLocalPlaceHoursUnavailable) {
		t.Fatal("missing hours was guessed or silently declined")
	}
}

func TestNowLocalPlaceHoursDisagreementDoesNotChooseLatestAndAgreementUsesEarliestDeadline(t *testing.T) {
	p, fields, now := nowLocalHoursTestFacts()
	first := fields[len(fields)-1]
	second := first
	second.ID, second.RowToken = "other-hours-evidence", "other-xmin"
	second.VerifiedAt = now.Add(-time.Minute)
	second.PermissionID, second.Permission.ID = "other-permission", "other-permission"
	second.Permission.RetentionUntil = now.Add(10 * time.Minute)
	second.Permission.SourceURL = "https://example.invalid/other-synthetic-hours"
	second.Value = json.RawMessage(`"Conflicting hours 09:00-15:00"`)
	for _, pair := range [][]cityseed.PlaceFieldEvidence{{first, second}, {second, first}} {
		full := append(append([]cityseed.PlaceFieldEvidence{}, fields[:len(fields)-1]...), pair...)
		if text, err := localPlaceHoursText(p, full, now); text != "" || !errors.Is(err, agentworkspace.ErrLocalPlaceHoursUnavailable) {
			t.Fatal("source order/latest timestamp won a disagreement")
		}
		filtered := removeConflictingPlaceFields(full)
		if _, ok := localFieldsForPlace(p, filtered); !ok {
			t.Fatal("hour conflict discarded native identity")
		}
		if _, err := localPlaceHoursText(p, filtered, now); !errors.Is(err, agentworkspace.ErrLocalPlaceHoursUnavailable) {
			t.Fatal("filtered conflict became sufficient hours")
		}
	}
	second.Value = append(json.RawMessage{}, first.Value...)
	text, err := localPlaceHoursText(p, append(fields, second), now)
	if err != nil || !strings.Contains(text, "2026-10-09T07:59:00Z") || !strings.Contains(text, "2026-10-09T08:10:00Z") {
		t.Fatalf("agreement lost latest verification/earliest retained deadline: %q %v", text, err)
	}
}

func TestNowLocalPlaceHoursCitationsFollowHoursFieldSourcesWithoutAddressAttribution(t *testing.T) {
	field := func(key, url string) cityseed.PlaceFieldEvidence {
		return cityseed.PlaceFieldEvidence{FieldKey: key, Permission: cityseed.SourcePermission{SourceURL: url}}
	}
	fields := []cityseed.PlaceFieldEvidence{field("name", "https://example.invalid/name"), field("address", "https://example.invalid/address"), field("coordinates", "https://example.invalid/coordinates"), field("opening_hours", "https://example.invalid/hours"), field("opening_hours", "https://example.invalid/hours"), field("opening_hours", "https://example.invalid/other-hours")}
	ids := map[string]string{"https://example.invalid/name": "source-2", "https://example.invalid/address": "source-3", "https://example.invalid/coordinates": "source-4", "https://example.invalid/hours": "source-7", "https://example.invalid/other-hours": "source-9"}
	if got := localPlaceFieldCitations(fields, ids, agentworkspace.PlaceOpeningHours, false); got != " [source-2] [source-7] [source-9]" {
		t.Fatalf("hours inherited address/fixed citation or lost another source: %q", got)
	}
	if got := localPlaceCitations(fields, ids, false); got != " [source-2] [source-3]" {
		t.Fatal("original address citations changed", got)
	}
}

func TestNowLocalPlaceHoursEvidenceDigestIncludesHoursVersionGrantAndNativeRowToken(t *testing.T) {
	_, fields, _ := nowLocalHoursTestFacts()
	before := localFieldDigest(fields, nil)
	for _, change := range []func(*cityseed.PlaceFieldEvidence){
		func(f *cityseed.PlaceFieldEvidence) { f.Version++ },
		func(f *cityseed.PlaceFieldEvidence) { f.Permission.Version++ },
		func(f *cityseed.PlaceFieldEvidence) { f.RowToken = "changed-xmin" },
		func(f *cityseed.PlaceFieldEvidence) { f.Value = json.RawMessage(`"new hours"`) },
		func(f *cityseed.PlaceFieldEvidence) { f.RefreshDue = f.RefreshDue.Add(-time.Second) },
	} {
		copyFields := append([]cityseed.PlaceFieldEvidence{}, fields...)
		change(&copyFields[len(copyFields)-1])
		if localFieldDigest(copyFields, nil) == before {
			t.Fatal("hours field change did not retire its source evidence digest")
		}
	}
}

func TestNowLocalPlaceHoursQuestionDoesNotInferOpenNowFromOldTopic(t *testing.T) {
	task := agentworkspace.Task{Filters: map[string]string{"currentQuery": "Aberdeen Maritime Museum的营业时间", "searchTerm": "Aberdeen Art Gallery", agentworkspace.PlaceFollowupTopicFilter: agentworkspace.PlaceOpeningHours}}
	if q, ok := currentLocalPlaceQuestion(task); !ok || q.Name != "Aberdeen Maritime Museum" || q.Field != agentworkspace.PlaceOpeningHours {
		t.Fatal("new literal fact inherited old place", q)
	}
	task.Filters["currentQuery"] = "现在开门吗"
	if q, ok := currentLocalPlaceQuestion(task); ok {
		t.Fatal("unknown opening status became local hours claim", q)
	}
}

func nowLocalHoursFollowupTestTask() agentworkspace.Task {
	query := "what are its opening hours"
	return agentworkspace.Task{Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskActive, ContextType: "CITY", CityID: "synthetic-city", Filters: map[string]string{
		"currentQuery": query, "searchTerm": "Synthetic Glenhouse", "targetIntent": agentworkspace.FindPlace,
		agentworkspace.PlaceFollowupIDFilter:         "33333333-3333-4333-8333-333333333333",
		agentworkspace.PlaceFollowupGenerationFilter: strings.Repeat("a", 64), agentworkspace.PlaceFollowupTopicFilter: agentworkspace.PlaceOpeningHours,
	}, Conversation: []agentworkspace.Message{{Role: "user", Text: "Synthetic Glenhouse的地址是什么"}, {Role: "assistant", Text: "TEST_ONLY retained address"}, {Role: "user", Text: query}}}
}

func TestNowLocalPlaceHoursPronounUsesOnlyTheCurrentFullyBoundNativeTask(t *testing.T) {
	task := nowLocalHoursFollowupTestTask()
	if q, ok := currentLocalPlaceQuestion(task); !ok || q.Name != "Synthetic Glenhouse" || q.Field != agentworkspace.PlaceOpeningHours || q.Recommendation {
		t.Fatal("real failure shape still declined to a supplier path", q, ok)
	}
	for _, change := range []struct {
		name  string
		apply func(*agentworkspace.Task)
	}{
		{"missing place", func(t *agentworkspace.Task) { delete(t.Filters, agentworkspace.PlaceFollowupIDFilter) }},
		{"malformed place", func(t *agentworkspace.Task) { t.Filters[agentworkspace.PlaceFollowupIDFilter] = "PRIVATE_INVALID_ID" }},
		{"missing generation", func(t *agentworkspace.Task) { delete(t.Filters, agentworkspace.PlaceFollowupGenerationFilter) }},
		{"zero generation", func(t *agentworkspace.Task) {
			t.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("0", 64)
		}},
		{"uppercase generation", func(t *agentworkspace.Task) {
			t.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("A", 64)
		}},
		{"missing topic", func(t *agentworkspace.Task) { delete(t.Filters, agentworkspace.PlaceFollowupTopicFilter) }},
		{"old address topic", func(t *agentworkspace.Task) {
			t.Filters[agentworkspace.PlaceFollowupTopicFilter] = agentworkspace.PlaceAddress
		}},
		{"foreign intent", func(t *agentworkspace.Task) { t.Intent = agentworkspace.FindActivity }},
		{"foreign target", func(t *agentworkspace.Task) { t.Filters["targetIntent"] = agentworkspace.FindOrganization }},
		{"private context", func(t *agentworkspace.Task) { t.ContextType = "PERSON" }},
		{"missing city", func(t *agentworkspace.Task) { t.CityID = "" }},
		{"completed old turn", func(t *agentworkspace.Task) { t.Status = agentworkspace.TaskCompleted }},
		{"missing name", func(t *agentworkspace.Task) { t.Filters["searchTerm"] = "" }},
		{"malformed name", func(t *agentworkspace.Task) { t.Filters["searchTerm"] = "Synthetic\nprivate name" }},
		{"no turn", func(t *agentworkspace.Task) { t.Conversation = nil }},
		{"last assistant", func(t *agentworkspace.Task) { t.Conversation[len(t.Conversation)-1].Role = "assistant" }},
		{"query turn differs", func(t *agentworkspace.Task) { t.Conversation[len(t.Conversation)-1].Text = "private unrelated turn" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			task := nowLocalHoursFollowupTestTask()
			change.apply(&task)
			if q, ok := currentLocalPlaceQuestion(task); ok {
				t.Fatal("unbound/retired/private context became a local hours answer", q)
			}
		})
	}
}

func TestNowLocalPlaceHoursPronounRetainedTopicDoesNotAcceptCurrentOpeningOrAnotherName(t *testing.T) {
	for _, query := range []string{"is it open now?", "它现在开门吗？", "它今天几点开门？", "它周末几点开门？", "what are its opening hours and admission fees?", "Museum opening hours"} {
		task := nowLocalHoursFollowupTestTask()
		task.Filters["currentQuery"], task.Conversation[len(task.Conversation)-1].Text = query, query
		if q, ok := currentLocalPlaceQuestion(task); ok {
			t.Fatal("old hours topic invented unsupported current fact", query, q)
		}
	}
	task := nowLocalHoursFollowupTestTask()
	query := "What are the opening hours of Maritime Museum?"
	task.Filters["currentQuery"], task.Conversation[len(task.Conversation)-1].Text = query, query
	if q, ok := currentLocalPlaceQuestion(task); !ok || q.Name != "Maritime Museum" || q.Name == task.Filters["searchTerm"] {
		t.Fatal("explicit new place accidentally inherited Glenhouse", q)
	}
}

func TestNowLocalPlaceHoursPronounSourceMustRemainTheExactSingleCanonicalEntity(t *testing.T) {
	place := nativeResolvedPublicPlace{id: "33333333-3333-4333-8333-333333333333", name: "Synthetic Glenhouse", valid: true}
	fresh := func() arp.Receipt {
		return arp.Receipt{Places: []foundation.Place{{ID: place.id, Name: place.name}}, Items: []arp.Item{{Entity: arp.Ref{Type: "place", ID: place.id}, Title: place.name}}}
	}
	if !localPlaceHoursSourceMatches(place, fresh()) {
		t.Fatal("canonical selected receipt rejected")
	}
	for _, change := range []func(*arp.Receipt){
		func(r *arp.Receipt) { r.Places = nil },
		func(r *arp.Receipt) { r.Items = nil },
		func(r *arp.Receipt) { r.Places = append(r.Places, r.Places[0]) },
		func(r *arp.Receipt) { r.Items = append(r.Items, r.Items[0]) },
		func(r *arp.Receipt) { r.Places[0].ID = "another-place" },
		func(r *arp.Receipt) { r.Places[0].Name = "Museum" },
		func(r *arp.Receipt) { r.Items[0].Entity.Type = "activity" },
		func(r *arp.Receipt) { r.Items[0].Entity.ID = "another-place" },
		func(r *arp.Receipt) { r.Items[0].Title = "Museum" },
	} {
		source := fresh()
		change(&source)
		if localPlaceHoursSourceMatches(place, source) {
			t.Fatal("source change/ambiguity substituted a different place for the pronoun")
		}
	}
	place.valid = false
	if localPlaceHoursSourceMatches(place, fresh()) {
		t.Fatal("unvalidated scalar name and ID became a native grant")
	}
}
