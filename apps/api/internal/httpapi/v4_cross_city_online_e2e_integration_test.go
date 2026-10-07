package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	nq "github.com/birdtie/birdtie/apps/api/internal/nowcontextquery"
	oso "github.com/birdtie/birdtie/apps/api/internal/onlinesocialopportunity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type crossCityOnlineFixture struct {
	*privateProfileHTTPDBFixture
	cities [2]string
}

func crossCityOnlineNative(t *testing.T) *crossCityOnlineFixture {
	t.Helper()
	v4PrivacyHTTPDatabase(t) // actual owned database, migrations and original seeds; no skip
	checkCtx, stopCheck := context.WithTimeout(context.Background(), 65*time.Second)
	checkPool, e := pgxpool.New(checkCtx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		stopCheck()
		t.Fatal(e)
	}
	probe := &crossCityOnlineFixture{privateProfileHTTPDBFixture: &privateProfileHTTPDBFixture{t: t, ctx: checkCtx, pool: checkPool}}
	oldPublic := probe.digestPublicTables(true)
	oldTables := probe.publicTableHashes()
	t.Cleanup(func() {
		defer stopCheck()
		defer checkPool.Close()
		if oldPublic != probe.digestPublicTables(true) {
			current := probe.publicTableHashes()
			for name, digest := range oldTables {
				if current[name] != digest {
					t.Log("changed owned cleanup table", name)
				}
			}
			t.Error("owned child complete pre-existing public rows changed after fixture cleanup")
		} else {
			t.Log("E2E004 owned child complete old public rows unchanged after native/cleanup")
		}
	})
	b := privateProfileHTTPDBNew(t)
	f := &crossCityOnlineFixture{privateProfileHTTPDBFixture: b}
	g, e := postgres.NewHumanActiveIntents(b.store)
	if e != nil {
		t.Fatal(e)
	}
	b.handler = New(b.store, b.store, nil, b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, nil, false, nil, nil, nil, WithHumanActiveIntents(g))
	for i := 0; i < 2; i++ {
		if _, e = b.store.EnsureAgentProfile(b.ctx, b.agentIDs[i], actorref.PrincipalRef{Type: actorref.Person, ID: b.accountIDs[i]}); e != nil {
			t.Fatal(e)
		}
		f.cities[i] = fmt.Sprintf("e2e004-city-%d-%s", i, b.accountIDs[0])
		b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'LOCAL','GB','UTC','published','LOCAL_SYNTHETIC','local:E2E004','合成维护者',$3)`, f.cities[i], fmt.Sprintf("跨城合成城市%d", i), b.accountIDs[i])
		b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, f.cities[i])
	}
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		for _, q := range []string{
			`DELETE FROM activity_plans WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM activity_participations WHERE participant_account_id=ANY($1::uuid[])`,
			`DELETE FROM activity_invitations WHERE activity_id IN (SELECT id FROM activities WHERE created_by_account_id=ANY($1::uuid[]))`,
			`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`,
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`,
			`DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`,
			`DELETE FROM contexts WHERE city_id=ANY($1::text[]) OR online_key=ANY($1::text[])`,
			`DELETE FROM cities WHERE maintainer_account_id=ANY($1::uuid[])`,
		} {
			args := any(b.accountIDs)
			if strings.Contains(q, "online_key=") {
				args = []string{f.cities[0], f.cities[1], "e2e004-online-" + b.accountIDs[0]}
			}
			if _, e := b.pool.Exec(c, q, args); e != nil {
				t.Error("owned E2E cleanup", e)
			}
		}
	})
	return f
}

func (f *crossCityOnlineFixture) publicTableHashes() map[string][32]byte {
	f.t.Helper()
	rows, e := f.pool.Query(f.ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if e != nil {
		f.t.Fatal(e)
	}
	names := []string{}
	for rows.Next() {
		var n string
		if e = rows.Scan(&n); e != nil {
			f.t.Fatal(e)
		}
		names = append(names, n)
	}
	rows.Close()
	if rows.Err() != nil {
		f.t.Fatal(rows.Err())
	}
	out := map[string][32]byte{}
	for _, n := range names {
		var raw []byte
		e = f.pool.QueryRow(f.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb) FROM public.`+pgx.Identifier{n}.Sanitize()+` t`).Scan(&raw)
		if e != nil {
			f.t.Fatal(e)
		}
		out[n] = sha256.Sum256(raw)
	}
	return out
}

func (f *crossCityOnlineFixture) call(method, path string, body any, who, want int) *httptest.ResponseRecorder {
	f.t.Helper()
	var payload []byte
	if body != nil {
		var e error
		payload, e = json.Marshal(body)
		if e != nil {
			f.t.Fatal(e)
		}
	}
	f.requestCount++
	r := httptest.NewRequest(method, path, strings.NewReader(string(payload)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+f.tokens[who])
	rid := fmt.Sprintf("e2e004_%04d", f.requestCount)
	r.Header.Set("X-Request-ID", rid)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	f.t.Logf("E2E004 registered HTTP request=%s subject=%s %s %s status=%d", rid, f.accountIDs[who], method, path, w.Code)
	if want != 0 && w.Code != want {
		f.t.Fatalf("request %s want%d got%d %s", rid, want, w.Code, w.Body.String())
	}
	return w
}

func (f *crossCityOnlineFixture) activity() string {
	f.t.Helper()
	return f.activityOf(activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[0]}, "invite_only")
}
func (f *crossCityOnlineFixture) activityOf(organizer activitypublish.Organizer, visibility string) string {
	start := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	w := f.call("POST", "/v1/me/activities", activitypublish.Input{Organizer: organizer, CityID: f.cities[0], Title: "异地线上羽毛球协调（合成）", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "UTC", Visibility: visibility, Modality: "online", PhysicalPlaceStatus: "not_applicable"}, 0, 201)
	var v struct{ Data activitypublish.Activity }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Data.ID == "" || v.Data.PlaceID != nil {
		f.t.Fatal("actual no-place activity draft", w.Body.String())
	}
	f.call("POST", "/v1/me/activities/"+v.Data.ID+"/publish", nil, 0, 200)
	return v.Data.ID
}

func (f *crossCityOnlineFixture) checkPlan(activity string, visible bool) string {
	f.t.Helper()
	w := f.call("GET", "/v1/me/activity-plans", nil, 1, 200)
	var v struct{ Data []activityplan.Plan }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		f.t.Fatal("plan DTO")
	}
	for _, p := range v.Data {
		if p.ActivityID == activity {
			if p.Available != visible || (!visible && (p.Title != "" || p.CityID != "" || p.StartsAt != nil || p.EndsAt != nil || p.Status != "unavailable")) {
				f.t.Fatal("private source not neutral / current visibility", w.Body.String())
			}
			return p.ID
		}
	}
	f.t.Fatal("own stable plan missing", w.Body.String())
	return ""
}

func TestV4CrossCityOnlinePlanInvitationWithdrawalBlockAndSourceExpiry(t *testing.T) {
	f := crossCityOnlineNative(t)
	id := f.activity()
	before := f.identitySnapshot()
	f.call("POST", "/v1/me/activities/"+id+"/invitations", map[string]string{"userAccountId": f.accountIDs[1]}, 0, 204)
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 201)
	original := f.checkPlan(id, true)
	f.exec(`UPDATE activity_invitations SET status='revoked' WHERE activity_id=$1 AND invitee_account_id=$2`, id, f.accountIDs[1])
	if f.checkPlan(id, false) != original {
		t.Fatal("revoke replaced own plan")
	}
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
	f.call("POST", "/v1/me/activities/"+id+"/invitations", map[string]string{"userAccountId": f.accountIDs[1]}, 0, 204)
	if f.checkPlan(id, true) != original {
		t.Fatal("reauthorized view changed stable plan")
	}
	for _, direction := range [][2]int{{0, 1}, {1, 0}} {
		f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[direction[0]], f.accountIDs[direction[1]])
		if f.checkPlan(id, false) != original {
			t.Fatal("block changed own plan")
		}
		f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
		f.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.accountIDs[direction[0]], f.accountIDs[direction[1]])
	}
	f.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.cities[0])
	f.checkPlan(id, false)
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
	f.exec(`UPDATE cities SET expires_at=NULL WHERE id=$1`, f.cities[0])
	f.checkPlan(id, true)
	f.exec(`UPDATE activities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, id)
	f.checkPlan(id, false)
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
	f.exec(`UPDATE activities SET expires_at=NULL WHERE id=$1`, id)
	f.call("POST", "/v1/me/activities/"+id+"/cancel", nil, 0, 200)
	w := f.call("GET", "/v1/me/activity-plans", nil, 1, 200)
	var v struct{ Data []activityplan.Plan }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Data) != 1 || v.Data[0].ID != original || v.Data[0].Status != "cancelled" || !v.Data[0].Available {
		t.Fatal("original authorized cancellation semantics", w.Body.String())
	}
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
	if before != f.identitySnapshot() {
		t.Fatal("source checks changed identities/ties")
	}
}

func TestV4CrossCityOnlinePlanOrganizerMembershipCurrentACL(t *testing.T) {
	f := crossCityOnlineNative(t)
	var org string
	if e := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&org); e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role,status) VALUES($1,$2,'owner','active')`, org, f.accountIDs[0])
	id := f.activityOf(activitypublish.Organizer{Type: "ORGANIZATION", ID: org}, "organizer_members")
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role,status) VALUES($1,$2,'member','active')`, org, f.accountIDs[1])
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 201)
	original := f.checkPlan(id, true)
	f.exec(`UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1])
	if f.checkPlan(id, false) != original {
		t.Fatal("membership revoke removed own stable plan")
	}
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
}

func TestV4CrossCityOnlinePlanNaturalExpiryAndOriginalPastSemantics(t *testing.T) {
	f := crossCityOnlineNative(t)
	id := f.activityOf(activitypublish.Organizer{Type: "PERSON", ID: f.accountIDs[0]}, "public")
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 201)
	original := f.checkPlan(id, true)
	// A genuine PG-clock boundary, rather than a client timestamp or an already
	// expired fabricated request. New persistence stops; the own row stays neutral.
	f.exec(`UPDATE cities SET expires_at=clock_timestamp()+interval '200 milliseconds' WHERE id=$1`, f.cities[0])
	f.exec(`SELECT pg_sleep(0.3)`)
	if f.checkPlan(id, false) != original {
		t.Fatal("natural expiry changed own plan")
	}
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
	f.exec(`UPDATE cities SET expires_at=NULL WHERE id=$1`, f.cities[0])
	f.exec(`UPDATE activities SET starts_at=clock_timestamp()-interval '2 hours',ends_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, id)
	w := f.call("GET", "/v1/me/activity-plans", nil, 1, 200)
	var v struct{ Data []activityplan.Plan }
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Data) != 1 || v.Data[0].ID != original || !v.Data[0].Available || v.Data[0].Status != "past" {
		t.Fatal("authorized past remains original semantics", w.Body.String())
	}
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
}

// Compare every public-domain table, excluding only native authentication idle
// refresh in sessions. No arbitrary domain read, notification or audit mutation
// is treated as an acceptable side effect. Only the digest leaves this helper.
func (f *crossCityOnlineFixture) publicDomainDigest() [32]byte {
	return f.digestPublicTables(false)
}
func (f *crossCityOnlineFixture) digestPublicTables(includeSessions bool) [32]byte {
	f.t.Helper()
	rows, e := f.pool.Query(f.ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND ($1 OR tablename<>'sessions') ORDER BY tablename`, includeSessions)
	if e != nil {
		f.t.Fatal(e)
	}
	names := []string{}
	for rows.Next() {
		var n string
		if e = rows.Scan(&n); e != nil {
			f.t.Fatal(e)
		}
		names = append(names, n)
	}
	rows.Close()
	if rows.Err() != nil {
		f.t.Fatal(rows.Err())
	}
	data := map[string]json.RawMessage{}
	for _, n := range names {
		var raw []byte
		e = f.pool.QueryRow(f.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb) FROM public.`+pgx.Identifier{n}.Sanitize()+` t`).Scan(&raw)
		if e != nil {
			f.t.Fatal(e)
		}
		data[n] = json.RawMessage(raw)
	}
	raw, e := json.Marshal(data)
	if e != nil {
		f.t.Fatal(e)
	}
	return sha256.Sum256(raw)
}

func TestV4CrossCityOnlineUninvitedPlansClosed(t *testing.T) {
	f := crossCityOnlineNative(t)
	id := f.activity()
	f.call("GET", "/v1/activities/"+id, nil, 1, 404)
	// Knowing an ID is not an invitation or permission to persist/read private details.
	f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": id}, 1, 404)
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM activity_plans WHERE owner_account_id=$1 AND activity_id=$2`, f.accountIDs[1], id).Scan(&n); e != nil || n != 0 {
		t.Fatal("uninvited plan persisted", n, e)
	}
}

func (f *crossCityOnlineFixture) identitySnapshot() string {
	f.t.Helper()
	var text string
	e := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'accounts',(SELECT jsonb_agg(jsonb_build_object('id',id,'kind',account_type,'status',status) ORDER BY id) FROM accounts WHERE id=ANY($1::uuid[])),
 'agents',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM agents a WHERE principal_account_id=ANY($1::uuid[])),
 'profiles',(SELECT jsonb_agg(to_jsonb(p) ORDER BY account_id) FROM user_profiles p WHERE account_id=ANY($1::uuid[])),
 'agentMetadata',(SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_profiles p WHERE owner_id=ANY($1::uuid[])),
 'sessionIdentity',(SELECT jsonb_agg(jsonb_build_object('id',id,'owner',account_id,'digest',token_sha256,'method',authentication_method,'created',created_at,'absoluteExpiry',expires_at,'revoked',revoked_at) ORDER BY id) FROM sessions WHERE account_id=ANY($1::uuid[])),
 'contexts',(SELECT jsonb_agg(to_jsonb(pc) ORDER BY person_account_id,context_id,relation) FROM person_contexts pc WHERE person_account_id=ANY($1::uuid[])),
 'ties',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM person_ties t WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])))::text`, f.accountIDs).Scan(&text)
	if e != nil {
		f.t.Fatal(e)
	}
	return text
}

func (f *crossCityOnlineFixture) activate(who int, contextID string) string {
	f.t.Helper()
	w := f.call("POST", "/v1/me/social-intents", socialintent.DraftInput{Type: "FIND_COMPANION", Title: "线上 badminton 羽毛球交流（合成）", Constraints: json.RawMessage(`{"category":"badminton"}`), Audience: "PUBLIC", Modality: "ONLINE", ContextID: contextID, ExpiresAt: time.Now().UTC().Add(time.Hour)}, who, 201)
	var draft struct{ Data socialintent.Record }
	if json.Unmarshal(w.Body.Bytes(), &draft) != nil || draft.Data.Status != "DRAFT" || draft.Data.CityID != "" {
		f.t.Fatal("original private-stage online draft", w.Body.String())
	}
	id := draft.Data.ID
	w = f.call("GET", "/v1/me/active-social-intents/"+id, nil, who, 200)
	var d struct{ Data ai.Detail }
	if json.Unmarshal(w.Body.Bytes(), &d) != nil {
		f.t.Fatal("detail")
	}
	w = f.call("POST", "/v1/me/active-social-intents/"+id+"/preview", ai.Input{Operation: "ACTIVATE", ExpectedVersion: d.Data.Item.Version}, who, 200)
	var p struct{ Data ai.Preview }
	if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Data.PreviewID == "" {
		f.t.Fatal("actual specific-version preview", w.Body.String())
	}
	w = f.call("POST", "/v1/me/active-social-intents/"+id+"/approve", map[string]string{"previewId": p.Data.PreviewID}, who, 200)
	var receipt struct{ Data ai.Receipt }
	if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Data.Item.Intent.ID != id || receipt.Data.Item.Intent.Status != "ACTIVE" {
		f.t.Fatal("independently approved original online intent", w.Body.String())
	}
	return id
}

func TestV4CrossCityOnlineTwoSubjectsCoordinateWithoutLocationAndRestart(t *testing.T) {
	f := crossCityOnlineNative(t)
	var online string
	for who := 0; who < 2; who++ {
		w := f.call("POST", "/v1/me/contexts", contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.cities[who], Relation: "current"}, who, 201)
		var city struct{ Data contextgraph.Declaration }
		if json.Unmarshal(w.Body.Bytes(), &city) != nil || city.Data.SourceKey != f.cities[who] || city.Data.Visibility != "private" {
			t.Fatal("explicit synthetic current city", w.Body.String())
		}
		w = f.call("POST", "/v1/me/contexts", contextgraph.DeclarationInput{Type: contextgraph.Online, SourceKey: "e2e004-online-" + f.accountIDs[0], Relation: "interest"}, who, 201)
		var c struct{ Data contextgraph.Declaration }
		if json.Unmarshal(w.Body.Bytes(), &c) != nil || c.Data.Visibility != "private" || c.Data.Type != contextgraph.Online {
			t.Fatal("explicit online declaration", w.Body.String())
		}
		if who == 0 {
			online = c.Data.ContextID
		} else if online != c.Data.ContextID {
			t.Fatal("shared actual ONLINE node")
		}
	}
	before := f.identitySnapshot()
	intents := [2]string{f.activate(0, online), f.activate(1, online)}
	tasks := [2]string{}
	for who := 0; who < 2; who++ {
		w := f.call("POST", "/v1/me/now/online/tasks", nq.Input{ContextID: online, Query: "badminton"}, who, 200)
		var result struct{ Data nq.Response }
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.Task == nil || result.Data.Task.CityID != "" || result.Data.Task.ContextType != "ONLINE" || result.Data.Task.ContextID != online || result.Data.Task.PrincipalID != f.accountIDs[who] || result.Data.Task.ActingUserID != f.accountIDs[who] {
			t.Fatal("true native no-city task", w.Body.String())
		}
		found := false
		for _, item := range result.Data.Items {
			if item.ID == intents[1-who] {
				found = true
			}
		}
		if !found || result.Data.ModelAccess != "UNAVAILABLE" || result.Data.Promotion {
			t.Fatal("native original other-city intent", w.Body.String())
		}
		tasks[who] = result.Data.Task.ID
		f.call("GET", "/v1/me/now/online/tasks/"+tasks[who], nil, 1-who, 404)
		w = f.call("GET", "/v1/me/online-social-opportunities/"+intents[who], nil, who, 200)
		var opportunities struct{ Data oso.View }
		if json.Unmarshal(w.Body.Bytes(), &opportunities) != nil || oso.Validate(opportunities.Data) != nil {
			t.Fatal("native opportunity shape", w.Body.String())
		}
		found = false
		for _, item := range opportunities.Data.Items {
			if item.Source.ID == intents[1-who] && item.Source.Type == "SOCIAL_INTENT" {
				found = true
			}
		}
		if !found {
			t.Fatal("independent other-city coordination choice", w.Body.String())
		}
		for _, secret := range []string{"latitude", "longitude", "mapBounds", "token_sha256"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("no map/private transport", secret)
			}
		}
	}
	activity := f.activity()
	f.call("GET", "/v1/activities/"+activity, nil, 1, 404)
	f.call("POST", "/v1/me/activities/"+activity+"/invitations", map[string]string{"userAccountId": f.accountIDs[1]}, 0, 204)
	for who := 0; who < 2; who++ {
		f.call("GET", "/v1/activities/"+activity, nil, who, 200)
		f.call("POST", "/v1/activities/"+activity+"/participations", nil, who, 201)
		f.call("POST", "/v1/activities/"+activity+"/participations", nil, who, 200)
		f.call("POST", "/v1/me/activity-plans", map[string]string{"activityId": activity}, who, 201)
		w := f.call("GET", "/v1/me/activity-plans", nil, who, 200)
		if !strings.Contains(w.Body.String(), activity) {
			t.Fatal("actual own plan", w.Body.String())
		}
	}
	if before != f.identitySnapshot() {
		t.Fatal("ONLINE coordination mutated identity/Agent/current city/declarations/Ties")
	}
	// Two independent OS processes reopen the same durable IDs via the real registered HTTP.
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("owned loopback restart boundary")
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	for round := 1; round <= 2; round++ {
		readBefore := f.publicDomainDigest()
		cmd := exec.CommandContext(f.ctx, exe, "-test.run=^TestV4CrossCityOnlineRestartChild$", "-test.v")
		env := append(os.Environ(), "BIRDTIE_E2E004_CHILD=owned-native-http", "BIRDTIE_E2E004_CHILD_DB="+cfg.ConnConfig.Database, "BIRDTIE_E2E004_ACTIVITY="+activity, "BIRDTIE_E2E004_ONLINE="+online)
		for who := 0; who < 2; who++ {
			env = append(env, fmt.Sprintf("BIRDTIE_E2E004_TOKEN%d=%s", who, f.tokens[who]), fmt.Sprintf("BIRDTIE_E2E004_PERSON%d=%s", who, f.accountIDs[who]), fmt.Sprintf("BIRDTIE_E2E004_INTENT%d=%s", who, intents[who]), fmt.Sprintf("BIRDTIE_E2E004_TASK%d=%s", who, tasks[who]))
		}
		cmd.Env = env
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal("true process restart failed", round, string(out))
		}
		t.Logf("independent process round=%d %s", round, strings.TrimSpace(string(out)))
		if readBefore != f.publicDomainDigest() {
			t.Fatal("restart GET wrote public-domain rows; only sessions idle refresh excluded")
		}
	}
	if before != f.identitySnapshot() {
		t.Fatal("process restart identity/ties mutation")
	}
}

func TestV4CrossCityOnlineRestartChild(t *testing.T) {
	if os.Getenv("BIRDTIE_E2E004_CHILD") == "" {
		return
	}
	if os.Getenv("BIRDTIE_E2E004_CHILD") != "owned-native-http" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("child boundary")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_E2E004_CHILD_DB") || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_saf001_http_") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("owned real child database")
	}
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := postgres.New(pool, false)
	h := New(s, s, nil, s, s, s, s, s, s, s, s, nil, false, nil, nil, nil)
	srv := httptest.NewServer(h)
	defer srv.Close()
	for who := 0; who < 2; who++ {
		token := os.Getenv(fmt.Sprintf("BIRDTIE_E2E004_TOKEN%d", who))
		person := os.Getenv(fmt.Sprintf("BIRDTIE_E2E004_PERSON%d", who))
		intent := os.Getenv(fmt.Sprintf("BIRDTIE_E2E004_INTENT%d", who))
		task := os.Getenv(fmt.Sprintf("BIRDTIE_E2E004_TASK%d", who))
		activity := os.Getenv("BIRDTIE_E2E004_ACTIVITY")
		for index, path := range []string{"/v1/me/contexts", "/v1/me/social-intents", "/v1/me/now/online/tasks/" + task, "/v1/activities/" + activity + "/participations/me", "/v1/me/activity-plans", "/v1/me/ties"} {
			req, e := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
			if e != nil {
				t.Fatal(e)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Request-ID", fmt.Sprintf("e2e004_restart_%d_%d", who, index))
			resp, e := srv.Client().Do(req)
			if e != nil {
				t.Fatal(e)
			}
			var raw json.RawMessage
			e = json.NewDecoder(resp.Body).Decode(&raw)
			resp.Body.Close()
			if e != nil || resp.StatusCode != 200 {
				t.Fatal("registered restart GET", path, resp.StatusCode, string(raw), e)
			}
			switch index {
			case 0:
				if !strings.Contains(string(raw), os.Getenv("BIRDTIE_E2E004_ONLINE")) {
					t.Fatal("durable online context")
				}
			case 1:
				if !strings.Contains(string(raw), intent) {
					t.Fatal("durable original intent")
				}
			case 2:
				var v struct{ Data nq.Response }
				if json.Unmarshal(raw, &v) != nil || v.Data.Task == nil || v.Data.Task.ID != task || v.Data.Task.CityID != "" || v.Data.Task.PrincipalID != person {
					t.Fatal("durable own no-city task")
				}
			case 3:
				if !strings.Contains(string(raw), activity) || !strings.Contains(string(raw), "going") {
					t.Fatal("durable RSVP not attendance", string(raw))
				}
			case 4:
				if !strings.Contains(string(raw), activity) {
					t.Fatal("durable own plan")
				}
			case 5:
				var v struct{ Data []any }
				if json.Unmarshal(raw, &v) != nil || !reflect.DeepEqual(v.Data, []any{}) {
					t.Fatal("no implicit Ties", string(raw))
				}
			}
			t.Logf("OS PID=%d subject=%s real GET %s status=200", os.Getpid(), person, path)
		}
	}
}
