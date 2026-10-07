package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func TestBusinessNotificationRegisteredNativeReviewInboxAndPrivateTarget(t *testing.T) {
	f := businessConsoleHTTPNew(t)
	var agent string
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) RETURNING id`, f.ids[0]).Scan(&agent); e != nil {
		t.Fatal(e)
	}
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: f.ids[0]}
	store := postgres.New(f.pool, false)
	if _, e := store.EnsureAgentProfile(f.ctx, agent, owner); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256([]byte(f.tokens[0]))
	if _, e := store.PutOwnNotificationPolicy(f.ctx, agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: owner}, agentnotification.PutInput{Enabled: true, DefaultRoute: agentnotification.Normal, Rules: []agentnotification.Rule{}, ExpiresAt: now.UTC().Add(time.Hour).Truncate(time.Microsecond)}); e != nil {
		t.Fatal(e)
	}
	f.claim(t) // Actual registered PUT, self-review403, independent native reviewer POST200.
	var response struct {
		Data []inbox.Item `json:"data"`
	}
	w := f.request(t, "GET", "/v1/me/inbox", f.tokens[0], "", 200)
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if len(response.Data) != 1 {
		t.Fatal("native review not exactly one Inbox", len(response.Data))
	}
	item := response.Data[0]
	if item.ResourceType != "business_claim_review" || item.SemanticCategory != "BUSINESS" || item.TargetBusinessID == nil || *item.TargetBusinessID != f.business || item.ResourceID == f.business {
		t.Fatal("untyped/misbound private target", item)
	}
	for _, token := range []string{f.tokens[1], f.tokens[2], f.tokens[3]} {
		w = f.request(t, "GET", "/v1/me/inbox", token, "", 200)
		if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil || len(response.Data) != 0 {
			t.Fatal("other Person sees recipient Inbox", e, len(response.Data))
		}
		f.request(t, "POST", "/v1/me/inbox/"+item.ID+"/read", token, "", 404)
	}
	f.request(t, "GET", "/v1/me/inbox", "", "", 401)
	f.request(t, "POST", "/v1/me/inbox/"+item.ID+"/read", f.tokens[0], "", 200)
	// The notification follows the stable, current private management resource.
	f.request(t, "GET", f.path("/console"), f.tokens[0], "", 200)
	f.request(t, "GET", f.path("/console"), f.tokens[2], "", 403)
	review := businessConsoleHTTPJSON(t, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "独立角色核验合成材料"})
	for range 100 {
		f.request(t, "POST", f.path("/claim/review"), f.tokens[1], review, 200)
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM native_notification_decisions WHERE business_claim_id=$1`, f.business).Scan(&count); e != nil || count != 1 {
		t.Fatal("registered CAS duplicated native effect", count, e)
	}
	f.exec(t, `UPDATE business_memberships SET role='member' WHERE business_id=$1 AND user_account_id=$2`, f.business, f.ids[0])
	f.request(t, "GET", f.path("/console"), f.tokens[0], "", 403)
	w = f.request(t, "GET", "/v1/me/inbox", f.tokens[0], "", 200)
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil || len(response.Data) != 0 {
		t.Fatal("removed management authority disclosed old notification", e, len(response.Data))
	}
	f.request(t, "POST", "/v1/me/inbox/"+item.ID+"/read", f.tokens[0], "", 404)
}
