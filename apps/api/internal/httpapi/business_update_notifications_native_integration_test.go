package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
)

func TestBusinessPublicUpdateRegisteredNativePublish(t *testing.T) {
	f := businessConsoleHTTPNew(t)
	t.Cleanup(func() {
		for _, table := range []string{"native_notification_decisions", "business_public_profile_audit", "business_public_profile_permissions"} {
			query := "DELETE FROM " + table + " WHERE business_id=$1"
			if table == "native_notification_decisions" {
				query = "DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[]) OR actor_id=ANY($1::uuid[])"
				f.exec(t, query, f.ids)
			} else {
				f.exec(t, query, f.business)
			}
		}
	})
	f.claim(t)
	p := businessconsole.ProfileInput{Facts: businessconsole.ProfileFacts{Name: "本地合成公开商家", Description: "明确公开批准的资料", TimeZone: "Europe/London", OpeningHours: []businessconsole.HoursDay{}, OfficialLinks: []string{"https://example.invalid/public"}}, SourceURL: "https://example.invalid/PRIVATE_PROOF", RightsNote: "PRIVATE_RIGHTS", ValidUntil: time.Now().UTC().Add(time.Hour)}
	f.request(t, "PUT", f.path("/profile"), f.tokens[0], businessConsoleHTTPJSON(t, p), 200)
	f.request(t, "POST", f.path("/profile/review"), f.tokens[1], businessConsoleHTTPJSON(t, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "具体合成资料版本"}), 200)
	f.request(t, "POST", "/v1/me/follows", f.tokens[2], businessConsoleHTTPJSON(t, map[string]string{"targetType": "BUSINESS", "targetId": f.business}), 201)
	var view struct {
		Data supplierprofile.PermissionView `json:"data"`
	}
	raw := f.request(t, "GET", f.path("/public-profile/permission"), f.tokens[0], "", 200)
	if e := json.Unmarshal(raw.Body.Bytes(), &view); e != nil || view.Data.Preview == nil {
		t.Fatal(e)
	}
	preview := view.Data.Preview
	input := supplierprofile.PermissionInput{Action: "publish", ExpectedProfileVersion: preview.ProfileVersion, SourceSnapshot: preview.SourceSnapshot, ValidUntil: preview.ValidUntil.Format(time.RFC3339Nano)}
	body := businessConsoleHTTPJSON(t, input)
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], body, 200)
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], body, 200)
	list := f.request(t, "GET", "/v1/me/inbox", f.tokens[2], "", 200)
	if strings.Count(list.Body.String(), `"resourceType":"business_update"`) != 1 || !strings.Contains(list.Body.String(), `"targetBusinessId":"`+f.business+`"`) {
		t.Fatal("real explicit publish must yield exactly one public business destination", list.Body.String())
	}
	for _, private := range []string{"PRIVATE_PROOF", "PRIVATE_RIGHTS", "明确公开批准的资料", "sourceSnapshot"} {
		if strings.Contains(list.Body.String(), private) {
			t.Fatal("notification copied source content", private)
		}
	}
	t.Log("ACTUAL_PUBLIC_UPDATE_INBOX_JSON " + list.Body.String())
	var inbox struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e := json.Unmarshal(list.Body.Bytes(), &inbox); e != nil || len(inbox.Data) != 1 {
		t.Fatal(e)
	}
	read := f.request(t, "POST", "/v1/me/inbox/"+inbox.Data[0].ID+"/read", f.tokens[2], "", 200)
	if !strings.Contains(read.Body.String(), `"targetBusinessId":"`+f.business+`"`) || !strings.Contains(read.Body.String(), `"readAt":`) {
		t.Fatal("actual read destination", read.Body.String())
	}
	t.Log("ACTUAL_PUBLIC_UPDATE_READ_JSON " + read.Body.String())
	public := f.request(t, "GET", "/v1/businesses/"+f.business, f.tokens[2], "", 200)
	t.Log("ACTUAL_PUBLIC_UPDATE_PUBLIC_JSON " + public.Body.String())
	f.request(t, "PUT", f.path("/public-profile/permission"), f.tokens[0], businessConsoleHTTPJSON(t, supplierprofile.PermissionInput{ExpectedVersion: 1, Action: "revoke"}), 200)
	list = f.request(t, "GET", "/v1/me/inbox", f.tokens[2], "", 200)
	if strings.Contains(list.Body.String(), `"resourceType":"business_update"`) {
		t.Fatal("revoked public source remains visible", list.Body.String())
	}
	f.request(t, "POST", "/v1/me/inbox/"+inbox.Data[0].ID+"/read", f.tokens[2], "", 404)
}
