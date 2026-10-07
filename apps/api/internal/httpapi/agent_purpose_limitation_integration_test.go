package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

func TestPurposeLimitationRegisteredHTTPCannotSupplyRetentionAuthority(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	var id, orgID string
	if err := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid(),(SELECT id FROM organizations WHERE account_id=$1)`, f.accountIDs[2]).Scan(&id, &orgID); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner'),($1,$3,'admin')`, orgID, f.accountIDs[0], f.accountIDs[1])
	path := memoryHTTPList + "/" + id
	const canary = "合成062本人手工记忆，不是活动临时许可"
	created := memoryDBHTTPRecord(t, f.request(t, f.handler, "PUT", path, memoryDBHTTPBody(t, 0, "purpose-062", canary), f.tokens[0], 200, nil).Body.Bytes())
	f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.accountIDs[0])
	if _, err := f.store.GrantProfileRead(f.ctx, f.accountIDs[0], f.accountIDs[1], time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	public := f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", f.tokens[1], 200, nil)
	if strings.Contains(public.Body.String(), canary) {
		t.Fatal("specific human profile_view leaked Memory")
	}
	var before string
	if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"purpose", "retention", "origin", "activityId", "temporaryGrant", "grant", "approved", "confirmed", "sourceType", "ownerId", "ownerType", "agentId"} {
		t.Run("owner_wire_"+field, func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal([]byte(memoryDBHTTPBody(t, created.Version, "purpose-062", canary)), &body); err != nil {
				t.Fatal(err)
			}
			body[field] = "ACTIVITY_TEMPORARY_AUTHORITY_IS_NOT_CLIENT_DATA"
			raw, _ := json.Marshal(body)
			rw := f.request(t, f.handler, "PUT", path, string(raw), f.tokens[0], 400, nil)
			if strings.Contains(rw.Body.String(), canary) {
				t.Fatal("wire error exposed content")
			}
		})
	}
	for _, c := range []struct {
		name, token string
		workspace   *string
	}{
		{"active_org", f.tokens[2], nil}, {"reserved_business", f.tokens[3], nil}, {"owner_org_workspace", f.tokens[0], &f.accountIDs[2]}, {"admin_org_workspace", f.tokens[1], &f.accountIDs[2]},
	} {
		t.Run(c.name, func(t *testing.T) {
			rw := f.request(t, f.handler, "PUT", path, memoryDBHTTPBody(t, created.Version, "purpose-062", canary), c.token, 403, c.workspace)
			if strings.Contains(rw.Body.String(), canary) || strings.Contains(rw.Body.String(), id) {
				t.Fatal("denial leaked body/reference")
			}
		})
	}
	var after string
	if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1`, id).Scan(&after); err != nil || before != after {
		t.Fatal("invalid purpose/recipient changed complete native Memory row")
	}
	owner := f.request(t, f.handler, "GET", memoryHTTPList, "", f.tokens[0], 200, nil)
	var envelope struct {
		Data []agentmemory.Record `json:"data"`
	}
	if err := json.Unmarshal(owner.Body.Bytes(), &envelope); err != nil || len(envelope.Data) != 1 || !reflect.DeepEqual(envelope.Data[0], created) {
		t.Fatal("native own explicit memory regression")
	}
	t.Log("LOCAL_SYNTHETIC_NATIVE_ONLY: registered existing human routes reject purpose/retention/owner injection; no new machine route or Activity delegation resolver")
}
