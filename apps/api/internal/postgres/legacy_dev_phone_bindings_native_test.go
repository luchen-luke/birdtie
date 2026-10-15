package postgres

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

// One actual routed/native chain, confined to a freshly owned synthetic child.
func TestLegacyDevPhoneBindingsNativeFixedLoginOriginalOwnerAndAtomicConflict(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	source, manifest := legacySyntheticInput(t, func(ts map[string]any) {
		row := func(table string) map[string]any {
			r := map[string]any{}
			for _, k := range legacymigration.ColumnNames(table) {
				r[k] = nil
			}
			return r
		}
		first := ts["users"].([]any)[0].(map[string]any)
		second := row("users")
		for k, v := range first {
			second[k] = v
		}
		second["id"] = "14600000-0000-4000-8000-000000000011"
		second["display_name"] = "第二合成旧用户"
		ts["users"] = []any{first, second}
		phones := []any{}
		for i, phone := range []string{"+44-7700-900001", "+447700900002"} {
			a := row("auth_identities")
			a["id"] = []string{"15400000-0000-4000-8000-000000000001", "15400000-0000-4000-8000-000000000002"}[i]
			a["user_id"] = []string{first["id"].(string), second["id"].(string)}[i]
			a["provider"] = "phone"
			a["subject"] = phone
			phones = append(phones, a)
		}
		ts["auth_identities"] = phones
		theme := row("theme_maps")
		theme["id"] = "15400000-0000-4000-8000-000000000003"
		theme["owner_id"] = first["id"]
		theme["title"] = "仅本人原主题地图"
		theme["revision"] = 1
		for _, k := range []string{"summary", "cover_url", "audit_status", "moderation_status", "commercial_disclosure", "topic", "review_reason", "decision_reason"} {
			theme[k] = ""
		}
		theme["visibility"] = "private"
		theme["status"] = "active"
		theme["operator_created"] = false
		theme["created_at"] = "2026-10-13T00:00:00Z"
		theme["updated_at"] = "2026-10-14T00:00:00Z"
		ts["theme_maps"] = []any{theme}
	})
	if _, e := f.Importer.Apply(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("original synthetic archive import", e)
	}
	if _, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("original synthetic personal archive import", e)
	}
	var owner, secondOwner, archive string
	if e := f.Pool.QueryRow(f.Context, `SELECT target_id::text FROM legacy_entity_mappings WHERE entity_kind='user' AND source_id='14600000-0000-4000-8000-000000000010'`).Scan(&owner); e != nil {
		t.Fatal("first exact mapping")
	}
	if e := f.Pool.QueryRow(f.Context, `SELECT target_id::text FROM legacy_entity_mappings WHERE entity_kind='user' AND source_id='14600000-0000-4000-8000-000000000011'`).Scan(&secondOwner); e != nil {
		t.Fatal("second exact mapping")
	}
	if e := f.Pool.QueryRow(f.Context, `SELECT id::text FROM legacy_note_archives WHERE NOT deleted AND owner_account_id=$1`, owner).Scan(&archive); e != nil {
		t.Fatal("original archive ID")
	}
	store := New(f.Pool, true)
	router := httpapi.New(store, store, nil, store, nil, nil, nil, nil, nil, nil, nil, store, true, nil, nil, nil)
	request := func(method, path, token string, input any) *httptest.ResponseRecorder {
		var body bytes.Buffer
		if input != nil {
			if json.NewEncoder(&body).Encode(input) != nil {
				t.Fatal("wire fixture")
			}
		}
		req := httptest.NewRequest(method, path, &body)
		if input != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	// A native dev identity already assigned to another owner blocks the entire
	// two-phone import; no first binding/account activation may leak through.
	plan, e := legacymigration.BuildDevPhonePlan(legacyTestRunID, source, manifest)
	if e != nil {
		t.Fatal("binding source")
	}
	conflictDigest := plan.Bindings[len(plan.Bindings)-1].PhoneDigest
	targetOfConflict := owner
	if plan.Bindings[len(plan.Bindings)-1].SourceOwnerID == "14600000-0000-4000-8000-000000000010" {
		targetOfConflict = secondOwner
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO account_auth_identities(issuer,subject,account_id,verified_at) VALUES($1,$2,$3,clock_timestamp())`, devPhoneIssuer, hex.EncodeToString(conflictDigest[:]), targetOfConflict); e != nil {
		t.Fatal("owned conflict fixture")
	}
	if _, e = f.Importer.ApplyDevPhoneBindings(f.Context, legacyTestRunID, source, manifest); !errors.Is(e, ErrLegacyMigrationConflict) {
		t.Fatal("foreign identity conflict was accepted", e)
	}
	var bindings, active int
	if e = f.Pool.QueryRow(f.Context, `SELECT (SELECT count(*) FROM legacy_dev_phone_bindings),(SELECT count(*) FROM accounts a JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=a.id WHERE a.status='active')`).Scan(&bindings, &active); e != nil || bindings != 0 || active != 0 {
		t.Fatal("conflict leaked partial binding or activation", bindings, active)
	}
	if _, e = f.Pool.Exec(f.Context, `DELETE FROM account_auth_identities WHERE issuer=$1 AND subject=$2`, devPhoneIssuer, hex.EncodeToString(conflictDigest[:])); e != nil {
		t.Fatal("owned conflict cleanup")
	}
	result, e := f.Importer.ApplyDevPhoneBindings(f.Context, legacyTestRunID, source, manifest)
	if e != nil || result.Status != "DEV_PHONE_BINDINGS_COMMITTED" || result.Counts.BindableOwners != 2 || result.EnrolledOwners != 0 {
		t.Fatal("complete digest-only binding import", e)
	}
	again, e := f.Importer.ApplyDevPhoneBindings(f.Context, legacyTestRunID, source, manifest)
	if e != nil || again != result {
		t.Fatal("binding replay changed immutable facts", e)
	}
	login := func(phone string, expected int) string {
		w := request("POST", "/v1/auth/dev-phone/code", "", map[string]string{"phone": phone})
		if w.Code != 200 {
			t.Fatal("actual code route", w.Code)
		}
		w = request("POST", "/v1/auth/dev-phone/verify", "", map[string]string{"phone": phone, "code": devauth.Code})
		if w.Code != expected {
			t.Fatal("actual fixed-code route", w.Code, "expected", expected)
		}
		if expected != 200 {
			return ""
		}
		var data struct {
			Data struct {
				AccessToken string `json:"accessToken"`
			}
		}
		if json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Data.AccessToken == "" {
			t.Fatal("new current session missing")
		}
		return data.Data.AccessToken
	}
	token := login("+447700900001", 200)
	w := request("GET", "/v1/me", token, nil)
	var me struct{ Data identity.Actor }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &me) != nil || me.Data.ID != owner {
		t.Fatal("first login resolved a new or foreign owner", w.Code)
	}
	w = request("GET", "/v1/me/migration-archive/"+archive, token, nil)
	var note struct{ Data legacyarchive.Detail }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &note) != nil || note.Data.Body != "仅测试旧正文" {
		t.Fatal("same owner original note not reachable", w.Code)
	}
	w = request("GET", "/v1/me/migration-archive-records?kind=theme_map", token, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "仅本人原主题地图") {
		t.Fatal("same owner original personal archive not reachable", w.Code)
	}
	var paused, unclaimed, sessions, enrolled, audits int
	if e = f.Pool.QueryRow(f.Context, `SELECT (SELECT count(*) FROM agents ag JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=ag.principal_account_id WHERE ag.status='suspended'),(SELECT count(*) FROM legacy_entity_mappings WHERE claim_status='UNCLAIMED'),(SELECT count(*) FROM sessions s JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=s.account_id WHERE s.authentication_method='dev_phone'),(SELECT count(first_enrolled_at) FROM legacy_dev_phone_bindings),(SELECT count(*) FROM audit_events WHERE purpose='FIRST_FIXED_TEST_LOGIN')`).Scan(&paused, &unclaimed, &sessions, &enrolled, &audits); e != nil || paused != 2 || unclaimed != 4 || sessions != 1 || enrolled != 1 || audits != 1 {
		t.Fatal("first enrollment activated Agent, changed mapping, or copied sessions")
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE legacy_dev_phone_bindings SET first_enrolled_at=NULL WHERE first_enrolled_at IS NOT NULL`); e == nil {
		t.Fatal("first enrollment receipt could be cleared")
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE legacy_dev_phone_bindings SET source_owner_sha256=repeat('0',64)`); e == nil {
		t.Fatal("immutable source binding could be rewritten")
	}
	// Preserve original throttling. Only the synthetic challenge's old request
	// clock is advanced; no sleep and no product rate-limit exception.
	if _, e = f.Pool.Exec(f.Context, `DELETE FROM dev_phone_challenges`); e != nil {
		t.Fatal("owned challenge reset")
	}
	repeat := login("+44 7700 900001", 200)
	w = request("GET", "/v1/me", repeat, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &me) != nil || me.Data.ID != owner {
		t.Fatal("repeat login changed owner")
	}
	other := login("+447700900002", 200)
	w = request("GET", "/v1/me/migration-archive/"+archive, other, nil)
	if w.Code != 404 {
		t.Fatal("foreign owner read private original archive", w.Code)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='suspended' WHERE id=$1`, owner); e != nil {
		t.Fatal("owned later suspension")
	}
	login("+447700900001", 403)
	if e = f.Pool.QueryRow(f.Context, `SELECT (SELECT count(*) FROM accounts WHERE id=$1 AND status='suspended'),(SELECT count(*) FROM agents ag JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=ag.principal_account_id WHERE ag.status='suspended'),(SELECT count(first_enrolled_at) FROM legacy_dev_phone_bindings),(SELECT count(*) FROM sessions s JOIN legacy_entity_mappings m ON m.entity_kind='user' AND m.target_id=s.account_id WHERE s.authentication_method='dev_phone')`, owner).Scan(&active, &paused, &enrolled, &sessions); e != nil || active != 1 || paused != 2 || enrolled != 2 || sessions != 3 {
		t.Fatal("later fixed login revived owner or duplicated effects")
	}
	after, e := f.Importer.ApplyDevPhoneBindings(f.Context, legacyTestRunID, source, manifest)
	if e != nil || after.Counts != result.Counts || after.EnrolledOwners != 2 {
		t.Fatal("idempotent import did not re-read current enrollment counts", e)
	}
	firstDigest := devauth.PhoneDigest("+447700900001")
	if _, e = f.Pool.Exec(f.Context, `DELETE FROM account_auth_identities WHERE issuer=$1 AND subject=$2`, devPhoneIssuer, hex.EncodeToString(firstDigest[:])); e != nil {
		t.Fatal("owned identity removal")
	}
	// Removing a native identity cannot reset immutable first enrollment.
	if _, e = f.Pool.Exec(f.Context, `UPDATE dev_phone_challenges SET requested_at=clock_timestamp()-interval '61 seconds'`); e != nil {
		t.Fatal("owned throttle fixture")
	}
	login("+447700900001", 403)
}
