package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func visibilityRealRecord(t *testing.T, w []byte) agentprofile.VisibilityRecord {
	t.Helper()
	var envelope struct {
		Data agentprofile.VisibilityRecord `json:"data"`
	}
	if json.Unmarshal(w, &envelope) != nil || agentprofile.ValidateVisibilityRecord(envelope.Data) != nil {
		t.Fatal("real visibility response has invalid control schema")
	}
	return envelope.Data
}

// Uses actual New routes/PostgreSQL, random owned identities and synthetic
// source text. No user data, IdP, model/provider or live pilot is involved.
func TestAgentProfileVisibilityHTTPRealLifecycleIntegration(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	t.Cleanup(func() {
		for _, statement := range []string{
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
		} {
			if _, err := f.pool.Exec(context.Background(), statement, f.accountIDs); err != nil {
				t.Error("owned visibility HTTP relation cleanup failed")
			}
		}
	})
	self := "/v1/me/agent-profile-visibility"
	target := "/v1/accounts/" + f.accountIDs[0] + "/agent-profile-fields"
	rules := agentprofile.DefaultFieldRules()
	currentVersion := int64(1)
	save := func(t *testing.T, next agentprofile.FieldRules, want int) agentprofile.VisibilityRecord {
		t.Helper()
		normalized, err := agentprofile.NormalizeFieldRules(next)
		if err != nil {
			t.Fatal("synthetic rule fixture invalid")
		}
		raw, err := json.Marshal(agentprofile.ReplaceVisibilityInput{ExpectedVersion: currentVersion, Rules: normalized})
		if err != nil {
			t.Fatal(err)
		}
		w := f.request(t, f.handler, "PUT", self, string(raw), f.tokens[0], want, nil)
		if want != 200 {
			return agentprofile.VisibilityRecord{}
		}
		record := visibilityRealRecord(t, w.Body.Bytes())
		if record.Profile.ProfileVersion != currentVersion+1 {
			t.Fatal("real policy CAS did not advance once")
		}
		currentVersion = record.Profile.ProfileVersion
		return record
	}
	project := func(t *testing.T, token string, want int, keys ...agentprofile.FieldKey) {
		t.Helper()
		w := f.request(t, f.handler, "GET", target, "", token, want, nil)
		if want != 200 {
			return
		}
		var envelope struct {
			Data agentprofile.ProjectedRecord `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || agentprofile.ValidateProjectedRecord(envelope.Data) != nil || envelope.Data.AccountID != f.accountIDs[0] || len(envelope.Data.Fields) != len(keys) {
			t.Fatal("real projection shape/count incorrect")
		}
		for _, key := range keys {
			if _, ok := envelope.Data.Fields[key]; !ok {
				t.Fatalf("expected authorized key %s missing", key)
			}
		}
		for _, key := range []string{"profileVersion", "communityIds", "rules", "agentId", "ownerType", "configured"} {
			if strings.Contains(w.Body.String(), `"`+key+`"`) {
				t.Fatal("real projection leaks policy/metadata")
			}
		}
	}
	t.Run("absent defaults no write", func(t *testing.T) {
		w := f.request(t, f.handler, "GET", self, "", f.tokens[0], 200, nil)
		record := visibilityRealRecord(t, w.Body.Bytes())
		if record.Configured || record.Profile.ProfileVersion != 1 {
			t.Fatal("unexpected default")
		}
		var count int
		if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_profile_field_visibility WHERE owner_id=ANY($1::uuid[])`, f.accountIDs).Scan(&count) != nil || count != 0 {
			t.Fatal("read persisted defaults")
		}
		project(t, "", 200, agentprofile.FieldDisplayName, agentprofile.FieldBio)
	})
	t.Run("explicit private source saved without sharing", func(t *testing.T) {
		w := f.request(t, f.handler, "PUT", privateProfileHTTPPath, `{"expectedVersion":1,"fields":{"agentNotes":"`+privateProfileHTTPMarker+`"}}`, f.tokens[0], 200, nil)
		currentVersion = privateProfileHTTPDBRecord(t, w).Profile.ProfileVersion
		project(t, "", 200, agentprofile.FieldDisplayName, agentprofile.FieldBio)
	})
	t.Run("private public and only Agent remain distinct", func(t *testing.T) {
		rules[agentprofile.FieldDisplayName] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPrivate}
		rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityAgentOnly}
		save(t, rules, 200)
		project(t, "", 200, agentprofile.FieldBio)
		project(t, f.tokens[1], 200, agentprofile.FieldBio)
		project(t, f.tokens[0], 200, agentprofile.FieldDisplayName, agentprofile.FieldBio, agentprofile.FieldAgentNotes)
		w := f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", "", 200, nil)
		if strings.Contains(w.Body.String(), "合成 HTTP 公开资料") || strings.Contains(w.Body.String(), privateProfileHTTPMarker) {
			t.Fatal("ordinary Profile exposed hidden name/Private field")
		}
	})
	t.Run("invalid identity and authority never reach write", func(t *testing.T) {
		for _, token := range []string{"", f.tokens[2], f.tokens[3], f.newSession(f.accountIDs[0], true, false), f.newSession(f.accountIDs[0], false, true)} {
			want := 403
			if token == "" || token != f.tokens[2] && token != f.tokens[3] {
				want = 401
			}
			f.request(t, f.handler, "GET", self, "", token, want, nil)
		}
		empty := ""
		f.request(t, f.handler, "GET", target, "", f.tokens[1], 403, &empty)
		f.request(t, f.handler, "GET", target+"?ownerId="+f.accountIDs[1], "", f.tokens[0], 400, nil)
		project(t, f.newSession(f.accountIDs[1], true, false), 401)
		f.request(t, f.handler, "PUT", self, `{"expectedVersion":1,"confirmed":true,"rules":{}}`, f.tokens[0], 400, nil)
	})
	var requestID, tieID string
	t.Run("only accepted friend authorizes connections", func(t *testing.T) {
		rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityConnections}
		save(t, rules, 200)
		project(t, f.tokens[1], 200, agentprofile.FieldBio)
		if f.pool.QueryRow(f.ctx, `INSERT INTO connection_requests(sender_account_id,recipient_account_id,note,scope,state,expires_at) VALUES($1,$2,'Synthetic friend field boundary','friend','accepted',now()+interval '1 day') RETURNING id`, f.accountIDs[0], f.accountIDs[1]).Scan(&requestID) != nil {
			t.Fatal("cannot create owned friend request")
		}
		if f.pool.QueryRow(f.ctx, `INSERT INTO person_ties(person_a_account_id,person_b_account_id,request_id) VALUES(LEAST($1::uuid,$2::uuid),GREATEST($1::uuid,$2::uuid),$3) RETURNING id`, f.accountIDs[0], f.accountIDs[1], requestID).Scan(&tieID) != nil {
			t.Fatal("cannot create owned strong Tie")
		}
		project(t, f.tokens[1], 200, agentprofile.FieldBio, agentprofile.FieldAgentNotes)
		if f.store.RemoveTie(f.ctx, f.accountIDs[0], tieID) != nil {
			t.Fatal("real Tie revoke failed")
		}
		project(t, f.tokens[1], 200, agentprofile.FieldBio)
	})
	var communityID string
	t.Run("explicit same current community", func(t *testing.T) {
		if f.pool.QueryRow(f.ctx, `INSERT INTO communities(owner_account_id,name,summary,visibility,join_policy,lifecycle_status,publication_status,owner_confirmed_at,source_label,source_ref,maintainer_label) VALUES($1,'Synthetic field HTTP community','Owned fixture','hidden','invite_only','active','published',now(),'Synthetic HTTP fixture','synthetic:'||gen_random_uuid()::text,'Synthetic maintainer') RETURNING id`, f.accountIDs[0]).Scan(&communityID) != nil {
			t.Fatal("cannot create owned Community")
		}
		t.Cleanup(func() {
			if _, err := f.pool.Exec(f.ctx, `DELETE FROM communities WHERE id=$1`, communityID); err != nil {
				t.Error("owned Community cleanup failed")
			}
		})
		rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{communityID}}
		save(t, rules, 200)
		project(t, f.tokens[1], 200, agentprofile.FieldBio)
		f.exec(`INSERT INTO community_memberships(community_id,user_account_id,status) VALUES($1,$2,'invited')`, communityID, f.accountIDs[1])
		project(t, f.tokens[1], 200, agentprofile.FieldBio)
		f.exec(`UPDATE community_memberships SET status='active' WHERE community_id=$1 AND user_account_id=$2`, communityID, f.accountIDs[1])
		project(t, f.tokens[1], 200, agentprofile.FieldBio, agentprofile.FieldAgentNotes)
		f.exec(`UPDATE community_memberships SET status='left' WHERE community_id=$1 AND user_account_id=$2`, communityID, f.accountIDs[1])
		project(t, f.tokens[1], 200, agentprofile.FieldBio)
	})
	t.Run("coarse private Profile grant is not field grant", func(t *testing.T) {
		f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.accountIDs[0])
		project(t, "", 404)
		project(t, f.tokens[1], 404)
		grant, err := f.store.GrantProfileRead(f.ctx, f.accountIDs[0], f.accountIDs[1], time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal("real ordinary Profile grant failed")
		}
		project(t, f.tokens[1], 200, agentprofile.FieldBio)
		if err := f.store.RevokeProfileGrant(f.ctx, f.accountIDs[0], grant.ID); err != nil {
			t.Fatal("real ordinary Profile grant revoke failed")
		}
		project(t, f.tokens[1], 404)
		f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.accountIDs[0])
	})
	t.Run("explicit public field is not all private row", func(t *testing.T) {
		rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
		save(t, rules, 200)
		project(t, "", 200, agentprofile.FieldBio, agentprofile.FieldAgentNotes)
		w := f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", "", 200, nil)
		if strings.Contains(w.Body.String(), privateProfileHTTPMarker) || strings.Contains(w.Body.String(), "agentNotes") {
			t.Fatal("old four-field projection grew private row")
		}
	})
	t.Run("Block and source deactivation override public field", func(t *testing.T) {
		if f.store.BlockAccount(f.ctx, f.accountIDs[1], f.accountIDs[0]) != nil {
			t.Fatal("real block failed")
		}
		project(t, f.tokens[1], 404)
		if f.store.UnblockAccount(f.ctx, f.accountIDs[1], f.accountIDs[0]) != nil {
			t.Fatal("real unblock failed")
		}
		project(t, f.tokens[1], 200, agentprofile.FieldBio, agentprofile.FieldAgentNotes)
		f.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, f.agentIDs[0])
		project(t, "", 404)
		f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
	})
	t.Run("stale policy conflicts without changes", func(t *testing.T) {
		raw, _ := json.Marshal(agentprofile.ReplaceVisibilityInput{ExpectedVersion: currentVersion - 1, Rules: agentprofile.DefaultFieldRules()})
		f.request(t, f.handler, "PUT", self, string(raw), f.tokens[0], 409, nil)
		w := f.request(t, f.handler, "GET", self, "", f.tokens[0], 200, nil)
		if visibilityRealRecord(t, w.Body.Bytes()).Profile.ProfileVersion != currentVersion {
			t.Fatal("stale update mutated version")
		}
	})
	t.Run("reconnected authority reads latest", func(t *testing.T) {
		connected, err := pgxpool.New(f.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
		if err != nil {
			t.Fatal(err)
		}
		defer connected.Close()
		reconnected := postgres.New(connected, false)
		handler := privateProfileHTTPNew(reconnected, reconnected)
		w := f.request(t, handler, "GET", self, "", f.tokens[0], 200, nil)
		if visibilityRealRecord(t, w.Body.Bytes()).Profile.ProfileVersion != currentVersion {
			t.Fatal("policy not persistent")
		}
	})
	t.Run("explicit default clear persists without private copy", func(t *testing.T) {
		record := save(t, agentprofile.DefaultFieldRules(), 200)
		if record.Configured {
			t.Fatal("default clear remained configured")
		}
		project(t, "", 200, agentprofile.FieldDisplayName, agentprofile.FieldBio)
		w := f.request(t, f.handler, "GET", privateProfileHTTPPath, "", f.tokens[0], 200, nil)
		private := privateProfileHTTPDBRecord(t, w)
		if private.Fields.AgentNotes != privateProfileHTTPMarker || private.Profile.ProfileVersion != currentVersion {
			t.Fatal("policy clear erased private content or diverged version")
		}
		t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY final_profile_version=%d", currentVersion)
	})
}
