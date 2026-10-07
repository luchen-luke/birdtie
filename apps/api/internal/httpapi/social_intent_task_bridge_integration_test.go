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

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSocialIntentTaskBridgeIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for bridge integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.social_intents') IS NOT NULL
		AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='social_intents'
		AND column_name='source_agent_task_id')`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("038_social_intent_agent_origin not applied")
	}
	ids := make([]string, 3)
	tokens := make([]string, 3)
	for i := range ids {
		kind := "person"
		if i == 2 {
			kind = "organization"
		}
		if err := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if kind == "person" {
			if _, err := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name)
				VALUES($1,'Synthetic bridge tester')`, ids[i]); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO agents(agent_type,principal_account_id)
				VALUES('personal',$1) ON CONFLICT DO NOTHING`, ids[i]); err != nil {
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

	defer func() {
		// All three identities are unique owned fixtures; never delete seed actors.
		for _, q := range []string{
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, err := pool.Exec(ctx, q, ids); err != nil {
				t.Errorf("owned bridge cleanup: %v", err)
			}
		}
		var residue int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+(SELECT count(*) FROM audit_events WHERE actor_account_id=ANY($1::uuid[]))+(SELECT count(*) FROM social_intents WHERE creator_account_id=ANY($1::uuid[]))+(SELECT count(*) FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[]))`, ids).Scan(&residue); err != nil || residue != 0 {
			t.Errorf("owned bridge residue=%d error=%v", residue, err)
		}
	}()

	insertTask := func(owner, intent, status string) string {
		t.Helper()
		var id string
		err := pool.QueryRow(ctx, `INSERT INTO agent_tasks
			(owner_account_id,acting_user_account_id,principal_type,city_id,city_context_id,query,intent,status)
			VALUES($1,$1,'person','aberdeen-gb','aberdeen-gb','帮我找周末的羽毛球',$2,$3) RETURNING id`, owner, intent, status).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	good := insertTask(ids[0], "FIND_ACTIVITY", "COMPLETED")
	other := insertTask(ids[1], "FIND_ACTIVITY", "COMPLETED")
	unsupported := insertTask(ids[0], "FIND_PLACE", "COMPLETED")
	refinement := insertTask(ids[0], "REFINE_RESULTS", "COMPLETED")
	areaSearch := insertTask(ids[0], "AREA_DISCOVERY", "COMPLETED")
	active := insertTask(ids[0], "FIND_ACTIVITY", "ACTIVE")
	store := postgres.New(pool, false)
	s := &server{access: store, agent: store, socialIntents: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/me/agent-tasks/{taskID}/social-intent-drafts", s.createSocialIntentDraftFromTask)
	call := func(taskID string, who int, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest("POST", "/v1/me/agent-tasks/"+taskID+"/social-intent-drafts", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var result map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return w.Code, result
	}
	draft := map[string]any{
		"type": "FIND_ACTIVITY", "title": "我想周末打羽毛球", "audience": "PRIVATE",
		"modality": "IN_PERSON", "constraints": map[string]any{"areaLabel": "市中心", "category": "badminton"},
		"expiresAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	encode := func(confirmed bool) string {
		b, _ := json.Marshal(map[string]any{"confirmed": confirmed, "draft": draft})
		return string(b)
	}
	for _, tc := range []struct {
		name, task string
		who, want  int
		body       string
	}{
		{"anonymous", good, -1, 401, encode(true)},
		{"organization", good, 2, 403, encode(true)},
		{"unconfirmed", good, 0, 400, encode(false)},
		{"foreign", other, 0, 404, encode(true)},
		{"non socializable", unsupported, 0, 404, encode(true)},
		{"refinement", refinement, 0, 404, encode(true)},
		{"area search", areaSearch, 0, 404, encode(true)},
		{"unfinished", active, 0, 404, encode(true)},
	} {
		if code, _ := call(tc.task, tc.who, tc.body); code != tc.want {
			t.Fatalf("%s status=%d want=%d", tc.name, code, tc.want)
		}
	}
	code, response := call(good, 0, encode(true))
	if code != 201 {
		t.Fatalf("create: %d %+v", code, response)
	}
	item := response["data"].(map[string]any)
	if item["status"] != "DRAFT" || item["audience"] != "PRIVATE" || item["title"] != draft["title"] {
		t.Fatalf("bridge draft: %+v", item)
	}
	var source string
	if err := pool.QueryRow(ctx, `SELECT source_agent_task_id FROM social_intents WHERE id=$1`, item["id"]).Scan(&source); err != nil || source != good {
		t.Fatalf("source provenance: %s %v", source, err)
	}
	if code, _ := call(good, 0, encode(true)); code != 409 {
		t.Fatalf("duplicate: %d", code)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM social_intents WHERE source_agent_task_id=$1`, good).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate persisted: %d %v", count, err)
	}
	// A search task and its conversation remain intact after the explicit save.
	var originalIntent, originalStatus string
	if err := pool.QueryRow(ctx, `SELECT intent,status FROM agent_tasks WHERE id=$1`, good).Scan(&originalIntent, &originalStatus); err != nil || originalIntent != "FIND_ACTIVITY" || originalStatus != "COMPLETED" {
		t.Fatalf("search task mutated: %s/%s %v", originalIntent, originalStatus, err)
	}
}
