package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPersonContextLifecycleIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	people := make([]string, 2)
	tokens := make([]string, 2)
	for i := range people {
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&people[i]); err != nil {
			t.Fatal(err)
		}
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
			VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, people[i], digest[:]); e != nil {
			t.Fatal(e)
		}
	}
	var cityIDs []string
	var ownedOtherContextIDs []string
	institutionKey := "合成大学 context-http-" + people[0]
	onlineKey := "合成线上羽毛球社群 context-http-" + people[0]
	defer func() {
		cleanup := func(query string, args ...any) {
			if _, cleanupErr := pool.Exec(ctx, query, args...); cleanupErr != nil {
				t.Error("owned Person Context fixture cleanup failed")
			}
		}
		cleanup(`DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`, people)
		// The non-City nodes are created by this test's unique source keys, not
		// shared development sources. Track exact returned IDs, including a
		// redeclaration, so successful tests cannot leave global context nodes.
		cleanup(`DELETE FROM contexts WHERE id=ANY($1::uuid[])`, ownedOtherContextIDs)
		for _, id := range cityIDs {
			cleanup(`DELETE FROM contexts WHERE city_id=$1`, id)
			cleanup(`DELETE FROM city_contexts WHERE city_id=$1`, id)
			cleanup(`DELETE FROM cities WHERE id=$1`, id)
		}
		cleanup(`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, people)
		cleanup(`DELETE FROM accounts WHERE id=ANY($1::uuid[])`, people)
		var residue int
		if pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM contexts WHERE id=ANY($1::uuid[]))+
			(SELECT count(*) FROM person_contexts WHERE person_account_id=ANY($2::uuid[]))+
			(SELECT count(*) FROM accounts WHERE id=ANY($2::uuid[]))`, ownedOtherContextIDs, people).Scan(&residue) != nil || residue != 0 {
			t.Error("owned Person Context fixture residue")
		}
	}()
	for _, name := range []string{"Synthetic Edinburgh", "Synthetic London"} {
		var id string
		if err = pool.QueryRow(ctx, `INSERT INTO cities(id,name,region,country_code,time_zone,
			publication_status,source_label,source_ref,maintainer_label)
			VALUES('context-fixture-'||gen_random_uuid()::text,$1,'Test','GB','Europe/London',
			'published','Synthetic','test:context','Test') RETURNING id`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		cityIDs = append(cityIDs, id)
		if _, err = pool.Exec(ctx, `INSERT INTO city_contexts(city_id) VALUES($1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	store := postgres.New(pool, false)
	s := &server{access: store, contextDeclarations: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/contexts", s.listOwnContexts)
	mux.HandleFunc("POST /v1/me/contexts", s.declareOwnContext)
	mux.HandleFunc("DELETE /v1/me/contexts/{contextID}/{relation}", s.removeOwnContext)
	call := func(method, path string, who int, body any) (int, map[string]json.RawMessage) {
		t.Helper()
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		var decoded map[string]json.RawMessage
		_ = json.Unmarshal(rw.Body.Bytes(), &decoded)
		return rw.Code, decoded
	}
	if code, _ := call("GET", "/v1/me/contexts", -1, nil); code != 401 {
		t.Fatalf("anonymous context read: %d", code)
	}
	if code, _ := call("POST", "/v1/me/contexts", -1, contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: "aberdeen-gb", Relation: "current"}); code != 401 {
		t.Fatalf("anonymous context write: %d", code)
	}
	fixtures := []contextgraph.DeclarationInput{
		{Type: contextgraph.City, SourceKey: "aberdeen-gb", Relation: "current"},
		{Type: contextgraph.City, SourceKey: cityIDs[0], Relation: "past"},
		{Type: contextgraph.Institution, SourceKey: institutionKey, Relation: "past"},
		{Type: contextgraph.City, SourceKey: cityIDs[1], Relation: "destination"},
		{Type: contextgraph.Online, SourceKey: onlineKey, Relation: "interest"},
	}
	var onlineID string
	for _, fixture := range fixtures {
		code, raw := call("POST", "/v1/me/contexts", 0, fixture)
		if code != 201 {
			t.Fatalf("declaration %+v: %d %+v", fixture, code, raw)
		}
		var item contextgraph.Declaration
		if err := json.Unmarshal(raw["data"], &item); err != nil || item.Visibility != "private" || item.SourceKey != fixture.SourceKey {
			t.Fatalf("declaration response %+v %v", item, err)
		}
		if fixture.Type == contextgraph.Online {
			onlineID = item.ContextID
		}
		if fixture.Type == contextgraph.Institution || fixture.Type == contextgraph.Online {
			ownedOtherContextIDs = append(ownedOtherContextIDs, item.ContextID)
		}
	}
	if code, _ := call("POST", "/v1/me/contexts", 0, contextgraph.DeclarationInput{
		Type: contextgraph.City, SourceKey: cityIDs[1], Relation: "current"}); code != 201 {
		t.Fatalf("replace current city: %d", code)
	}
	code, raw := call("GET", "/v1/me/contexts", 0, nil)
	var own []contextgraph.Declaration
	if code != 200 || json.Unmarshal(raw["data"], &own) != nil || len(own) != 5 {
		t.Fatalf("multi-context read: %d %+v", code, own)
	}
	current := 0
	for _, item := range own {
		if item.Relation == "current" {
			current++
			if item.SourceKey != cityIDs[1] {
				t.Fatalf("old current city retained: %+v", item)
			}
		}
	}
	if current != 1 {
		t.Fatalf("current city count: %d", current)
	}
	if code, other := call("GET", "/v1/me/contexts", 1, nil); code != 200 || string(other["data"]) != "[]" {
		t.Fatalf("other person's declarations leaked: %d %+v", code, other)
	}
	path := "/v1/me/contexts/" + onlineID + "/interest"
	if code, _ := call("DELETE", path, 1, nil); code != 404 {
		t.Fatalf("other person removed context: %d", code)
	}
	if code, _ := call("DELETE", path, 0, nil); code != 204 {
		t.Fatalf("owner removed online context: %d", code)
	}
	if code, _ := call("POST", "/v1/me/contexts", 0, contextgraph.DeclarationInput{
		Type: contextgraph.Online, SourceKey: onlineKey, Relation: "interest"}); code != 201 {
		t.Fatalf("redeclare online context: %d", code)
	}
	var accountCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[])`, people).Scan(&accountCount); err != nil || accountCount != 2 {
		t.Fatalf("context duplicated identity: %d %v", accountCount, err)
	}
}
