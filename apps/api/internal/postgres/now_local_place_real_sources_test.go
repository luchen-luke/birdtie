package postgres

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

// These are in-process synthetic rows shaped like the retained API24 fixtures
// and independently reviewed public rows. The URLs are never fetched, and no
// fixture is submitted/published or mistaken for real acquisition evidence.
func localRecommendationTestFacts(number int) (foundation.Place, []cityseed.PlaceFieldEvidence, time.Time) {
	place, fields := nowLocalPlaceTestFacts()
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	place.ID = fmt.Sprintf("test-place-%02d", number)
	place.Name = fmt.Sprintf("Test venue %02d", number)
	place.Source.Reference = fmt.Sprintf("https://www.openstreetmap.org/node/%d", number)
	fields[0].Value, _ = json.Marshal(place.Name)
	reviewer := "test-independent-reviewer"
	reviewed := now.Add(-5 * time.Minute)
	permissionID := fmt.Sprintf("test-permission-%02d", number)
	candidateID := fmt.Sprintf("test-candidate-%02d", number)
	for i := range fields {
		field := &fields[i]
		field.ID = fmt.Sprintf("test-evidence-%02d-%s", number, field.FieldKey)
		field.PlaceID, field.SourceCandidateID = place.ID, candidateID
		field.Version, field.PermissionVersion, field.PermissionID = 1, 1, permissionID
		field.ConflictState, field.RowToken = "NONE", "test-field-xmin:permission-xmin"
		field.ObservedAt, field.VerifiedAt = now.Add(-10*time.Minute), now.Add(-5*time.Minute)
		field.ValidFrom, field.ValidTo, field.RefreshDue = now.Add(-10*time.Minute), now.Add(time.Hour), now.Add(30*time.Minute)
		field.Permission = cityseed.SourcePermission{
			ID: permissionID, CandidateID: candidateID, Version: 1, SourceURL: place.Source.Reference,
			DeclaredBy: "test-declarer", ReviewedBy: &reviewer, ReviewedAt: &reviewed,
			SourcePermissionInput: cityseed.SourcePermissionInput{
				State: cityseed.PermissionConfirmed, Acquisition: "OPEN_LICENSE", OriginProvider: "DIRECT_SOURCE",
				LicenseURL: "https://opendatacommons.org/licenses/odbl/1-0/", LicenseVersion: "ODbL 1.0",
				Attribution: "TEST_ONLY shaped public-source row", AcquiredAt: now.Add(-time.Hour), RetentionUntil: now.Add(2 * time.Hour),
			},
		}
	}
	return place, fields, now
}

func TestNowLocalPlaceRealSourcesRejectReservedOrDevelopmentProvenance(t *testing.T) {
	for _, raw := range []string{
		"dev-seed://aberdeen/test-place", "synthetic://test-place", "http://www.openstreetmap.org/node/1",
		"https://birdtie.example/isolated-fixtures/pg-2", "https://birdtie.example/isolated-fixtures/pg-3",
		"https://BIRDTIE.EXAMPLE./fixture", "https://dev.test/place", "https://example.invalid/place",
		"https://localhost/place", "https://dev.localhost/place", "https://dev.local/place", "https://dev.internal/place",
		"https://host.home.arpa/place", "https://example.com/place", "https://sub.example.net/place", "https://example.org/place",
		"https://127.0.0.1/place", "https://192.168.1.10/place", "https://[::1]/place", "https://one-word/place",
		"https://user@www.openstreetmap.org/node/1", "https://www.openstreetmap.org/node/1#fixture",
	} {
		t.Run(raw, func(t *testing.T) {
			if localRecommendationPublicSource(raw) {
				t.Fatal("development or reserved URL admitted as real-place provenance", raw)
			}
		})
	}
	for _, raw := range []string{
		"https://www.openstreetmap.org/node/290633294", "https://www.aberdeencity.gov.uk/venue",
		"https://birdtie.app/owned-place/1", "https://restaurant.co.uk/location", "https://example.com.restaurant.co.uk/venue",
	} {
		if !localRecommendationPublicSource(raw) {
			t.Fatal("public host was rejected using a provider or name allowlist", raw)
		}
	}
}

func TestNowLocalPlaceRealSourcesRequireEveryCompleteFieldAndPermission(t *testing.T) {
	place, fields, now := localRecommendationTestFacts(1)
	if !localRecommendationFieldsUsable(place, fields, now) {
		t.Fatal("complete permitted source shape rejected")
	}
	for _, test := range []struct {
		name string
		edit func(*foundation.Place, []cityseed.PlaceFieldEvidence)
	}{
		{"canonical development source", func(p *foundation.Place, _ []cityseed.PlaceFieldEvidence) {
			p.Source.Reference = "dev-seed://test-only"
		}},
		{"one fixture field source", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) {
			f[1].Permission.SourceURL = "https://birdtie.example/fixture"
		}},
		{"one fixture license", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) {
			f[2].Permission.LicenseURL = "https://birdtie.example/permission"
		}},
		{"unreviewed permission", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].Permission.ReviewedBy = nil }},
		{"self-reviewed permission", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) {
			f[1].Permission.DeclaredBy = *f[1].Permission.ReviewedBy
		}},
		{"pending permission", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) {
			f[1].Permission.State = cityseed.PermissionPending
		}},
		{"revoked permission", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) {
			revoked := now.Add(-time.Second)
			f[1].Permission.RevokedAt = &revoked
		}},
		{"permission expiry", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].Permission.RetentionUntil = now }},
		{"permission id mismatch", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].PermissionID = "another-permission" }},
		{"permission version changed", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].Permission.Version++ }},
		{"candidate id mismatch", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) {
			f[1].SourceCandidateID = "another-candidate"
		}},
		{"another place field", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].PlaceID = "another-place" }},
		{"missing native field token", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].RowToken = "" }},
		{"missing evidence id", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].ID = "" }},
		{"invalid field version", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].Version = 0 }},
		{"conflicting field", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].ConflictState = "UNRESOLVED" }},
		{"stale field", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].RefreshDue = now }},
		{"expired field", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].ValidTo = now }},
		{"future field", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].ValidFrom = now.Add(time.Second) }},
		{"unobserved field", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].ObservedAt = time.Time{} }},
		{"future observation", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].ObservedAt = now.Add(time.Second) }},
		{"unverified field", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].VerifiedAt = time.Time{} }},
		{"future verification", func(_ *foundation.Place, f []cityseed.PlaceFieldEvidence) { f[1].VerifiedAt = now.Add(time.Second) }},
		{"canonical name differs", func(p *foundation.Place, _ []cityseed.PlaceFieldEvidence) { p.Name = "Other place" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := place
			f := append([]cityseed.PlaceFieldEvidence(nil), fields...)
			test.edit(&p, f)
			if localRecommendationFieldsUsable(p, f, now) {
				t.Fatal("one field's invalid provenance or authority was discarded while retaining its card")
			}
		})
	}
	if localRecommendationFieldsUsable(place, fields[1:], now) || localRecommendationFieldsUsable(place, fields, time.Time{}) {
		t.Fatal("missing identity field or observation time treated as sufficient")
	}
}

func TestNowLocalPlaceRealSourcesDoNotReplaceRightsReviewWithPublicLookingURL(t *testing.T) {
	place, fields, now := localRecommendationTestFacts(1)
	for _, acquisition := range []string{"FIRST_PARTY", "DIRECT_AUTHORIZATION", "OPEN_LICENSE"} {
		f := append([]cityseed.PlaceFieldEvidence(nil), fields...)
		for i := range f {
			f[i].Permission.Acquisition = acquisition
		}
		if !localRecommendationFieldsUsable(place, f, now) {
			t.Fatal("existing permitted acquisition route was replaced by an OSM-only allowlist", acquisition)
		}
		f[0].Permission.State = cityseed.PermissionPending
		if localRecommendationFieldsUsable(place, f, now) {
			t.Fatal("a public-looking URL substituted for confirmation", acquisition)
		}
	}
	// No substring check on a user's real canonical name or description is used.
	place.Name = "Example Restaurant"
	fields[0].Value, _ = json.Marshal(place.Name)
	if !localRecommendationFieldsUsable(place, fields, now) {
		t.Fatal("canonical place name was used as a fixture blacklist")
	}
}

func TestNowLocalPlaceRealSourcesAPI24MixedShapeOmitsFixturesBeforeBudgetAndMemoryUses(t *testing.T) {
	input := []localPlaceCandidate{}
	admitted := []localPlaceCandidate{}
	for i := 1; i <= 17; i++ {
		place, fields, now := localRecommendationTestFacts(i)
		candidate := localPlaceCandidate{place: place, fields: fields}
		if i == 6 || i == 7 {
			candidate.place.Source.Reference = fmt.Sprintf("https://birdtie.example/isolated-fixtures/pg-%d", i-4)
			candidate.place.Name = fmt.Sprintf("PG合成%d", i)
			candidate.fields[0].Value, _ = json.Marshal(candidate.place.Name)
			for j := range candidate.fields {
				candidate.fields[j].Permission.SourceURL = candidate.place.Source.Reference
				candidate.fields[j].Permission.LicenseURL = "https://birdtie.example/isolated-fixtures/permission"
				candidate.fields[j].Permission.Acquisition = "FIRST_PARTY"
				candidate.fields[j].Permission.OriginProvider = "BIRDTIE_FIRST_PARTY"
			}
			candidate.uses = []acb.LocalFoodPreference{{MemoryID: "test-fixture-only-use", Topic: "spicy", Value: "LIKE", Version: 2}}
		}
		input = append(input, candidate)
		if localRecommendationFieldsUsable(candidate.place, candidate.fields, now) {
			admitted = append(admitted, candidate)
		}
	}
	selected, bounded := boundedLocalPlaceCandidates(admitted)
	fields, uses := localPlaceCandidateEvidence(selected)
	if len(input) != 17 || len(admitted) != 15 || !bounded || len(selected) != 10 || len(fields) != 30 || len(uses) != 0 {
		t.Fatal("mixed source shape lost real source capacity or retained fixture causal evidence", len(admitted), len(selected), len(fields), uses)
	}
	wantIDs := []string{"test-place-01", "test-place-02", "test-place-03", "test-place-04", "test-place-05", "test-place-08", "test-place-09", "test-place-10", "test-place-11", "test-place-12"}
	if !reflect.DeepEqual(localBoundsCandidateIDs(selected), wantIDs) {
		t.Fatal("reserved fixture consumed a real-place slot", localBoundsCandidateIDs(selected))
	}
	for _, field := range fields {
		if !localRecommendationPublicSource(field.Permission.SourceURL) || strings.Contains(field.PlaceID, "-06") || strings.Contains(field.PlaceID, "-07") {
			t.Fatal("a fixture field survived into citation/digest/deadline evidence")
		}
	}
	// Filtering never mutates retained rows or their prior message references.
	if !strings.Contains(input[5].place.Source.Reference, "birdtie.example") || input[5].uses[0].MemoryID != "test-fixture-only-use" {
		t.Fatal("filter mutated the original retained fixture")
	}
}

func TestNowLocalPlaceRealSourcesFilterHasNoRealAuthorityWithoutFieldsAndNeverRewritesHistory(t *testing.T) {
	place, fields, now := localRecommendationTestFacts(1)
	place.Source.Reference = "https://birdtie.example/isolated-fixtures/pg-2"
	before, _ := json.Marshal(struct {
		Place  foundation.Place
		Fields []cityseed.PlaceFieldEvidence
	}{place, fields})
	if localRecommendationFieldsUsable(place, fields, now) || localRecommendationFieldsUsable(place, nil, now) {
		t.Fatal("retained fixture was allowed to produce an empty or successful real recommendation")
	}
	after, _ := json.Marshal(struct {
		Place  foundation.Place
		Fields []cityseed.PlaceFieldEvidence
	}{place, fields})
	if !reflect.DeepEqual(before, after) {
		t.Fatal("recommendation exclusion rewrote the original source history")
	}
}
