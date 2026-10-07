package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSocialIntentDraftHTTPIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for social intent integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.social_intents') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("036_social_intents not applied")
	}
	// Owner, connected Person, Organization, Community member and outsider are
	// owned by this test; concurrent packages may publish their own valid Intents.
	ids := make([]string, 5)
	tokens := make([]string, len(ids))
	ownedIntentIDs := map[string]bool{}
	var communityID string
	defer func() {
		ownedAccounts := []string{}
		for _, id := range ids {
			if id != "" {
				ownedAccounts = append(ownedAccounts, id)
			}
		}
		for _, statement := range []string{
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
		} {
			ownedSocialFixtureCleanup(t, ctx, pool, statement, ownedAccounts)
		}
		if communityID != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM communities WHERE id=$1`, communityID)
		}
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, ownedAccounts)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`, ownedAccounts)
	}()
	for i := range ids {
		kind := "person"
		if i == 2 {
			kind = "organization"
		}
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if kind == "person" {
			if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name) VALUES($1,'Synthetic social intent')`, ids[i]); err != nil {
				t.Fatal(err)
			}
		}
		token, digest, err := identity.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		tokens[i] = token
		if _, err := pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, ids[i], digest[:]); err != nil {
			t.Fatal(err)
		}
	}
	store := postgres.New(pool, false)
	s := &server{access: store, socialIntents: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/me/social-intents", s.createSocialIntentDraft)
	mux.HandleFunc("GET /v1/me/social-intents", s.listOwnSocialIntents)
	mux.HandleFunc("GET /v1/me/social-intents/{intentID}", s.getOwnSocialIntent)
	mux.HandleFunc("POST /v1/me/social-intents/{intentID}/activate", s.activateSocialIntent)
	mux.HandleFunc("POST /v1/me/social-intents/{intentID}/cancel", s.cancelSocialIntent)
	mux.HandleFunc("GET /v1/social-intents", s.listVisibleSocialIntents)
	mux.HandleFunc("GET /v1/social-intents/{intentID}", s.getVisibleSocialIntent)
	call := func(method, path string, who int, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		var result map[string]any
		if err := json.Unmarshal(rw.Body.Bytes(), &result); err != nil {
			t.Fatalf("HTTP response is not JSON: %d %s", rw.Code, rw.Body.String())
		}
		if method == "POST" && path == "/v1/me/social-intents" && rw.Code == http.StatusCreated {
			item, ok := result["data"].(map[string]any)
			if !ok || item["creatorAccountId"] != ids[who] {
				t.Fatalf("created Intent ownership: %+v", result)
			}
			id, ok := item["id"].(string)
			if !ok || id == "" || ownedIntentIDs[id] {
				t.Fatalf("created Intent identifier: %+v", result)
			}
			ownedIntentIDs[id] = true
		}
		return rw.Code, result
	}
	// Assert an exact set within our fixture, while allowing unrelated lawful
	// public results from other packages that share the disposable database.
	assertVisible := func(who int, expectedIDs ...string) {
		t.Helper()
		code, result := call("GET", "/v1/social-intents", who, "")
		items, ok := result["data"].([]any)
		if code != http.StatusOK || !ok {
			t.Fatalf("visible Intent list: %d %+v", code, result)
		}
		want := map[string]bool{}
		for _, id := range expectedIDs {
			if !ownedIntentIDs[id] || want[id] {
				t.Fatalf("invalid expected fixture Intent: %s", id)
			}
			want[id] = true
		}
		seen := map[string]bool{}
		for _, value := range items {
			item, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("invalid visible Intent: %+v", value)
			}
			id, ok := item["id"].(string)
			if !ok || id == "" || seen[id] {
				t.Fatalf("invalid or duplicate visible Intent identifier: %+v", item)
			}
			seen[id] = true
			if ownedIntentIDs[id] && !want[id] {
				t.Fatalf("viewer %d saw protected fixture Intent %s: %+v", who, id, item)
			}
			if item["creatorAccountId"] == ids[0] && !ownedIntentIDs[id] {
				t.Fatalf("unexpected owner Intent outside fixture IDs: %+v", item)
			}
		}
		for id := range want {
			if !seen[id] {
				t.Fatalf("viewer %d missed authorized fixture Intent %s: %+v", who, id, result)
			}
		}
	}
	input, err := json.Marshal(map[string]any{
		"type": "FIND_COMPANION", "title": "周末一起打羽毛球",
		"constraints": map[string]any{"category": "badminton", "minParticipants": 2},
		"audience":    "PRIVATE", "modality": "ONLINE",
		"expiresAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := call("POST", "/v1/me/social-intents", -1, string(input)); code != 401 {
		t.Fatalf("anonymous create: %d", code)
	}
	if code, _ := call("POST", "/v1/me/social-intents", 2, string(input)); code != 403 {
		t.Fatalf("organization create: %d", code)
	}
	if code, _ := call("POST", "/v1/me/social-intents", 0, `{"type":"UNKNOWN"}`); code != 400 {
		t.Fatalf("invalid contract: %d", code)
	}
	code, result := call("POST", "/v1/me/social-intents", 0, string(input))
	if code != 201 {
		t.Fatalf("create draft: %d %+v", code, result)
	}
	item := result["data"].(map[string]any)
	id := item["id"].(string)
	if item["creatorAccountId"] != ids[0] || item["status"] != "DRAFT" ||
		item["modality"] != "ONLINE" || item["audience"] != "PRIVATE" ||
		item["contextId"] != nil || item["createdAt"] == nil || item["updatedAt"] == nil {
		t.Fatalf("draft contract: %+v", item)
	}
	if code, _ := call("GET", "/v1/me/social-intents/"+id, 1, ""); code != 404 {
		t.Fatalf("outsider read: %d", code)
	}
	if code, result := call("GET", "/v1/me/social-intents", 1, ""); code != 200 || len(result["data"].([]any)) != 0 {
		t.Fatalf("outsider list: %d %+v", code, result)
	}
	fresh, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	freshStore := postgres.New(fresh, false)
	if own, err := freshStore.GetOwnSocialIntent(ctx, ids[0], id); err != nil || own.ID != id || own.Title != "周末一起打羽毛球" {
		t.Fatalf("draft lost after new DB pool: %+v %v", own, err)
	}
	if code, result := call("GET", "/v1/me/social-intents", 0, ""); code != 200 || len(result["data"].([]any)) != 1 ||
		result["data"].([]any)[0].(map[string]any)["id"] != id {
		t.Fatalf("owner list: %d %+v", code, result)
	}
	if code, _ := call("POST", "/v1/me/social-intents/"+id+"/activate", 1, `{"confirmed":true}`); code != 404 {
		t.Fatalf("outsider activated draft: %d", code)
	}
	if code, _ := call("POST", "/v1/me/social-intents/"+id+"/activate", 0, `{"confirmed":false}`); code != 400 {
		t.Fatalf("unconfirmed activation: %d", code)
	}
	if code, result := call("POST", "/v1/me/social-intents/"+id+"/activate", 0, `{"confirmed":true}`); code != 200 || result["data"].(map[string]any)["status"] != "ACTIVE" {
		t.Fatalf("explicit activation: %d %+v", code, result)
	}
	if code, _ := call("POST", "/v1/me/social-intents/"+id+"/activate", 0, `{"confirmed":true}`); code != 409 {
		t.Fatalf("duplicate activation: %d", code)
	}
	if code, result := call("POST", "/v1/me/social-intents/"+id+"/cancel", 0, ""); code != 200 || result["data"].(map[string]any)["status"] != "CANCELLED" {
		t.Fatalf("cancel active Intent: %d %+v", code, result)
	}
	if code, _ := call("POST", "/v1/me/social-intents/"+id+"/cancel", 0, ""); code != 409 {
		t.Fatalf("duplicate cancellation: %d", code)
	}
	var lifecycleAudit int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1
		AND resource_type='social_intent' AND resource_id=$2 AND action IN ('activate','cancel')`, ids[0], id).Scan(&lifecycleAudit); err != nil || lifecycleAudit != 2 {
		t.Fatalf("lifecycle audit: %d %v", lifecycleAudit, err)
	}
	for _, tc := range []struct {
		name        string
		modality    string
		constraints map[string]any
		want        int
	}{
		{"physical area", "IN_PERSON", map[string]any{"areaLabel": "Aberdeen centre"}, 201},
		{"physical missing area", "IN_PERSON", map[string]any{}, 400},
		{"online rejects area", "ONLINE", map[string]any{"areaLabel": "Aberdeen centre"}, 400},
		{"hybrid both", "HYBRID", map[string]any{"areaLabel": "Aberdeen", "onlinePlatform": "video call"}, 201},
		{"hybrid missing platform", "HYBRID", map[string]any{"areaLabel": "Aberdeen"}, 400},
	} {
		body, err := json.Marshal(map[string]any{
			"type": "FIND_COMPANION", "title": tc.name,
			"constraints": tc.constraints, "audience": "PRIVATE",
			"modality":  tc.modality,
			"expiresAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
		if err != nil {
			t.Fatal(err)
		}
		if code, data := call("POST", "/v1/me/social-intents", 0, string(body)); code != tc.want {
			t.Fatalf("%s: %d %+v", tc.name, code, data)
		}
	}
	code, expiryResult := call("POST", "/v1/me/social-intents", 0, string(input))
	if code != 201 {
		t.Fatalf("expiring draft create: %d %+v", code, expiryResult)
	}
	expiringID := expiryResult["data"].(map[string]any)["id"].(string)
	if _, err := pool.Exec(ctx, `UPDATE social_intents SET
		created_at=now()-interval '2 days',expires_at=now()-interval '1 day'
		WHERE id=$1`, expiringID); err != nil {
		t.Fatal(err)
	}
	if code, result := call("GET", "/v1/me/social-intents/"+expiringID, 0, ""); code != 200 || result["data"].(map[string]any)["status"] != "EXPIRED" {
		t.Fatalf("read-time expiry: %d %+v", code, result)
	}
	if code, _ := call("POST", "/v1/me/social-intents/"+expiringID+"/activate", 0, `{"confirmed":true}`); code != 409 {
		t.Fatalf("expired Intent activated: %d", code)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO social_intents(creator_account_id,intent_type,title,audience,modality,expires_at)
		VALUES($1,'OTHER','invalid owner','PRIVATE','ONLINE',now()+interval '1 day')`, ids[2]); err == nil {
		t.Fatal("database allowed organization as social intent creator")
	}
	var audienceSchema bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.social_intent_audience_targets') IS NOT NULL`).Scan(&audienceSchema); err != nil {
		t.Fatal(err)
	}
	if audienceSchema {
		assertVisible(-1)
		if code, _ := call("GET", "/v1/social-intents/"+id, -1, ""); code != 404 {
			t.Fatalf("anonymous detail leaked draft: %d", code)
		}
		publicInput, err := json.Marshal(map[string]any{
			"type": "FIND_COMPANION", "title": "Synthetic visible Intent",
			"constraints": map[string]any{}, "audience": "PUBLIC", "modality": "ONLINE",
			"expiresAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
		if err != nil {
			t.Fatal(err)
		}
		code, publicResult := call("POST", "/v1/me/social-intents", 0, string(publicInput))
		if code != 201 {
			t.Fatalf("public target draft: %d %+v", code, publicResult)
		}
		publicID := publicResult["data"].(map[string]any)["id"].(string)
		if code, _ := call("POST", "/v1/me/social-intents/"+publicID+"/activate", 0, `{"confirmed":true}`); code != 409 {
			t.Fatalf("private Profile activated public Intent: %d", code)
		}
		if _, err := pool.Exec(ctx, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, ids[0]); err != nil {
			t.Fatal(err)
		}
		if code, result := call("POST", "/v1/me/social-intents/"+publicID+"/activate", 0, `{"confirmed":true}`); code != 200 || result["data"].(map[string]any)["status"] != "ACTIVE" {
			t.Fatalf("activate public Intent: %d %+v", code, result)
		}
		assertVisible(-1, publicID)
		if code, result := call("GET", "/v1/social-intents/"+publicID, -1, ""); code != 200 ||
			result["data"].(map[string]any)["id"] != publicID {
			t.Fatalf("anonymous published detail: %d", code)
		}
		createdCommunity, err := store.CreateSocialCommunity(ctx, ids[0], community.SocialInput{
			Name: "Owned HTTP Intent Community", Summary: "Synthetic authorization fixture",
			Visibility: "public", JoinPolicy: "open",
		})
		if err != nil {
			t.Fatal(err)
		}
		communityID = createdCommunity.ID
		if member, err := store.JoinSocialCommunity(ctx, ids[3], communityID); err != nil || member.Status != "active" {
			t.Fatalf("explicit owned Community membership: %+v %v", member, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, ids[1]); err != nil {
			t.Fatal(err)
		}
		request, err := store.CreateFriendRequest(ctx, ids[0], ids[1], "Synthetic confirmed connection")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.DecideRequest(ctx, ids[1], request.ID, "accept"); err != nil {
			t.Fatal(err)
		}
		activeScopes := map[string]string{}
		for _, audience := range []string{"PRIVATE", "FRIENDS", "COMMUNITY"} {
			input := map[string]any{
				"type": "FIND_COMPANION", "title": "Owned HTTP " + audience + " Intent",
				"constraints": map[string]any{}, "audience": audience, "modality": "ONLINE",
				"expiresAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
			}
			if audience == "COMMUNITY" {
				input["communityId"] = communityID
			}
			body, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			code, result := call("POST", "/v1/me/social-intents", 0, string(body))
			if code != 201 {
				t.Fatalf("create %s scope: %d %+v", audience, code, result)
			}
			intentID := result["data"].(map[string]any)["id"].(string)
			activeScopes[audience] = intentID
			if code, result := call("POST", "/v1/me/social-intents/"+intentID+"/activate", 0, `{"confirmed":true}`); code != 200 ||
				result["data"].(map[string]any)["status"] != "ACTIVE" {
				t.Fatalf("activate %s scope: %d %+v", audience, code, result)
			}
		}
		assertVisible(-1, publicID)
		assertVisible(1, publicID, activeScopes["FRIENDS"])
		assertVisible(3, publicID, activeScopes["COMMUNITY"])
		assertVisible(4, publicID)
		for _, tc := range []struct {
			who      int
			intentID string
			want     int
		}{
			{-1, activeScopes["PRIVATE"], 404}, {-1, activeScopes["FRIENDS"], 404}, {-1, activeScopes["COMMUNITY"], 404},
			{1, activeScopes["PRIVATE"], 404}, {1, activeScopes["FRIENDS"], 200}, {1, activeScopes["COMMUNITY"], 404},
			{3, activeScopes["PRIVATE"], 404}, {3, activeScopes["FRIENDS"], 404}, {3, activeScopes["COMMUNITY"], 200},
			{4, activeScopes["PRIVATE"], 404}, {4, activeScopes["FRIENDS"], 404}, {4, activeScopes["COMMUNITY"], 404},
			{0, activeScopes["PRIVATE"], 404},
		} {
			code, result := call("GET", "/v1/social-intents/"+tc.intentID, tc.who, "")
			if code != tc.want || (code == 200 && result["data"].(map[string]any)["id"] != tc.intentID) {
				t.Fatalf("exact scope detail viewer=%d Intent=%s: %d %+v", tc.who, tc.intentID, code, result)
			}
		}
		if code, result := call("GET", "/v1/me/social-intents/"+activeScopes["PRIVATE"], 0, ""); code != 200 ||
			result["data"].(map[string]any)["id"] != activeScopes["PRIVATE"] ||
			result["data"].(map[string]any)["audience"] != "PRIVATE" {
			t.Fatalf("private active Intent lost owner-only access: %d %+v", code, result)
		}
		if err := store.BlockAccount(ctx, ids[0], ids[1]); err != nil {
			t.Fatal(err)
		}
		assertVisible(1)
		for _, intentID := range []string{publicID, activeScopes["PRIVATE"], activeScopes["FRIENDS"], activeScopes["COMMUNITY"]} {
			if code, result := call("GET", "/v1/social-intents/"+intentID, 1, ""); code != 404 {
				t.Fatalf("blocked viewer read own fixture Intent %s: %d %+v", intentID, code, result)
			}
		}
		if _, err := pool.Exec(ctx, `UPDATE social_intents SET
			created_at=now()-interval '2 days',expires_at=now()-interval '1 day'
			WHERE id=$1`, publicID); err != nil {
			t.Fatal(err)
		}
		assertVisible(-1)
		if code, result := call("GET", "/v1/social-intents/"+publicID, -1, ""); code != 404 {
			t.Fatalf("expired public Intent detail leaked: %d %+v", code, result)
		}
		if code, result := call("GET", "/v1/me/social-intents/"+publicID, 0, ""); code != 200 || result["data"].(map[string]any)["status"] != "EXPIRED" {
			t.Fatalf("expired active Intent not projected: %d %+v", code, result)
		}
	}
}
