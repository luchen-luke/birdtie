package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Real registered middleware -> original human writer -> original native audit.
// Synthetic local identities are never production/IdP evidence.
func TestAuditCorrelationHTTPRegisteredNative(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	current := privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[0], 200, nil))
	body, _ := json.Marshal(map[string]any{"expectedVersion": current.Profile.ProfileVersion, "fields": map[string]any{"agentNotes": "PRIVATE_TRACE_BODY_CANARY"}})
	send := func(token, requestID string, want int) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, privateProfileHTTPPath, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Request-ID", requestID)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("status=%d want=%d", w.Code, want)
		}
		return w
	}
	w := send(f.tokens[0], "http_native_audit_001", 200)
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='agent_private_profile' AND resource_id=$2 AND request_id=$3`, f.accountIDs[0], f.agentIDs[0], w.Header().Get("X-Request-ID")).Scan(&n); e != nil || n != 1 {
		t.Fatal("registered request did not reach original audit", n, e)
	}
	// Same request ID does not grant permission or replay a stale CAS.
	send(f.tokens[0], "http_native_audit_001", 409)
	send("invalid-credential", "http_native_audit_001", 401)
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE request_id='http_native_audit_001'`).Scan(&n); e != nil || n != 1 {
		t.Fatal("failed/repeated request fabricated success", n, e)
	}
	current = privateProfileHTTPDBRecord(t, f.request(t, f.handler, http.MethodGet, privateProfileHTTPPath, "", f.tokens[0], 200, nil))
	body, _ = json.Marshal(map[string]any{"expectedVersion": current.Profile.ProfileVersion, "fields": map[string]any{}})
	generated := send(f.tokens[0], "请求编号不允许", 200).Header().Get("X-Request-ID")
	if !safeRequestID.MatchString(generated) || generated == "请求编号不允许" {
		t.Fatal("unsafe supplied correlation retained")
	}
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND request_id=$2`, f.accountIDs[0], generated).Scan(&n); e != nil || n != 1 {
		t.Fatal("generated trace missing", n, e)
	}
	var logged string
	if e := f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(a))::text FROM audit_events a WHERE actor_account_id=$1`, f.accountIDs[0]).Scan(&logged); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(logged, "PRIVATE_TRACE_BODY_CANARY") || strings.Contains(logged, f.tokens[0]) {
		t.Fatal("private body or session leaked into audit")
	}
}

func TestAuditCorrelationHTTPRegisteredOrganizationMapPermissions(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	var org, city string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&org); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM cities ORDER BY id LIMIT 1`).Scan(&city); err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role,status) VALUES($1,$2,'owner','active')`, org, f.accountIDs[0])
	f.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role,state) VALUES($1,$2,'reviewer','active'),($1,$3,'reviewer','active')`, city, f.accountIDs[0], f.accountIDs[1])
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM organization_map_location_audit WHERE organization_id=$1`, `DELETE FROM organization_map_locations WHERE organization_id=$1`} {
			if _, err := f.pool.Exec(context.Background(), q, org); err != nil {
				t.Error(err)
			}
		}
		if _, err := f.pool.Exec(context.Background(), `DELETE FROM city_editor_memberships WHERE account_id=ANY($1::uuid[])`, f.accountIDs); err != nil {
			t.Error(err)
		}
		var n int
		if err := f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM organization_map_location_audit WHERE organization_id=$1)+(SELECT count(*) FROM organization_map_locations WHERE organization_id=$1)`, org).Scan(&n); err != nil || n != 0 {
			t.Errorf("owned map residue=%d error=%v", n, err)
		}
	})
	path := "/v1/me/organizations/" + org + "/map-location"
	point := `{"cityId":"` + city + `","latitude":57.123456,"longitude":-2.123456}`
	call := func(method, suffix, body, token, trace string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, path+suffix, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Request-ID", trace)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != want || w.Header().Get("X-Request-ID") != trace {
			t.Fatalf("registered map %s status=%d want=%d trace=%s", method, w.Code, want, trace)
		}
	}
	count := func() int {
		t.Helper()
		var n int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM organization_map_location_audit WHERE organization_id=$1`, org).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	assertAudit := func(actor, action, trace string, revision int64, want int) {
		t.Helper()
		var n int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM organization_map_location_audit WHERE organization_id=$1 AND actor_account_id=$2 AND action=$3 AND revision=$4 AND request_id IS NOT DISTINCT FROM NULLIF($5,'')`, org, actor, action, revision, trace).Scan(&n); err != nil || n != want {
			t.Fatalf("exact map actor/org/revision/request audit=%d want=%d error=%v", n, want, err)
		}
	}
	call("GET", "", "", f.tokens[0], "map_audit_initial_read", 404)
	call("PUT", "", point, f.tokens[1], "map_audit_nonmember", 403)
	call("PUT", "", point, "", "map_audit_anonymous", 401)
	if count() != 0 {
		t.Fatal("GET/denied map request manufactured audit")
	}
	call("PUT", "", point, f.tokens[0], "map_audit_submit", 200)
	assertAudit(f.accountIDs[0], "submit", "map_audit_submit", 1, 1)
	call("POST", "/review", `{"decision":"approve","note":"PRIVATE_REVIEW_BODY_CANARY"}`, f.tokens[0], "map_audit_self_review", 409)
	call("POST", "/review", `{"decision":"approve","note":"PRIVATE_REVIEW_BODY_CANARY"}`, f.tokens[1], "map_audit_approve", 204)
	assertAudit(f.accountIDs[1], "approve", "map_audit_approve", 1, 1)
	call("POST", "/review", `{"decision":"approve","note":""}`, f.tokens[1], "map_audit_repeat_review", 409)
	call("GET", "", "", f.tokens[0], "map_audit_read", 200)
	if count() != 2 {
		t.Fatal("read/self-review/retry added successful audit")
	}
	call("DELETE", "", "", f.tokens[0], "map_audit_hide", 204)
	assertAudit(f.accountIDs[0], "hide", "map_audit_hide", 2, 1)
	call("PUT", "", point, f.tokens[0], "map_audit_resubmit", 200)
	assertAudit(f.accountIDs[0], "submit", "map_audit_resubmit", 3, 1)
	f.exec(`UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1 AND account_id=$2`, city, f.accountIDs[1])
	call("POST", "/review", `{"decision":"reject","note":""}`, f.tokens[1], "map_audit_revoked_reviewer", 403)
	f.exec(`UPDATE city_editor_memberships SET state='active' WHERE city_id=$1 AND account_id=$2`, city, f.accountIDs[1])
	call("POST", "/review", `{"decision":"reject","note":"PRIVATE_REVIEW_BODY_CANARY"}`, f.tokens[1], "map_audit_reject", 204)
	assertAudit(f.accountIDs[1], "reject", "map_audit_reject", 3, 1)
	f.exec(`UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[0])
	call("DELETE", "", "", f.tokens[0], "map_audit_revoked_owner", 403)
	if count() != 5 {
		t.Fatal("revoked map permission manufactured audit")
	}
	f.exec(`UPDATE organization_memberships SET status='active' WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[0])
	// Native background work has no fabricated HTTP trace and cannot inherit a pooled GUC.
	if err := f.store.HideMapLocation(context.Background(), f.accountIDs[0], org); err != nil {
		t.Fatal(err)
	}
	assertAudit(f.accountIDs[0], "hide", "", 4, 1)
	var raw string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(a))::text FROM organization_map_location_audit a WHERE organization_id=$1`, org).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"PRIVATE_REVIEW_BODY_CANARY", "57.123456", "-2.123456", f.tokens[0]} {
		if strings.Contains(raw, private) {
			t.Fatal("point/note/session leaked to map audit")
		}
	}
}
