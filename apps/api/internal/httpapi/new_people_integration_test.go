package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
)

type navigationOnlyNewPeopleStore struct{ newpeople.Store }

func (navigationOnlyNewPeopleStore) FindNewPeople(context.Context, string, string) (newpeople.Response, error) {
	panic("Agent navigation must not query candidates before a person selects a source")
}

func (navigationOnlyNewPeopleStore) InviteNewPeople(context.Context, string, string, string, string) (connection.Request, error) {
	panic("Agent navigation must never send an invitation")
}

func TestNewPeopleAgentDispatchOnlyOpensOwnerReview(t *testing.T) {
	s := &server{newPeople: navigationOnlyNewPeopleStore{}}
	const ownerID = "30000000-0000-4000-8000-000000000001"
	const otherID = "30000000-0000-4000-8000-000000000002"
	for _, query := range []string{"找新朋友", "帮我找伙伴", "认识新朋友", "meet new people", "find a companion"} {
		intent := agentworkspace.ParseMVPIntent(query, nil)
		if !intent.Supported || intent.Operation != agentworkspace.FindNewPeople {
			t.Fatalf("explicit companion request not routed: %s %+v", query, intent)
		}
		for _, tc := range []struct {
			name, actor, principal, kind string
			allowed                      bool
		}{
			{"owner", ownerID, ownerID, "person", true},
			{"anonymous", "", "", "person", false},
			{"different principal", ownerID, otherID, "person", false},
			{"organization", ownerID, otherID, "organization", false},
		} {
			t.Run(query+"/"+tc.name, func(t *testing.T) {
				task := agentworkspace.Task{ID: "30000000-0000-4000-8000-000000000003",
					PrincipalID: tc.principal, PrincipalType: tc.kind, ActingUserID: tc.actor,
					Intent: intent.Operation, Status: agentworkspace.TaskCompleted}
				result, message, err := s.specialAgentResults(context.Background(), task, intent, query, "", tc.actor, nil)
				if err != nil || message == "" {
					t.Fatal("navigation failed", message, err)
				}
				result.PrincipalID, result.PrincipalType, result.Workspace = tc.principal, strings.ToUpper(tc.kind), "PERSONAL"
				if tc.kind == "organization" {
					result.Workspace = "ORGANIZATION"
				}
				result = agentworkspace.WithContract(result, task, "synthetic-navigation-request")
				if len(result.People) != 0 || len(result.Activities) != 0 || len(result.Organizations) != 0 || len(result.Groups) != 0 || len(result.Places) != 0 || result.RelationshipContext != nil || len(result.ResultSet.Entities) != 0 || len(result.MapEffects.PinEntityIDs) != 0 || result.MapEffects.Camera != "preserve" {
					t.Fatal("new people navigation altered public results or map", result)
				}
				if tc.allowed {
					if len(result.Actions) != 1 || result.Actions[0].Type != "OPEN_NEW_PEOPLE" || result.Actions[0].Label != "找新朋友" || result.Actions[0].TargetID != "" || result.Actions[0].TargetType != "" || !bytes.Contains([]byte(message), []byte("确认")) {
						t.Fatal("owner explicit review navigation missing", result, message)
					}
				} else if len(result.Actions) != 0 {
					t.Fatal("anonymous or non-owner received personal action", result.Actions)
				}
			})
		}
	}
	// An activity search stays in its existing business flow.
	if intent := agentworkspace.ParseMVPIntent("帮我找周末的羽毛球", nil); intent.Operation != agentworkspace.FindActivity {
		t.Fatal("activity query incorrectly turned into people matching", intent)
	}
}

func TestNewPeopleRoutingHTTPIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable DB")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var installed bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('public.person_new_people_consent') IS NOT NULL`).Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Skip("052 not applied")
	}
	ids, tokens := make([]string, 5), make([]string, 5)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, sql, args...); e != nil {
			t.Fatal(e)
		}
	}
	// Profiles are public intentionally: this must never turn consent on.
	for i := range ids {
		kind := "person"
		if i == 4 {
			kind = "organization"
		}
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		if kind == "person" {
			exec(`INSERT INTO user_profiles(account_id,display_name,visibility,bio) VALUES($1,$2,'public','PRIVATE_PROFILE_BIO_CANARY')`, ids[i], "合成新朋友 "+ids[i][:8])
		}
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = token
		exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, ids[i], digest[:])
	}
	defer func() {
		// An explicit false choice is also persisted, so remove all fixture
		// choices before migration down guards are exercised by the verifier.
		for _, sql := range []string{
			`DELETE FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_member_states WHERE member_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])`,
			`DELETE FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, e := pool.Exec(ctx, sql, ids); e != nil {
				t.Errorf("new people fixture cleanup: %v", e)
			}
		}
	}()
	store := postgres.New(pool, false)
	// The registered current read requires the real stable personal identity.
	// Native 053 bootstraps metadata; this grants no match consent or tool right.
	for _, id := range ids[:4] {
		exec(`INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, id)
		var count int
		if e := pool.QueryRow(ctx, `SELECT count(*) FROM agents a JOIN agent_profiles p ON p.agent_id=a.id AND p.owner_id=a.principal_account_id AND p.owner_type='PERSON' WHERE a.principal_account_id=$1 AND a.agent_type='personal' AND a.status='active' AND p.profile_version=1`, id).Scan(&count); e != nil || count != 1 { t.Fatal("exact native stable Agent/Profile bootstrap",e) }
	}
	s := &server{access: store, socialIntents: store, connections: store, newPeople: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me/new-people/consent", s.ownNewPeopleConsent)
	mux.HandleFunc("PUT /v1/me/new-people/consent", s.setNewPeopleConsent)
	mux.HandleFunc("GET /v1/me/new-people/intents", s.listOwnNewPeopleIntents)
	mux.HandleFunc("POST /v1/me/new-people/intents", s.createNewPeopleIntent)
	mux.HandleFunc("GET /v1/me/new-people/candidates", s.newPeopleCandidates)
	mux.HandleFunc("POST /v1/me/new-people/invitations", s.createNewPeopleInvitation)
	mux.HandleFunc("POST /v1/me/social-intents", s.createSocialIntentDraft)
	mux.HandleFunc("POST /v1/me/social-intents/{intentID}/activate", s.activateSocialIntent)
	mux.HandleFunc("POST /v1/me/social-intents/{intentID}/cancel", s.cancelSocialIntent)
	mux.HandleFunc("POST /v1/me/connection-requests/{requestID}/decision", s.decideConnectionRequest)
	mux.HandleFunc("GET /v1/me/ties", s.listPersonTies)
	mux.HandleFunc("POST /v1/me/blocks", s.blockAccount)
	mux.HandleFunc("DELETE /v1/me/blocks/{accountID}", s.unblockAccount)
	call := func(method, path string, who int, body any, organizationWorkspace bool, want int) []byte {
		t.Helper()
		payload, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		if who >= 0 {
			r.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		if organizationWorkspace {
			r.Header.Set("X-Birdtie-Organization-Workspace", "b1700000-0000-4000-8000-000000000001")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s actor%d: got%d want%d %s", method, path, who, w.Code, want, w.Body.String())
		}
		if method == "GET" && bytes.Contains([]byte(path), []byte("/candidates")) && want == http.StatusOK && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("new people candidate response cached")
		}
		return w.Body.Bytes()
	}
	consentPath := "/v1/me/new-people/consent"
	intentsPath := "/v1/me/new-people/intents"
	candidatesPath := "/v1/me/new-people/candidates"
	invitationsPath := "/v1/me/new-people/invitations"
	decodeRecord := func(raw []byte) socialintent.Record {
		t.Helper()
		var response struct {
			Data socialintent.Record `json:"data"`
		}
		if e := json.Unmarshal(raw, &response); e != nil || response.Data.ID == "" {
			t.Fatalf("invalid intent response: %s %v", raw, e)
		}
		return response.Data
	}
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{"GET", consentPath, nil}, {"PUT", consentPath, map[string]bool{"enabled": true}},
		{"GET", intentsPath, nil}, {"POST", intentsPath, map[string]any{}},
		{"GET", candidatesPath + "?sourceIntentId=" + ids[0], nil},
		{"POST", invitationsPath, map[string]any{}},
	} {
		call(route.method, route.path, -1, route.body, false, 401)
		call(route.method, route.path, 4, route.body, false, 403)
		call(route.method, route.path, 0, route.body, true, 403)
	}
	var choice struct {
		Data newpeople.Consent `json:"data"`
	}
	if e := json.Unmarshal(call("GET", consentPath, 0, nil, false, 200), &choice); e != nil || choice.Data.Enabled {
		t.Fatal("public profile silently opted in", choice, e)
	}
	call("GET", consentPath+"?accountId="+ids[1], 0, nil, false, 400)
	call("GET", intentsPath+"?ownerAccountId="+ids[1], 0, nil, false, 400)
	call("PUT", consentPath, 0, map[string]any{}, false, 400)
	call("PUT", consentPath, 0, map[string]any{"enabled": true, "accountId": ids[1]}, false, 400)

	category := "合成线上讨论 " + ids[0]
	platform := "PRIVATE_PLATFORM_CANARY " + ids[0]
	draftBody := func(title, cat string) map[string]any {
		return map[string]any{"title": title, "category": cat, "modality": "ONLINE", "onlinePlatform": platform, "minParticipants": 2, "maxParticipants": 4, "expiresAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}
	}
	for _, extra := range []map[string]any{
		{"cityId": "aberdeen-gb"}, {"placeId": ids[0]}, {"areaLabel": "PRIVATE_AREA_CANARY"},
		{"latitude": 57.0}, {"contextId": ids[0]}, {"creatorAccountId": ids[1]},
	} {
		body := draftBody("非法线上物理约束", category)
		for key, value := range extra {
			body[key] = value
		}
		call("POST", intentsPath, 0, body, false, 400)
	}
	call("POST", intentsPath, 0, draftBody("缺少明确类别", ""), false, 400)
	source := decodeRecord(call("POST", intentsPath, 0, draftBody("PRIVATE_OWNER_TITLE_CANARY", category), false, 201))
	peer := decodeRecord(call("POST", intentsPath, 1, draftBody("PRIVATE_PEER_TITLE_CANARY", category), false, 201))
	nonmatch := decodeRecord(call("POST", intentsPath, 2, draftBody("不相容的声明", category+" 不同"), false, 201))
	if source.Status != "DRAFT" || source.Type != "FIND_COMPANION" || source.Audience != "PUBLIC" || source.ContextID != nil || source.CityID != "" || source.Modality != "ONLINE" {
		t.Fatalf("online draft contract: %+v", source)
	}
	var own struct {
		Data []socialintent.Record `json:"data"`
	}
	json.Unmarshal(call("GET", intentsPath, 0, nil, false, 200), &own)
	if len(own.Data) != 1 || own.Data[0].ID != source.ID || own.Data[0].CreatorID != ids[0] {
		t.Fatalf("owner listing crossed account boundary: %+v", own)
	}
	call("GET", candidatesPath+"?sourceIntentId="+source.ID, 0, nil, false, 404)
	call("PUT", consentPath, 0, map[string]bool{"enabled": true}, false, 200)
	// A consented draft still supplies nothing until a separate confirmation.
	call("GET", candidatesPath+"?sourceIntentId="+source.ID, 0, nil, false, 404)
	call("POST", "/v1/me/social-intents/"+source.ID+"/activate", 0, map[string]bool{"confirmed": false}, false, 400)
	for who, item := range []socialintent.Record{source, peer, nonmatch} {
		activated := decodeRecord(call("POST", "/v1/me/social-intents/"+item.ID+"/activate", who, map[string]bool{"confirmed": true}, false, 200))
		if activated.Status != "ACTIVE" {
			t.Fatal("explicit publication failed", activated)
		}
	}
	readCandidates := func(sourceID string, who, want int) newpeople.Response {
		t.Helper()
		raw := call("GET", candidatesPath+"?sourceIntentId="+sourceID, who, nil, false, 200)
		var response struct {
			Data newpeople.Response `json:"data"`
		}
		if e := json.Unmarshal(raw, &response); e != nil || response.Data.Source != "RULE_BASED" || response.Data.RuleVersion != "v1" || response.Data.SourceIntentID != sourceID || len(response.Data.Candidates) != want || response.Data.Candidates == nil {
			t.Fatalf("candidates wanted%d: %s %v", want, raw, e)
		}
		for _, forbidden := range []string{"PRIVATE_", `"title"`, `"bio"`, `"constraints"`, `"areaLabel"`, `"onlinePlatform"`, `"contextId"`, `"latitude"`, `"longitude"`, `"personContext"`, `"inviteeAccountIds"`, `"mapEffects"`, `"people"`} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Fatalf("private or unrelated field in candidate response %s: %s", forbidden, raw)
			}
		}
		return response.Data
	}
	readCandidates(source.ID, 0, 0)
	call("PUT", consentPath, 2, map[string]bool{"enabled": true}, false, 200)
	readCandidates(source.ID, 0, 0) // Different category is not guessed compatible.
	call("PUT", consentPath, 1, map[string]bool{"enabled": true}, false, 200)
	got := readCandidates(source.ID, 0, 1)
	if candidate := got.Candidates[0]; candidate.AccountID != ids[1] || candidate.CandidateIntentID != peer.ID || candidate.SourceIntentID != source.ID || candidate.Modality != "ONLINE" || len(candidate.Reasons) == 0 || len(candidate.ReasonCodes) == 0 {
		t.Fatal("minimal candidate provenance/reasons missing", candidate)
	}
	// Querying never performs an invitation, Tie or conversation write.
	var requests, inbox, ties, conversations int
	counts := func() {
		t.Helper()
		if e := pool.QueryRow(ctx, `SELECT
		 (SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])),
		 (SELECT count(*) FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[]) AND resource_type='connection_request'),
		 (SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])),
		 (SELECT count(*) FROM conversations WHERE member_a_account_id=ANY($1::uuid[]) OR member_b_account_id=ANY($1::uuid[]))`, ids).Scan(&requests, &inbox, &ties, &conversations); e != nil {
			t.Fatal(e)
		}
	}
	counts()
	if requests != 0 || inbox != 0 || ties != 0 || conversations != 0 {
		t.Fatal("read caused relationship side effects", requests, inbox, ties, conversations)
	}
	call("GET", candidatesPath, 0, nil, false, 400)
	call("GET", candidatesPath+"?sourceIntentId=invalid", 0, nil, false, 400)
	call("GET", candidatesPath+"?sourceIntentId="+source.ID+"&sourceIntentId="+peer.ID, 0, nil, false, 400)
	call("GET", candidatesPath+"?sourceIntentId="+source.ID+"&principalAccountId="+ids[1], 0, nil, false, 400)
	call("GET", candidatesPath+"?sourceIntentId="+peer.ID, 0, nil, false, 404)
	exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, ids[1])
	readCandidates(source.ID, 0, 0)
	exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, ids[1])
	readCandidates(source.ID, 0, 1)
	exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, ids[1])
	readCandidates(source.ID, 0, 0)
	exec(`UPDATE accounts SET status='active' WHERE id=$1`, ids[1])
	readCandidates(source.ID, 0, 1)

	// An explicitly private source is never interpreted as match consent.
	privateBody := map[string]any{"type": "FIND_COMPANION", "title": "PRIVATE_INTENT_TITLE_CANARY", "audience": "PRIVATE", "modality": "ONLINE", "constraints": map[string]any{"category": category, "onlinePlatform": platform}, "expiresAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)}
	private := decodeRecord(call("POST", "/v1/me/social-intents", 0, privateBody, false, 201))
	call("POST", "/v1/me/social-intents/"+private.ID+"/activate", 0, map[string]bool{"confirmed": true}, false, 200)
	call("GET", candidatesPath+"?sourceIntentId="+private.ID, 0, nil, false, 404)
	// Reciprocal visibility matters. This owner source is open only to a
	// different person, so the otherwise compatible peer cannot use it.
	privateBody["audience"] = "INVITE_ONLY"
	privateBody["inviteeAccountIds"] = []string{ids[2]}
	targeted := decodeRecord(call("POST", "/v1/me/social-intents", 0, privateBody, false, 201))
	call("POST", "/v1/me/social-intents/"+targeted.ID+"/activate", 0, map[string]bool{"confirmed": true}, false, 200)
	readCandidates(targeted.ID, 0, 0)
	exec(`UPDATE social_intents SET audience='INVITE_ONLY' WHERE id=$1`, peer.ID)
	// An INVITE_ONLY record can revoke its last invitation. No target is
	// inferred from matching category/platform declarations.
	readCandidates(source.ID, 0, 0)
	exec(`INSERT INTO social_intent_invitations(intent_id,invitee_account_id) VALUES($1,$2)`, peer.ID, ids[0])
	readCandidates(source.ID, 0, 1)
	exec(`UPDATE social_intent_invitations SET status='revoked' WHERE intent_id=$1`, peer.ID)
	readCandidates(source.ID, 0, 0)
	exec(`UPDATE social_intent_invitations SET status='invited' WHERE intent_id=$1`, peer.ID)
	readCandidates(source.ID, 0, 1)

	inviteBody := func(sourceID, candidateID string) map[string]any {
		return map[string]any{"sourceIntentId": sourceID, "candidateIntentId": candidateID, "note": "合成邀请，请你确认后再成为好友", "confirmed": true}
	}
	for _, body := range []map[string]any{
		{"sourceIntentId": source.ID, "candidateIntentId": peer.ID, "note": "合成邀请"},
		{"sourceIntentId": source.ID, "candidateIntentId": peer.ID, "note": "合成邀请", "confirmed": false},
		{"sourceIntentId": source.ID, "candidateIntentId": peer.ID, "note": "合成邀请", "confirmed": true, "recipientAccountId": ids[2]},
		{"sourceIntentId": source.ID, "candidateIntentId": peer.ID, "note": "", "confirmed": true},
	} {
		call("POST", invitationsPath, 0, body, false, 400)
	}
	call("POST", invitationsPath, 0, inviteBody(peer.ID, source.ID), false, 404)
	call("POST", invitationsPath, 0, inviteBody(source.ID, nonmatch.ID), false, 404)
	call("POST", invitationsPath, 0, inviteBody(targeted.ID, peer.ID), false, 404)
	call("PUT", consentPath, 1, map[string]bool{"enabled": false}, false, 200)
	readCandidates(source.ID, 0, 0)
	call("POST", invitationsPath, 0, inviteBody(source.ID, peer.ID), false, 404)
	call("PUT", consentPath, 1, map[string]bool{"enabled": true}, false, 200)
	call("PUT", consentPath, 0, map[string]bool{"enabled": false}, false, 200)
	call("GET", candidatesPath+"?sourceIntentId="+source.ID, 0, nil, false, 404)
	call("POST", invitationsPath, 0, inviteBody(source.ID, peer.ID), false, 404)
	call("PUT", consentPath, 0, map[string]bool{"enabled": true}, false, 200)
	call("POST", "/v1/me/blocks", 1, map[string]string{"accountId": ids[0]}, false, 204)
	readCandidates(source.ID, 0, 0)
	call("POST", invitationsPath, 0, inviteBody(source.ID, peer.ID), false, 404)
	call("DELETE", "/v1/me/blocks/"+ids[0], 1, nil, false, 204)
	readCandidates(source.ID, 0, 1)
	counts()
	if requests != 0 || inbox != 0 || ties != 0 || conversations != 0 {
		t.Fatal("invalid or unconfirmed operation wrote a relationship", requests, inbox, ties, conversations)
	}

	var invitation struct {
		Data connection.Request `json:"data"`
	}
	raw := call("POST", invitationsPath, 0, inviteBody(source.ID, peer.ID), false, 201)
	if e := json.Unmarshal(raw, &invitation); e != nil || invitation.Data.ID == "" || invitation.Data.OtherAccountID != ids[1] || invitation.Data.Scope != "friend" || invitation.Data.State != "pending" || invitation.Data.CityID != "" || invitation.Data.ConversationID != "" {
		t.Fatalf("invitation did not derive recipient/pending scope: %s %v", raw, e)
	}
	counts()
	if requests != 1 || inbox != 1 || ties != 0 || conversations != 0 {
		t.Fatal("invitation auto-created friendship/chat", requests, inbox, ties, conversations)
	}
	readCandidates(source.ID, 0, 0)
	// A pending reverse request also excludes the owner from the peer's results.
	readCandidates(peer.ID, 1, 0)
	call("POST", invitationsPath, 0, inviteBody(source.ID, peer.ID), false, 404)
	call("POST", invitationsPath, 1, inviteBody(peer.ID, source.ID), false, 404)
	counts()
	if requests != 1 || inbox != 1 {
		t.Fatal("duplicate invitation wrote another request/inbox", requests, inbox)
	}
	// Closing discovery preserves an already human-sent request. Only its
	// recipient may accept it; accepting a friend request does not open chat.
	call("PUT", consentPath, 0, map[string]bool{"enabled": false}, false, 200)
	decision := "/v1/me/connection-requests/" + invitation.Data.ID + "/decision"
	call("POST", decision, 0, map[string]string{"action": "accept"}, false, 404)
	call("POST", decision, 1, map[string]string{"action": "accept"}, false, 200)
	counts()
	if requests != 1 || ties != 1 || conversations != 0 {
		t.Fatal("recipient acceptance semantics", requests, ties, conversations)
	}
	for who := 0; who < 2; who++ {
		var response struct {
			Data []connection.Tie `json:"data"`
		}
		if e := json.Unmarshal(call("GET", "/v1/me/ties", who, nil, false, 200), &response); e != nil || len(response.Data) != 1 || response.Data[0].OtherAccountID != ids[1-who] {
			t.Fatal("accepted bilateral Tie missing", response, e)
		}
	}
	call("PUT", consentPath, 0, map[string]bool{"enabled": true}, false, 200)
	readCandidates(source.ID, 0, 0)
	call("POST", invitationsPath, 0, inviteBody(source.ID, peer.ID), false, 404)
	call("POST", "/v1/me/social-intents/"+source.ID+"/cancel", 0, nil, false, 200)
	call("GET", candidatesPath+"?sourceIntentId="+source.ID, 0, nil, false, 404)
	call("POST", invitationsPath, 0, inviteBody(source.ID, peer.ID), false, 404)
	exec(`UPDATE sessions SET revoked_at=now() WHERE account_id=$1`, ids[3])
	call("GET", consentPath, 3, nil, false, 401)
	call("PUT", consentPath, 0, map[string]bool{"enabled": false}, false, 200)
	fresh, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer fresh.Close()
	if persisted, e := postgres.New(fresh, false).GetNewPeopleConsent(ctx, ids[0]); e != nil || persisted.Enabled {
		t.Fatal("revoked consent not persisted across pool restart", persisted, e)
	}
	var enables, revokes int
	if e := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE purpose='explicit_enable'),
	 count(*) FILTER(WHERE purpose='explicit_revoke') FROM audit_events
	 WHERE actor_account_id=$1 AND resource_type='new_people_consent' AND decision='allowed'`, ids[0]).Scan(&enables, &revokes); e != nil || enables < 1 || revokes < 2 {
		t.Fatal("explicit consent/revocation audit missing", enables, revokes, e)
	}
}
