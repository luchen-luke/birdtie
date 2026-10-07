package httpapi

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	so "github.com/birdtie/birdtie/apps/api/internal/sponsoredopportunity"
	"reflect"
	"strings"
	"testing"
	"time"
)

type sponsorHTTPFixture struct {
	base                                            *privateProfileHTTPDBFixture
	business, city, place, activity, venueCandidate string
}

func sponsorHTTPNative(t *testing.T) *sponsorHTTPFixture {
	t.Helper()
	b := privateProfileHTTPDBNew(t)
	f := &sponsorHTTPFixture{base: b}
	var id string
	if e := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	f.business = id
	f.city = "sponsor-http-" + strings.ReplaceAll(id, "-", "")
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM sponsored_opportunity_declarations WHERE business_id=$1`, `DELETE FROM agent_tasks WHERE owner_account_id=$1`, `DELETE FROM activities WHERE id=$1`, `DELETE FROM sponsored_opportunity_review_grants WHERE city_id=$1`, `DELETE FROM city_editor_memberships WHERE city_id=$1`, `DELETE FROM social_intents WHERE creator_account_id=$1`, `DELETE FROM business_console_audit_events WHERE business_id=$1`, `DELETE FROM business_console_membership_controls WHERE business_id=$1`, `DELETE FROM business_console_venue_facts WHERE business_id=$1`, `DELETE FROM business_console_profiles WHERE business_id=$1`, `DELETE FROM business_claim_controls WHERE business_id=$1`, `DELETE FROM business_review_grants WHERE business_id=$1`, `DELETE FROM business_venue_relations WHERE business_id=$1`, `DELETE FROM business_memberships WHERE business_id=$1`, `DELETE FROM venues WHERE city_id=$1`, `DELETE FROM venue_candidates WHERE city_id=$1`, `DELETE FROM places WHERE city_id=$1`, `DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`, `DELETE FROM businesses WHERE id=$1`, `DELETE FROM accounts WHERE id=$1`} {
			v := f.city
			switch {
			case strings.Contains(q, "business_id"), strings.Contains(q, "businesses WHERE"):
				v = f.business
			case strings.Contains(q, "activity_id"), strings.Contains(q, "activities WHERE"):
				v = f.activity
			case strings.Contains(q, "owner_account"), strings.Contains(q, "creator_account"):
				v = b.accountIDs[0]
			case strings.Contains(q, "accounts WHERE"):
				var p string
				if e := b.pool.QueryRow(b.ctx, `SELECT account_id FROM businesses WHERE id=$1`, f.business).Scan(&p); e == nil {
					v = p
				} else {
					continue
				}
			}
			if _, e := b.pool.Exec(b.ctx, q, v); e != nil {
				t.Error("owned sponsor HTTP cleanup", q, e)
			}
		}
	})
	// Source operations are the actual native claim and operation domains. Raw
	// SQL only provisions explicit synthetic permissions/public Venue supply;
	// sponsorship itself must pass the five real registered HTTP routes below.
	digest0, e := identity.ParseBearer("Bearer " + b.tokens[0])
	if e != nil {
		t.Fatal(e)
	}
	digest1, e := identity.ParseBearer("Bearer " + b.tokens[1])
	if e != nil {
		t.Fatal(e)
	}
	owner := businessconsole.Access{BusinessID: f.business, ActingPersonID: b.accountIDs[0], SessionDigest: digest0}
	reviewer := businessconsole.Access{BusinessID: f.business, ActingPersonID: b.accountIDs[1], SessionDigest: digest1}
	if _, e = b.store.SubmitBusinessClaim(b.ctx, owner, businessconsole.ClaimInput{Name: "合成赞助审核商家", Description: "无现实付款/合同核验", SourceURL: "https://qa.example/business", RightsNote: "本地合成经营权声明"}); e != nil {
		t.Fatal(e)
	}
	var principal string
	if e = b.pool.QueryRow(b.ctx, `SELECT account_id FROM businesses WHERE id=$1`, f.business).Scan(&principal); e != nil {
		t.Fatal(e)
	}
	b.accountIDs = append(b.accountIDs, principal)
	b.exec(`INSERT INTO business_review_grants(business_id,reviewer_account_id,permissions,valid_from,valid_until,state,provisioned_by,provision_note) VALUES($1,$2,ARRAY['claim','venue'],clock_timestamp()-interval '1 minute',clock_timestamp()+interval '1 hour','active',$3,'CONTROLLED_LOCAL_SYNTHETIC_ONLY')`, f.business, b.accountIDs[1], b.accountIDs[0])
	if _, e = b.store.ReviewBusinessClaim(b.ctx, reviewer, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "独立合成声明审核"}); e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'合成赞助HTTP验收城市','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:ADS001','合成维护者')`, f.city)
	// Real descriptive CityContext supplies the typed native task FK. This is
	// not a Person location, machine-purpose grant or sponsored approval.
	b.exec(`INSERT INTO city_contexts(city_id) VALUES($1) ON CONFLICT(city_id) DO NOTHING`, f.city)
	b.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES($1,$2,'reviewer')`, f.city, b.accountIDs[1])
	b.exec(`INSERT INTO sponsored_opportunity_review_grants(city_id,reviewer_account_id,permission,state,valid_from,valid_until,provisioned_by,provision_note) VALUES($1,$2,'review_sponsorship','active',clock_timestamp()-interval '1 minute',clock_timestamp()+interval '1 hour',$3,'CONTROLLED_LOCAL_SYNTHETIC_ONLY')`, f.city, b.accountIDs[1], b.accountIDs[0])
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),$1,'合成公开赞助场地','sports','published','LOCAL_SYNTHETIC_FIXTURE','local:ADS001','合成维护者') RETURNING id`, f.city).Scan(&f.place); e != nil {
		t.Fatal(e)
	}
	var operator string
	if e = b.pool.QueryRow(b.ctx, `SELECT id FROM organizations WHERE account_id=$1`, b.accountIDs[2]).Scan(&operator); e != nil {
		t.Fatal(e)
	}
	if e = b.pool.QueryRow(b.ctx, `INSERT INTO venue_candidates(place_id,city_id,submitted_by,capacity,reservation_support,source_url,rights_note,expires_at,status,reviewed_by,reviewed_at,operator_organization_id) VALUES($1,$2,$3,24,'contact','https://qa.example/venue','原公开场地合成fixture，不是赞助批准',clock_timestamp()+interval '1 hour','approved',$4,clock_timestamp(),$5) RETURNING id`, f.place, f.city, b.accountIDs[0], b.accountIDs[1], operator).Scan(&f.venueCandidate); e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO venues(place_id,city_id,capacity,reservation_support,suitability,source_candidate_id,source_url,reviewed_by,reviewed_at,expires_at,operator_organization_id) VALUES($1,$2,24,'contact',ARRAY['badminton'],$3,'https://qa.example/venue',$4,clock_timestamp(),clock_timestamp()+interval '1 hour',$5)`, f.place, f.city, f.venueCandidate, b.accountIDs[1], operator)
	if _, e = b.store.PutBusinessVenueFacts(b.ctx, owner, f.place, businessconsole.VenueInput{Facts: businessconsole.VenueFacts{Suitability: []string{"羽毛球"}}, SourceURL: "https://qa.example/operation", RightsNote: "本地合成经营权声明", ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ReviewBusinessVenueFacts(b.ctx, reviewer, f.place, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "明确合成经营关系审核"}); e != nil {
		t.Fatal(e)
	}
	start := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	draft, e := b.store.CreateSocialDraft(b.ctx, b.accountIDs[0], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "BUSINESS", ID: f.business}, CityID: f.city, PlaceID: f.place, Title: "合成公开赞助活动", Summary: "合成本地仅验收", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Europe/London", Visibility: "public", Modality: "in_person", PhysicalPlaceStatus: "confirmed"})
	if e != nil {
		t.Fatal(e)
	}
	f.activity = draft.ID
	if _, e = b.store.PublishSocialActivity(b.ctx, b.accountIDs[0], draft.ID); e != nil {
		t.Fatal(e)
	}
	b.handler = New(b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, b.store, nil, false, nil, b.pool, nil)
	return f
}
func (f *sponsorHTTPFixture) management(t *testing.T) so.Management {
	w := f.base.request(t, f.base.handler, "GET", "/v1/businesses/"+f.business+"/sponsored-opportunities", "", f.base.tokens[0], 200, nil)
	var reply struct {
		Data so.Management `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil {
		t.Fatal(w.Body.String())
	}
	return reply.Data
}
func (f *sponsorHTTPFixture) submit(t *testing.T, kind string) so.Declaration {
	t.Helper()
	b := f.base
	m := f.management(t)
	var target so.EligibleTarget
	for _, x := range m.Targets {
		if x.Target.Type == kind {
			target = x
		}
	}
	if target.SourceSnapshot == "" {
		t.Fatal("current source absent", kind, m)
	}
	var op string
	var at time.Time
	if b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid(),clock_timestamp()`).Scan(&op, &at) != nil {
		t.Fatal("native source clock")
	}
	input := so.SubmitInput{OperationID: op, CityID: f.city, TargetType: kind, TargetID: target.Target.ID, Statement: "明确本地合成商业展示声明，不含真实付款合同证明", SourceURL: "https://qa.example/sponsor", RightsNote: "PRIVATE_RIGHTS_NOTE_NOT_FOR_PUBLIC", ObservedAt: at.Add(-time.Minute).UTC().Truncate(time.Microsecond), ExpiresAt: at.Add(30 * time.Minute).UTC().Truncate(time.Microsecond), SourceSnapshot: target.SourceSnapshot, Confirmed: true}
	raw, _ := json.Marshal(input)
	route := "/v1/businesses/" + f.business + "/sponsored-opportunities"
	for _, i := range []int{1, 2, 3} {
		b.request(t, b.handler, "POST", route, string(raw), b.tokens[i], 403, nil)
	}
	b.request(t, b.handler, "POST", route, string(raw), "", 401, nil)
	w := b.request(t, b.handler, "POST", route, string(raw), b.tokens[0], 200, nil)
	var response struct {
		Data    so.Declaration `json:"data"`
		Created bool           `json:"created"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || !response.Created || response.Data.Status != "pending" {
		t.Fatal(w.Body.String())
	}
	b.request(t, b.handler, "POST", route, string(raw), b.tokens[0], 200, nil)
	return response.Data
}
func (f *sponsorHTTPFixture) review(t *testing.T, d so.Declaration) so.Declaration {
	t.Helper()
	b := f.base
	path := "/v1/cities/" + f.city + "/sponsored-opportunity-review"
	w := b.request(t, b.handler, "GET", path, "", b.tokens[1], 200, nil)
	var items struct {
		Data []so.Declaration `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &items) != nil {
		t.Fatal(w.Body.String())
	}
	for _, x := range items.Data {
		if x.ID == d.ID {
			d = x
		}
	}
	input := so.ReviewInput{ExpectedRevision: d.Revision, Snapshot: d.Snapshot, Decision: "approve", Note: "明确独立审核本地商业声明，不核验付款", Confirmed: true}
	raw, _ := json.Marshal(input)
	route := "/v1/sponsored-opportunities/" + d.ID + "/review"
	for _, i := range []int{0, 2, 3} {
		b.request(t, b.handler, "POST", route, string(raw), b.tokens[i], 403, nil)
	}
	w = b.request(t, b.handler, "POST", route, string(raw), b.tokens[1], 200, nil)
	var response struct {
		Data so.Declaration `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data.Revision != 2 || response.Data.Status != "approved" {
		t.Fatal(w.Body.String())
	}
	return response.Data
}
func (f *sponsorHTTPFixture) consumer(t *testing.T, method, path, body string) map[string]any {
	w := f.base.request(t, f.base.handler, method, path, body, f.base.tokens[0], 200, nil)
	var e map[string]any
	if json.Unmarshal(w.Body.Bytes(), &e) != nil {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), "PRIVATE_RIGHTS_NOTE_NOT_FOR_PUBLIC") || strings.Contains(w.Body.String(), "reviewedBy") {
		t.Fatal("private declaration leaked to public consumer")
	}
	return e
}
func TestSponsoredHTTPRegisteredLifecycleAndOrganicInvariance(t *testing.T) {
	f := sponsorHTTPNative(t)
	b := f.base
	// Native original intent/domain, not an invented sponsor ranking input.
	constraints, _ := json.Marshal(map[string]any{"placeId": f.place, "category": "badminton"})
	intent, e := b.store.CreateSocialIntentDraft(b.ctx, b.accountIDs[0], socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "合成自然找活动意图", Constraints: constraints, Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ActivateSocialIntent(b.ctx, b.accountIDs[0], intent.ID); e != nil {
		t.Fatal(e)
	}
	opp := f.consumer(t, "GET", "/v1/me/opportunities", "")
	match := f.consumer(t, "GET", "/v1/me/social-intents/"+intent.ID+"/place-matches", "")
	now := f.consumer(t, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找羽毛球活动"}`)
	placeNow := f.consumer(t, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"找地点"}`)
	if len(opp["data"].([]any)) == 0 || len(match["data"].([]any)) == 0 || len(now["data"].(map[string]any)["activities"].([]any)) == 0 || len(placeNow["data"].(map[string]any)["places"].([]any)) == 0 {
		t.Fatal("actual natural positive supply missing", opp, match, now, placeNow)
	}
	dA := f.review(t, f.submit(t, "ACTIVITY"))
	dP := f.review(t, f.submit(t, "PLACE"))
	afterOpp := f.consumer(t, "GET", "/v1/me/opportunities", "")
	afterMatch := f.consumer(t, "GET", "/v1/me/social-intents/"+intent.ID+"/place-matches", "")
	if !reflect.DeepEqual(opp["data"], afterOpp["data"]) || !reflect.DeepEqual(match["data"], afterMatch["data"]) {
		t.Fatal("Sponsor changed independently produced natural ranking")
	}
	for _, x := range []map[string]any{afterOpp, afterMatch} {
		if x["commercialTrustVersion"] != so.Version || x["sponsoredStatus"] != "available" || len(x["sponsoredOpportunities"].([]any)) != 1 {
			t.Fatal("real independent lane absent", x)
		}
	}
	// Restore the actual persisted task; comparing a new task's IDs/times would
	// incorrectly call the original ResultSet identity drift a ranking change.
	compareNow := func(old map[string]any, kind string) {
		data := old["data"].(map[string]any)
		task := data["task"].(map[string]any)
		restored := f.consumer(t, "GET", "/v1/me/agent-tasks/"+task["id"].(string), "")["data"].(map[string]any)
		for _, field := range []string{"activities", "places", "people", "groups", "organizations", "mapEffects"} {
			if !reflect.DeepEqual(data[field], restored[field]) {
				t.Fatal("Sponsor changed natural Now field", field)
			}
		}
		oldSet := data["resultSet"].(map[string]any)
		newSet := restored["resultSet"].(map[string]any)
		if !reflect.DeepEqual(oldSet["entities"], newSet["entities"]) || oldSet["taskId"] != newSet["taskId"] {
			t.Fatal("Sponsor changed ResultSet refs/task")
		}
		items := restored["sponsoredOpportunities"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["target"].(map[string]any)["type"] != kind {
			t.Fatal("Now lane missing", restored)
		}
	}
	compareNow(now, "ACTIVITY")
	compareNow(placeNow, "PLACE")
	// A real stable source persists through a new Store+HTTP constructor. This is
	// not an App/API process kill/restart or a production ad delivery assertion.
	restarted := postgres.New(b.pool, false)
	oldHandler := b.handler
	b.handler = New(restarted, restarted, restarted, restarted, restarted, restarted, restarted, restarted, restarted, restarted, restarted, nil, false, nil, b.pool, nil)
	if len(f.consumer(t, "GET", "/v1/me/opportunities", "")["sponsoredOpportunities"].([]any)) != 1 {
		t.Fatal("native persistence missing")
	}
	b.handler = oldHandler
	for _, d := range []so.Declaration{dA, dP} {
		m := f.management(t)
		for _, x := range m.Declarations {
			if x.ID == d.ID {
				d = x
			}
		}
		raw, _ := json.Marshal(so.ReviewInput{ExpectedRevision: d.Revision, Snapshot: d.Snapshot, Decision: "revoke", Note: "明确撤回合成展示", Confirmed: true})
		w := b.request(t, b.handler, "POST", "/v1/sponsored-opportunities/"+d.ID+"/revoke", string(raw), b.tokens[0], 200, nil)
		if !strings.Contains(w.Body.String(), `"status":"revoked"`) {
			t.Fatal(w.Body.String())
		}
	}
	finalOpp := f.consumer(t, "GET", "/v1/me/opportunities", "")
	finalMatch := f.consumer(t, "GET", "/v1/me/social-intents/"+intent.ID+"/place-matches", "")
	if len(finalOpp["sponsoredOpportunities"].([]any)) != 0 || len(finalMatch["sponsoredOpportunities"].([]any)) != 0 || !reflect.DeepEqual(opp["data"], finalOpp["data"]) || !reflect.DeepEqual(match["data"], finalMatch["data"]) {
		t.Fatal("revoke altered organic paths or retained sponsor")
	}
}
