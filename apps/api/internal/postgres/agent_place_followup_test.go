package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
)

func placeFollowupNativeTask(t *testing.T) agentworkspace.Task {
	t.Helper()
	task := agentworkspace.Task{ID: "11111111-1111-4111-8111-111111111111", PrincipalID: "22222222-2222-4222-8222-222222222222", ActingUserID: "22222222-2222-4222-8222-222222222222", PrincipalType: "person", ContextType: "CITY", CityID: "unit-city", Status: agentworkspace.TaskCompleted, Intent: agentworkspace.FindPlace, Filters: map[string]string{"searchTerm": "Gallery"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "UNIT find Gallery"}, {Role: "assistant", Text: "PRIVATE_ASSISTANT_NOT_A_NAME"}}}
	m, e := agentworkspace.NewReplyMembership(task, 1, "place", []arp.Ref{{Type: "place", ID: "33333333-3333-4333-8333-333333333333"}})
	if e != nil {
		t.Fatal(e)
	}
	task.Conversation[1].ResultMembership = m
	return task
}

func TestPlaceFollowupNativeOnlyLatestSingleMembership(t *testing.T) {
	for _, mode := range []string{"valid", "absent", "invalid", "empty", "ambiguous", "other_kind", "later_unbound_reply", "pending_user", "failed", "cross_task", "cross_city"} {
		t.Run(mode, func(t *testing.T) {
			task := placeFollowupNativeTask(t)
			m := task.Conversation[1].ResultMembership
			switch mode {
			case "absent":
				task.Conversation[1].ResultMembership = nil
			case "invalid":
				m.TurnDigest = strings.Repeat("a", 64)
			case "empty":
				m.Refs = []arp.Ref{}
			case "ambiguous":
				m.Refs = append(m.Refs, arp.Ref{Type: "place", ID: "44444444-4444-4444-8444-444444444444"})
			case "other_kind":
				m.Kind, m.Refs[0].Type = "activity", "activity"
			case "later_unbound_reply":
				task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: "new"}, agentworkspace.Message{Role: "assistant", Text: "private unbound"})
			case "pending_user":
				task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: "new"})
			case "failed":
				task.Status = agentworkspace.TaskFailed
			case "cross_task":
				m.TaskID = "55555555-5555-4555-8555-555555555555"
			case "cross_city":
				m.CityID = "other-city"
			}
			selector, e := latestSinglePlaceReply(task)
			if mode == "valid" {
				if e != nil || selector.index != 1 || selector.membership.Refs[0].ID != m.Refs[0].ID {
					t.Fatal("current unique public ref lost", e)
				}
			} else if !errors.Is(e, agentworkspace.ErrPlaceFollowupAmbiguous) || selector != nil {
				t.Fatal("absent/ambiguous/foreign membership fell back to old history", e)
			}
		})
	}
}

func TestPlaceFollowupNativePublicNameStillMatchesCurrentSubject(t *testing.T) {
	for _, v := range []struct {
		name, term string
		matches    bool
	}{
		{"Aberdeen Art Gallery", "Gallery", true},
		{"Aberdeen Art Gallery", "aberdeen art gallery", true},
		{"Aberdeen Maritime Museum", "Gallery", false},
		{"Aberdeen Art Gallery", "", false},
		{"Aberdeen Art Gallery", " Gallery ", false},
		{"Aberdeen Art Gallery", "Gallery\nPRIVATE_CANARY", false},
	} {
		if publicPlaceMatchesCurrentSearch(v.name, v.term) != v.matches {
			t.Fatal("renamed or malformed public subject was silently substituted")
		}
	}
}

func TestPlaceFollowupNativeClosedProofShapeBeforeAnySQL(t *testing.T) {
	now := time.Now()
	for _, mode := range []string{"legacy", "id_only", "generation_only", "topic_only", "foreign_intent", "zero_generation", "upper_generation", "invalid_topic", "wrong_target"} {
		t.Run(mode, func(t *testing.T) {
			task := placeFollowupNativeTask(t)
			task.Filters["targetIntent"] = agentworkspace.FindPlace
			if mode != "legacy" {
				task.Filters[agentworkspace.PlaceFollowupIDFilter] = "33333333-3333-4333-8333-333333333333"
				task.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("a", 64)
				task.Filters[agentworkspace.PlaceFollowupTopicFilter] = agentworkspace.PlaceOpeningHours
			}
			switch mode {
			case "id_only":
				delete(task.Filters, agentworkspace.PlaceFollowupGenerationFilter)
			case "generation_only":
				delete(task.Filters, agentworkspace.PlaceFollowupIDFilter)
			case "topic_only":
				delete(task.Filters, agentworkspace.PlaceFollowupIDFilter)
				delete(task.Filters, agentworkspace.PlaceFollowupGenerationFilter)
			case "foreign_intent":
				task.Intent = agentworkspace.FindActivity
			case "zero_generation":
				task.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("0", 64)
			case "upper_generation":
				task.Filters[agentworkspace.PlaceFollowupGenerationFilter] = strings.Repeat("A", 64)
			case "invalid_topic":
				task.Filters[agentworkspace.PlaceFollowupTopicFilter] = "PRIVATE_PROFILE"
			case "wrong_target":
				task.Filters["targetIntent"] = agentworkspace.FindActivity
			}
			place, term, e := nativeResolvedPlaceFromTaskTx(context.Background(), nil, task, now)
			if mode == "legacy" {
				if e != nil || place.valid || term != "" {
					t.Fatal("original no-place resolved proof changed", e)
				}
			} else if e == nil || place.valid || term != "" {
				t.Fatal("partial/arbitrary native proof reached SQL or model projection", mode)
			}
		})
	}
}

func TestPlaceFollowupPublicProjectionBindsPlaceGenerationWithoutExport(t *testing.T) {
	context := resolvedNativeTestContext()
	context.ResolvedSlots.Operation, context.ResolvedSlots.Target, context.ResolvedSlots.SearchTerm = agentworkspace.FindPlace, agentworkspace.FindPlace, "Aberdeen Art Gallery opening hours"
	place := nativeResolvedPublicPlace{id: "33333333-3333-4333-8333-333333333333", name: "Aberdeen Art Gallery", generation: strings.Repeat("a", 64), raw: `{"id":"33333333-3333-4333-8333-333333333333","row":"private-generation-1"}`, valid: true}
	first, e := resolvedPublicPlaceContextEvidence(context, []byte(`{"cityRow":"1"}`), place)
	place.raw = strings.Replace(place.raw, "generation-1", "generation-2", 1)
	second, e2 := resolvedPublicPlaceContextEvidence(context, []byte(`{"cityRow":"1"}`), place)
	if e != nil || e2 != nil || first == second {
		t.Fatal("place ABA generation failed to retire exact context evidence")
	}
	legacy, e := resolvedPublicContextEvidence(context, []byte(`{"cityRow":"1"}`))
	if e != nil || legacy == first || legacy == second {
		t.Fatal("new selected place proof conflated with original city-only proof")
	}
	payload, e := modelegressbudget.LiveResolvedPublicSearchPayload("它周末几点开门？", context, liveSourceTestSources())
	query, queryErr := modelegressbudget.CompileLiveResolvedPublicSearchQuery("它周末几点开门？", context)
	if e != nil || queryErr != nil {
		t.Fatal("bounded public place envelope", e, queryErr)
	}
	for _, secret := range []string{place.id, "private-generation", "cityRow", first, second, agentworkspace.PlaceFollowupGenerationFilter} {
		if strings.Contains(payload, secret) || strings.Contains(query, secret) {
			t.Fatal("retained private place proof entered public payload/query")
		}
	}
	place.valid = false
	if _, e = resolvedPublicPlaceContextEvidence(context, []byte(`{}`), place); e == nil {
		t.Fatal("unsealed place data acquired context evidence")
	}
}
