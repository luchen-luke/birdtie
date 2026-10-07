package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
)

type placeSemanticHTTPFixture struct {
	base        *privateProfileHTTPDBFixture
	city, place string
	at          time.Time
}

func semanticHTTPNative(t *testing.T) *placeSemanticHTTPFixture {
	t.Helper()
	f := &placeSemanticHTTPFixture{base: privateProfileHTTPDBNew(t)}
	b := f.base
	var id string
	if e := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid(),clock_timestamp()`).Scan(&id, &f.at); e != nil {
		t.Fatal(e)
	}
	f.city = "semantic002-" + strings.ReplaceAll(id, "-", "")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM place_semantic_profiles WHERE city_id=$1`, `DELETE FROM place_semantic_candidates WHERE city_id=$1`, `DELETE FROM venues WHERE city_id=$1`, `DELETE FROM venue_candidates WHERE city_id=$1`, `DELETE FROM social_intents WHERE creator_account_id IN (SELECT account_id FROM city_editor_memberships WHERE city_id=$1)`, `DELETE FROM city_editor_memberships WHERE city_id=$1`, `DELETE FROM places WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`} {
			if _, e := b.pool.Exec(ctx, q, f.city); e != nil {
				t.Error("owned semantic HTTP cleanup", e)
			}
		}
	})
	if _, e := b.pool.Exec(b.ctx, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'本地合成语义城市','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:PLC002','合成维护者')`, f.city); e != nil {
		t.Fatal(e)
	}
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,summary,publication_status,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),$1,'本地合成球馆','sports','PUBLIC_SYNTHETIC_ONLY','published','LOCAL_SYNTHETIC_FIXTURE','local:PLC002','合成维护者') RETURNING id`, f.city).Scan(&f.place); e != nil {
		t.Fatal(e)
	}
	for _, id := range b.accountIDs[:2] {
		if _, e := b.pool.Exec(b.ctx, `INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES($1,$2,'reviewer')`, f.city, id); e != nil {
			t.Fatal(e)
		}
	}
	return f
}
func (f *placeSemanticHTTPFixture) input(t *testing.T) pp.SubmitInput {
	t.Helper()
	var op string
	if e := f.base.pool.QueryRow(f.base.ctx, `SELECT gen_random_uuid()`).Scan(&op); e != nil {
		t.Fatal(e)
	}
	return pp.SubmitInput{OperationID: op, Facts: pp.Facts{GoodFor: []string{"badminton"}, GroupSize: &pp.GroupSize{Min: 2, Max: 12}}, Source: pp.SourceInput{Label: "本地合成审核资料", URL: "https://example.invalid/semantic", RightsNote: "明确虚构的本地测试来源，无真实运营授权", ObservedAt: f.at.Add(-time.Minute), ExpiresAt: f.at.Add(time.Hour)}, Confidence: pp.Assessment{Kind: "EDITOR_ASSESSMENT_UNCALIBRATED", Level: "MEDIUM"}}
}
func TestPlaceSemanticHTTPRegisteredNativeLifecycle(t *testing.T) {
	f := semanticHTTPNative(t)
	b := f.base
	profile := "/v1/places/" + f.place + "/profile"
	submit := "/v1/cities/" + f.city + "/places/" + f.place + "/profile-candidates"
	list := "/v1/cities/" + f.city + "/place-profile-candidates"
	in := f.input(t)
	raw, _ := json.Marshal(in)
	b.request(t, b.handler, "GET", profile, "", "", 404, nil)
	for _, index := range []int{2, 3} {
		b.request(t, b.handler, "POST", submit, string(raw), b.tokens[index], 403, nil)
	}
	w := b.request(t, b.handler, "POST", submit, string(raw), b.tokens[0], 201, nil)
	var response struct {
		Data pp.Candidate `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data.Status != "pending" {
		t.Fatal(w.Body.String())
	}
	c := response.Data
	b.request(t, b.handler, "POST", submit, string(raw), b.tokens[0], 200, nil)
	b.request(t, b.handler, "GET", list, "", b.tokens[1], 200, nil)
	decision, _ := json.Marshal(pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: 0, Note: "明确独立审核这个版本合成资料"})
	review := "/v1/place-profile-candidates/" + c.ID + "/review"
	b.request(t, b.handler, "POST", review, string(decision), b.tokens[0], 409, nil)
	b.request(t, b.handler, "POST", review, string(decision), b.tokens[1], 200, nil)
	receipt := b.request(t, b.handler, "POST", submit, string(raw), b.tokens[0], 200, nil)
	if !strings.Contains(receipt.Body.String(), `"publication":"APPROVED_CANDIDATE_RECEIPT"`) {
		t.Fatal("approved candidate receipt claimed a new pending review", receipt.Body.String())
	}
	w = b.request(t, b.handler, "GET", profile, "", "", 200, nil)
	capacity := 20
	v, e := b.store.SubmitVenueCandidate(b.ctx, b.accountIDs[0], f.city, f.place, venue.SubmitInput{Facts: venue.Facts{Capacity: &capacity, ReservationSupport: "contact", Suitability: []string{"badminton"}, Amenities: []string{}}, SourceURL: "https://example.invalid/synthetic-venue", RightsNote: "本地明确合成审核场地，不是实际运营资料", ExpiresAt: f.at.Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ReviewVenueCandidate(b.ctx, b.accountIDs[1], v.ID, venue.ReviewInput{Decision: "approve", Note: "独立审核本地合成场地资料"}); e != nil {
		t.Fatal(e)
	}
	constraints, _ := json.Marshal(map[string]any{"placeId": f.place, "category": "badminton", "minParticipants": 8, "maxParticipants": 10})
	intent, e := b.store.CreateSocialIntentDraft(b.ctx, b.accountIDs[0], socialintent.DraftInput{Type: "ORGANIZE", Title: "本地合成意图", Audience: "PRIVATE", Modality: "IN_PERSON", Constraints: constraints, ExpiresAt: f.at.Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ActivateSocialIntent(b.ctx, b.accountIDs[0], intent.ID); e != nil {
		t.Fatal(e)
	}
	matchPath := "/v1/me/social-intents/" + intent.ID + "/place-matches"
	match := b.request(t, b.handler, "GET", matchPath, "", b.tokens[0], 200, nil)
	if !strings.Contains(match.Body.String(), `"semanticScore":5`) || !strings.Contains(match.Body.String(), `"semanticProfile"`) {
		t.Fatal("native semantic not consumed", match.Body.String())
	}
	b.request(t, b.handler, "GET", matchPath, "", b.tokens[1], 404, nil)
	for _, bad := range []string{"rightsNote", "submittedBy", "reviewedBy", "latitude", "token_sha256"} {
		if strings.Contains(w.Body.String(), bad) {
			t.Fatal("private data", bad)
		}
	}
	if !strings.Contains(w.Body.String(), `"state":"UNKNOWN"`) {
		t.Fatal("unknown hidden")
	}
	b.request(t, b.handler, "POST", review, string(decision), b.tokens[1], 409, nil)
	wbody, _ := json.Marshal(pp.WithdrawInput{ExpectedVersion: 1, Note: "明确撤回这个公开资料版本"})
	b.request(t, b.handler, "POST", profile+"/withdraw", string(wbody), b.tokens[1], 200, nil)
	b.request(t, b.handler, "GET", profile, "", "", 404, nil)
	// Real registered paths, no manual test mux. Constructor restart preserves withdrawal.
	b.request(t, privateProfileHTTPNew(b.store, b.store), "GET", profile, "", "", 404, nil)
}

func TestPlaceSemanticHTTPRegisteredLateSession(t *testing.T) {
	for _, action := range []string{"submit", "match"} {
		for _, mode := range []string{"revoke", "expiry"} {
			t.Run(action+"_"+mode, func(t *testing.T) {
				f := semanticHTTPNative(t)
				b := f.base
				method, path, body := "POST", "/v1/cities/"+f.city+"/places/"+f.place+"/profile-candidates", ""
				if action == "submit" {
					raw, _ := json.Marshal(f.input(t))
					body = string(raw)
				} else {
					constraints, _ := json.Marshal(map[string]any{"placeId": f.place})
					i, e := b.store.CreateSocialIntentDraft(b.ctx, b.accountIDs[0], socialintent.DraftInput{Type: "ORGANIZE", Title: "合成当前检索意图", Audience: "PRIVATE", Modality: "IN_PERSON", Constraints: constraints, ExpiresAt: f.at.Add(time.Hour)})
					if e != nil {
						t.Fatal(e)
					}
					if _, e = b.store.ActivateSocialIntent(b.ctx, b.accountIDs[0], i.ID); e != nil {
						t.Fatal(e)
					}
					method, path = "GET", "/v1/me/social-intents/"+i.ID+"/place-matches"
				}
				digest, e := identity.ParseBearer("Bearer " + b.tokens[0])
				if e != nil {
					t.Fatal(e)
				}
				if mode == "expiry" {
					b.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds',idle_expires_at=clock_timestamp()+interval '1400 milliseconds' WHERE token_sha256=$1`, digest[:])
				}
				beforePublic, beforeCognitive := profileAPIsSnapshot(t, b, false), profileAPIsSnapshot(t, b, true)
				blocker, e := b.pool.Begin(b.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer blocker.Rollback(context.Background())
				var pid int
				if e = blocker.QueryRow(b.ctx, `SELECT pg_backend_pid() FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, b.accountIDs[0]).Scan(&pid); e != nil {
					t.Fatal(e)
				}
				done := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					w := httptest.NewRecorder()
					b.handler.ServeHTTP(w, privateProfileHTTPRequest(method, path, body, b.tokens[0], "application/json"))
					done <- w
				}()
				profileAPIsWaitBlocked(t, b, pid)
				if mode == "revoke" {
					if e = b.store.RevokeSession(b.ctx, digest); e != nil {
						t.Fatal(e)
					}
				} else {
					var expired bool
					deadline := time.Now().Add(4 * time.Second)
					for !expired && time.Now().Before(deadline) {
						if e = b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired); e != nil {
							t.Fatal(e)
						}
						if !expired {
							time.Sleep(15 * time.Millisecond)
						}
					}
					if !expired {
						t.Fatal("native session did not expire")
					}
				}
				if e = blocker.Commit(b.ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case w := <-done:
					var count int
					if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM place_semantic_candidates WHERE city_id=$1`, f.city).Scan(&count); e != nil {
						t.Fatal(e)
					}
					if w.Code != 403 || count != 0 || beforePublic != profileAPIsSnapshot(t, b, false) || beforeCognitive != profileAPIsSnapshot(t, b, true) {
						t.Fatal("late native HTTP authority permitted stale session or changed source", w.Code, count, w.Body.String())
					}
					t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY action=%s mode=%s registered_status=%d complete_owned_public_and_cognitive_unchanged=true", action, mode, w.Code)
				case <-time.After(6 * time.Second):
					t.Fatal("native registered request did not finish")
				}
			})
		}
	}
}
func TestPlaceSemanticHTTPRegisteredMalformedAndCurrentAuthorization(t *testing.T) {
	f := semanticHTTPNative(t)
	b := f.base
	path := fmt.Sprintf("/v1/cities/%s/places/%s/profile-candidates", f.city, f.place)
	in := f.input(t)
	raw, _ := json.Marshal(in)
	for _, bad := range []string{`null`, `{"ownerId":"foreign"}`, strings.Replace(string(raw), `"facts"`, `"Facts"`, 1), strings.Replace(string(raw), `"expectedVersion":0`, `"expectedVersion":0,"expectedVersion":0`, 1)} {
		b.request(t, b.handler, http.MethodPost, path, bad, b.tokens[0], 400, nil)
	}
	b.request(t, b.handler, "POST", path, string(raw), "", 401, nil)
	workspace := "foreign"
	b.request(t, b.handler, "POST", path, string(raw), b.tokens[0], 400, &workspace)
	if _, e := b.pool.Exec(b.ctx, `UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1 AND account_id=$2`, f.city, b.accountIDs[0]); e != nil {
		t.Fatal(e)
	}
	b.request(t, b.handler, "POST", path, string(raw), b.tokens[0], 403, nil)
	var count int
	if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM place_semantic_candidates WHERE city_id=$1`, f.city).Scan(&count); e != nil || count != 0 {
		t.Fatal("denied writes", count, e)
	}
}
