package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommunitySocialHTTPIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ids, cleanupPeople := ownedSocialPeopleFixture(t, ctx, pool, 3)
	defer cleanupPeople()
	store := postgres.New(pool, false)
	s := &server{access: store, socialCommunities: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/communities", s.createSocialCommunity)
	mux.HandleFunc("GET /v1/communities", s.discoverSocialCommunities)
	mux.HandleFunc("GET /v1/communities/{communityID}", s.getSocialCommunity)
	mux.HandleFunc("PATCH /v1/communities/{communityID}", s.updateSocialCommunity)
	mux.HandleFunc("POST /v1/communities/{communityID}/join", s.joinSocialCommunity)
	mux.HandleFunc("GET /v1/communities/{communityID}/requests", s.listSocialRequests)
	mux.HandleFunc("POST /v1/communities/{communityID}/requests/{requestID}/approve", s.approveSocialRequest)
	mux.HandleFunc("POST /v1/communities/{communityID}/leave", s.leaveSocialCommunity)
	tokens := make([]string, 3)
	for i, id := range ids {
		tok, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = tok
		_, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
		    VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, id, digest[:])
		if e != nil {
			t.Fatal(e)
		}
		defer ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM sessions WHERE token_sha256=$1`, digest[:])
	}
	call := func(method, path string, who int, body string) (int, map[string]any) {
		snapshot := ""
		if who >= 0 && (method == "PATCH" || strings.HasSuffix(path, "/approve")) {
			pre := httptest.NewRequest(method, path, bytes.NewBufferString(body))
			pre.Header.Set("Authorization", "Bearer "+tokens[who])
			pre.Header.Set("Content-Type", "application/json")
			pre.Header.Set("X-Birdtie-Community-Preview", "1")
			pw := httptest.NewRecorder()
			mux.ServeHTTP(pw, pre)
			var data map[string]any
			_ = json.Unmarshal(pw.Body.Bytes(), &data)
			if pw.Code != 200 {
				return pw.Code, data
			}
			approval, ok := data["data"].(map[string]any)
			if !ok {
				t.Fatal("actual native approval missing")
			}
			snapshot, _ = approval["snapshot"].(string)
			if snapshot == "" {
				t.Fatal("actual native approval token missing")
			}
		}
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		if snapshot != "" {
			req.Header.Set("X-Birdtie-Community-Snapshot", snapshot)
		}
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		var data map[string]any
		_ = json.Unmarshal(rw.Body.Bytes(), &data)
		return rw.Code, data
	}
	if code, _ := call("POST", "/v1/communities", -1, `{"name":"Synthetic HTTP Community"}`); code != 401 {
		t.Fatalf("anonymous create: %d", code)
	}
	code, data := call("POST", "/v1/communities", 0, `{"name":"Synthetic HTTP Community","description":"integration","visibility":"private","joinPolicy":"request"}`)
	if code != 201 {
		t.Fatalf("create: %d %+v", code, data)
	}
	item := data["data"].(map[string]any)
	id := item["id"].(string)
	defer func() {
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE resource_type='community' AND resource_id=$1`, id)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM communities WHERE id=$1`, id)
	}()
	if code, _ := call("GET", "/v1/communities/"+id, 2, ""); code != 200 {
		t.Fatalf("private basic detail: %d", code)
	}
	code, data = call("POST", "/v1/communities/"+id+"/join", 1, "")
	if code != 200 {
		t.Fatalf("request join: %d %+v", code, data)
	}
	requestID := data["data"].(map[string]any)["id"].(string)
	if code, _ := call("GET", "/v1/communities/"+id+"/requests", 1, ""); code != 403 {
		t.Fatalf("member requests: %d", code)
	}
	if code, _ := call("POST", "/v1/communities/"+id+"/requests/"+requestID+"/approve", 2, ""); code != 403 {
		t.Fatalf("outsider approve: %d", code)
	}
	if code, _ := call("POST", "/v1/communities/"+id+"/requests/"+requestID+"/approve", 0, ""); code != 200 {
		t.Fatalf("owner approve: %d", code)
	}
	if code, _ := call("POST", "/v1/communities/"+id+"/join", 1, ""); code != 409 {
		t.Fatalf("duplicate join: %d", code)
	}
	if code, _ := call("POST", "/v1/communities/"+id+"/leave", 1, ""); code != 204 {
		t.Fatalf("leave: %d", code)
	}
	code, data = call("PATCH", "/v1/communities/"+id, 0,
		`{"name":"Synthetic HTTP Community","description":"integration","visibility":"hidden","joinPolicy":"request"}`)
	if code != 200 {
		t.Fatalf("hide: %d %+v", code, data)
	}
	if code, _ := call("GET", "/v1/communities/"+id, 2, ""); code != 404 {
		t.Fatalf("hidden outsider detail: %d", code)
	}
	if code, _ := call("GET", "/v1/communities/"+id, 1, ""); code != 404 {
		t.Fatalf("hidden former member detail: %d", code)
	}
	if code, _ := call("GET", "/v1/communities/"+id, 0, ""); code != 200 {
		t.Fatalf("hidden owner detail: %d", code)
	}
	code, data = call("GET", "/v1/communities", 2, "")
	if code != 200 {
		t.Fatalf("discovery: %d %+v", code, data)
	}
	for _, raw := range data["data"].([]any) {
		if raw.(map[string]any)["id"] == id {
			t.Fatal("hidden community leaked through discovery")
		}
	}
}
