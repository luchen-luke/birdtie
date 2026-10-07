package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This uses real registered routes and real PostgreSQL authorization. Every
// principal, city and event is an owned disposable synthetic fixture; no IdP,
// partner, external activity or production publishing is verified here.
type cityActivityHTTPDB struct {
	*privateProfileHTTPDBFixture
	cityID, organizationID, communityID, businessID string
	candidateIDs                                    []string
	requests                                        int
}

func cityActivityHTTPDBNew(t *testing.T) *cityActivityHTTPDB {
	t.Helper()
	f := &cityActivityHTTPDB{privateProfileHTTPDBFixture: privateProfileHTTPDBNew(t)}
	var installed bool
	if err := f.pool.QueryRow(f.ctx, `SELECT to_regclass('public.city_activity_candidate_organizers') IS NOT NULL`).Scan(&installed); err != nil || !installed {
		t.Fatal("city HTTP integration requires actual migration 059")
	}
	// This cleanup runs before the reused identity fixture's cleanup. Only this
	// fixture's own City/domain IDs are removed; no stable development seed is reset.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for step, command := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM activity_sources WHERE activity_id IN(SELECT id FROM activities WHERE city_id=$1)`, []any{f.cityID}},
			{`DELETE FROM city_seed_activities WHERE city_id=$1`, []any{f.cityID}},
			{`DELETE FROM activity_candidates WHERE city_id=$1`, []any{f.cityID}},
			{`DELETE FROM activities WHERE city_id=$1`, []any{f.cityID}},
			{`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[]) AND resource_type='activity_candidate'`, []any{f.accountIDs}},
			{`DELETE FROM communities WHERE id=NULLIF($1,'')::uuid`, []any{f.communityID}},
			{`DELETE FROM business_memberships WHERE business_id=NULLIF($1,'')::uuid`, []any{f.businessID}},
			{`DELETE FROM businesses WHERE id=NULLIF($1,'')::uuid`, []any{f.businessID}},
			{`DELETE FROM city_editor_memberships WHERE city_id=$1`, []any{f.cityID}},
			{`DELETE FROM city_contexts WHERE city_id=$1`, []any{f.cityID}},
			{`DELETE FROM cities WHERE id=$1`, []any{f.cityID}},
		} {
			if _, err := f.pool.Exec(ctx, command.sql, command.args...); err != nil {
				t.Errorf("owned city HTTP cleanup step %d failed: %v", step, err)
			}
		}
		var remaining int
		if err := f.pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM cities WHERE id=$1)+
			(SELECT count(*) FROM activity_candidates WHERE city_id=$1)+
			(SELECT count(*) FROM city_activity_candidate_organizers WHERE candidate_id=ANY($2::uuid[]))+
			(SELECT count(*) FROM activities WHERE city_id=$1)+
			(SELECT count(*) FROM city_seed_activities WHERE city_id=$1)+
			(SELECT count(*) FROM city_editor_memberships WHERE city_id=$1)+
			(SELECT count(*) FROM communities WHERE id=NULLIF($3,'')::uuid)+
			(SELECT count(*) FROM businesses WHERE id=NULLIF($4,'')::uuid)`, f.cityID, f.candidateIDs, f.communityID, f.businessID).Scan(&remaining); err != nil || remaining != 0 {
			t.Errorf("owned city HTTP fixture residue=%d", remaining)
		} else {
			t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY owned city fixture residue=0")
		}
	})
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label)
		VALUES('city-http-'||gen_random_uuid()::text,'合成城市','合成地区','GB','Europe/London','published','合成验收来源','https://example.invalid/city','城市维护者') RETURNING id`).Scan(&f.cityID); err != nil {
		t.Fatal("cannot create owned synthetic city")
	}
	f.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES($1,$2,'contributor'),($1,$3,'reviewer')`, f.cityID, f.accountIDs[0], f.accountIDs[1])
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&f.organizationID); err != nil {
		t.Fatal("owned Organization fixture missing")
	}
	f.exec(`UPDATE organizations SET visibility='public' WHERE id=$1`, f.organizationID)
	// The reviewer independently owns all three managed resources; the original
	// submitter is an admin. Revoking that admin must not transfer the candidate's
	// publication authority to the reviewer.
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'admin'),($1,$3,'owner')`, f.organizationID, f.accountIDs[0], f.accountIDs[1])
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO communities(city_id,owner_account_id,name,summary,visibility,publication_status,owner_confirmed_at,source_label,source_ref,maintainer_label,verified_at,rights_note,reviewed_by,reviewed_at)
		VALUES($1,$2,'合成公开社区','仅本地可移除的合成资源','public','published',now(),'独立合成社区来源','https://example.invalid/community','社区维护者',now(),'合成数据仅供本地验证',$3,now()) RETURNING id`, f.cityID, f.accountIDs[1], f.accountIDs[0]).Scan(&f.communityID); err != nil {
		t.Fatalf("cannot create owned public Community fixture: %v", err)
	}
	f.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'admin','active')`, f.communityID, f.accountIDs[0])
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO businesses(account_id,name,claim_status,claim_source_url,claim_reviewed_by,claim_reviewed_at)
		VALUES($1,'合成商家','verified','https://example.invalid/synthetic-claim',$2,now()) RETURNING id`, f.accountIDs[3], f.accountIDs[1]).Scan(&f.businessID); err != nil {
		t.Fatal("cannot create owned synthetic reviewed Business shape")
	}
	f.exec(`INSERT INTO business_memberships(business_id,user_account_id,role) VALUES($1,$2,'admin'),($1,$3,'owner')`, f.businessID, f.accountIDs[0], f.accountIDs[1])
	f.exec(`UPDATE accounts SET handle='CITY_REVIEWER_PRIVATE_HANDLE_CANARY' WHERE id=$1`, f.accountIDs[1])
	f.exec(`UPDATE user_profiles SET display_name='CITY_REVIEWER_PRIVATE_HANDLE_CANARY',visibility='private' WHERE account_id=$1`, f.accountIDs[1])
	f.handler = cityActivityHTTPDBHandler(f.store, f.pool)
	return f
}

func cityActivityHTTPDBHandler(store *postgres.Store, pool *pgxpool.Pool) http.Handler {
	return New(store, store, store, store, store, store, store, store, store, store, store, store, false, nil, pool, nil)
}

func (f *cityActivityHTTPDB) request(t *testing.T, method, path, body, token string, want int, workspace *string) *httptest.ResponseRecorder {
	t.Helper()
	f.requests++
	r := privateProfileHTTPRequest(method, path, body, token, "application/json")
	requestID := fmt.Sprintf("city_organizer_real_http_%04d", f.requests)
	r.Header.Set("X-Request-ID", requestID)
	if workspace != nil {
		r.Header.Set("X-Birdtie-Organization-Workspace", *workspace)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("actual city HTTP status=%d want=%d request_id=%s error=%s", w.Code, want, requestID, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") != requestID {
		t.Fatal("actual city route lost no-store or request correlation")
	}
	if strings.Contains(w.Body.String(), "CITY_REVIEWER_PRIVATE_HANDLE_CANARY") {
		t.Fatal("city publication leaked automatically copied private reviewer label")
	}
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY method=%s status=%d request_id=%s", method, w.Code, requestID)
	return w
}

func cityActivityHTTPDBCandidate(t *testing.T, w *httptest.ResponseRecorder) cityseed.ActivityCandidate {
	t.Helper()
	var envelope struct {
		Data cityseed.ActivityCandidate `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil || !uuidPath.MatchString(envelope.Data.ID) {
		t.Fatal("invalid persisted City candidate response")
	}
	return envelope.Data
}

func (f *cityActivityHTTPDB) submit(t *testing.T, organizer *actorref.ActorRef) cityseed.ActivityCandidate {
	t.Helper()
	extra := ""
	if organizer != nil {
		raw, err := json.Marshal(organizer)
		if err != nil {
			t.Fatal("invalid synthetic organizer")
		}
		extra = `"organizer":` + string(raw)
	}
	w := f.request(t, http.MethodPost, "/v1/cities/"+f.cityID+"/activity-candidates", cityActivityHTTPBody(t, extra), f.tokens[0], 201, nil)
	c := cityActivityHTTPDBCandidate(t, w)
	f.candidateIDs = append(f.candidateIDs, c.ID)
	if c.SubmittedBy != f.accountIDs[0] || c.CityID != f.cityID || c.Status != "pending" || (organizer == nil) != (c.Organizer == nil) || (organizer != nil && !organizer.Equal(*c.Organizer)) {
		t.Fatal("submission did not persist the explicit typed owner selector")
	}
	return c
}

func (f *cityActivityHTTPDB) review(t *testing.T, id, decision string, want int) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodPost, "/v1/activity-candidates/"+id+"/review", `{"decision":"`+decision+`","note":"已核对合成资料、授权主办方和来源；仅供本地测试。"}`, f.tokens[1], want, nil)
}

func (f *cityActivityHTTPDB) publicationSnapshot(t *testing.T) string {
	t.Helper()
	var out string
	err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
	 'candidates',COALESCE((SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM activity_candidates c WHERE city_id=$1),'[]'::jsonb),
	 'selectors',COALESCE((SELECT jsonb_agg(to_jsonb(b) ORDER BY b.candidate_id) FROM city_activity_candidate_organizers b JOIN activity_candidates c ON c.id=b.candidate_id WHERE c.city_id=$1),'[]'::jsonb),
	 'activities',COALESCE((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM activities a WHERE city_id=$1),'[]'::jsonb),
	 'organizers',COALESCE((SELECT jsonb_agg(to_jsonb(o) ORDER BY o.activity_id) FROM activity_organizers o JOIN activities a ON a.id=o.activity_id WHERE a.city_id=$1),'[]'::jsonb),
	 'sources',COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.activity_id) FROM activity_sources s JOIN activities a ON a.id=s.activity_id WHERE a.city_id=$1),'[]'::jsonb),
	 'seed',COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.activity_id) FROM city_seed_activities s WHERE city_id=$1),'[]'::jsonb),
	 'inbox',COALESCE((SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM inbox_items i WHERE recipient_account_id=ANY($2::uuid[]) AND resource_type='activity_candidate'),'[]'::jsonb))::text`, f.cityID, f.accountIDs).Scan(&out)
	if err != nil {
		t.Fatal("cannot snapshot owned publication rows")
	}
	return out
}

func (f *cityActivityHTTPDB) unchanged(t *testing.T, before string) {
	t.Helper()
	if f.publicationSnapshot(t) != before {
		t.Fatal("denied command changed candidate, organizer, source, publication or inbox rows")
	}
}

func TestCityActivityOrganizerHTTPIntegration(t *testing.T) {
	f := cityActivityHTTPDBNew(t)
	t.Run("explicit null legacy pending remains rejectable", func(t *testing.T) {
		w := f.request(t, http.MethodPost, "/v1/cities/"+f.cityID+"/activity-candidates", cityActivityHTTPBody(t, `"organizer":null`), f.tokens[0], 201, nil)
		c := cityActivityHTTPDBCandidate(t, w)
		f.candidateIDs = append(f.candidateIDs, c.ID)
		if c.Organizer != nil || c.Status != "pending" {
			t.Fatal("null legacy selector must remain unknown and pending")
		}
		before := f.publicationSnapshot(t)
		cityActivityHTTPError(t, f.review(t, c.ID, "publish", 409), 409, "organizer_required")
		f.unchanged(t, before)
		if rejected := cityActivityHTTPDBCandidate(t, f.review(t, c.ID, "reject", 200)); rejected.Organizer != nil || rejected.Status != "rejected" {
			t.Fatal("null legacy selector was inferred during rejection")
		}
	})
	t.Run("legacy unknown can list reject but cannot publish", func(t *testing.T) {
		c := f.submit(t, nil)
		w := f.request(t, http.MethodGet, "/v1/cities/"+f.cityID+"/activity-candidates", "", f.tokens[1], 200, nil)
		var listed struct {
			Data []cityseed.ActivityCandidate `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &listed) != nil || len(listed.Data) != 1 || listed.Data[0].ID != c.ID || listed.Data[0].Organizer != nil {
			t.Fatal("legacy unknown-organizer pending candidate lost compatibility")
		}
		before := f.publicationSnapshot(t)
		cityActivityHTTPError(t, f.review(t, c.ID, "publish", 409), 409, "organizer_required")
		f.unchanged(t, before)
		rejected := cityActivityHTTPDBCandidate(t, f.review(t, c.ID, "reject", 200))
		if rejected.Status != "rejected" || rejected.Organizer != nil || rejected.ResolvedActivityID != "" {
			t.Fatal("legacy rejection guessed organizer or published content")
		}
	})
	for _, ref := range []actorref.ActorRef{{Type: actorref.Person, ID: f.accountIDs[0]}, {Type: actorref.Community, ID: f.communityID}, {Type: actorref.Organization, ID: f.organizationID}, {Type: actorref.Business, ID: f.businessID}} {
		t.Run("actual submit publish detail reconnect "+string(ref.Type), func(t *testing.T) {
			c := f.submit(t, &ref)
			published := cityActivityHTTPDBCandidate(t, f.review(t, c.ID, "publish", 200))
			if published.Status != "published" || published.Organizer == nil || !ref.Equal(*published.Organizer) || !uuidPath.MatchString(published.ResolvedActivityID) {
				t.Fatal("review did not publish selected typed organizer")
			}
			var persistedType, persistedID, creator, reviewer, hostLabel, sourceLabel, sourceURL, maintainer string
			var organizationCount int
			err := f.pool.QueryRow(f.ctx, `SELECT CASE WHEN o.person_account_id IS NOT NULL THEN 'PERSON' WHEN o.community_id IS NOT NULL THEN 'COMMUNITY' WHEN o.organization_id IS NOT NULL THEN 'ORGANIZATION' ELSE 'BUSINESS' END,
			 COALESCE(o.person_account_id,o.community_id,o.organization_id,o.business_id)::text,
			 a.created_by_account_id::text,s.reviewer_account_id::text,a.host_label,s.source_label,s.source_url,a.maintainer_label,
			 num_nonnulls(o.person_account_id,o.community_id,o.organization_id,o.business_id)
			 FROM activities a JOIN activity_organizers o ON o.activity_id=a.id JOIN activity_sources s ON s.activity_id=a.id WHERE a.id=$1 AND s.candidate_id=$2`, published.ResolvedActivityID, c.ID).Scan(&persistedType, &persistedID, &creator, &reviewer, &hostLabel, &sourceLabel, &sourceURL, &maintainer, &organizationCount)
			if err != nil || persistedType != string(ref.Type) || persistedID != ref.ID || creator != f.accountIDs[0] || reviewer != f.accountIDs[1] || organizationCount != 1 || hostLabel != "独立主办方标签" || sourceLabel != "合成线索来源" || sourceURL != "https://example.invalid/authorized-event" || maintainer != "城市维护者" {
				t.Fatalf("native organizer/source/FK publication invariant failed: %v", err)
			}
			if ref.Type == actorref.Person && persistedID == reviewer {
				t.Fatal("reviewer was inferred as Person organizer")
			}
			w := f.request(t, http.MethodGet, "/v1/activities/"+published.ResolvedActivityID, "", "", 200, nil)
			var detail struct {
				Data foundation.Activity `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Data.ID != published.ResolvedActivityID || detail.Data.Organizer.Type != string(ref.Type) || detail.Data.Organizer.ID != ref.ID || detail.Data.HostLabel != hostLabel || detail.Data.Source.Reference != sourceURL || detail.Data.Source.Maintainer != "城市维护者" {
				t.Fatal("public API changed selected organizer or independent source labels")
			}
			pool, err := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
			if err != nil {
				t.Fatal("cannot reconnect owned actual City database")
			}
			defer pool.Close()
			reconnected := cityActivityHTTPDBHandler(postgres.New(pool, false), pool)
			prior := f.handler
			f.handler = reconnected
			defer func() { f.handler = prior }()
			w = f.request(t, http.MethodGet, "/v1/activities/"+published.ResolvedActivityID, "", "", 200, nil)
			if json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.Data.Organizer.ID != ref.ID {
				t.Fatal("reconnected public API lost persisted typed organizer")
			}
			before := f.publicationSnapshot(t)
			cityActivityHTTPError(t, f.review(t, c.ID, "publish", 409), 409, "review_conflict")
			f.unchanged(t, before)
			var notices int
			var title, explanation string
			if err = f.pool.QueryRow(f.ctx, `SELECT count(*),min(title),min(detail) FROM inbox_items WHERE recipient_account_id=$1 AND resource_type='activity_candidate' AND resource_id=$2`, f.accountIDs[0], c.ID).Scan(&notices, &title, &explanation); err != nil || notices != 1 || title != "活动建议审核结果" || !strings.Contains(explanation, "已发布") {
				t.Fatal("actual review result notification lost Chinese or duplicated")
			}
			t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY candidate_id=%s activity_id=%s organizer_type=%s", c.ID, published.ResolvedActivityID, ref.Type)
		})
	}
	t.Run("anonymous public city discovery returns all persisted typed organizers", func(t *testing.T) {
		w := f.request(t, http.MethodGet, "/v1/cities/"+f.cityID+"/activities", "", "", 200, nil)
		var envelope struct {
			Data []foundation.Activity `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || len(envelope.Data) != 4 {
			t.Fatal("published candidates did not enter actual public city discovery")
		}
		want := map[string]string{"PERSON": f.accountIDs[0], "COMMUNITY": f.communityID, "ORGANIZATION": f.organizationID, "BUSINESS": f.businessID}
		for _, activity := range envelope.Data {
			if want[activity.Organizer.Type] != activity.Organizer.ID || activity.HostLabel != "独立主办方标签" || activity.Source.Maintainer != "城市维护者" {
				t.Fatal("city discovery guessed organizer or changed independent source/host labels")
			}
			delete(want, activity.Organizer.Type)
		}
		if len(want) != 0 {
			t.Fatal("city discovery omitted a selected organizer")
		}
	})
	t.Run("submission permission rejects current ordinary member", func(t *testing.T) {
		f.exec(`UPDATE organization_memberships SET role='member' WHERE organization_id=$1 AND user_account_id=$2`, f.organizationID, f.accountIDs[0])
		defer f.exec(`UPDATE organization_memberships SET role='admin' WHERE organization_id=$1 AND user_account_id=$2`, f.organizationID, f.accountIDs[0])
		before := f.publicationSnapshot(t)
		w := f.request(t, http.MethodPost, "/v1/cities/"+f.cityID+"/activity-candidates", cityActivityHTTPBody(t, `"organizer":{"type":"ORGANIZATION","id":"`+f.organizationID+`"}`), f.tokens[0], 409, nil)
		cityActivityHTTPError(t, w, 409, "organizer_unavailable")
		f.unchanged(t, before)
	})
	for _, tt := range []struct {
		name            string
		ref             actorref.ActorRef
		change, restore string
		args            []any
	}{
		{"original Organization admin revoked", actorref.ActorRef{Type: actorref.Organization, ID: f.organizationID}, `UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND user_account_id=$2`, `UPDATE organization_memberships SET status='active' WHERE organization_id=$1 AND user_account_id=$2`, []any{f.organizationID, f.accountIDs[0]}},
		{"original Community admin left", actorref.ActorRef{Type: actorref.Community, ID: f.communityID}, `UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, `UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, []any{f.communityID, f.accountIDs[0]}},
		{"original Business admin revoked", actorref.ActorRef{Type: actorref.Business, ID: f.businessID}, `UPDATE business_memberships SET status='removed' WHERE business_id=$1 AND user_account_id=$2`, `UPDATE business_memberships SET status='active' WHERE business_id=$1 AND user_account_id=$2`, []any{f.businessID, f.accountIDs[0]}},
		{"Organization becomes private", actorref.ActorRef{Type: actorref.Organization, ID: f.organizationID}, `UPDATE organizations SET visibility='private' WHERE id=$1`, `UPDATE organizations SET visibility='public' WHERE id=$1`, []any{f.organizationID}},
		{"Community becomes private", actorref.ActorRef{Type: actorref.Community, ID: f.communityID}, `UPDATE communities SET visibility='private' WHERE id=$1`, `UPDATE communities SET visibility='public' WHERE id=$1`, []any{f.communityID}},
		{"Business claim revoked", actorref.ActorRef{Type: actorref.Business, ID: f.businessID}, `UPDATE businesses SET claim_status='revoked' WHERE id=$1`, `UPDATE businesses SET claim_status='verified' WHERE id=$1`, []any{f.businessID}},
		{"Community archived", actorref.ActorRef{Type: actorref.Community, ID: f.communityID}, `UPDATE communities SET lifecycle_status='archived' WHERE id=$1`, `UPDATE communities SET lifecycle_status='active' WHERE id=$1`, []any{f.communityID}},
		{"Community expired", actorref.ActorRef{Type: actorref.Community, ID: f.communityID}, `UPDATE communities SET expires_at=clock_timestamp() WHERE id=$1`, `UPDATE communities SET expires_at=NULL WHERE id=$1`, []any{f.communityID}},
		{"Organization suspended", actorref.ActorRef{Type: actorref.Organization, ID: f.organizationID}, `UPDATE organizations SET status='suspended' WHERE id=$1`, `UPDATE organizations SET status='active' WHERE id=$1`, []any{f.organizationID}},
		{"original city editor revoked", actorref.ActorRef{Type: actorref.Person, ID: f.accountIDs[0]}, `UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1 AND account_id=$2`, `UPDATE city_editor_memberships SET state='active' WHERE city_id=$1 AND account_id=$2`, []any{f.cityID, f.accountIDs[0]}},
		{"original Person suspended", actorref.ActorRef{Type: actorref.Person, ID: f.accountIDs[0]}, `UPDATE accounts SET status='suspended' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`, []any{f.accountIDs[0]}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := f.submit(t, &tt.ref)
			f.exec(tt.change, tt.args...)
			defer f.exec(tt.restore, tt.args...)
			before := f.publicationSnapshot(t)
			cityActivityHTTPError(t, f.review(t, c.ID, "publish", 409), 409, "organizer_unavailable")
			f.unchanged(t, before)
			// Rejection needs the current reviewer, not a now-revoked original
			// organizer's approval; it must remain a safe recovery operation.
			if rejected := cityActivityHTTPDBCandidate(t, f.review(t, c.ID, "reject", 200)); rejected.Status != "rejected" {
				t.Fatal("cannot reject a no-longer-publishable candidate")
			}
		})
	}
	t.Run("expired candidate source cannot publish", func(t *testing.T) {
		c := f.submit(t, &actorref.ActorRef{Type: actorref.Person, ID: f.accountIDs[0]})
		f.exec(`UPDATE activity_candidates SET expires_at=clock_timestamp() WHERE id=$1`, c.ID)
		before := f.publicationSnapshot(t)
		cityActivityHTTPError(t, f.review(t, c.ID, "publish", 409), 409, "review_conflict")
		f.unchanged(t, before)
		if rejected := cityActivityHTTPDBCandidate(t, f.review(t, c.ID, "reject", 200)); rejected.Status != "rejected" {
			t.Fatal("expired source is not rejectable")
		}
	})
	t.Run("already ended activity cannot publish", func(t *testing.T) {
		c := f.submit(t, &actorref.ActorRef{Type: actorref.Person, ID: f.accountIDs[0]})
		f.exec(`UPDATE activity_candidates SET starts_at=clock_timestamp()-interval '2 hours',ends_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, c.ID)
		before := f.publicationSnapshot(t)
		cityActivityHTTPError(t, f.review(t, c.ID, "publish", 409), 409, "review_conflict")
		f.unchanged(t, before)
	})
	t.Run("contributor is not reviewer", func(t *testing.T) {
		c := f.submit(t, &actorref.ActorRef{Type: actorref.Person, ID: f.accountIDs[0]})
		before := f.publicationSnapshot(t)
		w := f.request(t, http.MethodPost, "/v1/activity-candidates/"+c.ID+"/review", `{"decision":"publish","note":"投稿角色不能代替审核角色，合成本地验收"}`, f.tokens[0], 403, nil)
		cityActivityHTTPError(t, w, 403, "reviewer_required")
		f.unchanged(t, before)
	})
	t.Run("current reviewer revoked", func(t *testing.T) {
		c := f.submit(t, &actorref.ActorRef{Type: actorref.Person, ID: f.accountIDs[0]})
		f.exec(`UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1 AND account_id=$2`, f.cityID, f.accountIDs[1])
		defer f.exec(`UPDATE city_editor_memberships SET state='active' WHERE city_id=$1 AND account_id=$2`, f.cityID, f.accountIDs[1])
		before := f.publicationSnapshot(t)
		cityActivityHTTPError(t, f.review(t, c.ID, "publish", 403), 403, "reviewer_required")
		f.unchanged(t, before)
	})
	t.Run("expired City cannot disclose list submit publish or reject", func(t *testing.T) {
		c := f.submit(t, &actorref.ActorRef{Type: actorref.Person, ID: f.accountIDs[0]})
		f.exec(`UPDATE cities SET expires_at=clock_timestamp() WHERE id=$1`, f.cityID)
		defer f.exec(`UPDATE cities SET expires_at=NULL WHERE id=$1`, f.cityID)
		before := f.publicationSnapshot(t)
		auditRows := func() string {
			var out string
			if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]'::jsonb)::text FROM audit_events a WHERE actor_account_id=ANY($1::uuid[]) AND resource_type='activity_candidate'`, f.accountIDs).Scan(&out); err != nil {
				t.Fatal("cannot snapshot owned review audit rows")
			}
			return out
		}
		beforeAudit := auditRows()
		cityActivityHTTPError(t, f.request(t, http.MethodGet, "/v1/cities/"+f.cityID+"/activity-candidates", "", f.tokens[1], 403, nil), 403, "city_editor_required")
		cityActivityHTTPError(t, f.review(t, c.ID, "reject", 403), 403, "reviewer_required")
		cityActivityHTTPError(t, f.review(t, c.ID, "publish", 403), 403, "reviewer_required")
		cityActivityHTTPError(t, f.request(t, http.MethodPost, "/v1/cities/"+f.cityID+"/activity-candidates", cityActivityHTTPBody(t, ""), f.tokens[0], 403, nil), 403, "city_editor_required")
		f.unchanged(t, before)
		if beforeAudit != auditRows() {
			t.Fatal("expired City command changed owned review audit rows")
		}
	})
	t.Run("self review remains forbidden", func(t *testing.T) {
		c := f.submit(t, &actorref.ActorRef{Type: actorref.Person, ID: f.accountIDs[0]})
		f.exec(`UPDATE city_editor_memberships SET role='reviewer' WHERE city_id=$1 AND account_id=$2`, f.cityID, f.accountIDs[0])
		defer f.exec(`UPDATE city_editor_memberships SET role='contributor' WHERE city_id=$1 AND account_id=$2`, f.cityID, f.accountIDs[0])
		before := f.publicationSnapshot(t)
		w := f.request(t, http.MethodPost, "/v1/activity-candidates/"+c.ID+"/review", `{"decision":"publish","note":"本人不能审核本人线索，合成本地验收"}`, f.tokens[0], 409, nil)
		cityActivityHTTPError(t, w, 409, "review_conflict")
		f.unchanged(t, before)
	})
	for _, tt := range []struct {
		name, kind, id string
		status         int
		code           string
	}{
		{"another Person", "PERSON", f.accountIDs[1], 403, "organizer_permission_required"},
		{"Org account is not Org domain ID", "ORGANIZATION", f.accountIDs[2], 409, "organizer_unavailable"},
		{"Business account is not Business domain ID", "BUSINESS", f.accountIDs[3], 409, "organizer_unavailable"},
		{"unknown Community", "COMMUNITY", cityActivityHTTPPeer, 409, "organizer_unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := f.publicationSnapshot(t)
			w := f.request(t, http.MethodPost, "/v1/cities/"+f.cityID+"/activity-candidates", cityActivityHTTPBody(t, `"organizer":{"type":"`+tt.kind+`","id":"`+tt.id+`"}`), f.tokens[0], tt.status, nil)
			cityActivityHTTPError(t, w, tt.status, tt.code)
			f.unchanged(t, before)
		})
	}
	t.Run("real session selector and workspace boundaries", func(t *testing.T) {
		for _, tt := range []struct {
			name, token, path string
			workspace         *string
			want              int
			code              string
		}{
			{name: "anonymous", path: "/v1/cities/" + f.cityID + "/activity-candidates", want: 401, code: "unauthorized"},
			{name: "expired", token: f.newSession(f.accountIDs[0], true, false), path: "/v1/cities/" + f.cityID + "/activity-candidates", want: 401, code: "unauthorized"},
			{name: "revoked", token: f.newSession(f.accountIDs[0], false, true), path: "/v1/cities/" + f.cityID + "/activity-candidates", want: 401, code: "unauthorized"},
			{name: "organization account", token: f.tokens[2], path: "/v1/cities/" + f.cityID + "/activity-candidates", want: 403, code: "person_account_required"},
			{name: "business account", token: f.tokens[3], path: "/v1/cities/" + f.cityID + "/activity-candidates", want: 403, code: "person_account_required"},
			{name: "org workspace", token: f.tokens[0], path: "/v1/cities/" + f.cityID + "/activity-candidates", workspace: &f.organizationID, want: 403, code: "person_account_required"},
			{name: "owner query", token: f.tokens[0], path: "/v1/cities/" + f.cityID + "/activity-candidates?ownerId=" + f.accountIDs[1], want: 400, code: "invalid_query"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				before := f.publicationSnapshot(t)
				cityActivityHTTPError(t, f.request(t, http.MethodPost, tt.path, cityActivityHTTPBody(t, ""), tt.token, tt.want, tt.workspace), tt.want, tt.code)
				f.unchanged(t, before)
			})
		}
	})
}
