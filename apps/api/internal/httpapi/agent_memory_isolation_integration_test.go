package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
)

func TestMemoryIsolationRegisteredHTTPContentMatrix(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	const canary = "合成060_PRIVATE_MEMORY_CONTENT"
	var memoryID, evidenceID, orgID string
	if err := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid(),gen_random_uuid(),(SELECT id FROM organizations WHERE account_id=$1)`, f.accountIDs[2]).Scan(&memoryID, &evidenceID, &orgID); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner'),($1,$3,'admin')`, orgID, f.accountIDs[0], f.accountIDs[1])
	if orgID == f.accountIDs[2] || f.agentIDs[2] == "" || f.agentIDs[3] != "" {
		t.Fatal("Org namespace/Business dormant fixture changed")
	}
	body, _ := json.Marshal(agentprofile.ReplacePrivateInput{ExpectedVersion: 1, Fields: agentprofile.PrivateFields{
		AgentNotes: canary, SocialPreferences: []string{canary}, PersonalPreferences: []string{},
		PreferredActivityTypes: []string{}, TravelPreferences: []string{}, InteractionPreferences: []string{}, LanguagePreferences: []string{},
	}})
	f.request(t, f.handler, "PUT", privateProfileHTTPPath, string(body), f.tokens[0], 200, nil)
	mpath := memoryHTTPList + "/" + memoryID
	created := memoryDBHTTPRecord(t, f.request(t, f.handler, "PUT", mpath, memoryDBHTTPBody(t, 0, "isolation-060", canary), f.tokens[0], 200, nil).Body.Bytes())
	moment, err := f.store.CreateMomentDraft(f.ctx, f.accountIDs[0], content.MomentInput{CityID: "aberdeen-gb", Title: "本地060私人来源", Body: canary, TimePrecision: "unknown", LocationPrecision: "city"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := f.pool.Exec(ctx, `DELETE FROM moments WHERE author_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Errorf("owned060Moment cleanup: %v", e)
		}
	})
	eb, _ := json.Marshal(agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: created.Version, SourceType: agentevent.MomentSource, SourceID: moment.ID})
	f.request(t, f.handler, "PUT", mpath+"/evidence/"+evidenceID, string(eb), f.tokens[0], 200, nil)
	ppath := mpath + "/provenance"
	f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.accountIDs[0])
	if _, err = f.store.GrantProfileRead(f.ctx, f.accountIDs[0], f.accountIDs[1], time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	t.Run("human_profile_view_does_not_include_private_resources", func(t *testing.T) {
		public := f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", f.tokens[1], 200, nil)
		if strings.Contains(public.Body.String(), canary) || strings.Contains(public.Body.String(), memoryID) || strings.Contains(public.Body.String(), evidenceID) {
			t.Fatal("public human projection leaked private resources")
		}
	})
	for _, path := range []string{privateProfileHTTPPath, memoryHTTPList, ppath} {
		t.Run("owner_positive"+path, func(t *testing.T) {
			rw := f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
			if path == ppath {
				p := provenanceHTTPRecord(t, rw.Body.Bytes())
				if len(p.Evidence) != 1 || p.MemoryID != memoryID || strings.Contains(rw.Body.String(), canary) {
					t.Fatal("native owner provenance missing or copied source body")
				}
			} else if !strings.Contains(rw.Body.String(), canary) {
				t.Fatal("owner private content positive control missing")
			}
		})
		t.Run("admin_personal_self"+path, func(t *testing.T) {
			status := 200
			if path == ppath {
				status = 404
			}
			rw := f.request(t, f.handler, "GET", path, "", f.tokens[1], status, nil)
			if strings.Contains(rw.Body.String(), canary) || strings.Contains(rw.Body.String(), memoryID) || strings.Contains(rw.Body.String(), evidenceID) || strings.Contains(rw.Body.String(), moment.ID) {
				t.Fatal("admin person leaked member private resource")
			}
		})
		for _, c := range []struct {
			name, token string
			status      int
			workspace   *string
		}{
			{"active_org", f.tokens[2], 403, nil}, {"dormant_business", f.tokens[3], 403, nil},
			{"person_owner_org_workspace", f.tokens[0], 403, &f.accountIDs[2]},
			{"admin_org_entity_workspace", f.tokens[1], 403, &orgID},
			{"unknown_session", "unknown060", 401, nil},
			{"expired_session", f.newSession(f.accountIDs[0], true, false), 401, nil},
			{"revoked_session", f.newSession(f.accountIDs[0], false, true), 401, nil},
		} {
			t.Run(c.name+path, func(t *testing.T) {
				rw := f.request(t, f.handler, "GET", path, "", c.token, c.status, c.workspace)
				for _, secret := range []string{canary, memoryID, evidenceID, moment.ID} {
					if strings.Contains(rw.Body.String(), secret) {
						t.Fatal("denied registered HTTP released private content/address")
					}
				}
			})
		}
		t.Run("client_owner_selector"+path, func(t *testing.T) {
			rw := f.request(t, f.handler, "GET", path+"?ownerId="+f.accountIDs[0]+"&confirmed=true&atShop=true", "", f.tokens[1], 400, nil)
			if strings.Contains(rw.Body.String(), canary) {
				t.Fatal("client location/confirmation manufactured authority")
			}
		})
	}
	f.request(t, f.handler, "PUT", mpath, `{"expectedVersion":1,"confirmed":true,"atShop":true}`, f.tokens[3], 403, nil)
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY: registered native HTTP profile/memory/evidence matrix; active Org and human admin do not inherit personal authorization; Business remains without Agent")
}
