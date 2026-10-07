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

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBusinessOrganizerAuthorizationIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Business authorization requires disposable database")
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
	var timeZone string
	if err = pool.QueryRow(ctx, `SELECT time_zone FROM cities WHERE id=$1`, cityID).Scan(&timeZone); err != nil {
		t.Fatal(err)
	}
	accounts := make([]string, 5)
	tokens := make([]string, 5)
	var businessID, candidateID, activityID string
	defer func() {
		if activityID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM activities WHERE id=$1`, activityID)
		}
		if businessID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM business_venue_relations WHERE business_id=$1`, businessID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM business_memberships WHERE business_id=$1`, businessID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM businesses WHERE id=$1`, businessID)
		}
		if candidateID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM venues WHERE source_candidate_id=$1`, candidateID)
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM venue_candidates WHERE id=$1`, candidateID)
		}
		ownedAccounts := []string{}
		for _, id := range accounts {
			if id != "" {
				ownedAccounts = append(ownedAccounts, id)
			}
		}
		for _, sql := range []string{
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			ownedSocialFixtureCleanup(t, ctx, pool, sql, ownedAccounts)
		}
	}()
	for i := range accounts {
		kind := "person"
		if i == 3 {
			kind = "business"
		}
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&accounts[i]); err != nil {
			t.Fatal(err)
		}
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, accounts[i], digest[:]); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO businesses(account_id,name) VALUES($1,'Bad principal')`, accounts[0]); err == nil {
		t.Fatal("Person accepted as Business principal")
	}
	if err = pool.QueryRow(ctx, `INSERT INTO businesses(account_id,name) VALUES($1,'Synthetic independent Business') RETURNING id`, accounts[3]).Scan(&businessID); err != nil {
		t.Fatal(err)
	}
	var personalAgentCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, accounts[3]).Scan(&personalAgentCount); err != nil || personalAgentCount != 0 {
		t.Fatalf("Business got a Personal/Organization Agent: %d %v", personalAgentCount, err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO business_memberships(business_id,user_account_id,role)
		VALUES($1,$2,'owner'),($1,$3,'member')`, businessID, accounts[0], accounts[1]); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO business_memberships(business_id,user_account_id,role)
		VALUES($1,$2,'owner')`, businessID, accounts[2]); err == nil {
		t.Fatal("second active owner accepted")
	}
	store := postgres.New(pool, false)
	s := &server{catalog: store, access: store, socialActivityPublish: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/me/activities", s.createSocialActivityDraft)
	mux.HandleFunc("GET /v1/me/activity-organizers/businesses", s.listManagedBusinessOrganizers)
	mux.HandleFunc("POST /v1/me/activities/{activityID}/publish", s.publishSocialActivity)
	mux.HandleFunc("PUT /v1/me/activities/{activityID}", s.updateSocialActivity)
	mux.HandleFunc("POST /v1/me/activities/{activityID}/cancel", s.cancelSocialActivity)
	mux.HandleFunc("GET /v1/activities/{activityID}", s.getActivity)
	mux.HandleFunc("GET /v1/cities/{cityID}/activities", s.listActivities)
	start := time.Now().Add(36 * time.Hour).UTC().Truncate(time.Second)
	in := activitypublish.Input{Organizer: activitypublish.Organizer{Type: "BUSINESS", ID: businessID},
		CityID: cityID, PlaceID: placeID, Title: "Synthetic Business activity", Summary: "Synthetic",
		StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: timeZone, Visibility: "public"}
	input, e := json.Marshal(in)
	if e != nil {
		t.Fatal(e)
	}
	call := func(method, path string, body []byte, who int) (int, []byte) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w.Code, w.Body.Bytes()
	}
	create := func(who int) (int, []byte) { return call("POST", "/v1/me/activities", input, who) }
	organizers := func(who int) (int, []byte) {
		return call("GET", "/v1/me/activity-organizers/businesses", nil, who)
	}
	if code, _ := organizers(-1); code != 401 {
		t.Fatalf("anonymous organizer list: %d", code)
	}
	if code, _ := organizers(3); code != 403 {
		t.Fatalf("Business principal organizer list: %d", code)
	}
	if code, body := organizers(0); code != 200 || strings.Contains(string(body), businessID) {
		t.Fatalf("pending claim listed: %d %s", code, body)
	}
	if code, _ := create(-1); code != 401 {
		t.Fatalf("anonymous Business draft: %d", code)
	}
	if code, _ := create(3); code != 403 {
		t.Fatalf("Business account acting as Person: %d", code)
	}
	if code, _ := create(0); code != 403 {
		t.Fatalf("pending claim published authority: %d", code)
	}
	if _, err = pool.Exec(ctx, `UPDATE businesses SET claim_status='verified',claim_source_url='https://example.org/synthetic-claim',
		claim_reviewed_by=$2,claim_reviewed_at=now() WHERE id=$1`, businessID, accounts[4]); err != nil {
		t.Fatal(err)
	}
	if code, body := organizers(0); code != 200 || !strings.Contains(string(body), businessID) {
		t.Fatalf("verified owner Business missing: %d %s", code, body)
	}
	if code, body := organizers(1); code != 200 || strings.Contains(string(body), businessID) {
		t.Fatalf("ordinary Business member listed: %d %s", code, body)
	}
	if _, err = pool.Exec(ctx, `UPDATE business_memberships SET role='admin' WHERE business_id=$1 AND user_account_id=$2`, businessID, accounts[1]); err != nil {
		t.Fatal(err)
	}
	if code, body := organizers(1); code != 200 || !strings.Contains(string(body), businessID) {
		t.Fatalf("Business admin missing: %d %s", code, body)
	}
	if _, err = pool.Exec(ctx, `UPDATE business_memberships SET role='member' WHERE business_id=$1 AND user_account_id=$2`, businessID, accounts[1]); err != nil {
		t.Fatal(err)
	}
	if code, body := organizers(2); code != 200 || strings.Contains(string(body), businessID) {
		t.Fatalf("outsider Business listed: %d %s", code, body)
	}
	if code, _ := create(0); code != 403 {
		t.Fatalf("unclaimed Venue accepted: %d", code)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO venue_candidates(place_id,city_id,submitted_by,capacity,
		reservation_support,source_url,rights_note,expires_at,status,reviewed_by,reviewed_at)
		VALUES($1,$2,$3,24,'contact','https://example.org/synthetic-venue',
		'Synthetic review rights',now()+interval '3 days','approved',$4,now()) RETURNING id`,
		placeID, cityID, accounts[0], accounts[4]).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO venues(place_id,city_id,capacity,reservation_support,source_candidate_id,
		source_url,reviewed_by,reviewed_at,expires_at)
		VALUES($1,$2,24,'contact',$3,'https://example.org/synthetic-venue',$4,now(),now()+interval '3 days')`,
		placeID, cityID, candidateID, accounts[4]); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO business_venue_relations(business_id,place_id,evidence_url,submitted_by)
		VALUES($1,$2,'https://example.org/synthetic-operation',$3)`, businessID, placeID, accounts[0]); err != nil {
		t.Fatal(err)
	}
	if code, _ := create(0); code != 403 {
		t.Fatalf("pending Venue relation accepted: %d", code)
	}
	if _, err = pool.Exec(ctx, `UPDATE business_venue_relations SET status='verified',reviewed_by=$3,
		reviewed_at=now() WHERE business_id=$1 AND place_id=$2`, businessID, placeID, accounts[4]); err != nil {
		t.Fatal(err)
	}
	publicVenue, err := store.GetPublicVenue(ctx, placeID)
	if err != nil || len(publicVenue.Businesses) != 1 || publicVenue.Businesses[0].ID != businessID {
		t.Fatalf("verified business Place relation: venue=%+v err=%v", publicVenue, err)
	}
	if code, _ := create(1); code != 403 {
		t.Fatalf("ordinary member created Business activity: %d", code)
	}
	if code, _ := create(2); code != 403 {
		t.Fatalf("outsider created Business activity: %d", code)
	}
	code, out := create(0)
	if code != 201 {
		t.Fatalf("owner Business draft: %d %s", code, out)
	}
	var result struct {
		Data activitypublish.Activity `json:"data"`
	}
	if err = json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	activityID = result.Data.ID
	if result.Data.Organizer.Type != "BUSINESS" || result.Data.Organizer.ID != businessID || result.Data.OrganizationID != "" {
		t.Fatalf("Business organizer projection: %+v", result.Data)
	}
	if code, _ = call("POST", "/v1/me/activities/"+activityID+"/publish", nil, 1); code != 403 {
		t.Fatalf("ordinary Business member published: %d", code)
	}
	if code, _ = call("PUT", "/v1/me/activities/"+activityID, input, 2); code != 403 {
		t.Fatalf("outsider edited Business activity: %d", code)
	}
	if _, err = pool.Exec(ctx, `UPDATE business_memberships SET role='admin'
		WHERE business_id=$1 AND user_account_id=$2`, businessID, accounts[1]); err != nil {
		t.Fatal(err)
	}
	if code, out = call("POST", "/v1/me/activities/"+activityID+"/publish", nil, 1); code != 200 {
		t.Fatalf("Business admin publish: %d %s", code, out)
	}
	if _, err = pool.Exec(ctx, `UPDATE business_memberships SET role='member'
		WHERE business_id=$1 AND user_account_id=$2`, businessID, accounts[1]); err != nil {
		t.Fatal(err)
	}
	if code, _ = call("POST", "/v1/me/activities/"+activityID+"/cancel", nil, 1); code != 403 {
		t.Fatalf("ordinary Business member cancelled: %d", code)
	}
	if code, out = call("PUT", "/v1/me/activities/"+activityID, input, 0); code != 200 {
		t.Fatalf("Business owner edit: %d %s", code, out)
	}
	if code, out = call("GET", "/v1/activities/"+activityID, nil, -1); code != 200 || !strings.Contains(string(out), businessID) {
		t.Fatalf("Business public Activity: %d %s", code, out)
	}
	if _, err = pool.Exec(ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if code, _ = call("GET", "/v1/activities/"+activityID, nil, -1); code != 404 {
		t.Fatalf("hidden Venue Place leaked Business Activity: %d", code)
	}
	if _, err = pool.Exec(ctx, `UPDATE places SET publication_status='published' WHERE id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE business_venue_relations SET status='revoked' WHERE business_id=$1 AND place_id=$2`, businessID, placeID); err != nil {
		t.Fatal(err)
	}
	publicVenue, err = store.GetPublicVenue(ctx, placeID)
	if err != nil || len(publicVenue.Businesses) != 0 {
		t.Fatalf("revoked business leaked from Venue: venue=%+v err=%v", publicVenue, err)
	}
	if code, _ = call("GET", "/v1/activities/"+activityID, nil, -1); code != 404 {
		t.Fatalf("revoked operation still visible: %d", code)
	}
	if code, out = call("POST", "/v1/me/activities/"+activityID+"/cancel", nil, 0); code != 200 {
		t.Fatalf("Business owner cancel: %d %s", code, out)
	}
}
