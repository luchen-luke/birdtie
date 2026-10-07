package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

// Every source, preview, approval and inventory below uses the original native
// Store/registered HTTP on owned disposable principals. No grant is fabricated.
func contextInventoryNativeRead(t *testing.T, f *contextPurposeHTTPFixture, token string) acb.PurposeInventory {
	t.Helper()
	w := f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants", "", token, 200, nil)
	var wire struct{ Data acb.PurposeInventory }
	if e := json.Unmarshal(w.Body.Bytes(), &wire); e != nil || acb.ValidatePurposeInventory(wire.Data) != nil {
		t.Fatal("invalid actual native inventory", e)
	}
	for _, forbidden := range []string{contextPurposeHTTPMemory, contextPurposeHTTPUnselected, "STRUCTURED_VALUE_NOT_IN_RUNTIME", "周末下午", "UNREQUESTED_CONTEXT_BODY_CANARY", "queryDigest", "rowToken", "authority", "review", "currentQuery", "session"} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatal("inventory disclosed contents/control instead of metadata", forbidden)
		}
	}
	return wire.Data
}

func contextInventoryNativeExact(t *testing.T, f *contextPurposeHTTPFixture, row acb.PurposeInventoryGrant, grant acb.PurposeGrant) {
	t.Helper()
	s := grant.Selection
	if row.ID != grant.ID || row.Revision != grant.Revision || row.Purpose != acb.TaskContextRead || row.TaskID != s.TaskID || row.CityID != s.CityID || !row.TaskUpdatedAt.Equal(s.TaskUpdatedAt) ||
		!reflect.DeepEqual(row.ProfileFields, s.ProfileFields) || !reflect.DeepEqual(row.MemoryIDs, s.MemoryIDs) || !reflect.DeepEqual(row.PlaceIDs, s.PlaceIDs) || !reflect.DeepEqual(row.ActivityIDs, s.ActivityIDs) || !reflect.DeepEqual(row.RelationshipTieIDs, s.RelationshipTieIDs) || !reflect.DeepEqual(row.PolicyFamilies, s.PolicyFamilies) || !row.CreatedAt.Equal(grant.CreatedAt) || !row.ExpiresAt.Equal(grant.ExpiresAt) {
		t.Fatal("inventory replaced the original grant/selection/version", row.ID)
	}
}

func contextInventoryNativeLifecycle(t *testing.T, f *contextPurposeHTTPFixture) string {
	t.Helper()
	var raw string
	if e := f.pool.QueryRow(f.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(g) ORDER BY g.id),'[]'::jsonb)::text FROM consent_grants g WHERE g.owner_account_id=ANY($1::uuid[])`, f.accountIDs).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw
}

func TestTaskContextInventoryNativeSessionOwnerMetadataAndNoEffects(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	g := f.approve(t, f.preview(t))
	beforeRows, beforeLifecycle := f.readonlyRows(t), contextInventoryNativeLifecycle(t, f)
	v := contextInventoryNativeRead(t, f, f.tokens[0])
	if v.Owner.ID != f.accountIDs[0] || v.AgentID != f.agentIDs[0] || v.Truncated || len(v.Grants) != 1 {
		t.Fatal("wrong original owner inventory")
	}
	contextInventoryNativeExact(t, f, v.Grants[0], g)
	for _, token := range []string{f.newSession(f.accountIDs[0], false, false), f.tokens[1]} {
		v := contextInventoryNativeRead(t, f, token)
		if v.Grants == nil || len(v.Grants) != 0 || v.Truncated {
			t.Fatal("another session/owner borrowed old approval")
		}
		f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants/"+g.ID, "", token, 403, nil)
	}
	for _, token := range []string{"", "invalid", f.newSession(f.accountIDs[0], true, false), f.newSession(f.accountIDs[0], false, true)} {
		w := f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants", "", token, 401, nil)
		if strings.Contains(w.Body.String(), g.ID) || strings.Contains(w.Body.String(), `"data"`) {
			t.Fatal("invalid session received inventory")
		}
	}
	f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants", "", f.tokens[2], 403, nil)
	f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants", "", f.tokens[0], 403, &f.accountIDs[2])
	if beforeRows != f.readonlyRows(t) || beforeLifecycle != contextInventoryNativeLifecycle(t, f) {
		t.Fatal("inventory read modified source, lifecycle or action domain")
	}
}

func TestTaskContextInventoryNativeStaleSourceStillRevokable(t *testing.T) {
	for _, change := range []string{"task", "memory", "place"} {
		t.Run(change, func(t *testing.T) {
			f := contextPurposeHTTPNative(t)
			g := f.approve(t, f.preview(t))
			switch change {
			case "task":
				f.exec(`UPDATE agent_tasks SET query='原Task已变化',updated_at=clock_timestamp() WHERE id=$1`, f.task.ID)
			case "memory":
				body := contextPurposeHTTPMemoryBody(t, f.memory.Version, f.memory.ValidUntil)
				body = strings.Replace(body, contextPurposeHTTPMemory, "当前记忆已改变", 1)
				f.request(t, f.handler, "PUT", memoryHTTPList+"/"+f.memory.ID, body, f.tokens[0], 200, nil)
			case "place":
				f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
			}
			path := contextPurposeHTTPBase + "/grants/" + g.ID
			f.request(t, f.handler, "GET", path, "", f.tokens[0], 403, nil)
			v := contextInventoryNativeRead(t, f, f.tokens[0])
			if len(v.Grants) != 1 {
				t.Fatal("stale source prevented human metadata revocation path")
			}
			contextInventoryNativeExact(t, f, v.Grants[0], g)
			f.request(t, f.handler, "DELETE", path, `{"expectedRevision":99}`, f.tokens[0], 403, nil)
			w := f.request(t, f.handler, "DELETE", path, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
			var revoked struct{ Data acb.PurposeGrant }
			if json.Unmarshal(w.Body.Bytes(), &revoked) != nil || revoked.Data.Revision != 2 || revoked.Data.RevokedAt == nil {
				t.Fatal("original exact revision revoke not committed")
			}
			f.request(t, f.handler, "DELETE", path, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
			v = contextInventoryNativeRead(t, f, f.tokens[0])
			if len(v.Grants) != 1 || v.Grants[0].Revision != 2 || v.Grants[0].RevokedAt == nil || !v.Grants[0].RevokedAt.Equal(*revoked.Data.RevokedAt) {
				t.Fatal("revoke/retry created another lifecycle or inventory lost revision")
			}
			f.runtime(t, g, 403)
		})
	}
}

func TestTaskContextInventoryNativeActiveFirst50AndTruncated(t *testing.T) {
	f := contextPurposeHTTPNative(t)
	active := f.approve(t, f.preview(t))
	ids := map[string]bool{active.ID: true}
	for i := 0; i < 51; i++ {
		g := f.approve(t, f.preview(t))
		if ids[g.ID] {
			t.Fatal("new original preview reused another approval identity")
		}
		ids[g.ID] = true
		f.request(t, f.handler, "DELETE", contextPurposeHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	}
	beforeRows, beforeLifecycle := f.readonlyRows(t), contextInventoryNativeLifecycle(t, f)
	v := contextInventoryNativeRead(t, f, f.tokens[0])
	if !v.Truncated || v.Limit != 50 || len(v.Grants) != 50 || v.Grants[0].ID != active.ID || v.Grants[0].RevokedAt != nil {
		t.Fatal("native ordering discarded older still-active grant behind revoked history")
	}
	seen := map[string]bool{}
	for i, row := range v.Grants {
		if !ids[row.ID] || seen[row.ID] || (i > 0 && (row.RevokedAt == nil || row.Revision != 2)) {
			t.Fatal("unknown/duplicate/unrevoked native inventory row")
		}
		seen[row.ID] = true
		if i > 1 && row.CreatedAt.After(v.Grants[i-1].CreatedAt) {
			t.Fatal("revoked history ordering not actual newest first")
		}
	}
	if beforeRows != f.readonlyRows(t) || beforeLifecycle != contextInventoryNativeLifecycle(t, f) {
		t.Fatal("bounded inventory mutated native lifecycle/source")
	}
	t.Logf("LOCAL_SYNTHETIC original approved grants=%d inventory=%d truncated=%t activeOriginal=%s", len(ids), len(v.Grants), v.Truncated, active.ID)
}

type contextInventoryNativeFinalSession struct {
	*postgres.Store
	before func(context.Context, [32]byte, identity.Actor) error
	calls  int
}

func (s *contextInventoryNativeFinalSession) ValidateHumanSocialResponse(ctx context.Context, digest [32]byte, actor identity.Actor) error {
	s.calls++
	if s.before != nil {
		if e := s.before(ctx, digest, actor); e != nil {
			return e
		}
	}
	return s.Store.ValidateHumanSocialResponse(ctx, digest, actor)
}

func TestTaskContextInventoryNativeFinalCurrentSessionAfterEncoding(t *testing.T) {
	for _, change := range []string{"unchanged", "refresh", "revoke", "suspend", "retire-agent"} {
		t.Run(change, func(t *testing.T) {
			f := contextPurposeHTTPNative(t)
			g := f.approve(t, f.preview(t))
			access := &contextInventoryNativeFinalSession{Store: f.store}
			access.before = func(ctx context.Context, digest [32]byte, actor identity.Actor) error {
				switch change {
				case "refresh":
					_, e := f.store.Authenticate(ctx, digest)
					return e
				case "revoke":
					return f.store.RevokeSession(ctx, digest)
				case "suspend":
					_, e := f.pool.Exec(ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, actor.ID)
					return e
				case "retire-agent":
					_, e := f.pool.Exec(ctx, `UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
					return e
				}
				return nil
			}
			f.handler = New(f.store, access, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
			want := http.StatusUnauthorized
			if change == "unchanged" || change == "refresh" {
				want = http.StatusOK
			}
			if change == "retire-agent" {
				want = http.StatusForbidden
			}
			w := f.request(t, f.handler, "GET", contextPurposeHTTPBase+"/grants", "", f.tokens[0], want, nil)
			if access.calls != 1 {
				t.Fatal("inventory bypassed native final response guard")
			}
			if want != http.StatusOK && (strings.Contains(w.Body.String(), g.ID) || strings.Contains(w.Body.String(), `"data"`)) {
				t.Fatal("old encoded inventory escaped native current session guard")
			}
		})
	}
}
