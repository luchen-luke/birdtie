package postgres

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// Synthetic inputs exercise the in-process whole-candidate boundary only.
// They are not reviewed sources, and no provider or database is opened.
func localBoundsTestCandidate(number int, urls ...string) localPlaceCandidate {
	id := fmt.Sprintf("synthetic-place-%02d", number)
	c := localPlaceCandidate{place: foundation.Place{ID: id, Name: fmt.Sprintf("Synthetic venue %02d", number)}, fields: []cityseed.PlaceFieldEvidence{}}
	for i, url := range urls {
		c.fields = append(c.fields, cityseed.PlaceFieldEvidence{
			ID: fmt.Sprintf("%s-evidence-%02d", id, i), PlaceID: id, FieldKey: "name", Value: json.RawMessage(`"Synthetic name"`),
			Permission: cityseed.SourcePermission{SourceURL: url, SourcePermissionInput: cityseed.SourcePermissionInput{Attribution: "TEST_ONLY"}},
		})
	}
	return c
}

func localBoundsTestSource(number int) string {
	return fmt.Sprintf("https://example.invalid/test-only-source-%02d", number)
}

func localBoundsCandidateIDs(candidates []localPlaceCandidate) []string {
	ids := []string{}
	for _, candidate := range candidates {
		ids = append(ids, candidate.place.ID)
	}
	return ids
}

func TestNowLocalPlaceBoundsFifteenIndependentSourcesSelectTenCompletePlaces(t *testing.T) {
	input := []localPlaceCandidate{}
	for i := 1; i <= 15; i++ {
		url := localBoundsTestSource(i)
		input = append(input, localBoundsTestCandidate(i, url, url, url))
	}
	before := localBoundsCandidateIDs(input)
	selected, bounded := boundedLocalPlaceCandidates(input)
	if !bounded || len(selected) != 10 || !reflect.DeepEqual(localBoundsCandidateIDs(selected), before[:10]) {
		t.Fatal("independent sources did not yield the deterministic admitted subset", localBoundsCandidateIDs(selected), bounded)
	}
	if !reflect.DeepEqual(before, localBoundsCandidateIDs(input)) {
		t.Fatal("bounding mutated the native source collection")
	}
	fields, uses := localPlaceCandidateEvidence(selected)
	if len(fields) != 30 || len(uses) != 0 {
		t.Fatal("candidate fields were partly discarded or unrelated Memory uses appeared", len(fields), uses)
	}
	sourceIDs := map[string]string{}
	for _, field := range fields {
		if _, seen := sourceIDs[field.Permission.SourceURL]; !seen {
			sourceIDs[field.Permission.SourceURL] = fmt.Sprintf("source-%d", len(sourceIDs)+1)
		}
	}
	if len(sourceIDs) != 10 {
		t.Fatal("answer source safety limit was bypassed", len(sourceIDs))
	}
	for _, c := range selected {
		if len(c.fields) != 3 || localPlaceCitations(c.fields, sourceIDs, true) == "" {
			t.Fatal("admitted card lost its full field/source boundary", c.place.ID)
		}
		for _, field := range c.fields {
			if field.PlaceID != c.place.ID || sourceIDs[field.Permission.SourceURL] == "" {
				t.Fatal("card/map entity survived without its own source", c.place.ID)
			}
		}
	}
}

func TestNowLocalPlaceBoundsPreservesOrderingAndPrioritizesEligiblePlaces(t *testing.T) {
	input := []localPlaceCandidate{}
	for i := 1; i <= 12; i++ {
		c := localBoundsTestCandidate(i, localBoundsTestSource(i))
		c.score = i % 3
		input = append(input, c)
	}
	input[0].excluded, input[0].score = true, 1000
	first, bounded := boundedLocalPlaceCandidates(input)
	reversed := append([]localPlaceCandidate{}, input...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	second, secondBounded := boundedLocalPlaceCandidates(reversed)
	if !bounded || !secondBounded || !reflect.DeepEqual(localBoundsCandidateIDs(first), localBoundsCandidateIDs(second)) {
		t.Fatal("native input order altered the bounded ranking")
	}
	for i, c := range first {
		if c.excluded {
			t.Fatal("hard-excluded high score displaced a usable place")
		}
		if i > 0 && first[i-1].score < c.score {
			t.Fatal("original score order was lost")
		}
	}
	// Name ties retain the canonical ID tie-breaker.
	one, two := localBoundsTestCandidate(1, localBoundsTestSource(1)), localBoundsTestCandidate(2, localBoundsTestSource(2))
	two.place.Name = one.place.Name
	ties, _ := boundedLocalPlaceCandidates([]localPlaceCandidate{two, one})
	if ties[0].place.ID != one.place.ID {
		t.Fatal("equal names discarded the original ID tie-breaker")
	}
}

func TestNowLocalPlaceBoundsKeepsAllSourcesForEachPlaceAndCanReuseSharedSources(t *testing.T) {
	tooMany := []string{}
	for i := 1; i <= 11; i++ {
		tooMany = append(tooMany, localBoundsTestSource(i))
	}
	oversized := localBoundsTestCandidate(1, tooMany...)
	complete := localBoundsTestCandidate(2, tooMany[:10]...)
	shared := localBoundsTestCandidate(3, localBoundsTestSource(2), localBoundsTestSource(8))
	unfittable := localBoundsTestCandidate(4, localBoundsTestSource(8), localBoundsTestSource(12))
	selected, bounded := boundedLocalPlaceCandidates([]localPlaceCandidate{oversized, complete, shared, unfittable})
	if !bounded || !reflect.DeepEqual(localBoundsCandidateIDs(selected), []string{complete.place.ID, shared.place.ID}) {
		t.Fatal("whole-place source boundary or shared-source accounting failed", localBoundsCandidateIDs(selected))
	}
	if len(selected[0].fields) != 10 || len(selected[1].fields) != 2 {
		t.Fatal("one selected place's required independent source was truncated")
	}
}

func TestNowLocalPlaceBoundsRespectsOneHundredFactsWithoutTruncatingAPlace(t *testing.T) {
	urls := make([]string, 100)
	for i := range urls {
		urls[i] = localBoundsTestSource(1)
	}
	exact := localBoundsTestCandidate(1, urls...)
	extra := localBoundsTestCandidate(2, localBoundsTestSource(1))
	selected, bounded := boundedLocalPlaceCandidates([]localPlaceCandidate{extra, exact})
	if !bounded || len(selected) != 1 || selected[0].place.ID != exact.place.ID || len(selected[0].fields) != 100 {
		t.Fatal("fact limit failed to retain a whole candidate at the exact boundary")
	}
	oversized := localBoundsTestCandidate(1, append(urls, localBoundsTestSource(1))...)
	selected, bounded = boundedLocalPlaceCandidates([]localPlaceCandidate{oversized, extra})
	if !bounded || len(selected) != 1 || selected[0].place.ID != extra.place.ID {
		t.Fatal("101-field candidate was partly exported or blocked a later fitting candidate")
	}
}

func TestNowLocalPlaceBoundsCollectsMemoryUsesOnlyFromRetainedFieldCandidates(t *testing.T) {
	first := localBoundsTestCandidate(1, localBoundsTestSource(1))
	first.uses = []acb.LocalFoodPreference{{MemoryID: "synthetic-retained-preference", Version: 2, Topic: "seafood", Value: "LIKE"}}
	urls := []string{}
	for i := 2; i <= 12; i++ {
		urls = append(urls, localBoundsTestSource(i))
	}
	dropped := localBoundsTestCandidate(2, urls...)
	dropped.uses = []acb.LocalFoodPreference{{MemoryID: "synthetic-dropped-preference", Version: 3, Topic: "spicy", Value: "DISLIKE"}}
	excluded := localBoundsTestCandidate(3, localBoundsTestSource(1))
	excluded.excluded = true
	excluded.uses = append(excluded.uses, first.uses[0])
	selected, bounded := boundedLocalPlaceCandidates([]localPlaceCandidate{first, dropped, excluded})
	fields, uses := localPlaceCandidateEvidence(selected)
	if !bounded || len(fields) != 2 || !reflect.DeepEqual(uses, first.uses) {
		t.Fatal("omitted fields left an unrelated preference in the causal proof", len(fields), uses)
	}
	for _, field := range fields {
		if field.PlaceID == dropped.place.ID {
			t.Fatal("dropped candidate affected digest, validity or attribution")
		}
	}
	if localFieldDigest(fields, uses) == localFieldDigest(append(fields, dropped.fields...), append(uses, dropped.uses...)) {
		t.Fatal("proof failed to distinguish dropped fields and Memory uses")
	}
}

func TestNowLocalPlaceBoundsDoesNotAdmitAnIncompleteIdentitySourceBoundary(t *testing.T) {
	for _, change := range []func(*localPlaceCandidate){
		func(c *localPlaceCandidate) { c.place.ID = "" },
		func(c *localPlaceCandidate) { c.fields = nil },
		func(c *localPlaceCandidate) { c.fields[0].Permission.SourceURL = "" },
		func(c *localPlaceCandidate) { c.fields[0].PlaceID = "other-place" },
	} {
		candidate := localBoundsTestCandidate(1, localBoundsTestSource(1))
		change(&candidate)
		selected, bounded := boundedLocalPlaceCandidates([]localPlaceCandidate{candidate})
		if len(selected) != 0 || !bounded {
			t.Fatal("incomplete identity/source was treated as admitted")
		}
	}
}

func TestNowLocalPlaceBoundsEmptyAndPartialRecommendationNeverClaimsCityWideAbsence(t *testing.T) {
	for _, hasUses := range []bool{false, true} {
		for _, bounded := range []bool{false, true} {
			text := localPlaceRecommendationText(nil, hasUses, bounded)
			if !strings.Contains(text, "本次核验的餐饮地点中") || strings.Contains(text, "站内许可有效的地点中暂时没有") || strings.Contains(text, "全部") {
				t.Fatal("omitted/filtered subset was described as a city-wide empty result", text)
			}
			if bounded && !strings.Contains(text, "本次展示已核验的地点子集") {
				t.Fatal("empty bounded subset hid its scope", text)
			}
		}
	}
	text := localPlaceRecommendationText([]string{"Synthetic venue [source-2]"}, true, true)
	if !strings.Contains(text, "本次展示已核验的地点子集") || !strings.Contains(text, "已按你确认的口味筛选和排序") || !strings.Contains(text, "[source-2]") {
		t.Fatal("bounded recommendation lost truthful scope, actual preference or citation", text)
	}
}
