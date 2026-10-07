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

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Native, owned local fixtures: no Agent, PrivateProfile, IdP or real merchant.
type businessConsoleHTTPFixture struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	handler     http.Handler
	ids, tokens []string
	business    string
}

func businessConsoleHTTPNew(t *testing.T) *businessConsoleHTTPFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Requires explicitly disposable native database")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &businessConsoleHTTPFixture{ctx: ctx, pool: pool}
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		var principal string
		if e := pool.QueryRow(clean, `SELECT account_id FROM businesses WHERE id=$1`, f.business).Scan(&principal); e == nil {
			f.ids = append(f.ids, principal)
		}
		for _, table := range []string{"business_console_audit_events", "business_console_membership_controls", "business_console_venue_facts", "business_console_profiles", "business_claim_controls", "business_review_grants", "business_venue_relations", "business_memberships"} {
			if _, e := pool.Exec(clean, `DELETE FROM `+table+` WHERE business_id=$1`, f.business); e != nil {
				t.Error(table, e)
			}
		}
		if _, e := pool.Exec(clean, `DELETE FROM businesses WHERE id=$1`, f.business); e != nil {
			t.Error(e)
		}
		for _, q := range []string{`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`} {
			if _, e := pool.Exec(clean, q, f.ids); e != nil {
				t.Error(e)
			}
		}
	})
	for _, kind := range []string{"person", "person", "person", "person", "organization", "business"} {
		var id string
		if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&id); e != nil {
			t.Fatal(e)
		}
		f.ids = append(f.ids, id)
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, id, digest[:]); e != nil {
			t.Fatal(e)
		}
		f.tokens = append(f.tokens, token)
	}
	if e = pool.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&f.business); e != nil {
		t.Fatal(e)
	}
	store := postgres.New(pool, false)
	f.handler = New(store, store, store, store, store, store, store, store, store, store, store, store, false, nil, pool, nil)
	return f
}
func (f *businessConsoleHTTPFixture) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *businessConsoleHTTPFixture) request(t *testing.T, method, path, token, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s want %d got %d: %s", method, path, want, w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing real request correlation")
	}
	t.Logf("LOCAL_SYNTHETIC_ONLY %s %d request=%s", method, w.Code, w.Header().Get("X-Request-ID"))
	return w
}
func businessConsoleHTTPJSON(t *testing.T, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func (f *businessConsoleHTTPFixture) path(suffix string) string {
	return "/v1/me/businesses/" + f.business + suffix
}
func (f *businessConsoleHTTPFixture) claim(t *testing.T) {
	body := businessConsoleHTTPJSON(t, businessconsole.ClaimInput{Name: "HTTP本地合成商家", SourceURL: "https://example.invalid/claim", RightsNote: "明确合成资料经营声明"})
	f.request(t, "PUT", f.path("/claim"), f.tokens[0], body, 200)
	f.exec(t, `INSERT INTO business_review_grants(business_id,reviewer_account_id,permissions,valid_from,valid_until,state,provisioned_by,provision_note) VALUES($1,$2,ARRAY['claim','profile','venue'],clock_timestamp()-interval '1 minute',clock_timestamp()+interval '1 hour','active',$3,'独立受信操作员本地配置，不是生产授权')`, f.business, f.ids[1], f.ids[3])
	review := businessConsoleHTTPJSON(t, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "独立角色核验合成材料"})
	f.request(t, "POST", f.path("/claim/review"), f.tokens[0], review, 403)
	f.request(t, "POST", f.path("/claim/review"), f.tokens[1], review, 200)
}
func TestBusinessConsoleRegisteredNativeLifecycle(t *testing.T) {
	f := businessConsoleHTTPNew(t)
	f.claim(t)
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"owner", f.tokens[0], 200}, {"reviewer", f.tokens[1], 200}, {"outsider", f.tokens[3], 403}, {"organization", f.tokens[4], 403}, {"business", f.tokens[5], 403}, {"anonymous", "", 401}} {
		t.Run(tc.name, func(t *testing.T) { f.request(t, "GET", f.path("/console"), tc.token, "", tc.want) })
	}
	f.request(t, "GET", "/v1/me/businesses", f.tokens[0], "", 200)
	profile := businessconsole.ProfileInput{Facts: businessconsole.ProfileFacts{Name: "本地合成资料", Description: "本人输入待审核", TimeZone: "Europe/London", OpeningHours: []businessconsole.HoursDay{{Day: 1, OpensAt: "22:00", ClosesAt: "02:00", NextDay: true}}, OfficialLinks: []string{"https://example.invalid/official"}}, SourceURL: "https://example.invalid/profile", RightsNote: "明确来源声明", ValidUntil: time.Now().UTC().Add(time.Hour)}
	p := f.request(t, "PUT", f.path("/profile"), f.tokens[0], businessConsoleHTTPJSON(t, profile), 200)
	if !strings.Contains(p.Body.String(), `"state":"pending"`) {
		t.Fatal("unreviewed profile mislabelled", p.Body.String())
	}
	review := businessConsoleHTTPJSON(t, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "独立审核这一具体资料版本"})
	f.request(t, "POST", f.path("/profile/review"), f.tokens[0], review, 403)
	f.request(t, "POST", f.path("/profile/review"), f.tokens[1], review, 200)
	profile.ExpectedVersion = 2
	profile.Facts.Description = "下一版本不能继承批准"
	p = f.request(t, "PUT", f.path("/profile"), f.tokens[0], businessConsoleHTTPJSON(t, profile), 200)
	if !strings.Contains(p.Body.String(), `"state":"pending"`) || !strings.Contains(p.Body.String(), `"version":3`) {
		t.Fatal(p.Body.String())
	}
	f.request(t, "POST", f.path("/profile/review"), f.tokens[1], review, 409)
	member := businessconsole.MemberInput{ExpectedVersion: 0, TargetPersonID: f.ids[2], Action: "grant", Role: "admin"}
	body := businessConsoleHTTPJSON(t, member)
	f.request(t, "PUT", f.path("/members"), f.tokens[0], body, 200)
	f.request(t, "PUT", f.path("/members"), f.tokens[0], body, 200)
	var audits int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1 AND action='member_grant'`, f.business).Scan(&audits); e != nil || audits != 1 {
		t.Fatal("duplicate intent audit", audits, e)
	}
	adminAttempt := member
	adminAttempt.ExpectedVersion = 1
	adminAttempt.TargetPersonID = f.ids[3]
	f.request(t, "PUT", f.path("/members"), f.tokens[2], businessConsoleHTTPJSON(t, adminAttempt), 403)
	member.ExpectedVersion = 1
	member.Action = "transfer_owner"
	member.Role = "owner"
	body = businessConsoleHTTPJSON(t, member)
	f.request(t, "PUT", f.path("/members"), f.tokens[0], body, 200)
	f.request(t, "PUT", f.path("/members"), f.tokens[0], body, 200)
	member.ExpectedVersion = 2
	member.Action = "remove"
	member.Role = ""
	member.TargetPersonID = f.ids[0]
	body = businessConsoleHTTPJSON(t, member)
	formerOwnerAttempt := member
	formerOwnerAttempt.TargetPersonID = f.ids[3]
	f.request(t, "PUT", f.path("/members"), f.tokens[0], businessConsoleHTTPJSON(t, formerOwnerAttempt), 403)
	f.request(t, "PUT", f.path("/members"), f.tokens[2], body, 200)
	f.request(t, "GET", f.path("/console"), f.tokens[0], "", 403)
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.ids[2])
	f.request(t, "GET", f.path("/console"), f.tokens[2], "", 401)
	var agents int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=ANY($1::uuid[])`, f.ids).Scan(&agents); e != nil || agents != 0 {
		t.Fatal("native human management depended on Agent", agents, e)
	}
}
func TestBusinessConsoleRegisteredNativeStrictBoundary(t *testing.T) {
	f := businessConsoleHTTPNew(t)
	f.claim(t)
	for _, tc := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"query", "GET", f.path("/console?ownerId=" + f.ids[0]), "", 400},
		{"get body", "GET", f.path("/console"), "{}", 400},
		{"duplicate", "PUT", f.path("/claim"), `{"expectedVersion":2,"expectedVersion":2}`, 400},
		{"unknown", "PUT", f.path("/members"), `{"expectedVersion":0,"targetPersonId":"` + f.ids[2] + `","action":"grant","role":"admin","confirmed":true}`, 400},
		{"oversize", "PUT", f.path("/claim"), strings.Repeat("x", businessconsole.MaxBodyBytes+1), 400},
	} {
		t.Run(tc.name, func(t *testing.T) { f.request(t, tc.method, tc.path, f.tokens[0], tc.body, tc.want) })
	}
	for _, header := range []string{"X-Birdtie-Organization-Workspace", "X-Birdtie-Business-Workspace"} {
		r := httptest.NewRequest("GET", f.path("/console"), nil)
		r.Header.Set("Authorization", "Bearer "+f.tokens[0])
		r.Header.Set(header, f.ids[0])
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(header, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("PUT", f.path("/claim"), strings.NewReader("{}"))
	r.Header.Set("Authorization", "Bearer "+f.tokens[0])
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code, w.Body.String())
	}
}

// Hook materializes a real current change before the final real Store read.
// It never returns canned permission, state or business data.
type businessConsoleHTTPMaterializationBarrier struct {
	*postgres.Store
	reads       int
	afterEncode func()
}

func (b *businessConsoleHTTPMaterializationBarrier) ReadBusinessConsole(ctx context.Context, a businessconsole.Access) (businessconsole.Console, error) {
	b.reads++
	if b.reads == 2 && b.afterEncode != nil {
		b.afterEncode()
	}
	return b.Store.ReadBusinessConsole(ctx, a)
}
func TestBusinessConsoleRegisteredNativeMaterializedRead(t *testing.T) {
	for _, change := range []string{"session", "membership", "source"} {
		t.Run(change, func(t *testing.T) {
			f := businessConsoleHTTPNew(t)
			f.claim(t)
			store := postgres.New(f.pool, false)
			b := &businessConsoleHTTPMaterializationBarrier{Store: store, afterEncode: func() {
				switch change {
				case "session":
					f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.ids[0])
				case "membership":
					f.exec(t, `UPDATE business_memberships SET status='removed' WHERE business_id=$1 AND user_account_id=$2`, f.business, f.ids[0])
				case "source":
					f.exec(t, `UPDATE businesses SET name='变化后的当前合成商家' WHERE id=$1`, f.business)
				}
			}}
			f.handler = New(b, store, store, store, store, store, store, store, store, store, store, store, false, nil, f.pool, nil)
			// Source material mismatch is withheld as an unknown current
			// outcome (503), not emitted as a stale success payload.
			want := 503
			if change == "session" {
				want = 401
			}
			if change == "membership" {
				want = 403
			}
			w := f.request(t, "GET", f.path("/console"), f.tokens[0], "", want)
			if b.reads != 2 || strings.Contains(w.Body.String(), "HTTP本地合成商家") {
				t.Fatal("stale encoded business released", b.reads, w.Body.String())
			}
		})
	}
}
