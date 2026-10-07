package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOpportunityHTTPRealSupplyIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Opportunity integration requires disposable database")
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
	const category = "badminton"
	var activityID, communityID string
	accounts := make([]string, 4) // viewer, Organization, friend, independent host
	tokens := make([]string, len(accounts))
	defer func() {
		ownedAccounts := []string{}
		for _, id := range accounts {
			if id != "" {
				ownedAccounts = append(ownedAccounts, id)
			}
		}
		if activityID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM activities WHERE id=$1`, activityID)
		}
		if communityID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM communities WHERE id=$1`, communityID)
		}
		for _, sql := range []string{
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			ownedSocialFixtureCleanup(t, ctx, pool, sql, ownedAccounts)
		}
	}()
	for i := range accounts {
		kind := "person"
		if i == 1 {
			kind = "organization"
		}
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type)
			VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&accounts[i]); err != nil {
			t.Fatal(err)
		}
		if i != 1 {
			if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility)
				VALUES($1,'Synthetic opportunity tester','public')`, accounts[i]); err != nil {
				t.Fatal(err)
			}
		}
		token, digest, err := identity.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = token
		if _, err := pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, accounts[i], digest[:]); err != nil {
			t.Fatal(err)
		}
	}
	store := postgres.New(pool, false)
	start := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	draft, err := store.CreateSocialDraft(ctx, accounts[3], activitypublish.Input{
		Organizer: activitypublish.Organizer{Type: "PERSON", ID: accounts[3]},
		CityID:    cityID, PlaceID: placeID, Title: "Owned synthetic opportunity supply", Summary: "Synthetic integration supply",
		StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Europe/London", CategoryCode: category,
		Visibility: "public", Modality: "in_person", PhysicalPlaceStatus: "confirmed",
	})
	if err != nil {
		t.Fatal(err)
	}
	activityID = draft.ID
	if _, err := store.PublishSocialActivity(ctx, accounts[3], activityID); err != nil {
		t.Fatal(err)
	}
	constraints, _ := json.Marshal(map[string]any{"placeId": placeID, "category": category})
	intent, err := store.CreateSocialIntentDraft(ctx, accounts[0], socialintent.DraftInput{
		Type: "FIND_ACTIVITY", Title: "Synthetic local activity intent",
		Constraints: constraints, Audience: "PRIVATE", Modality: "IN_PERSON",
		ExpiresAt: time.Now().Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActivateSocialIntent(ctx, accounts[0], intent.ID); err != nil {
		t.Fatal(err)
	}
	ownedCommunity, err := store.CreateSocialCommunity(ctx, accounts[3], community.SocialInput{
		Name: "Owned opportunity Community", Summary: "Synthetic relationship fixture", Visibility: "public", JoinPolicy: "open",
	})
	if err != nil {
		t.Fatal(err)
	}
	communityID = ownedCommunity.ID
	member, err := store.JoinSocialCommunity(ctx, accounts[0], communityID)
	if err != nil || member.Status != "active" {
		t.Fatalf("explicit Community membership: %+v %v", member, err)
	}
	request, err := store.CreateFriendRequest(ctx, accounts[0], accounts[2], "Synthetic invitation to connect")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideRequest(ctx, accounts[2], request.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	inputs, err := store.LoadOpportunityInputs(ctx, accounts[0])
	if err != nil || !inputs.TiedPeople[accounts[2]] || !inputs.JoinedCommunities[communityID] {
		t.Fatalf("authorized Tie/Community inputs: tie=%v community=%v err=%v",
			inputs.TiedPeople, inputs.JoinedCommunities, err)
	}
	if err := store.BlockAccount(ctx, accounts[0], accounts[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE community_memberships SET status='left'
		WHERE community_id=$1 AND user_account_id=$2`, communityID, accounts[0]); err != nil {
		t.Fatal(err)
	}
	inputs, err = store.LoadOpportunityInputs(ctx, accounts[0])
	if err != nil || inputs.TiedPeople[accounts[2]] || inputs.JoinedCommunities[communityID] {
		t.Fatalf("revoked Tie/Community still ranked: tie=%v community=%v err=%v",
			inputs.TiedPeople, inputs.JoinedCommunities, err)
	}
	s := &server{access: store, opportunities: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/opportunities", s.listOwnOpportunities)
	call := func(who int) (int, []map[string]any) {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/me/opportunities", nil)
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var response struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &response)
		return w.Code, response.Data
	}
	if code, _ := call(-1); code != 401 {
		t.Fatalf("anonymous opportunities: %d", code)
	}
	if code, _ := call(1); code != 403 {
		t.Fatalf("organization opportunities: %d", code)
	}
	code, items := call(0)
	if code != 200 || len(items) == 0 {
		t.Fatalf("verified supply: %d %+v", code, items)
	}
	found := false
	for _, item := range items {
		if item["intentId"] != intent.ID {
			continue
		}
		entity := item["entity"].(map[string]any)
		place := item["place"].(map[string]any)
		if entity["id"] == activityID && entity["type"] == "ACTIVITY" && place["id"] == placeID {
			found = true
		}
	}
	if !found {
		t.Fatalf("stable Activity/Place references missing: %+v", items)
	}
	if _, err := pool.Exec(ctx, `UPDATE activities SET visibility='invite_only' WHERE id=$1`, activityID); err != nil {
		t.Fatal(err)
	}
	code, items = call(0)
	if code != 200 {
		t.Fatalf("private supply request: %d", code)
	}
	for _, item := range items {
		if item["entity"].(map[string]any)["id"] == activityID {
			t.Fatal("invite-only supply leaked")
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE activities SET visibility='public' WHERE id=$1`, activityID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE places SET publication_status='hidden' WHERE id=$1`, placeID); err != nil {
		t.Fatal(err)
	}
	code, items = call(0)
	if code != 200 {
		t.Fatalf("hidden Place request: %d", code)
	}
	for _, item := range items {
		if item["place"].(map[string]any)["id"] == placeID {
			t.Fatal("hidden Place leaked")
		}
	}
}
