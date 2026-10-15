package legacymigration

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/devauth"
)

func devPhoneSyntheticTables() map[string]any {
	ts := syntheticTables()
	owner := ts["users"].([]any)[0].(map[string]any)
	owner["status"] = "active"
	second := baseRow("users", map[string]any{"id": testDeleted, "version": json.Number("1"), "status": "active", "data_origin": "synthetic_test", "display_name": "测试乙", "bio": "", "handle": nil})
	emailOnly := baseRow("users", map[string]any{"id": testPlace, "version": json.Number("1"), "status": "active", "data_origin": "synthetic_test", "display_name": "仅邮箱测试", "bio": "", "handle": nil})
	ts["users"] = []any{owner, second, emailOnly}
	ts["auth_identities"] = []any{
		baseRow("auth_identities", map[string]any{"id": testNote, "user_id": testOwner, "provider": "phone", "subject": "+44-7700-900001"}),
		baseRow("auth_identities", map[string]any{"id": testDeleted, "user_id": testDeleted, "provider": "phone", "subject": "+447700900002"}),
		baseRow("auth_identities", map[string]any{"id": testPlace, "user_id": testPlace, "provider": "email", "subject": "email-only@example.invalid"}),
		baseRow("auth_identities", map[string]any{"id": testMedia, "user_id": testOwner, "provider": "email", "subject": "second@example.invalid"}),
	}
	return ts
}

func TestLegacyDevPhonePlanCanonicalExactOwnerAndClosedSource(t *testing.T) {
	ts := devPhoneSyntheticTables()
	source, manifest := documents(t, ts)
	original := append([]byte(nil), source...)
	p, e := BuildDevPhonePlan(testRun, source, manifest)
	if e != nil || p.Counts != (DevPhoneCounts{Owners: 3, PhoneIdentities: 2, BindableOwners: 2, EmailOnlyOwners: 1}) || len(p.Bindings) != 2 {
		t.Fatal("closed canonical test binding plan", e)
	}
	digest := devauth.PhoneDigest("+447700900001")
	found := false
	for _, b := range p.Bindings {
		if b.PhoneDigest == digest {
			found = b.SourceOwnerID == testOwner && b.SourceOwnerVersion == 7 && b.SourceOwnerSHA256 == p.Original.Owners[0].SourceSHA256 && shaRE.MatchString(b.SourceIdentitySHA256)
		}
	}
	if !found || !bytes.Equal(original, source) {
		t.Fatal("canonical identity lost or source mutated")
	}
	encoded, e := json.Marshal(p)
	if e != nil {
		t.Fatal("summary encoding")
	}
	for _, secret := range []string{"7700", "example.invalid", "SYNTHETIC_PASSWORD", testOwner} {
		if strings.Contains(string(encoded), secret) || strings.Contains(p.String(), secret) {
			t.Fatal("plan summary disclosed private input")
		}
	}
	changes := []func(map[string]any){
		func(ts map[string]any) {
			rows := ts["auth_identities"].([]any)
			rows[1].(map[string]any)["subject"] = "+44 7700 900001"
		},
		func(ts map[string]any) { ts["auth_identities"].([]any)[1].(map[string]any)["user_id"] = testOwner },
		func(ts map[string]any) { ts["auth_identities"].([]any)[1].(map[string]any)["id"] = testNote },
		func(ts map[string]any) { ts["auth_identities"].([]any)[1].(map[string]any)["user_id"] = testThread },
		func(ts map[string]any) { ts["users"].([]any)[0].(map[string]any)["status"] = "suspended" },
		func(ts map[string]any) {
			ts["auth_identities"].([]any)[0].(map[string]any)["subject"] = "invalid-phone"
		},
		func(ts map[string]any) { ts["auth_identities"].([]any)[0].(map[string]any)["provider"] = "unsupported" },
	}
	for _, change := range changes {
		bad := devPhoneSyntheticTables()
		change(bad)
		s, m := documents(t, bad)
		if _, e := BuildDevPhonePlan(testRun, s, m); e == nil {
			t.Fatal("ambiguous, orphan, inactive or invalid source accepted")
		}
	}
	if _, e := BuildDevPhonePlan(testRun, append(source, ' '), manifest); e == nil {
		t.Fatal("source-file guard bypassed by phone projection")
	}
}
