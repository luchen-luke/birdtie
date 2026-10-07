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
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPlaceMatchingRealReviewedSupplyIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Place match requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cityID, places, cleanupPlaces := ownedSocialPlacesFixture(t, ctx, pool, 1)
	defer cleanupPlaces()
	placeID := places[0]
	people, cleanupPeople := ownedSocialPeopleFixture(t, ctx, pool, 2)
	defer cleanupPeople()
	accounts := append(people, "")
	if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&accounts[2]); err != nil {
		t.Fatal(err)
	}
	defer ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM accounts WHERE id=$1`, accounts[2])
	tokens := make([]string, 3)
	var candidateID, intentID string
	var sessionDigests [][]byte
	defer func() {
		if intentID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM social_intents WHERE id=$1`, intentID)
		}
		if candidateID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM venues WHERE source_candidate_id=$1`, candidateID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM venue_candidates WHERE id=$1`, candidateID)
		}
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, accounts)
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
	}
	store := postgres.New(pool, false)
	constraints, _ := json.Marshal(map[string]any{"placeId": placeID, "category": "badminton", "maxParticipants": 12})
	intent, err := store.CreateSocialIntentDraft(ctx, accounts[0], socialintent.DraftInput{
		Type: "ORGANIZE", Title: "Synthetic badminton", Constraints: constraints,
		Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: time.Now().Add(48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	intentID = intent.ID
	if _, err = store.ActivateSocialIntent(ctx, accounts[0], intentID); err != nil {
		t.Fatal(err)
	}
	s := &server{access: store, placeMatches: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/social-intents/{intentID}/place-matches", s.listOwnPlaceMatches)
	path := "/v1/me/social-intents/" + intentID + "/place-matches"
	call := func(who int) (int, []byte) {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		if who >= 0 {
			r.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	if code, _ := call(-1); code != 401 {
		t.Fatalf("anonymous match: %d", code)
	}
	if code, _ := call(1); code != 404 {
		t.Fatalf("other Person read own private Intent: %d", code)
	}
	if code, _ := call(2); code != 403 {
		t.Fatalf("Organization match: %d", code)
	}
	if code, out := call(0); code != 200 || !strings.Contains(string(out), `"data":[]`) {
		t.Fatalf("empty supply must be honest: %d %s", code, out)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO venue_candidates(place_id,city_id,submitted_by,capacity,
		reservation_support,suitability,source_url,rights_note,expires_at,status,reviewed_by,reviewed_at)
		VALUES($1,$2,$3,20,'contact',ARRAY['badminton'],
		'https://example.org/synthetic-venue','Synthetic rights evidence',
		now()+interval '3 days','approved',$4,now()) RETURNING id`,
		placeID, cityID, accounts[0], accounts[1]).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO venues(place_id,city_id,capacity,reservation_support,suitability,
		source_candidate_id,source_url,reviewed_by,reviewed_at,expires_at)
		VALUES($1,$2,20,'contact',ARRAY['badminton'],$3,
		'https://example.org/synthetic-venue',$4,now(),now()+interval '3 days')`,
		placeID, cityID, candidateID, accounts[1]); err != nil {
		t.Fatal(err)
	}
	code, out := call(0)
	if code != 200 || !strings.Contains(string(out), `"hasReviewedVenue":true`) ||
		!strings.Contains(string(out), placeID) || !strings.Contains(string(out), `"source":"RULE_BASED"`) {
		t.Fatalf("reviewed Place match: %d %s", code, out)
	}
	if _, err = pool.Exec(ctx, `UPDATE venues SET capacity=5 WHERE place_id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if code, out = call(0); code != 200 || !strings.Contains(string(out), `"data":[]`) {
		t.Fatalf("insufficient capacity matched: %d %s", code, out)
	}
	if _, err = pool.Exec(ctx, `UPDATE venues SET capacity=20 WHERE place_id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if code, out = call(0); code != 200 || !strings.Contains(string(out), `"data":[]`) {
		t.Fatalf("hidden Place matched: %d %s", code, out)
	}
	if _, err = pool.Exec(ctx, `UPDATE places SET publication_status='published' WHERE id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE venues SET expires_at=now()-interval '1 hour' WHERE place_id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if code, out = call(0); code != 200 || !strings.Contains(string(out), `"data":[]`) {
		t.Fatalf("expired Venue matched: %d %s", code, out)
	}
}
