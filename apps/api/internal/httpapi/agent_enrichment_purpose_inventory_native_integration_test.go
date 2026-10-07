package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// All permission rows originate in the original079 preview/approve routes.
// This is owned local native verification, not live identity/model activation.
func enrichmentInventoryNativeApprove(t *testing.T, f *enrichmentHTTPFixture) aep.Grant {
	t.Helper()
	w := f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews", enrichmentHTTPJSON(t, f.selection), f.tokens[0], 200, nil)
	p := enrichmentHTTPData[aep.Preview](t, w.Body.Bytes())
	if aep.ValidatePreview(p) != nil || p.State != "CURRENT_REVIEW" {
		t.Fatal("invalid original preview")
	}
	w = f.request(t, f.handler, "POST", enrichmentHTTPBase+"/previews/"+p.ID+"/approve", "", f.tokens[0], 200, nil)
	g := enrichmentHTTPData[aep.Grant](t, w.Body.Bytes())
	if aep.ValidateGrant(g) != nil || g.Revision != 1 || g.ModelAccess || g.CandidateRetentionAllowed {
		t.Fatal("invalid original local-only approval")
	}
	return g
}

func enrichmentInventoryNativeRead(t *testing.T, f *enrichmentHTTPFixture, token string) aep.Inventory {
	t.Helper()
	w := f.request(t, f.handler, "GET", enrichmentHTTPBase+"/grants", "", token, 200, nil)
	var wire struct{ Data aep.Inventory }
	if json.Unmarshal(w.Body.Bytes(), &wire) != nil || aep.ValidateInventory(wire.Data) != nil || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid native metadata inventory")
	}
	for _, bad := range []string{f.moment.Body, f.moment.Title, "CHANGED_LOCAL_MOMENT_CANARY", `"review"`, `"authority"`, `"rowToken"`, `"session"`, `"taskQuery"`} {
		if strings.Contains(w.Body.String(), bad) {
			t.Fatal("inventory disclosed source/control", bad)
		}
	}
	for _, g := range wire.Data.Grants {
		if g.ModelAccess || g.CandidateRetentionAllowed {
			t.Fatal("history observation created runtime/retention permission")
		}
	}
	return wire.Data
}

func enrichmentInventoryNativeExact(t *testing.T, row, original aep.Grant) {
	t.Helper()
	row.ObservedAt = original.ObservedAt
	if !reflect.DeepEqual(row, original) {
		t.Fatal("inventory replaced original selection/owner/Agent/ID/version/time")
	}
}

func enrichmentInventoryNativeRows(t *testing.T, f *enrichmentHTTPFixture) string {
	t.Helper()
	var raw string
	err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'grants',(SELECT coalesce(jsonb_agg(to_jsonb(g) ORDER BY id),'[]') FROM consent_grants g WHERE owner_account_id=ANY($1::uuid[])),
 'moments',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]') FROM moments m WHERE author_account_id=ANY($1::uuid[])),
 'tasks',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM agent_tasks t WHERE owner_account_id=ANY($1::uuid[])),
 'memories',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]') FROM agent_memories m WHERE owner_id=ANY($1::uuid[])),
 'messages',(SELECT count(*) FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])),
 'inbox',(SELECT count(*) FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])))::text`, f.accountIDs).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEnrichmentInventoryNativeOwnerHistoryAcrossSessionAndNoEffects(t *testing.T) {
	f := enrichmentHTTPNative(t)
	g := enrichmentInventoryNativeApprove(t, f)
	before := enrichmentInventoryNativeRows(t, f)
	for _, token := range []string{f.tokens[0], f.newSession(f.accountIDs[0], false, false)} {
		v := enrichmentInventoryNativeRead(t, f, token)
		if v.Owner.ID != f.accountIDs[0] || v.AgentID != f.agentIDs[0] || len(v.Grants) != 1 || v.Truncated {
			t.Fatal("own original history absent")
		}
		enrichmentInventoryNativeExact(t, v.Grants[0], g)
	}
	v := enrichmentInventoryNativeRead(t, f, f.tokens[1])
	if v.Grants == nil || len(v.Grants) != 0 {
		t.Fatal("foreign owner borrowed history")
	}
	for _, token := range []string{"", "invalid", f.newSession(f.accountIDs[0], true, false), f.newSession(f.accountIDs[0], false, true)} {
		w := f.request(t, f.handler, "GET", enrichmentHTTPBase+"/grants", "", token, 401, nil)
		if strings.Contains(w.Body.String(), g.ID) || strings.Contains(w.Body.String(), `"data"`) {
			t.Fatal("invalid session received private history")
		}
	}
	f.request(t, f.handler, "GET", enrichmentHTTPBase+"/grants", "", f.tokens[2], 403, nil)
	f.request(t, f.handler, "GET", enrichmentHTTPBase+"/grants", "", f.tokens[0], 403, &f.accountIDs[2])
	if before != enrichmentInventoryNativeRows(t, f) {
		t.Fatal("metadata observation changed original source, permission or action domain")
	}
}

func TestEnrichmentInventoryNativeStaleSourceRevokeFromNewOwnSession(t *testing.T) {
	f := enrichmentHTTPNative(t)
	g := enrichmentInventoryNativeApprove(t, f)
	_, err := f.store.UpdateMomentDraft(f.ctx, f.accountIDs[0], f.moment.ID, f.moment.Revision, content.MomentInput{CityID: f.city, Title: f.moment.Title, Body: "CHANGED_LOCAL_MOMENT_CANARY", TimePrecision: "unknown", LocationPrecision: "city"})
	if err != nil {
		t.Fatal(err)
	}
	newSession := f.newSession(f.accountIDs[0], false, false)
	v := enrichmentInventoryNativeRead(t, f, newSession)
	if len(v.Grants) != 1 {
		t.Fatal("stale source prevented human metadata/history revoke")
	}
	enrichmentInventoryNativeExact(t, v.Grants[0], g)
	path := enrichmentHTTPBase + "/grants/" + g.ID
	f.request(t, f.handler, "DELETE", path, `{"expectedRevision":99}`, newSession, 409, nil)
	w := f.request(t, f.handler, "DELETE", path, `{"expectedRevision":1}`, newSession, 200, nil)
	revoked := enrichmentHTTPData[aep.Grant](t, w.Body.Bytes())
	if revoked.Revision != 2 || revoked.RevokedAt == nil {
		t.Fatal("original exact-version revoke failed")
	}
	f.request(t, f.handler, "DELETE", path, `{"expectedRevision":1}`, newSession, 200, nil)
	v = enrichmentInventoryNativeRead(t, f, newSession)
	if len(v.Grants) != 1 || v.Grants[0].Revision != 2 || v.Grants[0].RevokedAt == nil || !v.Grants[0].RevokedAt.Equal(*revoked.RevokedAt) {
		t.Fatal("revoke retry renewed/duplicated lifecycle")
	}
}

func TestEnrichmentInventoryNativeActiveFirst50AndTruncated(t *testing.T) {
	f := enrichmentHTTPNative(t)
	active := enrichmentInventoryNativeApprove(t, f)
	ids := map[string]bool{active.ID: true}
	for i := 0; i < 51; i++ {
		g := enrichmentInventoryNativeApprove(t, f)
		if ids[g.ID] {
			t.Fatal("new preview borrowed another approval")
		}
		ids[g.ID] = true
		f.request(t, f.handler, "DELETE", enrichmentHTTPBase+"/grants/"+g.ID, `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	}
	before := enrichmentInventoryNativeRows(t, f)
	v := enrichmentInventoryNativeRead(t, f, f.tokens[0])
	if !v.Truncated || v.Limit != 50 || len(v.Grants) != 50 || v.Grants[0].ID != active.ID || v.Grants[0].RevokedAt != nil {
		t.Fatal("older active grant hidden behind revoked history")
	}
	seen := map[string]bool{}
	for i, g := range v.Grants {
		if !ids[g.ID] || seen[g.ID] || (i > 0 && (g.RevokedAt == nil || g.Revision != 2)) {
			t.Fatal("unknown, duplicate or wrong lifecycle row")
		}
		seen[g.ID] = true
		if i > 1 && g.CreatedAt.After(v.Grants[i-1].CreatedAt) {
			t.Fatal("revoked metadata not latest first")
		}
	}
	if before != enrichmentInventoryNativeRows(t, f) {
		t.Fatal("bounded history read changed source or permission")
	}
	t.Logf("LOCAL_SYNTHETIC original079 approvals=%d metadata=%d truncated=%t", len(ids), len(v.Grants), v.Truncated)
}

func TestEnrichmentInventoryNativeFinalCurrentSessionAndAgentAfterEncoding(t *testing.T) {
	for _, change := range []string{"unchanged", "refresh", "revoke", "suspend", "retire-agent"} {
		t.Run(change, func(t *testing.T) {
			f := enrichmentHTTPNative(t)
			g := enrichmentInventoryNativeApprove(t, f)
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
			f.handler = contextBuilderHTTPNew(f.store, access, f.store)
			want := http.StatusUnauthorized
			if change == "unchanged" || change == "refresh" {
				want = http.StatusOK
			}
			if change == "retire-agent" {
				want = http.StatusForbidden
			}
			w := f.request(t, f.handler, "GET", enrichmentHTTPBase+"/grants", "", f.tokens[0], want, nil)
			if access.calls != 1 {
				t.Fatal("inventory bypassed original native final Session guard")
			}
			if want != 200 && (strings.Contains(w.Body.String(), g.ID) || strings.Contains(w.Body.String(), `"data"`)) {
				t.Fatal("late-denied encoded history escaped")
			}
		})
	}
}
