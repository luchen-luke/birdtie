package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPlaceContextIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Place editorial integration requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	people, cleanupPeople := ownedSocialPeopleFixture(t, ctx, pool, 3)
	defer cleanupPeople()
	store := postgres.New(pool, false)
	var existingPlaceID, cityID string
	if err := pool.QueryRow(ctx, `SELECT p.id,p.city_id FROM places p
		WHERE p.id='b1700000-0000-4000-8000-000000000004'
		AND p.publication_status='published' AND p.location_precision='point'
		AND (p.expires_at IS NULL OR p.expires_at>now())
		AND EXISTS(SELECT 1 FROM activities a WHERE a.place_id=p.id AND a.publication_status='published'
		AND a.visibility='public' AND a.ends_at>now())`).Scan(&existingPlaceID, &cityID); err != nil {
		t.Fatalf("required 001_badminton seed Place/public Activity missing: %v", err)
	}
	existing, err := store.GetPlace(ctx, existingPlaceID)
	if err != nil || existing.ID != existingPlaceID {
		t.Fatalf("existing Place: %+v %v", existing, err)
	}
	linked, err := store.ListPlaceActivities(ctx, existingPlaceID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) == 0 {
		t.Fatal("seeded Place lost public Activity link")
	}
	for _, activity := range linked {
		if activity.PlaceID != existingPlaceID {
			t.Fatalf("cross-Place activity: %+v", activity)
		}
	}
	// Visibility checks mutate only this test's Activity. The seed and another
	// package's short-lived Activities remain untouched during parallel tests.
	fixtureActivity, err := store.CreateSocialDraft(ctx, people[0], activitypublish.Input{
		Organizer: activitypublish.Organizer{Type: "PERSON", ID: people[0]},
		CityID:    cityID, PlaceID: existingPlaceID, Title: "Synthetic Place visibility fixture",
		Summary: "Disposable fixture", StartsAt: time.Now().Add(48 * time.Hour),
		EndsAt: time.Now().Add(50 * time.Hour), TimeZone: "Europe/London", Visibility: "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	privateActivityID := fixtureActivity.ID
	defer func() {
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE resource_type='activity' AND resource_id=$1`, privateActivityID)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM activities WHERE id=$1`, privateActivityID)
	}()
	if _, err := store.PublishSocialActivity(ctx, people[0], privateActivityID); err != nil {
		t.Fatal(err)
	}
	publicLinked, err := store.ListPlaceActivities(ctx, existingPlaceID, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, activity := range publicLinked {
		found = found || activity.ID == privateActivityID
	}
	if !found {
		t.Fatal("own public Activity missing from Place before visibility change")
	}
	if _, err := pool.Exec(ctx, `UPDATE activities SET visibility='invite_only' WHERE id=$1`, privateActivityID); err != nil {
		t.Fatal(err)
	}
	privateFiltered, err := store.ListPlaceActivities(ctx, existingPlaceID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, activity := range privateFiltered {
		if activity.ID == privateActivityID {
			t.Fatal("invite-only Activity leaked from Place")
		}
	}
	personID := people[0]
	var privateMomentID string
	if err := pool.QueryRow(ctx, `INSERT INTO moments
		(author_account_id,city_id,place_id,title,location_precision)
		VALUES($1,$2,$3,'Synthetic private Place memory','place') RETURNING id`,
		personID, cityID, existingPlaceID).Scan(&privateMomentID); err != nil {
		t.Fatal(err)
	}
	defer ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM moments WHERE id=$1`, privateMomentID)
	var linkedMomentPlace string
	if err := pool.QueryRow(ctx, `SELECT place_id FROM moments WHERE id=$1`, privateMomentID).Scan(&linkedMomentPlace); err != nil || linkedMomentPlace != existingPlaceID {
		t.Fatalf("private Moment Place link: %s %v", linkedMomentPlace, err)
	}
	contributors := people[1:]
	var candidateID, newPlaceID string
	defer func() {
		if candidateID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM inbox_items WHERE resource_type='place_candidate' AND resource_id=$1`, candidateID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM place_sources WHERE candidate_id=$1`, candidateID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM place_candidates WHERE id=$1`, candidateID)
		}
		if newPlaceID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM city_seed_items WHERE place_id=$1`, newPlaceID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM places WHERE id=$1`, newPlaceID)
		}
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, contributors)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM city_editor_memberships WHERE account_id=ANY($1::uuid[])`, contributors)
	}()
	for i, id := range contributors {
		role := "contributor"
		if i == 1 {
			role = "reviewer"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO city_editor_memberships(city_id,account_id,role)
			VALUES($1,$2,$3)`, cityID, id, role); err != nil {
			t.Fatal(err)
		}
	}
	lat, lon := 57.15, -2.1
	input := cityseed.SubmitInput{
		Name: "Synthetic reviewed venue", CategoryCode: "sports", Summary: "Disposable Place test",
		AddressLabel: "Synthetic public venue address", Latitude: &lat, Longitude: &lon,
		LocationPrecision: "point", SourceLabel: "Synthetic source", SourceURL: "https://example.org/place",
		RightsNote: "Synthetic review rights", ExpiresAt: time.Now().Add(48 * time.Hour),
	}
	if !validCandidate(&input) {
		t.Fatal("valid sourced address rejected")
	}
	invalid := input
	invalid.LocationPrecision = "area"
	if validCandidate(&invalid) {
		t.Fatal("address accepted without point precision")
	}
	candidate, err := store.Submit(ctx, contributors[0], cityID, input)
	if err != nil {
		t.Fatal(err)
	}
	candidateID = candidate.ID
	if candidate.AddressLabel != input.AddressLabel || candidate.Status != "pending" {
		t.Fatalf("candidate address/status: %+v", candidate)
	}
	if _, err := store.Review(ctx, contributors[0], candidateID,
		cityseed.ReviewInput{Decision: "publish", Note: "Synthetic review"}); !errors.Is(err, cityseed.ErrForbidden) && !errors.Is(err, cityseed.ErrConflict) {
		t.Fatalf("submitter reviewed own candidate: %v", err)
	}
	reviewed, err := store.Review(ctx, contributors[1], candidateID,
		cityseed.ReviewInput{Decision: "publish", Note: "Synthetic independent review"})
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.ResolvedPlaceID == nil {
		t.Fatalf("review missing Place ID: %+v", reviewed)
	}
	newPlaceID = *reviewed.ResolvedPlaceID
	place, err := store.GetPlace(ctx, newPlaceID)
	if err != nil || place.ID != newPlaceID || place.AddressLabel != input.AddressLabel ||
		place.Source.Label != input.SourceLabel || place.CityID != cityID ||
		place.Location.Latitude == nil || *place.Location.Latitude != lat {
		t.Fatalf("canonical reviewed Place: %+v %v", place, err)
	}
	s := &server{catalog: store, access: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/places/{placeID}", s.getPlace)
	mux.HandleFunc("GET /v1/places/{placeID}/activities", s.listPlaceActivities)
	get := func(path string) (int, []byte) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w.Code, w.Body.Bytes()
	}
	if status, body := get("/v1/places/" + newPlaceID); status != 200 || !strings.Contains(string(body), input.AddressLabel) {
		t.Fatalf("Place detail: %d %s", status, body)
	}
	if status, body := get("/v1/places/" + existingPlaceID); status != 200 || strings.Contains(string(body), privateMomentID) {
		t.Fatalf("private Moment leaked through Place: %d %s", status, body)
	}
	if status, body := get("/v1/places/" + existingPlaceID + "/activities"); status != 200 || strings.Contains(string(body), privateActivityID) {
		t.Fatalf("invite-only Activity leaked through Place HTTP: %d %s", status, body)
	}
	if status, body := get("/v1/places/" + newPlaceID + "/activities"); status != 200 {
		t.Fatalf("Place activity link: %d %s", status, body)
	} else {
		var result struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil || len(result.Data) != 0 {
			t.Fatalf("empty Place activity list: %+v %v", result, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, newPlaceID); err != nil {
		t.Fatal(err)
	}
	if status, _ := get("/v1/places/" + newPlaceID); status != 404 {
		t.Fatalf("hidden Place leaked: %d", status)
	}
	if status, _ := get("/v1/places/" + newPlaceID + "/activities"); status != 404 {
		t.Fatalf("hidden Place activity leaked: %d", status)
	}
	if _, err := pool.Exec(ctx, `UPDATE places SET publication_status='published',expires_at=now()-interval '1 hour' WHERE id=$1`, newPlaceID); err != nil {
		t.Fatal(err)
	}
	if status, _ := get("/v1/places/" + newPlaceID); status != 404 {
		t.Fatalf("expired Place leaked: %d", status)
	}
	if _, err := store.GetPlace(ctx, newPlaceID); !errors.Is(err, foundation.ErrNotFound) {
		t.Fatalf("expired Place store read: %v", err)
	}
}
