package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentbusiness"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func businessKnowledgeHTTPNative(t *testing.T) (*privateProfileHTTPDBFixture, string, businessconsole.Access) {
	t.Helper()
	f := privateProfileHTTPDBNew(t)
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		var principal string
		if e := f.pool.QueryRow(ctx, `SELECT account_id FROM businesses WHERE id=$1`, id).Scan(&principal); e != nil {
			t.Error(e)
			return
		}
		for _, table := range []string{"business_console_audit_events", "business_console_membership_controls", "business_console_venue_facts", "business_console_profiles", "business_claim_controls", "business_review_grants", "business_venue_relations", "business_memberships"} {
			if _, e := f.pool.Exec(ctx, `DELETE FROM `+table+` WHERE business_id=$1`, id); e != nil {
				t.Error(e)
			}
		}
		if _, e := f.pool.Exec(ctx, `DELETE FROM businesses WHERE id=$1`, id); e != nil {
			t.Error(e)
		}
		if _, e := f.pool.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, principal); e != nil {
			t.Error(e)
		}
	})
	digest, e := identity.ParseBearer("Bearer " + f.tokens[0])
	if e != nil {
		t.Fatal(e)
	}
	a := businessconsole.Access{SessionDigest: digest, ActingPersonID: f.accountIDs[0], BusinessID: id}
	if _, e = f.store.SubmitBusinessClaim(f.ctx, a, businessconsole.ClaimInput{Name: "HTTP合成商家", SourceURL: "https://example.invalid/claim", RightsNote: "本地合成经营声明"}); e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO business_review_grants(business_id,reviewer_account_id,permissions,valid_from,valid_until,state,provisioned_by,provision_note) VALUES($1,$2,ARRAY['claim','profile'],clock_timestamp()-interval '1 minute',clock_timestamp()+interval '1 hour','active',$3,'本地受信审核角色配置')`, id, f.accountIDs[1], f.accountIDs[0])
	digest, e = identity.ParseBearer("Bearer " + f.tokens[1])
	if e != nil {
		t.Fatal(e)
	}
	reviewer := businessconsole.Access{SessionDigest: digest, ActingPersonID: f.accountIDs[1], BusinessID: id}
	review := businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "受控合成资料审核，不代表现实核验"}
	if _, e = f.store.ReviewBusinessClaim(f.ctx, reviewer, review); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e = f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.PutBusinessProfile(f.ctx, a, businessconsole.ProfileInput{Facts: businessconsole.ProfileFacts{Name: "合成商家", Description: "本人受控资料", TimeZone: "Europe/London", OpeningHours: []businessconsole.HoursDay{}, OfficialLinks: []string{}}, SourceURL: "https://example.invalid/profile", RightsNote: "本地声明", ValidUntil: now.UTC().Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.ReviewBusinessProfile(f.ctx, reviewer, review); e != nil {
		t.Fatal(e)
	}
	return f, id, a
}
func businessKnowledgeHTTPRequest(h http.Handler, id, token, query string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(agentbusiness.Query{Query: query})
	r := httptest.NewRequest(http.MethodPost, "/v1/me/businesses/"+id+"/knowledge/ask", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestBusinessKnowledgeRegisteredHTTPNative(t *testing.T) {
	f, id, _ := businessKnowledgeHTTPNative(t)
	// Human management keeps working without a personal Agent/Profile.
	f.exec(`DELETE FROM agents WHERE principal_account_id=$1`, f.accountIDs[0])
	h := privateProfileHTTPNew(f.store, f.store)
	for _, tc := range []struct {
		label, token string
		status       int
	}{{"owner", f.tokens[0], 200}, {"reviewer", f.tokens[1], 403}, {"organization", f.tokens[2], 403}, {"business", f.tokens[3], 403}, {"anonymous", "", 401}, {"revoked", f.newSession(f.accountIDs[0], false, true), 401}} {
		t.Run(tc.label, func(t *testing.T) {
			w := businessKnowledgeHTTPRequest(h, id, tc.token, "商家介绍")
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}
			if tc.status == 200 {
				var response struct {
					Data agentbusiness.Answer `json:"data"`
				}
				if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Data.Status != "known" || response.Data.Answer != "本人受控资料" || len(response.Data.Tools) != 0 || response.Data.ModelStatus != "unavailable" {
					t.Fatal(w.Body.String())
				}
			}
		})
	}
	w := businessKnowledgeHTTPRequest(h, id, f.tokens[0], "客人的个人记忆")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"unknown"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

// Hook is only a deterministic barrier. Both actual reads and final guards use
// the real Store, actual SQL revocation/version changes, and registered route.
type businessKnowledgeNativeBarrier struct {
	*postgres.Store
	validations int
	afterJSON   func()
}

func (b *businessKnowledgeNativeBarrier) ValidateOwnBusinessKnowledge(ctx context.Context, a businessconsole.Access, v string) error {
	b.validations++
	if b.validations == 2 && b.afterJSON != nil {
		b.afterJSON()
	}
	return b.Store.ValidateOwnBusinessKnowledge(ctx, a, v)
}
func TestBusinessKnowledgeRegisteredHTTPNativeAfterJSON(t *testing.T) {
	for _, change := range []string{"session", "source", "membership"} {
		t.Run(change, func(t *testing.T) {
			f, id, a := businessKnowledgeHTTPNative(t)
			barrier := &businessKnowledgeNativeBarrier{Store: f.store, afterJSON: func() {
				switch change {
				case "session":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
				case "source":
					f.exec(`UPDATE business_console_profiles SET version=version+1,state='revoked',review_note='当前source撤销' WHERE business_id=$1`, id)
				case "membership":
					f.exec(`UPDATE business_memberships SET status='removed' WHERE business_id=$1 AND user_account_id=$2`, id, a.ActingPersonID)
				}
			}}
			w := businessKnowledgeHTTPRequest(privateProfileHTTPNew(barrier, f.store), id, f.tokens[0], "商家介绍")
			expected := 409
			if change == "session" {
				expected = 401
			}
			if change == "membership" {
				expected = 403
			}
			if w.Code != expected || strings.Contains(w.Body.String(), "本人受控资料") {
				t.Fatal(w.Code, w.Body.String())
			}
			if barrier.validations != 2 {
				t.Fatal("materialization barrier not reached", barrier.validations)
			}
		})
	}
}
