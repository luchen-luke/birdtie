package httpapi

import (
	"context"
	"encoding/json"
	ai "github.com/birdtie/birdtie/apps/api/internal/agentintroduction"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"strings"
	"testing"
	"time"
)

func TestIntroductionHTTPNativeRegisteredPublicReadAndOwnBoundary(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	t.Cleanup(func() {
		for _, sql := range []string{`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, `DELETE FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(context.Background(), sql, f.accountIDs); e != nil {
				t.Error(e)
			}
		}
	})
	var sources []socialintent.Record
	for i := 0; i < 2; i++ {
		f.request(t, f.handler, "PUT", "/v1/me/new-people/consent", `{"enabled":true}`, f.tokens[i], 200, nil)
		raw := `{"expectedVersion":0,"settings":{"rules":[{"category":"UNKNOWN_PERSON","preference":"REVIEW_REQUIRED"}]},"expiresAt":"` + time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour).Format(time.RFC3339Nano) + `"}`
		f.request(t, f.handler, "PUT", "/v1/me/agent-policies/social", raw, f.tokens[i], 200, nil)
		d, e := f.store.CreateNewPeopleIntent(f.ctx, f.accountIDs[i], newpeople.DraftInput{Title: "PRIVATE_TITLE_NOT_PROJECTED", Category: "badminton", Modality: "ONLINE", ExpiresAt: time.Now().UTC().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		d, e = f.store.ActivateSocialIntent(f.ctx, f.accountIDs[i], d.ID)
		if e != nil {
			t.Fatal(e)
		}
		sources = append(sources, d)
	}
	path := "/v1/me/agent-introductions?sourceIntentId=" + sources[0].ID
	var before int64
	sql := `SELECT (SELECT count(*) FROM audit_events WHERE actor_account_id=ANY($1::uuid[]))+(SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[]))+(SELECT count(*) FROM agent_memories WHERE owner_id=ANY($1::uuid[]))+(SELECT count(*) FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]))`
	if e := f.pool.QueryRow(f.ctx, sql, f.accountIDs).Scan(&before); e != nil {
		t.Fatal(e)
	}
	w := f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
	var decoded struct {
		Data ai.Response `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &decoded); e != nil || ai.ValidateResponse(decoded.Data, sources[0].ID) != nil || len(decoded.Data.Candidates) != 1 || len(decoded.Data.Candidates[0].Basis) != 1 {
		t.Fatal("actual registered human suggestion", e, w.Body.String())
	}
	for _, v := range []string{"PRIVATE_TITLE_NOT_PROJECTED", "nativeRevision", "rules", "preferences", "rowToken"} {
		if strings.Contains(w.Body.String(), v) {
			t.Fatal("private settings/source leak", v)
		}
	}
	var after int64
	if e := f.pool.QueryRow(f.ctx, sql, f.accountIDs).Scan(&after); e != nil || after != before {
		t.Fatal("registered read wrote effects", before, after, e)
	}
	f.request(t, f.handler, "GET", path, "", f.tokens[1], 403, nil)
	f.request(t, f.handler, "GET", path, "", f.tokens[2], 403, nil)
	f.request(t, f.handler, "GET", path, "", f.tokens[0], 403, &f.accountIDs[2])
	f.request(t, f.handler, "GET", path+"&sourceBinding="+decoded.Data.Candidates[0].SourceBinding, "", f.tokens[0], 400, nil)
	f.request(t, f.handler, "PUT", "/v1/me/new-people/consent", `{"enabled":false}`, f.tokens[1], 200, nil)
	w = f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
	if json.Unmarshal(w.Body.Bytes(), &decoded) != nil || len(decoded.Data.Candidates) != 0 {
		t.Fatal("revoked mutual opt-in still emitted peer")
	}
	f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
	f.request(t, f.handler, "GET", path, "", f.tokens[0], 401, nil)
}
