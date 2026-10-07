package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVenueEditorialIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Venue editorial test requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cityID, places, cleanupPlaces := ownedSocialPlacesFixture(t, ctx, pool, 2)
	defer cleanupPlaces()
	placeID, otherPlaceID := places[0], places[1]
	accounts, cleanupPeople := ownedSocialPeopleFixture(t, ctx, pool, 3)
	defer cleanupPeople()
	tokens := make([]string, 3)
	var candidateID string
	var sessionDigests [][]byte
	defer func() {
		if candidateID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM venues WHERE source_candidate_id=$1`, candidateID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM venue_candidates WHERE id=$1`, candidateID)
		}
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, accounts)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM city_editor_memberships WHERE account_id=ANY($1::uuid[])`, accounts)
		for _, digest := range sessionDigests {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM sessions WHERE token_sha256=$1`, digest)
		}
	}()
	for i := range accounts {
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, accounts[i], digest[:]); e != nil {
			t.Fatal(e)
		}
		sessionDigests = append(sessionDigests, append([]byte(nil), digest[:]...))
		if i < 2 {
			role := "contributor"
			if i == 1 {
				role = "reviewer"
			}
			if _, e = pool.Exec(ctx, `INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES($1,$2,$3)`, cityID, accounts[i], role); e != nil {
				t.Fatal(e)
			}
		}
	}
	store := postgres.New(pool, false)
	s := &server{catalog: store, access: store, venues: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/places/{placeID}/venue", s.getPublicVenue)
	mux.HandleFunc("POST /v1/cities/{cityID}/places/{placeID}/venue-candidates", s.submitVenueCandidate)
	mux.HandleFunc("GET /v1/cities/{cityID}/venue-candidates", s.listVenueCandidates)
	mux.HandleFunc("POST /v1/venue-candidates/{candidateID}/review", s.reviewVenueCandidate)
	call := func(method, path, body string, who int) (int, []byte) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w.Code, w.Body.Bytes()
	}
	readPath := "/v1/places/" + placeID + "/venue"
	if code, _ := call("GET", readPath, "", -1); code != 404 {
		t.Fatalf("Place should not auto-have Venue: %d", code)
	}
	if code, _ := call("GET", "/v1/places/"+otherPlaceID+"/venue", "", -1); code != 404 {
		t.Fatalf("other Place Venue: %d", code)
	}
	postPath := "/v1/cities/" + cityID + "/places/" + placeID + "/venue-candidates"
	body := `{"capacity":32,"reservationSupport":"contact","suitability":["badminton"],"amenities":["indoor_court"],` +
		`"sourceUrl":"https://example.org/synthetic-venue","rightsNote":"Synthetic editorial test evidence",` +
		`"expiresAt":"` + time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339) + `"}`
	if code, _ := call("POST", postPath, body, -1); code != 401 {
		t.Fatalf("anonymous submitted: %d", code)
	}
	if code, _ := call("POST", postPath, body, 2); code != 403 {
		t.Fatalf("non-editor submitted: %d", code)
	}
	code, out := call("POST", postPath, body, 0)
	if code != 201 {
		t.Fatalf("editor submit: %d %s", code, out)
	}
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(out, &response); err != nil {
		t.Fatal(err)
	}
	candidateID = response.Data.ID
	if code, _ = call("POST", postPath, body, 0); code != 409 {
		t.Fatalf("duplicate pending Venue: %d", code)
	}
	if code, _ = call("GET", readPath, "", -1); code != 404 {
		t.Fatalf("pending Venue leaked: %d", code)
	}
	reviewPath := "/v1/venue-candidates/" + candidateID + "/review"
	review := `{"decision":"approve","note":"Synthetic independent review"}`
	if code, _ = call("POST", reviewPath, review, 0); code != 403 && code != 409 {
		t.Fatalf("self review: %d", code)
	}
	if code, _ = call("POST", reviewPath, review, 2); code != 403 {
		t.Fatalf("non-reviewer approved: %d", code)
	}
	if code, out = call("POST", reviewPath, review, 1); code != 200 {
		t.Fatalf("reviewer approve: %d %s", code, out)
	}
	if code, out = call("GET", readPath, "", -1); code != 200 || !strings.Contains(string(out), `"capacity":32`) ||
		strings.Contains(string(out), `"operatorOrganizationId"`) {
		t.Fatalf("approved public Venue: %d %s", code, out)
	}
	if code, _ = call("POST", reviewPath, review, 1); code != 409 {
		t.Fatalf("repeat approval: %d", code)
	}
	var audits int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE resource_type='venue_candidate' AND resource_id=$1`, candidateID).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("venue audit: %d %v", audits, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if code, _ = call("GET", readPath, "", -1); code != 404 {
		t.Fatalf("hidden Place leaked Venue: %d", code)
	}
	if _, err = pool.Exec(ctx, `UPDATE places SET publication_status='published' WHERE id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE venues SET expires_at=now()-interval '1 hour' WHERE place_id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if code, _ = call("GET", readPath, "", -1); code != 404 {
		t.Fatalf("expired Venue leaked: %d", code)
	}
}
