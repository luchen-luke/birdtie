package httpapi

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
	"strings"
	"testing"
	"time"
)

func TestSupplierRegisteredNativeDisclosureLifecycle(t *testing.T) {
	f := businessConsoleHTTPNew(t)
	t.Cleanup(func() {
		for _, table := range []string{"business_public_profile_audit", "business_public_profile_permissions"} {
			f.exec(t, `DELETE FROM `+table+` WHERE business_id=$1`, f.business)
		}
	})
	f.claim(t)
	profile := businessconsole.ProfileInput{Facts: businessconsole.ProfileFacts{Name: "明确批准的合成商家", Description: "只有本人公开批准后可见", TimeZone: "Europe/London", OpeningHours: []businessconsole.HoursDay{}, OfficialLinks: []string{"https://example.invalid/official"}}, SourceURL: "https://example.invalid/PRIVATE_PROOF", RightsNote: "PRIVATE_RIGHTS", ValidUntil: time.Now().UTC().Add(time.Hour)}
	f.request(t, "PUT", f.path("/profile"), f.tokens[0], businessConsoleHTTPJSON(t, profile), 200)
	f.request(t, "POST", f.path("/profile/review"), f.tokens[1], businessConsoleHTTPJSON(t, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "本地合成材料独立审阅"}), 200)
	public := "/v1/businesses/" + f.business
	initial := f.request(t, "GET", public, "", "", 200)
	if strings.Contains(initial.Body.String(), "只有本人公开批准后可见") || !strings.Contains(initial.Body.String(), `"profileStatus":"unpublished"`) {
		t.Fatal(initial.Body.String())
	}
	permission := f.path("/public-profile/permission")
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {"bad-token", 401}, {f.tokens[1], 403}, {f.tokens[2], 403}, {f.tokens[4], 403}, {f.tokens[5], 403}} {
		f.request(t, "GET", permission, tc.token, "", tc.want)
	}
	var view struct {
		Data supplierprofile.PermissionView `json:"data"`
	}
	raw := f.request(t, "GET", permission, f.tokens[0], "", 200)
	if e := json.Unmarshal(raw.Body.Bytes(), &view); e != nil || view.Data.Preview == nil {
		t.Fatal(view, e)
	}
	p := view.Data.Preview
	in := supplierprofile.PermissionInput{ExpectedProfileVersion: p.ProfileVersion, Action: "publish", ValidUntil: p.ValidUntil.Format(time.RFC3339Nano), SourceSnapshot: p.SourceSnapshot}
	body := businessConsoleHTTPJSON(t, in)
	f.request(t, "PUT", permission, f.tokens[0], body, 200)
	f.request(t, "PUT", permission, f.tokens[0], body, 200)
	final := f.request(t, "GET", public, "", "", 200)
	if !strings.Contains(final.Body.String(), "只有本人公开批准后可见") {
		t.Fatal(final.Body.String())
	}
	for _, secret := range []string{"PRIVATE_PROOF", "PRIVATE_RIGHTS", "sourceSnapshot", "reviewedBy"} {
		if strings.Contains(final.Body.String(), secret) {
			t.Fatal("private field exposed", secret)
		}
	}
	f.request(t, "GET", public, "bad-token", "", 401)
	for _, path := range []string{public + "?x=1", public + "?", "/v1/businesses/not-a-uuid"} {
		f.request(t, "GET", path, "", "", 400)
	}
	f.request(t, "GET", public, "", "{}", 400)
	f.request(t, "PUT", permission, f.tokens[0], strings.TrimSuffix(body, "}")+`,"unknown":true}`, 400)
	f.request(t, "PUT", permission, f.tokens[0], businessConsoleHTTPJSON(t, supplierprofile.PermissionInput{ExpectedVersion: 1, Action: "revoke"}), 200)
	final = f.request(t, "GET", public, "", "", 200)
	if strings.Contains(final.Body.String(), "只有本人公开批准后可见") {
		t.Fatal("revocation leak", final.Body.String())
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.ids[0])
	f.request(t, "GET", permission, f.tokens[0], "", 401)
	f.request(t, "GET", public, f.tokens[0], "", 401)
}
