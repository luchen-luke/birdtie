package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	cg "github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func interestHTTPOwnedDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("actual community interest requires disposable native DB")
	}
	uri, e := url.Parse(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	adminURI := *uri
	adminURI.Path = "/postgres"
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, adminURI.String())
	if e != nil {
		t.Fatal(e)
	}
	var random [12]byte
	if _, e = rand.Read(random[:]); e != nil {
		t.Fatal(e)
	}
	name := "birdtie_owned_interest_http_" + hex.EncodeToString(random[:])
	q := pgx.Identifier{name}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+q); e != nil {
		t.Fatal(e)
	}
	uri.Path = "/" + name
	pool, e := pgxpool.New(ctx, uri.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		if _, e := admin.Exec(ctx, "DROP DATABASE "+q); e != nil {
			t.Error("owned database DROP", e)
		}
		admin.Close()
	})
	fs, e := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if e != nil {
		t.Fatal(e)
	}
	sort.Strings(fs)
	for _, p := range fs {
		if strings.HasSuffix(p, ".down.sql") {
			continue
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(p, e)
		}
	}
	t.Log("owned native HTTP database", name)
	return pool
}
func TestCommunityInterestHTTPNativeActualRegisteredHumanLifecycle(t *testing.T) {
	pool := interestHTTPOwnedDB(t)
	ctx, c := context.WithTimeout(context.Background(), 45*time.Second)
	defer c()
	store := postgres.New(pool, false)
	owners, tokens := []string{}, []string{}
	for n := 0; n < 2; n++ {
		var id string
		if e := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, id); e != nil {
			t.Fatal(e)
		}
		token, d, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '2 hours',clock_timestamp()+interval '1 hour')`, id, d[:]); e != nil {
			t.Fatal(e)
		}
		owners = append(owners, id)
		tokens = append(tokens, token)
	}
	g, e := store.CreateSocialCommunity(ctx, owners[0], community.SocialInput{Name: "原生HTTP公开兴趣社群", Summary: "本地合成无消息", Visibility: "public", JoinPolicy: "request"})
	if e != nil {
		t.Fatal(e)
	}
	h := New(store, store, nil, nil, nil, store, nil, store, nil, nil, nil, nil, false, nil, nil, nil)
	call := func(method, path, body, token string, want int, workspace bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if workspace {
			r.Header.Set("X-Birdtie-Organization-Workspace", owners[0])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s actual status %d want%d %s", method, path, w.Code, want, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache")
		}
		return w
	}
	read := func(w *httptest.ResponseRecorder) cg.CommunityInterestView {
		var v struct {
			Data cg.CommunityInterestView `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("wire")
		}
		return v.Data
	}
	preview := func(op string) cg.CommunityInterestPreview {
		w := call("POST", "/v1/me/community-interests/preview", `{"communityId":"`+g.ID+`","operation":"`+op+`"}`, tokens[1], 200, false)
		var v struct {
			Data cg.CommunityInterestPreview `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("preview")
		}
		return v.Data
	}
	approve := func(p cg.CommunityInterestPreview, want int) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]string{"preview": p.Preview})
		return call("POST", "/v1/me/community-interests/approve", string(raw), tokens[1], want, false)
	}
	t.Run("current_read_preview_cancel_no_domain_write", func(t *testing.T) {
		var before, after int
		sql := `SELECT (SELECT count(*) FROM contexts WHERE community_id=$1)+(SELECT count(*) FROM person_contexts WHERE person_account_id=$2)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$2)`
		pool.QueryRow(ctx, sql, g.ID, owners[1]).Scan(&before)
		options := read(call("GET", "/v1/me/community-interests/options", "", tokens[1], 200, false))
		if len(options.Options) != 1 || options.Limit != 100 || options.Truncated {
			t.Fatal(options)
		}
		p := preview("PRIVATE")
		if p.State != "ABSENT" || p.TargetState != "PRIVATE" || p.Relation != "interest" {
			t.Fatal(p)
		}
		call("GET", "/v1/me/community-interests", "", tokens[1], 200, false)
		pool.QueryRow(ctx, sql, g.ID, owners[1]).Scan(&after)
		if before != after {
			t.Fatal("GET/prepare wrote")
		}
	})
	t.Run("concrete_nonmember_approve_repeat409_and_original_delete_denial", func(t *testing.T) {
		p := preview("PUBLIC")
		v := read(approve(p, 200))
		if v.OwnerID != owners[1] || len(v.Records) != 1 || v.Records[0].State != "PUBLIC" || v.ModelAccess || v.SendAllowed || v.MembershipGranted {
			t.Fatal(v)
		}
		approve(p, 409)
		call("DELETE", "/v1/me/contexts/"+v.Records[0].ContextID+"/interest", "", tokens[1], 404, false)
		if len(read(call("GET", "/v1/me/community-interests", "", tokens[1], 200, false)).Records) != 1 {
			t.Fatal("legacy deletion succeeded")
		}
	})
	t.Run("strict_person_scope_selectors_and_no_boolean_authority", func(t *testing.T) {
		call("GET", "/v1/me/community-interests?ownerId="+owners[1], "", tokens[1], 400, false)
		call("GET", "/v1/me/community-interests", "{}", tokens[1], 400, false)
		call("GET", "/v1/me/community-interests", "", tokens[1], 403, true)
		call("POST", "/v1/me/community-interests/preview", `{"communityId":"`+g.ID+`","operation":"PRIVATE","operation":"PUBLIC"}`, tokens[1], 400, false)
		call("POST", "/v1/me/community-interests/approve", `{"confirmed":true}`, tokens[1], 400, false)
		p := preview("PRIVATE")
		raw, _ := json.Marshal(map[string]string{"preview": p.Preview})
		call("POST", "/v1/me/community-interests/approve", string(raw), tokens[0], 403, false)
	})
	t.Run("hidden_own_neutral_revoke_and_effects_absent", func(t *testing.T) {
		if _, e := pool.Exec(ctx, `UPDATE communities SET visibility='hidden',name='PRIVATE_SECRET_COMMUNITY_NAME' WHERE id=$1`, g.ID); e != nil {
			t.Fatal(e)
		}
		w := call("GET", "/v1/me/community-interests", "", tokens[1], 200, false)
		if bytes.Contains(w.Body.Bytes(), []byte("PRIVATE_SECRET")) || read(w).Records[0].SourceAvailable {
			t.Fatal("hidden name leak")
		}
		approve(preview("DELETE"), 200)
		var effects int
		if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM community_memberships WHERE user_account_id=$1)+(SELECT count(*) FROM consent_grants WHERE owner_account_id=$1)+(SELECT count(*) FROM agent_memories WHERE owner_id=$1)`, owners[1]).Scan(&effects); e != nil || effects != 0 {
			t.Fatal("side effects", e, effects)
		}
	})
}
