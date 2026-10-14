package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"net/http/httptest"
	"strings"
)

func TestVisibilityMatrixHTTPNativeRegisteredAudienceEditAndACL(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	for i := 0; i < 2; i++ {
		if _, e := f.store.EnsureAgentProfile(f.ctx, f.agentIDs[i], actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[i]}); e != nil {
			t.Fatal(e)
		}
	}
	f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=ANY($1::uuid[])`, f.accountIDs[:2])
	if _, e := f.store.Follow(f.ctx, f.accountIDs[1], follow.Target{Type: follow.Person, ID: f.accountIDs[0]}); e != nil {
		t.Fatal(e)
	}
	r, e := f.store.CreateFriendRequest(f.ctx, f.accountIDs[0], f.accountIDs[1], "明确本地合成好友")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.DecideRequest(f.ctx, f.accountIDs[1], r.ID, "accept"); e != nil {
		t.Fatal(e)
	}
	access := ea.Access{Actor: identity.Actor{ID: f.accountIDs[0], AccountType: "person"}, SessionDigest: sha256.Sum256([]byte(f.tokens[0]))}
	view, e := f.store.ListOwnCloseConnectionsCurrent(f.ctx, access)
	if e != nil {
		t.Fatal(e)
	}
	if len(view.Connections) != 1 {
		t.Fatal("only real accepted friend is Close choice")
	}
	entry := view.Connections[0]
	if _, e = f.store.PutOwnCloseConnectionCurrent(f.ctx, access, entry.PersonID, connection.ClosePut{TieID: entry.TieID, RequestID: entry.RequestID, SourceVersion: entry.SourceVersion, ValidUntil: view.ValidUntil, Confirmed: true}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM follows WHERE follower_account_id=ANY($1::uuid[]) OR person_account_id=ANY($1::uuid[])`, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
				t.Error(e)
			}
		}
	})
	gateway, e := postgres.NewHumanActiveIntents(f.store)
	if e != nil {
		t.Fatal(e)
	}
	h := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithHumanActiveIntents(gateway))
	call := func(method, path, body string, who int) *httptest.ResponseRecorder {
		q := httptest.NewRequest(method, path, strings.NewReader(body))
		q.Header.Set("Content-Type", "application/json")
		if who >= 0 {
			q.Header.Set("Authorization", "Bearer "+f.tokens[who])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		return w
	}
	for _, audience := range []string{"PUBLIC", "FOLLOWERS", "CONNECTIONS", "CLOSE", "PRIVATE", "FRIENDS"} {
		t.Run(audience, func(t *testing.T) {
			input := socialintent.DraftInput{Type: "FIND_COMPANION", Title: "本人当前范围" + audience, Audience: "PRIVATE", Modality: "ONLINE", Constraints: json.RawMessage(`{}`), ExpiresAt: time.Now().Add(time.Hour)}
			body, _ := json.Marshal(input)
			w := call("POST", "/v1/me/social-intents", string(body), 0)
			if w.Code != 201 {
				t.Fatal("registered draft", w.Code, w.Body.String())
			}
			var created struct {
				Data socialintent.Record `json:"data"`
			}
			if e := json.Unmarshal(w.Body.Bytes(), &created); e != nil {
				t.Fatal(e)
			}
			base := "/v1/me/active-social-intents/" + created.Data.ID
			w = call("GET", base, "", 0)
			var detail struct {
				Data ai.Detail `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil {
				t.Fatal("current native detail", w.Code, w.Body.String())
			}
			edit := ai.DraftOf(detail.Data.Item.Intent)
			edit.Audience = audience
			body, _ = json.Marshal(ai.Input{Operation: "EDIT", ExpectedVersion: detail.Data.Item.Version, Edit: &edit})
			w = call("POST", base+"/preview", string(body), 0)
			var preview struct {
				Data ai.Preview `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Data.After.AudienceLabel == "范围未知" {
				t.Fatal("new audience preview label", w.Code, w.Body.String())
			}
			approve := func(p ai.Preview) ai.Receipt {
				body, _ := json.Marshal(map[string]string{"previewId": p.PreviewID})
				w := call("POST", base+"/approve", string(body), 0)
				var result struct {
					Data ai.Receipt `json:"data"`
				}
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
					t.Fatal("concrete original approve", w.Code, w.Body.String())
				}
				if result.Data.ModelAccess || result.Data.SendAllowed || result.Data.Item.Intent.ID != created.Data.ID {
					t.Fatal("new audience granted model/send or replaced ID")
				}
				return result.Data
			}
			edited := approve(preview.Data)
			body, _ = json.Marshal(ai.Input{Operation: "ACTIVATE", ExpectedVersion: edited.Item.Version})
			w = call("POST", base+"/preview", string(body), 0)
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil {
				t.Fatal("publication preview", w.Code, w.Body.String())
			}
			active := approve(preview.Data)
			if active.Item.Intent.Status != "ACTIVE" || active.Item.Intent.Audience != audience {
				t.Fatal("actual current audience lost")
			}
			for _, who := range []int{-1, 1} {
				want := 404
				if audience == "PUBLIC" || (who == 1 && audience != "PRIVATE") {
					want = 200
				}
				w = call("GET", "/v1/social-intents/"+created.Data.ID, "", who)
				if w.Code != want {
					t.Fatal("registered native audience ACL", audience, who, w.Code, w.Body.String())
				}
				if strings.Contains(w.Body.String(), "bio") || strings.Contains(w.Body.String(), "privateCityHistory") {
					t.Fatal("Intent consumer leaked Profile fields")
				}
			}
			w = call("GET", "/v1/social-intents", "", 1)
			if w.Code != 200 {
				t.Fatal("registered native list", w.Code, w.Body.String())
			}
			var visible struct {
				Data []socialintent.Record `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &visible) != nil {
				t.Fatal("list DTO")
			}
			found := false
			for _, row := range visible.Data {
				if row.ID == created.Data.ID {
					found = true
				}
			}
			if found != (audience != "PRIVATE") {
				t.Fatal("list and detail audience differ")
			}
		})
	}
	// Current human Intent author eligibility is separate from compatibility
	// of ordinary public Profile reads for organization/business principals.
	// The original active owner has an immutable Agent/Profile type binding.
	// Use a separate owned human source without an Agent to exercise a lawful
	// account type change; do not remove the existing Agent or its authority.
	var originalHuman, originalIntent string
	if e = f.pool.QueryRow(f.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&originalHuman); e != nil {
		t.Fatal(e)
	}
	f.accountIDs = append(f.accountIDs, originalHuman)
	f.exec(`INSERT INTO user_profiles(account_id,display_name,bio,visibility)
	  VALUES($1,'合成原始人类意向作者','仅用于当前人类作者资格检查','public')`, originalHuman)
	if e = f.pool.QueryRow(f.ctx, `INSERT INTO social_intents(creator_account_id,intent_type,title,audience,modality,status,expires_at)
	  VALUES($1,'FIND_COMPANION','合成原始人类意向','PUBLIC','ONLINE','ACTIVE',clock_timestamp()+interval '1 hour') RETURNING id`, originalHuman).Scan(&originalIntent); e != nil {
		t.Fatal(e)
	}
	w := call("GET", "/v1/social-intents/"+originalIntent, "", -1)
	if w.Code != 200 {
		t.Fatal("registered current Person public Intent positive control", w.Code, w.Body.String())
	}
	f.exec(`UPDATE accounts SET account_type='organization' WHERE id=$1`, originalHuman)
	w = call("GET", "/v1/social-intents/"+originalIntent, "", -1)
	if w.Code != 404 {
		t.Fatal("registered anonymous detail retained non-Person human Intent", w.Code)
	}
	w = call("GET", "/v1/social-intents", "", -1)
	if w.Code != 200 {
		t.Fatal("anonymous native list after owner type change", w.Code)
	}
	var remaining struct {
		Data []socialintent.Record `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &remaining); e != nil {
		t.Fatal(e)
	}
	for _, row := range remaining.Data {
		if row.CreatorID == originalHuman || row.ID == originalIntent {
			t.Fatal("registered anonymous caller retained non-Person human Intent")
		}
	}
}
